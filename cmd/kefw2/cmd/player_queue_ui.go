// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/hilli/go-kef-w2/kefw2"
)

type playerQueueLoadMsg struct{ Generation uint64 }

type playerQueuePlayMsg struct {
	Generation uint64
	Snapshot   *playerQueueSnapshot
	Index      int
}

type playerQueueMoveMsg struct {
	Generation uint64
	Snapshot   *playerQueueSnapshot
	From, To   int
}

type playerQueueRemoveMsg struct {
	Generation uint64
	Snapshot   *playerQueueSnapshot
	Index      int
}

type playerQueueClearMsg struct {
	Generation uint64
	Snapshot   *playerQueueSnapshot
	Confirmed  bool
}

type playerQueueShuffleMsg struct {
	Generation uint64
	ReadyEpoch uint64
	Enabled    bool
}

type playerQueueRepeatMsg struct {
	Generation uint64
	ReadyEpoch uint64
	Mode       string
}

type playerQueueAcknowledgeMsg struct {
	Generation uint64
	ReviewID   uint64
}

// Snapshot is an immutable token for the exact displayed rows. Pass it and
// Generation back with index-based requests; Items itself is a presentation copy.
type playerQueueUIState struct {
	Generation   uint64
	Target       playerSpeaker
	Snapshot     *playerQueueSnapshot
	Items        []kefw2.ContentItem
	Mode         playerQueueMode
	ModeKnown    bool
	Ready        bool   // Fresh status for this generation; queue writes require it.
	ReadyEpoch   uint64 // Capture in shuffle and repeat requests.
	Busy         bool
	Loading      bool
	Err          error
	ModeErr      error
	Uncertain    bool
	ReviewReady  bool
	ReviewID     uint64
	PlayingIndex int
}

type playerQueueUIClientFactory func(context.Context, playerSpeaker) playerQueueClient

// The queue controller keeps its snapshot identity while the exclusively owned
// underlying client is rebound between serialized operations.
type playerQueueUIBoundClient struct {
	playerQueueClient
}

func (c *playerQueueUIBoundClient) bind(client playerQueueClient) error {
	if client == nil || (reflect.ValueOf(client).Kind() == reflect.Pointer && reflect.ValueOf(client).IsNil()) {
		return errors.New("queue client is nil")
	}
	c.playerQueueClient = client
	return nil
}

type playerQueueUIOperation struct {
	mu       sync.Mutex
	cancel   context.CancelCauseFunc
	finished bool
	offline  bool
}

func (op *playerQueueUIOperation) cancelUnavailable() {
	op.mu.Lock()
	defer op.mu.Unlock()
	if !op.finished {
		op.offline = true
		op.cancel(errPlayerQueueStatusUnavailable)
	}
}

func (op *playerQueueUIOperation) complete(outcome playerQueueUIOutcome) playerQueueUIOutcome {
	op.mu.Lock()
	offline := op.offline
	op.finished = true
	op.mu.Unlock()
	if offline {
		if outcome.result.Sent && !errors.Is(outcome.err, errPlayerQueueUncertain) {
			outcome.err = errors.Join(errPlayerQueueUncertain, outcome.err)
		}
		outcome.err = errors.Join(errPlayerQueueStatusUnavailable, outcome.err)
	}
	return outcome
}

type playerQueueUIWorker struct {
	ctx        context.Context
	cancel     context.CancelFunc
	target     playerSpeaker
	generation uint64
	mutations  *playerMutationGate
	queue      *playerQueue
	client     *playerQueueUIBoundClient
	makeClient playerQueueUIClientFactory
	jobs       []<-chan struct{}
}

type playerQueueUI struct {
	current        playerQueueUIState
	active         *playerQueueUIWorker
	workers        []*playerQueueUIWorker
	makeClient     playerQueueUIClientFactory
	nextRequestID  uint64
	nextReadyEpoch uint64
	inFlightID     uint64
	inFlightWrite  *playerQueueUIOperation
	reviewMode     bool
	refreshPending bool
	playInFlight   bool
	playStatusSeq  uint64
	playingPath    string
	playingTitle   string
}

var (
	errPlayerQueueStatusUnavailable = errors.New("speaker status unavailable or stale; wait for fresh status and retry")
	errPlayerQueueStaleStatus       = errors.New("speaker status changed; review and retry the queue action")
)

func newPlayerQueueUI() *playerQueueUI {
	return &playerQueueUI{
		current: playerQueueUIState{PlayingIndex: -1},
		makeClient: func(ctx context.Context, target playerSpeaker) playerQueueClient {
			return kefw2.NewAirableClient(target.newSpeaker(), kefw2.WithContext(ctx))
		},
	}
}

func (u *playerQueueUI) extension() playerExtension {
	return playerExtension{update: u.update, reset: u.reset, stop: u.stop}
}

func (u *playerQueueUI) state() playerQueueUIState {
	state := u.current
	if state.Snapshot != nil {
		state.Items = state.Snapshot.items()
	}
	return state
}

const (
	playerQueueUILoad        = "load queue"
	playerQueueUIPlay        = "play queue item"
	playerQueueUIMove        = "move queue item"
	playerQueueUIRemove      = "remove queue item"
	playerQueueUIClear       = "clear queue"
	playerQueueUIShuffle     = "set shuffle"
	playerQueueUIRepeat      = "set repeat"
	playerQueueUIAcknowledge = "acknowledge queue uncertainty"
)

type playerQueueUIOutcome struct {
	result  playerQueueResult
	err     error
	modeErr error
}

type playerQueueUIResultMsg struct {
	generation uint64
	requestID  uint64
	action     string
	outcome    playerQueueUIOutcome
}

func (u *playerQueueUI) update(m *playerModel, message tea.Msg) (tea.Cmd, bool) {
	switch msg := message.(type) {
	case playerSessionStartedMsg:
		return u.start(m, msg), false
	case playerSessionStoppedMsg:
		u.sessionStopped(msg)
		return nil, false
	case playerSessionFailure:
		u.statusUnavailable()
		return nil, false
	case watchUpdate:
		u.watch(m, msg)
		return nil, false
	case playerStatusRefreshCompletedMsg:
		u.statusRefreshCompleted(m, msg)
		return nil, false
	case playerDiscoveryQueueChangedMsg:
		return u.discoveryChanged(msg), true
	case playerQueueUIResultMsg:
		return u.complete(m, msg), true
	default:
		return u.request(m, message)
	}
}

func (u *playerQueueUI) watch(m *playerModel, msg watchUpdate) {
	if u.active == nil || u.active.generation != m.generation {
		return
	}
	switch msg.kind {
	case watchSnapshotUpdate:
		if msg.err != nil || m.session == nil || u.active.ctx.Err() != nil ||
			m.connection != playerReady || m.readErr != nil || m.snapshot == nil {
			u.statusUnavailable()
			return
		}
		if !u.current.Ready {
			u.nextReadyEpoch++
			if u.nextReadyEpoch == 0 {
				u.nextReadyEpoch++
			}
			u.current.ReadyEpoch = u.nextReadyEpoch
			if u.current.Snapshot != nil {
				snapshot := *u.current.Snapshot
				u.current.Snapshot = &snapshot
			}
		}
		u.current.Ready = true
		if !u.playInFlight && u.playStatusSeq == 0 {
			u.capturePlayback(m.snapshot)
		}
	case playerStatusUnavailableUpdate:
		u.statusUnavailable()
	case playerEventConnectionUpdate:
		if msg.err != nil {
			u.statusUnavailable()
		}
	}
}

func (u *playerQueueUI) start(m *playerModel, msg playerSessionStartedMsg) tea.Cmd {
	if m.session == nil || m.target == nil || m.generation != msg.generation || *m.target != msg.target {
		return nil
	}
	u.reset(m)
	ctx, cancel := context.WithCancel(m.session.context())
	client := &playerQueueUIBoundClient{}
	if err := client.bind(u.makeClient(ctx, msg.target)); err != nil {
		cancel()
		u.current = playerQueueUIState{Generation: msg.generation, Target: msg.target, PlayingIndex: -1, Err: err}
		return nil
	}
	queue, err := newPlayerQueue(ctx, client)
	u.current = playerQueueUIState{Generation: msg.generation, Target: msg.target, PlayingIndex: -1}
	if err != nil {
		cancel()
		u.current.Err = err
		return nil
	}
	worker := &playerQueueUIWorker{
		ctx: ctx, cancel: cancel, target: msg.target, generation: msg.generation,
		mutations: m.mutations, queue: queue, client: client, makeClient: u.makeClient,
	}
	u.active = worker
	u.workers = append(u.workers, worker)
	return u.load(worker)
}

func (u *playerQueueUI) sessionStopped(msg playerSessionStoppedMsg) {
	if u.active == nil || u.active.generation != msg.generation {
		return
	}
	u.reset(nil)
	u.current.Generation = msg.generation
	u.current.Target = msg.target
	if msg.err == nil {
		u.current.Err = errors.New("queue session stopped")
	} else {
		u.current.Err = fmt.Errorf("queue session stopped: %w", msg.err)
	}
}

func (u *playerQueueUI) reset(_ *playerModel) {
	if u.active != nil {
		u.active.cancel()
		u.active = nil
	}
	u.current = playerQueueUIState{PlayingIndex: -1}
	u.inFlightID = 0
	u.inFlightWrite = nil
	u.reviewMode = false
	u.refreshPending = false
	u.playInFlight = false
	u.playStatusSeq = 0
	u.playingPath = ""
	u.playingTitle = ""
	u.pruneWorkers()
}

func (u *playerQueueUI) statusUnavailable() {
	u.current.Ready = false
	u.capturePlayback(nil)
	if u.inFlightWrite != nil {
		u.inFlightWrite.cancelUnavailable()
	}
}

func (u *playerQueueUI) statusRefreshCompleted(m *playerModel, msg playerStatusRefreshCompletedMsg) {
	if u.active == nil || u.active.generation != msg.generation || m.generation != msg.generation ||
		m.session == nil || u.active.ctx.Err() != nil || u.playInFlight ||
		u.playStatusSeq == 0 || msg.sequence < u.playStatusSeq {
		return
	}
	u.playStatusSeq = 0
	if msg.err == nil && u.current.Ready && m.connection == playerReady && m.readErr == nil && m.snapshot != nil {
		u.capturePlayback(m.snapshot)
	}
}

func (u *playerQueueUI) discoveryChanged(msg playerDiscoveryQueueChangedMsg) tea.Cmd {
	if u.active == nil || u.active.generation != msg.generation {
		return nil
	}
	if u.current.Busy {
		u.refreshPending = true
		return nil
	}
	return u.load(u.active)
}

func (u *playerQueueUI) stop(_ *playerModel) []<-chan struct{} {
	u.reset(nil)
	var done []<-chan struct{}
	for _, worker := range u.workers {
		worker.cancel()
		done = append(done, worker.jobs...)
	}
	return done
}

func (u *playerQueueUI) pruneWorkers() {
	workers := u.workers[:0]
	for _, worker := range u.workers {
		jobs := worker.jobs[:0]
		for _, done := range worker.jobs {
			select {
			case <-done:
			default:
				jobs = append(jobs, done)
			}
		}
		worker.jobs = jobs
		if worker == u.active || len(jobs) > 0 {
			workers = append(workers, worker)
		}
	}
	u.workers = workers
}

func (u *playerQueueUI) capturePlayback(snapshot *statusSnapshot) {
	u.playingPath, u.playingTitle = "", ""
	if snapshot != nil &&
		(snapshot.Player.State == kefw2.PlayerStatePlaying || snapshot.Player.State == kefw2.PlayerStatePaused) &&
		strings.HasPrefix(snapshot.Player.TrackRoles.Path, "playlists:item/") {
		u.playingPath = snapshot.Player.TrackRoles.Path
		u.playingTitle = snapshot.Player.TrackRoles.Title
	}
	u.updatePlayingIndex()
}

func (u *playerQueueUI) updatePlayingIndex() {
	u.current.PlayingIndex = -1
	if u.playingPath == "" || u.current.Snapshot == nil {
		return
	}
	for i, row := range u.current.Snapshot.rows {
		if row.Path != u.playingPath {
			continue
		}
		if u.current.PlayingIndex >= 0 {
			u.current.PlayingIndex = -1
			return
		}
		u.current.PlayingIndex = i
	}
	if u.current.PlayingIndex >= 0 || u.playingTitle == "" {
		return
	}
	for i, row := range u.current.Snapshot.rows {
		if row.Title != u.playingTitle {
			continue
		}
		if u.current.PlayingIndex >= 0 {
			u.current.PlayingIndex = -1
			return
		}
		u.current.PlayingIndex = i
	}
}

func (u *playerQueueUI) authorize(
	m *playerModel, generation uint64, snapshot *playerQueueSnapshot, needsSnapshot, mutation bool,
) (*playerQueueUIWorker, bool) {
	worker := u.active
	if worker == nil || worker.generation != generation || m.generation != generation {
		return nil, false
	}
	if mutation && (!u.current.Ready || m.session == nil || worker.ctx.Err() != nil ||
		m.connection != playerReady || m.readErr != nil || m.snapshot == nil) {
		u.statusUnavailable()
		if u.current.Uncertain {
			if !errors.Is(u.current.Err, errPlayerQueueStatusUnavailable) {
				u.current.Err = errors.Join(u.current.Err, errPlayerQueueStatusUnavailable)
			}
		} else {
			u.current.Err = errPlayerQueueStatusUnavailable
		}
		return nil, false
	}
	if u.current.Busy {
		if !u.current.Uncertain {
			u.current.Err = errPlayerQueueBusy
		}
		return nil, false
	}
	if u.current.Uncertain && mutation {
		return nil, false
	}
	if needsSnapshot && (snapshot == nil || snapshot != u.current.Snapshot) {
		u.current.Err = errPlayerQueueStale
		return nil, false
	}
	return worker, true
}

func (u *playerQueueUI) authorizeMode(m *playerModel, generation, epoch uint64) (*playerQueueUIWorker, bool) {
	worker, ok := u.authorize(m, generation, nil, false, true)
	if !ok {
		return nil, false
	}
	if epoch == 0 || epoch != u.current.ReadyEpoch {
		u.current.Err = errPlayerQueueStaleStatus
		return nil, false
	}
	return worker, true
}

func (u *playerQueueUI) request(m *playerModel, message tea.Msg) (tea.Cmd, bool) {
	switch msg := message.(type) {
	case playerQueueLoadMsg:
		worker, ok := u.authorize(m, msg.Generation, nil, false, false)
		if !ok {
			return nil, true
		}
		return u.load(worker), true
	case playerQueuePlayMsg:
		worker, ok := u.authorize(m, msg.Generation, msg.Snapshot, true, true)
		if !ok {
			return nil, true
		}
		command := u.launch(worker, playerQueueUIPlay, func(ctx context.Context, queue *playerQueue) playerQueueUIOutcome {
			result, err := queue.Play(ctx, msg.Snapshot, msg.Index)
			return playerQueueUIOutcome{result: result, err: err}
		})
		if command != nil {
			u.playInFlight = true
			u.playStatusSeq = 0
			u.capturePlayback(nil)
		}
		return command, true
	case playerQueueMoveMsg:
		worker, ok := u.authorize(m, msg.Generation, msg.Snapshot, true, true)
		if !ok {
			return nil, true
		}
		return u.launch(worker, playerQueueUIMove, func(ctx context.Context, queue *playerQueue) playerQueueUIOutcome {
			result, err := queue.Move(ctx, msg.Snapshot, msg.From, msg.To)
			return playerQueueUIOutcome{result: result, err: err}
		}), true
	case playerQueueRemoveMsg:
		worker, ok := u.authorize(m, msg.Generation, msg.Snapshot, true, true)
		if !ok {
			return nil, true
		}
		return u.launch(worker, playerQueueUIRemove, func(ctx context.Context, queue *playerQueue) playerQueueUIOutcome {
			result, err := queue.Remove(ctx, msg.Snapshot, msg.Index)
			return playerQueueUIOutcome{result: result, err: err}
		}), true
	case playerQueueClearMsg:
		worker, ok := u.authorize(m, msg.Generation, msg.Snapshot, true, true)
		if !ok {
			return nil, true
		}
		return u.launch(worker, playerQueueUIClear, func(ctx context.Context, queue *playerQueue) playerQueueUIOutcome {
			result, err := queue.Clear(ctx, msg.Snapshot, msg.Confirmed)
			return playerQueueUIOutcome{result: result, err: err}
		}), true
	case playerQueueShuffleMsg:
		worker, ok := u.authorizeMode(m, msg.Generation, msg.ReadyEpoch)
		if !ok {
			return nil, true
		}
		return u.launch(worker, playerQueueUIShuffle, func(ctx context.Context, queue *playerQueue) playerQueueUIOutcome {
			result, err := queue.SetShuffle(ctx, msg.Enabled)
			return playerQueueUIOutcome{result: result, err: err}
		}), true
	case playerQueueRepeatMsg:
		worker, ok := u.authorizeMode(m, msg.Generation, msg.ReadyEpoch)
		if !ok {
			return nil, true
		}
		return u.launch(worker, playerQueueUIRepeat, func(ctx context.Context, queue *playerQueue) playerQueueUIOutcome {
			result, err := queue.SetRepeat(ctx, msg.Mode)
			return playerQueueUIOutcome{result: result, err: err}
		}), true
	case playerQueueAcknowledgeMsg:
		return u.acknowledge(m, msg), true
	default:
		return nil, false
	}
}

func (u *playerQueueUI) acknowledge(m *playerModel, msg playerQueueAcknowledgeMsg) tea.Cmd {
	worker, ok := u.authorize(m, msg.Generation, nil, false, false)
	if !ok || !u.current.Uncertain {
		return nil
	}
	if msg.ReviewID != u.current.ReviewID || !u.current.ReviewReady {
		u.current.Err = errors.Join(u.current.Err, errPlayerQueueReview)
		return nil
	}
	return u.launch(worker, playerQueueUIAcknowledge, func(ctx context.Context, queue *playerQueue) playerQueueUIOutcome {
		return playerQueueUIOutcome{err: queue.Acknowledge(ctx)}
	})
}

func (u *playerQueueUI) load(worker *playerQueueUIWorker) tea.Cmd {
	return u.launch(worker, playerQueueUILoad, func(ctx context.Context, queue *playerQueue) playerQueueUIOutcome {
		snapshot, queueErr := queue.Read(ctx)
		mode, modeErr := queue.ReadMode(ctx)
		result := playerQueueResult{Snapshot: snapshot}
		if modeErr == nil {
			result.Mode = &mode
		}
		return playerQueueUIOutcome{result: result, err: queueErr, modeErr: modeErr}
	})
}

func playerQueueUIMutation(action string) bool {
	switch action {
	case playerQueueUIPlay, playerQueueUIMove, playerQueueUIRemove,
		playerQueueUIClear, playerQueueUIShuffle, playerQueueUIRepeat:
		return true
	default:
		return false
	}
}

// Each operation owns a client bound to its context. Disconnect cancels the
// current write; resetting the session cancels all operations.
func (u *playerQueueUI) launch(
	worker *playerQueueUIWorker,
	action string,
	run func(context.Context, *playerQueue) playerQueueUIOutcome,
) tea.Cmd {
	var ticket *playerMutationTicket
	mutation := playerQueueUIMutation(action)
	if mutation {
		if worker.mutations == nil {
			u.current.Err = errors.New("queue mutation gate unavailable")
			return nil
		}
		ticket = worker.mutations.reserve()
	}
	ctx, cancel := context.WithCancelCause(worker.ctx)
	operation := &playerQueueUIOperation{cancel: cancel}
	if mutation {
		u.inFlightWrite = operation
	}
	u.nextRequestID++
	if u.nextRequestID == 0 {
		u.nextRequestID++
	}
	requestID := u.nextRequestID
	u.inFlightID = requestID
	u.current.Busy = true
	u.current.Loading = action == playerQueueUILoad
	finished := make(chan struct{})
	outcomes := make(chan playerQueueUIOutcome, 1)
	worker.jobs = append(worker.jobs, finished)
	go func() {
		defer close(finished)
		defer cancel(nil)
		if ticket != nil {
			defer ticket.release()
			if err := ticket.wait(ctx); err != nil {
				outcomes <- operation.complete(playerQueueUIOutcome{err: err})
				return
			}
		}
		if err := worker.client.bind(worker.makeClient(ctx, worker.target)); err != nil {
			outcomes <- operation.complete(playerQueueUIOutcome{err: err})
			return
		}
		outcomes <- operation.complete(run(ctx, worker.queue))
	}()
	return func() tea.Msg {
		outcome := <-outcomes
		completed := playerQueueUIResultMsg{
			generation: worker.generation, requestID: requestID, action: action, outcome: outcome,
		}
		scoped := playerScopedCmd(worker.generation, func() tea.Msg { return completed })
		if outcome.result.Sent && errors.Is(outcome.err, errPlayerQueueUncertain) {
			notice := func() tea.Msg {
				return playerNoticeMsg{target: worker.target, action: action, err: outcome.err}
			}
			return tea.Batch(scoped, notice)()
		}
		return scoped()
	}
}

func (u *playerQueueUI) complete(m *playerModel, msg playerQueueUIResultMsg) tea.Cmd {
	if u.active == nil || msg.generation != u.active.generation || msg.requestID != u.inFlightID {
		return nil
	}
	u.current.Busy, u.current.Loading = false, false
	u.inFlightID = 0
	u.inFlightWrite = nil
	u.pruneWorkers()
	if msg.action == playerQueueUIPlay {
		u.playInFlight = false
		u.capturePlayback(nil)
		if msg.outcome.result.Sent {
			u.playStatusSeq = m.requestStatusRefresh(msg.generation)
		}
	}
	switch msg.action {
	case playerQueueUILoad:
		u.completeLoad(msg.outcome)
	case playerQueueUIAcknowledge:
		u.completeAcknowledgement(msg.outcome)
	default:
		u.completeMutation(msg)
	}
	u.updatePlayingIndex()
	if u.refreshPending {
		u.refreshPending = false
		return u.load(u.active)
	}
	return nil
}

func (u *playerQueueUI) completeLoad(outcome playerQueueUIOutcome) {
	u.current.Snapshot = outcome.result.Snapshot
	if outcome.err != nil {
		if u.current.Uncertain {
			u.current.Err = errors.Join(u.current.Err, outcome.err)
		} else {
			u.current.Err = outcome.err
		}
	} else if !u.current.Uncertain {
		u.current.Err = nil
	}
	u.current.ModeErr = outcome.modeErr
	u.current.ModeKnown = outcome.result.Mode != nil
	if outcome.result.Mode != nil {
		u.current.Mode = *outcome.result.Mode
	}
	if u.current.Uncertain {
		if u.reviewMode {
			u.current.ReviewReady = outcome.result.Mode != nil
		} else {
			u.current.ReviewReady = outcome.result.Snapshot != nil
		}
	}
}

func (u *playerQueueUI) completeAcknowledgement(outcome playerQueueUIOutcome) {
	if outcome.err != nil {
		u.current.Err = errors.Join(u.current.Err, outcome.err)
		return
	}
	u.current.Uncertain = false
	u.current.ReviewReady = false
	u.current.ReviewID = 0
	u.current.Err = nil
	u.reviewMode = false
}

func (u *playerQueueUI) completeMutation(msg playerQueueUIResultMsg) {
	result, err := msg.outcome.result, msg.outcome.err
	if result.Snapshot != nil {
		u.current.Snapshot = result.Snapshot
	}
	if result.Mode != nil {
		u.current.Mode = *result.Mode
		u.current.ModeKnown = true
		u.current.ModeErr = nil
	}
	u.current.Err = err
	if !result.Sent || !errors.Is(err, errPlayerQueueUncertain) {
		return
	}
	u.current.Uncertain = true
	u.current.ReviewID = msg.requestID
	u.reviewMode = msg.action == playerQueueUIShuffle || msg.action == playerQueueUIRepeat
	if u.reviewMode {
		u.current.ReviewReady = result.Mode != nil
		if result.Mode == nil {
			u.current.ModeKnown = false
		}
	} else {
		u.current.ReviewReady = result.Snapshot != nil
		if result.Snapshot == nil {
			u.current.Snapshot = nil
		}
	}
}
