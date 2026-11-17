// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hilli/go-kef-w2/kefw2"
)

func writeUPnPCLIConfig(t *testing.T, home string, speaker *kefw2.KEFSpeaker, settings map[string]string) string {
	t.Helper()
	config := map[string]any{"upnp": settings}
	if speaker != nil {
		config["speakers"] = []kefw2.KEFSpeaker{*speaker}
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "kefw2.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeUPnPTestIndex(t *testing.T, home string) string {
	t.Helper()
	cache := filepath.Join(home, "cache")
	if runtime.GOOS == "darwin" {
		cache = filepath.Join(home, "Library", "Caches")
	}
	dir := filepath.Join(cache, "kefw2")
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "upnp_track_index.json")
	data := `{"server_path":"upnp:/server","server_name":"Library","index_version":2,` +
		`"indexed_at":"2026-11-17T00:00:00Z","track_count":1,"tracks":[` +
		`{"title":"Song","artist":"Artist","album":"Album","path":"upnp:/track","search_field":"song artist album"}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUPnPJSONReadOnlyBrowsing(t *testing.T) {
	for _, args := range [][]string{
		{"upnp", "--json"},
		{"config", "upnp", "server", "list", "--json"},
		{"upnp", "browse", "--json"},
		{"upnp", "browse", "--path", "upnp:/server", "--json"},
		{"upnp", "browse", "music", "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.rows["ui:/upnp"] = `{"rowsCount":2,"rows":[{"title":"Search","type":"query"},` +
				`{"title":"Library","type":"container","path":"upnp:/server"}]}`
			f.rows["upnp:/server"] = `{"rowsCount":1,"rows":[{"title":"Music","type":"container","path":"upnp:/music"}]}`
			f.rows["upnp:/music"] = `{"rowsCount":1,"rows":[{"title":"Song","type":"audio","path":"upnp:/track"}]}`
			home := t.TempDir()
			config := writeUPnPCLIConfig(t, home, &speaker, map[string]string{
				"default_server": "Library", "default_server_path": "upnp:/server",
			})
			stdout, stderr, err := runCLI(t, home, config, args...)
			result := decodeCLI[struct {
				Speaker *speakerIdentity `json:"speaker"`
				Data    contentPage      `json:"data"`
			}](t, stdout, stderr, err)
			if result.Speaker == nil || result.Data.Items == nil || len(result.Data.Items) != 1 {
				t.Fatalf("unexpected page: %+v", result)
			}
			if len(f.postRequests()) != 0 {
				t.Fatal("browsing mutated playback")
			}
		})
	}
}

func TestUPnPJSONEmptyAndPagedBrowse(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.rows["upnp:/empty"] = `{"rowsCount":0,"rows":null}`
	f.rows["upnp:/page"] = `{"rowsCount":10,"rows":[{"title":"Track","type":"audio","path":"upnp:/track"}]}`
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	for _, tc := range []struct {
		path string
		more bool
	}{
		{"upnp:/empty", false}, {"upnp:/page", true},
	} {
		stdout, stderr, err := runCLI(t, home, config, "upnp", "browse", "--json",
			"--path", tc.path, "--offset", "2", "--limit", "1")
		result := decodeCLI[struct {
			Data contentPage `json:"data"`
		}](t, stdout, stderr, err)
		if result.Data.Items == nil || result.Data.HasMore != tc.more || result.Data.Offset != 2 ||
			result.Data.NextOffset != 2+len(result.Data.Items) || result.Data.Limit != 1 {
			t.Fatalf("unexpected pagination: %+v", result.Data)
		}
	}
}

func TestUPnPJSONExactPlayback(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"track", []string{"--path", "upnp:/track"}, []string{
			"playlists:pl/clear", "playlists:pl/addexternalitems", "player:player/control"}},
		{"display track", []string{"Music/Song"}, []string{
			"playlists:pl/clear", "playlists:pl/addexternalitems", "player:player/control"}},
		{"container", []string{"--path", "upnp:/music"}, []string{
			"playlists:pl/clear", "playlists:pl/addexternalitems", "player:player/control"}},
		{"queue track", []string{"--path", "upnp:/track", "--queue"}, []string{"playlists:pl/addexternalitems"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.rows["upnp:/server"] = `{"rowsCount":1,"rows":[{"title":"Music","type":"container","path":"upnp:/music"}]}`
			f.rows["upnp:/music"] = `{"rowsCount":1,"rows":[{"title":"Song","type":"audio","path":"upnp:/track"}]}`
			f.rows["upnp:/track"] = `{"roles":{"title":"Song","type":"audio","path":"upnp:/track"}}`
			f.rows["playlists:pq/getitems"] = `{"rowsCount":1,"rows":[{"title":"Existing","type":"audio","path":"upnp:/existing"}]}`
			home := t.TempDir()
			config := writeUPnPCLIConfig(t, home, &speaker, map[string]string{
				"default_server": "Library", "default_server_path": "upnp:/server",
			})
			args := append([]string{"upnp", "play", "--json"}, tc.args...)
			stdout, stderr, err := runCLI(t, home, config, args...)
			result := decodeCLI[struct {
				Data struct {
					actionResult
					TrackCount int `json:"track_count"`
				} `json:"data"`
			}](t, stdout, stderr, err)
			if !result.Data.Accepted || result.Data.TrackCount != 1 {
				t.Fatalf("unexpected result: %+v", result)
			}
			posts := f.postRequests()
			if len(posts) != len(tc.want) {
				t.Fatalf("unexpected posts: %+v", posts)
			}
			for i, path := range tc.want {
				if posts[i].Path != path {
					t.Fatalf("post %d = %s, want %s", i, posts[i].Path, path)
				}
			}
			// The selected opaque path, not a title or a different result, is queued.
			if !strings.Contains(string(*posts[len(posts)-1].Value), "upnp:/track") {
				t.Fatalf("selected track missing from payload: %+v", posts)
			}
		})
	}
}

func TestUPnPJSONRefusesUnsafeSelections(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"duplicate title", []string{"upnp", "play", "Music"}, "ambiguous"},
		{"duplicate server", []string{"config", "upnp", "server", "default", "Library"}, "ambiguous"},
		{"missing title", []string{"upnp", "play", "Missing"}, "not found"},
		{"positional API", []string{"upnp", "play", "upnp:/track"}, "use --path"},
		{"mixed paths", []string{"upnp", "play", "Music", "--path", "upnp:/track"}, "cannot be combined"},
		{"extra args", []string{"upnp", "browse", "Music", "Other"}, "at most one"},
		{"browse queue", []string{"upnp", "browse", "--queue"}, "read-only"},
		{"search queue", []string{"upnp", "search", "Song", "--queue"}, "read-only"},
		{"missing play path", []string{"upnp", "play"}, "provide a display path"},
		{"invalid scheme", []string{"upnp", "play", "--path", "http://example.invalid"}, "upnp:/"},
		{"bad limit", []string{"upnp", "browse", "--limit", "0"}, "limit"},
		{"negative offset", []string{"upnp", "browse", "--offset", "-1"}, "offset"},
		{"index extra args", []string{"upnp", "index", "anything"}, "unknown command"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			duplicates := `{"rowsCount":2,"rows":[{"title":"Music","type":"container","path":"upnp:/1"},` +
				`{"title":"Music","type":"container","path":"upnp:/2"}]}`
			f.rows["upnp:/server"] = duplicates
			f.rows["ui:/upnp"] = strings.ReplaceAll(duplicates, "Music", "Library")
			home := t.TempDir()
			config := writeUPnPCLIConfig(t, home, &speaker, map[string]string{"default_server_path": "upnp:/server"})
			args := append(append([]string(nil), tc.args...), "--json")
			stdout, stderr, err := runCLI(t, home, config, args...)
			requireCLIFailure(t, stdout, stderr, err, tc.want)
			if len(f.postRequests()) != 0 {
				t.Fatal("invalid selection changed playback")
			}
		})
	}
}

func TestUPnPJSONErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		settings map[string]string
		failPath string
		postFail bool
		want     string
	}{
		{"missing config", []string{"upnp", "play", "Music"}, nil, "", false, "no default UPnP server"},
		{"read failure", []string{"upnp", "browse", "--path", "upnp:/music"}, nil, "upnp:/music", false, "HTTP 503"},
		{"write failure", []string{"upnp", "play", "--path", "upnp:/music"}, nil, "", true, "HTTP 503"},
		{"queue read failure", []string{"upnp", "play", "--path", "upnp:/music", "--queue"}, nil,
			"playlists:pq/getitems", false, "checking queue"},
		{"failed branch", []string{"upnp", "play", "--path", "upnp:/server"}, nil, "upnp:/broken", false, "failed to scan"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.rows["upnp:/music"] = `{"rowsCount":1,"rows":[{"title":"Song","type":"audio","path":"upnp:/track"}]}`
			f.rows["upnp:/server"] = `{"rowsCount":2,"rows":[{"title":"Music","type":"container","path":"upnp:/music"},` +
				`{"title":"Broken","type":"container","path":"upnp:/broken"}]}`
			f.failPath, f.postFail = tc.failPath, tc.postFail
			home := t.TempDir()
			config := writeUPnPCLIConfig(t, home, &speaker, tc.settings)
			stdout, stderr, err := runCLI(t, home, config, append(tc.args, "--json")...)
			requireCLIFailure(t, stdout, stderr, err, tc.want)
			if !tc.postFail && len(f.postRequests()) != 0 {
				t.Fatal("read failure changed playback")
			}
		})
	}
}

func TestUPnPJSONLocalIndexAndSearch(t *testing.T) {
	home := t.TempDir()
	config := writeUPnPCLIConfig(t, home, nil, nil)
	writeUPnPTestIndex(t, home)
	for _, args := range [][]string{
		{"upnp", "index", "--json"},
		{"upnp", "search", "Song", "--json"},
		{"upnp", "search", "absent", "--json"},
		{"upnp", "search", "--json"},
	} {
		stdout, stderr, err := runCLI(t, home, config, args...)
		result := decodeCLI[struct {
			Speaker *speakerIdentity `json:"speaker"`
			Data    struct {
				Items      []contentSummary `json:"items"`
				TrackCount int              `json:"track_count"`
				ServerName string           `json:"server_name"`
			} `json:"data"`
		}](t, stdout, stderr, err)
		if result.Speaker != nil || result.Data.ServerName != "Library" {
			t.Fatalf("local result unexpectedly bound to a speaker: %+v", result)
		}
		if args[1] == "search" && result.Data.Items == nil {
			t.Fatal("search items must be []")
		}
		if len(args) > 2 && args[2] == "absent" && len(result.Data.Items) != 0 {
			t.Fatal("unexpected search match")
		}
	}
}

func TestUPnPJSONMissingIndex(t *testing.T) {
	home := t.TempDir()
	config := writeUPnPCLIConfig(t, home, nil, nil)
	for _, command := range []string{"search", "index"} {
		stdout, stderr, err := runCLI(t, home, config, "upnp", command, "--json")
		requireCLIFailure(t, stdout, stderr, err, "upnp index --rebuild")
	}
	path := writeUPnPTestIndex(t, home)
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runCLI(t, home, config, "upnp", "index", "--json")
	requireCLIFailure(t, stdout, stderr, err, "loading UPnP index")
}

func TestFileOnlyJSONIgnoresInvalidConfig(t *testing.T) {
	for _, kind := range []string{"malformed", "directory"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			config := filepath.Join(home, "broken.yaml")
			if kind == "malformed" {
				if err := os.WriteFile(config, []byte("speakers: ["), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(config, 0700); err != nil {
				t.Fatal(err)
			}
			index := writeUPnPTestIndex(t, home)
			cache := filepath.Join(filepath.Dir(index), "browse_cache.json")
			if err := os.WriteFile(cache, []byte(`{}`), 0600); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{
				{"upnp", "index", "--json"},
				{"upnp", "search", "Song", "--json"},
				{"cache", "clear", "--json"},
			} {
				stdout, stderr, err := runCLI(t, home, config, args...)
				result := decodeCLI[struct {
					Speaker *speakerIdentity `json:"speaker"`
					Data    json.RawMessage  `json:"data"`
				}](t, stdout, stderr, err)
				if result.Speaker != nil || len(result.Data) == 0 {
					t.Fatalf("file-only result unexpectedly required a speaker: %+v", result)
				}
			}
			if _, err := os.Stat(cache); !os.IsNotExist(err) {
				t.Fatalf("cache clear did not remove the browse cache: %v", err)
			}
		})
	}
}

func TestUPnPJSONIndexRebuild(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.rows["upnp:/server"] = `{"rowsCount":1,"rows":[{"title":"Music","type":"container","path":"upnp:/music"}]}`
	f.rows["upnp:/music"] = `{"rowsCount":1,"rows":[{"title":"New song","type":"audio","path":"upnp:/new"}]}`
	home := t.TempDir()
	config := writeUPnPCLIConfig(t, home, &speaker, map[string]string{
		"default_server": "Library", "default_server_path": "upnp:/server",
	})
	path := writeUPnPTestIndex(t, home)
	stdout, stderr, err := runCLI(t, home, config, "upnp", "index", "--rebuild", "--container", "music", "--json")
	result := decodeCLI[struct {
		Data kefw2.TrackIndex `json:"data"`
	}](t, stdout, stderr, err)
	if result.Data.TrackCount != 1 || result.Data.Tracks[0].Title != "New song" ||
		result.Data.ServerPath != "upnp:/server" || result.Data.ContainerPath != "upnp:/music" {
		t.Fatalf("unexpected index: %+v", result)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.rows["upnp:/server"] = `{"rowsCount":2,"rows":[{"title":"Music","type":"container","path":"upnp:/music"},` +
		`{"title":"Broken","type":"container","path":"upnp:/broken"}]}`
	f.failPath = "upnp:/broken"
	f.mu.Unlock()
	stdout, stderr, err = runCLI(t, home, config, "upnp", "index", "--rebuild", "--json")
	requireCLIFailure(t, stdout, stderr, err, "index incomplete")
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || len(f.postRequests()) != 0 {
		t.Fatal("failed indexing replaced previous index or changed playback")
	}
}

func TestUPnPJSONConfig(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.rows["ui:/upnp"] = `{"rowsCount":1,"rows":[{"title":"Library","type":"container","path":"upnp:/server"}]}`
	f.rows["upnp:/server"] = `{"rowsCount":1,"rows":[{"title":"Music","type":"container","path":"upnp:/music"}]}`
	f.rows["upnp:/music"] = `{"rowsCount":0,"rows":[],"roles":{"title":"Music","type":"container","path":"upnp:/music"}}`
	home := t.TempDir()
	config := writeUPnPCLIConfig(t, home, &speaker, nil)
	for _, args := range [][]string{
		{"config", "upnp", "server", "default", "libRARY"},
		{"config", "upnp", "server", "default"},
		{"config", "upnp", "server", "default", "--path", "upnp:/server"},
		{"config", "upnp", "container", "browse", "Music"},
		{"config", "upnp", "container", "browse"},
		{"config", "upnp", "container", "index", "--path", "upnp:/music"},
		{"config", "upnp", "container", "index"},
		{"config", "upnp", "container", "browse", ""},
	} {
		stdout, stderr, err := runCLI(t, home, config, append(args, "--json")...)
		decodeCLI[commandResponse](t, stdout, stderr, err)
	}
	if len(f.postRequests()) != 0 {
		t.Fatal("config modified speaker playback")
	}
}

func TestUPnPJSONConfigGettersAndHumanDefault(t *testing.T) {
	home := t.TempDir()
	config := writeUPnPCLIConfig(t, home, nil, map[string]string{
		"default_server": "Library", "browse_container": "Music", "index_container": "Albums",
	})
	for _, args := range [][]string{
		{"config", "upnp", "server", "default"},
		{"config", "upnp", "container", "browse"},
		{"config", "upnp", "container", "index"},
	} {
		stdout, stderr, err := runCLI(t, home, config, append(args, "--json")...)
		result := decodeCLI[commandResponse](t, stdout, stderr, err)
		if result.Speaker != nil {
			t.Fatal("local config getter resolved a speaker")
		}
	}
	stdout, stderr, err := runCLI(t, home, config, "config", "upnp", "server", "default")
	if err != nil || stderr != "" || !strings.Contains(stdout, "Library") || strings.HasPrefix(stdout, "{") {
		t.Fatalf("human default regressed: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
}

func TestUPnPHumanPlaybackRemainsAvailable(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.rows["upnp:/music"] = `{"rowsCount":1,"rows":[{"title":"Song","type":"audio","path":"upnp:/track"}]}`
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	stdout, stderr, err := runCLI(t, home, config, "upnp", "play", "upnp:/music")
	if err != nil || stderr != "" || !strings.Contains(stdout, "Playback started!") || strings.HasPrefix(stdout, "{") {
		t.Fatalf("human playback changed: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if len(f.postRequests()) != 3 {
		t.Fatalf("human playback did not use existing workflow: %+v", f.postRequests())
	}
	f.mu.Lock()
	f.posts = nil
	f.mu.Unlock()
	stdout, stderr, err = runCLI(t, home, config, "upnp", "play", "--path", "upnp:/music")
	requireCLIFailure(t, stdout, stderr, err, "--path requires --json")
	if len(f.postRequests()) != 0 {
		t.Fatal("JSON-only flag was silently ignored on human path")
	}
}
