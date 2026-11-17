// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentRadioSearchDoesNotPlay(t *testing.T) {
	for _, rows := range []string{
		`{"rowsCount":0,"rows":[]}`,
		`{"rowsCount":2,"rows":[{"title":"Jazz","type":"audio","path":"station:jazz","longDescription":"Jazz radio"}]}`,
	} {
		f, speaker := newCLIFixture(t)
		f.rows["search"] = rows
		home := t.TempDir()
		config := writeCLIConfig(t, home, speaker)
		stdout, stderr, err := runCLI(t, home, config, "radio", "search", "Jazz", "--json")
		result := decodeCLI[struct {
			Data struct {
				Items      []radioStation `json:"items"`
				HasMore    bool           `json:"has_more"`
				NextOffset int            `json:"next_offset"`
			} `json:"data"`
		}](t, stdout, stderr, err)
		if result.Data.Items == nil {
			t.Fatal("items must be an array, not null")
		}
		if strings.Contains(rows, "Jazz") {
			if len(result.Data.Items) != 1 || result.Data.Items[0].Path != "station:jazz" ||
				result.Data.Items[0].Description != "Jazz radio" || !result.Data.HasMore ||
				result.Data.NextOffset != 1 {
				t.Fatalf("unexpected search response: %+v", result)
			}
		} else if len(result.Data.Items) != 0 || result.Data.HasMore {
			t.Fatalf("unexpected empty search: %+v", result)
		}
		if len(f.postRequests()) != 0 {
			t.Fatal("search changed playback")
		}
	}
}

func TestAgentExactRadioPlayback(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.rows["station:jazz"] = `{"roles":{"title":"Jazz","path":"station:jazz","type":"audio",` +
		`"mediaData":{"resources":[{"uri":"https://radio.example/stream","mimeType":"audio/mpeg"}]}}}`
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	stdout, stderr, err := runCLI(t, home, config, "radio", "play", "--path", "station:jazz", "--json")
	result := decodeCLI[struct {
		Data struct {
			actionResult
			Path string `json:"path"`
		} `json:"data"`
	}](t, stdout, stderr, err)
	if !result.Data.Accepted || result.Data.Path != "station:jazz" {
		t.Fatalf("unexpected playback response: %+v", result)
	}
	posts := f.postRequests()
	if len(posts) != 2 || posts[0].Path != "playlists:pl/clear" || posts[1].Path != "player:player/control" {
		t.Fatalf("unexpected playback requests: %+v", posts)
	}
	var value struct {
		Control    string `json:"control"`
		MediaRoles struct {
			Path string `json:"path"`
		} `json:"mediaRoles"`
	}
	if err := json.Unmarshal(*posts[1].Value, &value); err != nil {
		t.Fatal(err)
	}
	if value.Control != "play" || value.MediaRoles.Path != "station:jazz" {
		t.Fatalf("played wrong station: %+v", value)
	}
}

func TestAgentRadioPreparesWiFiPlayback(t *testing.T) {
	const stationPath = "airable:https://radio.example/id/airable/radio/6"
	for _, source := range []string{"standby", "optical", "wifi"} {
		t.Run(source, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.requireWiFi = true
			f.data["settings:/kef/play/physicalSource"] =
				`[{"type":"kefPhysicalSource","kefPhysicalSource":"` + source + `"}]`
			f.rows[stationPath] = `{"roles":{"title":"Test Radio","path":"` + stationPath + `","type":"audio",` +
				`"mediaData":{"resources":[{"uri":"https://stream.example/radio","mimeType":"audio/mpeg"}]}}}`
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			stdout, stderr, err := runCLI(t, home, config, "radio", "play", "--path", stationPath, "--json")
			result := decodeCLI[struct {
				Data struct {
					actionResult
					Path string `json:"path"`
				} `json:"data"`
			}](t, stdout, stderr, err)
			if !result.Data.Accepted || result.Data.Path != stationPath {
				t.Fatalf("unexpected playback response: %+v", result)
			}
			posts := f.postRequests()
			if source != "wifi" {
				if len(posts) != 3 || posts[0].Path != "settings:/kef/play/physicalSource" {
					t.Fatalf("must select WiFi before touching the queue: %+v", posts)
				}
				posts = posts[1:]
			}
			if len(posts) != 2 || posts[0].Path != "playlists:pl/clear" || posts[1].Path != "player:player/control" {
				t.Fatalf("unexpected playback request order: %+v", posts)
			}
			var value struct {
				MediaRoles struct {
					Path      string `json:"path"`
					MediaData struct {
						Resources []struct {
							URI string `json:"uri"`
						} `json:"resources"`
					} `json:"mediaData"`
				} `json:"mediaRoles"`
			}
			if err := json.Unmarshal(*posts[1].Value, &value); err != nil {
				t.Fatal(err)
			}
			if value.MediaRoles.Path != stationPath || len(value.MediaRoles.MediaData.Resources) != 1 ||
				value.MediaRoles.MediaData.Resources[0].URI != "https://stream.example/radio" {
				t.Fatalf("opaque station path must not replace the resolved stream: %+v", value)
			}
		})
	}
}

func TestAgentRadioStopsOnSourceFailure(t *testing.T) {
	for _, stage := range []string{"read", "write"} {
		t.Run(stage, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.rows["station:jazz"] = `{"roles":{"path":"station:jazz",` +
				`"mediaData":{"resources":[{"uri":"https://stream.example/radio"}]}}}`
			if stage == "read" {
				f.failPath = "settings:/kef/play/physicalSource"
			} else {
				f.data["settings:/kef/play/physicalSource"] =
					`[{"type":"kefPhysicalSource","kefPhysicalSource":"standby"}]`
				f.postFail = true
			}
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			stdout, stderr, err := runCLI(t, home, config, "radio", "play", "--path", "station:jazz", "--json")
			requireCLIFailure(t, stdout, stderr, err, "503")
			for _, post := range f.postRequests() {
				if post.Path != "settings:/kef/play/physicalSource" {
					t.Fatalf("touched queue or playback after source failure: %+v", post)
				}
			}
		})
	}
}

func TestAgentRadioRefusesUnsafeSelections(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows string
		args []string
		want string
	}{
		{"duplicate names", `{"rowsCount":2,"rows":[{"title":"Jazz","type":"audio","path":"station:1"},` +
			`{"title":"Jazz","type":"audio","path":"station:2"}]}`, []string{"Jazz"}, "ambiguous"},
		{"partial matches", `{"rowsCount":2,"rows":[{"title":"Jazz 1","type":"audio","path":"station:1"},` +
			`{"title":"Jazz 2","type":"audio","path":"station:2"}]}`, []string{"Jazz"}, "ambiguous"},
		{"truncated search", `{"rowsCount":100,"rows":[{"title":"Jazz","type":"audio","path":"station:1"}]}`,
			[]string{"Jazz"}, "incomplete"},
		{"empty search", `{"rowsCount":0,"rows":[]}`, []string{"Jazz"}, "no playable"},
		{"ambiguous path", `{"rowsCount":2,"rows":[{"path":"station:1",` +
			`"mediaData":{"resources":[{"uri":"stream:wrong"}]}},{"path":"station:2"}]}`,
			[]string{"--path", "station:stale"}, "unambiguous"},
		{"stale path", `{"rowsCount":0,"rows":[]}`, []string{"--path", "station:stale"}, "unambiguous"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.rows["search"] = tc.rows
			f.rows["station:stale"] = tc.rows
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			args := append([]string{"radio", "play", "--json"}, tc.args...)
			stdout, stderr, err := runCLI(t, home, config, args...)
			requireCLIFailure(t, stdout, stderr, err, tc.want)
			if len(f.postRequests()) != 0 {
				t.Fatal("unsafe selection mutated playback")
			}
		})
	}
}

func TestAgentPlaybackControls(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command string
		state   string
		want    string
		posts   int
	}{
		{"already playing", "play", "playing", "alreadyPlaying", 0},
		{"resume", "play", "paused", "resumed", 1},
		{"empty queue", "play", "stopped", "nothingToPlay", 0},
		{"pause", "pause", "playing", "pause", 1},
		{"pause no-op", "pause", "paused", "pause", 0},
		{"stop", "stop", "playing", "stop", 1},
		{"off", "off", "playing", "off", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.data["player:player/data"] = `[{"state":"` + tc.state + `"}]`
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			stdout, stderr, err := runCLI(t, home, config, tc.command, "--json")
			result := decodeCLI[struct {
				Data struct {
					actionResult
					Outcome string `json:"outcome"`
				} `json:"data"`
			}](t, stdout, stderr, err)
			if result.Data.Action != tc.want && result.Data.Outcome != tc.want {
				t.Fatalf("unexpected result: %+v", result)
			}
			if result.Data.Accepted != (tc.posts > 0) || len(f.postRequests()) != tc.posts {
				t.Fatalf("result does not match mutations: result=%+v posts=%+v", result, f.postRequests())
			}
		})
	}
}

func TestAgentPlayStillWakesFromStandby(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.requireWiFi = true
	f.data["settings:/kef/play/physicalSource"] = `[{"type":"kefPhysicalSource","kefPhysicalSource":"standby"}]`
	f.data["player:player/data"] = `[{"state":"paused"}]`
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	stdout, stderr, err := runCLI(t, home, config, "play", "--json")
	result := decodeCLI[struct {
		Data struct {
			Outcome         string `json:"outcome"`
			WokeFromStandby bool   `json:"woke_from_standby"`
		} `json:"data"`
	}](t, stdout, stderr, err)
	if !result.Data.WokeFromStandby || result.Data.Outcome != "resumed" {
		t.Fatalf("wake behavior changed: %+v", result)
	}
	posts := f.postRequests()
	if len(posts) != 2 || posts[0].Path != "settings:/kef/play/physicalSource" ||
		posts[1].Path != "player:player/control" {
		t.Fatalf("unexpected resume sequence: %+v", posts)
	}
}

func TestAgentMutationFailures(t *testing.T) {
	for _, args := range [][]string{
		{"off"}, {"stop"}, {"pause"}, {"volume", "25"}, {"mute", "on"}, {"source", "optical"},
		{"radio", "play", "--path", "station:jazz"},
	} {
		t.Run(args[0], func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.postFail = true
			f.rows["station:jazz"] = `{"roles":{"path":"station:jazz",` +
				`"mediaData":{"resources":[{"uri":"https://radio.example/stream"}]}}}`
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			stdout, stderr, err := runCLI(t, home, config, append(args, "--json")...)
			requireCLIFailure(t, stdout, stderr, err, "503")
			if posts := f.postRequests(); len(posts) != 1 {
				t.Fatalf("mutation continued after failure: %+v", posts)
			}
		})
	}
}
