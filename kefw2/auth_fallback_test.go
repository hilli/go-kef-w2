// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package kefw2

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

type authFallbackTransport func(*http.Request) (*http.Response, error)

func (f authFallbackTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func authFallbackResponse(status int) *http.Response {
	body := `{"error":"credential-value"}`
	if status == http.StatusOK {
		body = `{}`
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func mockAuthenticatedControl(t *testing.T, transport authFallbackTransport) {
	t.Helper()
	client := authHTTPClient()
	previousTransport, previousDebug := client.Transport, authDebug
	client.Transport, authDebug = transport, false
	t.Cleanup(func() {
		client.Transport, authDebug = previousTransport, previousDebug
	})
}

func fallbackTestClients(transport authFallbackTransport) (*AirableClient, *KEFSpeaker) {
	httpClient := &http.Client{Transport: transport}
	speaker := &KEFSpeaker{IPAddress: "192.0.2.1", client: httpClient}
	client := NewAirableClient(speaker)
	client.HTTPClient = httpClient
	return client, speaker
}

func TestAuthenticatedFallbackUsesSpeakerHost(t *testing.T) {
	for _, tc := range []struct {
		address string
		want    string
	}{
		{"192.0.2.1:8080", "192.0.2.1:4430"},
		{"speaker.local:8080", "speaker.local:4430"},
		{"[2001:db8::1]:8080", "[2001:db8::1]:4430"},
		{"[2001:db8::1]", "[2001:db8::1]:4430"},
	} {
		for _, api := range []string{"speaker", "airable"} {
			t.Run(tc.address+"/"+api, func(t *testing.T) {
				attempts := 0
				mockAuthenticatedControl(t, func(r *http.Request) (*http.Response, error) {
					if r.URL.Host != tc.want {
						return nil, fmt.Errorf("unexpected authenticated host %q", r.URL.Host)
					}
					attempts++
					if attempts == 1 {
						return authFallbackResponse(http.StatusUnauthorized), nil
					}
					return authFallbackResponse(http.StatusOK), nil
				})
				client, speaker := fallbackTestClients(func(*http.Request) (*http.Response, error) {
					return authFallbackResponse(http.StatusUnauthorized), nil
				})
				speaker.IPAddress = tc.address
				var err error
				if api == "speaker" {
					err = speaker.Stop(context.Background())
				} else {
					err = client.ClearPlaylist()
				}
				if err != nil || attempts != 3 {
					t.Fatalf("authenticated control/provisioning failed: err=%v attempts=%d", err, attempts)
				}
			})
		}
	}
}

func TestRadioPlaybackAuthenticatesRejectedMutations(t *testing.T) {
	var pending []byte
	var publicWrites int
	var authenticated []string
	client, speaker := fallbackTestClients(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			response := authFallbackResponse(http.StatusOK)
			response.Body = io.NopCloser(strings.NewReader(`[{"type":"kefPhysicalSource","kefPhysicalSource":"wifi"}]`))
			return response, nil
		}
		publicWrites++
		var err error
		pending, err = io.ReadAll(r.Body)
		return authFallbackResponse(http.StatusUnauthorized), err
	})
	mockAuthenticatedControl(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Port() != "4430" || r.URL.Path != "/api/setData" ||
			!strings.HasPrefix(r.Header.Get("Authorization"), "HMAC_SHA256 ") {
			t.Error("rejected control was not retried on the signed TLS channel")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(body, pending) {
			t.Error("authentication changed the original playback payload")
		}
		var payload struct {
			Path  string `json:"path"`
			Value struct {
				Control string `json:"control"`
			} `json:"value"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		authenticated = append(authenticated, payload.Path+":"+payload.Value.Control)
		return authFallbackResponse(http.StatusOK), nil
	})
	station := &ContentItem{
		Title: "Jazz", Path: "airable:https://radio.example/id/radio/jazz",
		MediaData: &MediaData{Resources: []MediaResource{{URI: "https://stream.example/jazz"}}},
	}
	if err := client.PlayRadioStation(station); err != nil {
		t.Fatal(err)
	}
	if err := speaker.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	expected := []string{"playlists:pl/clear:", "player:player/control:play", "player:player/control:stop"}
	if publicWrites != 3 || !slices.Equal(authenticated, expected) {
		t.Fatalf("unexpected control attempts: public=%d signed=%v", publicWrites, authenticated)
	}
}

func TestControlDoesNotRetryOtherFailures(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden, http.StatusServiceUnavailable, 0} {
		for _, api := range []string{"airable", "speaker"} {
			t.Run(http.StatusText(status)+"/"+api, func(t *testing.T) {
				var public, signed int
				mockAuthenticatedControl(t, func(*http.Request) (*http.Response, error) {
					signed++
					return authFallbackResponse(http.StatusOK), nil
				})
				client, speaker := fallbackTestClients(func(*http.Request) (*http.Response, error) {
					public++
					if status == 0 {
						return nil, errors.New("connection dropped")
					}
					return authFallbackResponse(status), nil
				})
				var err error
				if api == "airable" {
					err = client.ClearPlaylist()
				} else {
					err = speaker.Stop(context.Background())
				}
				if (err != nil) != (status != http.StatusOK) || public != 1 || signed != 0 {
					t.Fatalf("unexpected retry/result: err=%v public=%d signed=%d", err, public, signed)
				}
			})
		}
	}
}

type brokenAuthBody struct{}

func (brokenAuthBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestAuthenticatedControlProvisioningAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name            string
		firstStatus     int
		finalStatus     int
		provisionStatus int
		brokenBody      bool
		wantError       string
		wantSigned      int
		wantProvision   int
	}{
		{"denied", 403, 403, 200, false, "HTTP 403", 1, 0},
		{"truncated reply", 200, 200, 200, true, "unexpected EOF", 1, 0},
		{"first use", 401, 200, 200, false, "", 2, 1},
		{"provision denied", 401, 200, 500, false, "HTTP 500", 1, 1},
		{"still unauthorized", 401, 401, 200, false, "HTTP 401", 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var signed, provisions int
			mockAuthenticatedControl(t, func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") == "" {
					provisions++
					var payload struct {
						Path string `json:"path"`
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Path != "webserver:addUser" {
						t.Error("unexpected unsigned request on the authenticated channel")
					}
					return authFallbackResponse(tc.provisionStatus), nil
				}
				signed++
				status := tc.finalStatus
				if signed == 1 {
					status = tc.firstStatus
				}
				response := authFallbackResponse(status)
				if tc.brokenBody {
					response.Body = io.NopCloser(brokenAuthBody{})
				}
				return response, nil
			})
			client, _ := fallbackTestClients(func(*http.Request) (*http.Response, error) {
				return authFallbackResponse(http.StatusUnauthorized), nil
			})
			err := client.ClearPlaylist()
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) ||
				strings.Contains(err.Error(), "credential-value") {
				t.Fatalf("missing failure or exposed raw authenticated response: %v", err)
			}
			if signed != tc.wantSigned || provisions != tc.wantProvision {
				t.Fatalf("unexpected retries: signed=%d provisions=%d", signed, provisions)
			}
		})
	}
}

func TestAuthenticatedControlRefusesRawDebugLogging(t *testing.T) {
	var signed int
	mockAuthenticatedControl(t, func(*http.Request) (*http.Response, error) {
		signed++
		return authFallbackResponse(http.StatusOK), nil
	})
	authDebug = true
	client, _ := fallbackTestClients(func(*http.Request) (*http.Response, error) {
		return authFallbackResponse(http.StatusUnauthorized), nil
	})
	if err := client.ClearPlaylist(); err == nil || !strings.Contains(err.Error(), "KEFW2_AUTH_DEBUG") || signed != 0 {
		t.Fatalf("unsafe logging was not refused before authentication: err=%v signed=%d", err, signed)
	}
}

func TestReadRejectionDoesNotBecomeAuthenticatedWrite(t *testing.T) {
	var signed int
	mockAuthenticatedControl(t, func(*http.Request) (*http.Response, error) {
		signed++
		return authFallbackResponse(http.StatusOK), nil
	})
	_, speaker := fallbackTestClients(func(*http.Request) (*http.Response, error) {
		return authFallbackResponse(http.StatusUnauthorized), nil
	})
	if _, err := speaker.Source(context.Background()); err == nil || signed != 0 {
		t.Fatalf("read rejection became a write: err=%v signed=%d", err, signed)
	}
}

func TestRebootPreservesHTTPRejections(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			mockAuthenticatedControl(t, func(*http.Request) (*http.Response, error) {
				response := authFallbackResponse(status)
				response.Body = io.NopCloser(brokenAuthBody{})
				return response, nil
			})
			speaker := &KEFSpeaker{IPAddress: "192.0.2.1"}
			err := speaker.Reboot(context.Background())
			if status == http.StatusOK {
				if err != nil {
					t.Fatalf("accepted reboot disconnect failed: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status)) {
				t.Fatalf("explicit HTTP rejection reported as success or lost its status: %v", err)
			}
		})
	}
}

func TestRebootDoesNotAcceptProvisioningDisconnect(t *testing.T) {
	mockAuthenticatedControl(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			return authFallbackResponse(http.StatusUnauthorized), nil
		}
		return nil, io.ErrUnexpectedEOF
	})
	speaker := &KEFSpeaker{IPAddress: "192.0.2.1"}
	if err := speaker.Reboot(context.Background()); err == nil {
		t.Fatal("authentication provisioning failure reported as a successful reboot")
	}
}
