// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package kefw2

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEventClientRegistrationCancellation(t *testing.T) {
	for _, blockMethod := range []string{http.MethodGet, http.MethodPost} {
		t.Run(blockMethod, func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != blockMethod {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					t.Error(err)
					return
				}
				close(started)
				<-r.Context().Done()
			}))
			defer server.Close()
			speaker := &KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := speaker.NewEventClientContext(ctx); done <- err }()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("registration never started")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("registration cancellation: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("registration ignored cancellation")
			}
		})
	}
}

func TestEventClientWatchPlayerDataInvalidations(t *testing.T) {
	for _, changed := range []string{
		`"trackRoles":{"title":"Same","icon":"new"}`,
		`"trackRoles":{"title":"Same","mediaData":{"metaData":{"album":"new"}}}`,
		`"trackRoles":{"title":"Same"},"status":{"duration":123}`,
		`"trackRoles":{"title":"Same"},"controls":{"pause":true}`,
	} {
		for _, invalidations := range []bool{false, true} {
			client := &EventClient{playerDataInvalidations: invalidations}
			first := rawEvent{Path: pathPlayerData, ItemValue: json.RawMessage(
				`[{"state":"playing","trackRoles":{"title":"Same"}}]`)}
			if client.parseRawEvent(context.Background(), first) == nil {
				t.Fatal("first player-data event was lost")
			}
			second := rawEvent{Path: pathPlayerData, ItemValue: json.RawMessage(`[{"state":"playing",` + changed + `}]`)}
			if got := client.parseRawEvent(context.Background(), second); (got != nil) != invalidations {
				t.Fatalf("invalidations=%t, change=%s, got=%+v", invalidations, changed, got)
			}
		}
	}
}

func TestEventClientPollHealthRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/event/modifyQueue" {
			_, _ = w.Write([]byte(`"queue"`))
			return
		}
		polls++
		if polls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	speaker := &KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")}
	var observations []error
	client, err := speaker.NewEventClientContext(ctx, WithEventPollObserver(func(err error) {
		observations = append(observations, err)
		if err == nil {
			cancel()
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("poll stopped with %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if len(observations) != 2 || observations[0] == nil || observations[1] != nil {
		t.Fatalf("missing poll failure/recovery: %v", observations)
	}
}
