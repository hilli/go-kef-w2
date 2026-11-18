// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/hilli/go-kef-w2/kefw2"
)

type playerControlAction string

const (
	playerControlMute         playerControlAction = "mute"
	playerControlPlayPause    playerControlAction = "play/pause"
	playerControlPrevious     playerControlAction = "previous"
	playerControlNext         playerControlAction = "next"
	playerControlSeekBackward playerControlAction = "seek backward"
	playerControlSeekForward  playerControlAction = "seek forward"
)

const (
	playerKeyRight = "right"
	playerKeyDown  = "down"
)

// playerControls owns an immutable copy of one speaker and serializes its
// mutations. Reads and rate-limit waits never hold the mutation slot.
type playerControls struct {
	speaker   kefw2.KEFSpeaker
	mutations chan struct{}
}

func newPlayerControls(speaker *kefw2.KEFSpeaker) (*playerControls, error) {
	if speaker == nil {
		return nil, errors.New("player speaker is nil")
	}
	if speaker.IPAddress == "" {
		return nil, kefw2.ErrEmptyIPAddress
	}
	controls := &playerControls{speaker: *speaker, mutations: make(chan struct{}, 1)}
	controls.mutations <- struct{}{}
	return controls, nil
}

func (c *playerControls) target() string {
	if c.speaker.Name == "" {
		return c.speaker.IPAddress
	}
	return fmt.Sprintf("%s (%s)", c.speaker.Name, c.speaker.IPAddress)
}

func (c *playerControls) acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.mutations:
	}
	if err := ctx.Err(); err != nil {
		c.release()
		return err
	}
	return nil
}

func (c *playerControls) release() {
	c.mutations <- struct{}{}
}

type playerControlRead struct {
	Snapshot   statusSnapshot
	StatusErr  error
	Maximum    int
	MaximumErr error
}

type playerMutationUncertainError struct {
	Action string
	Target string
	Err    error
}

type playerMutationDispatchedError struct{ Err error }

func (e *playerMutationDispatchedError) Error() string { return e.Err.Error() }
func (e *playerMutationDispatchedError) Unwrap() error { return e.Err }

func (e *playerMutationUncertainError) Error() string {
	return fmt.Sprintf("%s on %s may have been accepted: %v", e.Action, e.Target, e.Err)
}

func (e *playerMutationUncertainError) Unwrap() error { return e.Err }

func (c *playerControls) uncertain(action string, err error) error {
	return &playerMutationUncertainError{Action: action, Target: c.target(), Err: err}
}

func playerReadStatus(ctx context.Context, speaker *kefw2.KEFSpeaker) (statusSnapshot, error) {
	snapshot, err := readStatus(ctx, speaker)
	if err != nil {
		return statusSnapshot{}, err
	}
	if snapshot.Data.Volume < 0 || snapshot.Data.Volume > 100 {
		return statusSnapshot{}, fmt.Errorf("speaker returned invalid volume %d", snapshot.Data.Volume)
	}
	return snapshot, nil
}

func playerReadMaximumVolume(ctx context.Context, speaker *kefw2.KEFSpeaker) (int, error) {
	maximum, err := speaker.GetMaxVolume(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting maximum volume: %w", err)
	}
	if maximum < 0 || maximum > 100 {
		return 0, fmt.Errorf("speaker returned invalid maximum volume %d", maximum)
	}
	return maximum, nil
}

// Read reports status and maximum independently; callers must inspect both errors.
// As with the mutation methods, call it in a worker rather than a Tea update.
func (c *playerControls) Read(ctx context.Context) playerControlRead {
	speaker := c.speaker
	snapshot, statusErr := playerReadStatus(ctx, &speaker)
	maximum, maximumErr := playerReadMaximumVolume(ctx, &speaker)
	return playerControlRead{Snapshot: snapshot, StatusErr: statusErr, Maximum: maximum, MaximumErr: maximumErr}
}

func playerControlKey(action playerControlAction) (string, error) {
	switch action {
	case playerControlMute:
		return "m", nil
	case playerControlPlayPause:
		return " ", nil
	case playerControlPrevious:
		return "p", nil
	case playerControlNext:
		return "n", nil
	case playerControlSeekBackward:
		return watchKeyLeft, nil
	case playerControlSeekForward:
		return playerKeyRight, nil
	default:
		return "", fmt.Errorf("unsupported player action %q", action)
	}
}

// playerControlUnavailable is suitable for rendering disabled controls. The
// mutation path checks again using a fresh snapshot, not this presentation state.
func playerControlUnavailable(snapshot statusSnapshot, action playerControlAction) error {
	if _, err := playerControlKey(action); err != nil {
		return err
	}
	if snapshot.Data.Power != kefw2.SpeakerStatusOn || snapshot.Data.Source == kefw2.SourceStandby {
		return errors.New("controls do not wake a standby speaker")
	}
	if action == playerControlMute {
		return nil
	}
	playback := snapshot.Data.Playback
	if (snapshot.Data.Source != kefw2.SourceWiFi && snapshot.Data.Source != kefw2.SourceBluetooth) ||
		playback == nil || (playback.State != kefw2.PlayerStatePlaying && playback.State != kefw2.PlayerStatePaused) {
		return errors.New("control requires existing WiFi/Bluetooth playback")
	}
	switch action {
	case playerControlPlayPause:
		if !snapshot.Player.Controls.Pause {
			return errors.New("play/pause is unavailable for this source")
		}
	case playerControlPrevious:
		if !snapshot.Player.Controls.Previous {
			return errors.New("previous track is unavailable")
		}
	case playerControlNext:
		if !snapshot.Player.Controls.Next {
			return errors.New("next track is unavailable")
		}
	case playerControlSeekBackward, playerControlSeekForward:
		return playerSeekUnavailable(snapshot)
	}
	return nil
}

func playerSeekUnavailable(snapshot statusSnapshot) error {
	playback := snapshot.Data.Playback
	if playback.State != kefw2.PlayerStatePlaying || playback.PositionMS == nil ||
		playback.DurationMS == nil || *playback.PositionMS < 0 || *playback.DurationMS <= 0 ||
		snapshot.Player.MediaRoles.MediaData.MetaData.Live {
		return errors.New("seeking requires playing media with a known position and duration")
	}
	return nil
}

func (c *playerControls) readAfterMutation(
	ctx context.Context, speaker *kefw2.KEFSpeaker, action string,
) (statusSnapshot, error) {
	snapshot, err := playerReadStatus(ctx, speaker)
	if err != nil {
		return statusSnapshot{}, c.uncertain(action, fmt.Errorf("readback failed: %w", err))
	}
	return snapshot, nil
}

func playerAwakeSource(ctx context.Context, speaker *kefw2.KEFSpeaker) (kefw2.Source, error) {
	source, err := speaker.Source(ctx)
	if err != nil {
		return "", fmt.Errorf("getting source: %w", err)
	}
	power, err := speaker.SpeakerState(ctx)
	if err != nil {
		return "", fmt.Errorf("getting power state: %w", err)
	}
	if power != kefw2.SpeakerStatusOn || source == kefw2.SourceStandby {
		return "", errors.New("controls do not wake a standby speaker")
	}
	return source, nil
}

func (c *playerControls) toggleMute(ctx context.Context, speaker *kefw2.KEFSpeaker) (bool, error) {
	source, err := playerAwakeSource(ctx, speaker)
	if err != nil {
		return false, fmt.Errorf("checking mute on %s: %w", c.target(), err)
	}
	muted, err := speaker.IsMuted(ctx)
	if err != nil {
		return false, fmt.Errorf("getting mute state on %s: %w", c.target(), err)
	}
	currentSource, err := playerAwakeSource(ctx, speaker)
	if err != nil {
		return muted, fmt.Errorf("checking mute on %s: %w", c.target(), err)
	}
	if currentSource != source {
		return muted, fmt.Errorf("source on %s changed while checking mute; try again", c.target())
	}
	if muted {
		err = speaker.Unmute(ctx)
	} else {
		err = speaker.Mute(ctx)
	}
	if err != nil {
		return muted, c.uncertain("mute", err)
	}
	confirmed, err := speaker.IsMuted(ctx)
	if err != nil {
		return muted, c.uncertain("mute", fmt.Errorf("readback failed: %w", err))
	}
	if confirmed == muted {
		return confirmed, &playerMutationDispatchedError{
			Err: fmt.Errorf("mute on %s did not change after the request", c.target()),
		}
	}
	return confirmed, nil
}

// ToggleMute confirms the mute bit independently of playback metadata.
func (c *playerControls) ToggleMute(ctx context.Context) (bool, error) {
	if err := c.acquire(ctx); err != nil {
		return false, err
	}
	defer c.release()
	speaker := c.speaker
	return c.toggleMute(ctx, &speaker)
}

// Perform uses a fresh snapshot and the status-watch playback implementation;
// successful results are read back rather than inferred from the request.
func (c *playerControls) Perform(ctx context.Context, action playerControlAction) (statusSnapshot, error) {
	key, err := playerControlKey(action)
	if err != nil {
		return statusSnapshot{}, err
	}
	if err := c.acquire(ctx); err != nil {
		return statusSnapshot{}, err
	}
	defer c.release()

	speaker := c.speaker
	if action == playerControlMute {
		if _, err := c.toggleMute(ctx, &speaker); err != nil {
			return statusSnapshot{}, err
		}
		return c.readAfterMutation(ctx, &speaker, string(action))
	}
	snapshot, err := playerReadStatus(ctx, &speaker)
	if err != nil {
		return statusSnapshot{}, fmt.Errorf("checking %s on %s: %w", action, c.target(), err)
	}
	if err := playerControlUnavailable(snapshot, action); err != nil {
		return statusSnapshot{}, fmt.Errorf("%s on %s: %w", action, c.target(), err)
	}
	source, err := playerAwakeSource(ctx, &speaker)
	if err != nil {
		return statusSnapshot{}, fmt.Errorf("checking %s on %s: %w", action, c.target(), err)
	}
	if source != snapshot.Data.Source {
		return statusSnapshot{}, fmt.Errorf("source on %s changed while checking %s; try again", c.target(), action)
	}
	err = controlWatchPlayback(ctx, &speaker, snapshot, key)
	if err != nil {
		return statusSnapshot{}, c.uncertain(string(action), err)
	}
	return c.readAfterMutation(ctx, &speaker, string(action))
}

// SetSource is an explicit action and may wake the selected speaker. Callers
// must filter model-specific inputs; a known source name does not prove support.
func (c *playerControls) SetSource(ctx context.Context, source kefw2.Source) (statusSnapshot, error) {
	canonical, err := parseSource(string(source))
	if err != nil {
		return statusSnapshot{}, fmt.Errorf("invalid source: %w", err)
	}
	if err := c.acquire(ctx); err != nil {
		return statusSnapshot{}, err
	}
	defer c.release()

	speaker := c.speaker
	if err := speaker.SetSource(ctx, canonical); err != nil {
		return statusSnapshot{}, c.uncertain(fmt.Sprintf("switch to %s", canonical), err)
	}
	return c.readAfterMutation(ctx, &speaker, fmt.Sprintf("switch to %s", canonical))
}

// SeekTo is an absolute counterpart to the watch's relative seek controls.
func (c *playerControls) SeekTo(ctx context.Context, positionMS int64) (statusSnapshot, error) {
	if err := c.acquire(ctx); err != nil {
		return statusSnapshot{}, err
	}
	defer c.release()

	speaker := c.speaker
	snapshot, err := playerReadStatus(ctx, &speaker)
	if err != nil {
		return statusSnapshot{}, fmt.Errorf("checking seek on %s: %w", c.target(), err)
	}
	if err := playerControlUnavailable(snapshot, playerControlSeekForward); err != nil {
		return statusSnapshot{}, fmt.Errorf("seek on %s: %w", c.target(), err)
	}
	source, err := playerAwakeSource(ctx, &speaker)
	if err != nil {
		return statusSnapshot{}, fmt.Errorf("checking seek on %s: %w", c.target(), err)
	}
	if source != snapshot.Data.Source {
		return statusSnapshot{}, fmt.Errorf("source on %s changed while checking seek; try again", c.target())
	}
	positionMS = max(int64(0), min(int64(*snapshot.Data.Playback.DurationMS), positionMS))
	if err := speaker.SeekTo(ctx, positionMS); err != nil {
		return statusSnapshot{}, c.uncertain("seek", err)
	}
	return c.readAfterMutation(ctx, &speaker, "seek")
}

type playerVolumeOutcome struct {
	Target            int
	Confirmed         int
	ConfirmedKnown    bool
	ReadbackConfirmed bool
	Maximum           int
	MaximumRead       bool
	MaximumErr        error
	Attempted         bool
	Uncertain         bool
}

// SetVolume rechecks power, current volume and the current speaker's limit.
// An unavailable limit permits only decreases from the freshly read volume.
func (c *playerControls) SetVolume(ctx context.Context, target int) (playerVolumeOutcome, error) {
	var outcome playerVolumeOutcome
	if err := c.acquire(ctx); err != nil {
		return outcome, err
	}
	defer c.release()

	speaker := c.speaker
	source, err := playerAwakeSource(ctx, &speaker)
	if err != nil {
		return outcome, fmt.Errorf("checking volume on %s: %w", c.target(), err)
	}
	current, err := speaker.GetVolume(ctx)
	if err != nil {
		return outcome, fmt.Errorf("getting volume on %s: %w", c.target(), err)
	}
	if current < 0 || current > 100 {
		return outcome, fmt.Errorf("speaker %s returned invalid volume %d", c.target(), current)
	}
	outcome.Confirmed, outcome.ConfirmedKnown = current, true
	target = max(0, min(100, target))
	outcome.Maximum, outcome.MaximumErr = playerReadMaximumVolume(ctx, &speaker)
	outcome.MaximumRead = true
	if err := ctx.Err(); err != nil {
		return outcome, err
	}
	if outcome.MaximumErr != nil {
		if target > current {
			return outcome, fmt.Errorf("increasing volume on %s is blocked: %w", c.target(), outcome.MaximumErr)
		}
	} else {
		target = min(target, outcome.Maximum)
	}
	outcome.Target = target
	if target == current {
		outcome.ReadbackConfirmed = true
		return outcome, nil
	}
	currentSource, err := playerAwakeSource(ctx, &speaker)
	if err != nil {
		return outcome, fmt.Errorf("checking volume on %s: %w", c.target(), err)
	}
	if currentSource != source {
		return outcome, fmt.Errorf("source on %s changed while checking volume; try again", c.target())
	}
	if outcome.MaximumErr != nil {
		current, err = speaker.GetVolume(ctx)
		if err != nil {
			return outcome, fmt.Errorf("rechecking volume on %s: %w", c.target(), err)
		}
		if current < 0 || current > 100 {
			return outcome, fmt.Errorf("speaker %s returned invalid volume %d", c.target(), current)
		}
		outcome.Confirmed = current
		if target > current {
			return outcome, fmt.Errorf("increasing volume on %s is blocked: %w", c.target(), outcome.MaximumErr)
		}
		if target == current {
			outcome.ReadbackConfirmed = true
			return outcome, nil
		}
	}
	return c.writeVolume(ctx, &speaker, outcome)
}

func (c *playerControls) writeVolume(
	ctx context.Context, speaker *kefw2.KEFSpeaker, outcome playerVolumeOutcome,
) (playerVolumeOutcome, error) {
	if err := ctx.Err(); err != nil {
		return outcome, err
	}
	target := outcome.Target
	outcome.Attempted = true
	if err := speaker.SetVolume(ctx, target); err != nil {
		outcome.Uncertain = true
		return outcome, c.uncertain("set volume", err)
	}
	confirmed, err := speaker.GetVolume(ctx)
	if err != nil {
		outcome.Uncertain = true
		return outcome, c.uncertain("set volume", fmt.Errorf("readback failed: %w", err))
	}
	if confirmed < 0 || confirmed > 100 {
		outcome.Uncertain = true
		return outcome, c.uncertain("set volume", fmt.Errorf("invalid volume %d in readback", confirmed))
	}
	outcome.Confirmed, outcome.ReadbackConfirmed = confirmed, true
	if confirmed != target {
		return outcome, fmt.Errorf("volume on %s requested %d, but confirmed %d", c.target(), target, confirmed)
	}
	return outcome, nil
}

// A one-cell slider maps to zero; a zero-width slider is not interactive.
func playerVolumeAtPosition(x, width, maximum int) (int, error) {
	if maximum < 0 || maximum > 100 {
		return 0, fmt.Errorf("invalid maximum volume %d", maximum)
	}
	if width <= 0 {
		return 0, errors.New("volume slider has no width")
	}
	if width == 1 {
		return 0, nil
	}
	position := max(0, min(width-1, x))
	value := int(math.Round(float64(position) / float64(width-1) * float64(maximum)))
	return max(0, min(maximum, value)), nil
}

// A nil maximum means it could not be loaded: decreases still work, but
// increases and End are rejected instead of assuming a ceiling.
func playerVolumeFromKey(current int, maximum *int, key string) (int, error) {
	if current < 0 || current > 100 {
		return 0, fmt.Errorf("invalid current volume %d", current)
	}
	if maximum != nil && (*maximum < 0 || *maximum > 100) {
		return 0, fmt.Errorf("invalid maximum volume %d", *maximum)
	}
	step := 0
	switch key {
	case "+", "=", playerKeyRight:
		step = 1
	case "-", "left":
		step = -1
	case "up":
		step = 5
	case playerKeyDown:
		step = -5
	case "home":
		return 0, nil
	case "end":
		if maximum == nil {
			return current, errors.New("maximum volume is unavailable; increases are disabled")
		}
		return *maximum, nil
	default:
		return current, fmt.Errorf("unsupported volume key %q", key)
	}
	if step > 0 && maximum == nil {
		return current, errors.New("maximum volume is unavailable; increases are disabled")
	}
	if step > 0 && maximum != nil && current > *maximum {
		return current, errors.New("current volume exceeds the maximum; increases are disabled")
	}
	value := max(0, min(100, current+step))
	if maximum != nil {
		value = min(value, *maximum)
	}
	return value, nil
}

const playerVolumeWriteInterval = 100 * time.Millisecond

type playerVolumeWriter func(context.Context, int) (playerVolumeOutcome, error)

type playerVolumeClock struct {
	now   func() time.Time
	after func(time.Duration) <-chan time.Time
}

type playerVolumeState struct {
	Revision     uint64
	Target       int
	Confirmed    int
	Pending      bool
	InFlight     bool
	Stopped      bool
	Err          error
	Uncertain    bool
	CanRetry     bool
	RetryTarget  int
	Maximum      int
	MaximumKnown bool
	MaximumErr   error
}

// playerVolumeCoalescer stores state under a short-held mutex. Changes is a
// level-triggered notification: consumers read State after receiving it.
type playerVolumeCoalescer struct {
	ctx     context.Context
	cancel  context.CancelFunc
	write   playerVolumeWriter
	clock   playerVolumeClock
	mu      sync.Mutex
	state   playerVolumeState
	queued  bool
	wake    chan struct{}
	changes chan struct{}
	done    chan struct{}
}

func newPlayerVolumeCoalescer(
	ctx context.Context, confirmed int, write playerVolumeWriter,
) (*playerVolumeCoalescer, error) {
	return newPlayerVolumeCoalescerWithClock(ctx, confirmed, write, playerVolumeClock{
		now: time.Now, after: time.After,
	})
}

func newPlayerVolumeCoalescerWithClock(
	ctx context.Context, confirmed int, write playerVolumeWriter, clock playerVolumeClock,
) (*playerVolumeCoalescer, error) {
	if ctx == nil || write == nil || clock.now == nil || clock.after == nil {
		return nil, errors.New("volume coalescer requires a context, writer, and clock")
	}
	if confirmed < 0 || confirmed > 100 {
		return nil, fmt.Errorf("invalid confirmed volume %d", confirmed)
	}
	ctx, cancel := context.WithCancel(ctx)
	c := &playerVolumeCoalescer{
		ctx: ctx, cancel: cancel, write: write, clock: clock,
		state: playerVolumeState{Target: confirmed, Confirmed: confirmed},
		wake:  make(chan struct{}, 1), changes: make(chan struct{}, 1), done: make(chan struct{}),
	}
	go c.run()
	return c, nil
}

func (c *playerVolumeCoalescer) changedLocked() {
	c.state.Revision++
	select {
	case c.changes <- struct{}{}:
	default:
	}
}

func (c *playerVolumeCoalescer) signalLocked() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *playerVolumeCoalescer) setTarget(target int, release bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if c.state.Err != nil {
		return fmt.Errorf("volume change requires an explicit retry or dismissal: %w", c.state.Err)
	}
	target = max(0, min(100, target))
	if (c.state.Pending && c.state.Target == target) ||
		(release && !c.state.Pending && c.state.Confirmed == target) {
		return nil
	}
	c.state.Target, c.state.Pending = target, true
	c.queued = true
	c.changedLocked()
	c.signalLocked()
	return nil
}

func (c *playerVolumeCoalescer) SetTarget(target int) error {
	return c.setTarget(target, false)
}

// Release retains the final drag value even when an earlier write is in flight.
func (c *playerVolumeCoalescer) Release(target int) error {
	return c.setTarget(target, true)
}

// Retry is the only way to replay a failed request. A new gesture may instead
// explicitly dismiss the error before setting another target.
func (c *playerVolumeCoalescer) Retry() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if !c.state.CanRetry || c.state.Pending {
		return errors.New("there is no failed volume change to retry")
	}
	c.state.Target, c.state.Pending = c.state.RetryTarget, true
	c.queued = true
	c.changedLocked()
	c.signalLocked()
	return nil
}

func (c *playerVolumeCoalescer) DismissError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if c.state.Pending {
		return errors.New("a volume change is still pending")
	}
	c.state.Err, c.state.Uncertain, c.state.CanRetry = nil, false, false
	c.changedLocked()
	return nil
}

func (c *playerVolumeCoalescer) Changes() <-chan struct{} { return c.changes }

func (c *playerVolumeCoalescer) State() playerVolumeState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// ObserveConfirmed accepts an independent status read only if the caller
// captured revision before starting that read and no volume activity raced it.
func (c *playerVolumeCoalescer) ObserveConfirmed(value int, revision uint64) (bool, error) {
	if value < 0 || value > 100 {
		return false, fmt.Errorf("invalid confirmed volume %d", value)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ctx.Err(); err != nil {
		return false, err
	}
	if c.state.Revision != revision || c.state.Pending {
		return false, nil
	}
	if c.state.Confirmed == value && c.state.Target == value {
		return true, nil
	}
	c.state.Confirmed, c.state.Target = value, value
	c.changedLocked()
	return true, nil
}

func (c *playerVolumeCoalescer) Stop()                 { c.cancel() }
func (c *playerVolumeCoalescer) Done() <-chan struct{} { return c.done }

func (c *playerVolumeCoalescer) finishCancellation(outcome playerVolumeOutcome) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if outcome.ConfirmedKnown {
		c.state.Confirmed = outcome.Confirmed
	}
	if outcome.Attempted && !outcome.ReadbackConfirmed {
		c.state.Err = fmt.Errorf("volume write may have reached the retired speaker: %w", c.ctx.Err())
		c.state.Uncertain = true
	}
	c.queued = false
	c.state.Target, c.state.Pending, c.state.InFlight = c.state.Confirmed, false, false
	c.state.Stopped, c.state.CanRetry = true, false
	c.changedLocked()
}

func (c *playerVolumeCoalescer) complete(target int, outcome playerVolumeOutcome, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.InFlight = false
	if outcome.MaximumRead {
		c.state.Maximum = outcome.Maximum
		c.state.MaximumKnown = outcome.MaximumErr == nil
		c.state.MaximumErr = outcome.MaximumErr
	}
	if outcome.ConfirmedKnown {
		c.state.Confirmed = outcome.Confirmed
	}
	if err == nil && (!outcome.ConfirmedKnown || !outcome.ReadbackConfirmed) {
		err = errors.New("volume change was not confirmed")
		outcome.Uncertain = outcome.Attempted
	}
	if err != nil {
		c.state.Err, c.state.Uncertain = err, outcome.Uncertain
		c.state.CanRetry, c.state.RetryTarget = true, c.state.Target
		c.state.Target, c.state.Pending = c.state.Confirmed, false
		c.queued = false
	} else {
		c.state.Err, c.state.Uncertain, c.state.CanRetry = nil, false, false
		if c.queued && (c.state.Target == target || c.state.Target == outcome.Confirmed) {
			c.queued = false
		}
		if !c.queued {
			c.state.Target, c.state.Pending = outcome.Confirmed, false
		}
	}
	c.changedLocked()
}

func (c *playerVolumeCoalescer) run() {
	defer close(c.done)
	var nextWrite time.Time
	for {
		if c.ctx.Err() != nil {
			c.finishCancellation(playerVolumeOutcome{})
			return
		}
		c.mu.Lock()
		queued := c.queued
		c.mu.Unlock()
		if !queued {
			select {
			case <-c.ctx.Done():
			case <-c.wake:
			}
			continue
		}
		if delay := nextWrite.Sub(c.clock.now()); !nextWrite.IsZero() && delay > 0 {
			select {
			case <-c.ctx.Done():
			case <-c.clock.after(delay):
			}
			continue
		}
		c.mu.Lock()
		if !c.queued {
			c.mu.Unlock()
			continue
		}
		target := c.state.Target
		c.queued, c.state.InFlight = false, true
		c.changedLocked()
		c.mu.Unlock()
		if c.ctx.Err() != nil {
			c.finishCancellation(playerVolumeOutcome{})
			return
		}
		outcome, err := c.write(c.ctx, target)
		if outcome.Attempted {
			// Waiting from completion also covers time spent in preflight/readback,
			// so actual HTTP writes cannot bunch up after a slow preflight.
			nextWrite = c.clock.now().Add(playerVolumeWriteInterval)
		}
		if c.ctx.Err() != nil {
			c.finishCancellation(outcome)
			return
		}
		c.complete(target, outcome, err)
	}
}
