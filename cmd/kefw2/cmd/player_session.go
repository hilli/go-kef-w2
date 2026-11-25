// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

type playerSession interface {
	context() context.Context
	messages() <-chan tea.Msg
	done() <-chan struct{}
	cancel()
	refreshStatus(uint64)
}

// A factory returns immediately; its session owns any I/O in separate workers.
type playerSessionFactory func(context.Context, playerSpeaker, uint64) playerSession

type playerMaxVolumeResult struct {
	volume int
	err    error
}

type playerInfoResult struct {
	speaker playerSpeaker
	err     error
}

type playerSessionFailure struct{ err error }

type playerStatusRefreshedResult struct {
	sequence uint64
	update   watchUpdate
}

type watchPlayerSession struct {
	ctx            context.Context
	stop           context.CancelFunc
	updates        chan tea.Msg
	finished       chan struct{}
	refresh        chan string
	refreshMu      sync.Mutex
	pendingRefresh uint64
}

func newPlayerSession(parent context.Context, target playerSpeaker, _ uint64) playerSession {
	ctx, cancel := context.WithCancel(parent)
	session := &watchPlayerSession{
		ctx: ctx, stop: cancel, updates: make(chan tea.Msg, 8), finished: make(chan struct{}),
		refresh: make(chan string, 1),
	}
	go func() {
		var workers sync.WaitGroup
		workers.Add(3)
		go func() { defer workers.Done(); session.watch(target) }()
		go func() { defer workers.Done(); session.readMaximum(target) }()
		go func() { defer workers.Done(); session.readInfo(target) }()
		workers.Wait()
		close(session.updates)
		close(session.finished)
	}()
	return session
}

func (s *watchPlayerSession) context() context.Context { return s.ctx }
func (s *watchPlayerSession) messages() <-chan tea.Msg { return s.updates }
func (s *watchPlayerSession) done() <-chan struct{}    { return s.finished }
func (s *watchPlayerSession) cancel()                  { s.stop() }

func (s *watchPlayerSession) refreshStatus(sequence uint64) {
	if sequence == 0 || s.ctx.Err() != nil {
		return
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.pendingRefresh = max(s.pendingRefresh, sequence)
	select {
	case s.refresh <- "":
	default:
	}
}

func (s *watchPlayerSession) takeRefresh() uint64 {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	sequence := s.pendingRefresh
	s.pendingRefresh = 0
	select {
	case <-s.refresh:
	default:
	}
	return sequence
}

func (s *watchPlayerSession) send(msg tea.Msg) bool {
	select {
	case s.updates <- msg:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *watchPlayerSession) watch(target playerSpeaker) {
	statusSpeaker := target.newSpeaker()
	eventSpeaker := target.newSpeaker()
	var refreshSequence uint64
	deps := watchDependencies{
		read: func(ctx context.Context) (statusSnapshot, error) { return readStatus(ctx, statusSpeaker) },
		action: func(context.Context, string) error {
			refreshSequence = s.takeRefresh()
			return nil
		},
		events: func(ctx context.Context, refresh chan<- struct{}, health chan<- error) {
			followWatchEvents(ctx, eventSpeaker, refresh, health)
		},
		after: time.After,
	}
	err := watchStatus(s.ctx, deps, s.refresh, func(update watchUpdate) error {
		if update.kind == watchActionUpdate {
			return nil
		}
		if refreshSequence != 0 &&
			(update.kind == watchSnapshotUpdate || update.kind == playerStatusUnavailableUpdate) {
			sequence := refreshSequence
			refreshSequence = 0
			if !s.send(playerStatusRefreshedResult{sequence: sequence, update: update}) {
				return s.ctx.Err()
			}
			return nil
		}
		if !s.send(update) {
			return s.ctx.Err()
		}
		return nil
	})
	if s.ctx.Err() == nil {
		if err == nil {
			err = errors.New("speaker status watcher stopped")
		}
		s.send(playerSessionFailure{err: err})
		s.cancel()
	}
}

func (s *watchPlayerSession) readMaximum(target playerSpeaker) {
	speaker := target.newSpeaker()
	for s.ctx.Err() == nil {
		volume, err := speaker.GetMaxVolume(s.ctx)
		if !s.send(playerMaxVolumeResult{volume: volume, err: err}) || err == nil {
			return
		}
		if !s.waitToRetry() {
			return
		}
	}
}

func (s *watchPlayerSession) readInfo(target playerSpeaker) {
	speaker := target.newSpeaker()
	for s.ctx.Err() == nil {
		err := speaker.UpdateInfo(s.ctx)
		info := playerSpeakerFromConfig(*speaker, "")
		info.Configured, info.Default = target.Configured, target.Default
		if !s.send(playerInfoResult{speaker: info, err: err}) || err == nil {
			return
		}
		if !s.waitToRetry() {
			return
		}
	}
}

func (s *watchPlayerSession) waitToRetry() bool {
	timer := time.NewTimer(watchRecoveryDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-s.ctx.Done():
		return false
	}
}
