// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package kefw2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type airableTestContextKey struct{}

type airableProbeTransport struct {
	started  chan struct{}
	calls    atomic.Int32
	closed   atomic.Int32
	sawValue atomic.Bool
}

func (t *airableProbeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	t.sawValue.Store(req.Context().Value(airableTestContextKey{}) == "operation")
	close(t.started)
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func (t *airableProbeTransport) CloseIdleConnections() {
	t.closed.Add(1)
}

type airableLegacyTransport struct {
	started  chan struct{}
	canceled chan struct{}
	once     sync.Once
}

func (t *airableLegacyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	close(t.started)
	<-t.canceled
	return nil, errors.New("legacy transport canceled")
}

func (t *airableLegacyTransport) CancelRequest(*http.Request) {
	t.once.Do(func() { close(t.canceled) })
}

type airableObservedTransport struct {
	transport http.RoundTripper
	contexts  chan context.Context
}

func (t *airableObservedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Query().Get("path") == "playlists:pq/getitems" {
		t.contexts <- req.Context()
	}
	return t.transport.RoundTrip(req)
}

type airableTrackedContext struct {
	context.Context
	registered atomic.Int32
	stopped    atomic.Int32
}

func (c *airableTrackedContext) Value(any) any { return nil }

func (c *airableTrackedContext) AfterFunc(func()) func() bool {
	c.registered.Add(1)
	return func() bool {
		c.stopped.Add(1)
		return true
	}
}

func waitAirableSignal(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func waitAirableError(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("canceled operation did not return")
		return nil
	}
}

func TestAirableContextTransportOptionOrder(t *testing.T) {
	orders := [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
	for _, replaceClient := range []bool{false, true} {
		for _, timeout := range []time.Duration{0, 3 * time.Second, 10 * time.Second} {
			for _, order := range orders {
				t.Run(fmt.Sprintf("replace=%t/timeout=%s/order=%v", replaceClient, timeout, order), func(t *testing.T) {
					probe := &airableProbeTransport{started: make(chan struct{})}
					supplied := &http.Client{Transport: probe, Timeout: 17 * time.Second}
					parent := context.WithValue(context.Background(), airableTestContextKey{}, "operation")
					ctx, cancel := context.WithCancel(parent)
					defer cancel()
					custom := AirableClientOption(func(a *AirableClient) {
						if replaceClient {
							a.HTTPClient = supplied
						} else {
							a.HTTPClient.Transport = probe
						}
					})
					options := [3]AirableClientOption{WithContext(ctx), WithHTTPTimeout(timeout), custom}
					client := NewAirableClient(&KEFSpeaker{IPAddress: "speaker.invalid"},
						options[order[0]], options[order[1]], options[order[2]])
					wantTimeout := timeout
					if replaceClient {
						for _, option := range order {
							switch option {
							case 1:
								wantTimeout = timeout
							case 2:
								wantTimeout = supplied.Timeout
							}
						}
					}
					if client.HTTPClient.Timeout != wantTimeout {
						t.Fatalf("timeout = %s, want %s", client.HTTPClient.Timeout, wantTimeout)
					}
					result := make(chan error, 1)
					go func() {
						_, err := client.GetRows("test:path", 0, 1)
						result <- err
					}()
					waitAirableSignal(t, probe.started, "custom transport")
					cancel()
					if err := waitAirableError(t, result); !errors.Is(err, context.Canceled) {
						t.Fatalf("request returned %v, want cancellation", err)
					}
					if probe.calls.Load() != 1 || !probe.sawValue.Load() {
						t.Fatal("custom transport did not receive the operation context")
					}
					client.HTTPClient.CloseIdleConnections()
					if probe.closed.Load() != 1 {
						t.Fatal("CloseIdleConnections was not forwarded to the custom transport")
					}
					if replaceClient && (supplied.Timeout != 17*time.Second || supplied.Transport != probe) {
						t.Fatal("constructor modified the supplied HTTP client")
					}
				})
			}
		}
	}
}

func TestAirableHTTPTimeoutLastOptionWins(t *testing.T) {
	for _, withContext := range []bool{false, true} {
		for _, timeoutLast := range []bool{false, true} {
			for _, replaceClient := range []bool{false, true} {
				t.Run(fmt.Sprintf("context=%t/timeoutLast=%t/replace=%t", withContext, timeoutLast, replaceClient),
					func(t *testing.T) {
						custom := AirableClientOption(func(a *AirableClient) {
							if replaceClient {
								a.HTTPClient = &http.Client{Timeout: 17 * time.Second}
							} else {
								a.HTTPClient.Timeout = 17 * time.Second
							}
						})
						options := []AirableClientOption{WithHTTPTimeout(3 * time.Second), custom}
						want := 17 * time.Second
						if timeoutLast {
							options[0], options[1] = options[1], options[0]
							want = 3 * time.Second
						}
						if withContext {
							options = append(options, WithContext(context.Background()))
						}
						client := NewAirableClient(&KEFSpeaker{IPAddress: "speaker.invalid"}, options...)
						if client.HTTPClient.Timeout != want {
							t.Fatalf("timeout = %s, want %s", client.HTTPClient.Timeout, want)
						}
					})
			}
		}
	}
}

func TestAirableContextTransportKeepsCustomClientTimeout(t *testing.T) {
	for _, contextFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(contextFirst), func(t *testing.T) {
			probe := &airableProbeTransport{started: make(chan struct{})}
			supplied := &http.Client{Transport: probe, Timeout: 17 * time.Second}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			custom := AirableClientOption(func(a *AirableClient) { a.HTTPClient = supplied })
			options := []AirableClientOption{custom, WithContext(ctx)}
			if contextFirst {
				options[0], options[1] = options[1], options[0]
			}
			client := NewAirableClient(&KEFSpeaker{IPAddress: "speaker.invalid"}, options...)
			if client.HTTPClient.Timeout != supplied.Timeout || supplied.Transport != probe {
				t.Fatal("custom HTTP client configuration was not preserved")
			}
			result := make(chan error, 1)
			go func() {
				_, err := client.GetRows("test:path", 0, 1)
				result <- err
			}()
			waitAirableSignal(t, probe.started, "custom HTTP client")
			cancel()
			if err := waitAirableError(t, result); !errors.Is(err, context.Canceled) {
				t.Fatalf("request returned %v, want cancellation", err)
			}
		})
	}
}

func TestAirableContextOptionOverridesReusedBoundTransport(t *testing.T) {
	for _, rebind := range []bool{false, true} {
		t.Run(fmt.Sprint(rebind), func(t *testing.T) {
			probe := &airableProbeTransport{started: make(chan struct{})}
			oldCtx, cancelOld := context.WithCancel(context.Background())
			other := NewAirableClient(&KEFSpeaker{IPAddress: "speaker.invalid"},
				WithContext(oldCtx), func(a *AirableClient) { a.HTTPClient.Transport = probe })
			if rebind {
				WithContext(oldCtx)(other)
			}
			cancelOld()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := NewAirableClient(&KEFSpeaker{IPAddress: "speaker.invalid"},
				WithContext(ctx), func(a *AirableClient) { a.HTTPClient.Transport = other.HTTPClient.Transport })
			result := make(chan error, 1)
			go func() {
				_, err := client.GetRows("test:path", 0, 1)
				result <- err
			}()
			waitAirableSignal(t, probe.started, "reused custom transport")
			cancel()
			if err := waitAirableError(t, result); !errors.Is(err, context.Canceled) {
				t.Fatalf("request returned %v, want cancellation from the new context", err)
			}
		})
	}
}

func TestAirableContextTransportPreservesTimeoutAndLegacyCancel(t *testing.T) {
	t.Run("context-aware transport", func(t *testing.T) {
		probe := &airableProbeTransport{started: make(chan struct{})}
		client := NewAirableClient(&KEFSpeaker{IPAddress: "speaker.invalid"},
			WithContext(context.Background()),
			func(a *AirableClient) { a.HTTPClient.Transport = probe },
			WithHTTPTimeout(40*time.Millisecond))
		_, err := client.GetRows("test:path", 0, 1)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("HTTP timeout returned %v, want deadline exceeded", err)
		}
	})
	t.Run("legacy transport", func(t *testing.T) {
		probe := &airableLegacyTransport{started: make(chan struct{}), canceled: make(chan struct{})}
		client := NewAirableClient(&KEFSpeaker{IPAddress: "speaker.invalid"},
			func(a *AirableClient) { a.HTTPClient.Transport = probe },
			WithHTTPTimeout(40*time.Millisecond),
			WithContext(context.Background()))
		result := make(chan error, 1)
		go func() {
			_, err := client.GetRows("test:path", 0, 1)
			result <- err
		}()
		waitAirableSignal(t, probe.started, "legacy transport")
		if err := waitAirableError(t, result); err == nil {
			t.Fatal("legacy transport timeout reported success")
		}
		waitAirableSignal(t, probe.canceled, "legacy CancelRequest")
	})
}

func TestAirableContextReboundAfterTransportReplacement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewAirableClient(&KEFSpeaker{IPAddress: "speaker.invalid"}, WithContext(ctx))
	probe := &airableProbeTransport{started: make(chan struct{})}
	client.HTTPClient.Transport = probe
	WithContext(ctx)(client)
	result := make(chan error, 1)
	go func() {
		_, err := client.GetPlayQueue()
		result <- err
	}()
	waitAirableSignal(t, probe.started, "uncached queue transport")
	cancel()
	if err := waitAirableError(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("uncached queue returned %v, want cancellation", err)
	}
}

func TestAirableContextCancelsHTTP(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		streamBody bool
		operation  func(*AirableClient) error
	}{
		{"UPnP browse", http.MethodGet, "/api/getRows", false, func(a *AirableClient) error {
			_, err := a.BrowseContainerPaged("upnp:/album", 10, 19)
			return err
		}},
		{"response body", http.MethodGet, "/api/getRows", true, func(a *AirableClient) error {
			_, err := a.GetRows("upnp:/album", 0, 19)
			return err
		}},
		{"play mode", http.MethodGet, "/api/getData", false, func(a *AirableClient) error {
			_, err := a.GetPlayMode()
			return err
		}},
		{"events", http.MethodGet, "/api/event/pollQueue", false, func(a *AirableClient) error {
			_, err := a.PollEvents(30)
			return err
		}},
		{"podcast favorite", http.MethodPost, "/api/setData", false, func(a *AirableClient) error {
			return a.AddPodcastFavorite(&ContentItem{ID: "airable://airable.feeds/podcast/42"})
		}},
		{"queue move", http.MethodPost, "/api/setData", false, func(a *AirableClient) error {
			return a.MoveQueueItem(0, 1)
		}},
		{"queue remove", http.MethodPost, "/api/setData", false, func(a *AirableClient) error {
			return a.RemoveFromQueue([]int{0})
		}},
		{"queue play", http.MethodPost, "/api/setData", false, func(a *AirableClient) error {
			return a.PlayQueueIndex(0, &ContentItem{Path: "track", Type: ContentTypeAudio})
		}},
		{"queue shuffle", http.MethodPost, "/api/setData", false, func(a *AirableClient) error {
			return a.SetShuffle(true)
		}},
		{"queue repeat", http.MethodPost, "/api/setData", false, func(a *AirableClient) error {
			return a.SetRepeat("all")
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			started := make(chan struct{})
			disconnected := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Query().Get("path") == "settings:/mediaPlayer/playMode" &&
					tc.method == http.MethodPost {
					_, _ = w.Write([]byte(`[{"type":"playerPlayMode","playerPlayMode":"normal"}]`))
					return
				}
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					http.Error(w, "unexpected request", http.StatusNotFound)
					return
				}
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					t.Errorf("read request body: %v", err)
					return
				}
				if tc.streamBody {
					_, _ = w.Write([]byte(`{`))
					w.(http.Flusher).Flush()
				}
				close(started)
				select {
				case <-r.Context().Done():
					close(disconnected)
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := NewAirableClient(
				&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")},
				WithContext(ctx),
			)
			client.PodcastBaseURL = "airable:https://podcast.example"
			result := make(chan error, 1)
			go func() { result <- tc.operation(client) }()

			waitAirableSignal(t, started, "HTTP request")
			cancel()
			if err := waitAirableError(t, result); !errors.Is(err, context.Canceled) {
				t.Fatalf("operation returned %v, want context cancellation", err)
			}
			waitAirableSignal(t, disconnected, "server request cancellation")
		})
	}
}

func TestAirableContextDefaultsAndCache(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"rowsCount":1,"rows":[{"path":"track"}]}`))
	}))
	defer server.Close()
	speaker := &KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")}

	for _, client := range []*AirableClient{
		NewAirableClient(speaker),
		NewAirableClient(speaker, WithContext(nil)), //nolint:staticcheck // Nil is explicitly supported.
		{Speaker: speaker, HTTPClient: &http.Client{Timeout: time.Second}},
	} {
		if _, err := client.GetRows("test:default", 0, 100); err != nil {
			t.Fatalf("legacy or nil-context client failed: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewAirableClient(speaker, WithContext(ctx), WithDefaultCache())
	if _, err := client.GetAllRows("test:cached"); err != nil {
		t.Fatal(err)
	}
	beforeCancel := requests.Load()
	cancel()
	if _, err := client.GetRows("test:cached", 0, 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached page ignored cancellation: %v", err)
	}
	if _, err := client.GetAllRows("test:cached"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached complete response ignored cancellation: %v", err)
	}
	if requests.Load() != beforeCancel {
		t.Fatal("canceled client sent another HTTP request")
	}
}

func TestAirablePaginationCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("from") == "0" {
			_, _ = w.Write([]byte(`{"rowsCount":2,"rows":[{"path":"first"}]}`))
			return
		}
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewAirableClient(
		&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")},
		WithContext(ctx),
	)
	result := make(chan struct {
		rows *RowsResponse
		err  error
	}, 1)
	go func() {
		rows, err := client.GetAllRows("test:pages")
		result <- struct {
			rows *RowsResponse
			err  error
		}{rows, err}
	}()
	waitAirableSignal(t, started, "second page")
	cancel()
	select {
	case got := <-result:
		if got.rows != nil || !errors.Is(got.err, context.Canceled) {
			t.Fatalf("pagination returned partial results or lost cancellation: rows=%+v err=%v", got.rows, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pagination did not stop after cancellation")
	}
}

func TestAirableUPnPRecursiveBrowseCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("path") {
		case "upnp:/root":
			_, _ = w.Write([]byte(`{"rowsCount":2,"rows":[` +
				`{"type":"audio","path":"upnp:/track"},` +
				`{"type":"container","path":"upnp:/child"}]}`))
		case "upnp:/child":
			close(started)
			select {
			case <-r.Context().Done():
			case <-release:
			}
		default:
			t.Errorf("unexpected request: %s", r.URL)
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewAirableClient(
		&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")},
		WithContext(ctx),
	)
	result := make(chan struct {
		tracks []ContentItem
		err    error
	}, 1)
	go func() {
		tracks, err := client.GetContainerTracksRecursive("upnp:/root")
		result <- struct {
			tracks []ContentItem
			err    error
		}{tracks, err}
	}()
	waitAirableSignal(t, started, "nested UPnP container")
	cancel()
	select {
	case got := <-result:
		if got.tracks != nil || !errors.Is(got.err, context.Canceled) {
			t.Fatalf("recursive browse returned partial tracks or lost cancellation: tracks=%+v err=%v",
				got.tracks, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("recursive UPnP browse did not stop after cancellation")
	}
}

func TestRadioActivationStopsDuringSourceChange(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var sourceWrites, otherWrites atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/getRows":
			_, _ = w.Write([]byte(`{"roles":{"path":"station","type":"audio","audioType":"audioBroadcast"}}`))
		case "/api/getData":
			_, _ = w.Write([]byte(`[{"type":"kefPhysicalSource","kefPhysicalSource":"standby"}]`))
		case "/api/setData":
			var payload struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
				return
			}
			if payload.Path != "settings:/kef/play/physicalSource" {
				otherWrites.Add(1)
				_, _ = w.Write([]byte(`{}`))
				return
			}
			sourceWrites.Add(1)
			close(started)
			select {
			case <-r.Context().Done():
			case <-release:
			}
		default:
			t.Errorf("unexpected request: %s", r.URL)
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewAirableClient(
		&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")},
		WithContext(ctx),
	)
	result := make(chan error, 1)
	go func() { result <- client.PlayRadioByPath("station") }()
	waitAirableSignal(t, started, "radio source change")
	cancel()
	if err := waitAirableError(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("radio activation returned %v, want cancellation", err)
	}
	if sourceWrites.Load() != 1 || otherWrites.Load() != 0 {
		t.Fatalf("unexpected writes after cancellation: source=%d playback=%d",
			sourceWrites.Load(), otherWrites.Load())
	}
}

func TestRadioCancellationWhileSourceInitializes(t *testing.T) {
	sourceReady := make(chan struct{})
	var reads, sourceWrites, playbackWrites atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			source := "standby"
			count := reads.Add(1)
			if count > 1 {
				source = "wifi"
			}
			_, _ = fmt.Fprintf(w, `[{"type":"kefPhysicalSource","kefPhysicalSource":%q}]`, source)
			if count == 2 {
				w.(http.Flusher).Flush()
				close(sourceReady)
			}
			return
		}
		var payload struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if payload.Path == "settings:/kef/play/physicalSource" {
			sourceWrites.Add(1)
		} else {
			playbackWrites.Add(1)
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewAirableClient(
		&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")},
		WithContext(ctx),
	)
	result := make(chan error, 1)
	go func() { result <- client.PlayRadioStation(&ContentItem{Path: "station"}) }()
	waitAirableSignal(t, sourceReady, "WiFi source transition")
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("radio initialization returned %v, want cancellation", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("radio initialization did not stop promptly")
	}
	if sourceWrites.Load() != 1 || playbackWrites.Load() != 0 {
		t.Fatalf("unexpected writes after cancellation: source=%d playback=%d",
			sourceWrites.Load(), playbackWrites.Load())
	}
}

func TestAirableSpeakerDeadlinePreserved(t *testing.T) {
	for _, tc := range []struct {
		name      string
		operation func(*AirableClient) error
	}{
		{"radio source", func(a *AirableClient) error {
			return a.PlayRadioStation(&ContentItem{Path: "station"})
		}},
		{"current queue index", func(a *AirableClient) error {
			_, err := a.GetCurrentQueueIndex()
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			client := NewAirableClient(
				&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")},
				WithContext(ctx),
			)
			result := make(chan error, 1)
			go func() { result <- tc.operation(client) }()
			waitAirableSignal(t, started, "speaker request")
			if err := waitAirableError(t, result); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("speaker timeout returned %v, want context deadline", err)
			}
		})
	}
}

func TestAirableQueueAppendWaitCancellation(t *testing.T) {
	polled := make(chan struct{})
	var reads, writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writes.Add(1)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		count := reads.Add(1)
		_, _ = w.Write([]byte(`{"rowsCount":0,"rows":[]}`))
		if count == 2 {
			w.(http.Flusher).Flush()
			close(polled)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewAirableClient(
		&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")},
		WithContext(ctx),
	)
	result := make(chan error, 1)
	go func() {
		result <- client.AddToQueue([]ContentItem{{Path: "track", Type: ContentTypeAudio}}, true)
	}()
	waitAirableSignal(t, polled, "queue poll after append")
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queue wait returned %v, want cancellation", err)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("queue append wait did not stop promptly")
	}
	if reads.Load() != 2 || writes.Load() != 1 {
		t.Fatalf("queue continued after cancellation: reads=%d writes=%d", reads.Load(), writes.Load())
	}
}

func TestPlayOrResumeQueueUsesOperationContext(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bindClient bool
		methodCtx  string
	}{
		{"legacy explicit context", false, "same"},
		{"same bound context", true, "same"},
		{"bound context with background argument", true, "background"},
		{"bound context with nil argument", true, "nil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Query().Get("path") {
				case "settings:/kef/play/physicalSource":
					_, _ = w.Write([]byte(`[{"type":"kefPhysicalSource","kefPhysicalSource":"wifi"}]`))
				case "player:player/data":
					_, _ = w.Write([]byte(`[{"state":"stopped"}]`))
				case "playlists:pq/getitems":
					close(started)
					select {
					case <-r.Context().Done():
					case <-release:
					}
				default:
					t.Errorf("unexpected request: %s", r.URL)
					http.Error(w, "unexpected request", http.StatusNotFound)
				}
			}))
			defer server.Close()
			defer close(release)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			speaker := &KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")}
			var client *AirableClient
			if tc.bindClient {
				client = NewAirableClient(speaker, WithContext(ctx))
			} else {
				client = NewAirableClient(speaker)
			}
			originalHTTPClient := client.HTTPClient
			methodCtx := ctx
			switch tc.methodCtx {
			case "background":
				methodCtx = context.Background()
			case "nil":
				methodCtx = nil
			}
			result := make(chan error, 1)
			go func() {
				_, err := client.PlayOrResumeFromQueue(methodCtx)
				result <- err
			}()
			waitAirableSignal(t, started, "queue read during play")
			cancel()
			if err := waitAirableError(t, result); !errors.Is(err, context.Canceled) {
				t.Fatalf("play returned %v, want cancellation", err)
			}
			if client.HTTPClient != originalHTTPClient {
				t.Fatal("operation replaced the original client's HTTP configuration")
			}
		})
	}
}

func playQueueCancellationServer(
	t *testing.T, started, disconnected chan<- struct{}, release <-chan struct{},
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("path") {
		case "settings:/kef/play/physicalSource":
			_, _ = w.Write([]byte(`[{"type":"kefPhysicalSource","kefPhysicalSource":"wifi"}]`))
		case "player:player/data":
			_, _ = w.Write([]byte(`[{"state":"stopped"}]`))
		case "playlists:pq/getitems":
			close(started)
			select {
			case <-r.Context().Done():
				close(disconnected)
			case <-release:
			}
		default:
			t.Errorf("unexpected request: %s", r.URL)
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
}

func assertIndependentPlayContexts(
	t *testing.T, child bool, canceled string, clientErr, operationErr error,
) {
	t.Helper()
	if !child && strings.HasPrefix(canceled, "client") && operationErr != nil {
		t.Fatalf("client cancellation mutated the independent operation context: %v", operationErr)
	}
	if canceled == "operation" && clientErr != nil {
		t.Fatalf("operation cancellation mutated the client context: %v", clientErr)
	}
}

func TestPlayOrResumeCombinesOperationAndClientContexts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		child  bool
		cancel string
	}{
		{name: "independent client", cancel: "client"},
		{name: "independent client deadline", cancel: "client deadline"},
		{name: "independent operation", cancel: "operation"},
		{name: "child operation", child: true, cancel: "operation"},
		{name: "child deadline", child: true, cancel: "deadline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := make(chan struct{})
			disconnected := make(chan struct{})
			release := make(chan struct{})
			server := playQueueCancellationServer(t, started, disconnected, release)
			defer server.Close()
			defer close(release)

			clientParent, cancelClient := context.WithCancelCause(context.Background())
			defer cancelClient(nil)
			clientCtx := clientParent
			clientDeadlineCause := errors.New("client deadline cause")
			var clientDeadline time.Time
			if tc.cancel == "client deadline" {
				clientDeadline = time.Now().Add(time.Second)
				var cancelClientDeadline context.CancelFunc
				clientCtx, cancelClientDeadline = context.WithDeadlineCause(clientCtx, clientDeadline, clientDeadlineCause)
				defer cancelClientDeadline()
			}
			operationParent := context.Background()
			if tc.child {
				operationParent = clientCtx
			}
			operationParent = context.WithValue(operationParent, airableTestContextKey{}, "operation")
			deadline := time.Now().Add(8 * time.Second)
			deadlineCause := errors.New("operation deadline cause")
			if tc.cancel == "deadline" {
				deadline = time.Now().Add(time.Second)
			}
			deadlineCtx, cancelDeadline := context.WithDeadlineCause(operationParent, deadline, deadlineCause)
			defer cancelDeadline()
			operationCtx, cancelOperation := context.WithCancelCause(deadlineCtx)
			defer cancelOperation(nil)

			transport := &http.Transport{}
			defer transport.CloseIdleConnections()
			observed := &airableObservedTransport{
				transport: transport, contexts: make(chan context.Context, 1),
			}
			client := NewAirableClient(
				&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")},
				WithContext(clientCtx),
				func(a *AirableClient) { a.HTTPClient.Transport = observed },
			)
			originalHTTPClient := client.HTTPClient
			result := make(chan error, 1)
			go func() {
				_, err := client.PlayOrResumeFromQueue(operationCtx)
				result <- err
			}()
			waitAirableSignal(t, started, "queue read with distinct contexts")
			requestCtx := <-observed.contexts
			if value := requestCtx.Value(airableTestContextKey{}); value != "operation" {
				t.Fatalf("request context value = %v, want operation value", value)
			}
			wantDeadline := deadline
			if !clientDeadline.IsZero() {
				wantDeadline = clientDeadline
			}
			if got, ok := requestCtx.Deadline(); !ok || !got.Equal(wantDeadline) {
				t.Fatalf("request deadline = %v, %t; want %v", got, ok, wantDeadline)
			}

			cause := errors.New("canceled by " + tc.cancel)
			wantErr := context.Canceled
			switch tc.cancel {
			case "client":
				cancelClient(cause)
			case "operation":
				cancelOperation(cause)
			case "deadline":
				cause = deadlineCause
				wantErr = context.DeadlineExceeded
			case "client deadline":
				cause = clientDeadlineCause
				wantErr = context.DeadlineExceeded
			}
			err := waitAirableError(t, result)
			if !errors.Is(err, wantErr) || !errors.Is(err, cause) {
				t.Fatalf("play error = %v; want %v and cause %v", err, wantErr, cause)
			}
			if context.Cause(requestCtx) != cause { //nolint:errorlint // Verify the exact cancellation cause.
				t.Fatalf("request cause = %v, want %v", context.Cause(requestCtx), cause)
			}
			waitAirableSignal(t, disconnected, "queue request cancellation")
			if client.HTTPClient != originalHTTPClient {
				t.Fatal("operation replaced the original client's HTTP configuration")
			}
			assertIndependentPlayContexts(t, tc.child, tc.cancel, clientCtx.Err(), operationCtx.Err())
		})
	}
}

func TestPlayOrResumePreservesAlreadyCanceledCauses(t *testing.T) {
	for _, canceled := range []string{"client", "operation"} {
		t.Run(canceled, func(t *testing.T) {
			clientCtx, cancelClient := context.WithCancelCause(context.Background())
			defer cancelClient(nil)
			operationCtx, cancelOperation := context.WithCancelCause(context.Background())
			defer cancelOperation(nil)
			cause := errors.New(canceled + " already canceled")
			if canceled == "client" {
				cancelClient(cause)
			} else {
				cancelOperation(cause)
			}
			client := NewAirableClient(&KEFSpeaker{IPAddress: "invalid"}, WithContext(clientCtx))
			_, err := client.PlayOrResumeFromQueue(operationCtx)
			if !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
				t.Fatalf("already-canceled play error = %v, want cancellation and %v", err, cause)
			}
		})
	}
}

func TestPlayOrResumeReleasesContextRegistrations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("path") {
		case "settings:/kef/play/physicalSource":
			_, _ = w.Write([]byte(`[{"type":"kefPhysicalSource","kefPhysicalSource":"wifi"}]`))
		case "player:player/data":
			_, _ = w.Write([]byte(`[{"state":"playing"}]`))
		default:
			t.Errorf("unexpected request: %s", r.URL)
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	clientParent, cancelClient := context.WithCancel(context.Background())
	defer cancelClient()
	operationParent, cancelOperation := context.WithCancel(context.Background())
	defer cancelOperation()
	clientCtx := &airableTrackedContext{Context: clientParent}
	operationCtx := &airableTrackedContext{Context: operationParent}
	client := NewAirableClient(
		&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")}, WithContext(clientCtx),
	)
	result, err := client.PlayOrResumeFromQueue(operationCtx)
	if err != nil || result.Action != PlayActionAlreadyPlaying {
		t.Fatalf("play returned %+v, %v", result, err)
	}
	for name, tracked := range map[string]*airableTrackedContext{
		"client": clientCtx, "operation": operationCtx,
	} {
		if tracked.registered.Load() != 1 || tracked.stopped.Load() != 1 {
			t.Errorf("%s context registrations=%d stopped=%d, want 1 each",
				name, tracked.registered.Load(), tracked.stopped.Load())
		}
	}
}
