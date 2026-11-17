// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const queueTestRows = `{"rowsCount":3,"rows":[` +
	`{"title":"Same","path":"playlists:item/1","type":"audio","mediaData":{"resources":[{"uri":"https://media.example/1"}]}},` +
	`{"title":"Same","path":"playlists:item/2","type":"audio","mediaData":{"resources":[{"uri":"https://media.example/2"}]}},` +
	`{"title":"Third","path":"playlists:item/3","type":"audio","mediaData":{"resources":[{"uri":"https://media.example/3"}]}}]}`

func TestAgentQueueListingAndSelection(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.rows["playlists:pq/getitems"] = queueTestRows
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	stdout, stderr, err := runCLI(t, home, config, "queue", "--json")
	result := decodeCLI[struct {
		Data struct {
			Items []queueEntry `json:"items"`
		} `json:"data"`
	}](t, stdout, stderr, err)
	if len(result.Data.Items) != 3 || result.Data.Items[1].Position != 2 ||
		result.Data.Items[1].Path != "playlists:item/2" {
		t.Fatalf("unexpected queue: %+v", result)
	}
	stdout, stderr, err = runCLI(t, home, config, "queue", "remove", "Same", "--json")
	requireCLIFailure(t, stdout, stderr, err, "ambiguous")
	if len(f.postRequests()) != 0 {
		t.Fatal("listing/ambiguous selection changed queue")
	}
	stdout, stderr, err = runCLI(t, home, config, "queue", "remove", "2", "--json")
	removed := decodeCLI[struct {
		Data struct {
			actionResult
			Position int `json:"position"`
		} `json:"data"`
	}](t, stdout, stderr, err)
	if !removed.Data.Accepted || removed.Data.Position != 2 {
		t.Fatalf("wrong selection response: %+v", removed)
	}
	posts := f.postRequests()
	var value struct {
		Items []int `json:"items"`
	}
	if len(posts) != 1 || posts[0].Path != "playlists:pl/removeitems" {
		t.Fatalf("wrong removal request: %+v", posts)
	}
	if err := json.Unmarshal(*posts[0].Value, &value); err != nil {
		t.Fatal(err)
	}
	if len(value.Items) != 1 || value.Items[0] != 1 {
		t.Fatalf("wrong index sent to speaker: %+v", value)
	}
}

func TestAgentQueueMove(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		expected int
		accepted bool
	}{
		{[]string{"3", "top"}, 1, true},
		{[]string{"1", "up"}, 1, false},
		{[]string{"1", "after", "1"}, 1, false},
		{[]string{"1", "after", "3"}, 3, true},
		{[]string{"3", "before", "1"}, 1, true},
	} {
		f, speaker := newCLIFixture(t)
		f.rows["playlists:pq/getitems"] = queueTestRows
		home := t.TempDir()
		config := writeCLIConfig(t, home, speaker)
		args := append([]string{"queue", "move", "--json"}, tc.args...)
		stdout, stderr, err := runCLI(t, home, config, args...)
		result := decodeCLI[struct {
			Data struct {
				actionResult
				To int `json:"to"`
			} `json:"data"`
		}](t, stdout, stderr, err)
		if result.Data.To != tc.expected || result.Data.Accepted != tc.accepted {
			t.Fatalf("move %v result=%+v", tc.args, result)
		}
		posts := f.postRequests()
		if !tc.accepted {
			if len(posts) != 0 {
				t.Fatal("no-op move changed queue")
			}
			continue
		}
		var value struct {
			To int `json:"to"`
		}
		if len(posts) != 1 {
			t.Fatalf("move requests=%+v", posts)
		}
		if err := json.Unmarshal(*posts[0].Value, &value); err != nil {
			t.Fatal(err)
		}
		if value.To != tc.expected-1 {
			t.Fatalf("move used wrong API index: %d", value.To)
		}
	}
}

func TestAgentQueueModes(t *testing.T) {
	f, speaker := newCLIFixture(t)
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	stdout, stderr, err := runCLI(t, home, config, "queue", "mode", "--json")
	result := decodeCLI[struct {
		Data struct {
			Shuffle bool   `json:"shuffle"`
			Repeat  string `json:"repeat"`
		} `json:"data"`
	}](t, stdout, stderr, err)
	if result.Data.Shuffle || result.Data.Repeat != "off" {
		t.Fatalf("unexpected modes: %+v", result)
	}
	f.mu.Lock()
	f.data["settings:/mediaPlayer/playMode"] = `[]`
	f.mu.Unlock()
	stdout, stderr, err = runCLI(t, home, config, "queue", "mode", "--json")
	requireCLIFailure(t, stdout, stderr, err, "no play mode")
}

func TestAgentSavedQueuesAreLocal(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(home, "broken.yaml")
	if err := os.WriteFile(config, []byte("speakers: ["), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runCLI(t, home, config, "queue", "saved", "--json")
	result := decodeCLI[struct {
		Data struct {
			Items []string `json:"items"`
		} `json:"data"`
	}](t, stdout, stderr, err)
	if result.Data.Items == nil || len(result.Data.Items) != 0 {
		t.Fatalf("unexpected saved queues: %+v", result)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 1 {
		t.Fatalf("saved queue listing created files: %v %v", entries, err)
	}
}

func TestAgentSaveAndDeleteQueue(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.rows["playlists:pq/getitems"] = queueTestRows
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	stdout, stderr, err := runCLI(t, home, config, "queue", "save", "workout", "--json")
	decodeCLI[commandResponse](t, stdout, stderr, err)
	stdout, stderr, err = runCLI(t, home, filepath.Join(home, "missing.yaml"), "queue", "saved", "--json")
	result := decodeCLI[struct {
		Data struct {
			Items []string `json:"items"`
		} `json:"data"`
	}](t, stdout, stderr, err)
	if len(result.Data.Items) != 1 || result.Data.Items[0] != "workout" {
		t.Fatalf("queue not saved: %+v", result)
	}
	stdout, stderr, err = runCLI(t, home, config, "queue", "delete-saved", "workout", "--json")
	decodeCLI[commandResponse](t, stdout, stderr, err)
	if len(f.postRequests()) != 0 {
		t.Fatal("saving/deleting queue changed speaker")
	}
	err = filepath.WalkDir(home, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(path, "workout.yaml") {
			return errors.New("saved queue still exists")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgentSavedQueueLoad(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.rows["playlists:pq/getitems"] = queueTestRows
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	stdout, stderr, err := runCLI(t, home, config, "queue", "save", "reload", "--json")
	decodeCLI[commandResponse](t, stdout, stderr, err)
	stdout, stderr, err = runCLI(t, home, config, "queue", "load", "reload", "--json")
	result := decodeCLI[struct {
		Data struct {
			actionResult
			TrackCount int `json:"track_count"`
		} `json:"data"`
	}](t, stdout, stderr, err)
	if !result.Data.Accepted || result.Data.TrackCount != 3 {
		t.Fatalf("unexpected load result: %+v", result)
	}
	posts := f.postRequests()
	if len(posts) != 3 || posts[0].Path != "playlists:pl/clear" ||
		posts[1].Path != "playlists:pl/addexternalitems" || posts[2].Path != "player:player/control" {
		t.Fatalf("queue load did not replace and start queue: %+v", posts)
	}
}

func TestAgentQueueRejectsInvalidSelection(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.rows["playlists:pq/getitems"] = queueTestRows
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	for _, args := range [][]string{
		{"remove", ""}, {"remove", "0"}, {"play", "4"},
		{"move", "1", "before"}, {"move", "1", "top", "2"},
	} {
		stdout, stderr, err := runCLI(t, home, config, append([]string{"queue", "--json"}, args...)...)
		requireCLIFailure(t, stdout, stderr, err, "Error:")
	}
	if len(f.postRequests()) != 0 {
		t.Fatal("invalid selection changed the queue")
	}
}
