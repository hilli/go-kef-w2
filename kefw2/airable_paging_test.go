// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package kefw2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetPlayQueuePaginatesUncached(t *testing.T) {
	requests := make(chan [2]string, 6)
	var generation atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/getRows" || q.Get("path") != "playlists:pq/getitems" {
			t.Errorf("unexpected queue request: %s", r.URL)
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		requests <- [2]string{q.Get("from"), q.Get("to")}
		var indices []int
		switch q.Get("from") {
		case "0":
			generation.Add(1)
			indices = []int{0, 1}
		case "2":
			indices = []int{2}
		case "3":
			indices = []int{3, 4}
		default:
			t.Errorf("unexpected queue offset: %s", q.Get("from"))
		}
		rows := make([]ContentItem, 0, len(indices))
		for _, i := range indices {
			path := fmt.Sprintf("track:%d", i)
			if i < 2 {
				path = "track:duplicate"
			}
			rows = append(rows, ContentItem{Path: path, Title: fmt.Sprintf("%d:%d", generation.Load(), i)})
		}
		if err := json.NewEncoder(w).Encode(RowsResponse{RowsCount: 5, RowsVersion: 7, Rows: rows}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := NewAirableClient(
		&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")},
		WithDefaultCache(),
	)
	client.Cache.Set("rows:playlists:pq/getitems:0:32766",
		&RowsResponse{RowsCount: 1, Rows: []ContentItem{{Title: "stale"}}})

	for gen := 1; gen <= 2; gen++ {
		queue, err := client.GetPlayQueue()
		if err != nil {
			t.Fatal(err)
		}
		if queue.RowsCount != 5 || queue.RowsVersion != 7 || len(queue.Rows) != 5 {
			t.Fatalf("incomplete queue: %+v", queue)
		}
		for i, row := range queue.Rows {
			if row.Title != fmt.Sprintf("%d:%d", gen, i) {
				t.Fatalf("queue order or freshness at %d: %+v", i, queue.Rows)
			}
		}
		if queue.Rows[0].Path != queue.Rows[1].Path {
			t.Fatalf("duplicate queue entries were lost: %+v", queue.Rows)
		}
		for _, want := range [][2]string{{"0", "32766"}, {"2", "3"}, {"3", "4"}} {
			if got := <-requests; got != want {
				t.Fatalf("queue page request = %q, want %q", got, want)
			}
		}
	}
}

func TestGetPlayQueueRejectsIncompleteOrChangedPages(t *testing.T) {
	const first = `{"rowsCount":3,"rowsVersion":4,"rows":[{"path":"one"}]}`
	for _, tc := range []struct {
		name       string
		first      string
		next       string
		nextStatus int
	}{
		{"changed count", first, `{"rowsCount":4,"rowsVersion":4,"rows":[{"path":"two"}]}`, 0},
		{"changed version", first, `{"rowsCount":3,"rowsVersion":5,"rows":[{"path":"two"}]}`, 0},
		{"empty page", first, `{"rowsCount":3,"rowsVersion":4,"rows":[]}`, 0},
		{"oversized page", first, `{"rowsCount":3,"rowsVersion":4,"rows":[{"path":"two"},{"path":"three"}]}`, 0},
		{"redirected page", first, `{"rowsCount":3,"rowsVersion":4,"rowsRedirect":"elsewhere"}`, 0},
		{"failed page", first, `{}`, http.StatusServiceUnavailable},
		{"too many initial rows", `{"rowsCount":1,"rows":[{"path":"one"},{"path":"two"}]}`, "", 0},
		{"empty initial page", `{"rowsCount":2,"rows":[]}`, "", 0},
		{"redirected queue", `{"rowsCount":0,"rowsRedirect":"elsewhere"}`, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("from") == "0" {
					_, _ = w.Write([]byte(tc.first))
					return
				}
				if tc.nextStatus != 0 {
					w.WriteHeader(tc.nextStatus)
				}
				_, _ = w.Write([]byte(tc.next))
			}))
			defer server.Close()
			client := NewAirableClient(&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")})
			if queue, err := client.GetPlayQueue(); err == nil || queue != nil {
				t.Fatalf("invalid queue reported success: queue=%+v err=%v", queue, err)
			}
		})
	}
}

func TestGetPlayQueueCancelsPagination(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("from") == "0" {
			_, _ = w.Write([]byte(`{"rowsCount":2,"rowsVersion":4,"rows":[{"path":"one"}]}`))
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
		queue *RowsResponse
		err   error
	}, 1)
	go func() {
		queue, err := client.GetPlayQueue()
		result <- struct {
			queue *RowsResponse
			err   error
		}{queue, err}
	}()
	waitAirableSignal(t, started, "next queue page")
	cancel()
	select {
	case got := <-result:
		if got.queue != nil || !errors.Is(got.err, context.Canceled) {
			t.Fatalf("queue pagination lost cancellation: queue=%+v err=%v", got.queue, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queue pagination did not stop after cancellation")
	}
}

func TestFavoritesPageBoundsAndLegacyRange(t *testing.T) {
	for _, tc := range []struct {
		name     string
		path     string
		page     func(*AirableClient, int, int) (*RowsResponse, error)
		original func(*AirableClient) (*RowsResponse, error)
	}{
		{"radio", "airable:https://radio.example/airable/radios/favorites",
			(*AirableClient).GetRadioFavoritesPage, (*AirableClient).GetRadioFavorites},
		{"podcast", "airable:https://podcast.example/airable/feeds/favorites",
			(*AirableClient).GetPodcastFavoritesPage, (*AirableClient).GetPodcastFavorites},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan [3]string, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				requests <- [3]string{q.Get("path"), q.Get("from"), q.Get("to")}
				_, _ = w.Write([]byte(`{"rowsCount":0,"rows":[]}`))
			}))
			defer server.Close()
			client := NewAirableClient(&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")})
			client.RadioBaseURL = "airable:https://radio.example/airable/radios"
			client.PodcastBaseURL = "airable:https://podcast.example/airable/feeds"

			if _, err := tc.original(client); err != nil {
				t.Fatal(err)
			}
			if got, want := <-requests, [3]string{tc.path, "0", "50"}; got != want {
				t.Fatalf("legacy favorites request = %q, want %q", got, want)
			}
			for _, pair := range [][2]int{{0, 1}, {24, 10}, {1_000_000, 100}} {
				if _, err := tc.page(client, pair[0], pair[1]); err != nil {
					t.Fatal(err)
				}
				want := [3]string{tc.path, strconv.Itoa(pair[0]), strconv.Itoa(pair[0] + pair[1] - 1)}
				if got := <-requests; got != want {
					t.Fatalf("favorites request = %q, want %q", got, want)
				}
			}
			for _, pair := range [][2]int{{-1, 24}, {0, 0}, {0, 101}, {1_000_001, 24}} {
				if _, err := tc.page(client, pair[0], pair[1]); err == nil {
					t.Errorf("accepted invalid pagination %v", pair)
				}
			}
			select {
			case got := <-requests:
				t.Fatalf("invalid pagination sent a request: %q", got)
			default:
			}
		})
	}
}

func TestFavoritesPageDiscoversService(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry string
		base  string
		page  func(*AirableClient, int, int) (*RowsResponse, error)
	}{
		{"radio", "ui:/airableradios", "airable:https://radio.example/airable/radios",
			(*AirableClient).GetRadioFavoritesPage},
		{"podcast", "airable:linkService_airable.feeds", "airable:https://podcast.example/airable/feeds",
			(*AirableClient).GetPodcastFavoritesPage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				switch q.Get("path") {
				case tc.entry:
					_ = json.NewEncoder(w).Encode(RowsResponse{RowsRedirect: tc.base})
				case tc.base + "/favorites":
					if q.Get("from") != "4" || q.Get("to") != "8" {
						t.Errorf("unexpected favorites page: %s", r.URL)
					}
					_, _ = w.Write([]byte(`{"rowsCount":0,"rows":[]}`))
				default:
					t.Errorf("unexpected discovery request: %s", r.URL)
					http.Error(w, "unexpected request", http.StatusNotFound)
				}
			}))
			defer server.Close()
			client := NewAirableClient(&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")})
			if _, err := tc.page(client, 4, 5); err != nil {
				t.Fatal(err)
			}
		})
	}
}
