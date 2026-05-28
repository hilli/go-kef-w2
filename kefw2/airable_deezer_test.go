package kefw2

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAirableClient_GetDeezerMenu(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/getRows" {
			path := r.URL.Query().Get("path")
			switch path {
			case "ui:/airabledeezer":
				response := RowsResponse{
					RowsRedirect: "airable:linkService_deezer",
				}
				_ = json.NewEncoder(w).Encode(response)
			case "airable:linkService_deezer":
				response := RowsResponse{
					RowsRedirect: "airable:https://mock.airable.io/deezer",
				}
				_ = json.NewEncoder(w).Encode(response)
			case "airable:https://mock.airable.io/deezer":
				response := RowsResponse{
					RowsCount: 3,
					Rows: []ContentItem{
						{Title: "Flow", Type: "audio", AudioType: "audioBroadcast", Path: "airable:https://mock.airable.io/id/deezer/program/flow"},
						{Title: "Charts", Type: "container", Path: "airable:https://mock.airable.io/deezer/charts"},
						{Title: "Search", Type: "container", Path: "airable:https://mock.airable.io/deezer/search"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			}
		}
	}))
	defer mockServer.Close()

	client := NewAirableClient(&KEFSpeaker{IPAddress: mockServer.URL[7:]})

	resp, err := client.GetDeezerMenu()
	if err != nil {
		t.Fatalf("GetDeezerMenu failed: %v", err)
	}

	if len(resp.Rows) != 3 {
		t.Errorf("Expected 3 rows, got %d", len(resp.Rows))
	}

	if client.DeezerBaseURL != "airable:https://mock.airable.io/deezer" {
		t.Errorf("Expected DeezerBaseURL 'airable:https://mock.airable.io/deezer', got '%s'", client.DeezerBaseURL)
	}
}

func TestAirableClient_SearchDeezer(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/getRows" {
			path := r.URL.Query().Get("path")
			switch path {
			case "ui:/airabledeezer":
				response := RowsResponse{
					RowsRedirect: "airable:https://mock.airable.io/deezer",
				}
				_ = json.NewEncoder(w).Encode(response)
			default:
				// Return search results for any other path
				response := RowsResponse{
					RowsCount: 2,
					Rows: []ContentItem{
						{Title: "Soulfly", Type: "container", Path: "airable:https://mock.airable.io/id/deezer/artist/537", ID: "airable://deezer/artist/537"},
						{Title: "Soulfly - Primitive", Type: "container", Path: "airable:https://mock.airable.io/id/deezer/album/123", ID: "airable://deezer/album/123"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			}
		}
	}))
	defer mockServer.Close()

	client := NewAirableClient(&KEFSpeaker{IPAddress: mockServer.URL[7:]})

	resp, err := client.SearchDeezer("soulfly")
	if err != nil {
		t.Fatalf("SearchDeezer failed: %v", err)
	}

	if resp.RowsCount != 2 {
		t.Errorf("Expected RowsCount 2, got %d", resp.RowsCount)
	}

	if resp.Rows[0].Title != "Soulfly" {
		t.Errorf("Expected first result 'Soulfly', got '%s'", resp.Rows[0].Title)
	}
}

func TestAirableClient_GetDeezerCharts(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/getRows" {
			path := r.URL.Query().Get("path")
			switch path {
			case "ui:/airabledeezer":
				response := RowsResponse{
					RowsRedirect: "airable:https://mock.airable.io/deezer",
				}
				_ = json.NewEncoder(w).Encode(response)
			default:
				response := RowsResponse{
					RowsCount: 2,
					Rows: []ContentItem{
						{Title: "Tracks", Type: "container", Path: "airable:https://mock.airable.io/deezer/charts/tracks"},
						{Title: "Albums", Type: "container", Path: "airable:https://mock.airable.io/deezer/charts/albums"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			}
		}
	}))
	defer mockServer.Close()

	client := NewAirableClient(&KEFSpeaker{IPAddress: mockServer.URL[7:]})

	resp, err := client.GetDeezerCharts()
	if err != nil {
		t.Fatalf("GetDeezerCharts failed: %v", err)
	}

	if len(resp.Rows) != 2 {
		t.Errorf("Expected 2 rows, got %d", len(resp.Rows))
	}
}

func TestAirableClient_GetDeezerMoods(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/getRows" {
			path := r.URL.Query().Get("path")
			switch path {
			case "ui:/airabledeezer":
				response := RowsResponse{
					RowsRedirect: "airable:https://mock.airable.io/deezer",
				}
				_ = json.NewEncoder(w).Encode(response)
			case "airable:https://mock.airable.io/deezer":
				response := RowsResponse{
					RowsCount: 5,
					Rows: []ContentItem{
						{Title: "Flow", Type: "audio", AudioType: "audioBroadcast"},
						{Title: "Happy", Type: "audio", AudioType: "audioBroadcast"},
						{Title: "Charts", Type: "container"},
						{Title: "Workout", Type: "audio", AudioType: "audioBroadcast"},
						{Title: "Search", Type: "container"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			}
		}
	}))
	defer mockServer.Close()

	client := NewAirableClient(&KEFSpeaker{IPAddress: mockServer.URL[7:]})

	moods, err := client.GetDeezerMoods()
	if err != nil {
		t.Fatalf("GetDeezerMoods failed: %v", err)
	}

	// Should only return audio items with audioBroadcast type (Flow, Happy, Workout)
	if len(moods) != 3 {
		t.Errorf("Expected 3 moods, got %d", len(moods))
	}

	expectedMoods := []string{"Flow", "Happy", "Workout"}
	for i, expected := range expectedMoods {
		if i < len(moods) && moods[i].Title != expected {
			t.Errorf("Expected mood[%d] = '%s', got '%s'", i, expected, moods[i].Title)
		}
	}
}

func TestAirableClient_PlayDeezerTrack(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/setData":
			// Accept any setData call (clear playlist + add to queue + play)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("null"))
		case "/api/getData":
			// Return empty queue for GetPlayQueue and player data for PlayQueueIndex
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"rows":[],"rowsCount":0}]`))
		}
	}))
	defer mockServer.Close()

	client := NewAirableClient(&KEFSpeaker{IPAddress: mockServer.URL[7:]})

	t.Run("track uses queue-based playback", func(t *testing.T) {
		track := &ContentItem{
			Title: "Aperture",
			Type:  "audio",
			Path:  "airable:https://mock.airable.io/id/deezer/track/123",
			ID:    "airable://deezer/track/123",
			MediaData: &MediaData{
				MetaData: MediaMetaData{
					ServiceID: "deezer",
					Artist:    "Harry Styles",
					Album:     "Aperture",
				},
				Resources: []MediaResource{
					{URI: "https://mock.airable.io/deezer/play/flac/1411/123", MimeType: "audio/flac", BitRate: 1411000, Codec: "FLAC"},
				},
			},
		}
		err := client.PlayDeezerTrack(track)
		if err != nil {
			t.Fatalf("PlayDeezerTrack (track) failed: %v", err)
		}
	})

	t.Run("stream uses mediaRoles playback", func(t *testing.T) {
		stream := &ContentItem{
			Title:     "Flow",
			Type:      "audio",
			AudioType: "audioBroadcast",
			Path:      "airable:https://mock.airable.io/id/deezer/program/flow",
			ID:        "airable://deezer/program/flow",
		}
		err := client.PlayDeezerTrack(stream)
		if err != nil {
			t.Fatalf("PlayDeezerTrack (stream) failed: %v", err)
		}
	})
}

func TestParseDeezerID(t *testing.T) {
	tests := []struct {
		id           string
		expectedType string
		expectedID   string
	}{
		{"airable://deezer/track/97311944", "track", "97311944"},
		{"airable://deezer/album/847240702", "album", "847240702"},
		{"airable://deezer/artist/537", "artist", "537"},
		{"airable://deezer/playlist/14961071463", "playlist", "14961071463"},
		{"airable://deezer/program/flow", "program", "flow"},
		{"invalid", "", ""},
		{"airable://other/track/123", "", ""},
	}

	for _, tt := range tests {
		itemType, itemID := parseDeezerID(tt.id)
		if itemType != tt.expectedType {
			t.Errorf("parseDeezerID(%q) type = %q, want %q", tt.id, itemType, tt.expectedType)
		}
		if itemID != tt.expectedID {
			t.Errorf("parseDeezerID(%q) id = %q, want %q", tt.id, itemID, tt.expectedID)
		}
	}
}

func TestAirableClient_AddDeezerFavorite(t *testing.T) {
	var receivedPath string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/setData":
			// Capture the action path from the request
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if p, ok := payload["path"].(string); ok {
				receivedPath = p
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("null"))
		case "/api/getRows":
			// Service discovery
			response := RowsResponse{
				RowsRedirect: "airable:https://mock.airable.io/deezer",
			}
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
	defer mockServer.Close()

	client := NewAirableClient(&KEFSpeaker{IPAddress: mockServer.URL[7:]})
	client.DeezerBaseURL = "airable:https://mock.airable.io/deezer"

	track := &ContentItem{
		Title: "Shade",
		Type:  "audio",
		ID:    "airable://deezer/track/97311944",
	}

	err := client.AddDeezerFavorite(track)
	if err != nil {
		t.Fatalf("AddDeezerFavorite failed: %v", err)
	}

	expectedPath := "airable:action:https://mock.airable.io/actions/deezer/track/97311944/favorites/insert"
	if receivedPath != expectedPath {
		t.Errorf("Expected action path '%s', got '%s'", expectedPath, receivedPath)
	}
}

func TestAirableClient_RemoveDeezerFavorite(t *testing.T) {
	var receivedPath string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/setData":
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if p, ok := payload["path"].(string); ok {
				receivedPath = p
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("null"))
		case "/api/getRows":
			response := RowsResponse{
				RowsRedirect: "airable:https://mock.airable.io/deezer",
			}
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
	defer mockServer.Close()

	client := NewAirableClient(&KEFSpeaker{IPAddress: mockServer.URL[7:]})
	client.DeezerBaseURL = "airable:https://mock.airable.io/deezer"

	track := &ContentItem{
		Title: "Shade",
		Type:  "audio",
		ID:    "airable://deezer/track/97311944",
	}

	err := client.RemoveDeezerFavorite(track)
	if err != nil {
		t.Fatalf("RemoveDeezerFavorite failed: %v", err)
	}

	expectedPath := "airable:action:https://mock.airable.io/actions/deezer/track/97311944/favorites/remove"
	if receivedPath != expectedPath {
		t.Errorf("Expected action path '%s', got '%s'", expectedPath, receivedPath)
	}
}
