// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package kefw2

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestGetAllRowsAdvancesByActualPageSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from, err := strconv.Atoi(r.URL.Query().Get("from"))
		if err != nil {
			t.Error(err)
		}
		var count int
		switch from {
		case 0, 3:
			count = 2
		case 2:
			count = 1
		default:
			t.Errorf("skipped or repeated row offset: %d", from)
		}
		rows := make([]ContentItem, 0, count)
		for i := range count {
			rows = append(rows, ContentItem{Title: fmt.Sprint(from + i)})
		}
		_ = json.NewEncoder(w).Encode(RowsResponse{RowsCount: 5, Rows: rows})
	}))
	defer server.Close()
	client := NewAirableClient(&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")})
	result, err := client.GetAllRows("test:page")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 5 {
		t.Fatalf("rows=%+v", result.Rows)
	}
	for i, row := range result.Rows {
		if row.Title != fmt.Sprint(i) {
			t.Fatalf("row %d=%q; lost pagination position", i, row.Title)
		}
	}
}

func TestGetAllRowsRejectsIncompleteData(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("from") == "0" {
					_, _ = w.Write([]byte(`{"rowsCount":3,"rows":[{"title":"First"}]}`))
					return
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"rowsCount":3,"rows":[]}`))
			}))
			defer server.Close()
			client := NewAirableClient(&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")})
			if rows, err := client.GetAllRows("test:page"); err == nil || rows != nil {
				t.Fatalf("incomplete page reported success: rows=%+v err=%v", rows, err)
			}
		})
	}
}

func TestAddToQueueDoesNotGuessAfterReadFailure(t *testing.T) {
	for _, failAfterAdd := range []bool{false, true} {
		t.Run(strconv.FormatBool(failAfterAdd), func(t *testing.T) {
			var posts, reads int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts++
					_, _ = w.Write([]byte(`{}`))
					return
				}
				reads++
				if !failAfterAdd || reads > 1 {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte(`{}`))
					return
				}
				_, _ = w.Write([]byte(`{"rowsCount":0,"rows":[]}`))
			}))
			defer server.Close()
			client := NewAirableClient(&KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")})
			err := client.AddToQueue([]ContentItem{{Title: "Track"}}, true)
			if err == nil {
				t.Fatal("queue read failure ignored")
			}
			wantPosts := 0
			if failAfterAdd {
				wantPosts = 1
			}
			if posts != wantPosts {
				t.Fatalf("guessed playback after read failure: posts=%d want=%d", posts, wantPosts)
			}
		})
	}
}
