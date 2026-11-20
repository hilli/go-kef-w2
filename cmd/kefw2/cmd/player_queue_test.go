// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/hilli/go-kef-w2/kefw2"
)

type playerQueueFakePlay struct {
	index int
	track kefw2.ContentItem
}

type playerQueueFake struct {
	mu sync.Mutex

	rows       []kefw2.ContentItem
	version    int
	mode       string
	queueReads int
	modeReads  int

	queueErrors map[int]error
	modeErrors  map[int]error
	queueHook   func(int)
	modeHook    func(int)
	queueResult func(int, *kefw2.RowsResponse) *kefw2.RowsResponse
	afterWrite  func(string)
	writeErr    error
	ignoreWrite bool

	moves   [][2]int
	plays   []playerQueueFakePlay
	removes [][]int
	clears  int
	modes   []string
}

func newPlayerQueueFake(rows ...kefw2.ContentItem) *playerQueueFake {
	return &playerQueueFake{
		rows: clonePlayerQueueItems(rows), version: 1, mode: kefw2.PlayModeNormal,
		queueErrors: make(map[int]error), modeErrors: make(map[int]error),
	}
}

func (f *playerQueueFake) GetPlayQueue() (*kefw2.RowsResponse, error) {
	f.mu.Lock()
	f.queueReads++
	n := f.queueReads
	response := &kefw2.RowsResponse{
		RowsCount: len(f.rows), RowsVersion: f.version, Rows: clonePlayerQueueItems(f.rows),
	}
	err, hook, result := f.queueErrors[n], f.queueHook, f.queueResult
	f.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	if err != nil {
		return nil, err
	}
	if result != nil {
		response = result(n, response)
	}
	return response, nil
}

func (f *playerQueueFake) PlayQueueIndex(index int, track *kefw2.ContentItem) error {
	f.mu.Lock()
	f.plays = append(f.plays, playerQueueFakePlay{index: index, track: clonePlayerQueueItems([]kefw2.ContentItem{*track})[0]})
	err, hook := f.writeErr, f.afterWrite
	f.mu.Unlock()
	if hook != nil {
		hook("play")
	}
	return err
}

func (f *playerQueueFake) MoveQueueItem(from, to int) error {
	f.mu.Lock()
	f.moves = append(f.moves, [2]int{from, to})
	if !f.ignoreWrite && from >= 0 && from < len(f.rows) && to >= 0 && to < len(f.rows) {
		row := f.rows[from]
		if from < to {
			copy(f.rows[from:to], f.rows[from+1:to+1])
		} else {
			copy(f.rows[to+1:from+1], f.rows[to:from])
		}
		f.rows[to] = row
		f.version++
	}
	err, hook := f.writeErr, f.afterWrite
	f.mu.Unlock()
	if hook != nil {
		hook("move")
	}
	return err
}

func (f *playerQueueFake) RemoveFromQueue(indices []int) error {
	f.mu.Lock()
	f.removes = append(f.removes, append([]int(nil), indices...))
	if !f.ignoreWrite && len(indices) == 1 && indices[0] >= 0 && indices[0] < len(f.rows) {
		index := indices[0]
		f.rows = append(f.rows[:index], f.rows[index+1:]...)
		f.version++
	}
	err, hook := f.writeErr, f.afterWrite
	f.mu.Unlock()
	if hook != nil {
		hook("remove")
	}
	return err
}

func (f *playerQueueFake) ClearPlaylist() error {
	f.mu.Lock()
	f.clears++
	if !f.ignoreWrite {
		f.rows = []kefw2.ContentItem{}
		f.version++
	}
	err, hook := f.writeErr, f.afterWrite
	f.mu.Unlock()
	if hook != nil {
		hook("clear")
	}
	return err
}

func (f *playerQueueFake) GetPlayMode() (string, error) {
	f.mu.Lock()
	f.modeReads++
	n := f.modeReads
	mode, err, hook := f.mode, f.modeErrors[n], f.modeHook
	f.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	return mode, err
}

func (f *playerQueueFake) SetPlayMode(mode string) error {
	f.mu.Lock()
	f.modes = append(f.modes, mode)
	if !f.ignoreWrite {
		f.mode = mode
	}
	err, hook := f.writeErr, f.afterWrite
	f.mu.Unlock()
	if hook != nil {
		hook("mode")
	}
	return err
}

func playerQueueTestItem(title, path string) kefw2.ContentItem {
	return kefw2.ContentItem{Title: title, Path: path, Type: kefw2.ContentTypeAudio}
}

func playerQueueTestBool(value bool) *bool {
	return &value
}

func playerQueueTestSetup(
	session context.Context, t *testing.T, rows ...kefw2.ContentItem,
) (*playerQueue, *playerQueueFake, *playerQueueSnapshot) {
	t.Helper()
	fake := newPlayerQueueFake(rows...)
	queue, err := newPlayerQueue(session, fake)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := queue.Read(session)
	if err != nil {
		t.Fatal(err)
	}
	return queue, fake, snapshot
}

func playerQueueTestTitles(rows []kefw2.ContentItem) []string {
	titles := make([]string, len(rows))
	for i := range rows {
		titles[i] = rows[i].Title
	}
	return titles
}

func TestPlayerQueueMoveEdgesAndDuplicateOccurrences(t *testing.T) {
	a := playerQueueTestItem("A", "same")
	b := playerQueueTestItem("B", "middle")
	c := playerQueueTestItem("C", "same")
	identical := playerQueueTestItem("Duplicate", "same")
	for _, tc := range []struct {
		name       string
		rows       []kefw2.ContentItem
		from, to   int
		wantTitles []string
	}{
		{"first to last", []kefw2.ContentItem{a, b, c}, 0, 2, []string{"B", "C", "A"}},
		{"last to first", []kefw2.ContentItem{a, b, c}, 2, 0, []string{"C", "A", "B"}},
		{"identical paths and rows", []kefw2.ContentItem{identical, b, identical}, 2, 0,
			[]string{"Duplicate", "Duplicate", "B"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			queue, fake, snapshot := playerQueueTestSetup(ctx, t, tc.rows...)
			result, err := queue.Move(ctx, snapshot, tc.from, tc.to)
			if err != nil || !result.Sent || result.Noop || result.Snapshot == nil {
				t.Fatalf("Move result=%+v err=%v", result, err)
			}
			if got := playerQueueTestTitles(result.Snapshot.items()); !reflect.DeepEqual(got, tc.wantTitles) {
				t.Fatalf("queue order = %v, want %v", got, tc.wantTitles)
			}
			if !reflect.DeepEqual(fake.moves, [][2]int{{tc.from, tc.to}}) {
				t.Fatalf("move indices = %v", fake.moves)
			}
			if len(fake.plays) != 0 {
				t.Fatalf("moving the queue started playback: %v", fake.plays)
			}
			if fake.queueReads != 3 {
				t.Fatalf("queue reads = %d, want initial, preflight and readback", fake.queueReads)
			}
		})
	}
}

func TestPlayerQueueDropTargetAndKeyboardMoveAgree(t *testing.T) {
	for _, tc := range []struct {
		from, gap, length, want int
		valid                   bool
	}{
		{0, 3, 3, 2, true}, {2, 0, 3, 0, true},
		{1, 1, 3, 1, true}, {1, 2, 3, 1, true},
		{-1, 0, 3, 0, false}, {3, 0, 3, 0, false},
		{0, -1, 3, 0, false}, {0, 4, 3, 0, false},
	} {
		got, err := playerQueueDropTarget(tc.from, tc.gap, tc.length)
		if (err == nil) != tc.valid || (tc.valid && got != tc.want) {
			t.Errorf("dropTarget(%d,%d,%d) = %d,%v, want %d valid=%v",
				tc.from, tc.gap, tc.length, got, err, tc.want, tc.valid)
		}
	}

	ctx := context.Background()
	rows := []kefw2.ContentItem{playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b")}
	keyboard, keyboardFake, keyboardSnapshot := playerQueueTestSetup(ctx, t, rows...)
	drag, dragFake, dragSnapshot := playerQueueTestSetup(ctx, t, rows...)
	target, err := playerQueueDropTarget(0, 2, len(rows))
	if err != nil {
		t.Fatal(err)
	}
	keyResult, keyErr := keyboard.Move(ctx, keyboardSnapshot, 0, 1)
	dragResult, dragErr := drag.Move(ctx, dragSnapshot, 0, target)
	if keyErr != nil || dragErr != nil || !reflect.DeepEqual(keyboardFake.moves, dragFake.moves) ||
		!reflect.DeepEqual(keyResult.Snapshot.items(), dragResult.Snapshot.items()) {
		t.Fatalf("keyboard and drag differ: %v %v, %v %v", keyboardFake.moves, dragFake.moves, keyErr, dragErr)
	}
}

func TestPlayerQueueCancelledNoopAndInvalidMoveDoNotWrite(t *testing.T) {
	ctx := context.Background()
	queue, fake, snapshot := playerQueueTestSetup(ctx, t,
		playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	result, err := queue.Move(canceled, snapshot, 0, 1)
	if !errors.Is(err, context.Canceled) || result.Sent {
		t.Fatalf("canceled move = %+v, %v", result, err)
	}
	result, err = queue.Move(ctx, snapshot, 1, 1)
	if err != nil || !result.Noop || result.Sent {
		t.Fatalf("no-op move = %+v, %v", result, err)
	}
	for _, indices := range [][2]int{{-1, 0}, {0, -1}, {2, 0}, {0, 2}} {
		result, err = queue.Move(ctx, snapshot, indices[0], indices[1])
		if err == nil || result.Sent {
			t.Errorf("invalid move %v = %+v, %v", indices, result, err)
		}
	}
	if fake.queueReads != 1 || len(fake.moves) != 0 || len(fake.plays) != 0 {
		t.Fatalf("cancel/no-op/invalid performed I/O: reads=%d moves=%v plays=%v",
			fake.queueReads, fake.moves, fake.plays)
	}
}

func TestPlayerQueueRejectsStaleWholeSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*playerQueueFake)
	}{
		{"ordered rows", func(f *playerQueueFake) { f.rows[0], f.rows[1] = f.rows[1], f.rows[0] }},
		{"metadata", func(f *playerQueueFake) { f.rows[1].MediaData.Resources[0].URI = "changed" }},
		{"version only", func(f *playerQueueFake) { f.version++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			first := playerQueueTestItem("Same", "same")
			second := playerQueueTestItem("Same", "same")
			second.MediaData = &kefw2.MediaData{Resources: []kefw2.MediaResource{{URI: "original"}}}
			queue, fake, snapshot := playerQueueTestSetup(ctx, t, first, second)
			tc.change(fake)
			result, err := queue.Move(ctx, snapshot, 1, 0)
			if !errors.Is(err, errPlayerQueueStale) || result.Sent || result.Snapshot == nil {
				t.Fatalf("stale move = %+v, %v", result, err)
			}
			if len(fake.moves) != 0 {
				t.Fatalf("stale move was sent: %v", fake.moves)
			}
		})
	}
}

func TestPlayerQueueSnapshotCopiesNestedContent(t *testing.T) {
	ctx := context.Background()
	item := playerQueueTestItem("A", "a")
	item.MediaData = &kefw2.MediaData{Resources: []kefw2.MediaResource{{URI: "original"}}}
	item.Images = &kefw2.ImageSet{Images: []kefw2.Image{{URL: "image"}}}
	item.Context = &kefw2.Context{Path: "context"}
	item.Value = &kefw2.Value{Type: "value"}
	queue, fake, snapshot := playerQueueTestSetup(ctx, t, item, playerQueueTestItem("B", "b"))
	displayed := snapshot.items()
	displayed[0].MediaData.Resources[0].URI = "changed"
	displayed[0].Images.Images[0].URL = "changed"
	displayed[0].Context.Path = "changed"
	displayed[0].Value.Type = "changed"
	result, err := queue.Move(ctx, snapshot, 0, 1)
	if err != nil || !result.Sent || len(fake.moves) != 1 {
		t.Fatalf("display copy altered the comparison snapshot: %+v, %v", result, err)
	}
}

func TestPlayerQueuePlayAndRemoveUseSelectedIndex(t *testing.T) {
	ctx := context.Background()
	duplicate := playerQueueTestItem("Same", "same")
	queue, fake, snapshot := playerQueueTestSetup(ctx, t, duplicate, playerQueueTestItem("B", "b"), duplicate)
	played, err := queue.Play(ctx, snapshot, 2)
	if err != nil || !played.Sent || played.Snapshot == nil {
		t.Fatalf("play = %+v, %v", played, err)
	}
	if len(fake.plays) != 1 || fake.plays[0].index != 2 || !reflect.DeepEqual(fake.plays[0].track, duplicate) {
		t.Fatalf("play selected the wrong occurrence: %+v", fake.plays)
	}
	removed, err := queue.Remove(ctx, played.Snapshot, 2)
	if err != nil || !removed.Sent || removed.Snapshot == nil {
		t.Fatalf("remove = %+v, %v", removed, err)
	}
	if !reflect.DeepEqual(fake.removes, [][]int{{2}}) ||
		!reflect.DeepEqual(playerQueueTestTitles(removed.Snapshot.items()), []string{"Same", "B"}) {
		t.Fatalf("remove selected the wrong occurrence: indices=%v rows=%v",
			fake.removes, playerQueueTestTitles(removed.Snapshot.items()))
	}
}

func TestPlayerQueuePlayAndRemoveRejectStaleSelection(t *testing.T) {
	for _, action := range []string{"play", "remove"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			queue, fake, snapshot := playerQueueTestSetup(ctx, t,
				playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
			fake.rows[0].Title = "changed"
			var result playerQueueResult
			var err error
			if action == "play" {
				result, err = queue.Play(ctx, snapshot, 1)
			} else {
				result, err = queue.Remove(ctx, snapshot, 1)
			}
			if !errors.Is(err, errPlayerQueueStale) || result.Sent || result.Snapshot == nil ||
				len(fake.plays) != 0 || len(fake.removes) != 0 {
				t.Fatalf("stale %s = %+v, %v; plays=%v removes=%v", action, result, err, fake.plays, fake.removes)
			}
		})
	}
}

func TestPlayerQueueClearRequiresConfirmationAndFreshSnapshot(t *testing.T) {
	ctx := context.Background()
	queue, fake, snapshot := playerQueueTestSetup(ctx, t, playerQueueTestItem("A", "a"))
	result, err := queue.Clear(ctx, snapshot, false)
	if !errors.Is(err, errPlayerQueueConfirmation) || result.Sent || fake.clears != 0 || fake.queueReads != 1 {
		t.Fatalf("unconfirmed clear = %+v, %v", result, err)
	}

	fake.rows = append(fake.rows, playerQueueTestItem("B", "b"))
	fake.version++
	result, err = queue.Clear(ctx, snapshot, true)
	if !errors.Is(err, errPlayerQueueStale) || result.Sent || result.Snapshot == nil || fake.clears != 0 {
		t.Fatalf("stale clear = %+v, %v", result, err)
	}
	result, err = queue.Clear(ctx, result.Snapshot, true)
	if err != nil || !result.Sent || result.Snapshot == nil || len(result.Snapshot.items()) != 0 || fake.clears != 1 {
		t.Fatalf("confirmed clear = %+v, %v; clears=%d", result, err, fake.clears)
	}
	result, err = queue.Clear(ctx, result.Snapshot, true)
	if err != nil || !result.Noop || result.Sent || fake.clears != 1 {
		t.Fatalf("empty clear = %+v, %v; clears=%d", result, err, fake.clears)
	}
}

func TestPlayerQueueAcceptedWriteWithFailedReadbackIsUncertain(t *testing.T) {
	ctx := context.Background()
	queue, fake, snapshot := playerQueueTestSetup(ctx, t,
		playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	readErr := errors.New("readback unavailable")
	fake.queueErrors[3] = readErr
	result, err := queue.Move(ctx, snapshot, 0, 1)
	if !result.Sent || result.Snapshot != nil || !errors.Is(err, errPlayerQueueUncertain) || !errors.Is(err, readErr) {
		t.Fatalf("accepted move/readback failure = %+v, %v", result, err)
	}
	if fake.queueReads != 3 || len(fake.moves) != 1 {
		t.Fatalf("write was retried or readback omitted: reads=%d moves=%v", fake.queueReads, fake.moves)
	}
	if _, err := queue.Move(ctx, snapshot, 0, 1); !errors.Is(err, errPlayerQueueReview) {
		t.Fatalf("unreviewed mutation error = %v", err)
	}
	if _, err := queue.ReadMode(ctx); err != nil {
		t.Fatal(err)
	}
	if err := queue.Acknowledge(ctx); !errors.Is(err, errPlayerQueueReview) {
		t.Fatalf("acknowledged without a queue readback: %v", err)
	}
	refreshed, err := queue.Read(ctx)
	if err != nil || !reflect.DeepEqual(playerQueueTestTitles(refreshed.items()), []string{"B", "A"}) {
		t.Fatalf("review read = %v, %v", refreshed, err)
	}
	if err := queue.Acknowledge(ctx); err != nil {
		t.Fatal(err)
	}
	result, err = queue.Move(ctx, refreshed, 0, 1)
	if err != nil || !result.Sent || len(fake.moves) != 2 {
		t.Fatalf("explicit new action after review = %+v, %v", result, err)
	}
}

func TestPlayerQueueWriteErrorStillReadsBackWithoutRetry(t *testing.T) {
	ctx := context.Background()
	queue, fake, snapshot := playerQueueTestSetup(ctx, t,
		playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	writeErr := errors.New("request timed out")
	fake.writeErr = writeErr
	result, err := queue.Move(ctx, snapshot, 1, 0)
	if !result.Sent || result.Snapshot == nil || !errors.Is(err, errPlayerQueueUncertain) || !errors.Is(err, writeErr) {
		t.Fatalf("ambiguous write = %+v, %v", result, err)
	}
	if !reflect.DeepEqual(playerQueueTestTitles(result.Snapshot.items()), []string{"B", "A"}) ||
		fake.queueReads != 3 || len(fake.moves) != 1 {
		t.Fatalf("unexpected readback or retry: rows=%v reads=%d moves=%v",
			playerQueueTestTitles(result.Snapshot.items()), fake.queueReads, fake.moves)
	}
	if _, err := queue.Remove(ctx, result.Snapshot, 0); !errors.Is(err, errPlayerQueueReview) {
		t.Fatalf("write after uncertain result = %v", err)
	}
	if err := queue.Acknowledge(ctx); err != nil {
		t.Fatalf("observed result could not be acknowledged: %v", err)
	}
}

func TestPlayerQueueUnexpectedReadbackIsUncertain(t *testing.T) {
	ctx := context.Background()
	queue, fake, snapshot := playerQueueTestSetup(ctx, t,
		playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	fake.ignoreWrite = true
	result, err := queue.Remove(ctx, snapshot, 1)
	if !result.Sent || result.Snapshot == nil || !errors.Is(err, errPlayerQueueUncertain) {
		t.Fatalf("mismatched readback = %+v, %v", result, err)
	}
	if len(fake.removes) != 1 || len(result.Snapshot.items()) != 2 {
		t.Fatalf("unexpected removal/readback: removes=%v rows=%v", fake.removes, result.Snapshot.items())
	}
}

func TestPlayerQueueRejectsIncompleteReads(t *testing.T) {
	ctx := context.Background()
	queue, fake, snapshot := playerQueueTestSetup(ctx, t,
		playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
	fake.queueResult = func(n int, response *kefw2.RowsResponse) *kefw2.RowsResponse {
		if n == 2 {
			response.RowsCount++
		}
		return response
	}
	result, err := queue.Move(ctx, snapshot, 0, 1)
	if err == nil || result.Sent || len(fake.moves) != 0 {
		t.Fatalf("incomplete preflight = %+v, %v", result, err)
	}
}

func TestPlayerQueueSessionCancellationAtEachStage(t *testing.T) {
	for _, stage := range []string{"preflight", "write", "readback"} {
		t.Run(stage, func(t *testing.T) {
			session, cancel := context.WithCancel(context.Background())
			defer cancel()
			queue, fake, snapshot := playerQueueTestSetup(session, t,
				playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b"))
			switch stage {
			case "preflight":
				fake.queueHook = func(n int) {
					if n == 2 {
						cancel()
					}
				}
			case "write":
				fake.afterWrite = func(string) { cancel() }
			case "readback":
				fake.queueHook = func(n int) {
					if n == 3 {
						cancel()
					}
				}
			}
			result, err := queue.Move(context.Background(), snapshot, 0, 1)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s error = %v", stage, err)
			}
			if stage == "preflight" {
				if result.Sent || len(fake.moves) != 0 || errors.Is(err, errPlayerQueueUncertain) {
					t.Fatalf("canceled preflight wrote: %+v, %v", result, err)
				}
			} else if !result.Sent || len(fake.moves) != 1 || !errors.Is(err, errPlayerQueueUncertain) {
				t.Fatalf("canceled %s must be uncertain: %+v, %v", stage, result, err)
			}
			wantReads := 2
			if stage == "readback" {
				wantReads = 3
			}
			if fake.queueReads != wantReads || len(fake.plays) != 0 {
				t.Fatalf("%s continued after cancellation: reads=%d plays=%v", stage, fake.queueReads, fake.plays)
			}
		})
	}
}

func TestPlayerQueueSnapshotCannotCrossSpeakers(t *testing.T) {
	sessionA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	rows := []kefw2.ContentItem{playerQueueTestItem("A", "same"), playerQueueTestItem("B", "same")}
	queueA, fakeA, snapshotA := playerQueueTestSetup(sessionA, t, rows...)
	queueB, fakeB, snapshotB := playerQueueTestSetup(context.Background(), t, rows...)

	result, err := queueB.Move(context.Background(), snapshotA, 0, 1)
	if !errors.Is(err, errPlayerQueueWrongSession) || result.Sent || fakeB.queueReads != 1 || len(fakeB.moves) != 0 {
		t.Fatalf("cross-session snapshot = %+v, %v", result, err)
	}
	cancelA()
	result, err = queueA.Move(context.Background(), snapshotA, 0, 1)
	if !errors.Is(err, context.Canceled) || result.Sent || len(fakeA.moves) != 0 {
		t.Fatalf("retired speaker mutation = %+v, %v", result, err)
	}
	result, err = queueB.Move(context.Background(), snapshotB, 0, 1)
	if err != nil || !result.Sent || len(fakeB.moves) != 1 || len(fakeA.moves) != 0 {
		t.Fatalf("current speaker mutation = %+v, %v", result, err)
	}
}

func TestPlayerQueueSwitchDuringInFlightOperation(t *testing.T) {
	for _, stage := range []string{"preflight", "write"} {
		t.Run(stage, func(t *testing.T) {
			sessionA, cancelA := context.WithCancel(context.Background())
			defer cancelA()
			rows := []kefw2.ContentItem{playerQueueTestItem("A", "a"), playerQueueTestItem("B", "b")}
			queueA, fakeA, snapshotA := playerQueueTestSetup(sessionA, t, rows...)
			queueB, fakeB, snapshotB := playerQueueTestSetup(context.Background(), t, rows...)
			started, release := make(chan struct{}), make(chan struct{})
			if stage == "preflight" {
				fakeA.queueHook = func(n int) {
					if n == 2 {
						close(started)
						<-release
					}
				}
			} else {
				fakeA.afterWrite = func(string) {
					close(started)
					<-release
				}
			}
			type answer struct {
				result playerQueueResult
				err    error
			}
			done := make(chan answer, 1)
			go func() {
				result, err := queueA.Move(context.Background(), snapshotA, 0, 1)
				done <- answer{result, err}
			}()
			<-started
			cancelA()
			close(release)
			old := <-done
			if !errors.Is(old.err, context.Canceled) || len(fakeB.moves) != 0 {
				t.Fatalf("retired %s crossed targets: %+v, %v; B moves=%v", stage, old.result, old.err, fakeB.moves)
			}
			if stage == "preflight" && (old.result.Sent || len(fakeA.moves) != 0) {
				t.Fatalf("retired preflight sent a write: %+v; A moves=%v", old.result, fakeA.moves)
			}
			if stage == "write" && (!old.result.Sent || len(fakeA.moves) != 1 ||
				!errors.Is(old.err, errPlayerQueueUncertain)) {
				t.Fatalf("retired write lost its uncertainty: %+v, %v", old.result, old.err)
			}
			current, err := queueB.Move(context.Background(), snapshotB, 0, 1)
			if err != nil || !current.Sent || len(fakeB.moves) != 1 {
				t.Fatalf("current target action = %+v, %v; B moves=%v", current, err, fakeB.moves)
			}
		})
	}
}

func TestPlayerQueueOnlyOneOperationInFlight(t *testing.T) {
	ctx := context.Background()
	fake := newPlayerQueueFake(playerQueueTestItem("A", "a"))
	started, release := make(chan struct{}), make(chan struct{})
	fake.queueHook = func(int) {
		close(started)
		<-release
	}
	queue, err := newPlayerQueue(ctx, fake)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := queue.Read(ctx)
		done <- err
	}()
	<-started
	result, busyErr := queue.SetShuffle(ctx, true)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(busyErr, errPlayerQueueBusy) || result.Sent || len(fake.modes) != 0 || fake.modeReads != 0 {
		t.Fatalf("concurrent action = %+v, %v", result, busyErr)
	}
}

func TestPlayerQueueShuffleAndRepeatReadBack(t *testing.T) {
	for _, tc := range []struct {
		name, before, want string
		shuffle            *bool
		repeat             string
		noop               bool
	}{
		{"enable shuffle preserving repeat one", kefw2.PlayModeRepeatOne, kefw2.PlayModeShuffleRepeatOne, playerQueueTestBool(true), "", false},
		{"disable shuffle preserving repeat all", kefw2.PlayModeShuffleRepeatAll, kefw2.PlayModeRepeatAll, playerQueueTestBool(false), "", false},
		{"repeat one preserving shuffle", kefw2.PlayModeShuffle, kefw2.PlayModeShuffleRepeatOne, nil, "one", false},
		{"repeat all preserving shuffle", kefw2.PlayModeShuffleRepeatOne, kefw2.PlayModeShuffleRepeatAll, nil, "all", false},
		{"repeat off no-op", kefw2.PlayModeNormal, kefw2.PlayModeNormal, nil, "off", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fake := newPlayerQueueFake()
			fake.mode = tc.before
			queue, err := newPlayerQueue(ctx, fake)
			if err != nil {
				t.Fatal(err)
			}
			var result playerQueueResult
			if tc.shuffle != nil {
				result, err = queue.SetShuffle(ctx, *tc.shuffle)
			} else {
				result, err = queue.SetRepeat(ctx, tc.repeat)
			}
			if err != nil || result.Mode == nil || result.Noop != tc.noop || result.Sent == tc.noop {
				t.Fatalf("mode result = %+v, %v", result, err)
			}
			if tc.noop {
				if len(fake.modes) != 0 || fake.modeReads != 1 {
					t.Fatalf("no-op wrote or reread mode: %v %d", fake.modes, fake.modeReads)
				}
			} else if !reflect.DeepEqual(fake.modes, []string{tc.want}) || fake.modeReads != 2 {
				t.Fatalf("mode writes=%v reads=%d, want %s and two reads", fake.modes, fake.modeReads, tc.want)
			}
			wire, err := result.Mode.wireMode()
			if err != nil || wire != tc.want {
				t.Fatalf("mode readback = %q, %v, want %q", wire, err, tc.want)
			}
		})
	}
}

func TestPlayerQueueInvalidAndUncertainModes(t *testing.T) {
	ctx := context.Background()
	fake := newPlayerQueueFake()
	queue, err := newPlayerQueue(ctx, fake)
	if err != nil {
		t.Fatal(err)
	}
	result, err := queue.SetRepeat(ctx, "invalid")
	if err == nil || result.Sent || fake.modeReads != 0 || len(fake.modes) != 0 {
		t.Fatalf("invalid repeat = %+v, %v", result, err)
	}
	readErr := errors.New("mode readback failed")
	fake.modeErrors[2] = readErr
	result, err = queue.SetShuffle(ctx, true)
	if !result.Sent || result.Mode != nil || !errors.Is(err, errPlayerQueueUncertain) || !errors.Is(err, readErr) {
		t.Fatalf("uncertain mode = %+v, %v", result, err)
	}
	if len(fake.modes) != 1 || fake.modeReads != 2 {
		t.Fatalf("mode was retried or not read back: %v %d", fake.modes, fake.modeReads)
	}
	if _, err := queue.Read(ctx); err != nil {
		t.Fatal(err)
	}
	if err := queue.Acknowledge(ctx); !errors.Is(err, errPlayerQueueReview) {
		t.Fatalf("acknowledged mode without a mode readback: %v", err)
	}
	mode, err := queue.ReadMode(ctx)
	if err != nil || !mode.Shuffle || mode.Repeat != "off" {
		t.Fatalf("review mode = %+v, %v", mode, err)
	}
	if err := queue.Acknowledge(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPlayerQueueModeWriteErrorStillReadsBack(t *testing.T) {
	ctx := context.Background()
	fake := newPlayerQueueFake()
	writeErr := errors.New("mode request timed out")
	fake.writeErr = writeErr
	queue, err := newPlayerQueue(ctx, fake)
	if err != nil {
		t.Fatal(err)
	}
	result, err := queue.SetRepeat(ctx, "one")
	if !result.Sent || result.Mode == nil || result.Mode.Repeat != "one" ||
		!errors.Is(err, errPlayerQueueUncertain) || !errors.Is(err, writeErr) {
		t.Fatalf("ambiguous mode write = %+v, %v", result, err)
	}
	if fake.modeReads != 2 || !reflect.DeepEqual(fake.modes, []string{kefw2.PlayModeRepeatOne}) {
		t.Fatalf("mode was retried or not read back: reads=%d writes=%v", fake.modeReads, fake.modes)
	}
	if _, err := queue.SetShuffle(ctx, true); !errors.Is(err, errPlayerQueueReview) {
		t.Fatalf("mode write before review = %v", err)
	}
	if err := queue.Acknowledge(ctx); err != nil {
		t.Fatal(err)
	}
}
