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
	}
}

func podcastLibraryRows(t *testing.T, w http.ResponseWriter, rows RowsResponse) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(rows); err != nil {
		t.Error(err)
	}
}
