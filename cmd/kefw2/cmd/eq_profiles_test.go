// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func setEQProfilesConfigHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	return home
}

func sampleRawEQProfile(t *testing.T, extra string) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(sampleEQProfile())
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.TrimSuffix(data, []byte("}"))
	if extra != "" {
		data = append(data, ',')
		data = append(data, extra...)
	}
	data = append(data, '}')
	return data
}

func TestEQProfileLibraryRoundTrip(t *testing.T) {
	home := setEQProfilesConfigHome(t)
	f, speaker := newCLIFixture(t)
	raw := sampleRawEQProfile(t, `"futureFirmwareField":{"curve":[1,2,3]}`)
	f.data["kef:eqProfile/v2"] = `[{"type":"kefEqProfileV2","kefEqProfileV2":` + string(raw) + `}]`
	config := writeCLIConfig(t, home, speaker)

	stdout, stderr, err := runCLI(t, home, config, "config", "eq_profile", "save", "Night listening", "--json")
	result := settingsData(t, stdout, stderr, err, false)
	if result["action"] != "save_eq_profile" || result["name"] != "Night listening" {
		t.Fatalf("save result: %v", result)
	}
	if posts := f.postRequests(); len(posts) != 0 {
		t.Fatalf("saving profile changed speaker state: %+v", posts)
	}

	stdout, stderr, err = runCLI(t, home, config, "config", "eq_profile", "list", "--json")
	data := settingsData(t, stdout, stderr, err, true)
	items, ok := data["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("saved EQ profile list: %v", data)
	}
	item, ok := items[0].(map[string]any)
	if !ok || item["name"] != "Night listening" || item["profile_name"] != "Desk" ||
		item["source"] != "wifi" {
		t.Fatalf("saved EQ profile metadata: %v", items[0])
	}

	stdout, stderr, err = runCLI(t, home, config, "config", "eq_profile", "load", "Night listening", "--json")
	data = settingsData(t, stdout, stderr, err, false)
	if data["action"] != "load_eq_profile" || data["accepted"] != true ||
		data["name"] != "Night listening" || data["source"] != "wifi" {
		t.Fatalf("load result: %v", data)
	}
	posts := f.postRequests()
	if len(posts) != 1 || posts[0].Path != "kef:eqProfile/v2" {
		t.Fatalf("unexpected EQ writes: %+v", posts)
	}
	if !bytes.Contains(*posts[0].Value, []byte(`"futureFirmwareField"`)) {
		t.Fatalf("load dropped unknown firmware fields: %s", *posts[0].Value)
	}

	stdout, stderr, err = runCLI(t, home, config, "config", "eq_profile", "show", "Night listening", "--json")
	data = settingsData(t, stdout, stderr, err, true)
	if data["name"] != "Night listening" || data["profile_name"] != "Desk" {
		t.Fatalf("saved profile show result: %v", data)
	}
	if posts = f.postRequests(); len(posts) != 1 {
		t.Fatalf("show mutated speaker: %+v", posts)
	}
}

func TestEQProfileLibraryImportExport(t *testing.T) {
	home := setEQProfilesConfigHome(t)
	config := filepath.Join(home, "missing.yaml")
	raw := sampleRawEQProfile(t, `"firmwareExtension":17`)
	legacyFile := writeEQProfileFile(t, raw)
	stdout, stderr, err := runCLI(t, home, config, "config", "eq_profile", "import", "Imported", legacyFile, "--json")
	result := settingsData(t, stdout, stderr, err, true)
	if result["action"] != "import_eq_profile" || result["name"] != "Imported" {
		t.Fatalf("import result: %v", result)
	}

	exported := filepath.Join(t.TempDir(), "backup.json")
	stdout, stderr, err = runCLI(t, home, config, "config", "eq_profile", "export", "Imported", exported, "--json")
	result = settingsData(t, stdout, stderr, err, true)
	if result["action"] != "export_eq_profile" || result["path"] != exported {
		t.Fatalf("export result: %v", result)
	}
	_, stderr, err = runCLI(t, home, config, "config", "eq_profile", "export", "Imported", exported)
	if err == nil || !strings.Contains(stderr, "without overwrite") {
		t.Fatal("export overwrote an existing file without --force")
	}
	stdout, stderr, err = runCLI(t, home, config, "config", "eq_profile", "export", "Imported", exported, "--force")
	if err != nil || stderr != "" {
		t.Fatalf("forced export failed: %v; stderr=%q; stdout=%q", err, stderr, stdout)
	}
	data, err := os.ReadFile(exported)
	if err != nil {
		t.Fatal(err)
	}
	exportedDoc, err := decodeEQProfileDocument(data, "Imported", false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(exportedDoc.Profile, []byte(`"firmwareExtension"`)) {
		t.Fatalf("export altered raw profile: %s", exportedDoc.Profile)
	}
	info, err := os.Stat(exported)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("export permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestEQProfileLibraryReplaceImportAndDelete(t *testing.T) {
	home := setEQProfilesConfigHome(t)
	config := filepath.Join(home, "missing.yaml")
	legacyFile := writeEQProfileFile(t, sampleRawEQProfile(t, `"firmwareExtension":17`))
	if _, _, err := runCLI(t, home, config, "config", "eq_profile", "import", "Imported", legacyFile); err != nil {
		t.Fatal(err)
	}
	exported := filepath.Join(t.TempDir(), "backup.json")
	if _, _, err := runCLI(t, home, config, "config", "eq_profile", "export", "Imported", exported); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runCLI(t, home, config, "config", "eq_profile", "import", "Imported", legacyFile)
	if err == nil || !strings.Contains(stderr, "without overwrite") {
		t.Fatal("import replaced an existing profile without --force")
	}
	stdout, stderr, err := runCLI(t, home, config, "config", "eq_profile", "import", "Imported", legacyFile, "--force")
	if err != nil || stderr != "" {
		t.Fatalf("forced import failed: %v; stderr=%q; stdout=%q", err, stderr, stdout)
	}
	stdout, stderr, err = runCLI(t, home, config, "config", "eq_profile", "import", "Renamed", exported, "--json")
	result := settingsData(t, stdout, stderr, err, true)
	if result["name"] != "Renamed" {
		t.Fatalf("versioned export import result: %v", result)
	}
	stdout, stderr, err = runCLI(t, home, config, "config", "eq_profile", "delete", "Imported", "--json")
	result = settingsData(t, stdout, stderr, err, true)
	if result["action"] != "delete_eq_profile" || result["name"] != "Imported" {
		t.Fatalf("delete result: %v", result)
	}
	profiles, err := listSavedEQProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Name != "Renamed" {
		t.Fatalf("profiles after delete: %+v", profiles)
	}
}

func TestEQProfileLibraryRejectsCorruptFilesAndUnsafeNames(t *testing.T) {
	setEQProfilesConfigHome(t)
	profilesDir, err := eqProfileDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(profilesDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "broken.json"), []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := listSavedEQProfiles(); err == nil {
		t.Fatal("corrupt profile was silently hidden")
	}
	for _, name := range []string{"", "..", "../escape", `..\escape`, " name", "name ", "bad\nname", "-option"} {
		if _, err := eqProfileFilePath(name); err == nil {
			t.Errorf("unsafe profile name %q accepted", name)
		}
	}
	if _, err := readSavedEQProfile("missing"); err == nil {
		t.Fatal("missing profile succeeded")
	}
}

func TestEQProfileCompletions(t *testing.T) {
	setEQProfilesConfigHome(t)
	doc := savedEQProfile{
		Version: eqProfileDocumentVersion, Name: "Night listening",
		ProfileName: "Desk", Profile: sampleRawEQProfile(t, ""),
		CapturedAt: sampleEQProfileTime(),
	}
	if err := writeSavedEQProfile(doc, false); err != nil {
		t.Fatal(err)
	}
	completions, directive := savedEQProfileCompletion(nil, nil, "night")
	if len(completions) != 1 || completions[0] != "Night listening" ||
		directive&cobra.ShellCompDirectiveNoFileComp == 0 {
		t.Fatalf("EQ profile completion = %v, directive %v", completions, directive)
	}
}

func sampleEQProfileTime() time.Time {
	return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
}

func TestEQProfileCompletionDoesNotNeedSpeaker(t *testing.T) {
	home := setEQProfilesConfigHome(t)
	doc := savedEQProfile{
		Version: eqProfileDocumentVersion, Name: "Studio",
		ProfileName: "Desk", Profile: sampleRawEQProfile(t, ""),
		CapturedAt: sampleEQProfileTime(),
	}
	if err := writeSavedEQProfile(doc, false); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(home, "missing.yaml")
	stdout, stderr, err := runCLI(t, home, config, "__complete", "config", "eq_profile", "load", "Stu")
	if err != nil || !strings.Contains(stdout, "Studio") {
		t.Fatalf("saved EQ profile completion failed offline: %v stdout=%q stderr=%q", err, stdout, stderr)
	}
}
