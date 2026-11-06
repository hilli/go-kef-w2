package kefw2

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEventClientRegisterQueue(t *testing.T) {
	tests := []struct {
		name       string
		getStatus  int
		postStatus int
		response   string
		wantError  string
	}{
		{name: "GET supported", getStatus: http.StatusOK, response: `"{test-queue}"`},
		{name: "405 fallback", getStatus: http.StatusMethodNotAllowed, postStatus: http.StatusOK, response: `"{test-queue}"`},
		{name: "501 fallback", getStatus: http.StatusNotImplemented, postStatus: http.StatusOK, response: `"{test-queue}"`},
		{name: "bad request", getStatus: http.StatusBadRequest, wantError: "HTTP 400: Invalid method"},
		{name: "server error", getStatus: http.StatusInternalServerError, wantError: "HTTP 500: Invalid method"},
		{name: "POST error", getStatus: http.StatusNotImplemented, postStatus: http.StatusForbidden, response: "Forbidden", wantError: "HTTP 403: Forbidden"},
		{name: "invalid GET response", getStatus: http.StatusOK, response: `{}`, wantError: "failed to parse queue ID"},
		{name: "empty GET queue", getStatus: http.StatusOK, response: `""`, wantError: "empty queue ID"},
		{name: "invalid POST response", getStatus: http.StatusNotImplemented, postStatus: http.StatusOK, response: `{}`, wantError: "failed to parse queue ID"},
		{name: "empty POST queue", getStatus: http.StatusMethodNotAllowed, postStatus: http.StatusOK, response: `""`, wantError: "empty queue ID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subscriptions := []EventSubscription{{Path: pathPlayerVolume, Type: "itemWithValue"}}
			var methods []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/event/pollQueue" {
					if r.Method != http.MethodGet || r.URL.Query().Get("queueId") != "{test-queue}" {
						t.Errorf("unexpected poll request: %s %s", r.Method, r.URL)
					}
					_, _ = fmt.Fprint(w, `[{"path":"player:volume","itemValue":{"type":"i32_","i32_":25}}]`)
					return
				}
				if r.URL.Path != "/api/event/modifyQueue" {
					t.Errorf("unexpected registration path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				methods = append(methods, r.Method)
				checkQueueRegistrationRequest(t, r, subscriptions, methods)
				switch r.Method {
				case http.MethodGet:
					w.WriteHeader(tt.getStatus)
					if tt.getStatus != http.StatusOK {
						_, _ = fmt.Fprint(w, "Invalid method")
						return
					}
				case http.MethodPost:
					w.WriteHeader(tt.postStatus)
				default:
					t.Errorf("unexpected method: %s", r.Method)
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				_, _ = fmt.Fprint(w, tt.response)
			}))
			defer server.Close()

			speaker := &KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")}
			client, err := speaker.NewEventClient(WithSubscriptions(subscriptions))
			wantMethods := []string{http.MethodGet}
			if tt.getStatus == http.StatusMethodNotAllowed || tt.getStatus == http.StatusNotImplemented {
				wantMethods = append(wantMethods, http.MethodPost)
			}
			if !reflect.DeepEqual(methods, wantMethods) {
				t.Errorf("registration methods = %v, want %v", methods, wantMethods)
			}
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("NewEventClient error = %v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewEventClient: %v", err)
			}
			defer client.Close()
			events, err := client.poll(context.Background(), server.URL+"/api/event/pollQueue?queueId="+client.QueueID())
			if err != nil {
				t.Fatalf("poll: %v", err)
			}
			if len(events) != 1 {
				t.Fatalf("got %d events, want 1", len(events))
			}
			volume, ok := events[0].(*VolumeEvent)
			if !ok || volume.Volume != 25 {
				t.Errorf("event = %#v, want volume 25", events[0])
			}
		})
	}
}

func checkQueueRegistrationRequest(t *testing.T, r *http.Request, subscriptions []EventSubscription, methods []string) {
	t.Helper()
	switch r.Method {
	case http.MethodGet:
		if len(methods) != 1 {
			t.Error("GET must be the first registration attempt")
		}
		query := r.URL.Query()
		subscribeJSON, err := json.Marshal(subscriptions)
		if err != nil {
			t.Error(err)
		}
		if query.Get("subscribe") != string(subscribeJSON) ||
			query.Get("unsubscribe") != "[]" || !query.Has("queueId") || query.Get("queueId") != "" {
			t.Errorf("unexpected GET parameters: %v", query)
		}
	case http.MethodPost:
		if len(methods) != 2 || methods[0] != http.MethodGet {
			t.Error("POST must follow a single GET")
		}
		if r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected POST query or content type: %s %s", r.URL.RawQuery, r.Header.Get("Content-Type"))
		}
		var body struct {
			Subscribe   []EventSubscription `json:"subscribe"`
			Unsubscribe []EventSubscription `json:"unsubscribe"`
			QueueID     *string             `json:"queueId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode POST body: %v", err)
		}
		if !reflect.DeepEqual(body.Subscribe, subscriptions) ||
			body.Unsubscribe == nil || len(body.Unsubscribe) != 0 || body.QueueID == nil || *body.QueueID != "" {
			t.Errorf("unexpected POST body: %+v", body)
		}
	}
}

func TestEventTypes(t *testing.T) {
	// Test that all event types have expected values
	eventTypes := []struct {
		eventType EventType
		want      string
	}{
		{EventTypeVolume, "volume"},
		{EventTypeSource, "source"},
		{EventTypePower, "power"},
		{EventTypeMute, "mute"},
		{EventTypePlayTime, "play_time"},
		{EventTypePlayerData, "player_data"},
		{EventTypePlayMode, "play_mode"},
		{EventTypeEQProfile, "eq_profile"},
		{EventTypePlaylist, "playlist"},
		{EventTypeBluetooth, "bluetooth"},
		{EventTypeNetwork, "network"},
		{EventTypeFirmware, "firmware"},
		{EventTypeNotification, "notification"},
		{EventTypeUnknown, "unknown"},
	}

	for _, tt := range eventTypes {
		if string(tt.eventType) != tt.want {
			t.Errorf("EventType = %q, want %q", tt.eventType, tt.want)
		}
	}
}

func TestBaseEvent(t *testing.T) {
	now := time.Now()
	base := baseEvent{
		eventType: EventTypeVolume,
		path:      "player:volume",
		timestamp: now,
	}

	if base.Type() != EventTypeVolume {
		t.Errorf("Type() = %v, want %v", base.Type(), EventTypeVolume)
	}
	if base.Path() != "player:volume" {
		t.Errorf("Path() = %q, want %q", base.Path(), "player:volume")
	}
	if !base.Timestamp().Equal(now) {
		t.Errorf("Timestamp() = %v, want %v", base.Timestamp(), now)
	}
}

func TestVolumeEvent(t *testing.T) {
	event := &VolumeEvent{
		baseEvent: baseEvent{
			eventType: EventTypeVolume,
			path:      "player:volume",
			timestamp: time.Now(),
		},
		Volume: 50,
	}

	if event.Type() != EventTypeVolume {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeVolume)
	}
	if event.Volume != 50 {
		t.Errorf("Volume = %d, want %d", event.Volume, 50)
	}
}

func TestSourceEvent(t *testing.T) {
	event := &SourceEvent{
		baseEvent: baseEvent{
			eventType: EventTypeSource,
			path:      "settings:/kef/play/physicalSource",
			timestamp: time.Now(),
		},
		Source: SourceWiFi,
	}

	if event.Type() != EventTypeSource {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeSource)
	}
	if event.Source != SourceWiFi {
		t.Errorf("Source = %v, want %v", event.Source, SourceWiFi)
	}
}

func TestPowerEvent(t *testing.T) {
	event := &PowerEvent{
		baseEvent: baseEvent{
			eventType: EventTypePower,
			path:      "settings:/kef/host/speakerStatus",
			timestamp: time.Now(),
		},
		Status: SpeakerStatusOn,
	}

	if event.Type() != EventTypePower {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypePower)
	}
	if event.Status != SpeakerStatusOn {
		t.Errorf("Status = %v, want %v", event.Status, SpeakerStatusOn)
	}
}

func TestMuteEvent(t *testing.T) {
	event := &MuteEvent{
		baseEvent: baseEvent{
			eventType: EventTypeMute,
			path:      "settings:/mediaPlayer/mute",
			timestamp: time.Now(),
		},
		Muted: true,
	}

	if event.Type() != EventTypeMute {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeMute)
	}
	if !event.Muted {
		t.Error("Muted should be true")
	}
}

func TestPlayTimeEvent(t *testing.T) {
	event := &PlayTimeEvent{
		baseEvent: baseEvent{
			eventType: EventTypePlayTime,
			path:      "player:player/data/playTime",
			timestamp: time.Now(),
		},
		PositionMS: 180000,
	}

	if event.Type() != EventTypePlayTime {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypePlayTime)
	}
	if event.PositionMS != 180000 {
		t.Errorf("PositionMS = %d, want %d", event.PositionMS, 180000)
	}
}

func TestPlayerDataEvent(t *testing.T) {
	event := &PlayerDataEvent{
		baseEvent: baseEvent{
			eventType: EventTypePlayerData,
			path:      "player:player/data",
			timestamp: time.Now(),
		},
		State:    PlayerStatePlaying,
		Title:    "Test Song",
		Artist:   "Test Artist",
		Album:    "Test Album",
		Duration: 180000,
		Icon:     "http://example.com/art.jpg",
	}

	if event.Type() != EventTypePlayerData {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypePlayerData)
	}
	if event.State != PlayerStatePlaying {
		t.Errorf("State = %q, want %q", event.State, PlayerStatePlaying)
	}
	if event.Title != "Test Song" {
		t.Errorf("Title = %q, want %q", event.Title, "Test Song")
	}
	if event.Artist != "Test Artist" {
		t.Errorf("Artist = %q, want %q", event.Artist, "Test Artist")
	}
}

func TestEQProfileEvent(t *testing.T) {
	profile := EQProfileV2{
		ProfileName: "Custom",
		Balance:     0,
	}

	event := &EQProfileEvent{
		baseEvent: baseEvent{
			eventType: EventTypeEQProfile,
			path:      "kef:eqProfile/v2",
			timestamp: time.Now(),
		},
		Profile: profile,
	}

	if event.Type() != EventTypeEQProfile {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeEQProfile)
	}
	if event.Profile.ProfileName != "Custom" {
		t.Errorf("Profile.ProfileName = %q, want %q", event.Profile.ProfileName, "Custom")
	}
}

func TestPlaylistEvent(t *testing.T) {
	event := &PlaylistEvent{
		baseEvent: baseEvent{
			eventType: EventTypePlaylist,
			path:      "playlists:pq/getitems",
			timestamp: time.Now(),
		},
		Changes: []PlaylistChange{
			{Type: "add", Index: 0},
			{Type: "remove", Index: 3},
		},
		Version: 5,
	}

	if event.Type() != EventTypePlaylist {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypePlaylist)
	}
	if len(event.Changes) != 2 {
		t.Errorf("len(Changes) = %d, want %d", len(event.Changes), 2)
	}
	if event.Version != 5 {
		t.Errorf("Version = %d, want %d", event.Version, 5)
	}
}

func TestBluetoothEvent(t *testing.T) {
	event := &BluetoothEvent{
		baseEvent: baseEvent{
			eventType: EventTypeBluetooth,
			path:      "bluetooth:state",
			timestamp: time.Now(),
		},
		Bluetooth: BluetoothState{
			State:     "connected",
			Connected: true,
			Pairing:   false,
		},
	}

	if event.Type() != EventTypeBluetooth {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeBluetooth)
	}
	if !event.Bluetooth.Connected {
		t.Error("Bluetooth.Connected should be true")
	}
}

func TestParseTypedValue(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
	}{
		{
			name:    "empty input",
			input:   "",
			wantNil: true,
		},
		{
			name:    "valid i32",
			input:   `{"type":"i32_","i32_":50}`,
			wantNil: false,
		},
		{
			name:    "valid string",
			input:   `{"type":"string_","string_":"test"}`,
			wantNil: false,
		},
		{
			name:    "invalid json",
			input:   `{invalid}`,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTypedValue(json.RawMessage(tt.input))
			if (result == nil) != tt.wantNil {
				t.Errorf("parseTypedValue() = %v, wantNil %v", result, tt.wantNil)
			}
		})
	}
}

func TestDefaultEventSubscriptions(t *testing.T) {
	// Verify we have expected subscriptions
	if len(DefaultEventSubscriptions) == 0 {
		t.Error("DefaultEventSubscriptions should not be empty")
	}

	// Check for key subscriptions
	expectedPaths := []string{
		"player:volume",
		"player:player/data",
		"settings:/kef/play/physicalSource",
		"settings:/kef/host/speakerStatus",
		"settings:/mediaPlayer/mute",
	}

	for _, path := range expectedPaths {
		found := false
		for _, sub := range DefaultEventSubscriptions {
			if sub.Path == path {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("DefaultEventSubscriptions missing path %q", path)
		}
	}
}

func TestEventSubscription(t *testing.T) {
	sub := EventSubscription{
		Path: "player:volume",
		Type: "itemWithValue",
	}

	// Test JSON marshaling
	data, err := json.Marshal(sub)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded EventSubscription
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.Path != sub.Path {
		t.Errorf("Path = %q, want %q", decoded.Path, sub.Path)
	}
	if decoded.Type != sub.Type {
		t.Errorf("Type = %q, want %q", decoded.Type, sub.Type)
	}
}
