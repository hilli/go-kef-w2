// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStatusUnavailablePositionIsNull(t *testing.T) {
	for _, position := range []string{"-1", "0"} {
		t.Run(position, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.mu.Lock()
			f.data["player:player/data/playTime"] = `[{"type":"i64_","i64_":` + position + `}]`
			f.mu.Unlock()
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			stdout, stderr, err := runCLI(t, home, config, "status", "--json")
			result := decodeCLI[struct {
				Data statusResponse `json:"data"`
			}](t, stdout, stderr, err)
			if (result.Data.Playback.PositionMS == nil) != (position == "-1") {
				t.Fatalf("incorrect unavailable-position handling: %+v", result.Data.Playback)
			}
		})
	}
}

func TestConfigWritersCreateParentDirectories(t *testing.T) {
	for _, args := range [][]string{
		{"config", "cache", "disable"},
		{"config", "cache", "disable", "--json"},
		{"config", "upnp", "server", "default", "Library"},
		{"config", "upnp", "server", "default", "--path", "upnp:/server", "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.mu.Lock()
			f.rows["ui:/upnp"] = `{"rowsCount":1,"rows":[{"title":"Library","type":"container","path":"upnp:/server"}]}`
			f.mu.Unlock()
			home := t.TempDir()
			config := filepath.Join(home, "fresh", "kefw2.yaml")
			stdout, stderr, err := runCLI(t, home, config, append([]string{"--speaker", speaker.IPAddress}, args...)...)
			if err != nil || strings.Contains(stdout+stderr, "Failed") {
				t.Fatalf("first config write failed: %v stdout=%s stderr=%s", err, stdout, stderr)
			}
			if _, err := os.Stat(config); err != nil {
				t.Fatalf("configuration was not saved: %v", err)
			}
		})
	}
}

func TestCompletionHonorsExplicitSpeaker(t *testing.T) {
	first, defaultSpeaker := newCLIFixture(t)
	second, target := newCLIFixture(t)
	target.Name = "Target"
	first.mu.Lock()
	first.rows["playlists:pq/getitems"] = `{"rowsCount":1,"rows":[{"title":"Default song","path":"queue:default"}]}`
	first.mu.Unlock()
	second.mu.Lock()
	second.rows["playlists:pq/getitems"] = `{"rowsCount":1,"rows":[{"title":"Target song","path":"queue:target"}]}`
	second.mu.Unlock()
	home := t.TempDir()
	config := writeCLIConfig(t, home, defaultSpeaker, target)
	for _, args := range [][]string{
		{"--speaker", target.Name, "__complete", "queue", "play", ""},
		{"__complete", "queue", "play", "--speaker", target.Name, ""},
	} {
		stdout, stderr, err := runCLI(t, home, config, args...)
		if err != nil || !strings.Contains(stdout, "Target song") || strings.Contains(stdout, "Default song") {
			t.Fatalf("completion used the wrong speaker: %v stdout=%q stderr=%q", err, stdout, stderr)
		}
	}
	stdout, stderr, err := runCLI(t, home, config, "__complete", "queue", "play", "")
	if err != nil || strings.Contains(stdout+stderr, "Error:") {
		t.Fatalf("completion without a selected speaker must remain optional: %v %q %q", err, stdout, stderr)
	}
}

func TestCompletionHonorsExplicitConfig(t *testing.T) {
	_, speaker := newCLIFixture(t)
	speaker.Name = "Selected config speaker"
	home := t.TempDir()
	selected := writeCLIConfig(t, home, speaker)
	for _, args := range [][]string{
		{"--config", selected, "__complete", "config", "speaker", "default", ""},
		{"__complete", "config", "speaker", "default", "--config", selected, ""},
	} {
		stdout, stderr, err := runCLI(t, home, filepath.Join(home, "missing.yaml"), args...)
		if err != nil || !strings.Contains(stdout, speaker.Name) {
			t.Fatalf("completion ignored selected config: %v stdout=%q stderr=%q", err, stdout, stderr)
		}
	}
}

func TestOpaqueIndexPathUsesConfiguredServer(t *testing.T) {
	for _, tc := range []struct {
		path string
		ok   bool
	}{
		{"upnp:/server-a/folder?itemType=container", true},
		{"upnp:/server-b/folder?itemType=container", false},
		{"upnp:/server-ab/folder?itemType=container", false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.mu.Lock()
			f.rows[tc.path] = `{"rowsCount":1,"rows":[{"title":"Song","type":"audio","path":"upnp:/song"}]}`
			f.mu.Unlock()
			home := t.TempDir()
			config := writeUPnPCLIConfig(t, home, &speaker, map[string]string{
				"default_server": "Library A", "default_server_path": "upnp:/server-a?itemType=server",
			})
			index := writeUPnPTestIndex(t, home)
			before, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := runCLI(t, home, config, "upnp", "index", "--rebuild", "--path", tc.path, "--json")
			if tc.ok {
				decodeCLI[commandResponse](t, stdout, stderr, err)
				return
			}
			requireCLIFailure(t, stdout, stderr, err, "configured default UPnP server")
			after, err := os.ReadFile(index)
			if err != nil || string(before) != string(after) {
				t.Fatalf("rejected path replaced previous index: %v", err)
			}
		})
	}
}

func TestHumanContainerPreservesOpaqueSelection(t *testing.T) {
	for _, setting := range []string{"browse", "index"} {
		t.Run(setting, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			path := "upnp:/server/artist/1"
			f.mu.Lock()
			f.rows[path] = `{"rowsCount":0,"rows":[],"roles":{"title":"Albums","type":"container","path":"` + path + `"}}`
			f.mu.Unlock()
			home := t.TempDir()
			config := writeUPnPCLIConfig(t, home, &speaker, map[string]string{"default_server_path": "upnp:/server"})
			stdout, stderr, err := runCLI(t, home, config, "config", "upnp", "container", setting, path)
			if err != nil {
				t.Fatalf("container setter: %v %s %s", err, stdout, stderr)
			}
			data, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			var saved struct {
				UPnP map[string]string `yaml:"upnp"`
			}
			if err := yaml.Unmarshal(data, &saved); err != nil {
				t.Fatal(err)
			}
			if saved.UPnP[setting+"_container"] != path {
				t.Fatalf("opaque selection was replaced by a leaf title: %+v", saved.UPnP)
			}
		})
	}
}

func TestShuffleRepeatOneRemainsControllable(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.mu.Lock()
	f.data["settings:/mediaPlayer/playMode"] = `[{"type":"playerPlayMode","playerPlayMode":"shuffleRepeatOne"}]`
	f.mu.Unlock()
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	stdout, stderr, err := runCLI(t, home, config, "queue", "mode", "--json")
	result := decodeCLI[struct {
		Data struct {
			Shuffle bool   `json:"shuffle"`
			Repeat  string `json:"repeat"`
		} `json:"data"`
	}](t, stdout, stderr, err)
	if !result.Data.Shuffle || result.Data.Repeat != "one" {
		t.Fatalf("lost repeat-one state: %+v", result)
	}
	for _, setting := range []string{"off", "on"} {
		stdout, stderr, err := runCLI(t, home, config, "queue", "shuffle", setting, "--json")
		decodeCLI[commandResponse](t, stdout, stderr, err)
		posts := f.postRequests()
		var value struct {
			Mode string `json:"playerPlayMode"`
		}
		if err := json.Unmarshal(*posts[len(posts)-1].Value, &value); err != nil {
			t.Fatal(err)
		}
		want := "repeatOne"
		if setting == "on" {
			want = "shuffleRepeatOne"
		}
		if value.Mode != want {
			t.Fatalf("shuffle %s lost repeat state: %s", setting, value.Mode)
		}
	}
}
