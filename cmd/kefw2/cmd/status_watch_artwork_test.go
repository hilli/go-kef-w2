// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type artworkProofModel struct {
	lines []string
	step  string
}

func (m artworkProofModel) Init() tea.Cmd { return nil }
func (m artworkProofModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case string:
		m.step = msg
	case []string:
		m.lines = msg
	}
	return m, nil
}
func (m artworkProofModel) View() string {
	return strings.Join(m.lines, "\n") + "\nprogress: " + m.step
}

type watchTestOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	writes chan string
}

func (w *watchTestOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	n, err := w.buffer.Write(p)
	w.mu.Unlock()
	select {
	case w.writes <- string(p):
	default:
	}
	return n, err
}

func (w *watchTestOutput) text() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

func (w *watchTestOutput) wait(t *testing.T, contains string) string {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case frame := <-w.writes:
			if strings.Contains(frame, contains) {
				return frame
			}
		case <-timeout.C:
			t.Fatalf("no frame containing %q: %q", contains, w.text())
			return ""
		}
	}
}

func TestWatchArtworkRetainsNativePlacement(t *testing.T) {
	for _, backend := range []watchArtworkBackend{watchArtworkKitty, watchArtworkIterm} {
		t.Run(fmt.Sprint(backend), func(t *testing.T) {
			testWatchArtworkRetainsNativePlacement(t, backend)
		})
	}
}

func testWatchArtworkRetainsNativePlacement(t *testing.T, backend watchArtworkBackend) {
	t.Helper()
	lines, err := renderWatchArtwork(watchArtwork{image: image.NewRGBA(image.Rect(0, 0, 2, 2))}, 20, 4, backend)
	if err != nil {
		t.Fatal(err)
	}
	width, header := 8, "\x1b_Ga=T,"
	if backend == watchArtworkIterm {
		width, header = 20, "\x1b]1337;File="
		payload := lines[len(lines)-1]
		if !strings.Contains(payload, "width=20;height=4;") || !strings.Contains(payload, ";inline=1:") ||
			strings.Contains(payload, "preserveAspectRatio=0") {
			t.Fatal("iTerm image must fit its bounding box without stretching")
		}
	}
	for _, line := range lines {
		if ansi.StringWidth(line) != width || ansi.Truncate(line, width, "") != line {
			t.Fatal("terminal parser would clip the image payload")
		}
	}
	output := &watchTestOutput{writes: make(chan string, 64)}
	program := tea.NewProgram(artworkProofModel{lines: lines, step: "one"},
		tea.WithInput(nil), tea.WithOutput(output), tea.WithAltScreen(), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	t.Cleanup(func() {
		program.Kill()
		if err := <-done; err != nil && !errors.Is(err, tea.ErrProgramKilled) {
			t.Errorf("stopping artwork proof: %v", err)
		}
	})
	initial := output.wait(t, "progress: one")
	if backend == watchArtworkIterm {
		assertWatchItermImageNotOverwritten(t, initial, width, len(lines))
	}
	program.Send("two")
	frame := output.wait(t, "progress: two")
	if strings.Contains(frame, header) || strings.Contains(frame, "\x1b[20X") ||
		!strings.Contains(frame, "\x1b[H"+strings.Repeat("\n", 4)) {
		t.Fatalf("text update touched native artwork: %q", frame)
	}
	if got := strings.Count(output.text(), header); got != 1 {
		t.Fatalf("image transmitted %d times, want one", got)
	}
	if backend == watchArtworkIterm {
		replacement, err := renderWatchArtwork(
			watchArtwork{image: image.NewRGBA(image.Rect(0, 0, 3, 2))}, 20, 4, backend)
		if err != nil {
			t.Fatal(err)
		}
		program.Send(replacement)
		assertWatchItermImageNotOverwritten(t, output.wait(t, header), width, len(replacement))
	}
}

func assertWatchItermImageNotOverwritten(t *testing.T, frame string, width, height int) {
	t.Helper()
	p := ansi.NewParser()
	var state byte
	var x, y, savedX, savedY int
	placed := false
	region := image.Rect(0, 0, width, height)
	for len(frame) > 0 {
		seq, cells, n, next := ansi.DecodeSequence(frame, state, p)
		frame, state = frame[n:], next
		switch {
		case strings.HasPrefix(seq, "\x1b]1337;File="):
			if x != 0 || y != 0 {
				t.Fatalf("iTerm image placed at (%d,%d), want top left", x, y)
			}
			placed = true
		case seq == "\x1b7":
			savedX, savedY = x, y
		case seq == "\x1b8":
			x, y = savedX, savedY
		case seq == "\r":
			x = 0
		case seq == "\n":
			y++
		case strings.HasPrefix(seq, "\x1b["):
			param, _ := p.Param(0, 1)
			switch byte(p.Command()) {
			case 'H':
				y = param - 1
				col, _ := p.Param(1, 1)
				x = col - 1
			case 'A', 'B', 'C', 'D':
				delta := map[byte]image.Point{
					'A': {Y: -param}, 'B': {Y: param}, 'C': {X: param}, 'D': {X: -param},
				}[byte(p.Command())]
				x, y = x+delta.X, y+delta.Y
			case 'K', 'X':
				if placed && image.Pt(x, y).In(region) {
					t.Fatalf("erase sequence %q overwrote iTerm image at (%d,%d)", seq, x, y)
				}
			}
		case cells > 0:
			if placed && image.Pt(x, y).In(region) {
				t.Fatalf("text %q overwrote iTerm image at (%d,%d)", seq, x, y)
			}
			x += cells
		}
	}
	if !placed {
		t.Fatal("iTerm image was not transmitted")
	}
}

func TestWatchArtworkBackendSelection(t *testing.T) {
	for _, name := range []string{"TERM_PROGRAM", "TERM", "TMUX", "LC_TERMINAL", "KITTY_WINDOW_ID"} {
		t.Setenv(name, "")
	}
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	if got := detectWatchArtworkBackend(false); got != watchArtworkIterm {
		t.Fatalf("iTerm2 selected backend %d", got)
	}
	if got := detectWatchArtworkBackend(true); got != watchArtworkASCII {
		t.Fatalf("--ascii selected backend %d", got)
	}
	t.Setenv("TMUX", "/tmp/test-tmux")
	if got := detectWatchArtworkBackend(false); got != watchArtworkASCII {
		t.Fatalf("tmux selected backend %d", got)
	}
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "rex")
	if got := detectWatchArtworkBackend(false); got != watchArtworkKitty {
		t.Fatalf("Rex selected backend %d", got)
	}
	t.Setenv("TERM_PROGRAM", "unknown")
	if got := detectWatchArtworkBackend(false); got != watchArtworkASCII {
		t.Fatalf("unsupported terminal selected backend %d", got)
	}
}

func TestWatchArtworkPreservesAspectRatio(t *testing.T) {
	for _, tt := range []struct {
		name                    string
		imageWidth, imageHeight int
		width, height           int
		placement               string
	}{
		{"square", 100, 100, 24, 12, "r=12"},
		{"landscape", 400, 100, 48, 6, "c=48"},
		{"portrait", 100, 200, 12, 12, "r=12"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			art := watchArtwork{image: image.NewRGBA(image.Rect(0, 0, tt.imageWidth, tt.imageHeight))}
			for _, backend := range []watchArtworkBackend{watchArtworkASCII, watchArtworkKitty} {
				lines, err := renderWatchArtwork(art, 48, 12, backend)
				if err != nil {
					t.Fatal(err)
				}
				if len(lines) != tt.height || ansi.StringWidth(lines[0]) != tt.width {
					t.Fatalf("backend=%d: got %dx%d cells, want %dx%d",
						backend, ansi.StringWidth(lines[0]), len(lines), tt.width, tt.height)
				}
				if backend == watchArtworkKitty {
					payload := strings.Join(lines, "\n")
					if !strings.Contains(payload, tt.placement) ||
						(strings.Contains(payload, ",c=") && strings.Contains(payload, ",r=")) {
						t.Fatalf("native placement must constrain only %s: %q", tt.placement, payload)
					}
				}
			}
		})
	}
}

func TestWatchArtworkCachesContentAndLayout(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(pngData.Bytes())
	}))
	defer server.Close()
	model := &watchModel{ctx: context.Background(), backend: watchArtworkKitty, width: 80, height: 30,
		snapshot: &statusSnapshot{Data: statusResponse{Playback: &playbackStatus{
			Track: &trackStatus{Title: "first", ImageURL: server.URL},
		}}}}
	t.Cleanup(func() {
		if model.artCancel != nil {
			model.artCancel()
		}
	})
	fetch := model.loadArtwork()
	_, render := model.Update(fetch())
	model.Update(render())
	first := strings.Join(model.artLines, "\n")
	if !strings.Contains(first, "\x1b_Ga=T,") {
		t.Fatal("native image was not prepared")
	}
	model.snapshot.Data.Volume++
	if model.loadArtwork() != nil {
		t.Fatal("text change fetched or re-encoded artwork")
	}
	model.snapshot.Data.Playback.Track.Title = "second"
	_, command := model.Update(model.loadArtwork()())
	if command != nil || strings.Join(model.artLines, "\n") != first {
		t.Fatal("identical artwork at a reused URL was retransmitted")
	}
	_, resize := model.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	model.Update(resize())
	if strings.Join(model.artLines, "\n") == first || requests.Load() != 2 {
		t.Fatalf("resize did not use cached image: requests=%d", requests.Load())
	}
	model.snapshot.Data.Playback = nil
	if model.loadArtwork() != nil || len(model.artLines) != 0 || !strings.Contains(model.View(), clearWatchImage) {
		t.Fatal("stopped playback did not remove the native placement")
	}
}

func TestWatchArtworkFailuresAndCancellation(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusOK} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("not an image"))
		}))
		_, err := fetchWatchArtwork(context.Background(), server.URL)
		server.Close()
		if err == nil {
			t.Fatalf("HTTP %d invalid artwork was accepted", status)
		}
	}
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := fetchWatchArtwork(ctx, server.URL); done <- err }()
	receiveWatch(t, started)
	cancel()
	if err := receiveWatch(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("image fetch did not honor cancellation: %v", err)
	}
	model := &watchModel{ctx: context.Background(), minimal: true,
		snapshot: &statusSnapshot{Data: statusResponse{Playback: &playbackStatus{
			Track: &trackStatus{ImageURL: server.URL},
		}}}}
	if model.loadArtwork() != nil {
		t.Fatal("minimal mode fetched an image")
	}
}

func TestWatchUIRetainsArtworkDuringTextUpdates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines, err := renderWatchArtwork(watchArtwork{image: image.NewRGBA(image.Rect(0, 0, 2, 2))}, 20, 4, watchArtworkKitty)
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan watchUpdate, 1)
	snapshot := statusSnapshot{Data: statusResponse{Volume: 25, Playback: &playbackStatus{
		Track: &trackStatus{Title: "Song", ImageURL: "http://example.test/art"},
	}}}
	model := &watchModel{ctx: ctx, updates: updates, done: make(chan error),
		snapshot: &snapshot, width: 80, height: 24, backend: watchArtworkKitty, artLines: lines}
	// Set the loaded track identity without performing the returned HTTP command.
	model.loadArtwork()
	defer model.artCancel()
	output := &watchTestOutput{writes: make(chan string, 64)}
	program := tea.NewProgram(model, tea.WithInput(nil), tea.WithOutput(output),
		tea.WithAltScreen(), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	t.Cleanup(func() {
		cancel()
		program.Kill()
		if err := receiveWatch(t, done); err != nil && !errors.Is(err, tea.ErrProgramKilled) {
			t.Errorf("stopping watch UI: %v", err)
		}
	})
	output.wait(t, "Volume: 25")
	changed := snapshot
	changed.Data.Volume = 26
	updates <- watchUpdate{kind: watchSnapshotUpdate, snapshot: changed}
	frame := output.wait(t, "Volume: 26")
	if strings.Contains(frame, "\x1b_G") || !strings.Contains(frame, "\x1b[H"+strings.Repeat("\n", 4)) {
		t.Fatalf("watch text update repainted artwork: %q", frame)
	}
	changed.Data.Playback = nil
	updates <- watchUpdate{kind: watchSnapshotUpdate, snapshot: changed}
	frame = output.wait(t, clearWatchImage)
	if strings.Contains(frame, "\x1b_Ga=T,") {
		t.Fatal("removing artwork retransmitted the image")
	}
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	output.wait(t, "\x1b[?1049l")
}

func TestWatchArtworkRetriesFailuresAndIgnoresOldTracks(t *testing.T) {
	model := &watchModel{ctx: context.Background(), artKey: "current"}
	if command := model.acceptArtwork(watchArtworkMsg{key: "old", err: errors.New("old failure")}); command != nil {
		t.Fatal("outdated artwork scheduled a retry")
	}
	if command := model.acceptArtwork(watchArtworkMsg{key: "current", err: errors.New("offline")}); command == nil {
		t.Fatal("artwork failure did not schedule recovery")
	}
	if model.artErr == nil {
		t.Fatal("artwork failure was hidden")
	}
	model.snapshot = &statusSnapshot{Data: statusResponse{Playback: &playbackStatus{
		Track: &trackStatus{ImageURL: "http://example.test/art"},
	}}}
	_, command := model.Update(watchArtworkRetryMsg{key: "current"})
	if command == nil {
		t.Fatal("artwork recovery did not request another image")
	}
	model.artCancel()
}
