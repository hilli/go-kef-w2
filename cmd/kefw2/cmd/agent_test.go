// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/hilli/go-kef-w2/kefw2"
)

func TestCLIProcess(t *testing.T) {
	if os.Getenv("KEFW2_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"kefw2"}, os.Args[i+1:]...)
			Execute()
			os.Exit(0)
		}
	}
	t.Fatal("missing CLI argument separator")
}

func runCLI(t *testing.T, home, config string, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	argv := append([]string{"-test.run=^TestCLIProcess$", "--", "--config", config}, args...)
	process := exec.CommandContext(ctx, executable, argv...)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "KEFW2_") && key != "HOME" &&
			key != "XDG_CONFIG_HOME" && key != "XDG_CACHE_HOME" {
			process.Env = append(process.Env, entry)
		}
	}
	process.Env = append(process.Env, "KEFW2_TEST_PROCESS=1", "HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, "config"), "XDG_CACHE_HOME="+filepath.Join(home, "cache"))
	var stdout, stderr bytes.Buffer
	process.Stdout, process.Stderr = &stdout, &stderr
	err = process.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI failed to terminate: %v", ctx.Err())
	}
	return stdout.String(), stderr.String(), err
}

func decodeCLI[T any](t *testing.T, stdout, stderr string, runErr error) T {
	t.Helper()
	if runErr != nil || stderr != "" {
		t.Fatalf("CLI: %v; stderr=%q; stdout=%q", runErr, stderr, stdout)
	}
	if strings.Contains(stdout, "\x1b") {
		t.Fatalf("ANSI output in JSON: %q", stdout)
	}
	var result T
	decoder := json.NewDecoder(strings.NewReader(stdout))
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v; stdout=%q", err, stdout)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("extra output after JSON: %v; stdout=%q", err, stdout)
	}
	return result
}

func requireCLIFailure(t *testing.T, stdout, stderr string, err error, contains string) {
	t.Helper()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || stdout != "" ||
		!strings.Contains(stderr, contains) {
		t.Fatalf("want failure containing %q, got err=%v stdout=%q stderr=%q", contains, err, stdout, stderr)
	}
}

func writeCLIConfig(t *testing.T, home string, speakers ...kefw2.KEFSpeaker) string {
	t.Helper()
	data, err := json.Marshal(struct {
		Speakers []kefw2.KEFSpeaker `json:"speakers"`
	}{speakers})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "kefw2.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

type cliFixture struct {
	mu          sync.Mutex
	data        map[string]string
	rows        map[string]string
	failPath    string
	postFail    bool
	requireWiFi bool
	posts       []kefw2.KEFPostRequest
}

func newCLIFixture(t *testing.T) (*cliFixture, kefw2.KEFSpeaker) {
	t.Helper()
	f := &cliFixture{
		data: map[string]string{
			"settings:/system/primaryMacAddress": `[{"type":"string_","string_":"aa:bb"}]`,
			"settings:/deviceName":               `[{"type":"string_","string_":"Office"}]`,
			"settings:/releasetext":              `[{"type":"string_","string_":"ls60w_1.2.3"}]`,
			"settings:/kef/host/maximumVolume":   `[{"type":"i32_","i32_":80}]`,
			"settings:/kef/play/physicalSource":  `[{"type":"kefPhysicalSource","kefPhysicalSource":"wifi"}]`,
			"settings:/kef/host/speakerStatus":   `[{"type":"kefSpeakerStatus","kefSpeakerStatus":"powerOn"}]`,
			"player:volume":                      `[{"type":"i32_","i32_":25}]`,
			"settings:/mediaPlayer/mute":         `[{"type":"bool_","bool_":false}]`,
			"settings:/mediaPlayer/playMode":     `[{"type":"playerPlayMode","playerPlayMode":"normal"}]`,
			"player:player/data/playTime":        `[{"type":"i64_","i64_":12345}]`,
			"player:player/data": `[{"state":"playing","status":{"duration":180000},` +
				`"trackRoles":{"title":"Song","icon":"http://invalid.local/art.png",` +
				`"mediaData":{"metaData":{"artist":"Artist","album":"Album"}}}}]`,
		},
		rows: map[string]string{
			"grouping:members":      `{"groupingMember":[{"master":{"id":"office","name":"Office"}}]}`,
			"ui:/airableradios":     `{"rowsRedirect":"airable:https://radio.example/airable/radios"}`,
			"playlists:pq/getitems": `{"rowsCount":0,"rows":[]}`,
		},
	}
	server := httptest.NewServer(http.HandlerFunc(f.serveHTTP))
	t.Cleanup(server.Close)
	return f, kefw2.KEFSpeaker{
		Name: "Office", Model: "KEF LS60 Wireless", IPAddress: strings.TrimPrefix(server.URL, "http://"),
	}
}

func (f *cliFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Query().Get("path")
	if r.Method == http.MethodPost {
		var request struct {
			kefw2.KEFPostRequest
			Role string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		payload := request.KEFPostRequest
		if payload.Roles == "" {
			payload.Roles = request.Role
		}
		f.posts = append(f.posts, payload)
		if f.postFail {
			http.Error(w, `{}`, http.StatusServiceUnavailable)
			return
		}
		if f.requireWiFi && (payload.Path == "playlists:pl/clear" || payload.Path == "player:player/control") &&
			!strings.Contains(f.data["settings:/kef/play/physicalSource"], `"wifi"`) {
			http.Error(w, `{"error":{"message":"Forbidden"}}`, http.StatusUnauthorized)
			return
		}
		if payload.Roles == "value" {
			f.data[payload.Path] = "[" + string(*payload.Value) + "]"
		}
		if payload.Path == "playlists:pl/clear" {
			f.rows["playlists:pq/getitems"] = `{"rowsCount":0,"rows":[]}`
		}
		if payload.Path == "playlists:pl/addexternalitems" {
			if err := f.addQueueItems(*payload.Value); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		_, _ = io.WriteString(w, `{}`)
		return
	}
	if path == f.failPath {
		http.Error(w, `{}`, http.StatusServiceUnavailable)
		return
	}
	var value string
	switch r.URL.Path {
	case "/api/getData":
		value = f.data[path]
	case "/api/getRows":
		value = f.rows[path]
		if strings.Contains(path, "/search") {
			value = f.rows["search"]
		}
	}
	if value == "" {
		http.Error(w, "unexpected API path: "+path, http.StatusNotFound)
		return
	}
	_, _ = io.WriteString(w, value)
}

func (f *cliFixture) addQueueItems(value json.RawMessage) error {
	var request struct {
		Items []struct {
			Roles string `json:"nsdkRoles"`
		} `json:"items"`
	}
	if err := json.Unmarshal(value, &request); err != nil {
		return err
	}
	var rows kefw2.RowsResponse
	if err := json.Unmarshal([]byte(f.rows["playlists:pq/getitems"]), &rows); err != nil {
		return err
	}
	for _, entry := range request.Items {
		var item kefw2.ContentItem
		if err := json.Unmarshal([]byte(entry.Roles), &item); err != nil {
			return err
		}
		rows.Rows = append(rows.Rows, item)
	}
	rows.RowsCount = len(rows.Rows)
	data, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	f.rows["playlists:pq/getitems"] = string(data)
	return nil
}

func (f *cliFixture) postRequests() []kefw2.KEFPostRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]kefw2.KEFPostRequest(nil), f.posts...)
}

func TestAgentSpeakerListing(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(home, "missing.yaml")
	stdout, stderr, err := runCLI(t, home, config, "config", "speaker", "list", "--json")
	result := decodeCLI[struct {
		Data struct {
			Items []listedSpeaker `json:"items"`
		} `json:"data"`
	}](t, stdout, stderr, err)
	if result.Data.Items == nil || len(result.Data.Items) != 0 {
		t.Fatalf("expected empty array, got %+v", result.Data.Items)
	}
	_, speaker := newCLIFixture(t)
	config = writeCLIConfig(t, home, speaker)
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runCLI(t, home, config, "--speaker", "invalid:99999", "config", "speaker", "list", "--json")
	result = decodeCLI[struct {
		Data struct {
			Items []listedSpeaker `json:"items"`
		} `json:"data"`
	}](t, stdout, stderr, err)
	if len(result.Data.Items) != 1 || !result.Data.Items[0].Default || !result.Data.Items[0].Configured {
		t.Fatalf("unexpected speakers: %+v", result)
	}
	after, err := os.ReadFile(config)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("listing changed config: %v", err)
	}
}

func TestAgentStatus(t *testing.T) {
	for _, source := range []string{"wifi", "bluetooth", "standby", "optical"} {
		t.Run(source, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.data["settings:/kef/play/physicalSource"] = fmt.Sprintf(
				`[{"type":"kefPhysicalSource","kefPhysicalSource":%q}]`, source)
			if source == "standby" || source == "optical" {
				f.failPath = "player:player/data"
			}
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			stdout, stderr, err := runCLI(t, home, config, "--speaker", speaker.Name, "status", "--json")
			result := decodeCLI[struct {
				Speaker speakerIdentity `json:"speaker"`
				Data    statusResponse  `json:"data"`
			}](t, stdout, stderr, err)
			if result.Speaker.IPAddress != speaker.IPAddress || result.Data.Volume != 25 || result.Data.Muted ||
				string(result.Data.Source) != source {
				t.Fatalf("unexpected state: %+v", result)
			}
			if source == "wifi" || source == "bluetooth" {
				p := result.Data.Playback
				if p == nil || p.Track.Title != "Song" || p.PositionMS == nil || *p.PositionMS != 12345 ||
					p.DurationMS == nil || *p.DurationMS != 180000 {
					t.Fatalf("unexpected playback: %+v", p)
				}
			} else if result.Data.Playback != nil {
				t.Fatalf("physical/standby source exposed playback: %+v", result.Data.Playback)
			}
			if len(f.postRequests()) != 0 {
				t.Fatal("status made a mutation")
			}
		})
	}
}

func TestAgentReadFailures(t *testing.T) {
	for _, command := range []string{"status", "volume", "mute", "source"} {
		t.Run(command, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			switch command {
			case "status", "volume":
				f.failPath = "player:volume"
			case "mute":
				f.failPath = "settings:/mediaPlayer/mute"
			case "source":
				f.failPath = "settings:/kef/play/physicalSource"
			}
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			stdout, stderr, err := runCLI(t, home, config, command, "--json")
			requireCLIFailure(t, stdout, stderr, err, "503")
		})
	}
}

func TestAgentAbsoluteControls(t *testing.T) {
	for _, tc := range []struct {
		command  string
		value    string
		path     string
		body     string
		readback string
	}{
		{"volume", "35", "player:volume", `{"type":"i32_","i32_":35}`, `{"volume":35}`},
		{"mute", "on", "settings:/mediaPlayer/mute", `{"type":"bool_","bool_":true}`, `{"muted":true}`},
		{"source", "optical", "settings:/kef/play/physicalSource",
			`{"type":"kefPhysicalSource","kefPhysicalSource":"optical"}`, `{"source":"optical"}`},
	} {
		t.Run(tc.command, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			stdout, stderr, err := runCLI(t, home, config, tc.command, tc.value, "--json")
			result := decodeCLI[struct {
				Data actionResult `json:"data"`
			}](t, stdout, stderr, err)
			if !result.Data.Accepted {
				t.Fatal("mutation not reported as accepted")
			}
			posts := f.postRequests()
			if len(posts) != 1 || posts[0].Path != tc.path {
				t.Fatalf("unexpected mutations: %+v", posts)
			}
			var got, want any
			if err := json.Unmarshal(*posts[0].Value, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.body), &want); err != nil {
				t.Fatal(err)
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)
			if !bytes.Equal(gotJSON, wantJSON) {
				t.Fatalf("mutation = %s, want %s", gotJSON, wantJSON)
			}
			stdout, stderr, err = runCLI(t, home, config, tc.command, "--json")
			readback := decodeCLI[struct {
				Data json.RawMessage `json:"data"`
			}](t, stdout, stderr, err)
			if string(readback.Data) != tc.readback {
				t.Fatalf("readback = %s, want %s", readback.Data, tc.readback)
			}
		})
	}
}

func TestAgentInvalidArgumentsDoNotMutate(t *testing.T) {
	f, speaker := newCLIFixture(t)
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	for _, args := range [][]string{
		{"volume", "35oops", "--json"}, {"volume", "101", "--json"}, {"mute", "maybe", "--json"},
		{"source", "bogus", "--json"}, {"radio", "play", "--path", "", "--json"},
		{"radio", "play", "name", "--path", "station:1", "--json"},
		{"radio", "search", "BBC", "--limit", "0", "--json"},
		{"skill", "show", "--json"},
	} {
		stdout, stderr, err := runCLI(t, home, config, args...)
		requireCLIFailure(t, stdout, stderr, err, "Error:")
	}
	if len(f.postRequests()) != 0 {
		t.Fatal("invalid arguments caused a mutation")
	}
}

func TestAgentSpeakerResolution(t *testing.T) {
	home := t.TempDir()
	f, speaker := newCLIFixture(t)
	config := filepath.Join(home, "missing.yaml")
	stdout, stderr, err := runCLI(t, home, config, "--speaker", speaker.IPAddress, "volume", "--json")
	result := decodeCLI[struct {
		Speaker speakerIdentity `json:"speaker"`
		Data    struct {
			Volume int `json:"volume"`
		} `json:"data"`
	}](t, stdout, stderr, err)
	if result.Data.Volume != 25 || result.Speaker.Name != "Office" {
		t.Fatalf("failed explicit targeting without config: %+v", result)
	}
	if len(f.postRequests()) != 0 {
		t.Fatal("resolution made a mutation")
	}
	second := speaker
	second.IPAddress = "127.0.0.1:1"
	config = writeCLIConfig(t, home, speaker, second)
	stdout, stderr, err = runCLI(t, home, config, "volume", "--json")
	requireCLIFailure(t, stdout, stderr, err, "no unambiguous speaker")
	stdout, stderr, err = runCLI(t, home, config, "--speaker", "Office", "volume", "--json")
	requireCLIFailure(t, stdout, stderr, err, "ambiguous")
	stdout, stderr, err = runCLI(t, home, config, "--speaker", "127.0.0.1:1", "volume", "--json")
	requireCLIFailure(t, stdout, stderr, err, "connection refused")
}

func TestAgentUtilityCommands(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(home, "broken.yaml")
	if err := os.WriteFile(config, []byte("speakers: ["), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"skill", "show"}, {"version"}, {"status", "--help"}} {
		stdout, stderr, err := runCLI(t, home, config, args...)
		if err != nil || stderr != "" || stdout == "" {
			t.Fatalf("utility %v: %v stdout=%q stderr=%q", args, err, stdout, stderr)
		}
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 1 {
		t.Fatalf("utilities created config/cache files: %v %v", entries, err)
	}
	stdout, stderr, err := runCLI(t, home, config, "status", "--json")
	requireCLIFailure(t, stdout, stderr, err, "reading configuration")
}

func TestAgentDiscovery(t *testing.T) {
	previousDiscover, previousViper := discoverSpeakers, viper.GetViper()
	t.Cleanup(func() {
		discoverSpeakers = previousDiscover
		*viper.GetViper() = *previousViper
		speakers, defaultSpeaker, currentSpeaker = nil, nil, nil
	})
	viper.Reset()
	viper.SetConfigType("yaml")
	config := filepath.Join(t.TempDir(), "config", "kefw2.yaml")
	viper.SetConfigFile(config)
	speakers, defaultSpeaker, currentSpeaker = nil, nil, nil
	discoverSpeakers = func(context.Context, time.Duration) ([]*kefw2.KEFSpeaker, error) {
		return []*kefw2.KEFSpeaker{{Name: "Found", IPAddress: "192.0.2.1"}}, nil
	}
	previousJSON, _ := speakerDiscoverCmd.Flags().GetBool("json")
	previousSave, _ := speakerDiscoverCmd.Flags().GetBool("save")
	t.Cleanup(func() {
		_ = speakerDiscoverCmd.Flags().Set("json", fmt.Sprint(previousJSON))
		_ = speakerDiscoverCmd.Flags().Set("save", fmt.Sprint(previousSave))
		speakerDiscoverCmd.SetOut(nil)
	})
	if err := speakerDiscoverCmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	speakerDiscoverCmd.SetOut(&output)
	speakerDiscoverCmd.SetContext(context.Background())
	if err := speakerDiscoverCmd.RunE(speakerDiscoverCmd, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only discovery wrote config: %v", err)
	}
	if err := speakerDiscoverCmd.Flags().Set("save", "true"); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := speakerDiscoverCmd.RunE(speakerDiscoverCmd, nil); err != nil {
		t.Fatal(err)
	}
	result := decodeCLI[struct {
		Data struct {
			Items []listedSpeaker `json:"items"`
			Saved bool            `json:"saved"`
		} `json:"data"`
	}](t, output.String(), "", nil)
	if !result.Data.Saved || len(result.Data.Items) != 1 || !result.Data.Items[0].Configured {
		t.Fatalf("unexpected saved discovery: %+v", result)
	}
	if _, err := os.Stat(config); err != nil {
		t.Fatalf("discovery did not persist configuration: %v", err)
	}
}

func TestAgentHumanControlsRemainAvailable(t *testing.T) {
	_, speaker := newCLIFixture(t)
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"volume"}, "Volume is:"},
		{[]string{"mute"}, "not muted"},
		{[]string{"source"}, "wifi"},
		{[]string{"config", "maxvolume"}, "80%"},
	} {
		stdout, stderr, err := runCLI(t, home, config, tc.args...)
		if err != nil || stderr != "" || !strings.Contains(stdout, tc.want) {
			t.Fatalf("human command %v: err=%v stdout=%q stderr=%q", tc.args, err, stdout, stderr)
		}
	}
}

func TestAgentStatusPlaybackAvailability(t *testing.T) {
	for _, state := range []string{"stopped", "playing", "empty"} {
		t.Run(state, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.data["player:player/data"] = `[{"state":"` + state + `"}]`
			if state == "empty" {
				f.data["player:player/data"] = `[]`
			}
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			stdout, stderr, err := runCLI(t, home, config, "status", "--json")
			if state == "empty" {
				requireCLIFailure(t, stdout, stderr, err, "no player data")
				return
			}
			result := decodeCLI[struct {
				Data statusResponse `json:"data"`
			}](t, stdout, stderr, err)
			p := result.Data.Playback
			if p == nil || p.State != state || p.DurationMS != nil {
				t.Fatalf("unavailable duration must be null: %+v", p)
			}
			if state == "stopped" && (p.PositionMS != nil || p.Track != nil) {
				t.Fatalf("stopped playback has stale metadata: %+v", p)
			}
		})
	}
}
