// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

type settingsEnvelope struct {
	Speaker *speakerIdentity `json:"speaker"`
	Data    json.RawMessage  `json:"data"`
}

func settingsData(t *testing.T, stdout, stderr string, err error, local bool) map[string]any {
	t.Helper()
	result := decodeCLI[settingsEnvelope](t, stdout, stderr, err)
	if (result.Speaker == nil) != local {
		t.Fatalf("unexpected speaker envelope: %+v", result.Speaker)
	}
	var data map[string]any
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSettingsJSONSpeakerReads(t *testing.T) {
	tests := []struct {
		name string
		args []string
		key  string
		want any
	}{
		{"info", []string{"info"}, "firmware_version", "1.2.3"},
		{"maxvolume", []string{"config", "maxvolume"}, "max_volume", float64(80)},
		{"eq_profile", []string{"config", "eq_profile"}, "eq_profile", map[string]any{"profileName": "Desk"}},
		{"schedule", []string{"reboot", "schedule"}, "time", "03:30"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.data["settings:/kef/host/cableMode"] = `[{"type":"kefCableMode","kefCableMode":"wireless"}]`
			f.data["kef:eqProfile/v2"] = `[{"type":"kefEqProfileV2","kefEqProfileV2":{"profileName":"Desk"}}]`
			f.data["settings:/kef/scheduledReboot/enabled"] = `[{"type":"bool_","bool_":true}]`
			f.data["settings:/kef/scheduledReboot/dayOfWeek"] = `[{"type":"i32_","i32_":2}]`
			f.data["settings:/kef/scheduledReboot/time"] = `[{"type":"string_","string_":"03:30"}]`
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			stdout, stderr, err := runCLI(t, home, config, append(tt.args, "--json")...)
			data := settingsData(t, stdout, stderr, err, false)
			if tt.name == "eq_profile" {
				profile, ok := data[tt.key].(map[string]any)
				if !ok || profile["profileName"] != "Desk" {
					t.Fatalf("EQ profile: %v", data)
				}
			} else if !reflect.DeepEqual(data[tt.key], tt.want) {
				t.Fatalf("got %v, want %v", data[tt.key], tt.want)
			}
			if len(f.postRequests()) != 0 {
				t.Fatal("read issued a mutation")
			}
		})
	}
}

func TestSettingsJSONActions(t *testing.T) {
	tests := []struct {
		args   []string
		action string
		path   string
		key    string
		want   any
	}{
		{[]string{"config", "maxvolume", "73%"}, "set_max_volume",
			"settings:/kef/host/maximumVolume", "max_volume", float64(73)},
		{[]string{"seek", "1:23"}, "seek", "player:player/control", "position_ms", float64(83000)},
		{[]string{"next"}, "next", "player:player/control", "", nil},
		{[]string{"previous"}, "previous", "player:player/control", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			stdout, stderr, err := runCLI(t, home, config, append(tt.args, "--json")...)
			data := settingsData(t, stdout, stderr, err, false)
			if data["action"] != tt.action || data["accepted"] != true {
				t.Fatalf("action result: %v", data)
			}
			if tt.key != "" && data[tt.key] != tt.want {
				t.Fatalf("action value: %v", data)
			}
			posts := f.postRequests()
			if len(posts) != 1 || posts[0].Path != tt.path {
				t.Fatalf("unexpected mutations: %+v", posts)
			}
			var value map[string]any
			if err := json.Unmarshal(*posts[0].Value, &value); err != nil {
				t.Fatal(err)
			}
			switch tt.action {
			case "set_max_volume":
				if value["i32_"] != float64(73) {
					t.Fatalf("max volume payload: %v", value)
				}
			case "seek":
				if value["control"] != "seekTime" || value["time"] != float64(83000) {
					t.Fatalf("seek payload: %v", value)
				}
			default:
				if value["control"] != tt.action {
					t.Fatalf("track payload: %v", value)
				}
			}
		})
	}
}

func TestSettingsJSONInvalidArgumentsDoNotMutate(t *testing.T) {
	tests := [][]string{
		{"config", "maxvolume", "101"}, {"config", "maxvolume", "1x"},
		{"config", "eq_profile", "ignored"}, {"seek", "-1"}, {"seek", "1:60"},
		{"seek", "9223372036854775807"}, {"seek", "181"},
		{"next", "extra"}, {"previous", "extra"}, {"reboot", "extra"},
		{"reboot", "schedule", "--day=7"}, {"reboot", "schedule", "--time=3:30"},
		{"reboot", "schedule", "--enable", "--disable"},
		{"reboot", "schedule", "--enable=false", "--disable=false"},
		{"config", "cache", "ttl-radio", "5garbage"}, {"config", "cache", "ttl-radio", "-1"},
		{"config", "cache", "ttl-upnp", "0"}, {"config", "cache", "ttl-upnp", "9223372036854775807"},
		{"config", "cache", "enable", "ignored"}, {"config", "cache", "unknown"},
		{"config", "speaker", "remove", ""}, {"config", "speaker", "default", ""},
		{"config", "speaker", "remove"}, {"config", "speaker", "add", "user:secret@example.com"},
		{"cache", "clear", "extra"}, {"cache", "status", "extra"}, {"version", "extra"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			before, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := runCLI(t, home, config, append(args, "--json")...)
			if err == nil || stdout != "" || stderr == "" {
				t.Fatalf("invalid args succeeded: err=%v stdout=%q stderr=%q", err, stdout, stderr)
			}
			if strings.Contains(stderr, "secret") {
				t.Fatalf("credentials leaked: %q", stderr)
			}
			after, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) || len(f.postRequests()) != 0 {
				t.Fatal("invalid arguments mutated config or speaker")
			}
		})
	}
}

func TestSettingsJSONReadWriteFailures(t *testing.T) {
	for _, args := range [][]string{
		{"info"}, {"config", "eq_profile"}, {"config", "maxvolume"},
		{"config", "maxvolume", "50"}, {"seek", "12"}, {"next"}, {"previous"},
		{"reboot", "schedule"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.postFail = true
			f.failPath = "settings:/kef/host/maximumVolume"
			switch args[0] {
			case "next", "previous", "seek":
				f.failPath = "settings:/kef/play/physicalSource"
			case "reboot":
				f.failPath = "settings:/kef/scheduledReboot/enabled"
			case "config":
				if args[1] == "eq_profile" {
					f.failPath = "kef:eqProfile/v2"
				}
			}
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			stdout, stderr, err := runCLI(t, home, config, append(args, "--json")...)
			if err == nil || stdout != "" || stderr == "" {
				t.Fatalf("failure swallowed: err=%v stdout=%q stderr=%q", err, stdout, stderr)
			}
		})
	}
}

func TestSettingsJSONLateReadAndActionWriteFailures(t *testing.T) {
	for _, args := range [][]string{{"info"}, {"seek", "12"}, {"next"}, {"previous"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.postFail = true
			if args[0] == "info" {
				f.data["settings:/kef/host/cableMode"] = `[{"type":"kefCableMode","kefCableMode":"wireless"}]`
				f.failPath = "player:volume"
			}
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			stdout, stderr, err := runCLI(t, home, config, append(args, "--json")...)
			if err == nil || stdout != "" || stderr == "" {
				t.Fatalf("late failure swallowed: err=%v stdout=%q stderr=%q", err, stdout, stderr)
			}
			if args[0] != "info" && len(f.postRequests()) != 1 {
				t.Fatalf("action failure test did not reach write: %v", f.postRequests())
			}
		})
	}
}

func TestSettingsJSONPlaybackPreconditions(t *testing.T) {
	for _, name := range []string{"next", "previous", "seek"} {
		t.Run(name, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.data["settings:/kef/play/physicalSource"] = `[{"type":"kefPhysicalSource","kefPhysicalSource":"optical"}]`
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			args := []string{name, "--json"}
			if name == "seek" {
				args = append(args, "12")
			}
			stdout, stderr, err := runCLI(t, home, config, args...)
			requireCLIFailure(t, stdout, stderr, err, "WiFi/Bluetooth")
			if len(f.postRequests()) != 0 {
				t.Fatal("unsupported source mutated")
			}
		})
	}
}

func TestSettingsJSONLocalConfig(t *testing.T) {
	home := t.TempDir()
	// Deliberately unreachable: local operations must never resolve/connect.
	speaker := kefw2.KEFSpeaker{Name: "Offline", Model: "LSX", IPAddress: "127.0.0.1:1"}
	config := writeCLIConfig(t, home, speaker)
	for _, args := range [][]string{
		{"version"}, {"config", "cache"}, {"config", "cache", "ttl-upnp"},
		{"config", "speaker", "default"}, {"cache", "status"},
	} {
		stdout, stderr, err := runCLI(t, home, config, append(args, "--json")...)
		settingsData(t, stdout, stderr, err, true)
	}
	assertNoCacheDirSettings(t, home)
	stdout, stderr, err := runCLI(t, home, config, "config", "cache", "ttl-radio", "612", "--json")
	data := settingsData(t, stdout, stderr, err, true)
	if data["ttl_radio"] != float64(612) || data["accepted"] != true {
		t.Fatalf("cache config result: %v", data)
	}
	stdout, stderr, err = runCLI(t, home, config, "config", "cache", "ttl-radio", "--json")
	data = settingsData(t, stdout, stderr, err, true)
	if data["ttl_radio"] != float64(612) {
		t.Fatalf("cache config did not persist: %v", data)
	}
	stdout, stderr, err = runCLI(t, home, config, "config", "cache", "disable", "--json")
	data = settingsData(t, stdout, stderr, err, true)
	if data["enabled"] != false {
		t.Fatalf("cache disable did not persist: %v", data)
	}
	stdout, stderr, err = runCLI(t, home, config, "cache", "clear", "--json")
	settingsData(t, stdout, stderr, err, true)
	assertNoCacheDirSettings(t, home)
}

func TestSettingsJSONLocalWithoutSpeaker(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(home, "missing.yaml")
	for _, args := range [][]string{
		{"version"}, {"config", "cache"}, {"config", "cache", "ttl-upnp"},
		{"cache", "status"}, {"cache", "clear"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			stdout, stderr, err := runCLI(t, home, config, append(args, "--json")...)
			settingsData(t, stdout, stderr, err, true)
		})
	}
	assertNoCacheDirSettings(t, home)
	if _, err := os.Stat(config); !os.IsNotExist(err) {
		t.Fatalf("read-only operations created config: %v", err)
	}
}

func assertNoCacheDirSettings(t *testing.T, home string) {
	t.Helper()
	// Darwin's os.UserCacheDir uses Library/Caches rather than XDG_CACHE_HOME.
	for _, path := range []string{filepath.Join(home, "cache"), filepath.Join(home, "Library", "Caches")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("local read created cache directory %q: %v", path, err)
		}
	}
}

func TestSettingsJSONSpeakerPersistenceAndAmbiguity(t *testing.T) {
	home := t.TempDir()
	one := kefw2.KEFSpeaker{Name: "Same", IPAddress: "192.0.2.1", Model: "LSX"}
	two := kefw2.KEFSpeaker{Name: "Same", IPAddress: "192.0.2.2", Model: "LS60"}
	config := writeCLIConfig(t, home, one, two)
	for _, verb := range []string{"default", "remove"} {
		stdout, stderr, err := runCLI(t, home, config, "config", "speaker", verb, "Same", "--json")
		requireCLIFailure(t, stdout, stderr, err, "ambiguous")
	}
	stdout, stderr, err := runCLI(t, home, config, "config", "speaker", "default", two.IPAddress, "--json")
	data := settingsData(t, stdout, stderr, err, true)
	if data["action"] != "set_default_speaker" {
		t.Fatalf("default result: %v", data)
	}
	stdout, stderr, err = runCLI(t, home, config, "config", "speaker", "default", "--json")
	data = settingsData(t, stdout, stderr, err, true)
	if data["default"].(map[string]any)["ip_address"] != two.IPAddress {
		t.Fatalf("default did not persist: %v", data)
	}
	stdout, stderr, err = runCLI(t, home, config, "config", "speaker", "remove", two.IPAddress, "--json")
	settingsData(t, stdout, stderr, err, true)
	stdout, stderr, err = runCLI(t, home, config, "config", "speaker", "default", "--json")
	data = settingsData(t, stdout, stderr, err, true)
	if data["default"].(map[string]any)["ip_address"] != one.IPAddress {
		t.Fatalf("remove did not repair default: %v", data)
	}
	stdout, stderr, err = runCLI(t, home, config, "config", "speaker", "remove", "Same", "--json")
	settingsData(t, stdout, stderr, err, true)
	stdout, stderr, err = runCLI(t, home, config, "config", "speaker", "list", "--json")
	data = settingsData(t, stdout, stderr, err, true)
	if len(data["items"].([]any)) != 0 {
		t.Fatalf("remove did not persist: %v", data)
	}
}

func TestSettingsJSONSpeakerAdd(t *testing.T) {
	f, speaker := newCLIFixture(t)
	home := t.TempDir()
	config := filepath.Join(home, "new", "config.yaml")
	stdout, stderr, err := runCLI(t, home, config, "config", "speaker", "add", speaker.IPAddress, "--json")
	data := settingsData(t, stdout, stderr, err, true)
	if data["action"] != "add_speaker" || data["speaker"].(map[string]any)["name"] != "Office" {
		t.Fatalf("add result: %v", data)
	}
	stdout, stderr, err = runCLI(t, home, config, "config", "speaker", "default", "--json")
	settingsData(t, stdout, stderr, err, true)
	if len(f.postRequests()) != 0 {
		t.Fatal("adding speaker mutated speaker")
	}
}

func TestSettingsJSONMalformedConfigNotOverwritten(t *testing.T) {
	for _, args := range [][]string{
		{"config", "cache", "enable"}, {"config", "speaker", "default", "x"},
		{"config", "speaker", "remove", "x"}, {"config", "speaker", "add", "127.0.0.1:1"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			home := t.TempDir()
			config := filepath.Join(home, "bad.yaml")
			original := []byte("speakers: [\n")
			if err := os.WriteFile(config, original, 0600); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := runCLI(t, home, config, append(args, "--json")...)
			requireCLIFailure(t, stdout, stderr, err, "reading configuration")
			after, err := os.ReadFile(config)
			if err != nil || !bytes.Equal(after, original) {
				t.Fatalf("malformed config overwritten: %q %v", after, err)
			}
		})
	}
}

func TestSettingsJSONInvalidCacheConfig(t *testing.T) {
	for _, field := range []string{"enabled", "ttl_radio"} {
		t.Run(field, func(t *testing.T) {
			home := t.TempDir()
			config := filepath.Join(home, "config.yaml")
			const secret = "not-a-value-or-a-token-to-print"
			before := []byte("cache:\n  " + field + ": " + secret + "\n")
			if err := os.WriteFile(config, before, 0600); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"config", "cache"}, {"config", "cache", "enable"}, {"cache", "status"}} {
				stdout, stderr, err := runCLI(t, home, config, append(args, "--json")...)
				requireCLIFailure(t, stdout, stderr, err, "invalid cache configuration")
				if strings.Contains(stderr, secret) {
					t.Fatal("invalid configuration value leaked")
				}
				after, err := os.ReadFile(config)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("invalid cache config overwritten: %q %v", after, err)
				}
			}
		})
	}
}

func TestSettingsJSONLocalWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged user for file permission errors")
	}
	for _, args := range [][]string{
		{"config", "cache", "enable"}, {"config", "speaker", "default", "Office"},
		{"config", "speaker", "remove", "Office"}, {"config", "speaker", "add", "FIXTURE"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			_, speaker := newCLIFixture(t)
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			if args[len(args)-1] == "FIXTURE" {
				config = writeCLIConfig(t, home)
				args[len(args)-1] = speaker.IPAddress
			}
			before, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(config, 0400); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := runCLI(t, home, config, append(args, "--json")...)
			requireCLIFailure(t, stdout, stderr, err, "saving configuration")
			after, err := os.ReadFile(config)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("config altered on save failure: %q %v", after, err)
			}
		})
	}
}

func TestSettingsJSONSeekOverflow(t *testing.T) {
	for _, input := range []string{
		"9223372036854775807", "153722867280912931:01", "2562047788015215:00:00",
		"9223372036854776", "153722867280912:56",
	} {
		if got, err := parseTimePosition(input); err == nil {
			t.Fatalf("overflow accepted %q: %d", input, got)
		}
	}
	got, err := parseTimePosition("9223372036854775")
	if err != nil || got != (math.MaxInt64/1000)*1000 {
		t.Fatalf("largest seconds rejected: %d %v", got, err)
	}
}

func settingsCacheDir(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, "cache", "kefw2")
	if runtime.GOOS == "darwin" {
		dir = filepath.Join(home, "Library", "Caches", "kefw2")
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSettingsJSONCacheStatusAndClear(t *testing.T) {
	home := t.TempDir()
	config := writeCLIConfig(t, home, kefw2.KEFSpeaker{Name: "Offline", IPAddress: "127.0.0.1:1"})
	dir := settingsCacheDir(t, home)
	for _, file := range []string{"browse_cache.json", "rows_cache.json"} {
		if err := os.WriteFile(filepath.Join(dir, file),
			[]byte(`{"entry":{"fetched_at":"2026-01-01T00:00:00Z"}}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, err := runCLI(t, home, config, "cache", "status", "--json")
	data := settingsData(t, stdout, stderr, err, true)
	for _, key := range []string{"browse", "rows"} {
		status := data[key].(map[string]any)
		if status["entries"] != float64(1) || status["exists"] != true ||
			status["oldest_at"] != "2026-01-01T00:00:00Z" {
			t.Fatalf("cache status: %v", data)
		}
	}
	stdout, stderr, err = runCLI(t, home, config, "cache", "clear", "--json")
	data = settingsData(t, stdout, stderr, err, true)
	if data["accepted"] != true || data["action"] != "clear_browse_cache" {
		t.Fatalf("cache clear result: %v", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "browse_cache.json")); !os.IsNotExist(err) {
		t.Fatalf("browse cache not cleared: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rows_cache.json")); err != nil {
		t.Fatalf("unrelated response cache modified: %v", err)
	}
}

func TestSettingsJSONCacheReadErrors(t *testing.T) {
	for _, file := range []string{"browse_cache.json", "rows_cache.json", "upnp_track_index.json"} {
		for _, raw := range []string{"[", "null", `{"entry":null}`} {
			// Track index has a different schema and accepts null as no index.
			if file == "upnp_track_index.json" && raw != "[" {
				continue
			}
			t.Run(file+raw, func(t *testing.T) {
				home := t.TempDir()
				config := writeCLIConfig(t, home, kefw2.KEFSpeaker{Name: "Offline", IPAddress: "127.0.0.1:1"})
				dir := settingsCacheDir(t, home)
				if err := os.WriteFile(filepath.Join(dir, file), []byte(raw), 0600); err != nil {
					t.Fatal(err)
				}
				stdout, stderr, err := runCLI(t, home, config, "cache", "status", "--json")
				if err == nil || stdout != "" || stderr == "" {
					t.Fatalf("cache read failure swallowed: err=%v stdout=%q stderr=%q", err, stdout, stderr)
				}
			})
		}
	}
}

func TestSettingsJSONCacheIOErrors(t *testing.T) {
	home := t.TempDir()
	config := writeCLIConfig(t, home, kefw2.KEFSpeaker{Name: "Offline", IPAddress: "127.0.0.1:1"})
	path := filepath.Join(settingsCacheDir(t, home), "browse_cache.json")
	if err := os.Mkdir(path, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "keep"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"status", "clear"} {
		stdout, stderr, err := runCLI(t, home, config, "cache", verb, "--json")
		if err == nil || stdout != "" || stderr == "" {
			t.Fatalf("cache IO failure swallowed: err=%v stdout=%q stderr=%q", err, stdout, stderr)
		}
	}
}

func TestSettingsJSONAuthDebugRefused(t *testing.T) {
	t.Setenv("KEFW2_AUTH_DEBUG", "true")
	// No speaker is selected. Reaching the auth operation would panic.
	old := currentSpeaker
	currentSpeaker = nil
	t.Cleanup(func() { currentSpeaker = old })
	if err := runRebootJSON(&cobra.Command{}, nil); err == nil {
		t.Fatal("auth debug should fail closed")
	}
}

func newSettingsAuthServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	var config net.ListenConfig
	listener, err := config.Listen(context.Background(), "tcp", "127.0.0.1:4430")
	if err != nil {
		t.Skipf("authenticated API test requires localhost:4430: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	if err := server.Listener.Close(); err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.StartTLS()
	t.Cleanup(server.Close)
}

func TestSettingsJSONRebootAcceptedAndRedacted(t *testing.T) {
	const secret = "private-token-must-not-be-printed"
	var fail atomic.Bool
	newSettingsAuthServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			http.Error(w, secret, http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	})
	home := t.TempDir()
	config := writeCLIConfig(t, home, kefw2.KEFSpeaker{Name: "Office", IPAddress: "127.0.0.1"})
	stdout, stderr, err := runCLI(t, home, config, "reboot", "--json")
	data := settingsData(t, stdout, stderr, err, false)
	if data["action"] != "reboot" || data["accepted"] != true {
		t.Fatalf("reboot result: %v", data)
	}
	fail.Store(true)
	stdout, stderr, err = runCLI(t, home, config, "reboot", "--json")
	requireCLIFailure(t, stdout, stderr, err, "reboot request failed")
	if strings.Contains(stdout+stderr, secret) || strings.Contains(stdout+stderr, "HMAC") {
		t.Fatal("auth information leaked")
	}
}

func TestSettingsJSONRebootRefused(t *testing.T) {
	var listenerConfig net.ListenConfig
	listener, err := listenerConfig.Listen(context.Background(), "tcp", "127.0.0.1:4430")
	if err != nil {
		t.Skipf("refused reboot test requires localhost:4430: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	config := writeCLIConfig(t, home, kefw2.KEFSpeaker{Name: "Office", IPAddress: "127.0.0.1"})
	stdout, stderr, err := runCLI(t, home, config, "reboot", "--json")
	requireCLIFailure(t, stdout, stderr, err, "reboot request failed")
}

type settingsRoundTripper func(*http.Request) (*http.Response, error)

func (f settingsRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSettingsJSONScheduleWrite(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var fail atomic.Bool
	const secret = "credentials-must-not-be-echoed"
	newSettingsAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		var payload kefw2.KEFPostRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		paths = append(paths, payload.Path)
		mu.Unlock()
		if fail.Load() && strings.HasSuffix(payload.Path, "/time") {
			http.Error(w, secret, http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	})
	previous := currentSpeaker
	t.Cleanup(func() { currentSpeaker = previous })
	currentSpeaker = &kefw2.KEFSpeaker{Name: "Office", IPAddress: "127.0.0.1"}
	kefw2.WithHTTPClient(&http.Client{Transport: settingsRoundTripper(func(r *http.Request) (*http.Response, error) {
		value := `[{"type":"bool_","bool_":false}]`
		switch r.URL.Query().Get("path") {
		case "settings:/kef/scheduledReboot/dayOfWeek":
			value = `[{"type":"i32_","i32_":1}]`
		case "settings:/kef/scheduledReboot/time":
			value = `[{"type":"string_","string_":"03:30"}]`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(value))}, nil
	})})(currentSpeaker)
	cmd := &cobra.Command{Use: "schedule"}
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.Flags().Bool("enable", false, "")
	cmd.Flags().Bool("disable", false, "")
	cmd.Flags().Int("day", 0, "")
	cmd.Flags().String("time", "", "")
	if err := cmd.ParseFlags([]string{"--enable", "--day=5", "--time=04:20"}); err != nil {
		t.Fatal(err)
	}
	if err := runRebootScheduleJSON(cmd, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"settings:/kef/scheduledReboot/dayOfWeek", "settings:/kef/scheduledReboot/time",
		"settings:/kef/scheduledReboot/enabled",
	}
	mu.Lock()
	equal := reflect.DeepEqual(paths, want)
	mu.Unlock()
	if !equal {
		t.Fatalf("schedule writes: %v", paths)
	}
	var response struct {
		Data struct {
			actionResult
			kefw2.ScheduledReboot
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Data.Accepted || !response.Data.Enabled || response.Data.DayOfWeek != 5 ||
		response.Data.Time != "04:20" {
		t.Fatalf("schedule result: %+v", response)
	}
	mu.Lock()
	paths = nil
	mu.Unlock()
	fail.Store(true)
	out.Reset()
	err := runRebootScheduleJSON(cmd, nil)
	if err == nil || strings.Contains(err.Error(), secret) || out.Len() != 0 {
		t.Fatalf("schedule failure not redacted: %v, output=%q", err, out.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(paths, want[:2]) {
		t.Fatalf("write continued after schedule failure: %v", paths)
	}
}
