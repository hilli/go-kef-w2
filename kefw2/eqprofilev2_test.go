// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package kefw2

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestSetEQProfileV2(t *testing.T) {
	profile := EQProfileV2{
		AudioPolarity: "normal", Balance: -2, BassExtension: "standard",
		DeskMode: true, DeskModeSetting: -3.5, HighPassMode: true, HighPassModeFreq: 92.5,
		IsExpertMode: true, IsKW1: false, PhaseCorrection: true, ProfileID: "profile-id",
		ProfileName: "Desk", SoundProfile: "default", SubEnableStereo: false,
		SubOutLPFreq: 95, SubwooferCount: 1, SubwooferGain: 2, SubwooferOut: true,
		SubwooferPolarity: "normal", SubwooferPreset: "custom", TrebleAmount: 1.5,
		WallMode: true, WallModeSetting: -2.5, WirelessSub: "none",
	}

	server := mockSpeakerServer(t, map[string]http.HandlerFunc{
		"/api/setData": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", r.Method)
			}
			var request struct {
				Path  string `json:"path"`
				Role  string `json:"role"`
				Roles string `json:"roles"`
				Value struct {
					Type    string      `json:"type"`
					Profile EQProfileV2 `json:"kefEqProfileV2"`
				} `json:"value"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decoding request: %v", err)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if request.Path != "kef:eqProfile/v2" || request.Role != "value" || request.Roles != "" {
				t.Errorf("request envelope = %+v", request)
			}
			if request.Value.Type != "kefEqProfileV2" || request.Value.Profile != profile {
				t.Errorf("profile payload = %+v, want %+v", request.Value, profile)
			}
			w.WriteHeader(http.StatusOK)
		},
	})
	defer server.Close()

	speaker := &KEFSpeaker{IPAddress: extractIPFromURL(server.URL)}
	if err := speaker.SetEQProfileV2(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
}
