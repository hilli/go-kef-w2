// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package kefw2

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestUPnPStrictRecursiveIndex(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"failed branch", `{"rowsCount":1,"rows":[{"type":"container","path":"upnp:/broken"}]}`, "failed to scan"},
		{"cycle", `{"rowsCount":1,"rows":[{"type":"container","path":"upnp:/server"}]}`, "cycle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("path") == "upnp:/server" {
					_, _ = fmt.Fprint(w, tc.body)
					return
				}
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			}))
			defer server.Close()
			speaker := &KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")}
			index, err := BuildTrackIndex(NewAirableClient(speaker), "upnp:/server", "Library", "", nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) || index != nil {
				t.Fatalf("incomplete index presented as success: index=%+v err=%v", index, err)
			}
		})
	}
}

func TestUPnPStrictQueueStartsOnlyWhenEmpty(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			var posts []string
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == http.MethodPost {
					var payload struct {
						Path string `json:"path"`
					}

					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					posts = append(posts, payload.Path)
					_, _ = fmt.Fprint(w, `{}`)
					return
				}
				if empty && len(posts) == 0 {
					_, _ = fmt.Fprint(w, `{"rowsCount":0,"rows":[]}`)
					return
				}
				_, _ = fmt.Fprint(w, `{"rowsCount":1,"rows":[{"title":"Song","type":"audio","path":"upnp:/track"}]}`)
			}))
			defer server.Close()
			speaker := &KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")}
			err := NewAirableClient(speaker).AddToQueue(
				[]ContentItem{{Title: "Song", Type: "audio", Path: "upnp:/track"}}, true)
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			want := 1
			if empty {
				want = 2
			}
			if len(posts) != want || posts[0] != "playlists:pl/addexternalitems" ||
				(empty && posts[1] != "player:player/control") {
				t.Fatalf("queue cleared or playback incorrectly started: %v", posts)
			}
		})
	}
}

func TestUPnPPlaybackPreservesContainerOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") == "upnp:/server" {
			_, _ = fmt.Fprint(w, `{"rowsCount":3,"rows":[{"type":"audio","title":"First","path":"upnp:/first"},`+
				`{"type":"container","path":"upnp:/folder"},{"type":"audio","title":"Last","path":"upnp:/last"}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"rowsCount":1,"rows":[{"type":"audio","title":"Middle","path":"upnp:/middle"}]}`)
	}))
	defer server.Close()
	speaker := &KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")}
	tracks, err := NewAirableClient(speaker).GetContainerTracksRecursive("upnp:/server")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 3 || tracks[0].Title != "First" || tracks[1].Title != "Middle" || tracks[2].Title != "Last" {
		t.Fatalf("playback ordering changed: %+v", tracks)
	}
}

func TestUPnPUniqueContainerResolution(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		rows string
		want string
	}{
		{"duplicate", "Music", `{"rowsCount":2,"rows":[{"title":"Music","type":"container","path":"upnp:/one"},` +
			`{"title":"MUSIC","type":"container","path":"upnp:/two"}]}`, "ambiguous"},
		{"exact wins", "music", `{"rowsCount":2,"rows":[{"title":"Music videos","type":"container","path":"upnp:/wrong"},` +
			`{"title":"Music","type":"container","path":"upnp:/one"}]}`, ""},
		{"unique substring", "mus", `{"rowsCount":1,"rows":[{"title":"Music","type":"container","path":"upnp:/one"}]}`, ""},
		{"opaque", "upnp:/one", `{"rowsCount":0,"rows":[],"roles":{"type":"container","title":"Music","path":"upnp:/one"}}`, ""},
		{"not container", "upnp:/one", `{"rowsCount":0,"rows":[],"roles":{"type":"audio","title":"Song","path":"upnp:/one"}}`,
			"expected a container"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, tc.rows)
			}))
			defer server.Close()
			speaker := &KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")}
			path, _, err := FindContainerByPath(NewAirableClient(speaker), "upnp:/server", tc.path)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("expected %q, got path=%s err=%v", tc.want, path, err)
				}
			} else if err != nil || path != "upnp:/one" {
				t.Fatalf("wrong container: path=%s err=%v", path, err)
			}
		})
	}
}
