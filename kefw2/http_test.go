package kefw2

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockSpeakerServer creates a test server that simulates KEF speaker responses
func mockSpeakerServer(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	for path, handler := range handlers {
		mux.HandleFunc(path, handler)
	}

	return httptest.NewServer(mux)
}

// extractIPFromURL gets the host:port from a test server URL
func extractIPFromURL(url string) string {
	// Remove "http://" prefix
	return strings.TrimPrefix(url, "http://")
}

func TestGetData(t *testing.T) {
	// Create a mock server
	server := mockSpeakerServer(t, map[string]http.HandlerFunc{
		"/api/getData": func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Query().Get("path")
			roles := r.URL.Query().Get("roles")

			if path == "" {
				http.Error(w, "missing path", http.StatusBadRequest)
				return
			}

			if roles != "value" {
				http.Error(w, "invalid roles", http.StatusBadRequest)
				return
			}

			switch path {
			case "player:volume":
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`[{"type":"i32_","i32_":42}]`))
			case "settings:/deviceName":
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`[{"type":"string_","string_":"Living Room"}]`))
			case "settings:/kef/play/physicalSource":
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`[{"type":"kefPhysicalSource","kefPhysicalSource":"wifi"}]`))
			default:
				http.Error(w, "unknown path", http.StatusNotFound)
			}
		},
	})
	defer server.Close()

	// Create speaker with test server
	speaker := &KEFSpeaker{
		IPAddress: extractIPFromURL(server.URL),
		timeout:   DefaultTimeout,
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "get volume",
			path:    "player:volume",
			wantErr: false,
		},
		{
			name:    "get device name",
			path:    "settings:/deviceName",
			wantErr: false,
		},
		{
			name:    "get source",
			path:    "settings:/kef/play/physicalSource",
			wantErr: false,
		},
		{
			name:    "unknown path",
			path:    "unknown:path",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := speaker.getData(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("getData() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(data) == 0 {
				t.Error("getData() returned empty data")
			}
		})
	}
}

func TestSetTypedValue(t *testing.T) {
	var receivedBody KEFPostRequest

	server := mockSpeakerServer(t, map[string]http.HandlerFunc{
		"/api/setData": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}

			w.WriteHeader(http.StatusOK)
		},
	})
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: extractIPFromURL(server.URL),
		timeout:   DefaultTimeout,
	}

	tests := []struct {
		name    string
		path    string
		value   any
		wantErr bool
	}{
		{
			name:    "set int value",
			path:    "player:volume",
			value:   50,
			wantErr: false,
		},
		{
			name:    "set bool value",
			path:    "settings:/mediaPlayer/mute",
			value:   true,
			wantErr: false,
		},
		{
			name:    "set string value",
			path:    "settings:/deviceName",
			value:   "New Name",
			wantErr: false,
		},
		{
			name:    "set source value",
			path:    "settings:/kef/play/physicalSource",
			value:   SourceWiFi,
			wantErr: false,
		},
		{
			name:    "set speaker status",
			path:    "settings:/kef/host/speakerStatus",
			value:   SpeakerStatusOn,
			wantErr: false,
		},
		{
			name:    "unsupported type",
			path:    "some:path",
			value:   struct{}{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := speaker.setTypedValue(tt.path, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("setTypedValue() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if receivedBody.Path != tt.path {
					t.Errorf("setTypedValue() sent path = %v, want %v", receivedBody.Path, tt.path)
				}
			}
		})
	}
}

func TestSetActivate(t *testing.T) {
	var receivedBody KEFPostRequest

	server := mockSpeakerServer(t, map[string]http.HandlerFunc{
		"/api/setData": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}

			if receivedBody.Roles != "activate" {
				http.Error(w, "expected activate role", http.StatusBadRequest)
				return
			}

			w.WriteHeader(http.StatusOK)
		},
	})
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: extractIPFromURL(server.URL),
		timeout:   DefaultTimeout,
	}

	tests := []struct {
		name    string
		path    string
		item    string
		value   string
		wantErr bool
	}{
		{
			name:    "play/pause",
			path:    "player:player/control",
			item:    "control",
			value:   "pause",
			wantErr: false,
		},
		{
			name:    "next track",
			path:    "player:player/control",
			item:    "control",
			value:   "next",
			wantErr: false,
		},
		{
			name:    "previous track",
			path:    "player:player/control",
			item:    "control",
			value:   "previous",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := speaker.setActivate(tt.path, tt.item, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("setActivate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if receivedBody.Path != tt.path {
					t.Errorf("setActivate() sent path = %v, want %v", receivedBody.Path, tt.path)
				}
				if receivedBody.Roles != "activate" {
					t.Errorf("setActivate() sent roles = %v, want 'activate'", receivedBody.Roles)
				}
			}
		})
	}
}

func TestHandleConnectionError(t *testing.T) {
	speaker := &KEFSpeaker{IPAddress: "192.168.1.100"}

	tests := []struct {
		name    string
		errMsg  string
		wantErr error
	}{
		{
			name:    "connection refused",
			errMsg:  "dial tcp: connection refused",
			wantErr: ErrConnectionRefused,
		},
		{
			name:    "timeout",
			errMsg:  "request timeout exceeded",
			wantErr: ErrConnectionTimeout,
		},
		{
			name:    "no such host",
			errMsg:  "no such host found",
			wantErr: ErrHostNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputErr := &mockError{msg: tt.errMsg}
			result := speaker.handleConnectionError(inputErr)

			if result == nil {
				t.Fatal("handleConnectionError() returned nil")
			}

			// Check that the error wraps the expected error type
			if !strings.Contains(result.Error(), tt.wantErr.Error()) {
				t.Errorf("handleConnectionError() = %v, want to contain %v", result, tt.wantErr)
			}
		})
	}
}

// mockError implements error interface for testing
type mockError struct {
	msg     string
	timeout bool
}

func (e *mockError) Error() string { return e.msg }
func (e *mockError) Timeout() bool { return e.timeout }

func TestHTTPClientReuse(t *testing.T) {
	speaker := &KEFSpeaker{
		IPAddress: "192.168.1.100",
		timeout:   5 * time.Second,
	}

	// First call should create client
	client1 := speaker.httpClient()
	if client1 == nil {
		t.Fatal("httpClient() returned nil")
	}

	// Second call should return same client
	client2 := speaker.httpClient()
	if client1 != client2 {
		t.Error("httpClient() should return the same client instance")
	}
}

func TestDoRequestWithContext(t *testing.T) {
	requestReceived := make(chan struct{})

	server := mockSpeakerServer(t, map[string]http.HandlerFunc{
		"/api/getData": func(w http.ResponseWriter, r *http.Request) {
			close(requestReceived)
			// Simulate slow response
			time.Sleep(100 * time.Millisecond)
			w.Write([]byte(`[{"type":"i32_","i32_":1}]`))
		},
	})
	defer server.Close()

	speaker := &KEFSpeaker{
		IPAddress: extractIPFromURL(server.URL),
		timeout:   50 * time.Millisecond, // Short timeout
	}

	// Make request with short timeout
	_, err := speaker.getData("player:volume")

	// Should timeout
	if err == nil {
		t.Error("getData() should have timed out")
	}
}

func TestTypeEncoder(t *testing.T) {
	tests := []struct {
		name      string
		value     TypeEncoder
		wantType  string
		wantValue string
	}{
		{
			name:      "source wifi",
			value:     SourceWiFi,
			wantType:  "kefPhysicalSource",
			wantValue: "wifi",
		},
		{
			name:      "source bluetooth",
			value:     SourceBluetooth,
			wantType:  "kefPhysicalSource",
			wantValue: "bluetooth",
		},
		{
			name:      "speaker status on",
			value:     SpeakerStatusOn,
			wantType:  "kefSpeakerStatus",
			wantValue: `"powerOn"`,
		},
		{
			name:      "cable mode wired",
			value:     Wired,
			wantType:  "kefCableMode",
			wantValue: `"wired"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typeName, typeValue := tt.value.KEFTypeInfo()
			if typeName != tt.wantType {
				t.Errorf("KEFTypeInfo() type = %v, want %v", typeName, tt.wantType)
			}
			if typeValue != tt.wantValue {
				t.Errorf("KEFTypeInfo() value = %v, want %v", typeValue, tt.wantValue)
			}
		})
	}
}
