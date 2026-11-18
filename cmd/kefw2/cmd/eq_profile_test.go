// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilli/go-kef-w2/kefw2"
)

func sampleEQProfile() kefw2.EQProfileV2 {
	return kefw2.EQProfileV2{
		AudioPolarity: "normal", Balance: -2, BassExtension: "standard",
		DeskMode: true, DeskModeSetting: -3.5, HighPassMode: true, HighPassModeFreq: 92.5,
		IsExpertMode: true, IsKW1: false, PhaseCorrection: true, ProfileID: "profile-id",
		ProfileName: "Desk", SoundProfile: "default", SubEnableStereo: false,
		SubOutLPFreq: 95, SubwooferCount: 1, SubwooferGain: 2, SubwooferOut: true,
		SubwooferPolarity: "normal", SubwooferPreset: "custom", TrebleAmount: 1.5,
		WallMode: true, WallModeSetting: -2.5, WirelessSub: "none",
	}
}

func writeEQProfileFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadEQProfileFile(t *testing.T) {
	profile := sampleEQProfile()
	valid, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	incomplete := strings.Replace(string(valid), `"balance":-2,`, "", 1)

	tests := []struct {
		name string
		data string
	}{
		{"empty object", `{}`},
		{"missing setting", incomplete},
		{"null setting", strings.Replace(string(valid), `"balance":-2`, `"balance":null`, 1)},
		{"trailing JSON", string(valid) + ` {}`},
		{"non-object", `null`},
		{"malformed JSON", `{not json}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := readEQProfileFile(writeEQProfileFile(t, []byte(tt.data))); err == nil {
				t.Fatal("invalid EQ profile was accepted")
			}
		})
	}

	got, _, err := readEQProfileFile(writeEQProfileFile(t, valid))
	if err != nil {
		t.Fatal(err)
	}
	if got != profile {
		t.Fatalf("profile = %+v, want %+v", got, profile)
	}
	withUnknown := strings.TrimSuffix(string(valid), "}") + `,"futureFirmwareField":{"value":true}}`
	_, raw, err := readEQProfileFile(writeEQProfileFile(t, []byte(withUnknown)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"futureFirmwareField"`) {
		t.Fatalf("unknown firmware field was discarded: %s", raw)
	}
	var olderFields map[string]json.RawMessage
	if err := json.Unmarshal(valid, &olderFields); err != nil {
		t.Fatal(err)
	}
	delete(olderFields, "soundProfile")
	delete(olderFields, "wirelessSub")
	older, err := json.Marshal(olderFields)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := readEQProfileFile(writeEQProfileFile(t, older)); err != nil {
		t.Fatalf("older firmware profile was rejected: %v", err)
	}
}

func TestEQProfileRestoreJSON(t *testing.T) {
	f, speaker := newCLIFixture(t)
	profile := sampleEQProfile()
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	path := writeEQProfileFile(t, data)
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)

	stdout, stderr, err := runCLI(t, home, config, "config", "eq_profile", path, "--json")
	result := settingsData(t, stdout, stderr, err, false)
	if result["action"] != "restore_eq_profile" || result["accepted"] != true ||
		result["source"] != "wifi" {
		t.Fatalf("restore result: %v", result)
	}
	got, ok := result["eq_profile"].(map[string]any)
	if !ok || got["profileName"] != profile.ProfileName {
		t.Fatalf("restored profile: %v", result["eq_profile"])
	}
	posts := f.postRequests()
	if len(posts) != 1 || posts[0].Path != "kef:eqProfile/v2" || posts[0].Roles != "value" {
		t.Fatalf("unexpected mutations: %+v", posts)
	}
	var value struct {
		Type    string            `json:"type"`
		Profile kefw2.EQProfileV2 `json:"kefEqProfileV2"`
	}
	if err := json.Unmarshal(*posts[0].Value, &value); err != nil {
		t.Fatal(err)
	}
	if value.Type != "kefEqProfileV2" || value.Profile != profile {
		t.Fatalf("wire profile = %+v, want %+v", value, profile)
	}
}

func TestEQProfileBackupAndRestore(t *testing.T) {
	f, speaker := newCLIFixture(t)
	profile := sampleEQProfile()
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	f.data["kef:eqProfile/v2"] = `[{"type":"kefEqProfileV2","kefEqProfileV2":` + string(data) + `}]`
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)

	stdout, stderr, err := runCLI(t, home, config, "config", "eq_profile")
	if err != nil || stderr != "" {
		t.Fatalf("backup failed: %v, stderr=%q", err, stderr)
	}
	path := writeEQProfileFile(t, []byte(stdout))
	stdout, stderr, err = runCLI(t, home, config, "config", "eq_profile", path, "--json")
	result := settingsData(t, stdout, stderr, err, false)
	got, ok := result["eq_profile"].(map[string]any)
	if !ok || got["highPassModeFreq"] != 92.5 {
		t.Fatalf("fractional frequency lost: %v", result)
	}
	posts := f.postRequests()
	if len(posts) != 1 || posts[0].Path != "kef:eqProfile/v2" {
		t.Fatalf("unexpected mutations: %+v", posts)
	}
	var value struct {
		Profile kefw2.EQProfileV2 `json:"kefEqProfileV2"`
	}
	if err := json.Unmarshal(*posts[0].Value, &value); err != nil {
		t.Fatal(err)
	}
	if value.Profile != profile {
		t.Fatalf("restored profile = %+v, want %+v", value.Profile, profile)
	}
}

func TestEQProfileRestoreInvalidFileDoesNotMutate(t *testing.T) {
	f, speaker := newCLIFixture(t)
	path := writeEQProfileFile(t, []byte(`{"profileName":"incomplete"}`))
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)

	stdout, stderr, err := runCLI(t, home, config, "config", "eq_profile", path, "--json")
	requireCLIFailure(t, stdout, stderr, err, "missing required field")
	if len(f.postRequests()) != 0 {
		t.Fatalf("invalid profile mutated speaker: %+v", f.postRequests())
	}
}

func TestEQProfileRestoreWriteFailure(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.postFail = true
	profile, err := json.Marshal(sampleEQProfile())
	if err != nil {
		t.Fatal(err)
	}
	path := writeEQProfileFile(t, profile)
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)

	stdout, stderr, err := runCLI(t, home, config, "config", "eq_profile", path, "--json")
	requireCLIFailure(t, stdout, stderr, err, "restoring EQ profile")
	if len(f.postRequests()) != 1 {
		t.Fatalf("restore did not reach speaker: %+v", f.postRequests())
	}
}
