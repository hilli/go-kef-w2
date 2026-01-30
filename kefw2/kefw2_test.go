package kefw2

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// createMockSpeakerServer creates a comprehensive mock server for speaker tests.
func createMockSpeakerServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/getData":
			handleGetData(w, r)
		case "/api/getRows":
			handleGetRows(w, r)
		case "/api/setData":
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
}

func handleGetData(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	responses := map[string]string{
		"settings:/system/primaryMacAddress": `[{"type":"string_","string_":"AA:BB:CC:DD:EE:FF"}]`,
		"settings:/deviceName":               `[{"type":"string_","string_":"Living Room"}]`,
		"settings:/releasetext":              `[{"type":"string_","string_":"ls60w_1.2.3"}]`,
		"settings:/kef/host/maximumVolume":   `[{"type":"i32_","i32_":100}]`,
		"player:volume":                      `[{"type":"i32_","i32_":35}]`,
		"settings:/mediaPlayer/mute":         `[{"type":"bool_","bool_":"false"}]`,
		"settings:/kef/play/physicalSource":  `[{"type":"kefPhysicalSource","kefPhysicalSource":"wifi"}]`,
		"settings:/kef/host/speakerStatus":   `[{"type":"kefSpeakerStatus","kefSpeakerStatus":"powerOn"}]`,
		"settings:/kef/host/cableMode":       `[{"type":"kefCableMode","kefCableMode":"wireless"}]`,
		"player:player/data/playTime":        `[{"type":"i64_","i64_":125000}]`,
		"player:player/data":                 `[{"state":"playing","status":{"duration":180000},"trackRoles":{"title":"Test Song","icon":"http://example.com/art.jpg","mediaData":{"metaData":{"artist":"Test Artist","album":"Test Album"},"resources":[]}}}]`,
	}

	if resp, ok := responses[path]; ok {
		_, _ = w.Write([]byte(resp))
	} else {
		http.Error(w, "unknown path: "+path, http.StatusNotFound)
	}
}

func handleGetRows(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	if path == "grouping:members" {
		_, _ = w.Write([]byte(`{"groupingMember":[{"master":{"id":"speaker-123","name":"Living Room"},"follower":{"id":"","name":""}}]}`))
	} else {
		http.Error(w, "unknown path", http.StatusNotFound)
	}
}

func TestNewSpeaker(t *testing.T) {
	server := createMockSpeakerServer(t)
	defer server.Close()

	t.Run("valid IP address", func(t *testing.T) {
		speaker, err := NewSpeaker(strings.TrimPrefix(server.URL, "http://"))
		if err != nil {
			t.Fatalf("NewSpeaker() error = %v", err)
		}

		if speaker.Name != "Living Room" {
			t.Errorf("Name = %q, want %q", speaker.Name, "Living Room")
		}
		if speaker.MacAddress != "AA:BB:CC:DD:EE:FF" {
			t.Errorf("MacAddress = %q, want %q", speaker.MacAddress, "AA:BB:CC:DD:EE:FF")
		}
		if speaker.Model != "KEF LS60 Wireless" {
			t.Errorf("Model = %q, want %q", speaker.Model, "KEF LS60 Wireless")
		}
		if speaker.FirmwareVersion != "1.2.3" {
			t.Errorf("FirmwareVersion = %q, want %q", speaker.FirmwareVersion, "1.2.3")
		}
		if speaker.MaxVolume != 100 {
			t.Errorf("MaxVolume = %d, want %d", speaker.MaxVolume, 100)
		}
		if speaker.ID != "speaker-123" {
			t.Errorf("ID = %q, want %q", speaker.ID, "speaker-123")
		}
	})

	t.Run("empty IP address", func(t *testing.T) {
		_, err := NewSpeaker("")
		if !errors.Is(err, ErrEmptyIPAddress) {
			t.Errorf("NewSpeaker() error = %v, want %v", err, ErrEmptyIPAddress)
		}
	})

	t.Run("invalid IP address", func(t *testing.T) {
		_, err := NewSpeaker("192.168.1.999:99999")
		if err == nil {
			t.Error("NewSpeaker() should fail for invalid IP")
		}
	})
}

func TestNewSpeakerWithOptions(t *testing.T) {
	server := createMockSpeakerServer(t)
	defer server.Close()

	customClient := &http.Client{Timeout: 10 * time.Second}

	speaker, err := NewSpeaker(
		strings.TrimPrefix(server.URL, "http://"),
		WithTimeout(5*time.Second),
		WithHTTPClient(customClient),
	)
	if err != nil {
		t.Fatalf("NewSpeaker() error = %v", err)
	}

	if speaker.timeout != 5*time.Second {
		t.Errorf("timeout = %v, want %v", speaker.timeout, 5*time.Second)
	}

	if speaker.client != customClient {
		t.Error("client was not set correctly")
	}
}

func TestSpeakerVolume(t *testing.T) {
	server := createMockSpeakerServer(t)
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: strings.TrimPrefix(server.URL, "http://"),
		timeout:   DefaultTimeout,
	}
	ctx := context.Background()

	t.Run("GetVolume", func(t *testing.T) {
		volume, err := speaker.GetVolume(ctx)
		if err != nil {
			t.Fatalf("GetVolume() error = %v", err)
		}
		if volume != 35 {
			t.Errorf("GetVolume() = %d, want %d", volume, 35)
		}
	})

	t.Run("SetVolume", func(t *testing.T) {
		err := speaker.SetVolume(ctx, 50)
		if err != nil {
			t.Errorf("SetVolume() error = %v", err)
		}
	})

	t.Run("GetVolume with canceled context", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := speaker.GetVolume(cancelledCtx)
		if err == nil {
			t.Error("GetVolume() should fail with canceled context")
		}
	})

	t.Run("SetVolume with timeout", func(t *testing.T) {
		timeoutCtx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(1 * time.Millisecond) // Ensure timeout

		err := speaker.SetVolume(timeoutCtx, 50)
		if err == nil {
			t.Error("SetVolume() should fail with expired context")
		}
	})
}

func TestSpeakerMute(t *testing.T) {
	server := createMockSpeakerServer(t)
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: strings.TrimPrefix(server.URL, "http://"),
		timeout:   DefaultTimeout,
	}
	ctx := context.Background()

	t.Run("IsMuted", func(t *testing.T) {
		muted, err := speaker.IsMuted(ctx)
		if err != nil {
			t.Fatalf("IsMuted() error = %v", err)
		}
		if muted != false {
			t.Errorf("IsMuted() = %v, want %v", muted, false)
		}
	})

	t.Run("Mute", func(t *testing.T) {
		err := speaker.Mute(ctx)
		if err != nil {
			t.Errorf("Mute() error = %v", err)
		}
	})

	t.Run("Unmute", func(t *testing.T) {
		err := speaker.Unmute(ctx)
		if err != nil {
			t.Errorf("Unmute() error = %v", err)
		}
	})
}

func TestSpeakerSource(t *testing.T) {
	server := createMockSpeakerServer(t)
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: strings.TrimPrefix(server.URL, "http://"),
		timeout:   DefaultTimeout,
	}
	ctx := context.Background()

	t.Run("Source", func(t *testing.T) {
		source, err := speaker.Source(ctx)
		if err != nil {
			t.Fatalf("Source() error = %v", err)
		}
		if source != SourceWiFi {
			t.Errorf("Source() = %v, want %v", source, SourceWiFi)
		}
	})

	t.Run("SetSource", func(t *testing.T) {
		err := speaker.SetSource(ctx, SourceBluetooth)
		if err != nil {
			t.Errorf("SetSource() error = %v", err)
		}
	})

	t.Run("CanControlPlayback wifi", func(t *testing.T) {
		canControl, err := speaker.CanControlPlayback(ctx)
		if err != nil {
			t.Fatalf("CanControlPlayback() error = %v", err)
		}
		if !canControl {
			t.Error("CanControlPlayback() should return true for WiFi source")
		}
	})
}

func TestSpeakerPower(t *testing.T) {
	server := createMockSpeakerServer(t)
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: strings.TrimPrefix(server.URL, "http://"),
		timeout:   DefaultTimeout,
	}
	ctx := context.Background()

	t.Run("IsPoweredOn", func(t *testing.T) {
		isPowered, err := speaker.IsPoweredOn(ctx)
		if err != nil {
			t.Fatalf("IsPoweredOn() error = %v", err)
		}
		if !isPowered {
			t.Error("IsPoweredOn() should return true for powerOn status")
		}
	})

	t.Run("SpeakerState", func(t *testing.T) {
		state, err := speaker.SpeakerState(ctx)
		if err != nil {
			t.Fatalf("SpeakerState() error = %v", err)
		}
		if state != SpeakerStatusOn {
			t.Errorf("SpeakerState() = %v, want %v", state, SpeakerStatusOn)
		}
	})

	t.Run("PowerOff", func(t *testing.T) {
		err := speaker.PowerOff(ctx)
		if err != nil {
			t.Errorf("PowerOff() error = %v", err)
		}
	})
}

func TestSpeakerMaxVolume(t *testing.T) {
	server := createMockSpeakerServer(t)
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: strings.TrimPrefix(server.URL, "http://"),
		timeout:   DefaultTimeout,
	}
	ctx := context.Background()

	t.Run("GetMaxVolume", func(t *testing.T) {
		maxVol, err := speaker.GetMaxVolume(ctx)
		if err != nil {
			t.Fatalf("GetMaxVolume() error = %v", err)
		}
		if maxVol != 100 {
			t.Errorf("GetMaxVolume() = %d, want %d", maxVol, 100)
		}
		if speaker.MaxVolume != 100 {
			t.Errorf("speaker.MaxVolume = %d, want %d", speaker.MaxVolume, 100)
		}
	})

	t.Run("SetMaxVolume", func(t *testing.T) {
		err := speaker.SetMaxVolume(ctx, 80)
		if err != nil {
			t.Errorf("SetMaxVolume() error = %v", err)
		}
		if speaker.MaxVolume != 80 {
			t.Errorf("speaker.MaxVolume = %d, want %d", speaker.MaxVolume, 80)
		}
	})
}

func TestSpeakerPlayback(t *testing.T) {
	server := createMockSpeakerServer(t)
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: strings.TrimPrefix(server.URL, "http://"),
		timeout:   DefaultTimeout,
	}
	ctx := context.Background()

	t.Run("PlayPause", func(t *testing.T) {
		err := speaker.PlayPause(ctx)
		if err != nil {
			t.Errorf("PlayPause() error = %v", err)
		}
	})

	t.Run("NextTrack", func(t *testing.T) {
		err := speaker.NextTrack(ctx)
		if err != nil {
			t.Errorf("NextTrack() error = %v", err)
		}
	})

	t.Run("PreviousTrack", func(t *testing.T) {
		err := speaker.PreviousTrack(ctx)
		if err != nil {
			t.Errorf("PreviousTrack() error = %v", err)
		}
	})

	t.Run("IsPlaying", func(t *testing.T) {
		isPlaying, err := speaker.IsPlaying(ctx)
		if err != nil {
			t.Fatalf("IsPlaying() error = %v", err)
		}
		if !isPlaying {
			t.Error("IsPlaying() should return true")
		}
	})

	t.Run("SongProgressMS", func(t *testing.T) {
		progress, err := speaker.SongProgressMS(ctx)
		if err != nil {
			t.Fatalf("SongProgressMS() error = %v", err)
		}
		if progress != 125000 {
			t.Errorf("SongProgressMS() = %d, want %d", progress, 125000)
		}
	})

	t.Run("SongProgress", func(t *testing.T) {
		progress, err := speaker.SongProgress(ctx)
		if err != nil {
			t.Fatalf("SongProgress() error = %v", err)
		}
		// 125000ms = 2:05
		if progress != "2:05" {
			t.Errorf("SongProgress() = %q, want %q", progress, "2:05")
		}
	})
}

func TestSpeakerNetworkMode(t *testing.T) {
	server := createMockSpeakerServer(t)
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: strings.TrimPrefix(server.URL, "http://"),
		timeout:   DefaultTimeout,
	}
	ctx := context.Background()

	mode, err := speaker.NetworkOperationMode(ctx)
	if err != nil {
		t.Fatalf("NetworkOperationMode() error = %v", err)
	}
	if mode != Wireless {
		t.Errorf("NetworkOperationMode() = %v, want %v", mode, Wireless)
	}
}

func TestSpeakerPlayerData(t *testing.T) {
	server := createMockSpeakerServer(t)
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: strings.TrimPrefix(server.URL, "http://"),
		timeout:   DefaultTimeout,
	}
	ctx := context.Background()

	pd, err := speaker.PlayerData(ctx)
	if err != nil {
		t.Fatalf("PlayerData() error = %v", err)
	}

	if pd.State != PlayerStatePlaying {
		t.Errorf("State = %q, want %q", pd.State, PlayerStatePlaying)
	}
	if pd.TrackRoles.Title != "Test Song" {
		t.Errorf("Title = %q, want %q", pd.TrackRoles.Title, "Test Song")
	}
	if pd.TrackRoles.MediaData.MetaData.Artist != "Test Artist" {
		t.Errorf("Artist = %q, want %q", pd.TrackRoles.MediaData.MetaData.Artist, "Test Artist")
	}
	if pd.Status.Duration != 180000 {
		t.Errorf("Duration = %d, want %d", pd.Status.Duration, 180000)
	}
}

func TestModels(t *testing.T) {
	tests := []struct {
		modelID string
		want    string
	}{
		{"lsxii", "KEF LSX II"},
		{"ls502w", "KEF LS50 II Wireless"},
		{"ls60w", "KEF LS60 Wireless"},
		{"LS60W", "KEF LS60 Wireless"},
		{"unknown", ""}, // Unknown models return empty string
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := Models[tt.modelID]
			if got != tt.want {
				t.Errorf("Models[%q] = %q, want %q", tt.modelID, got, tt.want)
			}
		})
	}
}

func TestPlayerResourceString(t *testing.T) {
	tests := []struct {
		duration int
		want     string
	}{
		{0, "0:00"},
		{60000, "1:00"},
		{90000, "1:30"},
		{180000, "3:00"},
		{185000, "3:05"},
		{3600000, "60:00"}, // 1 hour
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			pr := PlayerResource{Duration: tt.duration}
			got := pr.String()
			if got != tt.want {
				t.Errorf("PlayerResource{Duration: %d}.String() = %q, want %q", tt.duration, got, tt.want)
			}
		})
	}
}

// Edge case tests

func TestNewSpeakerUnreachable(t *testing.T) {
	// Test with a non-routable IP address
	_, err := NewSpeaker("192.0.2.1", WithTimeout(100*time.Millisecond))
	if err == nil {
		t.Error("NewSpeaker() should fail for unreachable host")
	}
}

func TestSpeakerServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: strings.TrimPrefix(server.URL, "http://"),
		timeout:   DefaultTimeout,
	}
	ctx := context.Background()

	_, err := speaker.GetVolume(ctx)
	if err == nil {
		t.Error("GetVolume() should fail on server error")
	}
}

func TestSpeakerMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: strings.TrimPrefix(server.URL, "http://"),
		timeout:   DefaultTimeout,
	}
	ctx := context.Background()

	_, err := speaker.GetVolume(ctx)
	if err == nil {
		t.Error("GetVolume() should fail on malformed response")
	}
}

func TestSpeakerEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: strings.TrimPrefix(server.URL, "http://"),
		timeout:   DefaultTimeout,
	}
	ctx := context.Background()

	_, err := speaker.GetVolume(ctx)
	if err == nil {
		t.Error("GetVolume() should fail on empty response")
	}
}

func TestSpeakerWrongType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Return a string when expecting an int
		_, _ = w.Write([]byte(`[{"type":"string_","string_":"not a number"}]`))
	}))
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: strings.TrimPrefix(server.URL, "http://"),
		timeout:   DefaultTimeout,
	}
	ctx := context.Background()

	_, err := speaker.GetVolume(ctx)
	if err == nil {
		t.Error("GetVolume() should fail on wrong type")
	}
}
