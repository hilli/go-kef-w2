// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hilli/go-kef-w2/kefw2"
)

type playerQueueUIHarness struct {
	model   *playerModel
	ui      *playerQueueUI
	factory *fakePlayerFactory
	notices []playerNoticeMsg
}

func newPlayerQueueUIHarness(t *testing.T, clients map[string]*playerQueueFake) *playerQueueUIHarness {
	t.Helper()
	factory := &fakePlayerFactory{autoFinish: true}
	model := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, factory.newSession)
	ui := newPlayerQueueUI()
	if clients != nil {
		ui.makeClient = func(_ context.Context, target playerSpeaker) playerQueueClient {
			return clients[target.IPAddress]
		}
	}
	harness := &playerQueueUIHarness{model: model, ui: ui, factory: factory}
	model.addExtension(ui.extension())
	model.addExtension(playerExtension{update: func(_ *playerModel, msg tea.Msg) (tea.Cmd, bool) {
		if notice, ok := msg.(playerNoticeMsg); ok {
			harness.notices = append(harness.notices, notice)
			return nil, true
		}
		return nil, false
	}})
	t.Cleanup(func() {
		if err := model.shutdown(); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
	return harness
}

func (h *playerQueueUIHarness) start(target playerSpeaker, generation uint64) tea.Cmd {
	command := h.startConnecting(target, generation)
	h.ready(generation)
	return command
}

func (h *playerQueueUIHarness) startConnecting(target playerSpeaker, generation uint64) tea.Cmd {
	if h.model.session != nil {
		h.model.session.cancel()
	}
	h.model.generation = generation
	h.model.target = &target
	h.model.snapshot = nil
	h.model.connection = playerConnecting
	h.model.readErr = nil
	h.ui.reset(h.model)
	h.model.session = h.factory.newSession(h.model.ctx, target, generation)
	h.model.sessions[generation] = h.model.session
	h.model.mutations = newPlayerMutationGate()
	command, _ := h.ui.update(h.model, playerSessionStartedMsg{generation: generation, target: target})
	return command
}

func (h *playerQueueUIHarness) ready(generation uint64) {
	h.dispatch(playerScopedMsg{generation: generation, message: watchUpdate{kind: watchSnapshotUpdate}})
}

func (h *playerQueueUIHarness) offline() {
	h.dispatch(playerScopedMsg{
		generation: h.model.generation,
		message:    watchUpdate{kind: playerStatusUnavailableUpdate, err: errors.New("offline")},
	})
}

func (h *playerQueueUIHarness) dispatch(msg tea.Msg) tea.Cmd {
	_, command := h.model.Update(msg)
	return command
}

func (h *playerQueueUIHarness) deliver(t *testing.T, command tea.Cmd) {
	t.Helper()
	if command == nil {
		t.Fatal("expected a queue command")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- command() }()
	var msg tea.Msg
	select {
	case msg = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for queue command")
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, next := range batch {
			h.deliver(t, next)
		}
		return
	}
	if msg != nil {
		if next := h.dispatch(msg); next != nil {
			h.deliver(t, next)
		}
	}
}

func playerQueueUIWait(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestPlayerQueueUIInitialLoadDoesNotWrite(t *testing.T) {
	duplicate := playerQueueTestItem("Same", "same")
	fake := newPlayerQueueFake(duplicate, playerQueueTestItem("Middle", "middle"), duplicate)
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	command := h.start(playerSpeaker{Name: "Living", IPAddress: "a"}, 1)
	if state := h.ui.state(); !state.Loading || !state.Busy || state.Snapshot != nil {
		t.Fatalf("initial loading state = %+v", state)
	}
	h.deliver(t, command)
	state := h.ui.state()
	if state.Busy || state.Loading || state.Err != nil || !state.ModeKnown || len(state.Items) != 3 {
		t.Fatalf("loaded state = %+v", state)
	}
	if len(fake.moves) != 0 || len(fake.plays) != 0 || len(fake.removes) != 0 || fake.clears != 0 ||
		len(fake.modes) != 0 {
		t.Fatal("loading the queue performed a write")
	}
	state.Items[0].Title = "presentation only"
	if got := h.ui.state().Items[0].Title; got != "Same" {
		t.Fatalf("presentation changed the snapshot: %q", got)
	}
}

func TestPlayerQueueUIRequiresFreshStatusForMutations(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.startConnecting(playerSpeaker{IPAddress: "a"}, 1))
	state := h.ui.state()
	if state.Ready || state.Snapshot == nil {
		t.Fatalf("connecting queue state = %+v", state)
	}
	for _, tc := range []struct {
		name string
		msg  tea.Msg
	}{
		{"play", playerQueuePlayMsg{Generation: 1, Snapshot: state.Snapshot, Index: 0}},
		{"move", playerQueueMoveMsg{Generation: 1, Snapshot: state.Snapshot, From: 0, To: 1}},
		{"remove", playerQueueRemoveMsg{Generation: 1, Snapshot: state.Snapshot, Index: 0}},
		{"clear", playerQueueClearMsg{Generation: 1, Snapshot: state.Snapshot, Confirmed: true}},
		{"shuffle", playerQueueShuffleMsg{Generation: 1, ReadyEpoch: state.ReadyEpoch, Enabled: true}},
		{"repeat", playerQueueRepeatMsg{Generation: 1, ReadyEpoch: state.ReadyEpoch, Mode: "all"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if command := h.dispatch(tc.msg); command != nil ||
				!errors.Is(h.ui.state().Err, errPlayerQueueStatusUnavailable) {
				t.Fatalf("mutation while connecting: command=%v state=%+v", command != nil, h.ui.state())
			}
		})
	}
	h.deliver(t, h.dispatch(playerQueueLoadMsg{Generation: 1}))
	h.ready(0)
	if h.ui.state().Ready || len(fake.moves) != 0 || len(fake.plays) != 0 ||
		len(fake.removes) != 0 || fake.clears != 0 || len(fake.modes) != 0 {
		t.Fatal("read or stale-generation status enabled or replayed a write")
	}
	beforeStatus := h.ui.state()
	h.ready(1)
	if !h.ui.state().Ready {
		t.Fatal("fresh current-generation status did not enable the queue")
	}
	if command := h.dispatch(playerQueuePlayMsg{
		Generation: 1, Snapshot: beforeStatus.Snapshot, Index: 0,
	}); command != nil || !errors.Is(h.ui.state().Err, errPlayerQueueStale) {
		t.Fatal("pre-status queue action survived readiness")
	}
}

func TestPlayerQueueUIRejectsStaleStatusAndSessionFailure(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
	h.model.readErr = errors.New("stale status")
	state := h.ui.state()
	if command := h.dispatch(playerQueueMoveMsg{
		Generation: 1, Snapshot: state.Snapshot, From: 0, To: 1,
	}); command != nil || h.ui.state().Ready || !errors.Is(h.ui.state().Err, errPlayerQueueStatusUnavailable) {
		t.Fatalf("stale status allowed a mutation: %+v", h.ui.state())
	}
	h.model.readErr = nil
	if command := h.dispatch(playerQueueShuffleMsg{
		Generation: 1, ReadyEpoch: h.ui.state().ReadyEpoch, Enabled: true,
	}); command != nil {
		t.Fatal("clearing an error without a fresh snapshot re-enabled writes")
	}
	h.ready(1)
	if len(fake.moves) != 0 || len(fake.modes) != 0 {
		t.Fatal("reconnection replayed a blocked request")
	}
	state = h.ui.state()
	h.deliver(t, h.dispatch(playerQueueMoveMsg{Generation: 1, Snapshot: state.Snapshot, From: 0, To: 1}))
	if len(fake.moves) != 1 || h.ui.state().Err != nil {
		t.Fatalf("explicit move after fresh status: state=%+v moves=%v", h.ui.state(), fake.moves)
	}
	h.dispatch(playerScopedMsg{generation: 1, message: playerSessionFailure{err: errors.New("watch stopped")}})
	if command := h.dispatch(playerQueueShuffleMsg{
		Generation: 1, ReadyEpoch: h.ui.state().ReadyEpoch, Enabled: true,
	}); command != nil ||
		h.ui.state().Ready || !errors.Is(h.ui.state().Err, errPlayerQueueStatusUnavailable) {
		t.Fatal("session failure allowed a queue write")
	}
	h.ready(1)
	h.deliver(t, h.dispatch(playerQueueShuffleMsg{Generation: 1, ReadyEpoch: h.ui.state().ReadyEpoch, Enabled: true}))
	if len(fake.modes) != 1 || h.ui.state().Err != nil {
		t.Fatalf("explicit mode change after recovery: state=%+v modes=%v", h.ui.state(), fake.modes)
	}
}

func TestPlayerQueueUIEventDisconnectRequiresFreshStatus(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
	h.dispatch(playerScopedMsg{generation: 1, message: watchUpdate{
		kind: playerEventConnectionUpdate, err: errors.New("event stream disconnected"),
	}})
	h.dispatch(playerScopedMsg{generation: 1, message: watchUpdate{kind: playerEventConnectionUpdate}})
	h.dispatch(playerScopedMsg{generation: 1, message: watchUpdate{
		kind: watchSnapshotUpdate, err: errors.New("snapshot failed"),
	}})
	state := h.ui.state()
	if state.Ready {
		t.Fatal("event reconnection or a failed snapshot re-enabled queue writes")
	}
	if command := h.dispatch(playerQueueMoveMsg{
		Generation: 1, Snapshot: state.Snapshot, From: 0, To: 1,
	}); command != nil || !errors.Is(h.ui.state().Err, errPlayerQueueStatusUnavailable) {
		t.Fatal("event disconnect allowed a queue write")
	}
	h.ready(1)
	if len(fake.moves) != 0 {
		t.Fatal("fresh status replayed a rejected write")
	}
	state = h.ui.state()
	h.deliver(t, h.dispatch(playerQueueMoveMsg{Generation: 1, Snapshot: state.Snapshot, From: 0, To: 1}))
	if len(fake.moves) != 1 || h.ui.state().Err != nil {
		t.Fatalf("explicit move after fresh status: state=%+v moves=%v", h.ui.state(), fake.moves)
	}
}

func TestPlayerQueueUIDisconnectDropsQueuedMutation(t *testing.T) {
	for _, cause := range []string{"status", "event stream"} {
		t.Run(cause, func(t *testing.T) {
			fake := newPlayerQueueFake(playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
			h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
			h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
			held := h.model.mutations.reserve()
			if err := held.wait(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer held.release()
			state := h.ui.state()
			command := h.dispatch(playerQueueMoveMsg{Generation: 1, Snapshot: state.Snapshot, From: 0, To: 1})
			if cause == "status" {
				h.offline()
			} else {
				h.dispatch(playerScopedMsg{generation: 1, message: watchUpdate{
					kind: playerEventConnectionUpdate, err: errors.New("event stream disconnected"),
				}})
			}
			if h.ui.state().Ready {
				t.Fatal("disconnect left queue writes enabled")
			}
			h.ready(1)
			held.release()
			h.deliver(t, command)
			state = h.ui.state()
			if !state.Ready || state.Busy || state.Uncertain ||
				!errors.Is(state.Err, errPlayerQueueStatusUnavailable) ||
				fake.queueReads != 1 || len(fake.moves) != 0 || len(h.notices) != 0 {
				t.Fatalf("dropped queued mutation: state=%+v reads=%d moves=%v notices=%v",
					state, fake.queueReads, fake.moves, h.notices)
			}
			h.deliver(t, h.dispatch(playerQueueMoveMsg{Generation: 1, Snapshot: state.Snapshot, From: 0, To: 1}))
			if len(fake.moves) != 1 {
				t.Fatalf("fresh explicit action was not sent: %v", fake.moves)
			}
		})
	}
}

func TestPlayerQueueUIRejectsDelayedIndexesAfterRecovery(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
	old := h.ui.state()
	h.offline()
	h.ready(1)
	fresh := h.ui.state()
	if !fresh.Ready || fresh.Snapshot == old.Snapshot {
		t.Fatal("fresh status retained the pre-disconnect input token")
	}
	for _, msg := range []tea.Msg{
		playerQueuePlayMsg{Generation: 1, Snapshot: old.Snapshot, Index: 0},
		playerQueueMoveMsg{Generation: 1, Snapshot: old.Snapshot, From: 0, To: 1},
		playerQueueRemoveMsg{Generation: 1, Snapshot: old.Snapshot, Index: 0},
		playerQueueClearMsg{Generation: 1, Snapshot: old.Snapshot, Confirmed: true},
	} {
		if command := h.dispatch(msg); command != nil || !errors.Is(h.ui.state().Err, errPlayerQueueStale) {
			t.Fatalf("pre-disconnect action was not rejected: %T, state=%+v", msg, h.ui.state())
		}
	}
	if len(fake.moves) != 0 || len(fake.plays) != 0 || len(fake.removes) != 0 || fake.clears != 0 {
		t.Fatal("pre-disconnect action wrote to the speaker")
	}
	h.deliver(t, h.dispatch(playerQueueMoveMsg{Generation: 1, Snapshot: fresh.Snapshot, From: 0, To: 1}))
	if len(fake.moves) != 1 || h.ui.state().Err != nil {
		t.Fatalf("fresh explicit action failed: state=%+v moves=%v", h.ui.state(), fake.moves)
	}
}

func TestPlayerQueueUIRejectsDelayedModesAfterRecovery(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"))
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.startConnecting(playerSpeaker{IPAddress: "a"}, 1))
	beforeStatus := h.ui.state()
	h.ready(1)
	first := h.ui.state()
	if first.ReadyEpoch == 0 || first.ReadyEpoch == beforeStatus.ReadyEpoch {
		t.Fatal("first fresh status did not assign a readiness epoch")
	}
	if command := h.dispatch(playerQueueShuffleMsg{
		Generation: 1, ReadyEpoch: beforeStatus.ReadyEpoch, Enabled: true,
	}); command != nil || !errors.Is(h.ui.state().Err, errPlayerQueueStaleStatus) {
		t.Fatal("pre-status mode request was not rejected")
	}
	h.ready(1)
	if h.ui.state().ReadyEpoch != first.ReadyEpoch {
		t.Fatal("ordinary status polling changed the readiness epoch")
	}
	h.offline()
	h.ready(1)
	fresh := h.ui.state()
	if fresh.ReadyEpoch == first.ReadyEpoch {
		t.Fatal("recovery retained the pre-disconnect readiness epoch")
	}
	for _, msg := range []tea.Msg{
		playerQueueShuffleMsg{Generation: 1, ReadyEpoch: first.ReadyEpoch, Enabled: true},
		playerQueueRepeatMsg{Generation: 1, ReadyEpoch: first.ReadyEpoch, Mode: "all"},
	} {
		if command := h.dispatch(msg); command != nil || !errors.Is(h.ui.state().Err, errPlayerQueueStaleStatus) {
			t.Fatalf("pre-disconnect mode request was not rejected: %T, state=%+v", msg, h.ui.state())
		}
	}
	if len(fake.modes) != 0 {
		t.Fatalf("stale mode action wrote to the speaker: %v", fake.modes)
	}
	h.deliver(t, h.dispatch(playerQueueShuffleMsg{Generation: 1, ReadyEpoch: fresh.ReadyEpoch, Enabled: true}))
	h.deliver(t, h.dispatch(playerQueueRepeatMsg{Generation: 1, ReadyEpoch: fresh.ReadyEpoch, Mode: "all"}))
	if len(fake.modes) != 2 || h.ui.state().Err != nil {
		t.Fatalf("fresh mode actions failed: state=%+v writes=%v", h.ui.state(), fake.modes)
	}
}

func TestPlayerQueueUIDropsQueuedModeOnDisconnect(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"))
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
	held := h.model.mutations.reserve()
	if err := held.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer held.release()
	old := h.ui.state()
	command := h.dispatch(playerQueueShuffleMsg{Generation: 1, ReadyEpoch: old.ReadyEpoch, Enabled: true})
	h.offline()
	h.ready(1)
	held.release()
	h.deliver(t, command)
	if h.ui.state().Uncertain || !errors.Is(h.ui.state().Err, errPlayerQueueStatusUnavailable) ||
		len(fake.modes) != 0 || len(h.notices) != 0 {
		t.Fatalf("queued mode was not dropped: state=%+v writes=%v notices=%+v",
			h.ui.state(), fake.modes, h.notices)
	}
	fresh := h.ui.state()
	h.deliver(t, h.dispatch(playerQueueShuffleMsg{Generation: 1, ReadyEpoch: fresh.ReadyEpoch, Enabled: true}))
	if len(fake.modes) != 1 || h.ui.state().Err != nil {
		t.Fatalf("fresh mode action failed: state=%+v writes=%v", h.ui.state(), fake.modes)
	}
}

func TestPlayerQueueUIDisconnectInFlightRequiresReview(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce, writeOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	fake.afterWrite = func(string) {
		writeOnce.Do(func() {
			close(started)
			<-release
		})
	}
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	t.Cleanup(unblock)
	h.deliver(t, h.start(playerSpeaker{Name: "Living", IPAddress: "a"}, 1))
	state := h.ui.state()
	command := h.dispatch(playerQueueMoveMsg{Generation: 1, Snapshot: state.Snapshot, From: 0, To: 1})
	playerQueueUIWait(t, started, "dispatched queue mutation")
	h.offline()
	unblock()
	h.deliver(t, command)
	state = h.ui.state()
	if state.Ready || !state.Uncertain || state.ReviewReady || state.Snapshot != nil ||
		!errors.Is(state.Err, errPlayerQueueUncertain) ||
		!errors.Is(state.Err, errPlayerQueueStatusUnavailable) ||
		len(h.notices) != 1 || h.notices[0].target.IPAddress != "a" ||
		!errors.Is(h.notices[0].err, errPlayerQueueUncertain) {
		t.Fatalf("disconnected write: state=%+v notices=%+v", state, h.notices)
	}
	h.deliver(t, h.dispatch(playerQueueLoadMsg{Generation: 1}))
	state = h.ui.state()
	if !state.ReviewReady || !state.Uncertain || state.Ready {
		t.Fatalf("offline review read: %+v", state)
	}
	h.deliver(t, h.dispatch(playerQueueAcknowledgeMsg{Generation: 1, ReviewID: state.ReviewID}))
	state = h.ui.state()
	if state.Uncertain || state.Ready {
		t.Fatalf("offline acknowledgement: %+v", state)
	}
	if command := h.dispatch(playerQueueMoveMsg{
		Generation: 1, Snapshot: state.Snapshot, From: 1, To: 0,
	}); command != nil || !errors.Is(h.ui.state().Err, errPlayerQueueStatusUnavailable) {
		t.Fatal("acknowledgement re-enabled offline mutations")
	}
	h.ready(1)
	if len(fake.moves) != 1 {
		t.Fatalf("reconnection replayed uncertain write: %v", fake.moves)
	}
	state = h.ui.state()
	h.deliver(t, h.dispatch(playerQueueMoveMsg{Generation: 1, Snapshot: state.Snapshot, From: 1, To: 0}))
	if len(fake.moves) != 2 || h.ui.state().Err != nil {
		t.Fatalf("explicit move after review: state=%+v moves=%v", h.ui.state(), fake.moves)
	}
}

func TestPlayerQueueUIIndexedActions(t *testing.T) {
	duplicate := playerQueueTestItem("Same", "same")
	fake := newPlayerQueueFake(duplicate, playerQueueTestItem("Middle", "middle"), duplicate)
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{Name: "Living", IPAddress: "a"}, 1))
	state := h.ui.state()
	command := h.dispatch(playerQueuePlayMsg{Generation: state.Generation, Snapshot: state.Snapshot, Index: 2})
	if !h.ui.state().Busy {
		t.Fatal("play did not mark the worker busy")
	}
	h.deliver(t, command)
	if len(fake.plays) != 1 || fake.plays[0].index != 2 {
		t.Fatalf("play chose the wrong duplicate: %+v", fake.plays)
	}
	state = h.ui.state()
	command = h.dispatch(playerQueueMoveMsg{Generation: state.Generation, Snapshot: state.Snapshot, From: 2, To: 0})
	h.deliver(t, command)
	if !reflect.DeepEqual(fake.moves, [][2]int{{2, 0}}) || len(fake.plays) != 1 {
		t.Fatalf("move sent wrong index or played: moves=%v plays=%v", fake.moves, fake.plays)
	}
	state = h.ui.state()
	command = h.dispatch(playerQueueRemoveMsg{Generation: state.Generation, Snapshot: state.Snapshot, Index: 1})
	h.deliver(t, command)
	if !reflect.DeepEqual(fake.removes, [][]int{{1}}) {
		t.Fatalf("remove selected wrong occurrence: %v", fake.removes)
	}
}

func TestPlayerQueueUIClearAndModes(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"))
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
	state := h.ui.state()
	h.deliver(t, h.dispatch(playerQueueClearMsg{Generation: 1, Snapshot: state.Snapshot, Confirmed: false}))
	if !errors.Is(h.ui.state().Err, errPlayerQueueConfirmation) || fake.clears != 0 {
		t.Fatalf("unconfirmed clear changed the queue: %+v", h.ui.state())
	}
	h.deliver(t, h.dispatch(playerQueueClearMsg{Generation: 1, Snapshot: state.Snapshot, Confirmed: true}))
	if state = h.ui.state(); state.Err != nil || len(state.Items) != 0 || fake.clears != 1 {
		t.Fatalf("confirmed clear = %+v, clears=%d", state, fake.clears)
	}
	h.deliver(t, h.dispatch(playerQueueShuffleMsg{Generation: 1, ReadyEpoch: h.ui.state().ReadyEpoch, Enabled: true}))
	h.deliver(t, h.dispatch(playerQueueRepeatMsg{Generation: 1, ReadyEpoch: h.ui.state().ReadyEpoch, Mode: "one"}))
	state = h.ui.state()
	if state.Err != nil || !state.ModeKnown || !state.Mode.Shuffle || state.Mode.Repeat != "one" ||
		!reflect.DeepEqual(fake.modes, []string{kefw2.PlayModeShuffle, kefw2.PlayModeShuffleRepeatOne}) {
		t.Fatalf("queue modes = %+v, writes=%v", state, fake.modes)
	}
}

func TestPlayerQueueUISessionIsolationABA(t *testing.T) {
	first := newPlayerQueueFake(playerQueueTestItem("A", "a"))
	second := newPlayerQueueFake(playerQueueTestItem("B", "b"))
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	first.queueHook = func(n int) {
		if n == 1 {
			close(started)
			<-release
		}
	}
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": first, "b": second})
	t.Cleanup(unblock)
	old := h.start(playerSpeaker{Name: "A", IPAddress: "a"}, 1)
	playerQueueUIWait(t, started, "old A read")
	h.deliver(t, h.start(playerSpeaker{Name: "B", IPAddress: "b"}, 2))
	bState := h.ui.state()
	h.deliver(t, h.start(playerSpeaker{Name: "A", IPAddress: "a"}, 3))
	aState := h.ui.state()
	if aState.Generation != 3 || aState.Target.IPAddress != "a" ||
		!reflect.DeepEqual(playerQueueTestTitles(aState.Items), []string{"A"}) {
		t.Fatalf("new A state = %+v", aState)
	}
	if command := h.dispatch(playerQueueRemoveMsg{Generation: 2, Snapshot: bState.Snapshot, Index: 0}); command != nil {
		t.Fatal("retired B request produced a command")
	}
	if command := h.dispatch(playerQueueShuffleMsg{Generation: 2, ReadyEpoch: bState.ReadyEpoch, Enabled: true}); command != nil {
		t.Fatal("retired B mode request produced a command")
	}
	if command := h.dispatch(playerQueueRemoveMsg{Generation: 3, Snapshot: bState.Snapshot, Index: 0}); command != nil {
		t.Fatal("another session's snapshot produced a command")
	}
	unblock()
	h.deliver(t, old)
	current := h.ui.state()
	if current.Generation != 3 || current.Snapshot != aState.Snapshot ||
		!reflect.DeepEqual(playerQueueTestTitles(current.Items), []string{"A"}) ||
		len(first.removes) != 0 || len(second.removes) != 0 || len(first.modes) != 0 || len(second.modes) != 0 {
		t.Fatalf("retired result or request crossed sessions: %+v", current)
	}
}

func TestPlayerQueueUICoreStartsAndRetiresSessions(t *testing.T) {
	first := newPlayerQueueFake(playerQueueTestItem("A", "a"))
	second := newPlayerQueueFake(playerQueueTestItem("B", "b"))
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	first.queueHook = func(int) {
		close(started)
		<-release
	}
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": first, "b": second})
	t.Cleanup(unblock)
	h.model.target = &playerSpeaker{Name: "A", IPAddress: "a"}
	h.model.generation = 1
	h.model.pendingStart = true
	initial, ok := h.model.Init()().(tea.BatchMsg)
	if !ok || len(initial) != 3 {
		t.Fatalf("core did not start queue alongside its session: %v", initial)
	}
	playerQueueUIWait(t, started, "core-started queue read")
	next, ok := h.model.selectSpeaker(playerSpeaker{Name: "B", IPAddress: "b"}, false)().(tea.BatchMsg)
	if !ok || len(next) != 3 {
		t.Fatalf("core did not start the new queue session: %v", next)
	}
	h.deliver(t, next[2])
	state := h.ui.state()
	if state.Generation != 2 || state.Target.IPAddress != "b" ||
		!reflect.DeepEqual(playerQueueTestTitles(state.Items), []string{"B"}) {
		t.Fatalf("core switch queue state = %+v", state)
	}
	unblock()
	h.deliver(t, initial[2])
	if current := h.ui.state(); current.Generation != 2 || current.Snapshot != state.Snapshot {
		t.Fatalf("retired core result changed the new queue: %+v", current)
	}
}

func TestPlayerQueueUIWaitsForSharedMutationGate(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
	held := h.model.mutations.reserve()
	if err := held.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer held.release()
	state := h.ui.state()
	command := h.dispatch(playerQueueMoveMsg{Generation: 1, Snapshot: state.Snapshot, From: 0, To: 1})
	h.model.mutations.mu.Lock()
	reservations := len(h.model.mutations.tickets)
	h.model.mutations.mu.Unlock()
	fake.mu.Lock()
	reads, writes := fake.queueReads, len(fake.moves)
	fake.mu.Unlock()
	if command == nil || !h.ui.state().Busy || reservations != 2 || reads != 1 || writes != 0 {
		t.Fatalf("queued transaction: reservations=%d reads=%d writes=%d state=%+v",
			reservations, reads, writes, h.ui.state())
	}
	held.release()
	h.deliver(t, command)
	if len(fake.moves) != 1 || fake.queueReads != 3 || len(fake.plays) != 0 {
		t.Fatalf("released transaction = moves %v, reads %d, plays %v", fake.moves, fake.queueReads, fake.plays)
	}
}

func TestPlayerQueueUICanceledGateWaitNeverWritesOrNotifies(t *testing.T) {
	first := newPlayerQueueFake(playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	second := newPlayerQueueFake(playerQueueTestItem("New", "new"))
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": first, "b": second})
	h.deliver(t, h.start(playerSpeaker{Name: "A", IPAddress: "a"}, 1))
	held := h.model.mutations.reserve()
	if err := held.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer held.release()
	state := h.ui.state()
	old := h.dispatch(playerQueueMoveMsg{Generation: 1, Snapshot: state.Snapshot, From: 0, To: 1})
	h.deliver(t, h.start(playerSpeaker{Name: "B", IPAddress: "b"}, 2))
	held.release()
	h.deliver(t, old)
	if first.queueReads != 1 || len(first.moves) != 0 || len(second.moves) != 0 || len(h.notices) != 0 {
		t.Fatalf("canceled queued action performed I/O or emitted a notice: reads=%d A=%v B=%v notices=%v",
			first.queueReads, first.moves, second.moves, h.notices)
	}
}

func TestPlayerQueueUIMissingMutationGateFailsClosed(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
	h.ui.active.mutations = nil
	state := h.ui.state()
	command := h.dispatch(playerQueueMoveMsg{Generation: 1, Snapshot: state.Snapshot, From: 0, To: 1})
	if command != nil || h.ui.state().Err == nil || fake.queueReads != 1 || len(fake.moves) != 0 {
		t.Fatalf("missing gate allowed a mutation: state=%+v moves=%v", h.ui.state(), fake.moves)
	}
}

func TestPlayerQueueUIDiscoveryInvalidationCoalesces(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"))
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	fake.queueHook = func(n int) {
		if n == 1 {
			close(started)
			<-release
		}
	}
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	t.Cleanup(unblock)
	initial := h.start(playerSpeaker{IPAddress: "a"}, 1)
	playerQueueUIWait(t, started, "initial queue read")
	changed := playerScopedMsg{generation: 1, message: playerDiscoveryQueueChangedMsg{generation: 1}}
	first, second := h.dispatch(changed), h.dispatch(changed)
	if first != nil || second != nil {
		t.Fatal("invalidation started a concurrent load")
	}
	fake.mu.Lock()
	fake.rows = append(fake.rows, playerQueueTestItem("Added", "b"))
	fake.version++
	fake.mu.Unlock()
	unblock()
	h.deliver(t, initial)
	state := h.ui.state()
	if state.Busy || state.Err != nil || fake.queueReads != 2 ||
		!reflect.DeepEqual(playerQueueTestTitles(state.Items), []string{"A", "Added"}) ||
		len(fake.moves) != 0 || len(fake.plays) != 0 || len(fake.modes) != 0 {
		t.Fatalf("coalesced invalidation = %+v, reads=%d", state, fake.queueReads)
	}
	if command := h.dispatch(playerScopedMsg{
		generation: 2, message: playerDiscoveryQueueChangedMsg{generation: 2},
	}); command != nil {
		t.Fatal("retired generation invalidation started a load")
	}
	fake.mu.Lock()
	fake.rows = append(fake.rows, playerQueueTestItem("Another", "c"))
	fake.version++
	fake.mu.Unlock()
	h.deliver(t, h.dispatch(changed))
	if current := h.ui.state(); fake.queueReads != 3 ||
		!reflect.DeepEqual(playerQueueTestTitles(current.Items), []string{"A", "Added", "Another"}) {
		t.Fatalf("idle invalidation = %+v, reads=%d", current, fake.queueReads)
	}
}

func TestPlayerQueueUIRejectsPreflightConflict(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
	displayed := h.ui.state()
	fake.mu.Lock()
	fake.rows[0].Title = "Externally changed"
	fake.mu.Unlock()
	h.deliver(t, h.dispatch(playerQueueMoveMsg{
		Generation: displayed.Generation, Snapshot: displayed.Snapshot, From: 1, To: 0,
	}))
	state := h.ui.state()
	if !errors.Is(state.Err, errPlayerQueueStale) || state.Snapshot == displayed.Snapshot ||
		!reflect.DeepEqual(playerQueueTestTitles(state.Items), []string{"Externally changed", "B"}) ||
		len(fake.moves) != 0 || len(fake.plays) != 0 {
		t.Fatalf("conflict was not surfaced without a write: %+v", state)
	}
	if command := h.dispatch(playerQueueMoveMsg{
		Generation: state.Generation, Snapshot: displayed.Snapshot, From: 1, To: 0,
	}); command != nil || len(fake.moves) != 0 {
		t.Fatal("outdated presentation token produced a write")
	}
}

func TestPlayerQueueUIUncertainWriteAfterSwitchNotifiesOldTarget(t *testing.T) {
	first := newPlayerQueueFake(playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	second := newPlayerQueueFake(playerQueueTestItem("New speaker", "new"))
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	first.afterWrite = func(string) {
		close(started)
		<-release
	}
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": first, "b": second})
	t.Cleanup(unblock)
	h.deliver(t, h.start(playerSpeaker{Name: "Living", IPAddress: "a"}, 1))
	stateA := h.ui.state()
	old := h.dispatch(playerQueueMoveMsg{Generation: 1, Snapshot: stateA.Snapshot, From: 0, To: 1})
	playerQueueUIWait(t, started, "old speaker write")
	if command := h.dispatch(playerQueueRemoveMsg{Generation: 1, Snapshot: stateA.Snapshot, Index: 0}); command != nil {
		t.Fatal("a second queue edit was queued while the first was unresolved")
	}
	h.offline()
	h.deliver(t, h.start(playerSpeaker{Name: "Bedroom", IPAddress: "b"}, 2))
	stateB := h.ui.state()
	unblock()
	h.deliver(t, old)
	current := h.ui.state()
	if current.Generation != 2 || current.Target.IPAddress != "b" || current.Snapshot != stateB.Snapshot ||
		!reflect.DeepEqual(playerQueueTestTitles(current.Items), []string{"New speaker"}) {
		t.Fatalf("old write changed new session presentation: %+v", current)
	}
	if len(h.notices) != 1 || h.notices[0].target.IPAddress != "a" ||
		h.notices[0].action != playerQueueUIMove || !errors.Is(h.notices[0].err, errPlayerQueueUncertain) {
		t.Fatalf("old target uncertainty notice = %+v", h.notices)
	}
	if len(first.moves) != 1 || len(second.moves) != 0 || len(second.plays) != 0 {
		t.Fatalf("write crossed targets: A=%v B=%v", first.moves, second.moves)
	}
}

func TestPlayerQueueUIUncertainReadbackRequiresExplicitReview(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	fake.queueErrors[3] = errors.New("readback unavailable")
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{Name: "A", IPAddress: "a"}, 1))
	state := h.ui.state()
	h.deliver(t, h.dispatch(playerQueueMoveMsg{Generation: 1, Snapshot: state.Snapshot, From: 0, To: 1}))
	state = h.ui.state()
	if !state.Uncertain || state.ReviewReady || state.ReviewID == 0 || state.Snapshot != nil ||
		!errors.Is(state.Err, errPlayerQueueUncertain) || len(h.notices) != 1 {
		t.Fatalf("uncertain state = %+v, notices=%v", state, h.notices)
	}
	if command := h.dispatch(playerQueueAcknowledgeMsg{Generation: 1, ReviewID: state.ReviewID}); command != nil {
		t.Fatal("uncertainty was acknowledged without a readback")
	}
	if command := h.dispatch(playerQueueRepeatMsg{Generation: 1, ReadyEpoch: state.ReadyEpoch, Mode: "all"}); command != nil {
		t.Fatal("write was accepted before uncertainty review")
	}
	h.deliver(t, h.dispatch(playerQueueLoadMsg{Generation: 1}))
	state = h.ui.state()
	if !state.Uncertain || !state.ReviewReady ||
		!reflect.DeepEqual(playerQueueTestTitles(state.Items), []string{"B", "A"}) {
		t.Fatalf("review readback = %+v", state)
	}
	if command := h.dispatch(playerQueueAcknowledgeMsg{Generation: 1, ReviewID: state.ReviewID + 1}); command != nil {
		t.Fatal("a different uncertainty was acknowledged")
	}
	h.deliver(t, h.dispatch(playerQueueAcknowledgeMsg{Generation: 1, ReviewID: state.ReviewID}))
	if state = h.ui.state(); state.Uncertain || state.ReviewReady || state.Err != nil || len(fake.moves) != 1 {
		t.Fatalf("explicit acknowledgement = %+v, moves=%v", state, fake.moves)
	}
}

func TestPlayerQueueUIModeUncertaintyRequiresModeReadback(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"))
	fake.modeErrors[3] = errors.New("mode readback unavailable")
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{Name: "A", IPAddress: "a"}, 1))
	h.deliver(t, h.dispatch(playerQueueShuffleMsg{Generation: 1, ReadyEpoch: h.ui.state().ReadyEpoch, Enabled: true}))
	state := h.ui.state()
	if !state.Uncertain || state.ReviewReady || state.ModeKnown || state.Snapshot == nil || len(h.notices) != 1 {
		t.Fatalf("uncertain mode state = %+v, notices=%v", state, h.notices)
	}
	h.deliver(t, h.dispatch(playerQueueLoadMsg{Generation: 1}))
	state = h.ui.state()
	if !state.Uncertain || !state.ReviewReady || !state.ModeKnown || !state.Mode.Shuffle {
		t.Fatalf("mode review readback = %+v", state)
	}
	h.deliver(t, h.dispatch(playerQueueAcknowledgeMsg{Generation: 1, ReviewID: state.ReviewID}))
	if state = h.ui.state(); state.Uncertain || state.Err != nil || len(fake.modes) != 1 {
		t.Fatalf("mode acknowledgement = %+v, writes=%v", state, fake.modes)
	}
}

func TestPlayerQueueUIPlayingMarkerRequiresUniquePath(t *testing.T) {
	path := "playlists:item/1"
	fake := newPlayerQueueFake(playerQueueTestItem("Same", path), playerQueueTestItem("Same", path))
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
	snapshot := statusSnapshot{Player: kefw2.PlayerData{
		State: kefw2.PlayerStatePlaying, TrackRoles: kefw2.PlayerTrackRoles{Path: path},
	}}
	h.dispatch(playerScopedMsg{generation: 1, message: watchUpdate{kind: watchSnapshotUpdate, snapshot: snapshot}})
	if got := h.ui.state().PlayingIndex; got != -1 {
		t.Fatalf("ambiguous playing occurrence = %d", got)
	}
	fake.mu.Lock()
	fake.rows[0].Path = "playlists:item/other"
	fake.version++
	fake.mu.Unlock()
	h.deliver(t, h.dispatch(playerQueueLoadMsg{Generation: 1}))
	if got := h.ui.state().PlayingIndex; got != 1 {
		t.Fatalf("unique playing occurrence = %d, want 1", got)
	}
}

func TestPlayerQueueUIPlayClearsMarkerUntilConfirmedStatus(t *testing.T) {
	firstPath, secondPath := "playlists:item/1", "playlists:item/2"
	fake := newPlayerQueueFake(playerQueueTestItem("First", firstPath), playerQueueTestItem("Second", secondPath))
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	fake.afterWrite = func(string) {
		close(started)
		<-release
	}
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	t.Cleanup(unblock)
	h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
	oldStatus := statusSnapshot{Player: kefw2.PlayerData{
		State: kefw2.PlayerStatePlaying, TrackRoles: kefw2.PlayerTrackRoles{Path: firstPath, Title: "First"},
	}}
	h.dispatch(playerScopedMsg{generation: 1, message: watchUpdate{kind: watchSnapshotUpdate, snapshot: oldStatus}})
	state := h.ui.state()
	if state.PlayingIndex != 0 {
		t.Fatalf("initial playing index = %d, want 0", state.PlayingIndex)
	}
	command := h.dispatch(playerQueuePlayMsg{Generation: 1, Snapshot: state.Snapshot, Index: 1})
	if command == nil {
		t.Fatal("queue play was not accepted")
	}
	playerQueueUIWait(t, started, "queue play")
	h.dispatch(playerScopedMsg{generation: 1, message: watchUpdate{kind: watchSnapshotUpdate, snapshot: oldStatus}})
	if got := h.ui.state().PlayingIndex; got != -1 {
		t.Fatalf("old status restored a marker during play: %d", got)
	}
	unblock()
	h.dispatch(command())
	sequence := h.factory.sessions[0].refreshes.Load()
	if h.ui.state().PlayingIndex != -1 || len(fake.plays) != 1 || sequence == 0 {
		t.Fatalf("play without status event: state=%+v plays=%v", h.ui.state(), fake.plays)
	}
	h.dispatch(playerScopedMsg{generation: 1, message: playerStatusRefreshedResult{
		sequence: sequence - 1, update: watchUpdate{kind: watchSnapshotUpdate, snapshot: oldStatus},
	}})
	if got := h.ui.state().PlayingIndex; got != -1 {
		t.Fatalf("an older status read restored a stale marker: %d", got)
	}
	newStatus := statusSnapshot{Player: kefw2.PlayerData{
		State: kefw2.PlayerStatePlaying, TrackRoles: kefw2.PlayerTrackRoles{Path: secondPath, Title: "Second"},
	}}
	h.dispatch(playerScopedMsg{generation: 1, message: playerStatusRefreshedResult{
		sequence: sequence + 1, update: watchUpdate{kind: watchSnapshotUpdate, snapshot: newStatus},
	}})
	if got := h.ui.state().PlayingIndex; got != 1 {
		t.Fatalf("confirmed playing index = %d, want 1", got)
	}
}

func TestPlayerQueueUIUnsentPlayDoesNotRefreshStatus(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("First", "first"))
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
	state := h.ui.state()
	fake.mu.Lock()
	fake.rows[0].Title = "Externally changed"
	fake.version++
	fake.mu.Unlock()
	command := h.dispatch(playerQueuePlayMsg{Generation: 1, Snapshot: state.Snapshot, Index: 0})
	if command == nil {
		t.Fatal("queue play was not accepted for preflight")
	}
	if refresh := h.dispatch(command()); refresh != nil || h.factory.sessions[0].refreshes.Load() != 0 ||
		len(fake.plays) != 0 ||
		!errors.Is(h.ui.state().Err, errPlayerQueueStale) {
		t.Fatalf("unsent stale play requested status refresh: state=%+v plays=%v", h.ui.state(), fake.plays)
	}
}

func TestPlayerQueueUIPlayRefreshFailureLeavesMarkerUnknown(t *testing.T) {
	firstPath := "playlists:item/1"
	fake := newPlayerQueueFake(playerQueueTestItem("First", firstPath), playerQueueTestItem("Second", "source:2"))
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
	oldStatus := statusSnapshot{Player: kefw2.PlayerData{
		State: kefw2.PlayerStatePlaying, TrackRoles: kefw2.PlayerTrackRoles{Path: firstPath, Title: "First"},
	}}
	h.dispatch(playerScopedMsg{generation: 1, message: watchUpdate{kind: watchSnapshotUpdate, snapshot: oldStatus}})
	state := h.ui.state()
	h.deliver(t, h.dispatch(playerQueuePlayMsg{Generation: 1, Snapshot: state.Snapshot, Index: 1}))
	sequence := h.factory.sessions[0].refreshes.Load()
	if sequence == 0 {
		t.Fatal("sent play did not request a status refresh")
	}
	h.dispatch(playerScopedMsg{generation: 1, message: playerStatusRefreshedResult{
		sequence: sequence, update: watchUpdate{kind: playerStatusUnavailableUpdate, err: errors.New("offline")},
	}})
	if state = h.ui.state(); state.Ready || state.PlayingIndex != -1 {
		t.Fatalf("failed refresh restored a marker: %+v", state)
	}
	newStatus := statusSnapshot{Player: kefw2.PlayerData{
		State: kefw2.PlayerStatePlaying,
		TrackRoles: kefw2.PlayerTrackRoles{
			Path: "playlists:item/2", Title: "Second",
		},
	}}
	h.dispatch(playerScopedMsg{generation: 1, message: watchUpdate{kind: watchSnapshotUpdate, snapshot: newStatus}})
	if got := h.ui.state().PlayingIndex; got != 1 {
		t.Fatalf("later confirmed title did not restore marker: %d", got)
	}
}

func TestPlayerQueueUINewerPlayRejectsOlderStatusRead(t *testing.T) {
	fake := newPlayerQueueFake(
		playerQueueTestItem("First", "playlists:item/1"),
		playerQueueTestItem("Second", "playlists:item/2"),
		playerQueueTestItem("Third", "playlists:item/3"),
	)
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
	state := h.ui.state()
	h.deliver(t, h.dispatch(playerQueuePlayMsg{Generation: 1, Snapshot: state.Snapshot, Index: 1}))
	firstSequence := h.factory.sessions[0].refreshes.Load()
	state = h.ui.state()
	h.deliver(t, h.dispatch(playerQueuePlayMsg{Generation: 1, Snapshot: state.Snapshot, Index: 2}))
	secondSequence := h.factory.sessions[0].refreshes.Load()
	if firstSequence == 0 || secondSequence <= firstSequence {
		t.Fatalf("status refresh sequences = %d, %d", firstSequence, secondSequence)
	}
	secondStatus := statusSnapshot{Player: kefw2.PlayerData{
		State: kefw2.PlayerStatePlaying,
		TrackRoles: kefw2.PlayerTrackRoles{
			Path: "playlists:item/2", Title: "Second",
		},
	}}
	h.dispatch(playerScopedMsg{generation: 1, message: playerStatusRefreshedResult{
		sequence: firstSequence, update: watchUpdate{kind: watchSnapshotUpdate, snapshot: secondStatus},
	}})
	if got := h.ui.state().PlayingIndex; got != -1 {
		t.Fatalf("old play's status marked row %d", got)
	}
	thirdStatus := statusSnapshot{Player: kefw2.PlayerData{
		State: kefw2.PlayerStatePlaying,
		TrackRoles: kefw2.PlayerTrackRoles{
			Path: "playlists:item/3", Title: "Third",
		},
	}}
	h.dispatch(playerScopedMsg{generation: 1, message: playerStatusRefreshedResult{
		sequence: secondSequence, update: watchUpdate{kind: watchSnapshotUpdate, snapshot: thirdStatus},
	}})
	if got := h.ui.state().PlayingIndex; got != 2 {
		t.Fatalf("new play's confirmed marker = %d, want 2", got)
	}
}

func TestPlayerQueueUIPlayingMarkerFallsBackToUniqueTitle(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rows  []kefw2.ContentItem
		path  string
		title string
		state string
		want  int
	}{
		{
			name: "internal path takes precedence",
			rows: []kefw2.ContentItem{
				playerQueueTestItem("Other", "source:other"),
				playerQueueTestItem("Playing", "playlists:item/1"),
			},
			path: "playlists:item/1", title: "Other", state: kefw2.PlayerStatePlaying, want: 1,
		},
		{
			name: "unique title for source path",
			rows: []kefw2.ContentItem{
				playerQueueTestItem("First", "source:first"),
				playerQueueTestItem("Playing", "source:playing"),
			},
			path: "playlists:item/1", title: "Playing", state: kefw2.PlayerStatePaused, want: 1,
		},
		{
			name: "duplicate title is ambiguous",
			rows: []kefw2.ContentItem{
				playerQueueTestItem("Same", "source:1"),
				playerQueueTestItem("Same", "source:2"),
			},
			path: "playlists:item/1", title: "Same", state: kefw2.PlayerStatePlaying, want: -1,
		},
		{
			name: "duplicate exact path is ambiguous",
			rows: []kefw2.ContentItem{
				playerQueueTestItem("First", "playlists:item/1"),
				playerQueueTestItem("Playing", "playlists:item/1"),
			},
			path: "playlists:item/1", title: "Playing", state: kefw2.PlayerStatePlaying, want: -1,
		},
		{
			name:  "empty title is not a fallback identity",
			rows:  []kefw2.ContentItem{playerQueueTestItem("", "source:1")},
			path:  "playlists:item/1",
			state: kefw2.PlayerStatePlaying,
			want:  -1,
		},
		{
			name:  "non-queue playback does not match title",
			rows:  []kefw2.ContentItem{playerQueueTestItem("Playing", "source:1")},
			path:  "source:1",
			title: "Playing",
			state: kefw2.PlayerStatePlaying,
			want:  -1,
		},
		{
			name:  "stopped playback has no marker",
			rows:  []kefw2.ContentItem{playerQueueTestItem("Playing", "source:1")},
			path:  "playlists:item/1",
			title: "Playing",
			state: kefw2.PlayerStateStopped,
			want:  -1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": newPlayerQueueFake(tc.rows...)})
			h.deliver(t, h.start(playerSpeaker{IPAddress: "a"}, 1))
			snapshot := statusSnapshot{Player: kefw2.PlayerData{
				State: tc.state, TrackRoles: kefw2.PlayerTrackRoles{Path: tc.path, Title: tc.title},
			}}
			h.dispatch(playerScopedMsg{generation: 1, message: watchUpdate{kind: watchSnapshotUpdate, snapshot: snapshot}})
			if got := h.ui.state().PlayingIndex; got != tc.want {
				t.Fatalf("playing index = %d, want %d", got, tc.want)
			}
			h.offline()
			if got := h.ui.state().PlayingIndex; got != -1 {
				t.Fatalf("unavailable status retained playing index %d", got)
			}
		})
	}
}

func TestPlayerQueueUIStopCancelsAndJoinsBlockedWorker(t *testing.T) {
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"))
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	fake.queueHook = func(int) {
		close(started)
		<-release
	}
	h := newPlayerQueueUIHarness(t, map[string]*playerQueueFake{"a": fake})
	t.Cleanup(unblock)
	command := h.start(playerSpeaker{IPAddress: "a"}, 1)
	playerQueueUIWait(t, started, "blocked queue read")
	done := h.ui.stop(h.model)
	if len(done) != 1 {
		t.Fatalf("shutdown completion channels = %d, want 1", len(done))
	}
	select {
	case <-done[0]:
		t.Fatal("shutdown claimed a blocked worker had completed")
	default:
	}
	unblock()
	playerQueueUIWait(t, done[0], "worker completion")
	h.deliver(t, command)
	if h.ui.state().Snapshot != nil || len(fake.moves) != 0 || len(h.notices) != 0 {
		t.Fatal("canceled read affected state or issued a write")
	}
}

type playerQueueUIDisconnectHTTP struct {
	t                          *testing.T
	phase                      string
	started, canceled, release chan struct{}
	reads, writes              atomic.Int32
}

func (f *playerQueueUIDisconnectHTTP) block(r *http.Request) {
	close(f.started)
	select {
	case <-r.Context().Done():
		close(f.canceled)
	case <-f.release:
	}
}

func (f *playerQueueUIDisconnectHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/getRows":
		if f.reads.Add(1) == 2 && f.phase == "preflight" {
			f.block(r)
			return
		}
		_, err := w.Write([]byte(`{"rowsCount":2,"rowsVersion":1,"rows":[` +
			`{"title":"A","path":"a","type":"audio"},` +
			`{"title":"B","path":"b","type":"audio"}]}`))
		if err != nil {
			f.t.Errorf("write queue response: %v", err)
		}
	case "/api/getData":
		if _, err := w.Write([]byte(`[{"type":"playerPlayMode","playerPlayMode":"normal"}]`)); err != nil {
			f.t.Errorf("write mode response: %v", err)
		}
	case "/api/setData":
		f.writes.Add(1)
		if f.phase == "write" {
			if _, err := io.Copy(io.Discard, r.Body); err != nil {
				f.t.Errorf("read mutation request: %v", err)
			}
			f.block(r)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	default:
		http.NotFound(w, r)
	}
}

func TestPlayerQueueUIDisconnectCancelsMutationHTTP(t *testing.T) {
	for _, phase := range []string{"preflight", "write"} {
		t.Run(phase, func(t *testing.T) {
			fixture := &playerQueueUIDisconnectHTTP{
				t: t, phase: phase, started: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{}),
			}
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(fixture.release) }) }
			server := httptest.NewServer(fixture)
			defer server.Close()
			defer unblock()
			h := newPlayerQueueUIHarness(t, nil)
			address := strings.TrimPrefix(server.URL, "http://")
			h.deliver(t, h.start(playerSpeaker{Name: "HTTP", IPAddress: address}, 1))
			state := h.ui.state()
			command := h.dispatch(playerQueueMoveMsg{Generation: 1, Snapshot: state.Snapshot, From: 0, To: 1})
			playerQueueUIWait(t, fixture.started, phase+" request")
			h.offline()
			playerQueueUIWait(t, fixture.canceled, phase+" HTTP cancellation")
			h.deliver(t, command)
			state = h.ui.state()
			if state.Ready || !errors.Is(state.Err, errPlayerQueueStatusUnavailable) {
				t.Fatalf("disconnected %s state = %+v", phase, state)
			}
			if phase == "preflight" {
				if fixture.writes.Load() != 0 || state.Uncertain || len(h.notices) != 0 {
					t.Fatalf("canceled preflight: writes=%d state=%+v notices=%+v", fixture.writes.Load(), state, h.notices)
				}
			} else if fixture.writes.Load() != 1 || !state.Uncertain ||
				!errors.Is(state.Err, errPlayerQueueUncertain) || len(h.notices) != 1 ||
				h.notices[0].target.IPAddress != address {
				t.Fatalf("canceled write: writes=%d state=%+v notices=%+v", fixture.writes.Load(), state, h.notices)
			}
			h.ready(1)
			wantWrites := int32(0)
			if phase == "write" {
				wantWrites = 1
			}
			if got := fixture.writes.Load(); got != wantWrites {
				t.Fatalf("reconnection replayed a %s write: %d", phase, got)
			}
		})
	}
}

func TestPlayerQueueUIWorkerContextCancelsHTTP(t *testing.T) {
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		close(started)
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-release:
		}
	}))
	defer server.Close()
	defer unblock()
	h := newPlayerQueueUIHarness(t, nil)
	address := strings.TrimPrefix(server.URL, "http://")
	command := h.start(playerSpeaker{Name: "HTTP", IPAddress: address}, 1)
	playerQueueUIWait(t, started, "HTTP queue request")
	done := h.ui.stop(h.model)
	playerQueueUIWait(t, canceled, "HTTP cancellation")
	for _, worker := range done {
		playerQueueUIWait(t, worker, "HTTP queue worker")
	}
	h.deliver(t, command)
	if writes.Load() != 0 || len(h.notices) != 0 {
		t.Fatalf("canceled queue load wrote to speaker: writes=%d notices=%v", writes.Load(), h.notices)
	}
}
