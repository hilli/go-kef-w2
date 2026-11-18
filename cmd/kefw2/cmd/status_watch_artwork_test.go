// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

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

func (w *watchTestOutput) waitSince(t *testing.T, offset int, contains string) string {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		text := w.text()
		if len(text) >= offset && strings.Contains(text[offset:], contains) {
			return text[offset:]
		}
		select {
		case <-w.writes:
		case <-timeout.C:
			t.Fatalf("no output containing %q after offset %d: %q", contains, offset, text)
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	header := "\x1b_Ga=T,"
	if backend == watchArtworkIterm {
		header = "\x1b]1337;File="
	}
	output := &watchTestOutput{writes: make(chan string, 64)}
	writer := &watchArtworkWriter{output: output, cancel: cancel}
	updates := make(chan watchUpdate, 1)
	snapshot := statusSnapshot{Data: statusResponse{Volume: 25, Playback: &playbackStatus{
		Track: &trackStatus{Title: "Song", ImageURL: "http://example.test/art"},
	}}}
	model := &watchModel{
		ctx: ctx, updates: updates, done: make(chan error), snapshot: &snapshot,
		width: 40, height: 18, backend: backend, terminal: writer,
		art: watchArtwork{image: image.NewRGBA(image.Rect(0, 0, 2, 2))},
	}
	model.loadArtwork()
	artKey := model.artKey
	defer model.artCancel()
	program := tea.NewProgram(model, tea.WithInput(nil), tea.WithOutput(writer),
		tea.WithWindowSize(40, 18), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	t.Cleanup(func() {
		cancel()
		program.Kill()
		if err := receiveWatch(t, done); err != nil && !errors.Is(err, tea.ErrProgramKilled) {
			t.Errorf("stopping artwork proof: %v", err)
		}
	})
	initial := output.waitSince(t, 0, header)
	if backend == watchArtworkIterm {
		if !strings.Contains(initial, "width=40;height=4;") || !strings.Contains(initial, ";inline=1:") ||
			strings.Contains(initial, "preserveAspectRatio=0") {
			t.Fatal("iTerm image must fit its bounding box without stretching")
		}
		assertWatchItermImageNotOverwritten(t, initial, 40, 4)
	}
	offset := len(output.text())
	changed := snapshot
	changed.Data.Volume = 26
	updates <- watchUpdate{kind: watchSnapshotUpdate, snapshot: changed}
	frame := output.waitSince(t, offset, "\x1b[7;10H6")
	if strings.Contains(frame, header) {
		t.Fatalf("text update touched native artwork: %q", frame)
	}
	if got := strings.Count(output.text(), header); got != 1 {
		t.Fatalf("image transmitted %d times, want one", got)
	}
	if backend == watchArtworkIterm {
		assertWatchItermImageNotOverwritten(t, initial+frame, 40, 4)
	}
	offset = len(output.text())
	program.Send(watchArtworkMsg{key: artKey, art: watchArtwork{
		image: image.NewRGBA(image.Rect(0, 0, 3, 2)), digest: [32]byte{1},
	}})
	replacement := output.waitSince(t, offset, header)
	if backend == watchArtworkIterm {
		assertWatchItermImageNotOverwritten(t, replacement, 40, 4)
	}
	offset = len(output.text())
	program.Send(tea.WindowSizeMsg{Width: 30, Height: 20})
	resized := output.waitSince(t, offset, header)
	if backend == watchArtworkIterm {
		if !strings.Contains(resized, "width=30;height=6;") {
			t.Fatalf("iTerm image did not resize: %q", resized)
		}
		assertWatchItermImageNotOverwritten(t, resized, 30, 6)
	}
	offset = len(output.text())
	changed.Data.Playback = nil
	updates <- watchUpdate{kind: watchSnapshotUpdate, snapshot: changed}
	if backend == watchArtworkKitty {
		output.waitSince(t, offset, clearWatchImage)
	} else {
		output.waitSince(t, offset, "\x1b[2J")
	}
	program.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	output.waitSince(t, offset, "\x1b[?1049l")
	text := output.text()
	if tail := text[strings.LastIndex(text, "\x1b[?1049l"):]; strings.Contains(tail, header) {
		t.Fatal("artwork was emitted after leaving the alternate screen")
	}
}

type watchItermProbe struct {
	t              *testing.T
	parser         *ansi.Parser
	region         image.Rectangle
	x, y           int
	savedX, savedY int
	placed         bool
}

func assertWatchItermImageNotOverwritten(t *testing.T, frame string, width, height int) {
	t.Helper()
	probe := watchItermProbe{t: t, parser: ansi.NewParser(), region: image.Rect(0, 0, width, height)}
	var state byte
	for len(frame) > 0 {
		seq, cells, n, next := ansi.DecodeSequence(frame, state, probe.parser)
		frame, state = frame[n:], next
		probe.consume(seq, cells)
	}
	if !probe.placed {
		t.Fatal("iTerm image was not transmitted")
	}
}

func (p *watchItermProbe) consume(seq string, cells int) {
	p.t.Helper()
	switch {
	case strings.HasPrefix(seq, "\x1b]1337;File="):
		if p.x != 0 || p.y != 0 {
			p.t.Fatalf("iTerm image placed at (%d,%d), want top left", p.x, p.y)
		}
		p.placed = true
	case seq == "\x1b7":
		p.savedX, p.savedY = p.x, p.y
	case seq == "\x1b8":
		p.x, p.y = p.savedX, p.savedY
	case seq == "\r":
		p.x = 0
	case seq == "\n":
		p.y++
	case strings.HasPrefix(seq, "\x1b["):
		p.consumeCSI(seq)
	case cells > 0:
		p.checkDamage(seq, image.Pt(p.x, p.y).In(p.region))
		p.x += cells
	}
}

func (p *watchItermProbe) consumeCSI(seq string) {
	p.t.Helper()
	param, _ := p.parser.Param(0, 1)
	switch byte(p.parser.Command()) {
	case 'H', 'f':
		p.y = param - 1
		col, _ := p.parser.Param(1, 1)
		p.x = col - 1
	case 'd':
		p.y = param - 1
	case 'G':
		p.x = param - 1
	case 'A', 'B', 'C', 'D':
		delta := map[byte]image.Point{
			'A': {Y: -param}, 'B': {Y: param}, 'C': {X: param}, 'D': {X: -param},
		}[byte(p.parser.Command())]
		p.x, p.y = p.x+delta.X, p.y+delta.Y
	case 'J', 'K', 'X':
		p.checkErase(seq, byte(p.parser.Command()), param)
	}
}

func (p *watchItermProbe) checkErase(seq string, command byte, param int) {
	p.t.Helper()
	switch command {
	case 'J':
		mode, _ := p.parser.Param(0, 0)
		p.checkDamage(seq, mode == 2 || mode == 3 || (mode == 0 && p.y < p.region.Dy()) || (mode == 1 && p.y >= 0))
	case 'K':
		mode, _ := p.parser.Param(0, 0)
		row := p.y >= 0 && p.y < p.region.Dy()
		p.checkDamage(seq, row && (mode == 2 || (mode == 0 && p.x < p.region.Dx()) || (mode == 1 && p.x >= 0)))
	case 'X':
		p.checkDamage(seq, p.y >= 0 && p.y < p.region.Dy() && p.x < p.region.Dx() && p.x+param > 0)
	}
}

func (p *watchItermProbe) checkDamage(seq string, touchesImage bool) {
	p.t.Helper()
	if p.placed && touchesImage {
		p.t.Fatalf("terminal output %q overwrote iTerm image at (%d,%d)", seq, p.x, p.y)
	}
}

func TestWatchArtworkWriterOrdersAndInvalidatesFrames(t *testing.T) {
	var output bytes.Buffer
	writer := &watchArtworkWriter{output: &output}
	write := func(data string) {
		t.Helper()
		if n, err := writer.Write([]byte(data)); err != nil || n != len(data) {
			t.Fatalf("writing %q: n=%d err=%v", data, n, err)
		}
	}
	first := writer.queue("first-image")
	write(first.String())
	write("text-before-redraw")
	if strings.Contains(output.String(), "first-image") {
		t.Fatal("image was sent before the reserved cells were redrawn")
	}
	write("\x1b[2Jcells")
	if !strings.HasSuffix(output.String(), "cellsfirst-image") {
		t.Fatal("image was not sent after the redraw")
	}
	write("text-only")
	if got := strings.Count(output.String(), "first-image"); got != 1 {
		t.Fatalf("text-only output transmitted the image %d times", got)
	}
	write("\x1b[2Jdamaged")
	if got := strings.Count(output.String(), "first-image"); got != 2 {
		t.Fatalf("screen damage did not restore the image: %d transmissions", got)
	}
	second := writer.queue("second-image")
	write(first.String())
	write("\x1b[2Jstale")
	if strings.Contains(output.String(), "second-image") {
		t.Fatal("stale marker drew the new image")
	}
	write(second.String())
	write("\x1b[2Jnew")
	if !strings.HasSuffix(output.String(), "newsecond-image") {
		t.Fatal("current marker did not draw the replacement")
	}
	writer.invalidate()
	write(second.String())
	write("\x1b[2Jcleared")
	if !strings.HasSuffix(output.String(), "cleared") {
		t.Fatal("invalidated artwork was redrawn")
	}
	if strings.Contains(output.String(), watchArtworkMarkerPrefix) {
		t.Fatal("internal artwork marker leaked to the terminal")
	}
}

func TestWatchArtworkWriterReportsOutputFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := &watchArtworkWriter{output: watchShortWriter{}, cancel: cancel}
	if _, err := writer.Write([]byte("text")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write was not reported: %v", err)
	}
	if !errors.Is(writer.failure(), io.ErrShortWrite) || ctx.Err() == nil {
		t.Fatal("output failure did not cancel the watch")
	}
}

type watchShortWriter struct{}

func (watchShortWriter) Write([]byte) (int, error) { return 0, nil }

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
				rendered, err := renderWatchArtwork(art, 48, 12, backend)
				if err != nil {
					t.Fatal(err)
				}
				if len(rendered.lines) != tt.height || ansi.StringWidth(rendered.lines[0]) != tt.width {
					t.Fatalf("backend=%d: got %dx%d cells, want %dx%d",
						backend, ansi.StringWidth(rendered.lines[0]), len(rendered.lines), tt.width, tt.height)
				}
				if backend == watchArtworkKitty {
					payload := rendered.native
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
		terminal: &watchArtworkWriter{output: io.Discard},
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
	first := model.terminal.payload
	if !strings.Contains(first, "\x1b_Ga=T,") {
		t.Fatal("native image was not prepared")
	}
	model.snapshot.Data.Volume++
	if model.loadArtwork() != nil {
		t.Fatal("text change fetched or re-encoded artwork")
	}
	model.snapshot.Data.Playback.Track.Title = "second"
	_, command := model.Update(model.loadArtwork()())
	if command != nil || model.terminal.payload != first {
		t.Fatal("identical artwork at a reused URL was retransmitted")
	}
	_, resize := model.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	model.Update(resize())
	if model.terminal.payload == first || requests.Load() != 2 {
		t.Fatalf("resize did not use cached image: requests=%d", requests.Load())
	}
	model.snapshot.Data.Playback = nil
	if model.loadArtwork() == nil || len(model.artLines) != 0 || model.terminal.payload != "" {
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
