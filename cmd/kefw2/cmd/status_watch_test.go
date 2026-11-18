// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func receiveWatch[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("watch operation did not complete")
		var zero T
		return zero
	}
}

func watchUpdateOfKind(t *testing.T, updates <-chan watchUpdate, kind string) watchUpdate {
	t.Helper()
	for {
		update := receiveWatch(t, updates)
		if update.kind == kind {
			return update
		}
	}
}

func TestWatchEventDrivenRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	refresh := make(chan chan<- struct{}, 1)
	health := make(chan chan<- error, 1)
	updates := make(chan watchUpdate, 32)
	ticks := make(chan time.Time, 1)
	scheduled := make(chan struct{}, 16)
	var reads atomic.Int32
	var fail atomic.Bool
	deps := watchDependencies{
		read: func(context.Context) (statusSnapshot, error) {
			count := reads.Add(1)
			if fail.Load() {
				return statusSnapshot{}, errors.New("offline")
			}
			return statusSnapshot{Data: statusResponse{Volume: int(count)}}, nil
		},
		events: func(ctx context.Context, r chan<- struct{}, h chan<- error) {
			refresh <- r
			health <- h
			<-ctx.Done()
		},
		after: func(time.Duration) <-chan time.Time { scheduled <- struct{}{}; return ticks },
	}
	done := make(chan error, 1)
	go func() { done <- watchStatus(ctx, deps, nil, func(u watchUpdate) error { updates <- u; return nil }) }()
	t.Cleanup(func() {
		cancel()
		if err := receiveWatch(t, done); err != nil {
			t.Errorf("watch shutdown: %v", err)
		}
	})
	if got := watchUpdateOfKind(t, updates, "snapshot").snapshot.Data.Volume; got != 1 {
		t.Fatalf("no immediate initial snapshot: %d", got)
	}
	r, h := receiveWatch(t, refresh), receiveWatch(t, health)
	receiveWatch(t, scheduled)
	h <- nil
	watchUpdateOfKind(t, updates, "snapshot")
	before := reads.Load()
	ticks <- time.Now()
	select {
	case u := <-updates:
		t.Fatalf("healthy idle watch produced an update: %+v", u)
	case <-time.After(50 * time.Millisecond):
	}
	if reads.Load() != before {
		t.Fatal("healthy watch polled status")
	}
	signalWatchRefresh(r)
	watchUpdateOfKind(t, updates, "snapshot")
	h <- errors.New("events offline")
	watchUpdateOfKind(t, updates, "event connection")
	receiveWatch(t, scheduled)
	// The tick left on the disabled healthy timer now becomes a recovery tick.
	watchUpdateOfKind(t, updates, "snapshot")
	receiveWatch(t, scheduled)
	fail.Store(true)
	ticks <- time.Now()
	if u := watchUpdateOfKind(t, updates, "status unavailable"); u.err == nil {
		t.Fatal("failed read was not reported")
	}
	receiveWatch(t, scheduled)
	fail.Store(false)
	ticks <- time.Now()
	watchUpdateOfKind(t, updates, "snapshot")
	h <- nil
	watchUpdateOfKind(t, updates, "snapshot")
	for len(updates) > 0 {
		<-updates
	}
	ticks <- time.Now()
	select {
	case u := <-updates:
		t.Fatalf("fallback continued after event recovery: %+v", u)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWatchCoalescesEventsDuringRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	refresh := make(chan chan<- struct{}, 1)
	started := make(chan int, 8)
	release := make(chan struct{})
	updates := make(chan watchUpdate, 8)
	calls := 0
	deps := watchDependencies{
		read: func(ctx context.Context) (statusSnapshot, error) {
			calls++
			started <- calls
			if calls == 2 {
				select {
				case <-release:
				case <-ctx.Done():
					return statusSnapshot{}, ctx.Err()
				}
			}
			return statusSnapshot{}, nil
		},
		events: func(ctx context.Context, r chan<- struct{}, _ chan<- error) { refresh <- r; <-ctx.Done() },
		after:  func(time.Duration) <-chan time.Time { return nil },
	}
	done := make(chan error, 1)
	go func() { done <- watchStatus(ctx, deps, nil, func(u watchUpdate) error { updates <- u; return nil }) }()
	t.Cleanup(func() {
		cancel()
		if err := receiveWatch(t, done); err != nil {
			t.Errorf("watch shutdown: %v", err)
		}
	})
	receiveWatch(t, started)
	r := receiveWatch(t, refresh)
	signalWatchRefresh(r)
	if receiveWatch(t, started) != 2 {
		t.Fatal("event did not trigger a read")
	}
	for range 1000 {
		signalWatchRefresh(r)
	}
	close(release)
	if receiveWatch(t, started) != 3 {
		t.Fatal("event during a read was lost")
	}
	select {
	case count := <-started:
		t.Fatalf("burst produced extra read %d", count)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWatchCancellationAndOutputFailure(t *testing.T) {
	t.Run("blocked read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- watchStatus(ctx, watchDependencies{
				read: func(ctx context.Context) (statusSnapshot, error) {
					close(started)
					<-ctx.Done()
					return statusSnapshot{}, ctx.Err()
				},
			}, nil, func(watchUpdate) error { t.Error("published canceled read"); return nil })
		}()
		receiveWatch(t, started)
		cancel()
		if err := receiveWatch(t, done); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("output failure", func(t *testing.T) {
		failure := errors.New("broken output")
		calls := 0
		err := watchStatus(context.Background(), watchDependencies{
			read: func(context.Context) (statusSnapshot, error) { calls++; return statusSnapshot{}, nil },
		}, nil, func(watchUpdate) error { return failure })
		if !errors.Is(err, failure) || calls != 1 {
			t.Fatalf("output failure was retried: calls=%d err=%v", calls, err)
		}
	})
}

func startWatchJSONProcess(t *testing.T, home, config string) (*exec.Cmd, io.ReadCloser, *watchTestOutput) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	process := exec.CommandContext(ctx, executable, "-test.run=^TestCLIProcess$", "--", "--config", config,
		"status", "watch", "--json")
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "KEFW2_") && key != "HOME" && key != "XDG_CACHE_HOME" && key != "XDG_CONFIG_HOME" {
			process.Env = append(process.Env, entry)
		}
	}
	process.Env = append(process.Env, "KEFW2_TEST_PROCESS=1", "HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, "config"), "XDG_CACHE_HOME="+filepath.Join(home, "cache"))
	process.Stdin = strings.NewReader("n m + \x1b[C")
	stderr := &watchTestOutput{}
	process.Stderr = stderr
	stdout, err := process.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	return process, stdout, stderr
}

func TestWatchCLIJSON(t *testing.T) {
	f, speaker := newCLIFixture(t)
	var artRequests atomic.Int32
	artServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		artRequests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer artServer.Close()
	f.data["player:player/data"] = `[{"state":"playing","trackRoles":{"title":"Song","icon":"` + artServer.URL + `"}}]`
	events := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/event/modifyQueue":
			_, _ = io.WriteString(w, `"watch-queue"`)
		case "/api/event/pollQueue":
			select {
			case event := <-events:
				_, _ = io.WriteString(w, event)
			case <-r.Context().Done():
			}
		default:
			f.serveHTTP(w, r)
		}
	}))
	defer server.Close()
	speaker.IPAddress = strings.TrimPrefix(server.URL, "http://")
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	process, stdout, stderr := startWatchJSONProcess(t, home, config)
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = process.Process.Kill()
			_ = process.Wait()
		}
	})
	type response struct {
		Speaker speakerIdentity `json:"speaker"`
		Data    statusResponse  `json:"data"`
	}
	responses := make(chan response, 8)
	decodeErrors := make(chan error, 1)
	go func() {
		decoder := json.NewDecoder(stdout)
		for {
			var result response
			if err := decoder.Decode(&result); err != nil {
				decodeErrors <- err
				return
			}
			responses <- result
		}
	}()
	for range 2 {
		result := receiveWatch(t, responses)
		if result.Speaker.IPAddress != speaker.IPAddress || result.Data.Playback.Track.ImageURL != artServer.URL {
			t.Fatalf("incomplete snapshot: %+v", result)
		}
	}
	f.mu.Lock()
	f.data["player:volume"] = `[{"type":"i32_","i32_":31}]`
	f.mu.Unlock()
	events <- `[{"path":"player:volume","itemValue":{"type":"i32_","i32_":31}}]`
	if result := receiveWatch(t, responses); result.Data.Volume != 31 {
		t.Fatalf("event did not refresh complete status: %+v", result)
	}
	if err := process.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	if err := receiveWatch(t, done); err != nil {
		t.Fatalf("watch exit: %v stderr=%s", err, stderr.text())
	}
	waited = true
	if err := receiveWatch(t, decodeErrors); !errors.Is(err, io.EOF) {
		t.Fatalf("stream contained non-JSON output: %v", err)
	}
	if stderr.text() != "" || artRequests.Load() != 0 || len(f.postRequests()) != 0 {
		t.Fatalf("JSON watch produced side effects: stderr=%q images=%d posts=%v",
			stderr.text(), artRequests.Load(), f.postRequests())
	}
}

func TestWatchCLIFlagsAndTerminal(t *testing.T) {
	_, speaker := newCLIFixture(t)
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	for _, flag := range []string{"--interval", "--sleep", "--watch"} {
		stdout, stderr, err := runCLI(t, home, config, "status", "watch", flag)
		requireCLIFailure(t, stdout, stderr, err, "unknown flag")
	}
	stdout, stderr, err := runCLI(t, home, config, "status", "watch")
	requireCLIFailure(t, stdout, stderr, err, "requires a terminal")
}

func TestStatusImageURL(t *testing.T) {
	for _, url := range []string{"", "http://invalid.local/cover.png"} {
		for _, state := range []string{"playing", "paused", "stopped"} {
			t.Run(state+"/"+url, func(t *testing.T) {
				f, speaker := newCLIFixture(t)
				f.data["player:player/data"] = fmt.Sprintf(`[{"state":%q,"trackRoles":{"icon":%q}}]`, state, url)
				home := t.TempDir()
				config := writeCLIConfig(t, home, speaker)
				stdout, stderr, err := runCLI(t, home, config, "status", "--json")
				result := decodeCLI[struct{ Data statusResponse }](t, stdout, stderr, err)
				if state == "stopped" {
					if result.Data.Playback.Track != nil {
						t.Fatal("stopped state exposed stale artwork")
					}
				} else if result.Data.Playback.Track.ImageURL != url || !strings.Contains(stdout, `"image_url":`) {
					t.Fatalf("artwork field missing or incorrect: %s", stdout)
				}
			})
		}
	}
}
