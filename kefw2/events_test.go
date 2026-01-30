package kefw2

import (
	"encoding/json"
	"testing"
	"time"
)

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
