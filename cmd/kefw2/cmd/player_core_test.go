// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hilli/go-kef-w2/kefw2"
)

func TestPlayerInitialSelection(t *testing.T) {
	configured := []kefw2.KEFSpeaker{
		{Name: "Living", Model: "LS60", IPAddress: "10.0.0.1"},
		{Name: "Living", Model: "LSX", IPAddress: "10.0.0.2"},
		{Name: "Bedroom", Model: "LS50", IPAddress: "10.0.0.3"},
	}
	original := append([]kefw2.KEFSpeaker(nil), configured...)
	tests := []struct {
		name, defaultIP, explicit, setting string
		speakers                           []kefw2.KEFSpeaker
		wantIP                             string
		wantChoose, wantConfigured         bool
		wantError                          string
	}{
		{name: "flag overrides setting and default", speakers: configured, defaultIP: "10.0.0.1",
			explicit: "Bedroom", setting: "10.0.0.2", wantIP: "10.0.0.3", wantConfigured: true},
		{name: "configured selector overrides default", speakers: configured, defaultIP: "10.0.0.1",
			setting: "10.0.0.2", wantIP: "10.0.0.2", wantConfigured: true},
		{name: "configured default", speakers: configured, defaultIP: "10.0.0.2",
			wantIP: "10.0.0.2", wantConfigured: true},
		{name: "sole speaker", speakers: configured[:1], wantIP: "10.0.0.1", wantConfigured: true},
		{name: "multiple without default", speakers: configured, wantChoose: true},
		{name: "missing default opens chooser", speakers: configured, defaultIP: "10.0.0.9", wantChoose: true},
		{name: "duplicate flag name", speakers: configured, explicit: "Living", wantError: "ambiguous"},
		{name: "duplicate configured name", speakers: configured, setting: "Living", wantError: "ambiguous"},
		{name: "no configured speakers", wantError: "config speaker discover --save"},
		{name: "explicit unconfigured endpoint", speakers: configured, explicit: "10.0.0.42", wantIP: "10.0.0.42"},
		{name: "unconfigured endpoint without config", explicit: "speaker.local", wantIP: "speaker.local"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := initialPlayerSelection(tt.speakers, tt.defaultIP, tt.explicit, tt.setting)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v; want %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.choose != tt.wantChoose {
				t.Fatalf("chooser = %t, want %t", got.choose, tt.wantChoose)
			}
			if tt.wantIP == "" {
				if got.selected != nil {
					t.Fatalf("unexpected selection: %+v", got.selected)
				}
				return
			}
			if got.selected == nil || got.selected.IPAddress != tt.wantIP || got.selected.Configured != tt.wantConfigured {
				t.Fatalf("selection = %+v, want %s configured=%t", got.selected, tt.wantIP, tt.wantConfigured)
			}
		})
	}
	if !reflect.DeepEqual(configured, original) {
		t.Fatal("selection changed configured speakers")
	}
}

func waitPlayerTestResult[T any](ctx context.Context, t *testing.T, results <-chan T) T {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-ctx.Done():
		t.Fatal("timed out waiting for a mutation worker")
		var zero T
		return zero
	}
}

func assertPlayerMutationBlocked(t *testing.T, acquired <-chan string) {
	t.Helper()
	select {
	case name := <-acquired:
		t.Fatalf("%s entered while another transaction was active", name)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestPlayerMutationGateOrdersAndCancels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	gate := newPlayerMutationGate()
	first, canceled, second, third := gate.reserve(), gate.reserve(), gate.reserve(), gate.reserve()
	defer first.release()
	if err := first.wait(ctx); err != nil {
		t.Fatal(err)
	}

	cancelCtx, cancelWait := context.WithCancel(ctx)
	canceledResult := make(chan error, 1)
	go func() { canceledResult <- canceled.wait(cancelCtx) }()
	cancelWait()
	if err := waitPlayerTestResult(ctx, t, canceledResult); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reservation: %v", err)
	}
	canceled.release()

	acquired := make(chan string, 2)
	releaseSecond := make(chan struct{})
	done := make(chan error, 2)
	go func() {
		defer second.release()
		if err := second.wait(ctx); err != nil {
			done <- err
			return
		}
		acquired <- "second"
		select {
		case <-releaseSecond:
			done <- nil
		case <-ctx.Done():
			done <- ctx.Err()
		}
	}()
	go func() {
		defer third.release()
		if err := third.wait(ctx); err != nil {
			done <- err
			return
		}
		acquired <- "third"
		done <- nil
	}()
	assertPlayerMutationBlocked(t, acquired)
	first.release()
	if name := waitPlayerTestResult(ctx, t, acquired); name != "second" {
		t.Fatalf("FIFO order: got %s before second", name)
	}
	first.release()
	assertPlayerMutationBlocked(t, acquired)
	close(releaseSecond)
	if name := waitPlayerTestResult(ctx, t, acquired); name != "third" {
		t.Fatalf("FIFO order: got %s after second", name)
	}
	for range 2 {
		if err := waitPlayerTestResult(ctx, t, done); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlayerMutationGateConcurrentReservations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	gate := newPlayerMutationGate()
	start := make(chan struct{})
	var running atomic.Int32
	var workers sync.WaitGroup
	errs := make(chan error, 64)
	for range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			ticket := gate.reserve()
			defer ticket.release()
			if err := ticket.wait(ctx); err != nil {
				errs <- err
				return
			}
			if running.Add(1) != 1 {
				errs <- errors.New("mutations overlapped")
			}
			running.Add(-1)
		}()
	}
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestPlayerMutationGateIsSessionScoped(t *testing.T) {
	factory := &fakePlayerFactory{autoFinish: true}
	selection := playerSelection{
		configured: []playerSpeaker{{IPAddress: "a"}, {IPAddress: "b"}},
		selected:   &playerSpeaker{IPAddress: "a"},
	}
	model := newPlayerModel(context.Background(), playerListeningDesk, selection, factory.newSession)
	var gates []*playerMutationGate
	model.addExtension(playerExtension{update: func(m *playerModel, msg tea.Msg) (tea.Cmd, bool) {
		if _, ok := msg.(playerSessionStartedMsg); ok {
			gates = append(gates, m.mutations)
		}
		return nil, false
	}})
	model.Init()
	defer func() {
		if err := model.shutdown(); err != nil {
			t.Error(err)
		}
	}()
	if len(gates) != 1 || gates[0] == nil {
		t.Fatal("first session did not expose a mutation gate")
	}
	oldCtx := factory.sessions[0].context()
	held := gates[0].reserve()
	defer held.release()
	if err := held.wait(oldCtx); err != nil {
		t.Fatal(err)
	}
	waiting := gates[0].reserve()
	model.selectSpeaker(model.speakers[1], false)
	if err := waiting.wait(oldCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("retired session reservation was not canceled: %v", err)
	}
	if len(gates) != 2 || gates[1] == nil || gates[1] == gates[0] {
		t.Fatal("new speaker did not receive an independent gate")
	}
	next := gates[1].reserve()
	if err := next.wait(factory.sessions[1].context()); err != nil {
		t.Fatalf("retired speaker blocked the new gate: %v", err)
	}
	next.release()
	model.Update(playerSessionDoneMsg{generation: 1})
	model.selectSpeaker(model.speakers[0], false)
	if len(gates) != 3 || gates[2] == nil || gates[2] == gates[0] || gates[2] == gates[1] {
		t.Fatal("returning to A reused a retired generation's gate")
	}
}

func TestPlayerStatusRefreshGenerationAndBroadcastOrder(t *testing.T) {
	factory := &fakePlayerFactory{autoFinish: true}
	model := testPlayerModel(t, factory)
	defer func() {
		if err := model.shutdown(); err != nil {
			t.Error(err)
		}
	}()
	firstGeneration := model.generation
	first := factory.sessions[0]
	firstSeq := model.requestStatusRefresh(firstGeneration)
	if firstSeq == 0 || first.refreshes.Load() != firstSeq {
		t.Fatal("current session did not receive a refresh")
	}
	model.Update(playerStatusRefreshRequest{generation: firstGeneration})
	if first.refreshes.Load() <= firstSeq {
		t.Fatal("refresh message did not signal the current session")
	}
	model.Update(playerStatusRefreshedResult{sequence: firstSeq, update: watchUpdate{
		kind: watchSnapshotUpdate, snapshot: statusSnapshot{Data: statusResponse{Volume: 99}},
	}})
	if model.snapshot != nil {
		t.Fatal("unscoped refresh result was accepted")
	}

	model.selectSpeaker(model.speakers[1], false)
	secondGeneration := model.generation
	second := factory.sessions[1]
	if model.requestStatusRefresh(firstGeneration) != 0 {
		t.Fatal("retired generation requested a refresh")
	}
	model.Update(playerScopedMsg{generation: firstGeneration, message: playerStatusRefreshRequest{
		generation: secondGeneration,
	}})
	if second.refreshes.Load() != 0 {
		t.Fatal("retired scoped request refreshed the new speaker")
	}
	model.Update(playerSessionDoneMsg{generation: firstGeneration})
	model.selectSpeaker(model.speakers[0], false)
	sequence := model.requestStatusRefresh(model.generation)
	if sequence == 0 || factory.sessions[2].refreshes.Load() != sequence {
		t.Fatal("returning to A did not refresh the new generation")
	}
	model.Update(playerSessionMsg{generation: firstGeneration, message: playerStatusRefreshedResult{
		sequence: firstSeq, update: watchUpdate{kind: watchSnapshotUpdate,
			snapshot: statusSnapshot{Data: statusResponse{Volume: 99}}},
	}})
	if model.snapshot != nil {
		t.Fatal("old A refresh survived A -> B -> A")
	}

	var notifications []string
	model.addExtension(playerExtension{update: func(m *playerModel, msg tea.Msg) (tea.Cmd, bool) {
		switch msg := msg.(type) {
		case watchUpdate:
			if msg.kind == watchSnapshotUpdate {
				notifications = append(notifications, "snapshot")
			}
		case playerStatusRefreshCompletedMsg:
			if msg.generation != m.generation || msg.sequence != sequence || msg.err != nil ||
				m.snapshot == nil || m.snapshot.Data.Volume != 42 {
				t.Error("refresh completion preceded its applied snapshot")
			}
			notifications = append(notifications, "completed")
		}
		return nil, false
	}})
	model.Update(playerSessionMsg{generation: model.generation, message: playerStatusRefreshedResult{
		sequence: sequence, update: watchUpdate{kind: watchSnapshotUpdate,
			snapshot: statusSnapshot{Data: statusResponse{Volume: 42}}},
	}})
	if !reflect.DeepEqual(notifications, []string{"snapshot", "completed"}) {
		t.Fatalf("refresh broadcast order = %v", notifications)
	}
}

func TestPlayerCLIValidationAndCompletion(t *testing.T) {
	home := t.TempDir()
	config := writeCLIConfig(t, home, kefw2.KEFSpeaker{Name: "Offline", IPAddress: "127.0.0.1:1"})
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"player"},
		{"player", "--layout", playerListeningDesk},
		{"player", "--layout", playerDiscoveryBrowser},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			stdout, stderr, err := runCLI(t, home, config, args...)
			requireCLIFailure(t, stdout, stderr, err, "requires a terminal for both input and output")
			if strings.Contains(stdout+stderr, "\x1b") {
				t.Fatalf("non-terminal invocation emitted escapes: %q %q", stdout, stderr)
			}
		})
	}
	// A malformed config and unreachable explicit target must not be touched
	// before Cobra rejects the layout or positional arguments.
	invalidConfig := filepath.Join(home, "invalid.yaml")
	if err := os.WriteFile(invalidConfig, []byte("speakers: [\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"--speaker", "127.0.0.1:1", "player", "--layout", "other"}, "invalid player layout"},
		{[]string{"--speaker", "127.0.0.1:1", "player", "extra"}, "unknown command"},
	} {
		stdout, stderr, err := runCLI(t, home, invalidConfig, tt.args...)
		requireCLIFailure(t, stdout, stderr, err, tt.want)
	}
	for _, args := range [][]string{
		{"--speaker", "127.0.0.1:1", "__complete", "player", "--layout", "disc"},
		{"__complete", "player", "--speaker", "127.0.0.1:1", "--layout", "disc"},
	} {
		stdout, stderr, err := runCLI(t, home, config, args...)
		if err != nil || !strings.Contains(stdout, playerDiscoveryBrowser) ||
			strings.Contains(stdout+stderr, "connection") {
			t.Fatalf("layout completion: %v stdout=%q stderr=%q", err, stdout, stderr)
		}
	}
	stdout, stderr, err := runCLI(t, home, config, "--speaker", "127.0.0.1:1", "__complete", "player", "")
	if err != nil || strings.Contains(stdout+stderr, "connection") {
		t.Fatalf("player argument completion resolved a speaker: %v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if commandRequiresSpeaker(playerCmd, nil) {
		t.Fatal("player completion must not resolve a speaker")
	}
	stdout, stderr, err = runCLI(t, home, config, "player", "--help")
	if err != nil || !strings.Contains(stdout, "--layout") {
		t.Fatalf("help: %v stdout=%q stderr=%q", err, stdout, stderr)
	}
	stdout, stderr, err = runCLI(t, home, config, "player", "--json")
	requireCLIFailure(t, stdout, stderr, err, "unknown flag: --json")
	after, err := os.ReadFile(config)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("player changed config: %v", err)
	}
}

func TestPlayerPreparationDoesNotInitializeCache(t *testing.T) {
	home := t.TempDir()
	config := writeCLIConfig(t, home, kefw2.KEFSpeaker{Name: "Offline", IPAddress: "127.0.0.1:1"})
	stdout, stderr, err := runCLI(t, home, config, "player")
	requireCLIFailure(t, stdout, stderr, err, "requires a terminal")
	for _, path := range []string{
		filepath.Join(home, "Library", "Caches", "kefw2"),
		filepath.Join(home, "cache", "kefw2"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("player initialized shared cache %q: %v", path, err)
		}
	}
}

func runPlayerCLIWithEnv(t *testing.T, home, config, speaker string, args ...string) (string, string, error) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	argv := append([]string{"-test.run=^TestCLIProcess$", "--", "--config", config}, args...)
	process := exec.CommandContext(ctx, executable, argv...)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "KEFW2_") && key != "HOME" &&
			key != "XDG_CONFIG_HOME" && key != "XDG_CACHE_HOME" {
			process.Env = append(process.Env, entry)
		}
	}
	process.Env = append(process.Env, "KEFW2_TEST_PROCESS=1", "KEFW2_SPEAKER="+speaker, "HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, "config"), "XDG_CACHE_HOME="+filepath.Join(home, "cache"))
	var stdout, stderr bytes.Buffer
	process.Stdout, process.Stderr = &stdout, &stderr
	err = process.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI failed to terminate: %v", ctx.Err())
	}
	return stdout.String(), stderr.String(), err
}

func TestPlayerCLISelectorPrecedenceAndEmptyConfig(t *testing.T) {
	home := t.TempDir()
	config := writeCLIConfig(t, home,
		kefw2.KEFSpeaker{Name: "Twin", IPAddress: "127.0.0.1:1"},
		kefw2.KEFSpeaker{Name: "Twin", IPAddress: "127.0.0.1:2"},
	)
	configData := `{"speakers":[{"name":"Twin","ip_address":"127.0.0.1:1"},` +
		`{"name":"Twin","ip_address":"127.0.0.1:2"}],"speaker":"Twin","defaultSpeaker":"127.0.0.1:1"}`
	if err := os.WriteFile(config, []byte(configData), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runCLI(t, home, config, "player")
	requireCLIFailure(t, stdout, stderr, err, "speaker name \"Twin\" is ambiguous")
	stdout, stderr, err = runPlayerCLIWithEnv(t, home, config, "127.0.0.1:2", "player")
	requireCLIFailure(t, stdout, stderr, err, "requires a terminal")
	stdout, stderr, err = runPlayerCLIWithEnv(t, home, config, "Twin", "player")
	requireCLIFailure(t, stdout, stderr, err, "speaker name \"Twin\" is ambiguous")
	stdout, stderr, err = runPlayerCLIWithEnv(t, home, config, "Twin", "--speaker", "127.0.0.1:1", "player")
	requireCLIFailure(t, stdout, stderr, err, "requires a terminal")
	emptyHome := t.TempDir()
	emptyConfig := writeCLIConfig(t, emptyHome)
	stdout, stderr, err = runCLI(t, emptyHome, emptyConfig, "player")
	requireCLIFailure(t, stdout, stderr, err, "config speaker discover --save")
	stdout, stderr, err = runCLI(t, emptyHome, emptyConfig, "--speaker", "127.0.0.1:1", "player")
	requireCLIFailure(t, stdout, stderr, err, "requires a terminal")
}

type fakePlayerSession struct {
	ctx        context.Context
	stop       context.CancelFunc
	updates    chan tea.Msg
	finished   chan struct{}
	autoFinish bool
	finishOnce sync.Once
	cancels    atomic.Int32
	refreshes  atomic.Uint64
}

func (s *fakePlayerSession) context() context.Context { return s.ctx }
func (s *fakePlayerSession) messages() <-chan tea.Msg { return s.updates }
func (s *fakePlayerSession) done() <-chan struct{}    { return s.finished }
func (s *fakePlayerSession) refreshStatus(sequence uint64) {
	s.refreshes.Store(sequence)
}
func (s *fakePlayerSession) cancel() {
	s.cancels.Add(1)
	s.stop()
	if s.autoFinish {
		s.finish()
	}
}
func (s *fakePlayerSession) finish() { s.finishOnce.Do(func() { close(s.finished) }) }

type fakePlayerFactory struct {
	sessions    []*fakePlayerSession
	targets     []playerSpeaker
	generations []uint64
	autoFinish  bool
}

func (f *fakePlayerFactory) newSession(parent context.Context, target playerSpeaker, generation uint64) playerSession {
	ctx, cancel := context.WithCancel(parent)
	session := &fakePlayerSession{
		ctx: ctx, stop: cancel, updates: make(chan tea.Msg, 8), finished: make(chan struct{}),
		autoFinish: f.autoFinish,
	}
	f.sessions = append(f.sessions, session)
	f.targets = append(f.targets, target)
	f.generations = append(f.generations, generation)
	return session
}

func testPlayerModel(t *testing.T, factory *fakePlayerFactory) *playerModel {
	t.Helper()
	speakers := []kefw2.KEFSpeaker{
		{Name: "Living", Model: "LS60", IPAddress: "a"},
		{Name: "Bedroom", Model: "LSX", IPAddress: "b"},
	}
	selection, err := initialPlayerSelection(speakers, "a", "", "")
	if err != nil {
		t.Fatal(err)
	}
	model := newPlayerModel(context.Background(), playerListeningDesk, selection, factory.newSession)
	model.Init()
	return model
}

func playerKey(text string, code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Text: text, Code: code}
}

func TestPlayerInitialChooserDoesNotConnect(t *testing.T) {
	configured := []kefw2.KEFSpeaker{
		{Name: "First", Model: "LS60", IPAddress: "a"},
		{Name: "Second", Model: "LSX", IPAddress: "b"},
	}
	selection, err := initialPlayerSelection(configured, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	factory := &fakePlayerFactory{autoFinish: true}
	model := newPlayerModel(context.Background(), playerDiscoveryBrowser, selection, factory.newSession)
	defer func() {
		if err := model.shutdown(); err != nil {
			t.Error(err)
		}
	}()
	if model.Init() != nil || len(factory.sessions) != 0 || !model.chooserOpen || model.target != nil {
		t.Fatal("startup chooser connected before a selection")
	}
	model.Update(playerKey("", tea.KeyEsc))
	if model.chooserOpen || len(factory.sessions) != 0 {
		t.Fatal("canceling the startup chooser started a session")
	}
	model.Update(playerKey("S", 'S'))
	model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if model.chooserIndex != 1 || !model.chooserOpen || len(factory.sessions) != 0 {
		t.Fatal("shift-tab did not stay in and navigate the startup chooser")
	}
	model.Update(playerKey("", tea.KeyTab))
	model.Update(playerKey("", tea.KeyDown))
	model.Update(playerKey("", tea.KeyEnter))
	if model.target == nil || model.target.IPAddress != "b" || model.generation != 1 || len(factory.sessions) != 1 {
		t.Fatal("startup chooser did not connect the chosen target")
	}
	model.Update(playerKey("S", 'S'))
	model.Update(playerKey("", tea.KeyEnter))
	if model.generation != 1 || factory.sessions[0].cancels.Load() != 0 || len(factory.sessions) != 1 {
		t.Fatal("selecting the active speaker restarted its session")
	}
}

func TestPlayerDirectTargetIsSessionOnly(t *testing.T) {
	selection, err := initialPlayerSelection(nil, "", "speaker.local", "")
	if err != nil {
		t.Fatal(err)
	}
	factory := &fakePlayerFactory{autoFinish: true}
	model := newPlayerModel(context.Background(), playerListeningDesk, selection, factory.newSession)
	model.Init()
	defer func() {
		if err := model.shutdown(); err != nil {
			t.Error(err)
		}
	}()
	if !strings.Contains(model.View().Content, "speaker.local") ||
		!strings.Contains(model.View().Content, "session-only") {
		t.Fatal("direct target is not visibly distinguished from configured speakers")
	}
	model.Update(playerKey("S", 'S'))
	if !strings.Contains(model.View().Content, "No configured speakers") || factory.sessions[0].cancels.Load() != 0 {
		t.Fatal("opening an empty chooser changed the direct session")
	}
}

func assertPlayerSwitchToB(t *testing.T, model *playerModel, factory *fakePlayerFactory, oldGeneration uint64) {
	t.Helper()
	if model.chooserOpen || model.target.IPAddress != "b" || model.generation != oldGeneration+1 ||
		model.connection != playerConnecting || factory.sessions[0].ctx.Err() == nil || len(factory.sessions) != 2 {
		t.Fatalf("incorrect switch: target=%+v generation=%d connection=%s sessions=%d",
			model.target, model.generation, model.connection, len(factory.sessions))
	}
	if model.snapshot != nil || model.readErr != nil || model.eventErr != nil || model.maxVolume != nil ||
		model.maxVolumeErr != nil || model.speakerInfo != nil {
		t.Fatal("speaker switch retained old speaker state")
	}
}

func assertPlayerReady(t *testing.T, model *playerModel, volume, maximum int) {
	t.Helper()
	if model.snapshot == nil || model.snapshot.Data.Volume != volume || model.maxVolume == nil ||
		*model.maxVolume != maximum || model.connection != playerReady {
		t.Fatal("current speaker results were not applied")
	}
}

func assertPlayerReturnToA(
	t *testing.T, model *playerModel, factory *fakePlayerFactory, oldGeneration, bGeneration uint64,
) {
	t.Helper()
	model.Update(playerSessionDoneMsg{generation: oldGeneration})
	model.selectSpeaker(model.speakers[0], false)
	if model.generation != oldGeneration+2 || model.target.IPAddress != "a" ||
		factory.sessions[1].ctx.Err() == nil || len(factory.sessions) != 3 {
		t.Fatal("returning to A did not start a new generation")
	}
	model.Update(playerSessionMsg{generation: oldGeneration, message: watchUpdate{
		kind: watchSnapshotUpdate, snapshot: statusSnapshot{Data: statusResponse{Volume: 99}},
	}})
	model.Update(playerSessionMsg{generation: bGeneration, message: playerMaxVolumeResult{volume: 70}})
	if model.snapshot != nil || model.maxVolume != nil {
		t.Fatal("A -> B -> A accepted a retired generation")
	}
	model.Update(playerSessionMsg{generation: model.generation, message: watchUpdate{
		kind: watchSnapshotUpdate, snapshot: statusSnapshot{Data: statusResponse{Volume: 30}},
	}})
	if model.snapshot == nil || model.snapshot.Data.Volume != 30 {
		t.Fatal("new A generation did not receive status")
	}
}

func TestPlayerChooserSwitchAndStaleResults(t *testing.T) {
	factory := &fakePlayerFactory{autoFinish: true}
	model := testPlayerModel(t, factory)
	defer func() {
		if err := model.shutdown(); err != nil {
			t.Error(err)
		}
	}()
	a := factory.sessions[0]
	oldGeneration := model.generation
	a.updates <- watchUpdate{kind: watchSnapshotUpdate, snapshot: statusSnapshot{Data: statusResponse{Volume: 99}}}
	queuedOldStatus := waitPlayerMessage(oldGeneration, a)()
	model.snapshot = &statusSnapshot{Data: statusResponse{Volume: 10}}
	model.readErr, model.eventErr = errors.New("old status"), errors.New("old event")
	maximum := 90
	model.maxVolume, model.maxVolumeErr = &maximum, errors.New("old max")
	model.speakerInfo = &playerSpeaker{Name: "Old info"}
	queue, searchPath, resets := []string{"old track"}, "old search", 0
	model.addExtension(playerExtension{reset: func(m *playerModel) {
		queue, searchPath = nil, ""
		resets++
		if resets == 1 && (m.target == nil || m.target.IPAddress != "b" || m.generation != oldGeneration+1) {
			t.Errorf("reset observed wrong target/generation: %+v %d", m.target, m.generation)
		}
	}})

	model.Update(playerKey("S", 'S'))
	if !model.chooserOpen || model.chooserIndex != 0 {
		t.Fatal("S did not open chooser on current speaker")
	}
	model.Update(playerKey("", tea.KeyEsc))
	if model.chooserOpen || model.generation != oldGeneration || a.cancels.Load() != 0 {
		t.Fatal("canceling chooser changed the active session")
	}
	model.Update(playerKey("S", 'S'))
	model.Update(playerKey("", tea.KeyDown))
	model.Update(playerKey("", tea.KeyEnter))
	assertPlayerSwitchToB(t, model, factory, oldGeneration)
	if len(queue) != 0 || searchPath != "" || resets != 1 {
		t.Fatal("speaker switch did not reset extension state")
	}
	model.Update(queuedOldStatus)
	model.Update(playerSessionMsg{generation: oldGeneration, message: playerMaxVolumeResult{volume: 100}})
	model.Update(playerSessionMsg{generation: oldGeneration, message: watchUpdate{kind: playerStatusUnavailableUpdate, err: errors.New("old")}})
	model.Update(playerSessionMsg{generation: oldGeneration, message: watchUpdate{kind: playerEventConnectionUpdate, err: errors.New("old event")}})
	model.Update(playerSessionMsg{generation: oldGeneration, message: playerInfoResult{err: errors.New("old info")}})
	if model.snapshot != nil || model.maxVolume != nil || model.readErr != nil ||
		model.eventErr != nil || model.speakerInfoErr != nil {
		t.Fatal("retired speaker results changed the new target")
	}
	bGeneration := model.generation
	model.Update(playerSessionMsg{generation: bGeneration, message: watchUpdate{
		kind: watchSnapshotUpdate, snapshot: statusSnapshot{Data: statusResponse{Volume: 20}},
	}})
	model.Update(playerSessionMsg{generation: bGeneration, message: playerMaxVolumeResult{volume: 70}})
	assertPlayerReady(t, model, 20, 70)
	assertPlayerReturnToA(t, model, factory, oldGeneration, bGeneration)
}

type playerTestDomainMsg struct{ value string }

func TestPlayerFocusedExtensionOwnsSpeakerShortcut(t *testing.T) {
	factory := &fakePlayerFactory{autoFinish: true}
	model := testPlayerModel(t, factory)
	defer func() {
		if err := model.shutdown(); err != nil {
			t.Error(err)
		}
	}()
	focused, typed := true, ""
	model.addExtension(playerExtension{update: func(_ *playerModel, msg tea.Msg) (tea.Cmd, bool) {
		key, ok := msg.(tea.KeyPressMsg)
		if focused && ok && key.String() == "S" {
			typed += key.String()
			return nil, true
		}
		return nil, false
	}})
	model.Update(playerKey("S", 'S'))
	if typed != "S" || model.chooserOpen {
		t.Fatal("speaker shortcut stole text from a focused input")
	}
	focused = false
	model.Update(playerKey("S", 'S'))
	if !model.chooserOpen {
		t.Fatal("speaker shortcut did not open chooser outside text input")
	}
}

type playerExtensionProbe struct {
	t                                         *testing.T
	started, ended, statuses, resets, stopped [2]int
	keys                                      []string
	finished                                  <-chan struct{}
}

func (p *playerExtensionProbe) update(index int, m *playerModel, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case playerSessionStartedMsg:
		p.started[index]++
		if m.session == nil || msg.generation != m.generation || msg.target != *m.target {
			p.t.Errorf("extension saw incorrect session start: %+v", msg)
		}
		return nil, true
	case watchUpdate:
		p.statuses[index]++
		if m.snapshot == nil || m.snapshot.Data.Volume != 24 {
			p.t.Error("status notification preceded model update")
		}
		return nil, true
	case playerSessionStoppedMsg:
		p.ended[index]++
		if m.session != nil || m.connection != playerUnavailable || msg.err == nil {
			p.t.Error("session stop notification preceded model update")
		}
		return nil, true
	case tea.KeyPressMsg:
		p.keys = append(p.keys, fmt.Sprintf("%d:%s", index, msg.String()))
		return nil, index == 0 && msg.String() == "x" || index == 1 && msg.String() == "y"
	}
	return nil, false
}

func TestPlayerComposableExtensionLifecycle(t *testing.T) {
	factory := &fakePlayerFactory{autoFinish: true}
	selection := playerSelection{
		configured: []playerSpeaker{{IPAddress: "a"}, {IPAddress: "b"}},
		selected:   &playerSpeaker{IPAddress: "a"},
	}
	model := newPlayerModel(context.Background(), playerListeningDesk, selection, factory.newSession)
	finished := make(chan struct{})
	close(finished)
	probe := &playerExtensionProbe{t: t, finished: finished}
	for i := range 2 {
		index := i
		model.addExtension(playerExtension{
			update: func(m *playerModel, msg tea.Msg) (tea.Cmd, bool) {
				return probe.update(index, m, msg)
			},
			reset: func(*playerModel) { probe.resets[index]++ },
			stop: func(*playerModel) []<-chan struct{} {
				probe.stopped[index]++
				return []<-chan struct{}{probe.finished}
			},
		})
	}
	model.Init()
	if probe.started != [2]int{1, 1} {
		t.Fatalf("session start was not broadcast: %v", probe.started)
	}
	update := watchUpdate{kind: watchSnapshotUpdate, snapshot: statusSnapshot{Data: statusResponse{Volume: 24}}}
	model.Update(update)
	if model.snapshot != nil || probe.statuses != [2]int{} {
		t.Fatal("unscoped speaker result was accepted")
	}
	model.Update(playerSessionMsg{generation: model.generation, message: update})
	model.Update(playerSessionMsg{generation: model.generation,
		message: playerMaxVolumeResult{err: errors.New("maximum unavailable")}})
	if model.connection != playerReady || model.snapshot == nil || model.maxVolume != nil || model.maxVolumeErr == nil {
		t.Fatal("maximum read failure incorrectly invalidated fresh status")
	}
	model.Update(playerKey("x", 'x'))
	model.Update(playerKey("y", 'y'))
	if probe.statuses != [2]int{1, 1} || !reflect.DeepEqual(probe.keys, []string{"0:x", "0:y", "1:y"}) {
		t.Fatalf("extension routing: statuses=%v keys=%v", probe.statuses, probe.keys)
	}
	factory.sessions[0].finish()
	model.Update(playerSessionDoneMsg{generation: model.generation})
	if probe.ended != [2]int{1, 1} || factory.sessions[0].ctx.Err() == nil {
		t.Fatal("unexpected session termination did not cancel and notify extensions")
	}
	model.selectSpeaker(model.speakers[1], false)
	if probe.resets != [2]int{1, 1} || probe.started != [2]int{2, 2} {
		t.Fatalf("extension switch lifecycle: resets=%v starts=%v", probe.resets, probe.started)
	}
	if err := model.shutdown(); err != nil {
		t.Fatal(err)
	}
	if probe.stopped != [2]int{1, 1} {
		t.Fatalf("extension shutdown not called: %v", probe.stopped)
	}
}

func TestPlayerExtensionRoutingRetryAndChooserMouse(t *testing.T) {
	factory := &fakePlayerFactory{autoFinish: true}
	model := testPlayerModel(t, factory)
	defer func() {
		if err := model.shutdown(); err != nil {
			t.Error(err)
		}
	}()
	var received []string
	model.addExtension(playerExtension{update: func(_ *playerModel, msg tea.Msg) (tea.Cmd, bool) {
		switch msg := msg.(type) {
		case playerTestDomainMsg:
			received = append(received, msg.value)
			return nil, true
		case tea.KeyPressMsg:
			if msg.String() == "s" {
				received = append(received, "source")
				return nil, true
			}
		}
		return nil, false
	}})
	old := model.generation
	command := playerScopedCmd(old, func() tea.Msg { return playerTestDomainMsg{value: "old"} })
	model.Update(playerKey("s", 's'))
	if model.chooserOpen || !reflect.DeepEqual(received, []string{"source"}) {
		t.Fatal("lowercase s was not left to the source handler")
	}
	view := model.View()
	click := view.OnMouse(tea.MouseClickMsg{X: 2, Y: 1, Button: tea.MouseLeft})
	if click == nil {
		t.Fatal("header click did not open the chooser")
	}
	model.Update(click())
	view = model.View()
	if !strings.Contains(view.Content, "Living | LS60 | a | default | current") ||
		!strings.Contains(view.Content, "Bedroom | LSX | b") {
		t.Fatalf("chooser lacks speaker details: %s", view.Content)
	}
	click = view.OnMouse(tea.MouseClickMsg{X: 2, Y: 3, Button: tea.MouseLeft})
	if click == nil {
		t.Fatal("chooser row was not clickable")
	}
	model.Update(click())
	if model.target.IPAddress != "b" {
		t.Fatal("click did not switch target")
	}
	model.Update(command())
	if len(received) != 1 {
		t.Fatal("retired scoped command reached extension")
	}
	current := playerScopedCmd(model.generation, func() tea.Msg { return playerTestDomainMsg{value: "current"} })
	model.Update(current())
	if !reflect.DeepEqual(received, []string{"source", "current"}) {
		t.Fatalf("extension results = %v", received)
	}
	model.Update(playerSessionMsg{generation: model.generation, message: watchUpdate{
		kind: playerStatusUnavailableUpdate, err: errors.New("offline"),
	}})
	if model.connection != playerUnavailable || model.readErr == nil {
		t.Fatal("offline target was not retained")
	}
	model.Update(playerSessionDoneMsg{generation: old})
	generation := model.generation
	model.Update(playerKey("r", 'r'))
	if model.generation != generation+1 || model.target.IPAddress != "b" ||
		model.connection != playerConnecting || model.readErr != nil {
		t.Fatal("retry did not create a fresh session for the same target")
	}
}

func TestPlayerRapidSwitchCoalescesAndJoins(t *testing.T) {
	factory := &fakePlayerFactory{}
	model := testPlayerModel(t, factory)
	model.selectSpeaker(playerSpeaker{IPAddress: "b"}, false)
	model.selectSpeaker(playerSpeaker{IPAddress: "c"}, false)
	model.selectSpeaker(playerSpeaker{IPAddress: "d"}, false)
	if len(factory.sessions) != playerMaxLiveSessions || !model.pendingStart || model.target.IPAddress != "d" ||
		factory.sessions[0].ctx.Err() == nil || factory.sessions[1].ctx.Err() == nil {
		t.Fatal("rapid switches did not cancel and coalesce")
	}
	factory.sessions[0].finish()
	model.Update(playerSessionDoneMsg{generation: factory.generations[0]})
	if len(factory.sessions) != 3 || factory.targets[2].IPAddress != "d" || model.pendingStart {
		t.Fatalf("did not start latest target after retirement: %+v", factory.targets)
	}
	factory.sessions[1].finish()
	model.Update(playerSessionDoneMsg{generation: factory.generations[1]})
	active := factory.sessions[2]
	extensionDone := make(chan struct{})
	model.addExtension(playerExtension{stop: func(*playerModel) []<-chan struct{} {
		return []<-chan struct{}{extensionDone}
	}})
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- model.shutdown() }()
	select {
	case <-active.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel active session")
	}
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before worker joined: %v", err)
	default:
	}
	active.finish()
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before extension worker joined: %v", err)
	default:
	}
	close(extensionDone)
	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not join worker")
	}
}

func receivePlayerMsg(t *testing.T, session playerSession) tea.Msg {
	t.Helper()
	select {
	case msg, ok := <-session.messages():
		if !ok {
			t.Fatal("player session stopped before producing the expected message")
		}
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for player session")
		return nil
	}
}

type playerRefreshHTTPFixture struct {
	*cliFixture
	blockVolume chan (<-chan struct{})
	started     chan struct{}
	canceled    chan struct{}
	pollStarted chan struct{}
	failSource  atomic.Bool
}

func newPlayerRefreshHTTPFixture(t *testing.T) (*playerRefreshHTTPFixture, playerSpeaker) {
	t.Helper()
	fixture, _ := newCLIFixture(t)
	f := &playerRefreshHTTPFixture{
		cliFixture: fixture, blockVolume: make(chan (<-chan struct{}), 1),
		started: make(chan struct{}, 1), canceled: make(chan struct{}, 1), pollStarted: make(chan struct{}, 1),
	}
	server := httptest.NewServer(http.HandlerFunc(f.serveRefreshHTTP))
	t.Cleanup(server.Close)
	return f, playerSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")}
}

func (f *playerRefreshHTTPFixture) serveRefreshHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/event/modifyQueue":
		_, _ = io.WriteString(w, `"refresh-test"`)
		return
	case "/api/event/pollQueue":
		select {
		case f.pollStarted <- struct{}{}:
		default:
		}
		<-r.Context().Done()
		return
	}
	path := r.URL.Query().Get("path")
	if path == "settings:/kef/play/physicalSource" && f.failSource.Swap(false) {
		http.Error(w, "status unavailable", http.StatusServiceUnavailable)
		return
	}
	if path == "player:volume" {
		select {
		case release := <-f.blockVolume:
			f.mu.Lock()
			value := f.data[path]
			f.mu.Unlock()
			f.started <- struct{}{}
			select {
			case <-release:
				_, _ = io.WriteString(w, value)
			case <-r.Context().Done():
				f.canceled <- struct{}{}
			}
			return
		default:
		}
	}
	f.serveHTTP(w, r)
}

func waitPlayerInitialSnapshots(t *testing.T, model *playerModel) {
	t.Helper()
	for snapshots := 0; snapshots < 2; {
		msg := receivePlayerMsg(t, model.session)
		model.Update(playerSessionMsg{generation: model.generation, message: msg})
		if update, ok := msg.(watchUpdate); ok && update.kind == watchSnapshotUpdate {
			snapshots++
		}
	}
}

func waitPlayerRefreshedResult(t *testing.T, model *playerModel, sequence uint64) playerStatusRefreshedResult {
	t.Helper()
	for {
		msg := receivePlayerMsg(t, model.session)
		model.Update(playerSessionMsg{generation: model.generation, message: msg})
		if result, ok := msg.(playerStatusRefreshedResult); ok && result.sequence >= sequence {
			return result
		}
	}
}

func TestPlayerStatusRefreshRealHTTPOrdersCoalescesAndCancels(t *testing.T) {
	fixture, target := newPlayerRefreshHTTPFixture(t)
	selection := playerSelection{selected: &target}
	model := newPlayerModel(context.Background(), playerListeningDesk, selection, newPlayerSession)
	var completed []playerStatusRefreshCompletedMsg
	model.addExtension(playerExtension{update: func(_ *playerModel, msg tea.Msg) (tea.Cmd, bool) {
		if result, ok := msg.(playerStatusRefreshCompletedMsg); ok {
			completed = append(completed, result)
		}
		return nil, false
	}})
	model.Init()
	defer func() {
		if err := model.shutdown(); err != nil {
			t.Error(err)
		}
	}()
	waitPlayerInitialSnapshots(t, model)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	waitPlayerTestResult(ctx, t, fixture.pollStarted)

	firstRelease := make(chan struct{})
	var releaseOnce sync.Once
	unblockFirst := func() { releaseOnce.Do(func() { close(firstRelease) }) }
	defer unblockFirst()
	fixture.blockVolume <- firstRelease
	first := model.requestStatusRefresh(model.generation)
	waitPlayerTestResult(ctx, t, fixture.started)
	fixture.mu.Lock()
	fixture.data["player:volume"] = `[{"type":"i32_","i32_":45}]`
	fixture.mu.Unlock()
	second := model.requestStatusRefresh(model.generation)
	last := model.requestStatusRefresh(model.generation)
	if first == 0 || first >= second || second >= last {
		t.Fatal("refresh sequences were not increasing")
	}
	unblockFirst()
	firstResult := waitPlayerRefreshedResult(t, model, first)
	if firstResult.sequence != first || firstResult.update.snapshot.Data.Volume != 25 {
		t.Fatalf("in-flight read was mislabeled as post-request: %+v", firstResult)
	}
	lastResult := waitPlayerRefreshedResult(t, model, last)
	if lastResult.sequence != last || lastResult.update.snapshot.Data.Volume != 45 {
		t.Fatalf("coalesced refresh did not observe the new status: %+v", lastResult)
	}

	fixture.failSource.Store(true)
	failed := model.requestStatusRefresh(model.generation)
	failure := waitPlayerRefreshedResult(t, model, failed)
	if failure.update.err == nil || model.connection != playerUnavailable || model.readErr == nil {
		t.Fatalf("failed refresh was not reported as unavailable: %+v", failure)
	}
	if len(completed) != 3 || completed[2].sequence != failed || completed[2].err == nil {
		t.Fatalf("failed refresh completion = %+v", completed)
	}
	finalRelease := make(chan struct{})
	defer close(finalRelease)
	fixture.blockVolume <- finalRelease
	model.requestStatusRefresh(model.generation)
	waitPlayerTestResult(ctx, t, fixture.started)
	session := model.session
	session.cancel()
	waitPlayerTestResult(ctx, t, fixture.canceled)
	waitPlayerTestResult(ctx, t, session.done())
	if len(fixture.postRequests()) != 0 {
		t.Fatal("refresh sent a playback or settings mutation")
	}
}

func TestPlayerMaximumReadIsIndependentAndCancellable(t *testing.T) {
	fixture, _ := newCLIFixture(t)
	started, canceled := make(chan struct{}, 1), make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") == "settings:/kef/play/physicalSource" {
			select {
			case started <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			canceled <- struct{}{}
			return
		}
		fixture.serveHTTP(w, r)
	}))
	defer server.Close()
	target := playerSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")}
	session := newPlayerSession(context.Background(), target, 1)
	defer session.cancel()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("status read did not start")
	}
	for {
		if result, ok := receivePlayerMsg(t, session).(playerMaxVolumeResult); ok {
			if result.err != nil || result.volume != 80 {
				t.Fatalf("maximum = %+v", result)
			}
			break
		}
	}
	session.cancel()
	select {
	case <-session.done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not join after canceling a blocked read")
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked status request was not canceled")
	}
}

func playerHTTPFixture(t *testing.T, volume int) (*cliFixture, kefw2.KEFSpeaker, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	fixture, speaker := newCLIFixture(t)
	fixture.mu.Lock()
	fixture.data["player:volume"] = fmt.Sprintf(`[{"type":"i32_","i32_":%d}]`, volume)
	fixture.mu.Unlock()
	pollStarted, pollCanceled := make(chan struct{}, 1), make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/event/modifyQueue":
			_, _ = io.WriteString(w, `"player-test"`)
		case "/api/event/pollQueue":
			select {
			case pollStarted <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			select {
			case pollCanceled <- struct{}{}:
			default:
			}
		default:
			fixture.serveHTTP(w, r)
		}
	}))
	t.Cleanup(server.Close)
	speaker.IPAddress = strings.TrimPrefix(server.URL, "http://")
	return fixture, speaker, pollStarted, pollCanceled
}

func TestPlayerRealSessionsSwitchWithoutActions(t *testing.T) {
	first, a, pollStarted, pollCanceled := playerHTTPFixture(t, 25)
	second, b, _, _ := playerHTTPFixture(t, 45)
	selection, err := initialPlayerSelection([]kefw2.KEFSpeaker{a, b}, a.IPAddress, "", "")
	if err != nil {
		t.Fatal(err)
	}
	model := newPlayerModel(context.Background(), playerListeningDesk, selection, newPlayerSession)
	model.Init()
	defer func() {
		if err := model.shutdown(); err != nil {
			t.Error(err)
		}
	}()
	for model.snapshot == nil || model.maxVolume == nil {
		model.Update(playerSessionMsg{generation: model.generation, message: receivePlayerMsg(t, model.session)})
	}
	if model.snapshot.Data.Volume != 25 || *model.maxVolume != 80 {
		t.Fatalf("wrong first speaker state: %+v max=%v", model.snapshot, model.maxVolume)
	}
	select {
	case <-pollStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first speaker event poll did not start")
	}
	model.selectSpeaker(selection.configured[1], false)
	select {
	case <-pollCanceled:
	case <-time.After(5 * time.Second):
		t.Fatal("switch did not cancel the old event poll")
	}
	for model.snapshot == nil || model.maxVolume == nil {
		model.Update(playerSessionMsg{generation: model.generation, message: receivePlayerMsg(t, model.session)})
	}
	if model.snapshot.Data.Volume != 45 || *model.maxVolume != 80 {
		t.Fatalf("wrong second speaker state: %+v max=%v", model.snapshot, model.maxVolume)
	}
	for _, fixture := range []*cliFixture{first, second} {
		fixture.mu.Lock()
		posts := len(fixture.posts)
		fixture.mu.Unlock()
		if posts != 0 {
			t.Fatalf("selecting a speaker sent %d playback or settings actions", posts)
		}
	}
}
