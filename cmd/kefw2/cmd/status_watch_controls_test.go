// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestWatchControls(t *testing.T) {
	for _, tc := range []struct {
		key, path, value string
	}{
		{" ", "player:player/control", `"control":"pause"`},
		{"n", "player:player/control", `"control":"next"`},
		{"p", "player:player/control", `"control":"previous"`},
		{"+", "player:volume", `"i32_":26`},
		{"-", "player:volume", `"i32_":24`},
		{"m", "settings:/mediaPlayer/mute", `"bool_":true`},
		{"left", "player:player/control", `"time":2345`},
		{"right", "player:player/control", `"time":22345`},
	} {
		t.Run(tc.key, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.data["player:player/data"] = `[{"state":"playing","status":{"duration":180000},` +
				`"controls":{"pause":true,"next_":true,"previous":true}}]`
			if err := controlWatch(context.Background(), &speaker, tc.key); err != nil {
				t.Fatal(err)
			}
			posts := f.postRequests()
			if len(posts) != 1 || posts[0].Path != tc.path || !strings.Contains(string(*posts[0].Value), tc.value) {
				t.Fatalf("wrong control request: %+v; wanted %s %s", posts, tc.path, tc.value)
			}
		})
	}
}

func TestWatchControlGuardsAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name, key, path, data, want string
		fail                        bool
		noWrite                     bool
	}{
		{name: "standby", key: "+", path: "settings:/kef/play/physicalSource",
			data: `[{"type":"kefPhysicalSource","kefPhysicalSource":"standby"}]`, fail: true},
		{name: "physical playback", key: " ", path: "settings:/kef/play/physicalSource",
			data: `[{"type":"kefPhysicalSource","kefPhysicalSource":"optical"}]`, fail: true},
		{name: "stopped", key: " ", path: "player:player/data", data: `[{"state":"stopped"}]`, fail: true},
		{name: "unknown duration", key: "right", path: "player:player/data",
			data: `[{"state":"playing"}]`, fail: true},
		{name: "unknown position", key: "right", path: "player:player/data/playTime",
			data: `[{"type":"i64_","i64_":-1}]`, fail: true},
		{name: "live", key: "right", path: "player:player/data",
			data: `[{"state":"playing","status":{"duration":180000},"mediaRoles":{"mediaData":{"metaData":{"live":true}}}}]`,
			fail: true},
		{name: "unsupported next", key: "n", path: "player:player/data",
			data: `[{"state":"playing","controls":{"next_":false}}]`, fail: true},
		{name: "upper volume", key: "+", path: "player:volume",
			data: `[{"type":"i32_","i32_":80}]`, noWrite: true},
		{name: "lower volume", key: "-", path: "player:volume",
			data: `[{"type":"i32_","i32_":0}]`, noWrite: true},
		{name: "seek start", key: "left", path: "player:player/data/playTime",
			data: `[{"type":"i64_","i64_":1000}]`, want: `"time":0`},
		{name: "seek end", key: "right", path: "player:player/data/playTime",
			data: `[{"type":"i64_","i64_":179000}]`, want: `"time":180000`},
		{name: "unmute", key: "m", path: "settings:/mediaPlayer/mute",
			data: `[{"type":"bool_","bool_":true}]`, want: `"bool_":false`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.data[tc.path] = tc.data
			err := controlWatch(context.Background(), &speaker, tc.key)
			if (err != nil) != tc.fail {
				t.Fatalf("unexpected control result: %v", err)
			}
			posts := f.postRequests()
			if tc.fail || tc.noWrite {
				if len(posts) != 0 {
					t.Fatalf("unavailable control mutated speaker: %+v", posts)
				}
			} else if len(posts) != 1 || !strings.Contains(string(*posts[0].Value), tc.want) {
				t.Fatalf("incorrect bounded control: %+v", posts)
			}
		})
	}
}

func TestWatchControlFailuresAreNotRetried(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.postFail = true
	if err := controlWatch(context.Background(), &speaker, "m"); err == nil {
		t.Fatal("mutation failure was hidden")
	}
	if got := len(f.postRequests()); got != 1 {
		t.Fatalf("mutation attempted %d times", got)
	}
	f.failPath = "settings:/kef/play/physicalSource"
	if err := controlWatch(context.Background(), &speaker, "+"); err == nil {
		t.Fatal("failed fresh-state read was ignored")
	}
	if len(f.postRequests()) != 1 {
		t.Fatal("failed fresh read resulted in a write")
	}
}

func TestWatchUIControlsAndState(t *testing.T) {
	actions := make(chan string, 2)
	model := &watchModel{actions: actions, width: 80, height: 24}
	model.handleKey("m")
	if len(actions) != 0 {
		t.Fatal("key before initial status dispatched a write")
	}
	model.snapshot = &statusSnapshot{}
	model.handleKey("m")
	model.handleKey("m")
	if len(actions) != 1 || !model.pending {
		t.Fatal("pending action did not block repeated keys")
	}
	model.Update(watchUpdate{kind: "action"})
	if model.pending {
		t.Fatal("completed action remained pending")
	}
	model.Update(watchUpdate{kind: "status unavailable", err: context.DeadlineExceeded})
	if !strings.Contains(model.View().Content, "STALE") {
		t.Fatal("stale snapshot was not identified")
	}
	model.handleKey("+")
	if len(actions) != 1 {
		t.Fatal("stale UI accepted a write")
	}
	model.Update(watchUpdate{kind: "snapshot"})
	if model.readErr != nil || strings.Contains(model.View().Content, "STALE") {
		t.Fatal("successful read did not clear stale state")
	}
	for _, key := range []string{"q", "ctrl+c"} {
		command := model.handleKey(key)
		if command == nil {
			t.Fatalf("%s did not quit", key)
		}
		if _, ok := command().(tea.QuitMsg); !ok {
			t.Fatalf("%s did not produce quit", key)
		}
	}
}

func TestWatchUIV2KeyPresses(t *testing.T) {
	for _, tc := range []struct {
		key  tea.KeyPressMsg
		want string
	}{
		{tea.KeyPressMsg{Code: ' ', Text: " "}, " "},
		{tea.KeyPressMsg{Code: '+', Text: "+"}, "+"},
		{tea.KeyPressMsg{Code: tea.KeyLeft}, "left"},
		{tea.KeyPressMsg{Code: tea.KeyRight}, "right"},
	} {
		actions := make(chan string, 1)
		model := &watchModel{actions: actions, snapshot: &statusSnapshot{}}
		model.Update(tc.key)
		select {
		case got := <-actions:
			if got != tc.want {
				t.Fatalf("key %q sent %q, want %q", tc.key.String(), got, tc.want)
			}
		default:
			t.Fatalf("key %q did not send an action", tc.key.String())
		}
	}
	actions := make(chan string, 1)
	model := &watchModel{actions: actions, snapshot: &statusSnapshot{}}
	model.Update(tea.KeyReleaseMsg{Code: 'm', Text: "m"})
	if len(actions) != 0 {
		t.Fatal("key release sent an action")
	}
}

func TestWatchActionReadback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	actions := make(chan string, 1)
	var sequence []string
	err := watchStatus(ctx, watchDependencies{
		read: func(context.Context) (statusSnapshot, error) {
			sequence = append(sequence, "read")
			return statusSnapshot{}, nil
		},
		action: func(context.Context, string) error { sequence = append(sequence, "action"); return nil },
		events: func(ctx context.Context, _ chan<- struct{}, _ chan<- error) { <-ctx.Done() },
		after:  func(_ time.Duration) <-chan time.Time { return nil },
	}, actions, func(u watchUpdate) error {
		if u.kind == "snapshot" {
			if len(sequence) == 1 {
				actions <- "m"
			} else {
				cancel()
			}
		}
		return nil
	})
	got, _ := json.Marshal(sequence)
	if err != nil || string(got) != `["read","action","read"]` {
		t.Fatalf("action did not refresh observed state: %s %v", got, err)
	}
}
