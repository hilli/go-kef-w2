// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hilli/go-kef-w2/kefw2"
)

type playerControlsUITestReply struct {
	snapshot  statusSnapshot
	muted     bool
	confirmed int
	err       error
	uncertain bool
	maximum   int
	maxRead   bool
	maxErr    error
}

type playerControlsUITestCall struct {
	target playerSpeaker
	action string
	value  int
	source kefw2.Source
	reply  chan playerControlsUITestReply
}

type playerControlsUITestBackend struct {
	target playerSpeaker
	calls  chan<- playerControlsUITestCall
	record func(playerControlsUITestCall, playerControlsUITestReply)
}

func (b *playerControlsUITestBackend) call(
	ctx context.Context, action string, value int, source kefw2.Source,
) (playerControlsUITestReply, bool) {
	call := playerControlsUITestCall{
		target: b.target, action: action, value: value, source: source,
		reply: make(chan playerControlsUITestReply, 1),
	}
	select {
	case b.calls <- call:
	case <-ctx.Done():
		return playerControlsUITestReply{err: ctx.Err()}, false
	}
	select {
	case reply := <-call.reply:
		if reply.err == nil && b.record != nil {
			b.record(call, reply)
		}
		return reply, true
	case <-ctx.Done():
		return playerControlsUITestReply{err: ctx.Err(), uncertain: true}, true
	}
}

func (b *playerControlsUITestBackend) actionError(action string, reply playerControlsUITestReply) error {
	if reply.uncertain {
		return &playerMutationUncertainError{Action: action, Target: b.target.IPAddress, Err: reply.err}
	}
	return reply.err
}

func (b *playerControlsUITestBackend) Perform(
	ctx context.Context, action playerControlAction,
) (statusSnapshot, error) {
	reply, _ := b.call(ctx, string(action), 0, "")
	return reply.snapshot, b.actionError(string(action), reply)
}

func (b *playerControlsUITestBackend) ToggleMute(ctx context.Context) (bool, error) {
	reply, _ := b.call(ctx, "mute", 0, "")
	return reply.muted, b.actionError("mute", reply)
}

func (b *playerControlsUITestBackend) SetSource(
	ctx context.Context, source kefw2.Source,
) (statusSnapshot, error) {
	reply, _ := b.call(ctx, "source", 0, source)
	return reply.snapshot, b.actionError("source", reply)
}

func (b *playerControlsUITestBackend) SeekTo(ctx context.Context, position int64) (statusSnapshot, error) {
	reply, _ := b.call(ctx, "seek", int(position), "")
	return reply.snapshot, b.actionError("seek", reply)
}

func (b *playerControlsUITestBackend) SetVolume(ctx context.Context, target int) (playerVolumeOutcome, error) {
	reply, attempted := b.call(ctx, "volume", target, "")
	outcome := playerVolumeOutcome{
		Target: target, Confirmed: reply.confirmed, ConfirmedKnown: attempted && reply.err == nil,
		ReadbackConfirmed: attempted && reply.err == nil, Attempted: attempted, Uncertain: reply.uncertain,
		Maximum: reply.maximum, MaximumRead: reply.maxRead, MaximumErr: reply.maxErr,
	}
	return outcome, b.actionError("set volume", reply)
}

type playerControlsUITestHarness struct {
	model     *playerModel
	ui        *playerControlsUI
	factory   *fakePlayerFactory
	clock     *playerTestClock
	calls     chan playerControlsUITestCall
	events    chan tea.Msg
	done      chan struct{}
	notices   []playerNoticeMsg
	mu        sync.Mutex
	snapshots map[string]statusSnapshot
}

func newPlayerControlsUITestHarness(t *testing.T) *playerControlsUITestHarness {
	t.Helper()
	h := &playerControlsUITestHarness{
		factory: &fakePlayerFactory{autoFinish: true}, clock: newPlayerTestClock(),
		calls: make(chan playerControlsUITestCall, 32), events: make(chan tea.Msg, 128),
		done: make(chan struct{}), snapshots: make(map[string]statusSnapshot),
	}
	a := playerSpeaker{Name: "Living", IPAddress: "a"}
	b := playerSpeaker{Name: "Bedroom", IPAddress: "b"}
	h.model = newPlayerModel(context.Background(), playerListeningDesk,
		playerSelection{configured: []playerSpeaker{a, b}, selected: &a}, h.factory.newSession)
	h.ui = newPlayerControlsUI()
	h.ui.backendFactory = func(target playerSpeaker) (playerControlsUIBackend, error) {
		return &playerControlsUITestBackend{target: target, calls: h.calls, record: h.record}, nil
	}
	h.ui.volumeFactory = func(
		ctx context.Context, confirmed int, writer playerVolumeWriter,
	) (*playerVolumeCoalescer, error) {
		return newPlayerVolumeCoalescerWithClock(ctx, confirmed, writer, playerVolumeClock{
			now: h.clock.now, after: h.clock.after,
		})
	}
	h.ui.readStatus = func(ctx context.Context, target playerSpeaker) (statusSnapshot, error) {
		if err := ctx.Err(); err != nil {
			return statusSnapshot{}, err
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		return playerControlsCopySnapshot(h.snapshots[target.IPAddress]), nil
	}
	h.model.addExtension(h.ui.extension())
	h.schedule(h.model.Init())
	t.Cleanup(func() {
		close(h.done)
		if err := h.model.shutdown(); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
	return h
}

func (h *playerControlsUITestHarness) record(call playerControlsUITestCall, reply playerControlsUITestReply) {
	h.mu.Lock()
	defer h.mu.Unlock()
	snapshot := h.snapshots[call.target.IPAddress]
	switch call.action {
	case "mute":
		snapshot.Data.Muted = reply.muted
	case "volume":
		snapshot.Data.Volume = reply.confirmed
	default:
		snapshot = reply.snapshot
	}
	h.snapshots[call.target.IPAddress] = playerControlsCopySnapshot(snapshot)
}

func (h *playerControlsUITestHarness) schedule(command tea.Cmd) {
	if command == nil {
		return
	}
	go func() {
		message := command()
		if batch, ok := message.(tea.BatchMsg); ok {
			for _, child := range batch {
				h.schedule(child)
			}
			return
		}
		if message == nil {
			return
		}
		select {
		case h.events <- message:
		case <-h.done:
		}
	}()
}

// send simulates the presentation capturing the current epoch; sendRaw preserves it.
func (h *playerControlsUITestHarness) send(message tea.Msg) {
	epoch := h.ui.State().epoch
	switch request := message.(type) {
	case playerTransportRequest:
		request.epoch = epoch
		message = request
	case playerMuteRequest:
		request.epoch = epoch
		message = request
	case playerSourceRequest:
		request.epoch = epoch
		message = request
	case playerSeekRequest:
		request.epoch = epoch
		message = request
	case playerVolumeTargetRequest:
		request.epoch = epoch
		message = request
	case playerVolumeSliderRequest:
		request.epoch = epoch
		message = request
	case playerVolumeKeyRequest:
		request.epoch = epoch
		message = request
	case playerVolumeRetryRequest:
		request.epoch = epoch
		message = request
	case playerVolumeDismissRequest:
		request.epoch = epoch
		message = request
	case playerControlDismissErrorRequest:
		request.epoch = epoch
		message = request
	}
	h.sendRaw(message)
}

func (h *playerControlsUITestHarness) sendRaw(message tea.Msg) {
	_, command := h.model.Update(message)
	h.schedule(command)
}

func (h *playerControlsUITestHarness) until(t *testing.T, predicate func() bool) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for !predicate() {
		select {
		case message := <-h.events:
			if notice, ok := message.(playerNoticeMsg); ok {
				h.notices = append(h.notices, notice)
			}
			h.sendRaw(message)
		case <-timer.C:
			t.Fatalf("player UI timed out; state: %+v", h.ui.State())
		}
	}
}

func (h *playerControlsUITestHarness) status(snapshot statusSnapshot) {
	h.mu.Lock()
	h.snapshots[h.model.target.IPAddress] = playerControlsCopySnapshot(snapshot)
	h.mu.Unlock()
	h.send(playerSessionMsg{generation: h.model.generation, message: watchUpdate{
		kind: watchSnapshotUpdate, snapshot: snapshot,
	}})
}

func (h *playerControlsUITestHarness) event(err error) {
	h.send(playerSessionMsg{generation: h.model.generation, message: watchUpdate{
		kind: playerEventConnectionUpdate, err: err,
	}})
}

func (h *playerControlsUITestHarness) maximum(maximum int, err error) {
	h.send(playerSessionMsg{generation: h.model.generation, message: playerMaxVolumeResult{
		volume: maximum, err: err,
	}})
}

func (h *playerControlsUITestHarness) info(model string) {
	h.send(playerSessionMsg{generation: h.model.generation, message: playerInfoResult{
		speaker: playerSpeaker{Model: model},
	}})
}

func (h *playerControlsUITestHarness) call(t *testing.T, action string) playerControlsUITestCall {
	t.Helper()
	call := receivePlayer(t, h.calls)
	if call.action != action {
		t.Fatalf("called %q, want %q", call.action, action)
	}
	return call
}

func (h *playerControlsUITestHarness) noCall(t *testing.T) {
	t.Helper()
	select {
	case call := <-h.calls:
		t.Fatalf("unexpected mutation: %+v", call)
	default:
	}
}

func playerControlsUITestSnapshot(volume int) statusSnapshot {
	position, duration := 1000, 100000
	return statusSnapshot{
		Data: statusResponse{
			Source: kefw2.SourceWiFi, Power: kefw2.SpeakerStatusOn, Volume: volume,
			Playback: &playbackStatus{State: kefw2.PlayerStatePlaying, PositionMS: &position, DurationMS: &duration},
		},
		Player: kefw2.PlayerData{Controls: kefw2.PlayerControls{Pause: true, Previous: true, Next: true}},
	}
}

func TestPlayerControlsUISourcesRespectFreshModel(t *testing.T) {
	tests := []struct {
		model  string
		allow  []kefw2.Source
		reject []kefw2.Source
	}{
		{"LSXII", []kefw2.Source{kefw2.SourceTV, kefw2.SourceUSB, kefw2.SourceAux},
			[]kefw2.Source{kefw2.SourceCoaxial}},
		{"LSXIILT", []kefw2.Source{kefw2.SourceTV, kefw2.SourceUSB, kefw2.SourceOptical},
			[]kefw2.Source{kefw2.SourceAux, kefw2.SourceCoaxial}},
		{"KEF LS50 II Wireless", []kefw2.Source{kefw2.SourceTV, kefw2.SourceCoaxial, kefw2.SourceAux},
			[]kefw2.Source{kefw2.SourceUSB}},
		{"LS60W", []kefw2.Source{kefw2.SourceTV, kefw2.SourceCoaxial, kefw2.SourceAux},
			[]kefw2.Source{kefw2.SourceUSB}},
		{"unknown", []kefw2.Source{kefw2.SourceWiFi, kefw2.SourceBluetooth},
			[]kefw2.Source{kefw2.SourceTV, kefw2.SourceAux}},
	}
	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			h := newPlayerControlsUITestHarness(t)
			h.status(playerControlsUITestSnapshot(25))
			h.info(test.model)
			for _, source := range test.allow {
				if !h.ui.SupportsSource(source) {
					t.Errorf("%s unexpectedly disabled on %s", source, test.model)
				}
			}
			for _, source := range test.reject {
				if h.ui.SupportsSource(source) {
					t.Errorf("%s unexpectedly enabled on %s", source, test.model)
				}
				h.send(playerSourceRequest{generation: h.model.generation, source: source})
				if h.ui.State().err == nil {
					t.Errorf("unverified source %s was not rejected", source)
				}
				h.noCall(t)
			}
		})
	}
}

func TestPlayerControlsUISwitchDiscardsSourceCapabilities(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.info("LS60W")
	if !h.ui.SupportsSource(kefw2.SourceAux) {
		t.Fatal("fresh LS60 information did not enable analog input")
	}
	oldGeneration := h.model.generation
	h.schedule(h.model.selectSpeaker(h.model.speakers[1], false))
	if h.ui.SupportsSource(kefw2.SourceAux) {
		t.Fatal("new speaker inherited previous speaker's input capabilities")
	}
	h.send(playerSessionMsg{generation: oldGeneration, message: playerInfoResult{
		speaker: playerSpeaker{Model: "LS60W"},
	}})
	h.info("LSXIILT")
	if h.ui.SupportsSource(kefw2.SourceAux) || !h.ui.SupportsSource(kefw2.SourceUSB) {
		t.Fatal("late old model information overrode the selected LSX II LT")
	}
}

func TestPlayerControlsUIExplicitSourceCanWakeStandby(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	standby := playerControlsUITestSnapshot(25)
	standby.Data.Power, standby.Data.Source = kefw2.SpeakerStatusStandby, kefw2.SourceStandby
	h.status(standby)
	generation := h.model.generation
	h.send(playerMuteRequest{generation: generation})
	if h.ui.State().err == nil {
		t.Fatal("mute from standby was not rejected")
	}
	h.send(playerVolumeKeyRequest{generation: generation, key: "-"})
	if h.ui.State().volumeErr == nil {
		t.Fatal("volume from standby was not rejected")
	}
	h.noCall(t)
	h.send(playerSourceRequest{generation: generation, source: kefw2.SourceWiFi})
	call := h.call(t, "source")
	if call.target.IPAddress != "a" || call.source != kefw2.SourceWiFi {
		t.Fatalf("wrong explicit source target: %+v", call)
	}
	awake := playerControlsUITestSnapshot(25)
	h.mu.Lock()
	h.snapshots["a"] = awake
	h.mu.Unlock()
	call.reply <- playerControlsUITestReply{snapshot: awake}
	h.until(t, func() bool { return !h.ui.State().pending })
	if state := h.ui.State(); state.err != nil || state.confirmed.Data.Source != kefw2.SourceWiFi {
		t.Fatalf("source readback not reflected: %+v", state)
	}
}

func TestPlayerControlsUISupportedPhysicalSourceUsesConfirmedReadback(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.info("LSXIILT")
	generation := h.model.generation
	h.send(playerSourceRequest{generation: generation, source: kefw2.SourceUSB})
	call := h.call(t, "source")
	if call.source != kefw2.SourceUSB || h.ui.State().confirmed.Data.Source != kefw2.SourceWiFi {
		t.Fatal("source selection was inferred instead of waiting for readback")
	}
	confirmed := playerControlsUITestSnapshot(25)
	confirmed.Data.Source, confirmed.Data.Playback = kefw2.SourceUSB, nil
	h.mu.Lock()
	h.snapshots["a"] = confirmed
	h.mu.Unlock()
	call.reply <- playerControlsUITestReply{snapshot: confirmed}
	h.until(t, func() bool { return !h.ui.State().pending })
	if state := h.ui.State(); state.err != nil || state.confirmed.Data.Source != kefw2.SourceUSB {
		t.Fatalf("confirmed physical source not displayed: %+v", state)
	}
}

func TestPlayerControlsUIBlocksUnavailableAndStaleControls(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	snapshot := playerControlsUITestSnapshot(25)
	snapshot.Player.Controls.Next = false
	h.status(snapshot)
	generation := h.model.generation
	h.send(playerTransportRequest{generation: generation, action: playerControlNext})
	if h.ui.State().err == nil || !strings.Contains(h.ui.State().err.Error(), "unavailable") {
		t.Fatalf("unavailable next control: %+v", h.ui.State())
	}
	h.send(playerSessionMsg{generation: generation, message: watchUpdate{
		kind: playerStatusUnavailableUpdate, err: errors.New("offline"),
	}})
	h.send(playerMuteRequest{generation: generation})
	if h.ui.State().ready || h.ui.State().err == nil {
		t.Fatalf("offline control was not visibly blocked: %+v", h.ui.State())
	}
	h.send(playerVolumeTargetRequest{generation: generation, target: 10})
	if h.ui.State().volumeErr == nil {
		t.Fatal("offline volume request was not visibly blocked")
	}
	h.noCall(t)
}

func TestPlayerControlsUIMaximumAndSlider(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(0, errors.New("maximum unavailable"))
	if state := h.ui.State(); state.maximum != nil || state.maximumErr == nil {
		t.Fatalf("failed maximum read was not reported: %+v", state)
	}
	h.maximum(101, nil)
	if state := h.ui.State(); state.maximum != nil || state.maximumErr == nil {
		t.Fatalf("invalid maximum accepted: %+v", state)
	}
	gen := h.model.generation
	h.send(playerVolumeKeyRequest{generation: gen, key: "+"})
	if h.ui.State().volumeErr == nil {
		t.Fatal("increase with unknown maximum accepted")
	}
	h.ui.syncVolume(h.ui.active.volume.State())
	if h.ui.State().volumeErr == nil {
		t.Fatal("unrelated volume notification erased the visible input error")
	}
	h.send(playerVolumeKeyRequest{generation: gen, key: "-"})
	call := h.call(t, "volume")
	if call.value != 24 {
		t.Fatalf("decrease with unknown maximum = %d, want 24", call.value)
	}
	call.reply <- playerControlsUITestReply{confirmed: 24}
	h.until(t, func() bool { return !h.ui.State().volumePending })
	h.maximum(0, nil)
	h.send(playerVolumeSliderRequest{generation: gen, x: 5, width: 0})
	if h.ui.State().volumeErr == nil {
		t.Fatal("zero-width slider accepted")
	}
	h.send(playerVolumeSliderRequest{generation: gen, x: 100, width: 1, release: true})
	receivePlayer(t, h.clock.scheduled)
	h.clock.advance(playerVolumeWriteInterval)
	call = h.call(t, "volume")
	if call.value != 0 {
		t.Fatalf("one-cell slider with maximum zero = %d", call.value)
	}
	call.reply <- playerControlsUITestReply{confirmed: 0}
	h.until(t, func() bool { return !h.ui.State().volumePending && h.ui.State().volume == 0 })
}

func TestPlayerControlsUINewMaximumErrorCannotBeOverwrittenByOldVolumeResult(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(80, nil)
	gen := h.model.generation
	h.send(playerVolumeTargetRequest{generation: gen, target: 30})
	first := h.call(t, "volume")
	first.reply <- playerControlsUITestReply{confirmed: 30, maximum: 80, maxRead: true}
	h.until(t, func() bool { return !h.ui.State().volumePending && h.ui.State().volume == 30 })
	h.maximum(101, nil)
	h.ui.syncVolume(h.ui.active.volume.State())
	if state := h.ui.State(); state.maximum != nil || state.maximumErr == nil {
		t.Fatalf("older maximum was restored over a newer invalid read: %+v", state)
	}
	h.send(playerVolumeTargetRequest{generation: gen, target: 20})
	if h.ui.State().maximum != nil {
		t.Fatal("starting a decrease restored an old maximum")
	}
	receivePlayer(t, h.clock.scheduled)
	h.clock.advance(playerVolumeWriteInterval)
	second := h.call(t, "volume")
	second.reply <- playerControlsUITestReply{confirmed: 20, maximum: 60, maxRead: true}
	h.until(t, func() bool { return !h.ui.State().volumePending && h.ui.State().volume == 20 })
	if state := h.ui.State(); state.maximum == nil || *state.maximum != 60 || state.maximumErr != nil {
		t.Fatalf("fresh validated maximum was not restored: %+v", state)
	}
}

func TestPlayerControlsUIRealVolumeReadbackAndStandbyGuard(t *testing.T) {
	fixture, speaker := newCLIFixture(t)
	h := newPlayerControlsUITestHarness(t)
	h.ui.backendFactory = func(target playerSpeaker) (playerControlsUIBackend, error) {
		return newPlayerControls(target.newSpeaker())
	}
	target := playerSpeaker{Name: speaker.Name, IPAddress: speaker.IPAddress}
	h.schedule(h.model.selectSpeaker(target, false))
	snapshot, err := playerReadStatus(context.Background(), &speaker)
	if err != nil {
		t.Fatal(err)
	}
	h.status(snapshot)
	h.maximum(80, nil)
	gen := h.model.generation
	h.send(playerVolumeSliderRequest{generation: gen, x: 100, width: 101, release: true})
	h.until(t, func() bool { return !h.ui.State().volumePending && h.ui.State().volume == 80 })
	if posts := fixture.postRequests(); len(posts) != 1 || posts[0].Path != "player:volume" {
		t.Fatalf("slider did not produce one confirmed volume write: %+v", posts)
	}
	fixture.mu.Lock()
	fixture.data["settings:/kef/play/physicalSource"] = `[{"type":"kefPhysicalSource","kefPhysicalSource":"standby"}]`
	fixture.data["settings:/kef/host/speakerStatus"] = `[{"type":"kefSpeakerStatus","kefSpeakerStatus":"standby"}]`
	fixture.mu.Unlock()
	h.send(playerVolumeKeyRequest{generation: gen, key: "-"})
	receivePlayer(t, h.clock.scheduled)
	h.clock.advance(playerVolumeWriteInterval)
	h.until(t, func() bool { return h.ui.State().volumeErr != nil })
	if posts := fixture.postRequests(); len(posts) != 1 {
		t.Fatalf("stale awake display caused a standby write: %+v", posts)
	}
}

func playerControlsUIUseRealBackend(t *testing.T, h *playerControlsUITestHarness, speaker kefw2.KEFSpeaker) {
	t.Helper()
	h.ui.backendFactory = func(target playerSpeaker) (playerControlsUIBackend, error) {
		return newPlayerControls(target.newSpeaker())
	}
	h.ui.readStatus = func(ctx context.Context, target playerSpeaker) (statusSnapshot, error) {
		return playerReadStatus(ctx, target.newSpeaker())
	}
	h.schedule(h.model.selectSpeaker(playerSpeaker{Name: speaker.Name, IPAddress: speaker.IPAddress}, false))
	snapshot, err := playerReadStatus(context.Background(), &speaker)
	if err != nil {
		t.Fatal(err)
	}
	h.status(snapshot)
	h.maximum(80, nil)
}

func TestPlayerControlsUIRealWritesRefreshSharedStatus(t *testing.T) {
	playerData := func(title string) string {
		return `[{"state":"playing","status":{"duration":180000},` +
			`"controls":{"next_":true,"previous":true,"pause":true},` +
			`"trackRoles":{"title":"` + title + `"}}]`
	}
	tests := []struct {
		name, path string
		request    func(uint64) tea.Msg
		change     func(*cliFixture)
		confirmed  func(statusSnapshot) bool
	}{
		{"next", "player:player/control", func(gen uint64) tea.Msg {
			return playerTransportRequest{generation: gen, action: playerControlNext}
		}, func(f *cliFixture) { f.data["player:player/data"] = playerData("New track") },
			func(s statusSnapshot) bool { return s.Data.Playback.Track.Title == "New track" }},
		{"seek", "player:player/control", func(gen uint64) tea.Msg {
			return playerSeekRequest{generation: gen, positionMS: 56789}
		}, func(f *cliFixture) { f.data["player:player/data/playTime"] = `[{"type":"i64_","i64_":56789}]` },
			func(s statusSnapshot) bool { return *s.Data.Playback.PositionMS == 56789 }},
		{"source", "settings:/kef/play/physicalSource", func(gen uint64) tea.Msg {
			return playerSourceRequest{generation: gen, source: kefw2.SourceBluetooth}
		}, nil, func(s statusSnapshot) bool { return s.Data.Source == kefw2.SourceBluetooth }},
		{"mute", "settings:/mediaPlayer/mute", func(gen uint64) tea.Msg {
			return playerMuteRequest{generation: gen}
		}, nil, func(s statusSnapshot) bool { return s.Data.Muted }},
		{"volume", "player:volume", func(gen uint64) tea.Msg {
			return playerVolumeTargetRequest{generation: gen, target: 40, release: true}
		}, nil, func(s statusSnapshot) bool { return s.Data.Volume == 40 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, speaker := newCLIFixture(t)
			fixture.mu.Lock()
			fixture.data["player:player/data"] = playerData("Old track")
			fixture.mu.Unlock()
			h := newPlayerControlsUITestHarness(t)
			playerControlsUIUseRealBackend(t, h, speaker)
			generation := h.model.generation
			session := h.factory.sessions[len(h.factory.sessions)-1]
			before := h.model.snapshot
			h.send(test.request(generation))
			h.until(t, func() bool {
				state := h.ui.State()
				return session.refreshes.Load() != 0 && !state.pending && !state.volumePending
			})
			if state := h.ui.State(); state.err != nil || state.volumeErr != nil {
				t.Fatalf("real mutation failed: %+v", state)
			}
			if h.model.snapshot != before {
				t.Fatal("HTTP acceptance was treated as a shared confirmed snapshot")
			}
			if posts := fixture.postRequests(); len(posts) != 1 || posts[0].Path != test.path {
				t.Fatalf("unexpected writes: %+v", posts)
			}
			if test.change != nil {
				fixture.mu.Lock()
				test.change(fixture)
				fixture.mu.Unlock()
			}
			fresh, err := playerReadStatus(context.Background(), &speaker)
			if err != nil {
				t.Fatal(err)
			}
			sequence := session.refreshes.Load()
			h.send(playerSessionMsg{generation: generation, message: playerStatusRefreshedResult{
				sequence: sequence, update: watchUpdate{kind: watchSnapshotUpdate, snapshot: fresh},
			}})
			if h.model.snapshot == before || !test.confirmed(*h.model.snapshot) {
				t.Fatalf("watcher confirmation did not update shared status: %+v", h.model.snapshot)
			}
			h.until(t, func() bool { return !h.ui.refreshing && !h.ui.refreshWanted })
			if session.refreshes.Load() != sequence || len(fixture.postRequests()) != 1 {
				t.Fatal("a confirmed refresh replayed the write or triggered a refresh loop")
			}
		})
	}
}

func TestPlayerControlsUIMuteMismatchRefreshesButPreflightDoesNot(t *testing.T) {
	fixture, speaker := newCLIFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.serveHTTP(w, r)
		if r.Method == http.MethodPost {
			fixture.mu.Lock()
			fixture.data["settings:/mediaPlayer/mute"] = `[{"type":"bool_","bool_":false}]`
			fixture.mu.Unlock()
		}
	}))
	t.Cleanup(server.Close)
	speaker.IPAddress = strings.TrimPrefix(server.URL, "http://")
	h := newPlayerControlsUITestHarness(t)
	playerControlsUIUseRealBackend(t, h, speaker)
	session := h.factory.sessions[len(h.factory.sessions)-1]
	h.send(playerMuteRequest{generation: h.model.generation})
	h.until(t, func() bool { return session.refreshes.Load() != 0 && h.ui.State().err != nil })
	var dispatched *playerMutationDispatchedError
	if !errors.As(h.ui.State().err, &dispatched) || h.ui.State().muted || h.ui.State().pending {
		t.Fatalf("unchanged mute readback was not classified as a dispatched failure: %+v", h.ui.State())
	}
	if posts := fixture.postRequests(); len(posts) != 1 || posts[0].Path != "settings:/mediaPlayer/mute" {
		t.Fatalf("expected one actual mute write: %+v", posts)
	}

	h.until(t, func() bool { return !h.ui.refreshing && !h.ui.refreshWanted })
	sequence := session.refreshes.Load()
	fixture.mu.Lock()
	fixture.data["settings:/kef/play/physicalSource"] = `[{"type":"kefPhysicalSource","kefPhysicalSource":"standby"}]`
	fixture.data["settings:/kef/host/speakerStatus"] = `[{"type":"kefSpeakerStatus","kefSpeakerStatus":"standby"}]`
	fixture.mu.Unlock()
	h.send(playerMuteRequest{generation: h.model.generation})
	h.until(t, func() bool {
		return !h.ui.State().pending && h.ui.State().err != nil &&
			!errors.As(h.ui.State().err, &dispatched)
	})
	if session.refreshes.Load() != sequence || len(fixture.postRequests()) != 1 {
		t.Fatal("an unsent standby preflight failure requested a refresh or wrote to the speaker")
	}
}

func TestPlayerControlsUIUnsentVolumeDoesNotRefresh(t *testing.T) {
	for _, standby := range []bool{false, true} {
		name := "confirmed no-op"
		if standby {
			name = "standby preflight"
		}
		t.Run(name, func(t *testing.T) {
			fixture, speaker := newCLIFixture(t)
			h := newPlayerControlsUITestHarness(t)
			playerControlsUIUseRealBackend(t, h, speaker)
			target := 25
			if standby {
				target = 30
				fixture.mu.Lock()
				fixture.data["settings:/kef/play/physicalSource"] =
					`[{"type":"kefPhysicalSource","kefPhysicalSource":"standby"}]`
				fixture.data["settings:/kef/host/speakerStatus"] =
					`[{"type":"kefSpeakerStatus","kefSpeakerStatus":"standby"}]`
				fixture.mu.Unlock()
			}
			session := h.factory.sessions[len(h.factory.sessions)-1]
			h.send(playerVolumeTargetRequest{generation: h.model.generation, target: target})
			h.until(t, func() bool {
				state := h.ui.State()
				return !state.volumePending && (!standby || state.volumeErr != nil)
			})
			if session.refreshes.Load() != 0 || len(fixture.postRequests()) != 0 {
				t.Fatal("an unsent volume operation refreshed the watcher or wrote to the speaker")
			}
		})
	}
}

func TestPlayerControlsUIUncertainWritesRefreshSharedStatus(t *testing.T) {
	tests := []struct {
		name    string
		request func(uint64) tea.Msg
		volume  bool
	}{
		{"mute", func(gen uint64) tea.Msg { return playerMuteRequest{generation: gen} }, false},
		{"source", func(gen uint64) tea.Msg {
			return playerSourceRequest{generation: gen, source: kefw2.SourceBluetooth}
		}, false},
		{"volume", func(gen uint64) tea.Msg {
			return playerVolumeTargetRequest{generation: gen, target: 40, release: true}
		}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, speaker := newCLIFixture(t)
			h := newPlayerControlsUITestHarness(t)
			playerControlsUIUseRealBackend(t, h, speaker)
			fixture.mu.Lock()
			fixture.postFail = true
			fixture.mu.Unlock()
			session := h.factory.sessions[len(h.factory.sessions)-1]
			before := h.model.snapshot
			h.send(test.request(h.model.generation))
			h.until(t, func() bool {
				state := h.ui.State()
				failed := state.err != nil
				if test.volume {
					failed = state.volumeErr != nil && state.volumeUncertain
				}
				return session.refreshes.Load() != 0 && failed
			})
			if h.model.snapshot != before || len(fixture.postRequests()) != 1 {
				t.Fatal("uncertain write was replayed or optimistically marked confirmed")
			}
		})
	}
}

func TestPlayerControlsUIRefreshesDispatchedWritesAfterEventLoss(t *testing.T) {
	for _, volume := range []bool{false, true} {
		name := "mute"
		if volume {
			name = "volume"
		}
		t.Run(name, func(t *testing.T) {
			h := newPlayerControlsUITestHarness(t)
			h.status(playerControlsUITestSnapshot(25))
			h.maximum(80, nil)
			generation := h.model.generation
			session, controls := h.factory.sessions[0], h.ui.active
			var actionDone <-chan struct{}
			if volume {
				h.send(playerVolumeTargetRequest{generation: generation, target: 40})
				h.call(t, "volume")
			} else {
				h.send(playerMuteRequest{generation: generation})
				actionDone = controls.workers[len(controls.workers)-1]
				h.call(t, "mute")
			}
			h.event(errors.New("events disconnected"))
			if volume {
				receivePlayer(t, controls.volume.Done())
			} else {
				receivePlayer(t, actionDone)
			}
			h.until(t, func() bool { return session.refreshes.Load() != 0 })
			if h.model.generation != generation || h.ui.State().ready {
				t.Fatal("a refresh request changed the generation or restored readiness without status")
			}
			h.noCall(t)
		})
	}
}

func TestPlayerControlsUIRetiredGenerationCannotRefreshNewSpeaker(t *testing.T) {
	for _, volume := range []bool{false, true} {
		name := "mute"
		if volume {
			name = "volume"
		}
		t.Run(name, func(t *testing.T) {
			h := newPlayerControlsUITestHarness(t)
			h.status(playerControlsUITestSnapshot(25))
			h.maximum(80, nil)
			oldGeneration, old := h.model.generation, h.ui.active
			var actionDone <-chan struct{}
			if volume {
				h.send(playerVolumeTargetRequest{generation: oldGeneration, target: 40})
				h.call(t, "volume")
			} else {
				h.send(playerMuteRequest{generation: oldGeneration})
				actionDone = old.workers[len(old.workers)-1]
				h.call(t, "mute")
			}
			h.schedule(h.model.selectSpeaker(h.model.speakers[1], false))
			if volume {
				receivePlayer(t, old.volume.Done())
			} else {
				receivePlayer(t, actionDone)
			}
			waitPlayerControlsScopedMessage(t, h, oldGeneration, func(message tea.Msg) bool {
				if volume {
					_, ok := message.(playerControlsVolumeRefresh)
					return ok
				}
				_, ok := message.(playerStatusRefreshRequest)
				return ok
			})
			current := h.factory.sessions[len(h.factory.sessions)-1]
			if h.model.generation == oldGeneration || current.refreshes.Load() != 0 {
				t.Fatal("retired generation refreshed the newly selected speaker")
			}
			h.noCall(t)
		})
	}
}

func TestPlayerControlsUIGateCanceledWritesDoNotRefresh(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(80, nil)
	generation, old := h.model.generation, h.ui.active
	blocker := h.model.mutations.reserve()
	if err := blocker.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(blocker.release)
	h.send(playerMuteRequest{generation: generation})
	actionDone := old.workers[len(old.workers)-1]
	h.send(playerVolumeTargetRequest{generation: generation, target: 40})
	h.until(t, func() bool { return h.ui.State().volumeInFlight })
	h.event(errors.New("events disconnected"))
	receivePlayer(t, actionDone)
	receivePlayer(t, old.volume.Done())
	blocker.release()
	waitPlayerControlsScopedMessage(t, h, generation, func(message tea.Msg) bool {
		_, ok := message.(playerControlsActionResult)
		return ok
	})
	if h.factory.sessions[0].refreshes.Load() != 0 {
		t.Fatal("undispatched gate-canceled writes requested a watcher refresh")
	}
	h.noCall(t)
}

func waitPlayerControlsScopedMessage(
	t *testing.T, h *playerControlsUITestHarness, generation uint64, match func(tea.Msg) bool,
) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case message := <-h.events:
			scoped, ok := message.(playerScopedMsg)
			matched := ok && scoped.generation == generation && match(scoped.message)
			if notice, ok := message.(playerNoticeMsg); ok {
				h.notices = append(h.notices, notice)
			}
			h.sendRaw(message)
			if matched {
				return
			}
		case <-timer.C:
			t.Fatal("timed out waiting for scoped controls result")
		}
	}
}

func TestPlayerControlsUISliderFlushAndKeyboard(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(60, nil)
	gen := h.model.generation
	h.send(playerVolumeSliderRequest{generation: gen, x: -3, width: 11})
	first := h.call(t, "volume")
	if first.value != 0 {
		t.Fatalf("left endpoint = %d", first.value)
	}
	h.send(playerVolumeSliderRequest{generation: gen, x: 5, width: 11})
	h.send(playerVolumeSliderRequest{generation: gen, x: 99, width: 11, release: true})
	if state := h.ui.State(); !state.volumePending || state.sliderTarget != 60 || state.volume != 25 {
		t.Fatalf("pending target or confirmed volume incorrect: %+v", state)
	}
	h.noCall(t)
	first.reply <- playerControlsUITestReply{confirmed: 0}
	receivePlayer(t, h.clock.scheduled)
	h.clock.advance(playerVolumeWriteInterval)
	final := h.call(t, "volume")
	if final.value != 60 {
		t.Fatalf("release target = %d, want 60", final.value)
	}
	final.reply <- playerControlsUITestReply{confirmed: 60}
	h.until(t, func() bool { return !h.ui.State().volumePending && h.ui.State().volume == 60 })
	h.send(playerVolumeKeyRequest{generation: gen, key: watchKeyLeft})
	receivePlayer(t, h.clock.scheduled)
	h.clock.advance(playerVolumeWriteInterval)
	keyboard := h.call(t, "volume")
	if keyboard.value != 59 {
		t.Fatalf("keyboard step = %d, want 59", keyboard.value)
	}
	keyboard.reply <- playerControlsUITestReply{confirmed: 59}
	h.until(t, func() bool { return !h.ui.State().volumePending && h.ui.State().volume == 59 })
}

func TestPlayerControlsUIVolumeFailureRequiresRetry(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(80, nil)
	gen := h.model.generation
	h.send(playerVolumeTargetRequest{generation: gen, target: 40})
	call := h.call(t, "volume")
	call.reply <- playerControlsUITestReply{err: errors.New("write failed"), uncertain: true}
	h.until(t, func() bool { return h.ui.State().volumeErr != nil && h.ui.State().volumeCanRetry })
	state := h.ui.State()
	if !state.volumeUncertain || state.volume != 25 || state.volumeRetryTarget != 40 {
		t.Fatalf("failure not represented correctly: %+v", state)
	}
	h.send(playerVolumeTargetRequest{generation: gen, target: 60})
	h.noCall(t)
	h.send(playerVolumeRetryRequest{generation: gen})
	receivePlayer(t, h.clock.scheduled)
	h.clock.advance(playerVolumeWriteInterval)
	retry := h.call(t, "volume")
	if retry.value != 40 || h.ui.State().volumeErr == nil {
		t.Fatalf("retry was changed or error cleared prematurely: %+v", h.ui.State())
	}
	retry.reply <- playerControlsUITestReply{confirmed: 40}
	h.until(t, func() bool { return !h.ui.State().volumePending && h.ui.State().volume == 40 })
	if state := h.ui.State(); state.volumeErr != nil || state.volumeUncertain {
		t.Fatalf("confirmed retry did not clear failure: %+v", state)
	}
}

func TestPlayerControlsUIRetiresUnsentVolume(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(100, nil)
	gen := h.model.generation
	old := h.ui.active
	h.send(playerVolumeTargetRequest{generation: gen, target: 30})
	first := h.call(t, "volume")
	first.reply <- playerControlsUITestReply{confirmed: 30}
	h.until(t, func() bool { return !h.ui.State().volumePending && h.ui.State().volume == 30 })
	h.send(playerVolumeTargetRequest{generation: gen, target: 70, release: true})
	receivePlayer(t, h.clock.scheduled)
	h.schedule(h.model.selectSpeaker(h.model.speakers[1], false))
	receivePlayer(t, old.volume.Done())
	if notice := waitPlayerControlsVolumeNotice(old)(); notice != nil {
		t.Fatalf("unsent volume produced uncertainty: %+v", notice)
	}
	h.clock.advance(time.Second)
	h.noCall(t)
	if state := h.ui.State(); state.volumeKnown || state.volumePending || state.sliderTarget != 0 {
		t.Fatalf("retired intent leaked to new speaker: %+v", state)
	}
}

func TestPlayerControlsUIRetiredAcceptedVolumeNotifiesOldTarget(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(100, nil)
	gen := h.model.generation
	old := h.ui.active
	h.send(playerVolumeTargetRequest{generation: gen, target: 30})
	h.call(t, "volume")
	h.send(playerVolumeTargetRequest{generation: gen, target: 70, release: true})
	h.schedule(h.model.selectSpeaker(h.model.speakers[1], false))
	receivePlayer(t, old.volume.Done())
	notice, ok := waitPlayerControlsVolumeNotice(old)().(playerNoticeMsg)
	if !ok || notice.target.IPAddress != "a" || notice.target.Name != "Living" ||
		notice.action != "set volume" || notice.err == nil {
		t.Fatalf("retired volume uncertainty missing: %+v", notice)
	}
	h.status(playerControlsUITestSnapshot(12))
	h.maximum(40, nil)
	h.clock.advance(time.Second)
	h.noCall(t)
	if state := h.ui.State(); state.volume != 12 || state.volumePending || state.sliderTarget != 12 {
		t.Fatalf("old write affected new speaker presentation: %+v", state)
	}
}

func TestPlayerControlsUIGenerationABARejectsOldRequestsAndResults(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(100, nil)
	firstGeneration := h.model.generation
	first := h.ui.active
	h.send(playerMuteRequest{generation: firstGeneration})
	h.call(t, "mute")
	h.schedule(h.model.selectSpeaker(h.model.speakers[1], false))
	secondGeneration := h.model.generation
	h.send(playerSessionDoneMsg{generation: firstGeneration})
	h.schedule(h.model.selectSpeaker(h.model.speakers[0], false))
	if h.model.generation == firstGeneration || h.model.generation == secondGeneration {
		t.Fatal("A→B→A reused a generation")
	}
	h.status(playerControlsUITestSnapshot(9))
	h.maximum(45, nil)
	h.send(playerVolumeKeyRequest{generation: firstGeneration, key: "+"})
	h.send(playerMuteRequest{generation: firstGeneration})
	h.send(playerScopedMsg{generation: firstGeneration, message: playerControlsActionResult{
		sessionID: first.id, actionID: 1, muted: new(true),
	}})
	h.noCall(t)
	if state := h.ui.State(); state.volume != 9 || state.muted || state.pending {
		t.Fatalf("retired A result leaked into current A: %+v", state)
	}
	h.until(t, func() bool { return len(h.notices) != 0 })
	if notice := h.notices[0]; notice.target.IPAddress != "a" || notice.err == nil {
		t.Fatalf("old A uncertainty lost target: %+v", notice)
	}
}

func TestPlayerControlsUIReadFailureRetiresWorkerAndRecovers(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(100, nil)
	gen := h.model.generation
	old := h.ui.active
	h.send(playerVolumeTargetRequest{generation: gen, target: 50})
	h.call(t, "volume")
	h.send(playerSessionMsg{generation: gen, message: watchUpdate{
		kind: playerStatusUnavailableUpdate, err: errors.New("offline"),
	}})
	receivePlayer(t, old.volume.Done())
	h.status(playerControlsUITestSnapshot(14))
	if h.ui.active == nil || h.ui.active.id == old.id || !h.ui.State().ready {
		t.Fatal("fresh status did not start a new worker incarnation")
	}
	h.send(playerScopedMsg{generation: gen, message: playerControlsVolumeChanged{
		sessionID: old.id, state: playerVolumeState{Confirmed: 99, Target: 99, Revision: 999},
	}})
	h.noCall(t)
	if state := h.ui.State(); state.volume != 14 || state.sliderTarget != 14 {
		t.Fatalf("old same-generation worker overwrote recovery: %+v", state)
	}
}

func TestPlayerControlsUIEventDisconnectDropsQueuedWrites(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(100, nil)
	gen := h.model.generation
	old := h.ui.active
	h.event(nil)
	if h.ui.active != old || !h.ui.State().ready {
		t.Fatal("healthy event update suspended a fresh session")
	}
	blocker := h.model.mutations.reserve()
	if err := blocker.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(blocker.release)
	h.send(playerMuteRequest{generation: gen})
	actionDone := old.workers[len(old.workers)-1]
	h.send(playerVolumeTargetRequest{generation: gen, target: 70, release: true})
	h.until(t, func() bool { return h.ui.State().volumeInFlight })
	h.event(errors.New("events disconnected"))
	if h.ui.active != nil || h.ui.State().ready || h.ui.State().volumePending {
		t.Fatalf("event loss left controls active: %+v", h.ui.State())
	}
	receivePlayer(t, actionDone)
	receivePlayer(t, old.volume.Done())
	blocker.release()
	if notice := waitPlayerControlsVolumeNotice(old)(); notice != nil {
		t.Fatalf("undispatched volume was reported as uncertain: %+v", notice)
	}
	h.event(nil)
	h.send(playerSessionMsg{generation: gen, message: watchUpdate{
		kind: watchSnapshotUpdate, snapshot: playerControlsUITestSnapshot(10), err: errors.New("partial status"),
	}})
	h.send(playerMuteRequest{generation: gen})
	h.send(playerVolumeKeyRequest{generation: gen, key: "-"})
	if h.ui.active != nil || h.ui.State().ready {
		t.Fatal("healthy event or failed snapshot re-enabled controls without a fresh snapshot")
	}
	h.noCall(t)
	h.status(playerControlsUITestSnapshot(14))
	if h.ui.active == nil || h.ui.active.id == old.id || !h.ui.State().ready {
		t.Fatal("fresh status did not start a new controls worker")
	}
	h.noCall(t)
	h.send(playerMuteRequest{generation: gen})
	mute := h.call(t, "mute")
	mute.reply <- playerControlsUITestReply{muted: true}
	h.until(t, func() bool { return !h.ui.State().pending })
}

func TestPlayerControlsUIEventDisconnectNoticesDispatchedAction(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	gen := h.model.generation
	old := h.ui.active
	h.send(playerMuteRequest{generation: gen})
	h.call(t, "mute")
	actionDone := old.workers[len(old.workers)-1]
	h.event(errors.New("events disconnected"))
	receivePlayer(t, actionDone)
	h.event(nil)
	if h.ui.State().ready {
		t.Fatal("event reconnection alone re-enabled the action")
	}
	h.status(playerControlsUITestSnapshot(14))
	h.send(playerScopedMsg{generation: gen, message: playerControlsActionResult{
		sessionID: old.id, actionID: 1, muted: new(true),
	}})
	if state := h.ui.State(); !state.ready || state.muted || state.pending {
		t.Fatalf("retired action overwrote recovered state: %+v", state)
	}
	h.schedule(h.model.selectSpeaker(h.model.speakers[1], false))
	h.status(playerControlsUITestSnapshot(12))
	h.until(t, func() bool { return len(h.notices) != 0 })
	notice := h.notices[0]
	if notice.target.IPAddress != "a" || notice.target.Name != "Living" ||
		notice.action != "mute" || notice.err == nil {
		t.Fatalf("lost dispatched action uncertainty or its target: %+v", notice)
	}
	if state := h.ui.State(); state.volume != 12 || state.muted || state.pending {
		t.Fatalf("retired action affected the selected speaker: %+v", state)
	}
	h.noCall(t)
}

func TestPlayerControlsUIEventDisconnectNoticesDispatchedVolume(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(100, nil)
	gen := h.model.generation
	old := h.ui.active
	h.send(playerVolumeTargetRequest{generation: gen, target: 30})
	h.call(t, "volume")
	h.send(playerVolumeTargetRequest{generation: gen, target: 70, release: true})
	h.event(errors.New("events disconnected"))
	receivePlayer(t, old.volume.Done())
	h.until(t, func() bool { return len(h.notices) != 0 })
	notice := h.notices[0]
	if notice.target.IPAddress != "a" || notice.target.Name != "Living" ||
		notice.action != "set volume" || notice.err == nil {
		t.Fatalf("lost dispatched volume uncertainty or its target: %+v", notice)
	}
	h.event(nil)
	if h.ui.State().ready {
		t.Fatal("event reconnection alone re-enabled volume")
	}
	h.status(playerControlsUITestSnapshot(12))
	h.clock.advance(time.Second)
	h.noCall(t)
	if state := h.ui.State(); !state.ready || state.volume != 12 ||
		state.volumePending || state.sliderTarget != 12 {
		t.Fatalf("retired volume intent leaked into recovery: %+v", state)
	}
}

func TestPlayerControlsUIFreshPollRestoresWhileEventsRemainDisconnected(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	old := h.ui.active
	h.event(errors.New("events disconnected"))
	h.status(playerControlsUITestSnapshot(14))
	if h.model.eventErr == nil || h.ui.active == nil || h.ui.active.id == old.id || !h.ui.State().ready {
		t.Fatal("fresh polled status did not restore readiness while events remained disconnected")
	}
	h.send(playerMuteRequest{generation: h.model.generation})
	mute := h.call(t, "mute")
	mute.reply <- playerControlsUITestReply{muted: true}
	h.until(t, func() bool { return !h.ui.State().pending })
}

func TestPlayerControlsUIReadinessEpochRejectsStaleRequests(t *testing.T) {
	tests := []struct {
		name      string
		interrupt func(*playerControlsUITestHarness)
	}{
		{"event disconnect", func(h *playerControlsUITestHarness) {
			h.event(errors.New("events disconnected"))
		}},
		{"status unavailable", func(h *playerControlsUITestHarness) {
			h.send(playerSessionMsg{generation: h.model.generation, message: watchUpdate{
				kind: playerStatusUnavailableUpdate, err: errors.New("status unavailable"),
			}})
		}},
		{"failed snapshot", func(h *playerControlsUITestHarness) {
			h.send(playerSessionMsg{generation: h.model.generation, message: watchUpdate{
				kind: watchSnapshotUpdate, snapshot: playerControlsUITestSnapshot(25), err: errors.New("partial"),
			}})
		}},
		{"invalid snapshot", func(h *playerControlsUITestHarness) {
			h.status(playerControlsUITestSnapshot(-1))
		}},
		{"session failure", func(h *playerControlsUITestHarness) {
			h.send(playerSessionMsg{generation: h.model.generation,
				message: playerSessionFailure{err: errors.New("session failed")}})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newPlayerControlsUITestHarness(t)
			h.status(playerControlsUITestSnapshot(25))
			h.maximum(100, nil)
			generation, before := h.model.generation, h.ui.State().epoch
			test.interrupt(h)
			offline := h.ui.State().epoch
			if before == 0 || offline == before || h.ui.State().ready {
				t.Fatalf("readiness loss did not retire the previous epoch: before %d, state %+v",
					before, h.ui.State())
			}
			h.event(nil)
			if h.ui.State().ready || h.ui.State().epoch != offline {
				t.Fatal("event reconnection alone changed write readiness")
			}
			h.status(playerControlsUITestSnapshot(14))
			current := h.ui.State().epoch
			if h.model.generation != generation || !h.ui.State().ready ||
				current == before || current == offline || current == 0 {
				t.Fatalf("fresh status did not establish a new same-generation epoch: %+v", h.ui.State())
			}

			h.send(playerSourceRequest{generation: generation, source: kefw2.SourceAux})
			h.send(playerVolumeKeyRequest{generation: generation, key: "invalid"})
			controlErr, volumeErr := h.ui.State().err, h.ui.State().volumeErr
			if controlErr == nil || volumeErr == nil {
				t.Fatal("could not establish errors for stale dismiss/retry checks")
			}
			assertPlayerControlsUIStaleRequests(t, h, generation, []uint64{before, offline, 0}, controlErr, volumeErr)
			h.noCall(t)
			h.sendRaw(playerMuteRequest{generation: generation, epoch: current})
			call := h.call(t, "mute")
			call.reply <- playerControlsUITestReply{muted: true}
			h.until(t, func() bool { return !h.ui.State().pending && h.ui.State().muted })
		})
	}
}

func assertPlayerControlsUIStaleRequests(
	t *testing.T, h *playerControlsUITestHarness, generation uint64, epochs []uint64, controlErr, volumeErr error,
) {
	t.Helper()
	for _, epoch := range epochs {
		for _, message := range []tea.Msg{
			playerTransportRequest{generation: generation, epoch: epoch, action: playerControlNext},
			playerMuteRequest{generation: generation, epoch: epoch},
			playerSourceRequest{generation: generation, epoch: epoch, source: kefw2.SourceBluetooth},
			playerSeekRequest{generation: generation, epoch: epoch, positionMS: 2000},
			playerVolumeTargetRequest{generation: generation, epoch: epoch, target: 40, release: true},
			playerVolumeSliderRequest{generation: generation, epoch: epoch, x: 8, width: 10, release: true},
			playerVolumeKeyRequest{generation: generation, epoch: epoch, key: "+"},
			playerVolumeRetryRequest{generation: generation, epoch: epoch},
			playerVolumeDismissRequest{generation: generation, epoch: epoch},
			playerControlDismissErrorRequest{generation: generation, epoch: epoch},
		} {
			h.sendRaw(message)
			state := h.ui.State()
			if state.pending || state.volumePending ||
				!errors.Is(state.err, controlErr) || !errors.Is(state.volumeErr, volumeErr) {
				t.Fatalf("stale %T at epoch %d changed controls: %+v", message, epoch, state)
			}
		}
	}
}

func TestPlayerControlsUIInitialAndStableReadinessEpoch(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	generation, connecting := h.model.generation, h.ui.State().epoch
	h.status(playerControlsUITestSnapshot(25))
	ready := h.ui.State().epoch
	if ready == 0 || ready == connecting {
		t.Fatalf("initial status did not establish a new epoch: connecting %d, ready %d", connecting, ready)
	}
	h.sendRaw(playerMuteRequest{generation: generation, epoch: connecting})
	h.noCall(t)
	if h.ui.State().pending {
		t.Fatal("connecting-time request became executable after initial status")
	}

	h.event(nil)
	h.status(playerControlsUITestSnapshot(25))
	if h.ui.State().epoch != ready {
		t.Fatal("healthy updates invalidated an already-ready request")
	}
	h.sendRaw(playerMuteRequest{generation: generation, epoch: ready})
	call := h.call(t, "mute")
	call.reply <- playerControlsUITestReply{muted: true}
	h.until(t, func() bool { return !h.ui.State().pending && h.ui.State().muted })
}

func TestPlayerControlsUIErrorsCanBeDismissedWhileOffline(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	generation := h.model.generation
	h.send(playerSourceRequest{generation: generation, source: kefw2.SourceAux})
	h.send(playerVolumeKeyRequest{generation: generation, key: "invalid"})
	h.event(errors.New("events disconnected"))
	offline := h.ui.State().epoch
	if h.ui.State().err == nil || h.ui.State().volumeErr == nil {
		t.Fatal("expected errors were not retained while offline")
	}
	h.sendRaw(playerControlDismissErrorRequest{generation: generation, epoch: offline})
	h.sendRaw(playerVolumeDismissRequest{generation: generation, epoch: offline})
	if state := h.ui.State(); state.err != nil || state.volumeErr != nil || state.ready {
		t.Fatalf("current offline dismissal failed or restored readiness: %+v", state)
	}
	h.noCall(t)
}

func TestPlayerControlsUICanceledSessionCannotBecomeReady(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.model.session.cancel()
	h.status(playerControlsUITestSnapshot(12))
	if state := h.ui.State(); state.ready || h.ui.active != nil {
		t.Fatalf("a stale snapshot revived canceled session controls: %+v", state)
	}
	h.send(playerMuteRequest{generation: h.model.generation})
	h.noCall(t)
}

func TestPlayerControlsUIInvalidSnapshotCancelsPendingWrite(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.send(playerMuteRequest{generation: h.model.generation})
	h.call(t, "mute")
	old := h.ui.active
	done := old.workers[len(old.workers)-1]
	h.status(playerControlsUITestSnapshot(101))
	receivePlayer(t, done)
	if state := h.ui.State(); state.ready || state.pending || state.err == nil ||
		!strings.Contains(state.err.Error(), "invalid volume") {
		t.Fatalf("invalid status did not suspend pending controls: %+v", state)
	}
	h.status(playerControlsUITestSnapshot(12))
	if state := h.ui.State(); !state.ready || state.pending || state.volume != 12 {
		t.Fatalf("fresh valid status did not restore controls: %+v", state)
	}
	h.noCall(t)
}

type playerControlsUITestRead struct {
	reply chan statusSnapshot
}

func TestPlayerControlsUIStaleReadCannotOverwriteNewerVolumeOrMute(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	old := playerControlsUITestSnapshot(25)
	h.status(old)
	h.maximum(100, nil)
	reads := make(chan playerControlsUITestRead, 2)
	h.ui.readStatus = func(ctx context.Context, _ playerSpeaker) (statusSnapshot, error) {
		call := playerControlsUITestRead{reply: make(chan statusSnapshot, 1)}
		select {
		case reads <- call:
		case <-ctx.Done():
			return statusSnapshot{}, ctx.Err()
		}
		select {
		case snapshot := <-call.reply:
			return snapshot, nil
		case <-ctx.Done():
			return statusSnapshot{}, ctx.Err()
		}
	}
	gen := h.model.generation
	h.send(playerMuteRequest{generation: gen})
	mute := h.call(t, "mute")
	mute.reply <- playerControlsUITestReply{muted: true}
	h.until(t, func() bool { return !h.ui.State().pending })
	firstRead := receivePlayer(t, reads)
	h.send(playerVolumeTargetRequest{generation: gen, target: 40})
	volume := h.call(t, "volume")
	firstRead.reply <- old
	h.until(t, func() bool { return !h.ui.refreshing })
	if state := h.ui.State(); !state.muted || !state.volumePending || state.sliderTarget != 40 {
		t.Fatalf("older status overwrote a newer mutation: %+v", state)
	}
	volume.reply <- playerControlsUITestReply{confirmed: 40}
	h.until(t, func() bool { return !h.ui.State().volumePending && h.ui.State().volume == 40 })
	secondRead := receivePlayer(t, reads)
	fresh := playerControlsUITestSnapshot(40)
	fresh.Data.Muted = true
	secondRead.reply <- fresh
	h.until(t, func() bool { return !h.ui.refreshing && !h.ui.refreshWanted })
	if state := h.ui.State(); state.volume != 40 || !state.muted || state.confirmed.Data.Volume != 40 {
		t.Fatalf("fresh confirmation was not applied: %+v", state)
	}
}

func TestPlayerControlsUIWatcherSnapshotSupersedesInFlightRead(t *testing.T) {
	tests := []struct {
		name, title string
		err         error
	}{
		{"changed track stale read", "New track", nil},
		{"changed track stale failure", "New track", errors.New("old read failed")},
		{"unchanged track stale read", "Old track", nil},
		{"unchanged track stale failure", "Old track", errors.New("old read failed")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newPlayerControlsUITestHarness(t)
			old := playerControlsUITestSnapshot(25)
			old.Data.Playback.Track = &trackStatus{Title: "Old track"}
			h.status(old)
			reads := make(chan chan playerControlsUITestReply, 2)
			h.ui.readStatus = func(ctx context.Context, _ playerSpeaker) (statusSnapshot, error) {
				reply := make(chan playerControlsUITestReply, 1)
				select {
				case reads <- reply:
				case <-ctx.Done():
					return statusSnapshot{}, ctx.Err()
				}
				select {
				case result := <-reply:
					return result.snapshot, result.err
				case <-ctx.Done():
					return statusSnapshot{}, ctx.Err()
				}
			}
			generation := h.model.generation
			h.send(playerMuteRequest{generation: generation})
			mute := h.call(t, "mute")
			mute.reply <- playerControlsUITestReply{muted: true}
			h.until(t, func() bool { return !h.ui.State().pending && h.ui.refreshing })
			first := receivePlayer(t, reads)

			// A status read requested after an external playback action arrives without an event.
			fresh := playerControlsUITestSnapshot(25)
			fresh.Data.Muted = true
			fresh.Data.Playback.Track = &trackStatus{Title: test.title}
			sequence := h.model.requestStatusRefresh(generation)
			if sequence == 0 {
				t.Fatal("could not request a scoped status refresh")
			}
			h.send(playerSessionMsg{generation: generation, message: playerStatusRefreshedResult{
				sequence: sequence, update: watchUpdate{kind: watchSnapshotUpdate, snapshot: fresh},
			}})
			first <- playerControlsUITestReply{snapshot: old, err: test.err}
			h.until(t, func() bool { return !h.ui.refreshWanted })
			state := h.ui.State()
			if !state.ready || !state.muted || state.confirmed.Data.Playback.Track.Title != "Old track" {
				t.Fatalf("older read or failure overwrote newer confirmation: %+v", state)
			}
			second := receivePlayer(t, reads)
			second <- playerControlsUITestReply{snapshot: fresh}
			h.until(t, func() bool {
				state := h.ui.State()
				return !h.ui.refreshing && state.confirmed.Data.Playback.Track.Title == test.title
			})
		})
	}
}

func TestPlayerControlsUIMutationGateOrdersTransactions(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(100, nil)
	gate := h.model.mutations
	blocker := gate.reserve()
	if err := blocker.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	gen := h.model.generation
	h.send(playerMuteRequest{generation: gen})
	h.send(playerVolumeTargetRequest{generation: gen, target: 40})
	h.noCall(t)
	blocker.release()
	mute := h.call(t, "mute")
	h.noCall(t)
	mute.reply <- playerControlsUITestReply{muted: true}
	volume := h.call(t, "volume")
	if volume.value != 40 {
		t.Fatalf("volume behind mute = %d", volume.value)
	}
	volume.reply <- playerControlsUITestReply{confirmed: 40}
	h.until(t, func() bool { return !h.ui.State().volumePending && h.ui.State().volume == 40 })
}

func TestPlayerControlsUIGateWaitCoalescesUnsentVolume(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(100, nil)
	blocker := h.model.mutations.reserve()
	if err := blocker.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	gen := h.model.generation
	h.send(playerVolumeTargetRequest{generation: gen, target: 20})
	h.until(t, func() bool { return h.ui.State().volumeInFlight })
	h.send(playerVolumeTargetRequest{generation: gen, target: 30, release: true})
	blocker.release()
	call := h.call(t, "volume")
	if call.value != 30 {
		t.Fatalf("gate sent stale volume %d, want final target 30", call.value)
	}
	call.reply <- playerControlsUITestReply{confirmed: 30}
	h.until(t, func() bool { return !h.ui.State().volumePending && h.ui.State().volume == 30 })
	h.clock.advance(time.Second)
	h.noCall(t)
}

func TestPlayerControlsUIGateReplacementStillFlushesLaterRelease(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(100, nil)
	blocker := h.model.mutations.reserve()
	if err := blocker.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	gen := h.model.generation
	h.send(playerVolumeTargetRequest{generation: gen, target: 20})
	h.until(t, func() bool { return h.ui.State().volumeInFlight })
	h.send(playerVolumeTargetRequest{generation: gen, target: 30})
	blocker.release()
	replaced := h.call(t, "volume")
	if replaced.value != 30 {
		t.Fatalf("gate sent %d instead of 30", replaced.value)
	}
	h.send(playerVolumeTargetRequest{generation: gen, target: 20, release: true})
	replaced.reply <- playerControlsUITestReply{confirmed: 30}
	h.until(t, func() bool {
		state := h.ui.State()
		return state.volume == 30 && state.volumePending && state.sliderTarget == 20
	})
	receivePlayer(t, h.clock.scheduled)
	h.clock.advance(playerVolumeWriteInterval)
	final := h.call(t, "volume")
	if final.value != 20 {
		t.Fatalf("later release sent %d instead of 20", final.value)
	}
	final.reply <- playerControlsUITestReply{confirmed: 20}
	h.until(t, func() bool { return !h.ui.State().volumePending && h.ui.State().volume == 20 })
	h.noCall(t)
}

func TestPlayerControlsUIGateVolumeClampsAfterMaximumDrops(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(10))
	h.maximum(100, nil)
	blocker := h.model.mutations.reserve()
	if err := blocker.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	gen := h.model.generation
	h.send(playerVolumeTargetRequest{generation: gen, target: 20})
	h.until(t, func() bool { return h.ui.State().volumeInFlight })
	h.send(playerVolumeTargetRequest{generation: gen, target: 30})
	blocker.release()
	replaced := h.call(t, "volume")
	h.send(playerVolumeTargetRequest{generation: gen, target: 40, release: true})
	replaced.reply <- playerControlsUITestReply{confirmed: 25, maximum: 25, maxRead: true}
	h.until(t, func() bool {
		state := h.ui.State()
		actual := h.ui.active.volume.State()
		return state.maximum != nil && *state.maximum == 25 && state.volume == 25 && state.sliderTarget == 25 &&
			!actual.InFlight && state.volumePending == actual.Pending
	})
	if !h.ui.State().volumePending {
		h.noCall(t)
		return
	}
	receivePlayer(t, h.clock.scheduled)
	h.clock.advance(playerVolumeWriteInterval)
	call := h.call(t, "volume")
	if call.value > 25 {
		t.Fatalf("replayed volume %d above the newly read cap", call.value)
	}
	call.reply <- playerControlsUITestReply{confirmed: 25, maximum: 25, maxRead: true}
	h.until(t, func() bool { return !h.ui.State().volumePending })
	h.noCall(t)
}

func TestPlayerControlsUIGateReplacementFailureNeverReplaysAutomatically(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(100, nil)
	blocker := h.model.mutations.reserve()
	if err := blocker.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	gen := h.model.generation
	h.send(playerVolumeTargetRequest{generation: gen, target: 20})
	h.until(t, func() bool { return h.ui.State().volumeInFlight })
	h.send(playerVolumeTargetRequest{generation: gen, target: 30})
	blocker.release()
	replaced := h.call(t, "volume")
	h.send(playerVolumeTargetRequest{generation: gen, target: 20, release: true})
	replaced.reply <- playerControlsUITestReply{err: errors.New("write failed"), uncertain: true}
	h.until(t, func() bool { return h.ui.State().volumeErr != nil && !h.ui.State().volumePending })
	if state := h.ui.State(); !state.volumeCanRetry || state.volumeRetryTarget != 20 {
		t.Fatalf("latest failed target was not retained for explicit retry: %+v", state)
	}
	h.clock.advance(time.Second)
	h.noCall(t)
}

func TestPlayerControlsUICanceledGateWaitIsNotUncertain(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(100, nil)
	blocker := h.model.mutations.reserve()
	if err := blocker.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	old := h.ui.active
	h.send(playerMuteRequest{generation: h.model.generation})
	actionDone := old.workers[len(old.workers)-1]
	h.send(playerVolumeTargetRequest{generation: h.model.generation, target: 40})
	h.schedule(h.model.selectSpeaker(h.model.speakers[1], false))
	receivePlayer(t, actionDone)
	receivePlayer(t, old.volume.Done())
	blocker.release()
	h.noCall(t)
	if notice := waitPlayerControlsVolumeNotice(old)(); notice != nil {
		t.Fatalf("unacquired volume gate reported uncertainty: %v", notice)
	}
}

type playerControlsUIBlockingBackend struct {
	*playerControlsUITestBackend
	started chan struct{}
	release chan struct{}
}

func (b *playerControlsUIBlockingBackend) ToggleMute(context.Context) (bool, error) {
	close(b.started)
	<-b.release
	return false, &playerMutationUncertainError{Action: "mute", Target: b.target.IPAddress, Err: context.Canceled}
}

func TestPlayerControlsUIShutdownReturnsActiveWorkerChannels(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	backend := &playerControlsUIBlockingBackend{
		playerControlsUITestBackend: &playerControlsUITestBackend{target: *h.model.target, calls: h.calls},
		started:                     make(chan struct{}), release: make(chan struct{}),
	}
	h.ui.active.backend = backend
	h.send(playerMuteRequest{generation: h.model.generation})
	receivePlayer(t, backend.started)
	actionDone := h.ui.active.workers[len(h.ui.active.workers)-1]
	stopped := make(chan []<-chan struct{}, 1)
	go func() { stopped <- h.ui.stop(h.model) }()
	channels := receivePlayer(t, stopped)
	if len(channels) < 2 || playerControlsDone(actionDone) {
		close(backend.release)
		t.Fatal("stop did not return the still-running control worker")
	}
	close(backend.release)
	for _, done := range channels {
		receivePlayer(t, done)
	}
	h.until(t, func() bool { return len(h.notices) > 0 })
	if h.notices[0].target.IPAddress != "a" {
		t.Fatalf("shutdown lost the captured target: %+v", h.notices[0])
	}
}

func TestPlayerControlsUIRateWaitDoesNotBlockMute(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.maximum(100, nil)
	gen := h.model.generation
	h.send(playerVolumeTargetRequest{generation: gen, target: 30})
	first := h.call(t, "volume")
	first.reply <- playerControlsUITestReply{confirmed: 30}
	h.until(t, func() bool { return !h.ui.State().volumePending && h.ui.State().volume == 30 })
	h.send(playerVolumeTargetRequest{generation: gen, target: 40})
	receivePlayer(t, h.clock.scheduled)
	h.send(playerMuteRequest{generation: gen})
	mute := h.call(t, "mute")
	mute.reply <- playerControlsUITestReply{muted: true}
	h.until(t, func() bool { return !h.ui.State().pending })
	h.clock.advance(playerVolumeWriteInterval)
	second := h.call(t, "volume")
	second.reply <- playerControlsUITestReply{confirmed: 40}
	h.until(t, func() bool { return !h.ui.State().volumePending && h.ui.State().volume == 40 })
}

func TestPlayerControlsUIStateCopiesMutableValues(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	snapshot := playerControlsUITestSnapshot(25)
	snapshot.Player.TrackRoles.MediaData.Resources = []kefw2.PlayerResource{{Duration: 10}}
	h.status(snapshot)
	h.maximum(60, nil)
	copied := h.ui.State()
	*copied.maximum = 1
	*copied.confirmed.Data.Playback.PositionMS = 2
	copied.confirmed.Player.TrackRoles.MediaData.Resources[0].Duration = 3
	copied.supportedSources[0] = kefw2.SourceUSB
	again := h.ui.State()
	if *again.maximum != 60 || *again.confirmed.Data.Playback.PositionMS != 1000 ||
		again.confirmed.Player.TrackRoles.MediaData.Resources[0].Duration != 10 ||
		again.supportedSources[0] != kefw2.SourceWiFi {
		t.Fatalf("presentation state leaked mutable data: %+v", again)
	}
}

func TestPlayerControlsUIMissingGateFailsClosed(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.ui.reset(h.model)
	h.model.mutations = nil
	h.ui.update(h.model, playerSessionStartedMsg{generation: h.model.generation, target: *h.model.target})
	if h.ui.active != nil || h.ui.State().err == nil {
		t.Fatalf("missing mutation gate was not rejected: %+v", h.ui.State())
	}
	h.noCall(t)
}

func TestPlayerControlsUIRefreshFailureDisablesMutations(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	h.status(playerControlsUITestSnapshot(25))
	h.ui.readStatus = func(context.Context, playerSpeaker) (statusSnapshot, error) {
		return statusSnapshot{}, fmt.Errorf("read failed")
	}
	h.ui.refreshWanted = true
	h.schedule(h.ui.refresh())
	h.until(t, func() bool { return h.ui.State().err != nil })
	if h.ui.State().ready || h.ui.active != nil {
		t.Fatal("failed confirmation left controls active")
	}
	h.send(playerMuteRequest{generation: h.model.generation})
	h.noCall(t)
}
