// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/hilli/go-kef-w2/kefw2"
)

type playerTransportRequest struct {
	generation, epoch uint64
	action            playerControlAction
}

type playerMuteRequest struct{ generation, epoch uint64 }

type playerSourceRequest struct {
	generation, epoch uint64
	source            kefw2.Source
}

type playerSeekRequest struct {
	generation, epoch uint64
	positionMS        int64
}

type playerVolumeTargetRequest struct {
	generation, epoch uint64
	target            int
	release           bool
}

type playerVolumeSliderRequest struct {
	generation, epoch uint64
	x, width          int
	release           bool
}

type playerVolumeKeyRequest struct {
	generation, epoch uint64
	key               string
}

type playerVolumeRetryRequest struct{ generation, epoch uint64 }
type playerVolumeDismissRequest struct{ generation, epoch uint64 }
type playerControlDismissErrorRequest struct{ generation, epoch uint64 }

type playerControlsUIState struct {
	// Each request captures this token when the UI creates it.
	epoch             uint64
	ready             bool
	pending           bool
	pendingAction     string
	err               error
	confirmed         *statusSnapshot
	muted             bool
	mutedKnown        bool
	volume            int
	volumeKnown       bool
	sliderTarget      int
	volumePending     bool
	volumeInFlight    bool
	volumeErr         error
	volumeUncertain   bool
	volumeCanRetry    bool
	volumeRetryTarget int
	maximum           *int
	maximumErr        error
	supportedSources  []kefw2.Source
}

type playerControlsUIBackend interface {
	Perform(context.Context, playerControlAction) (statusSnapshot, error)
	ToggleMute(context.Context) (bool, error)
	SetSource(context.Context, kefw2.Source) (statusSnapshot, error)
	SeekTo(context.Context, int64) (statusSnapshot, error)
	SetVolume(context.Context, int) (playerVolumeOutcome, error)
}

type playerControlsUISession struct {
	id, generation uint64
	target         playerSpeaker
	ctx            context.Context
	cancel         context.CancelFunc
	gate           *playerMutationGate
	backend        playerControlsUIBackend
	volume         *playerVolumeCoalescer
	volumeRefresh  chan struct{}
	volumeRevision uint64
	maximumApplied uint64
	maximumReads   *playerControlsMaximumReads
	workers        []<-chan struct{}
}

type playerControlsMaximumReads struct {
	mu       sync.Mutex
	revision uint64
	maximum  int
	err      error
}

func (r *playerControlsMaximumReads) record(outcome playerVolumeOutcome) {
	if !outcome.MaximumRead {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revision++
	r.maximum, r.err = outcome.Maximum, outcome.MaximumErr
}

func (r *playerControlsMaximumReads) state() (uint64, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.revision, r.maximum, r.err
}

type playerControlsUI struct {
	state                   playerControlsUIState
	active                  *playerControlsUISession
	retired                 []*playerControlsUISession
	nextSessionID           uint64
	nextEpoch               uint64
	nextAction              uint64
	activeAction            uint64
	epoch                   uint64
	modified                bool
	refreshing              bool
	refreshWanted           bool
	snapshotOverlapsRefresh bool
	volumeInputErr          error
	volumeIntent            bool
	desiredVolume           int
	backendFactory          func(playerSpeaker) (playerControlsUIBackend, error)
	volumeFactory           func(context.Context, int, playerVolumeWriter) (*playerVolumeCoalescer, error)
	readStatus              func(context.Context, playerSpeaker) (statusSnapshot, error)
}

func newPlayerControlsUI() *playerControlsUI {
	return &playerControlsUI{
		backendFactory: func(target playerSpeaker) (playerControlsUIBackend, error) {
			return newPlayerControls(target.newSpeaker())
		},
		volumeFactory: newPlayerVolumeCoalescer,
		readStatus: func(ctx context.Context, target playerSpeaker) (statusSnapshot, error) {
			return playerReadStatus(ctx, target.newSpeaker())
		},
	}
}

func (u *playerControlsUI) extension() playerExtension {
	return playerExtension{update: u.update, reset: u.reset, stop: u.stop}
}

func (u *playerControlsUI) State() playerControlsUIState {
	state := u.state
	state.supportedSources = slices.Clone(state.supportedSources)
	if state.maximum != nil {
		maximum := *state.maximum
		state.maximum = &maximum
	}
	if state.confirmed != nil {
		snapshot := playerControlsCopySnapshot(*state.confirmed)
		state.confirmed = &snapshot
	}
	return state
}

func (u *playerControlsUI) SupportsSource(source kefw2.Source) bool {
	return slices.Contains(u.state.supportedSources, source)
}

// Model inputs are from KEF's specifications. A USB service port is not an
// audio input, and LSX II LT has no analog input.
// https://international.kef.com/products/lsx-2
// https://international.kef.com/products/lsx-2-lt
// https://international.kef.com/products/ls50-wireless-2
// https://international.kef.com/products/ls60-wireless
func playerControlsSourcesForModel(model string) []kefw2.Source {
	sources := []kefw2.Source{kefw2.SourceWiFi, kefw2.SourceBluetooth, kefw2.SourceStandby}
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "lsxii", "lsx2", "kef lsx ii", "lsx ii":
		return append(sources, kefw2.SourceTV, kefw2.SourceOptical, kefw2.SourceUSB, kefw2.SourceAux)
	case "lsxiilt", "lsx2lt", "kef lsx ii lt", "lsx ii lt":
		return append(sources, kefw2.SourceTV, kefw2.SourceOptical, kefw2.SourceUSB)
	case "ls502w", "ls50wii", "ls50w2", "kef ls50 ii wireless", "kef ls50 wireless ii",
		"ls50 wireless ii", "ls60w", "ls60", "kef ls60 wireless", "ls60 wireless":
		return append(sources, kefw2.SourceTV, kefw2.SourceOptical, kefw2.SourceCoaxial, kefw2.SourceAux)
	default:
		return sources
	}
}

func playerControlsCopySnapshot(snapshot statusSnapshot) statusSnapshot {
	if snapshot.Data.Playback != nil {
		playback := *snapshot.Data.Playback
		if playback.PositionMS != nil {
			position := *playback.PositionMS
			playback.PositionMS = &position
		}
		if playback.DurationMS != nil {
			duration := *playback.DurationMS
			playback.DurationMS = &duration
		}
		if playback.Track != nil {
			track := *playback.Track
			playback.Track = &track
		}
		snapshot.Data.Playback = &playback
	}
	snapshot.Player.TrackRoles.MediaData.Resources = append(
		[]kefw2.PlayerResource(nil), snapshot.Player.TrackRoles.MediaData.Resources...)
	snapshot.Player.MediaRoles.MediaData.Resources = append(
		[]kefw2.PlayerMimeResource(nil), snapshot.Player.MediaRoles.MediaData.Resources...)
	return snapshot
}

type playerControlsVolumeChanged struct {
	sessionID uint64
	state     playerVolumeState
}

type playerControlsVolumeRefresh struct {
	session *playerControlsUISession
}

type playerControlsActionResult struct {
	sessionID, actionID, volumeRevision uint64
	snapshot                            *statusSnapshot
	muted                               *bool
	err                                 error
}

type playerControlsRefreshResult struct {
	sessionID, epoch, volumeRevision uint64
	snapshot                         statusSnapshot
	err                              error
}

func (u *playerControlsUI) update(m *playerModel, msg tea.Msg) (tea.Cmd, bool) {
	u.reap()
	switch msg := msg.(type) {
	case playerSessionStartedMsg:
		if msg.generation != m.generation || m.session == nil {
			return nil, false
		}
		return u.start(m, msg.target), false
	case playerSessionStoppedMsg:
		if u.active != nil && msg.generation == u.active.generation {
			u.suspend()
		}
		return nil, false
	case playerSessionFailure:
		u.suspend()
		return nil, false
	case watchUpdate:
		return u.watch(m, msg), false
	case playerMaxVolumeResult:
		u.readMaximum(m)
		if u.active != nil {
			u.reconcileVolume(u.active.volume.State())
		}
		return nil, false
	case playerInfoResult:
		u.readSources(m)
		return nil, false
	case playerControlsVolumeChanged:
		return u.volumeChanged(msg), true
	case playerControlsVolumeRefresh:
		m.requestStatusRefresh(msg.session.generation)
		return waitPlayerControlsVolumeRefresh(msg.session), true
	case playerControlsActionResult:
		return u.actionFinished(msg), true
	case playerControlsRefreshResult:
		return u.refreshFinished(msg), true
	case playerStatusRefreshCompletedMsg:
		u.statusRefreshCompleted(m, msg)
		return nil, false
	default:
		return u.updateRequest(m, msg)
	}
}

func (u *playerControlsUI) statusRefreshCompleted(m *playerModel, msg playerStatusRefreshCompletedMsg) {
	// The watcher read began after an external action; an overlapping read may be older.
	if msg.generation == m.generation && msg.err == nil && u.snapshotOverlapsRefresh && u.refreshing {
		u.epoch++
		u.refreshWanted = true
	}
	u.snapshotOverlapsRefresh = false
}

func (u *playerControlsUI) updateRequest(m *playerModel, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case playerTransportRequest:
		return u.transport(m, msg), true
	case playerMuteRequest:
		return u.mute(m, msg), true
	case playerSourceRequest:
		return u.source(m, msg), true
	case playerSeekRequest:
		return u.seek(m, msg), true
	case playerVolumeTargetRequest:
		return u.volumeTarget(m, msg), true
	case playerVolumeSliderRequest:
		return u.volumeSlider(m, msg), true
	case playerVolumeKeyRequest:
		return u.volumeKey(m, msg), true
	case playerVolumeRetryRequest:
		u.volumeRetry(m, msg)
		return nil, true
	case playerVolumeDismissRequest:
		u.volumeDismiss(m, msg)
		return nil, true
	case playerControlDismissErrorRequest:
		if u.matchesRequest(m, msg.generation, msg.epoch) {
			u.state.err = nil
		}
		return nil, true
	default:
		return nil, false
	}
}

func (u *playerControlsUI) start(m *playerModel, target playerSpeaker) tea.Cmd {
	if u.active != nil {
		u.retire()
	}
	u.state.ready = false
	u.advanceEpoch()
	if m.mutations == nil {
		u.state.err = errors.New("speaker mutation coordinator is unavailable")
		return nil
	}
	ctx, cancel := context.WithCancel(m.session.context())
	backend, err := u.backendFactory(target)
	if err != nil {
		cancel()
		u.state.err = fmt.Errorf("initializing controls for %s: %w", target.IPAddress, err)
		return nil
	}
	gate := m.mutations
	var volume *playerVolumeCoalescer
	maximumReads := &playerControlsMaximumReads{}
	volumeRefresh := make(chan struct{}, 1)
	writeVolume := func(ctx context.Context, target int) (playerVolumeOutcome, error) {
		ticket := gate.reserve()
		defer ticket.release()
		if err := ticket.wait(ctx); err != nil {
			return playerVolumeOutcome{}, err
		}
		if err := ctx.Err(); err != nil {
			return playerVolumeOutcome{}, err
		}
		if volume != nil {
			target = volume.State().Target
		}
		outcome, err := backend.SetVolume(ctx, target)
		maximumReads.record(outcome)
		if outcome.Attempted {
			select {
			case volumeRefresh <- struct{}{}:
			default:
			}
		}
		return outcome, err
	}
	volume, err = u.volumeFactory(ctx, 0, writeVolume)
	if err != nil {
		cancel()
		u.state.err = fmt.Errorf("initializing volume for %s: %w", target.IPAddress, err)
		return nil
	}
	u.nextSessionID++
	session := &playerControlsUISession{
		id: u.nextSessionID, generation: m.generation, target: target,
		ctx: ctx, cancel: cancel, gate: gate, backend: backend, volume: volume,
		volumeRefresh: volumeRefresh, maximumReads: maximumReads,
	}
	u.active = session
	u.state.ready, u.state.volumeKnown, u.state.mutedKnown = false, false, false
	u.state.pending, u.state.volumePending, u.state.volumeInFlight = false, false, false
	u.state.pendingAction = ""
	u.refreshing, u.refreshWanted, u.modified, u.snapshotOverlapsRefresh = false, false, false, false
	u.volumeIntent = false
	u.epoch++
	u.readMaximum(m)
	u.readSources(m)
	return tea.Batch(waitPlayerControlsVolume(session), waitPlayerControlsVolumeNotice(session),
		waitPlayerControlsVolumeRefresh(session))
}

func (u *playerControlsUI) retire() {
	if u.active == nil {
		return
	}
	u.active.cancel()
	u.active.volume.Stop()
	u.retired = append(u.retired, u.active)
	u.active = nil
}

func (u *playerControlsUI) suspend() {
	u.advanceEpoch()
	if u.active != nil {
		volume := u.active.volume.State()
		if volume.Err != nil {
			u.volumeInputErr = volume.Err
			u.state.volumeErr = volume.Err
		}
	}
	u.retire()
	u.state.ready, u.state.pending = false, false
	u.state.volumePending, u.state.volumeInFlight = false, false
	u.state.pendingAction = ""
	u.state.sliderTarget = u.state.volume
	u.state.volumeCanRetry = false
	u.refreshing, u.refreshWanted, u.snapshotOverlapsRefresh = false, false, false
	u.volumeIntent = false
	u.epoch++
}

func (u *playerControlsUI) reset(_ *playerModel) {
	u.retire()
	u.state = playerControlsUIState{}
	u.advanceEpoch()
	u.volumeInputErr = nil
	u.refreshing, u.refreshWanted, u.modified, u.snapshotOverlapsRefresh = false, false, false, false
	u.volumeIntent = false
	u.epoch++
}

func (u *playerControlsUI) stop(_ *playerModel) []<-chan struct{} {
	u.retire()
	u.state.ready = false
	u.advanceEpoch()
	var done []<-chan struct{}
	for _, session := range u.retired {
		done = append(done, session.volume.Done())
		done = append(done, session.workers...)
	}
	return done
}

func (u *playerControlsUI) advanceEpoch() {
	u.nextEpoch++
	u.state.epoch = u.nextEpoch
}

func playerControlsDone(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func (u *playerControlsUI) reap() {
	retired := u.retired[:0]
	for _, session := range u.retired {
		pending := session.workers[:0]
		for _, done := range session.workers {
			if !playerControlsDone(done) {
				pending = append(pending, done)
			}
		}
		session.workers = pending
		if !playerControlsDone(session.volume.Done()) || len(pending) > 0 {
			retired = append(retired, session)
		}
	}
	u.retired = retired
	if u.active != nil {
		pending := u.active.workers[:0]
		for _, done := range u.active.workers {
			if !playerControlsDone(done) {
				pending = append(pending, done)
			}
		}
		u.active.workers = pending
	}
}

func waitPlayerControlsVolume(session *playerControlsUISession) tea.Cmd {
	return playerScopedCmd(session.generation, func() tea.Msg {
		select {
		case <-session.volume.Changes():
			return playerControlsVolumeChanged{sessionID: session.id, state: session.volume.State()}
		case <-session.ctx.Done():
			return nil
		}
	})
}

func waitPlayerControlsVolumeRefresh(session *playerControlsUISession) tea.Cmd {
	return playerScopedCmd(session.generation, func() tea.Msg {
		select {
		case <-session.volumeRefresh:
			return playerControlsVolumeRefresh{session: session}
		case <-session.volume.Done():
			select {
			case <-session.volumeRefresh:
				return playerControlsVolumeRefresh{session: session}
			default:
				return nil
			}
		}
	})
}

func waitPlayerControlsVolumeNotice(session *playerControlsUISession) tea.Cmd {
	return func() tea.Msg {
		<-session.volume.Done()
		state := session.volume.State()
		if !state.Uncertain {
			return nil
		}
		return playerNoticeMsg{target: session.target, action: "set volume", err: state.Err}
	}
}

func (u *playerControlsUI) readMaximum(m *playerModel) {
	if u.active != nil {
		u.active.maximumApplied, _, _ = u.active.maximumReads.state()
	}
	u.state.maximum, u.state.maximumErr = nil, m.maxVolumeErr
	if m.maxVolume == nil {
		return
	}
	if *m.maxVolume < 0 || *m.maxVolume > 100 {
		u.state.maximumErr = fmt.Errorf("speaker returned invalid maximum volume %d", *m.maxVolume)
		return
	}
	maximum := *m.maxVolume
	u.state.maximum, u.state.maximumErr = &maximum, nil
}

func (u *playerControlsUI) readVolumeMaximum() {
	revision, maximum, err := u.active.maximumReads.state()
	if revision <= u.active.maximumApplied {
		return
	}
	u.active.maximumApplied = revision
	if err != nil {
		u.state.maximum, u.state.maximumErr = nil, err
		return
	}
	if maximum < 0 || maximum > 100 {
		u.state.maximum = nil
		u.state.maximumErr = fmt.Errorf("speaker returned invalid maximum volume %d", maximum)
		return
	}
	u.state.maximum, u.state.maximumErr = &maximum, nil
}

func (u *playerControlsUI) readSources(m *playerModel) {
	model := ""
	if m.speakerInfo != nil && m.speakerInfoErr == nil {
		model = m.speakerInfo.Model
	}
	u.state.supportedSources = playerControlsSourcesForModel(model)
}

func (u *playerControlsUI) watch(m *playerModel, update watchUpdate) tea.Cmd {
	switch update.kind {
	case playerStatusUnavailableUpdate:
		u.suspend()
	case playerEventConnectionUpdate:
		if update.err != nil {
			u.suspend()
		}
	case watchSnapshotUpdate:
		return u.watchSnapshot(m, update)
	}
	return nil
}

func (u *playerControlsUI) watchSnapshot(m *playerModel, update watchUpdate) tea.Cmd {
	if update.err != nil || m.session == nil || m.session.context().Err() != nil ||
		m.connection != playerReady || m.readErr != nil || m.snapshot == nil {
		u.suspend()
		return nil
	}
	if update.snapshot.Data.Volume < 0 || update.snapshot.Data.Volume > 100 {
		u.suspend()
		u.state.err = fmt.Errorf("speaker returned invalid volume %d", update.snapshot.Data.Volume)
		return nil
	}
	u.snapshotOverlapsRefresh = u.refreshing
	var started tea.Cmd
	if u.active == nil && m.target != nil {
		started = u.start(m, *m.target)
	}
	if u.active == nil {
		return started
	}
	if !u.state.ready {
		u.advanceEpoch()
	}
	u.state.ready = true
	if !u.state.volumeKnown || u.state.confirmed == nil {
		u.acceptSnapshot(update.snapshot, u.active.volume.State().Revision)
		return started
	}
	if !u.modified && !u.state.pending && !u.active.volume.State().Pending {
		u.acceptSnapshot(update.snapshot, u.active.volume.State().Revision)
		return started
	}
	if !reflect.DeepEqual(*u.state.confirmed, update.snapshot) {
		u.refreshWanted = true
	}
	return tea.Batch(started, u.refresh())
}

func (u *playerControlsUI) acceptSnapshot(snapshot statusSnapshot, revision uint64) {
	if u.active == nil {
		return
	}
	if snapshot.Data.Volume < 0 || snapshot.Data.Volume > 100 {
		u.suspend()
		u.state.err = fmt.Errorf("speaker returned invalid volume %d", snapshot.Data.Volume)
		return
	}
	accepted, err := u.active.volume.ObserveConfirmed(snapshot.Data.Volume, revision)
	if err != nil {
		u.state.err = err
		return
	}
	if accepted {
		u.state.volumeKnown = true
		u.syncVolume(u.active.volume.State())
	} else if u.state.volumeKnown {
		snapshot.Data.Volume = u.state.volume
	}
	u.state.muted, u.state.mutedKnown = snapshot.Data.Muted, true
	confirmed := playerControlsCopySnapshot(snapshot)
	u.state.confirmed = &confirmed
}

func (u *playerControlsUI) syncVolume(volume playerVolumeState) {
	if u.active == nil || volume.Revision < u.active.volumeRevision {
		return
	}
	u.active.volumeRevision = volume.Revision
	if u.state.volumeKnown {
		u.state.volume, u.state.sliderTarget = volume.Confirmed, volume.Target
		if u.state.confirmed != nil {
			u.state.confirmed.Data.Volume = volume.Confirmed
		}
	}
	u.state.volumePending, u.state.volumeInFlight = volume.Pending, volume.InFlight
	u.state.volumeUncertain = volume.Uncertain
	u.state.volumeCanRetry, u.state.volumeRetryTarget = volume.CanRetry, volume.RetryTarget
	if volume.Err != nil {
		u.state.volumeErr = volume.Err
	} else {
		u.state.volumeErr = u.volumeInputErr
	}
	u.readVolumeMaximum()
	u.reconcileVolume(volume)
}

func (u *playerControlsUI) reconcileVolume(volume playerVolumeState) {
	if !u.volumeIntent {
		return
	}
	if volume.Stopped || volume.Err != nil || !u.state.ready {
		if volume.Pending && volume.Err != nil {
			return
		}
		u.volumeIntent = false
		return
	}
	target := u.desiredVolume
	if u.state.maximum != nil {
		target = min(target, *u.state.maximum)
	} else if target > volume.Confirmed {
		u.volumeIntent = false
		u.setVolumeInputError(errors.New("maximum volume is unavailable; increases are disabled"))
		return
	}
	u.desiredVolume = target
	if volume.Pending {
		if volume.Target != target {
			u.queueReconciledVolume(target)
		}
		return
	}
	if target == volume.Confirmed {
		u.volumeIntent = false
		return
	}
	u.queueReconciledVolume(target)
}

func (u *playerControlsUI) queueReconciledVolume(target int) {
	if err := u.active.volume.SetTarget(target); err != nil {
		u.volumeIntent = false
		u.setVolumeInputError(err)
		return
	}
	u.epoch++
	u.syncVolume(u.active.volume.State())
}

func (u *playerControlsUI) volumeChanged(msg playerControlsVolumeChanged) tea.Cmd {
	if u.active == nil || msg.sessionID != u.active.id {
		return nil
	}
	u.syncVolume(msg.state)
	return tea.Batch(waitPlayerControlsVolume(u.active), u.refresh())
}

func (u *playerControlsUI) isCurrent(m *playerModel, generation uint64) bool {
	return u.active != nil && m.session != nil && generation == m.generation &&
		generation == u.active.generation && u.active.ctx.Err() == nil
}

func (u *playerControlsUI) matchesGeneration(m *playerModel, generation uint64) bool {
	return m.target != nil && generation == m.generation
}

func (u *playerControlsUI) matchesRequest(m *playerModel, generation, epoch uint64) bool {
	return epoch != 0 && epoch == u.state.epoch && u.matchesGeneration(m, generation)
}

func (u *playerControlsUI) ready(m *playerModel, generation uint64) error {
	if !u.isCurrent(m, generation) {
		return errors.New("speaker session is no longer active")
	}
	if !u.state.ready || m.connection != playerReady || m.readErr != nil || u.state.confirmed == nil {
		return errors.New("speaker status is unavailable or stale")
	}
	return nil
}

func (u *playerControlsUI) awake(m *playerModel, generation uint64) error {
	if err := u.ready(m, generation); err != nil {
		return err
	}
	return playerControlUnavailable(*u.state.confirmed, playerControlMute)
}

func (u *playerControlsUI) beginAction(
	m *playerModel, generation uint64, action string,
	run func(context.Context, playerControlsUIBackend) playerControlsActionResult,
) tea.Cmd {
	if !u.matchesGeneration(m, generation) {
		return nil
	}
	if err := u.ready(m, generation); err != nil {
		u.state.err = err
		return nil
	}
	if u.state.pending {
		u.state.err = errors.New("another player control is still pending")
		return nil
	}
	session := u.active
	ticket := session.gate.reserve()
	u.nextAction++
	actionID := u.nextAction
	revision := session.volume.State().Revision
	u.activeAction = actionID
	u.state.pending, u.state.pendingAction, u.state.err = true, action, nil
	u.modified = true
	u.epoch++
	result := make(chan playerControlsActionResult, 1)
	done := make(chan struct{})
	session.workers = append(session.workers, done)
	go func() {
		defer close(done)
		defer ticket.release()
		var value playerControlsActionResult
		if err := ticket.wait(session.ctx); err != nil {
			value.err = err
		} else {
			value = run(session.ctx, session.backend)
		}
		value.sessionID, value.actionID, value.volumeRevision = session.id, actionID, revision
		result <- value
	}()
	return waitPlayerControlsAction(session, result)
}

func waitPlayerControlsAction(session *playerControlsUISession, result <-chan playerControlsActionResult) tea.Cmd {
	return func() tea.Msg {
		value := <-result
		scoped := playerScopedCmd(session.generation, func() tea.Msg { return value })
		var uncertain *playerMutationUncertainError
		var dispatched *playerMutationDispatchedError
		isUncertain := errors.As(value.err, &uncertain)
		if value.err == nil || isUncertain || errors.As(value.err, &dispatched) {
			refresh := playerScopedCmd(session.generation, func() tea.Msg {
				return playerStatusRefreshRequest{generation: session.generation}
			})
			if isUncertain {
				return tea.Batch(scoped, refresh, func() tea.Msg {
					return playerNoticeMsg{target: session.target, action: uncertain.Action, err: uncertain.Err}
				})()
			}
			return tea.Batch(scoped, refresh)()
		}
		return scoped()
	}
}

func (u *playerControlsUI) transport(m *playerModel, request playerTransportRequest) tea.Cmd {
	if !u.matchesRequest(m, request.generation, request.epoch) {
		return nil
	}
	if err := u.ready(m, request.generation); err != nil {
		u.state.err = err
		return nil
	}
	if request.action == playerControlMute {
		u.state.err = errors.New("use the mute control for mute")
		return nil
	}
	if err := playerControlUnavailable(*u.state.confirmed, request.action); err != nil {
		u.state.err = err
		return nil
	}
	return u.beginAction(m, request.generation, string(request.action),
		func(ctx context.Context, backend playerControlsUIBackend) playerControlsActionResult {
			snapshot, err := backend.Perform(ctx, request.action)
			return playerControlsActionResult{snapshot: &snapshot, err: err}
		})
}

func (u *playerControlsUI) mute(m *playerModel, request playerMuteRequest) tea.Cmd {
	if !u.matchesRequest(m, request.generation, request.epoch) {
		return nil
	}
	if err := u.awake(m, request.generation); err != nil {
		u.state.err = err
		return nil
	}
	return u.beginAction(m, request.generation, "mute",
		func(ctx context.Context, backend playerControlsUIBackend) playerControlsActionResult {
			muted, err := backend.ToggleMute(ctx)
			return playerControlsActionResult{muted: &muted, err: err}
		})
}

func (u *playerControlsUI) source(m *playerModel, request playerSourceRequest) tea.Cmd {
	if !u.matchesRequest(m, request.generation, request.epoch) {
		return nil
	}
	source, err := parseSource(string(request.source))
	if err != nil {
		u.state.err = fmt.Errorf("invalid source: %w", err)
		return nil
	}
	if !u.SupportsSource(source) {
		u.state.err = fmt.Errorf("source %s is unsupported or unverified for this speaker model", source)
		return nil
	}
	return u.beginAction(m, request.generation, "source",
		func(ctx context.Context, backend playerControlsUIBackend) playerControlsActionResult {
			snapshot, err := backend.SetSource(ctx, source)
			return playerControlsActionResult{snapshot: &snapshot, err: err}
		})
}

func (u *playerControlsUI) seek(m *playerModel, request playerSeekRequest) tea.Cmd {
	if !u.matchesRequest(m, request.generation, request.epoch) {
		return nil
	}
	if err := u.ready(m, request.generation); err != nil {
		u.state.err = err
		return nil
	}
	if err := playerControlUnavailable(*u.state.confirmed, playerControlSeekForward); err != nil {
		u.state.err = err
		return nil
	}
	return u.beginAction(m, request.generation, "seek",
		func(ctx context.Context, backend playerControlsUIBackend) playerControlsActionResult {
			snapshot, err := backend.SeekTo(ctx, request.positionMS)
			return playerControlsActionResult{snapshot: &snapshot, err: err}
		})
}

func (u *playerControlsUI) actionFinished(result playerControlsActionResult) tea.Cmd {
	if u.active == nil || result.sessionID != u.active.id || result.actionID != u.activeAction {
		return nil
	}
	u.state.pending, u.state.pendingAction = false, ""
	u.state.err = result.err
	u.epoch++
	if result.err == nil {
		if result.snapshot != nil {
			u.acceptSnapshot(*result.snapshot, result.volumeRevision)
		}
		if result.muted != nil {
			u.state.muted, u.state.mutedKnown = *result.muted, true
			if u.state.confirmed != nil {
				u.state.confirmed.Data.Muted = *result.muted
			}
		}
	}
	u.refreshWanted = true
	return u.refresh()
}

func (u *playerControlsUI) refresh() tea.Cmd {
	if !u.refreshWanted || u.refreshing || u.active == nil || !u.state.ready ||
		u.state.pending || u.active.volume.State().Pending {
		return nil
	}
	session := u.active
	epoch, revision := u.epoch, session.volume.State().Revision
	u.refreshWanted, u.refreshing = false, true
	result := make(chan playerControlsRefreshResult, 1)
	done := make(chan struct{})
	session.workers = append(session.workers, done)
	go func() {
		defer close(done)
		snapshot, err := u.readStatus(session.ctx, session.target)
		result <- playerControlsRefreshResult{
			sessionID: session.id, epoch: epoch, volumeRevision: revision, snapshot: snapshot, err: err,
		}
	}()
	return playerScopedCmd(session.generation, func() tea.Msg { return <-result })
}

func (u *playerControlsUI) refreshFinished(result playerControlsRefreshResult) tea.Cmd {
	if u.active == nil || result.sessionID != u.active.id {
		return nil
	}
	u.refreshing = false
	if result.epoch != u.epoch || u.state.pending || u.active.volume.State().Pending {
		u.refreshWanted = true
		return u.refresh()
	}
	if result.err != nil {
		u.suspend()
		u.state.err = fmt.Errorf("refreshing confirmed controls: %w", result.err)
		return nil
	}
	u.acceptSnapshot(result.snapshot, result.volumeRevision)
	return u.refresh()
}

func (u *playerControlsUI) volumeTarget(m *playerModel, request playerVolumeTargetRequest) tea.Cmd {
	if !u.matchesRequest(m, request.generation, request.epoch) {
		return nil
	}
	if err := u.awake(m, request.generation); err != nil {
		u.setVolumeInputError(err)
		return nil
	}
	if !u.state.volumeKnown {
		u.setVolumeInputError(errors.New("confirmed volume is unavailable"))
		return nil
	}
	target := max(0, min(100, request.target))
	if u.state.maximum != nil {
		target = min(target, *u.state.maximum)
	} else if target > u.state.volume {
		u.setVolumeInputError(errors.New("maximum volume is unavailable; increases are disabled"))
		return nil
	}
	var err error
	if request.release {
		err = u.active.volume.Release(target)
	} else {
		err = u.active.volume.SetTarget(target)
	}
	if err != nil {
		u.setVolumeInputError(err)
		return nil
	}
	u.volumeInputErr = nil
	u.volumeIntent, u.desiredVolume = true, target
	u.modified = true
	u.epoch++
	u.syncVolume(u.active.volume.State())
	return nil
}

func (u *playerControlsUI) volumeSlider(m *playerModel, request playerVolumeSliderRequest) tea.Cmd {
	if !u.matchesRequest(m, request.generation, request.epoch) {
		return nil
	}
	if !u.state.volumeKnown {
		u.setVolumeInputError(errors.New("confirmed volume is unavailable"))
		return nil
	}
	maximum := u.state.volume
	if u.state.maximum != nil {
		maximum = *u.state.maximum
	}
	target, err := playerVolumeAtPosition(request.x, request.width, maximum)
	if err != nil {
		u.setVolumeInputError(err)
		return nil
	}
	return u.volumeTarget(m, playerVolumeTargetRequest{
		generation: request.generation, epoch: request.epoch, target: target, release: request.release,
	})
}

func (u *playerControlsUI) volumeKey(m *playerModel, request playerVolumeKeyRequest) tea.Cmd {
	if !u.matchesRequest(m, request.generation, request.epoch) {
		return nil
	}
	if !u.state.volumeKnown {
		u.setVolumeInputError(errors.New("confirmed volume is unavailable"))
		return nil
	}
	current := u.state.volume
	if u.state.volumePending {
		current = u.state.sliderTarget
	}
	target, err := playerVolumeFromKey(current, u.state.maximum, request.key)
	if err != nil {
		u.setVolumeInputError(err)
		return nil
	}
	if target == current {
		return nil
	}
	return u.volumeTarget(m, playerVolumeTargetRequest{
		generation: request.generation, epoch: request.epoch, target: target,
	})
}

func (u *playerControlsUI) volumeRetry(m *playerModel, request playerVolumeRetryRequest) {
	if !u.matchesRequest(m, request.generation, request.epoch) {
		return
	}
	if err := u.awake(m, request.generation); err != nil {
		u.setVolumeInputError(err)
		return
	}
	volume := u.active.volume.State()
	if u.state.maximum == nil && volume.RetryTarget > u.state.volume {
		u.setVolumeInputError(errors.New("maximum volume is unavailable; increases are disabled"))
		return
	}
	if err := u.active.volume.Retry(); err != nil {
		u.setVolumeInputError(err)
		return
	}
	u.volumeInputErr = nil
	u.volumeIntent, u.desiredVolume = true, volume.RetryTarget
	u.modified = true
	u.epoch++
	u.syncVolume(u.active.volume.State())
}

func (u *playerControlsUI) volumeDismiss(m *playerModel, request playerVolumeDismissRequest) {
	if !u.matchesRequest(m, request.generation, request.epoch) {
		return
	}
	if u.active == nil {
		u.volumeInputErr, u.state.volumeErr = nil, nil
		u.state.volumeUncertain, u.state.volumeCanRetry = false, false
		u.volumeIntent = false
		return
	}
	if err := u.active.volume.DismissError(); err != nil {
		u.setVolumeInputError(err)
		return
	}
	u.volumeInputErr = nil
	u.volumeIntent = false
	u.syncVolume(u.active.volume.State())
}

func (u *playerControlsUI) setVolumeInputError(err error) {
	u.volumeInputErr, u.state.volumeErr = err, err
}
