// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package kefw2

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestAirableClient_SearchPodcastsPage(t *testing.T) {
	requests := make(chan [3]string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		requests <- [3]string{q.Get("path"), q.Get("from"), q.Get("to")}
		_, _ = w.Write([]byte(`{"rowsCount":100,"rows":[]}`))
	}))
	defer server.Close()

	client := NewAirableClient(&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")})
	client.PodcastBaseURL = "airable:https://podcast.example/airable/feeds"
	for _, tc := range []struct {
		name      string
		offset    int
		limit     int
		wantTo    int
		firstPage bool
	}{
		{name: "default first page", wantTo: 23, firstPage: true},
		{name: "minimum", offset: 0, limit: 1, wantTo: 0},
		{name: "maximum", offset: 1_000_000, limit: 100, wantTo: 1_000_099},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.firstPage {
				_, err = client.SearchPodcasts("Jazz & Blues")
			} else {
				_, err = client.SearchPodcastsPage("Jazz & Blues", tc.offset, tc.limit)
			}
			if err != nil {
				t.Fatal(err)
			}
			got := <-requests
			want := [3]string{
				`airable:https://podcast.example/airable/feeds/search\?q\=Jazz+%26+Blues`,
				strconv.Itoa(tc.offset), strconv.Itoa(tc.wantTo),
			}
			if got != want {
				t.Fatalf("request = %q, want %q", got, want)
			}
		})
	}

	for _, pair := range [][2]int{{-1, 24}, {0, 0}, {0, 101}, {1_000_001, 24}} {
		if _, err := client.SearchPodcastsPage("Jazz", pair[0], pair[1]); err == nil {
			t.Errorf("accepted invalid pagination %v", pair)
		}
	}
	select {
	case got := <-requests:
		t.Fatalf("invalid pagination sent a request: %q", got)
	default:
	}
}

func TestPodcastJSONPagination(t *testing.T) {
	for _, failSecondPage := range []bool{false, true} {
		t.Run(fmt.Sprintf("fail=%t", failSecondPage), func(t *testing.T) {
			var offsets []string
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				path := r.URL.Query().Get("path")
				from := r.URL.Query().Get("from")
				offsets = append(offsets, path+":"+from)
				if strings.Contains(path, "/search") {
					if from == "0" {
						podcastLibraryRows(t, w, RowsResponse{RowsCount: 2, Rows: []ContentItem{
							{Title: "Serial", Type: ContentTypeContainer, Path: "show1"},
						}})
						return
					}
					if failSecondPage {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					podcastLibraryRows(t, w, RowsResponse{RowsCount: 2, Rows: []ContentItem{
						{Title: "Serial", Type: ContentTypeContainer, Path: "show2"},
					}})
					return
				}
				http.Error(w, "unexpected path", http.StatusNotFound)
			}))
			defer server.Close()
			client := NewAirableClient(&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")})
			client.PodcastBaseURL = "airable:https://podcast.example/airable/feeds"
			rows, err := client.SearchPodcastsAll("Serial")
			if failSecondPage {
				if err == nil || rows != nil || !strings.Contains(err.Error(), "offset 1") {
					t.Fatalf("must fail with offset, not return incomplete results: rows=%+v err=%v", rows, err)
				}
			} else if err != nil || len(rows.Rows) != 2 || rows.Rows[1].Path != "show2" {
				t.Fatalf("missing second ambiguous match: rows=%+v err=%v", rows, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(offsets) != 2 || !strings.HasSuffix(offsets[0], ":0") || !strings.HasSuffix(offsets[1], ":1") {
				t.Fatalf("wrong page requests: %v", offsets)
			}
		})
	}
}

func TestPodcastJSONEpisodesPaginateShowAndEpisodes(t *testing.T) {
	var paths []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path, from := r.URL.Query().Get("path"), r.URL.Query().Get("from")
		paths = append(paths, path+":"+from)
		switch {
		case path == "show" && from == "0":
			podcastLibraryRows(t, w, RowsResponse{RowsCount: 2, Rows: []ContentItem{
				{Title: "Description", Type: "text"},
			}})
		case path == "show" && from == "1":
			podcastLibraryRows(t, w, RowsResponse{RowsCount: 2, Rows: []ContentItem{
				{Title: "Episodes", Type: ContentTypeContainer, Path: "show/episodes"},
			}})
		case path == "show/episodes" && from == "0":
			podcastLibraryRows(t, w, RowsResponse{RowsCount: 2, Rows: []ContentItem{
				{Title: "One", Type: ContentTypeAudio, Path: "ep1"},
			}})
		case path == "show/episodes" && from == "1":
			podcastLibraryRows(t, w, RowsResponse{RowsCount: 2, Rows: []ContentItem{
				{Title: "Two", Type: ContentTypeAudio, Path: "ep2"},
			}})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := NewAirableClient(&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")})
	rows, err := client.GetPodcastEpisodesAll("show")
	if err != nil || len(rows.Rows) != 2 || rows.Rows[1].Path != "ep2" {
		t.Fatalf("episodes=%+v err=%v", rows, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(paths, ",") != "show:0,show:1,show/episodes:0,show/episodes:1" {
		t.Fatalf("incomplete show/episode pagination: %v", paths)
	}
}

func TestPodcastJSONDetailsRequireExactPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows RowsResponse
		ok   bool
	}{
		{"roles", RowsResponse{Roles: &ContentItem{Path: "selected", Type: ContentTypeAudio}}, true},
		{"wrong roles", RowsResponse{Roles: &ContentItem{Path: "wrong", Type: ContentTypeAudio}}, false},
		{"first child", RowsResponse{RowsCount: 1, Rows: []ContentItem{{Path: "child", Type: ContentTypeAudio}}}, false},
		{"exact row", RowsResponse{RowsCount: 2, Rows: []ContentItem{
			{Path: "child", Type: ContentTypeAudio}, {Path: "selected", Type: ContentTypeAudio},
		}}, true},
		{"duplicate row", RowsResponse{RowsCount: 2, Rows: []ContentItem{
			{Path: "selected", Type: ContentTypeAudio}, {Path: "selected", Type: ContentTypeAudio},
		}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				podcastLibraryRows(t, w, tc.rows)
			}))
			defer server.Close()
			client := NewAirableClient(&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")})
			item, err := client.GetPodcastDetailsExact("selected")
			if tc.ok {
				if err != nil || item.Path != "selected" {
					t.Fatalf("item=%+v err=%v", item, err)
				}
			} else if err == nil || item != nil {
				t.Fatalf("must not resolve arbitrary details: item=%+v err=%v", item, err)
			}
		})
	}
}

func TestPodcastJSONRejectsInvalidEpisodesContainer(t *testing.T) {
	for _, paths := range [][]string{{""}, {"first/episodes", "other/episodes"}} {
		t.Run(strings.Join(paths, ","), func(t *testing.T) {
			rows := RowsResponse{RowsCount: len(paths)}
			for _, path := range paths {
				rows.Rows = append(rows.Rows, ContentItem{Title: "Episodes", Type: ContentTypeContainer, Path: path})
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				podcastLibraryRows(t, w, rows)
			}))
			defer server.Close()
			client := NewAirableClient(&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")})
			if resp, err := client.GetPodcastEpisodesAll("show"); err == nil || resp != nil {
				t.Fatalf("invalid episodes container accepted: rows=%+v err=%v", resp, err)
			}
		})
	}
}

func TestPodcastJSONExactPlaybackValidation(t *testing.T) {
	client := NewAirableClient(&KEFSpeaker{IPAddress: "invalid"})
	for _, item := range []*ContentItem{
		nil, {Path: "show", Type: ContentTypeContainer}, {Type: ContentTypeAudio},
	} {
		if err := client.PlayPodcastEpisodeExact(item); err == nil {
			t.Fatalf("invalid exact episode accepted: %+v", item)
		}
		if err := client.RequestPodcastEpisodeExact(item); err == nil {
			t.Fatalf("invalid exact episode request accepted: %+v", item)
		}
	}
}

func TestPodcastExactPlaybackDoesNotClearQueue(t *testing.T) {
	type playbackRequest struct {
		Path  string `json:"path"`
		Role  string `json:"role"`
		Value struct {
			Type       string      `json:"type"`
			Control    string      `json:"control"`
			MediaRoles ContentItem `json:"mediaRoles"`
		} `json:"value"`
	}
	requests := make(chan playbackRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/setData" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		var payload playbackRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		requests <- payload
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := NewAirableClient(&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")})
	episode := &ContentItem{Path: "episode:42", Title: "Episode", Type: ContentTypeAudio}
	if err := client.RequestPodcastEpisodeExact(episode); err != nil {
		t.Fatal(err)
	}
	got := <-requests
	if got.Path != pathPlayerControl || got.Role != roleActivate || got.Value.Type != playbackTypeNone ||
		got.Value.Control != controlPlay || got.Value.MediaRoles.Path != episode.Path ||
		got.Value.MediaRoles.Type != ContentTypeAudio || !got.Value.MediaRoles.ContainerPlayable {
		t.Fatalf("unexpected exact episode play request: %+v", got)
	}
	select {
	case extra := <-requests:
		t.Fatalf("exact podcast playback sent an extra request (including queue clear): %+v", extra)
	default:
	}
	if episode.ContainerPlayable {
		t.Fatal("exact playback modified the caller's episode")
	}
}

func TestPodcastExactLegacyPlaybackClearsQueue(t *testing.T) {
	requests := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		requests <- payload.Path
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := NewAirableClient(&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")})
	if err := client.PlayPodcastEpisodeExact(&ContentItem{Path: "episode:42", Type: ContentTypeAudio}); err != nil {
		t.Fatal(err)
	}
	if first, second := <-requests, <-requests; first != "playlists:pl/clear" || second != pathPlayerControl {
		t.Fatalf("legacy exact playback requests = %q, %q", first, second)
	}
}

func podcastLibraryRows(t *testing.T, w http.ResponseWriter, rows RowsResponse) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(rows); err != nil {
		t.Error(err)
	}
}
