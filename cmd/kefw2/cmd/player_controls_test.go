// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hilli/go-kef-w2/kefw2"
)

func TestPlayerVolumePositionAndKeys(t *testing.T) {
	for _, tc := range []struct {
		name                string
		x, width, max, want int
		fail                bool
	}{
		{"before track", -9, 11, 80, 0, false},
		{"start", 0, 11, 80, 0, false},
		{"middle", 5, 11, 80, 40, false},
		{"rounded", 1, 3, 7, 4, false},
		{"end", 10, 11, 80, 80, false},
		{"past track", 99, 11, 80, 80, false},
		{"zero maximum", 8, 11, 0, 0, false},
		{"single cell", 99, 1, 100, 0, false},
		{"zero width", 0, 0, 100, 0, true},
		{"negative width", 0, -1, 100, 0, true},
		{"negative maximum", 0, 5, -1, 0, true},
		{"excess maximum", 0, 5, 101, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := playerVolumeAtPosition(tc.x, tc.width, tc.max)
			if (err != nil) != tc.fail || (!tc.fail && got != tc.want) {
				t.Fatalf("position = %d, %v; want %d, failure=%v", got, err, tc.want, tc.fail)
			}
		})
	}

	maximum := 80
	for _, tc := range []struct {
		name, key string
		current   int
		maximum   *int
		want      int
		fail      bool
	}{
		{"plus", "+", 25, &maximum, 26, false},
		{"equals", "=", 25, &maximum, 26, false},
		{"right", "right", 25, &maximum, 26, false},
		{"minus", "-", 25, &maximum, 24, false},
		{"left", "left", 25, &maximum, 24, false},
		{"up five", "up", 25, &maximum, 30, false},
		{"down five", "down", 25, &maximum, 20, false},
		{"home", "home", 25, &maximum, 0, false},
		{"end", "end", 25, &maximum, 80, false},
		{"upper clamp", "up", 78, &maximum, 80, false},
		{"lower clamp", "down", 2, &maximum, 0, false},
		{"unknown plus", "+", 25, nil, 25, true},
		{"unknown up", "up", 25, nil, 25, true},
		{"unknown end", "end", 25, nil, 25, true},
		{"unknown decrease", "-", 25, nil, 24, false},
		{"unknown home", "home", 25, nil, 0, false},
		{"above maximum increase", "+", 90, &maximum, 90, true},
		{"unknown key", "?", 25, &maximum, 25, true},
		{"invalid current", "+", -1, &maximum, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := playerVolumeFromKey(tc.current, tc.maximum, tc.key)
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("key = %d, %v; want %d, failure=%v", got, err, tc.want, tc.fail)
			}
		})
	}
	invalidMaximum := 101
	if _, err := playerVolumeFromKey(25, &invalidMaximum, "-"); err == nil {
		t.Fatal("invalid maximum was accepted")
	}
}

func TestPlayerControlReadsStatusAndMaximumIndependently(t *testing.T) {
	for _, tc := range []struct {
		name, maximum string
		want          int
		fail          bool
	}{
		{"zero", "0", 0, false},
		{"normal", "80", 80, false},
		{"hundred", "100", 100, false},
		{"negative", "-1", 0, true},
		{"too high", "101", 0, true},
		{"malformed", `"wrong"`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.data["settings:/kef/host/maximumVolume"] = `[{"type":"i32_","i32_":` + tc.maximum + `}]`
			controls, err := newPlayerControls(&speaker)
			if err != nil {
				t.Fatal(err)
			}
			result := controls.Read(context.Background())
			if result.StatusErr != nil || result.Snapshot.Data.Volume != 25 || result.Snapshot.Data.Source != kefw2.SourceWiFi {
				t.Fatalf("status was not available independently: %+v", result)
			}
			if (result.MaximumErr != nil) != tc.fail || (!tc.fail && result.Maximum != tc.want) {
				t.Fatalf("maximum = %d, %v; want %d, failure=%v", result.Maximum, result.MaximumErr, tc.want, tc.fail)
			}
			if len(f.postRequests()) != 0 {
				t.Fatal("reading player state mutated the speaker")
			}
		})
	}

	f, speaker := newCLIFixture(t)
	f.failPath = "settings:/kef/play/physicalSource"
	controls, err := newPlayerControls(&speaker)
	if err != nil {
		t.Fatal(err)
	}
	result := controls.Read(context.Background())
	if result.StatusErr == nil || result.MaximumErr != nil || result.Maximum != 80 {
		t.Fatalf("maximum did not survive an independent status failure: %+v", result)
	}
}

func TestPlayerVolumeLimitsAndCapturedSpeaker(t *testing.T) {
	first, firstSpeaker := newCLIFixture(t)
	second, secondSpeaker := newCLIFixture(t)
	first.data["settings:/kef/host/maximumVolume"] = `[{"type":"i32_","i32_":30}]`
	second.data["settings:/kef/host/maximumVolume"] = `[{"type":"i32_","i32_":55}]`
	firstControls, err := newPlayerControls(&firstSpeaker)
	if err != nil {
		t.Fatal(err)
	}
	secondControls, err := newPlayerControls(&secondSpeaker)
	if err != nil {
		t.Fatal(err)
	}
	firstSpeaker = secondSpeaker
	for _, tc := range []struct {
		controls *playerControls
		fixture  *cliFixture
		want     int
	}{
		{firstControls, first, 30},
		{secondControls, second, 55},
	} {
		outcome, err := tc.controls.SetVolume(context.Background(), 1000)
		if err != nil || !outcome.Attempted || !outcome.ReadbackConfirmed || outcome.Confirmed != tc.want ||
			outcome.Target != tc.want || !outcome.MaximumRead || outcome.MaximumErr != nil {
			t.Fatalf("bad clamped result: %+v, %v", outcome, err)
		}
		posts := tc.fixture.postRequests()
		if len(posts) != 1 || posts[0].Path != "player:volume" ||
			!strings.Contains(string(*posts[0].Value), fmt.Sprintf(`"i32_":%d`, tc.want)) {
			t.Fatalf("wrong captured speaker or limit: %+v", posts)
		}
	}
	first.mu.Lock()
	first.data["settings:/kef/host/maximumVolume"] = `[{"type":"i32_","i32_":0}]`
	first.mu.Unlock()
	outcome, err := firstControls.SetVolume(context.Background(), 90)
	if err != nil || outcome.Target != 0 || outcome.Confirmed != 0 {
		t.Fatalf("zero maximum was not enforced: %+v %v", outcome, err)
	}
}

func TestPlayerVolumeUnknownMaximum(t *testing.T) {
	for _, tc := range []struct {
		name, maxValue string
		failPath       bool
	}{
		{"unavailable", "", true},
		{"invalid", `[{"type":"i32_","i32_":101}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			if tc.failPath {
				f.failPath = "settings:/kef/host/maximumVolume"
			} else {
				f.data["settings:/kef/host/maximumVolume"] = tc.maxValue
			}
			controls, err := newPlayerControls(&speaker)
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := controls.SetVolume(context.Background(), 26)
			if err == nil || outcome.MaximumErr == nil || outcome.Attempted || len(f.postRequests()) != 0 {
				t.Fatalf("unknown maximum allowed an increase: %+v %v", outcome, err)
			}
			outcome, err = controls.SetVolume(context.Background(), 24)
			if err != nil || outcome.MaximumErr == nil || !outcome.ReadbackConfirmed || outcome.Confirmed != 24 {
				t.Fatalf("unknown maximum prevented a safe decrease: %+v %v", outcome, err)
			}
			if posts := f.postRequests(); len(posts) != 1 || !strings.Contains(string(*posts[0].Value), `"i32_":24`) {
				t.Fatalf("wrong decrease: %+v", posts)
			}
		})
	}
}

func TestPlayerStandbyControls(t *testing.T) {
	for _, tc := range []struct {
		name, path, value string
	}{
		{"standby input", "settings:/kef/play/physicalSource",
			`[{"type":"kefPhysicalSource","kefPhysicalSource":"standby"}]`},
		{"standby power", "settings:/kef/host/speakerStatus",
			`[{"type":"kefSpeakerStatus","kefSpeakerStatus":"standby"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.data[tc.path] = tc.value
			controls, err := newPlayerControls(&speaker)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := controls.SetVolume(context.Background(), 10); err == nil {
				t.Fatal("volume could wake standby")
			}
			for _, action := range []playerControlAction{playerControlMute, playerControlPlayPause, playerControlNext} {
				if _, err := controls.Perform(context.Background(), action); err == nil {
					t.Fatalf("%s could wake standby", action)
				}
			}
			if _, err := controls.SeekTo(context.Background(), 5000); err == nil {
				t.Fatal("seek could wake standby")
			}
			if len(f.postRequests()) != 0 {
				t.Fatal("ordinary controls wrote to standby")
			}
			result, err := controls.SetSource(context.Background(), kefw2.SourceAux)
			if err != nil || result.Data.Source != kefw2.SourceAux {
				t.Fatalf("explicit source action failed: %+v %v", result, err)
			}
			if posts := f.postRequests(); len(posts) != 1 || posts[0].Path != "settings:/kef/play/physicalSource" {
				t.Fatalf("wrong explicit source action: %+v", posts)
			}
		})
	}
}

func TestPlayerVolumeAndMuteWithoutPlaybackMetadata(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.failPath = "player:player/data"
	controls, err := newPlayerControls(&speaker)
	if err != nil {
		t.Fatal(err)
	}
	if result := controls.Read(context.Background()); result.StatusErr == nil || result.MaximumErr != nil {
		t.Fatalf("status failure and maximum were not independent: %+v", result)
	}
	muted, err := controls.ToggleMute(context.Background())
	if err != nil || !muted {
		t.Fatalf("mute unnecessarily depended on playback metadata: %v %v", muted, err)
	}
	outcome, err := controls.SetVolume(context.Background(), 24)
	if err != nil || !outcome.ReadbackConfirmed || outcome.Confirmed != 24 {
		t.Fatalf("volume unnecessarily depended on playback metadata: %+v %v", outcome, err)
	}
	posts := f.postRequests()
	if len(posts) != 2 || posts[0].Path != "settings:/mediaPlayer/mute" || posts[1].Path != "player:volume" {
		t.Fatalf("unexpected basic-control writes: %+v", posts)
	}
}

func TestPlayerVolumeRechecksDecreaseWithUnknownLimit(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.failPath = "settings:/kef/host/maximumVolume"
	var volumeReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") == "player:volume" && volumeReads.Add(1) == 2 {
			_, _ = io.WriteString(w, `[{"type":"i32_","i32_":10}]`)
			return
		}
		f.serveHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	speaker.IPAddress = strings.TrimPrefix(server.URL, "http://")
	controls, err := newPlayerControls(&speaker)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := controls.SetVolume(context.Background(), 20)
	if err == nil || outcome.Attempted || outcome.Confirmed != 10 || len(f.postRequests()) != 0 {
		t.Fatalf("changed current volume allowed an increase with no known maximum: %+v %v", outcome, err)
	}
}

func TestPlayerRechecksStandbyBeforeOrdinaryWrites(t *testing.T) {
	for _, action := range []string{"volume", "mute", "playback", "seek"} {
		t.Run(action, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.data["player:player/data"] = `[{"state":"playing","status":{"duration":180000},` +
				`"controls":{"next_":true}}]`
			var sourceReads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("path") == "settings:/kef/play/physicalSource" && sourceReads.Add(1) == 2 {
					_, _ = io.WriteString(w, `[{"type":"kefPhysicalSource","kefPhysicalSource":"standby"}]`)
					return
				}
				f.serveHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			speaker.IPAddress = strings.TrimPrefix(server.URL, "http://")
			controls, err := newPlayerControls(&speaker)
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "volume":
				_, err = controls.SetVolume(context.Background(), 20)
			case "mute":
				_, err = controls.ToggleMute(context.Background())
			case "playback":
				_, err = controls.Perform(context.Background(), playerControlNext)
			case "seek":
				_, err = controls.SeekTo(context.Background(), 5000)
			}
			if err == nil || sourceReads.Load() < 2 || len(f.postRequests()) != 0 {
				t.Fatalf("standby change was not caught before write: reads=%d err=%v posts=%+v",
					sourceReads.Load(), err, f.postRequests())
			}
		})
	}
}

func TestPlayerTransportAndReadback(t *testing.T) {
	before := int64(-100)
	after := int64(999999)
	for _, tc := range []struct {
		name, want string
		action     playerControlAction
		seek       *int64
	}{
		{"pause", `"control":"pause"`, playerControlPlayPause, nil},
		{"previous", `"control":"previous"`, playerControlPrevious, nil},
		{"next", `"control":"next"`, playerControlNext, nil},
		{"relative backward", `"time":2345`, playerControlSeekBackward, nil},
		{"relative forward", `"time":22345`, playerControlSeekForward, nil},
		{"absolute before", `"time":0`, "", &before},
		{"absolute after", `"time":180000`, "", &after},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.data["player:player/data"] = `[{"state":"playing","status":{"duration":180000},` +
				`"controls":{"pause":true,"next_":true,"previous":true}}]`
			controls, err := newPlayerControls(&speaker)
			if err != nil {
				t.Fatal(err)
			}
			var snapshot statusSnapshot
			if tc.seek == nil {
				snapshot, err = controls.Perform(context.Background(), tc.action)
			} else {
				snapshot, err = controls.SeekTo(context.Background(), *tc.seek)
			}
			if err != nil || snapshot.Data.Playback == nil || snapshot.Data.Playback.State != kefw2.PlayerStatePlaying {
				t.Fatalf("control did not read back status: %+v %v", snapshot, err)
			}
			posts := f.postRequests()
			if len(posts) != 1 || posts[0].Path != "player:player/control" ||
				!strings.Contains(string(*posts[0].Value), tc.want) {
				t.Fatalf("wrong transport request: %+v", posts)
			}
		})
	}

	f, speaker := newCLIFixture(t)
	f.data["settings:/kef/play/physicalSource"] = `[{"type":"kefPhysicalSource","kefPhysicalSource":"optical"}]`
	f.failPath = "settings:/kef/host/maximumVolume"
	controls, err := newPlayerControls(&speaker)
	if err != nil {
		t.Fatal(err)
	}
	muted, err := controls.Perform(context.Background(), playerControlMute)
	if err != nil || !muted.Data.Muted {
		t.Fatalf("mute failed without maximum: %+v %v", muted, err)
	}
	unmuted, err := controls.Perform(context.Background(), playerControlMute)
	if err != nil || unmuted.Data.Muted {
		t.Fatalf("unmute did not use fresh state: %+v %v", unmuted, err)
	}
	if posts := f.postRequests(); len(posts) != 2 || !strings.Contains(string(*posts[1].Value), `"bool_":false`) {
		t.Fatalf("wrong mute requests: %+v", posts)
	}
}

func TestPlayerUnavailableControlsNeverWrite(t *testing.T) {
	for _, tc := range []struct {
		name, path, value string
		action            playerControlAction
		absolute          bool
	}{
		{"external audio", "settings:/kef/play/physicalSource",
			`[{"type":"kefPhysicalSource","kefPhysicalSource":"optical"}]`, playerControlPlayPause, false},
		{"stopped", "player:player/data", `[{"state":"stopped"}]`, playerControlNext, false},
		{"unsupported next", "player:player/data",
			`[{"state":"playing","controls":{"next_":false}}]`, playerControlNext, false},
		{"unknown position", "player:player/data/playTime",
			`[{"type":"i64_","i64_":-1}]`, playerControlSeekForward, false},
		{"unknown duration", "player:player/data", `[{"state":"playing"}]`, playerControlSeekForward, true},
		{"live radio", "player:player/data",
			`[{"state":"playing","status":{"duration":180000},"mediaRoles":{"mediaData":{"metaData":{"live":true}}}}]`,
			playerControlSeekForward, true},
		{"paused seek", "player:player/data",
			`[{"state":"paused","status":{"duration":180000}}]`, playerControlSeekBackward, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.data[tc.path] = tc.value
			controls, err := newPlayerControls(&speaker)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := playerReadStatus(context.Background(), &speaker)
			if err != nil || playerControlUnavailable(snapshot, tc.action) == nil {
				t.Fatalf("disabled control was presented as available: %v", err)
			}
			if tc.absolute {
				_, err = controls.SeekTo(context.Background(), 5000)
			} else {
				_, err = controls.Perform(context.Background(), tc.action)
			}
			if err == nil || len(f.postRequests()) != 0 {
				t.Fatalf("unsupported control was sent: %v %+v", err, f.postRequests())
			}
		})
	}
}

func TestPlayerVolumeReadbackFailure(t *testing.T) {
	for _, tc := range []struct {
		name             string
		readback         string
		wantConfirmed    int
		readbackComplete bool
		uncertain        bool
	}{
		{"unavailable", "", 25, false, true},
		{"different confirmed value", `[{"type":"i32_","i32_":22}]`, 22, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			var posted atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if posted.Load() && r.URL.Query().Get("path") == "player:volume" {
					if tc.readback == "" {
						http.Error(w, "readback unavailable", http.StatusServiceUnavailable)
					} else {
						_, _ = io.WriteString(w, tc.readback)
					}
					return
				}
				f.serveHTTP(w, r)
				if r.Method == http.MethodPost {
					posted.Store(true)
				}
			}))
			t.Cleanup(server.Close)
			speaker.IPAddress = strings.TrimPrefix(server.URL, "http://")
			controls, err := newPlayerControls(&speaker)
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := controls.SetVolume(context.Background(), 30)
			if err == nil || !outcome.Attempted || outcome.Confirmed != tc.wantConfirmed ||
				outcome.ReadbackConfirmed != tc.readbackComplete || outcome.Uncertain != tc.uncertain {
				t.Fatalf("readback failure was hidden: %+v %v", outcome, err)
			}
			var uncertain *playerMutationUncertainError
			if errors.As(err, &uncertain) != tc.uncertain {
				t.Fatalf("incorrect uncertain outcome type: %T %v", err, err)
			}
			if uncertain != nil && (!strings.Contains(uncertain.Target, speaker.Name) ||
				!strings.Contains(uncertain.Target, speaker.IPAddress)) {
				t.Fatalf("uncertain result lost its target: %+v", uncertain)
			}
			if len(f.postRequests()) != 1 {
				t.Fatalf("failed write was replayed: %+v", f.postRequests())
			}
		})
	}
}

func TestPlayerSourceAndPreflightFailure(t *testing.T) {
	f, speaker := newCLIFixture(t)
	controls, err := newPlayerControls(&speaker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controls.SetSource(context.Background(), kefw2.Source("not-an-input")); err == nil {
		t.Fatal("invalid source accepted")
	}
	f.failPath = "settings:/kef/play/physicalSource"
	if _, err := controls.Perform(context.Background(), playerControlMute); err == nil || len(f.postRequests()) != 0 {
		t.Fatalf("failed preflight resulted in a write: %v", err)
	}
	f.mu.Lock()
	f.failPath = ""
	f.postFail = true
	f.mu.Unlock()
	_, err = controls.SetSource(context.Background(), kefw2.SourceWiFi)
	var uncertain *playerMutationUncertainError
	if !errors.As(err, &uncertain) || uncertain.Action != "switch to wifi" || uncertain.Target != controls.target() {
		t.Fatalf("failed source write lost its target or uncertainty: %v", err)
	}
}

func TestPlayerControlWaitingForMutationCanBeCanceled(t *testing.T) {
	f, speaker := newCLIFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce, releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			startedOnce.Do(func() { close(started) })
			<-release
		}
		f.serveHTTP(w, r)
	}))
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		server.Close()
	})
	speaker.IPAddress = strings.TrimPrefix(server.URL, "http://")
	controls, err := newPlayerControls(&speaker)
	if err != nil {
		t.Fatal(err)
	}
	written := make(chan error, 1)
	go func() {
		_, err := controls.SetVolume(context.Background(), 30)
		written <- err
	}()
	receivePlayer(t, started)
	ctx, cancel := context.WithCancel(context.Background())
	muted := make(chan error, 1)
	go func() {
		_, err := controls.ToggleMute(ctx)
		muted <- err
	}()
	cancel()
	if err := receivePlayer(t, muted); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting mutation did not cancel: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	if err := receivePlayer(t, written); err != nil {
		t.Fatal(err)
	}
	if posts := f.postRequests(); len(posts) != 1 || posts[0].Path != "player:volume" {
		t.Fatalf("canceled waiting mutation wrote to speaker: %+v", posts)
	}
}

type playerTestTimer struct {
	deadline time.Time
	ch       chan time.Time
}

type playerTestClock struct {
	mu        sync.Mutex
	current   time.Time
	timers    []playerTestTimer
	scheduled chan time.Duration
}

func newPlayerTestClock() *playerTestClock {
	return &playerTestClock{current: time.Unix(100, 0), scheduled: make(chan time.Duration, 32)}
}

func (c *playerTestClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *playerTestClock) after(delay time.Duration) <-chan time.Time {
	c.mu.Lock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, playerTestTimer{deadline: c.current.Add(delay), ch: ch})
	c.mu.Unlock()
	c.scheduled <- delay
	return ch
}

func (c *playerTestClock) advance(delay time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(delay)
	remaining := c.timers[:0]
	for _, timer := range c.timers {
		if timer.deadline.After(c.current) {
			remaining = append(remaining, timer)
		} else {
			timer.ch <- c.current
		}
	}
	c.timers = remaining
}

type playerTestReply struct {
	confirmed int
	err       error
	uncertain bool
}

type playerTestWrite struct {
	target int
	reply  chan playerTestReply
}

func playerTestWriter(calls chan<- playerTestWrite, active, mostActive *atomic.Int32) playerVolumeWriter {
	return func(ctx context.Context, target int) (playerVolumeOutcome, error) {
		count := active.Add(1)
		defer active.Add(-1)
		for {
			previous := mostActive.Load()
			if count <= previous || mostActive.CompareAndSwap(previous, count) {
				break
			}
		}
		call := playerTestWrite{target: target, reply: make(chan playerTestReply, 1)}
		select {
		case calls <- call:
		case <-ctx.Done():
			return playerVolumeOutcome{}, ctx.Err()
		}
		select {
		case reply := <-call.reply:
			return playerVolumeOutcome{
				Target: target, Confirmed: reply.confirmed, ConfirmedKnown: true,
				ReadbackConfirmed: reply.err == nil, Attempted: true, Uncertain: reply.uncertain,
			}, reply.err
		case <-ctx.Done():
			return playerVolumeOutcome{Attempted: true, Uncertain: true}, ctx.Err()
		}
	}
}

func receivePlayer[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("player operation timed out")
		var zero T
		return zero
	}
}

func assertNoPlayerWrite(t *testing.T, calls <-chan playerTestWrite, message string) {
	t.Helper()
	select {
	case call := <-calls:
		t.Fatalf("%s: %+v", message, call)
	default:
	}
}

func waitPlayerVolume(t *testing.T, c *playerVolumeCoalescer, predicate func(playerVolumeState) bool) playerVolumeState {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		state := c.State()
		if predicate(state) {
			return state
		}
		select {
		case <-c.Changes():
		case <-timeout.C:
			t.Fatalf("volume state did not reach expected condition: %+v", c.State())
		}
	}
}

func newTestPlayerVolume(
	ctx context.Context, t *testing.T, clock *playerTestClock, writer playerVolumeWriter,
) *playerVolumeCoalescer {
	t.Helper()
	var c *playerVolumeCoalescer
	var err error
	if clock == nil {
		c, err = newPlayerVolumeCoalescer(ctx, 25, writer)
	} else {
		c, err = newPlayerVolumeCoalescerWithClock(ctx, 25, writer, playerVolumeClock{
			now: clock.now, after: clock.after,
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c.Stop()
		receivePlayer(t, c.Done())
	})
	return c
}

func TestPlayerVolumeCoalescesAndFlushesFinalTarget(t *testing.T) {
	clock := newPlayerTestClock()
	calls := make(chan playerTestWrite, 8)
	var active, mostActive atomic.Int32
	c := newTestPlayerVolume(context.Background(), t, clock, playerTestWriter(calls, &active, &mostActive))
	if err := c.SetTarget(10); err != nil {
		t.Fatal(err)
	}
	first := receivePlayer(t, calls)
	if first.target != 10 {
		t.Fatalf("first target = %d", first.target)
	}
	for _, target := range []int{20, 30, 40} {
		if err := c.SetTarget(target); err != nil {
			t.Fatal(err)
		}
	}
	assertNoPlayerWrite(t, calls, "second write overlapped first")
	clock.advance(350 * time.Millisecond)
	first.reply <- playerTestReply{confirmed: 10}
	if delay := receivePlayer(t, clock.scheduled); delay != playerVolumeWriteInterval {
		t.Fatalf("rate limit = %v", delay)
	}
	if state := c.State(); !state.Pending || state.InFlight || state.Target != 40 || state.Confirmed != 10 {
		t.Fatalf("earlier readback overwrote later pending intent: %+v", state)
	}
	if err := c.SetTarget(60); err != nil {
		t.Fatal(err)
	}
	if err := c.Release(70); err != nil {
		t.Fatal(err)
	}
	clock.advance(99 * time.Millisecond)
	assertNoPlayerWrite(t, calls, "write started before 100ms")
	clock.advance(time.Millisecond)
	final := receivePlayer(t, calls)
	if final.target != 70 {
		t.Fatalf("final target = %d, want latest release 70", final.target)
	}
	final.reply <- playerTestReply{confirmed: 70}
	state := waitPlayerVolume(t, c, func(s playerVolumeState) bool { return !s.Pending && s.Confirmed == 70 })
	if state.Err != nil || state.Target != 70 || mostActive.Load() != 1 {
		t.Fatalf("unexpected final state or overlapping writes: %+v, max active %d", state, mostActive.Load())
	}
	if err := c.Release(70); err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Second)
	assertNoPlayerWrite(t, calls, "release replayed an already confirmed target")
}

func assertPlayerVolumeFailure(t *testing.T, c *playerVolumeCoalescer) {
	t.Helper()
	state := waitPlayerVolume(t, c, func(s playerVolumeState) bool { return s.Err != nil && !s.Pending })
	if state.Confirmed != 25 || state.Target != 25 || !state.Uncertain || !state.CanRetry || state.RetryTarget != 60 {
		t.Fatalf("failure did not revert or preserve latest retry target: %+v", state)
	}
	if accepted, err := c.ObserveConfirmed(22, state.Revision); err != nil || !accepted {
		t.Fatalf("fresh observation rejected after failure: %v %v", accepted, err)
	}
	if state = c.State(); state.Confirmed != 22 || state.Target != 22 || state.Err == nil || state.RetryTarget != 60 {
		t.Fatalf("fresh observation cleared the failure or intent: %+v", state)
	}
}

func TestPlayerVolumeFailureRequiresExplicitRetry(t *testing.T) {
	clock := newPlayerTestClock()
	calls := make(chan playerTestWrite, 8)
	var active, mostActive atomic.Int32
	c := newTestPlayerVolume(context.Background(), t, clock, playerTestWriter(calls, &active, &mostActive))
	if err := c.SetTarget(40); err != nil {
		t.Fatal(err)
	}
	first := receivePlayer(t, calls)
	if err := c.Release(60); err != nil {
		t.Fatal(err)
	}
	first.reply <- playerTestReply{confirmed: 25, err: errors.New("write failed"), uncertain: true}
	assertPlayerVolumeFailure(t, c)
	if err := c.Release(60); err == nil {
		t.Fatal("release silently replayed failed gesture")
	}
	if err := c.SetTarget(70); err == nil {
		t.Fatal("new target silently dismissed the failure")
	}
	clock.advance(time.Second)
	assertNoPlayerWrite(t, calls, "failure replayed automatically")
	if err := c.Retry(); err != nil {
		t.Fatal(err)
	}
	retry := receivePlayer(t, calls)
	if retry.target != 60 {
		t.Fatalf("retry target = %d", retry.target)
	}
	if state := c.State(); state.Err == nil {
		t.Fatal("persistent error disappeared before successful readback")
	}
	retry.reply <- playerTestReply{confirmed: 60}
	state := waitPlayerVolume(t, c, func(s playerVolumeState) bool { return !s.Pending && s.Confirmed == 60 })
	if state.Err != nil || state.Uncertain || state.CanRetry {
		t.Fatalf("confirmed retry did not clear the error: %+v", state)
	}
}

func TestPlayerVolumeRejectsStaleObservations(t *testing.T) {
	clock := newPlayerTestClock()
	calls := make(chan playerTestWrite, 8)
	var active, mostActive atomic.Int32
	c := newTestPlayerVolume(context.Background(), t, clock, playerTestWriter(calls, &active, &mostActive))
	revision := c.State().Revision
	if accepted, err := c.ObserveConfirmed(30, revision); err != nil || !accepted {
		t.Fatalf("fresh observation rejected: %v %v", accepted, err)
	}
	revision = c.State().Revision
	if err := c.SetTarget(40); err != nil {
		t.Fatal(err)
	}
	first := receivePlayer(t, calls)
	if accepted, err := c.ObserveConfirmed(20, revision); err != nil || accepted {
		t.Fatalf("observation predating an action was accepted: %v %v", accepted, err)
	}
	if state := c.State(); state.Confirmed != 30 || state.Target != 40 || !state.Pending {
		t.Fatalf("stale observation overwrote pending intent: %+v", state)
	}
	first.reply <- playerTestReply{confirmed: 40}
	waitPlayerVolume(t, c, func(s playerVolumeState) bool { return !s.Pending && s.Confirmed == 40 })
	if accepted, err := c.ObserveConfirmed(20, revision); err != nil || accepted {
		t.Fatalf("late observation overwrote confirmed write: %v %v", accepted, err)
	}
	revision = c.State().Revision
	if accepted, err := c.ObserveConfirmed(35, revision); err != nil || !accepted {
		t.Fatalf("current observation rejected: %v %v", accepted, err)
	}
	if state := c.State(); state.Confirmed != 35 || state.Target != 35 || state.Pending {
		t.Fatalf("current observation was not confirmed: %+v", state)
	}
	if accepted, err := c.ObserveConfirmed(101, c.State().Revision); err == nil || accepted {
		t.Fatal("invalid observed volume was accepted")
	}
}

func TestPlayerVolumeCancellationInFlight(t *testing.T) {
	clock := newPlayerTestClock()
	calls := make(chan playerTestWrite, 8)
	var active, mostActive atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	c := newTestPlayerVolume(ctx, t, clock, playerTestWriter(calls, &active, &mostActive))
	if err := c.SetTarget(40); err != nil {
		t.Fatal(err)
	}
	receivePlayer(t, calls)
	if err := c.Release(60); err != nil {
		t.Fatal(err)
	}
	cancel()
	receivePlayer(t, c.Done())
	state := c.State()
	if !state.Stopped || state.Pending || !state.Uncertain || state.Err == nil || state.CanRetry {
		t.Fatalf("in-flight cancellation was not reported: %+v", state)
	}
	if err := c.Retry(); !errors.Is(err, context.Canceled) {
		t.Fatalf("retired session allowed retry: %v", err)
	}
	assertNoPlayerWrite(t, calls, "retired target was replayed")
}

func TestPlayerVolumeCancellationAcceptedWrite(t *testing.T) {
	started := make(chan int, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	writer := func(_ context.Context, target int) (playerVolumeOutcome, error) {
		started <- target
		<-release
		return playerVolumeOutcome{Attempted: true, Uncertain: true}, errors.New("response lost")
	}
	c := newTestPlayerVolume(context.Background(), t, nil, writer)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	if err := c.SetTarget(40); err != nil {
		t.Fatal(err)
	}
	if got := receivePlayer(t, started); got != 40 {
		t.Fatalf("first target = %d", got)
	}
	if err := c.Release(60); err != nil {
		t.Fatal(err)
	}
	c.Stop()
	if err := c.SetTarget(70); !errors.Is(err, context.Canceled) {
		t.Fatalf("retired session accepted a target: %v", err)
	}
	select {
	case <-c.Done():
		t.Fatal("worker reported shutdown before accepted write returned")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	receivePlayer(t, c.Done())
	state := c.State()
	if !state.Uncertain || state.Err == nil || state.Pending || state.CanRetry {
		t.Fatalf("accepted old-target write was not reported as uncertain: %+v", state)
	}
	select {
	case target := <-started:
		t.Fatalf("queued target %d was replayed after retirement", target)
	default:
	}
}

func TestPlayerVolumeCancellationQueued(t *testing.T) {
	clock := newPlayerTestClock()
	calls := make(chan playerTestWrite, 8)
	var active, mostActive atomic.Int32
	c := newTestPlayerVolume(context.Background(), t, clock, playerTestWriter(calls, &active, &mostActive))
	if err := c.SetTarget(30); err != nil {
		t.Fatal(err)
	}
	first := receivePlayer(t, calls)
	if err := c.Release(40); err != nil {
		t.Fatal(err)
	}
	first.reply <- playerTestReply{confirmed: 30}
	receivePlayer(t, clock.scheduled)
	c.Stop()
	receivePlayer(t, c.Done())
	clock.advance(time.Second)
	state := c.State()
	if !state.Stopped || state.Pending || state.Uncertain || state.Confirmed != 30 {
		t.Fatalf("queued cancellation was misreported: %+v", state)
	}
	if err := c.SetTarget(40); !errors.Is(err, context.Canceled) {
		t.Fatalf("retired session accepted target: %v", err)
	}
	assertNoPlayerWrite(t, calls, "queued target survived cancellation")
}

func TestPlayerVolumeThrottleDoesNotBlockMute(t *testing.T) {
	f, speaker := newCLIFixture(t)
	controls, err := newPlayerControls(&speaker)
	if err != nil {
		t.Fatal(err)
	}
	clock := newPlayerTestClock()
	c := newTestPlayerVolume(context.Background(), t, clock, controls.SetVolume)
	if err := c.SetTarget(30); err != nil {
		t.Fatal(err)
	}
	waitPlayerVolume(t, c, func(s playerVolumeState) bool { return !s.Pending && s.Confirmed == 30 })
	if err := c.SetTarget(31); err != nil {
		t.Fatal(err)
	}
	receivePlayer(t, clock.scheduled)
	result := make(chan error, 1)
	go func() {
		_, err := controls.Perform(context.Background(), playerControlMute)
		result <- err
	}()
	if err := receivePlayer(t, result); err != nil {
		t.Fatalf("mute was blocked by the volume timer: %v", err)
	}
	posts := f.postRequests()
	if len(posts) != 2 || posts[0].Path != "player:volume" || posts[1].Path != "settings:/mediaPlayer/mute" {
		t.Fatalf("unexpected requests during rate wait: %+v", posts)
	}
}
