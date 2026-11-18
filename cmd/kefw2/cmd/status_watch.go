// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

const watchRecoveryDelay = 5 * time.Second

const (
	watchSnapshotUpdate = "snapshot"
	watchActionUpdate   = "action"
	watchKeyLeft        = "left"
	watchItemWithValue  = "itemWithValue"
)

type watchUpdate struct {
	kind     string
	snapshot statusSnapshot
	err      error
}

type watchDependencies struct {
	read   func(context.Context) (statusSnapshot, error)
	action func(context.Context, string) error
	events func(context.Context, chan<- struct{}, chan<- error)
	after  func(time.Duration) <-chan time.Time
}

var statusWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch status changes, or stream complete JSON snapshots",
	Long: `Follow speaker events and update status in place.

Cover art stays in place during text updates. Use --minimal to hide it or
--ascii to force text artwork. Native artwork supports Kitty-compatible
terminals and iTerm2 outside tmux; other terminals use ASCII.

Controls: Space play/pause, n/p next/previous, +/- volume, m mute,
Left/Right seek 10 seconds, q or Ctrl+C quit. Controls never wake the speaker.

With --json, emit complete JSON Lines snapshots without keyboard controls or
artwork downloads. Events drive updates; if events disconnect, watch reports
the problem and polls internally while reconnecting.`,
	Args: cobra.NoArgs,
	RunE: runStatusWatch,
}

func init() {
	statusCmd.AddCommand(statusWatchCmd)
	addJSONFlag(statusWatchCmd)
}

func runStatusWatch(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	speaker := currentSpeaker
	eventSpeaker := *speaker
	deps := watchDependencies{
		read:   func(ctx context.Context) (statusSnapshot, error) { return readStatus(ctx, speaker) },
		action: func(ctx context.Context, key string) error { return controlWatch(ctx, speaker, key) },
		events: func(ctx context.Context, refresh chan<- struct{}, health chan<- error) {
			followWatchEvents(ctx, &eventSpeaker, refresh, health)
		},
		after: time.After,
	}
	if !jsonOutput(cmd) {
		return runWatchTUI(ctx, cmd, deps)
	}
	return watchStatus(ctx, deps, nil, func(update watchUpdate) error {
		if update.kind == watchSnapshotUpdate {
			return writeSpeakerJSON(cmd, update.snapshot.Data)
		}
		if update.err != nil {
			if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "%s: %v (retrying)\n", update.kind, update.err); err != nil {
				return fmt.Errorf("writing watch diagnostic: %w", err)
			}
		}
		return nil
	})
}

func watchStatus(
	ctx context.Context, deps watchDependencies, actions <-chan string, emit func(watchUpdate) error,
) error {
	err := runWatchStatus(ctx, deps, actions, emit)
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return nil
	}
	return err
}

func runWatchStatus(
	ctx context.Context, deps watchDependencies, actions <-chan string, emit func(watchUpdate) error,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	refresh := make(chan struct{}, 1)
	health := make(chan error, 1)
	healthy, stale := false, true
	read := func() error {
		snapshot, err := deps.read(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		stale = err != nil
		kind := watchSnapshotUpdate
		if stale {
			kind = "status unavailable"
		}
		return emit(watchUpdate{kind: kind, snapshot: snapshot, err: err})
	}
	if err := read(); err != nil || ctx.Err() != nil {
		return err
	}
	eventDone := make(chan struct{})
	go func() {
		defer close(eventDone)
		deps.events(ctx, refresh, health)
	}()
	defer func() { cancel(); <-eventDone }()
	var retry <-chan time.Time
	for {
		if !healthy || stale {
			if retry == nil {
				retry = deps.after(watchRecoveryDelay)
			}
		} else {
			retry = nil
		}
		select {
		case <-ctx.Done():
			return nil
		case eventErr := <-health:
			wasHealthy := healthy
			healthy = eventErr == nil
			if err := emit(watchUpdate{kind: "event connection", err: eventErr}); err != nil {
				return err
			}
			if healthy && !wasHealthy {
				signalWatchRefresh(refresh)
			}
			continue
		case <-refresh:
			if stale {
				continue
			}
		case <-retry:
			retry = nil
		case key := <-actions:
			if err := performWatchAction(ctx, deps, key, emit); err != nil {
				return err
			}
		}
		if err := read(); err != nil {
			return err
		}
	}
}

func performWatchAction(ctx context.Context, deps watchDependencies, key string, emit func(watchUpdate) error) error {
	err := deps.action(ctx, key)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return emit(watchUpdate{kind: watchActionUpdate, err: err})
}

func signalWatchRefresh(refresh chan<- struct{}) {
	select {
	case refresh <- struct{}{}:
	default:
	}
}

func followWatchEvents(ctx context.Context, speaker *kefw2.KEFSpeaker, refresh chan<- struct{}, health chan<- error) {
	report := func(err error) {
		select {
		case health <- err:
		case <-ctx.Done():
		}
	}
	for ctx.Err() == nil {
		err := watchEventConnection(ctx, speaker, refresh, report)
		if ctx.Err() != nil {
			return
		}
		report(err)
		timer := time.NewTimer(watchRecoveryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func watchEventConnection(
	ctx context.Context, speaker *kefw2.KEFSpeaker, refresh chan<- struct{}, report func(error),
) error {
	subscriptions := []kefw2.EventSubscription{
		{Path: "settings:/kef/play/physicalSource", Type: watchItemWithValue},
		{Path: "settings:/kef/host/speakerStatus", Type: watchItemWithValue},
		{Path: "player:volume", Type: watchItemWithValue},
		{Path: "settings:/mediaPlayer/mute", Type: watchItemWithValue},
		{Path: "player:player/data", Type: watchItemWithValue},
		{Path: "player:player/data/playTime", Type: watchItemWithValue},
	}
	var lastFailure string
	observe := func(err error) {
		failure := ""
		if err != nil {
			failure = err.Error()
		}
		if failure != lastFailure {
			lastFailure = failure
			report(err)
		}
	}
	client, err := speaker.NewEventClientContext(ctx, kefw2.WithSubscriptions(subscriptions),
		kefw2.WithPlayerDataInvalidations(), kefw2.WithEventPollObserver(observe))
	if err != nil {
		return err
	}
	report(nil)
	done := make(chan error, 1)
	go func() { done <- client.Start(ctx) }()
	defer client.Close()
	for {
		select {
		case <-ctx.Done():
			return <-done
		case err := <-done:
			if err == nil {
				err = errors.New("event stream ended")
			}
			return err
		case <-client.Events():
			signalWatchRefresh(refresh)
		}
	}
}
