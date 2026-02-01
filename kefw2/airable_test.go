package kefw2

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAirableClient_GetRows(t *testing.T) {
	// Mock server to simulate the API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/getRows" {
			response := RowsResponse{
				RowsCount: 1,
				Rows: []ContentItem{
					{
						Title: "Test Item",
						Type:  "container",
						ID:    "test_id",
						Path:  "test_path",
					},
				},
				RowsRedirect: "airable:https://mock.airable.io/airable/radios",
			}
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
	defer mockServer.Close()

	// Initialize AirableClient with the mock server
	client := NewAirableClient(&KEFSpeaker{IPAddress: mockServer.URL[7:]})

	// Call GetRows
	rows, err := client.GetRows("test_path", 0, 10)
	if err != nil {
		t.Fatalf("GetRows failed: %v", err)
	}

	// Validate response
	if rows.RowsCount != 1 {
		t.Errorf("Expected RowsCount 1, got %d", rows.RowsCount)
	}
	if rows.Rows[0].Title != "Test Item" {
		t.Errorf("Expected Title 'Test Item', got '%s'", rows.Rows[0].Title)
	}
}

func TestAirableClient_GetRadioMenu(t *testing.T) {
	// Mock server to simulate the API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/getRows" {
			response := RowsResponse{
				RowsCount:    0,
				RowsRedirect: "airable:https://mock.airable.io/airable/radios",
			}
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
	defer mockServer.Close()

	// Initialize AirableClient with the mock server
	client := NewAirableClient(&KEFSpeaker{IPAddress: mockServer.URL[7:]})

	// Call GetRadioMenu - this should set RadioBaseURL from redirect
	_, err := client.GetRadioMenu()
	if err != nil {
		t.Fatalf("GetRadioMenu failed: %v", err)
	}

	// Validate RadioBaseURL was set
	if client.RadioBaseURL != "airable:https://mock.airable.io/airable/radios" {
		t.Errorf("Expected RadioBaseURL 'airable:https://mock.airable.io/airable/radios', got '%s'", client.RadioBaseURL)
	}
}

func TestAirableClient_SearchRadio(t *testing.T) {
	// Mock server to simulate the API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/getRows" {
			// Check if this is a search query or menu query
			path := r.URL.Query().Get("path")
			if path == "ui:/airableradios" {
				// Return redirect to set base URL
				response := RowsResponse{
					RowsRedirect: "airable:https://mock.airable.io/airable/radios",
				}
				_ = json.NewEncoder(w).Encode(response)
			} else {
				// Return search results
				response := RowsResponse{
					RowsCount: 1,
					Rows: []ContentItem{
						{
							Title:     "Radio Station 1",
							Type:      "audio",
							AudioType: "audioBroadcast",
							ID:        "station_1",
							Path:      "airable:https://mock.airable.io/station/1",
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			}
		}
	}))
	defer mockServer.Close()

	// Initialize AirableClient with the mock server
	client := NewAirableClient(&KEFSpeaker{IPAddress: mockServer.URL[7:]})

	// Call SearchRadio
	rows, err := client.SearchRadio("jazz")
	if err != nil {
		t.Fatalf("SearchRadio failed: %v", err)
	}

	// Validate response
	if rows.RowsCount != 1 {
		t.Errorf("Expected RowsCount 1, got %d", rows.RowsCount)
	}
	if rows.Rows[0].Title != "Radio Station 1" {
		t.Errorf("Expected Title 'Radio Station 1', got '%s'", rows.Rows[0].Title)
	}
	if rows.Rows[0].Type != "audio" {
		t.Errorf("Expected Type 'audio', got '%s'", rows.Rows[0].Type)
	}
}

func TestAirableClient_ClearPlaylist(t *testing.T) {
	// Mock server to simulate the API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/setData" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer mockServer.Close()

	// Initialize AirableClient with the mock server
	client := NewAirableClient(&KEFSpeaker{IPAddress: mockServer.URL[7:]})

	// Call ClearPlaylist
	err := client.ClearPlaylist()
	if err != nil {
		t.Fatalf("ClearPlaylist failed: %v", err)
	}
}

func TestAirableClient_SetData(t *testing.T) {
	// Mock server to simulate the API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/setData" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"xclass":"NsdkActionReply","result":{"path":"test/result"}}`))
		}
	}))
	defer mockServer.Close()

	// Initialize AirableClient with the mock server
	client := NewAirableClient(&KEFSpeaker{IPAddress: mockServer.URL[7:]})

	// Call SetData
	payload := map[string]interface{}{
		"path": "test:path",
		"role": "activate",
	}
	resp, err := client.SetData(payload)
	if err != nil {
		t.Fatalf("SetData failed: %v", err)
	}

	// Validate response
	if resp.Result.Path != "test/result" {
		t.Errorf("Expected result path 'test/result', got '%s'", resp.Result.Path)
	}
}

func TestContentItem_GetBestImage(t *testing.T) {
	// Test with images
	item := ContentItem{
		Title: "Test",
		Icon:  "http://example.com/icon.png",
		Images: &ImageSet{
			Images: []Image{
				{URL: "http://example.com/small.png", Width: 64, Height: 64},
				{URL: "http://example.com/large.png", Width: 512, Height: 512},
				{URL: "http://example.com/medium.png", Width: 256, Height: 256},
			},
		},
	}

	best := item.GetBestImage()
	if best != "http://example.com/large.png" {
		t.Errorf("Expected largest image, got '%s'", best)
	}

	// Test without images
	itemNoImages := ContentItem{
		Title: "Test",
		Icon:  "http://example.com/icon.png",
	}
	icon := itemNoImages.GetBestImage()
	if icon != "http://example.com/icon.png" {
		t.Errorf("Expected icon fallback, got '%s'", icon)
	}
}

func TestContentItem_GetThumbnail(t *testing.T) {
	// Test with images
	item := ContentItem{
		Title: "Test",
		Icon:  "http://example.com/icon.png",
		Images: &ImageSet{
			Images: []Image{
				{URL: "http://example.com/64.png", Width: 64, Height: 64},
				{URL: "http://example.com/128.png", Width: 128, Height: 128},
				{URL: "http://example.com/256.png", Width: 256, Height: 256},
			},
		},
	}

	thumb := item.GetThumbnail()
	if thumb != "http://example.com/128.png" {
		t.Errorf("Expected 128px thumbnail, got '%s'", thumb)
	}
}

func TestAirableClient_GetPodcastMenu(t *testing.T) {
	// Mock server to simulate the API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/getRows" {
			response := RowsResponse{
				RowsCount:    0,
				RowsRedirect: "airable:https://mock.airable.io/airable/feeds",
			}
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
	defer mockServer.Close()

	// Initialize AirableClient with the mock server
	client := NewAirableClient(&KEFSpeaker{IPAddress: mockServer.URL[7:]})

	// Call GetPodcastMenu
	_, err := client.GetPodcastMenu()
	if err != nil {
		t.Fatalf("GetPodcastMenu failed: %v", err)
	}

	// Validate PodcastBaseURL was set
	if client.PodcastBaseURL != "airable:https://mock.airable.io/airable/feeds" {
		t.Errorf("Expected PodcastBaseURL 'airable:https://mock.airable.io/airable/feeds', got '%s'", client.PodcastBaseURL)
	}
}

func TestAirableClient_GetMediaServers(t *testing.T) {
	// Mock server to simulate the API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/getRows" {
			response := RowsResponse{
				RowsCount: 2,
				Rows: []ContentItem{
					{
						Title: "Plex Media Server",
						Type:  "container",
						Path:  "upnp:/uuid:abc123?itemType=server",
					},
					{
						Title: "Sonos Library",
						Type:  "container",
						Path:  "upnp:/uuid:def456?itemType=server",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
	defer mockServer.Close()

	// Initialize AirableClient with the mock server
	client := NewAirableClient(&KEFSpeaker{IPAddress: mockServer.URL[7:]})

	// Call GetMediaServers
	rows, err := client.GetMediaServers()
	if err != nil {
		t.Fatalf("GetMediaServers failed: %v", err)
	}

	// Validate response
	if rows.RowsCount != 2 {
		t.Errorf("Expected RowsCount 2, got %d", rows.RowsCount)
	}
	if rows.Rows[0].Title != "Plex Media Server" {
		t.Errorf("Expected first server 'Plex Media Server', got '%s'", rows.Rows[0].Title)
	}
}
