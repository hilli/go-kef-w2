// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hilli/go-kef-w2/kefw2"
)

const (
	podcastTestBase = "airable:https://podcast.example/airable/feeds"
	podcastTestShow = "airable:https://podcast.example/show/serial"
	podcastTestEP1  = "airable:https://podcast.example/episode/one"
	podcastTestEP2  = "airable:https://podcast.example/episode/two"
)

func podcastJSONFixture(t *testing.T) (*cliFixture, string, string) {
	t.Helper()
	f, speaker := newCLIFixture(t)
	show := kefw2.ContentItem{
		Title: "Serial", Path: podcastTestShow, Type: "container", ID: "airable://airable/feeds/serial",
	}
	episode1 := kefw2.ContentItem{Title: "Episode One", Path: podcastTestEP1, Type: "audio"}
	episode2 := kefw2.ContentItem{
		Title: "Episode Two", Path: podcastTestEP2, Type: "audio",
		Context: &kefw2.Context{Path: podcastTestShow + "/episodes"},
	}
	podcastFixtureRows(t, f, podcastTestBase, nil, show)
	f.mu.Lock()
	f.rows["airable:linkService_airable.feeds"] = `{"rowsRedirect":"` + podcastTestBase + `"}`
	f.mu.Unlock()
	for _, path := range []string{"search", "favorites", "popular", "trending", "history"} {
		key := podcastTestBase + "/" + path
		if path == "search" {
			key = "search"
		}
		podcastFixtureRows(t, f, key, nil, show)
	}
	podcastFixtureRows(t, f, podcastTestShow, &show, kefw2.ContentItem{
		Title: "Episodes", Type: "container", Path: podcastTestShow + "/episodes",
	})
	podcastFixtureRows(t, f, podcastTestShow+"/episodes", nil,
		kefw2.ContentItem{Title: "Header", Type: "container", Path: "header"}, episode1, episode2)
	podcastFixtureRows(t, f, podcastTestEP1, &episode1)
	podcastFixtureRows(t, f, podcastTestEP2, &episode2)
	podcastFixtureRows(t, f, podcastTestBase+"/filter", nil,
		kefw2.ContentItem{Title: "Genres", Type: "container", Path: "genres"})
	podcastFixtureRows(t, f, "genres", nil,
		kefw2.ContentItem{Title: "Comedy", Type: "container", Path: "comedy"})
	podcastFixtureRows(t, f, "comedy", nil, show)
	home := t.TempDir()
	return f, home, writeCLIConfig(t, home, speaker)
}

func podcastFixtureRows(t *testing.T, f *cliFixture, path string, roles *kefw2.ContentItem, rows ...kefw2.ContentItem) {
	t.Helper()
	data, err := json.Marshal(kefw2.RowsResponse{RowsCount: len(rows), Rows: rows, Roles: roles})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[path] = string(data)
}

func TestPodcastJSONListsDoNotPlay(t *testing.T) {
	for _, args := range [][]string{
		{}, {"search", "Serial"}, {"favorites"}, {"popular"}, {"trending"}, {"history"},
		{"browse"}, {"browse", "trending"}, {"browse", "favorites"}, {"browse", "history"},
		{"filter"}, {"filter", "Genres"}, {"filter", "Genres/Comedy"},
		{"episodes", "--path", podcastTestShow},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f, home, config := podcastJSONFixture(t)
			argv := append([]string{"podcast"}, args...)
			stdout, stderr, err := runCLI(t, home, config, append(argv, "--json")...)
			result := decodeCLI[struct {
				Speaker speakerIdentity `json:"speaker"`
				Data    contentPage     `json:"data"`
			}](t, stdout, stderr, err)
			want := 1
			if len(args) > 0 && args[0] == "episodes" {
				want = 2
				if result.Data.Items[1].Path != podcastTestEP2 {
					t.Fatalf("wrong episode list: %+v", result.Data)
				}
			}
			if result.Data.Items == nil || len(result.Data.Items) != want || result.Data.HasMore ||
				result.Data.TotalRows != want || result.Speaker.Name != "Office" {
				t.Fatalf("unexpected list: %+v", result)
			}
			if posts := f.postRequests(); len(posts) != 0 {
				t.Fatalf("list mutated speaker: %+v", posts)
			}
		})
	}
}

func TestPodcastJSONExactPlayback(t *testing.T) {
	for _, args := range [][]string{
		{"play", "--episode-path", podcastTestEP2},
		{"play", "--path", podcastTestShow, "--episode-path", podcastTestEP2},
		{"play", "Serial/Episode Two"},
		{"favorites", "Serial/Episode Two"},
		{"popular", "Serial/Episode Two"},
		{"trending", "Serial/Episode Two"},
		{"history", "Serial/Episode Two"},
		{"search", "--path", podcastTestShow, "--episode-path", podcastTestEP2, "--action", "play"},
		{"play", "--path", podcastTestShow, "--latest"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f, home, config := podcastJSONFixture(t)
			stdout, stderr, err := runCLI(t, home, config, append(append([]string{"podcast"}, args...), "--json")...)
			result := decodeCLI[struct {
				Data struct {
					actionResult
					Item   contentSummary `json:"item"`
					Latest bool           `json:"latest"`
				} `json:"data"`
			}](t, stdout, stderr, err)
			want := podcastTestEP2
			latest := strings.Contains(strings.Join(args, " "), "--latest")
			if latest {
				want = podcastTestEP1
			}
			if !result.Data.Accepted || result.Data.Action != "play_podcast" ||
				result.Data.Item.Path != want || result.Data.Latest != latest {
				t.Fatalf("unexpected action: %+v", result)
			}
			posts := f.postRequests()
			if len(posts) != 2 || posts[0].Path != "playlists:pl/clear" || posts[1].Path != "player:player/control" {
				t.Fatalf("wrong playback requests: %+v", posts)
			}
			var value struct {
				MediaRoles struct {
					Path string `json:"path"`
				} `json:"mediaRoles"`
			}
			if err := json.Unmarshal(*posts[1].Value, &value); err != nil {
				t.Fatal(err)
			}
			if value.MediaRoles.Path != want {
				t.Fatalf("played parent/first episode instead of selection: %+v", value)
			}
		})
	}
}

func TestPodcastJSONLatestAndQueue(t *testing.T) {
	for _, args := range [][]string{
		{"play", "Serial", "--latest", "--queue"},
		{"favorites", "Serial", "--latest", "--queue"},
		{"popular", "--path", podcastTestShow, "--latest", "--action", "queue"},
		{"filter", "--episode-path", podcastTestEP1, "--queue"},
		{"browse", "trending", "--episode-path", podcastTestEP1, "--queue"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f, home, config := podcastJSONFixture(t)
			stdout, stderr, err := runCLI(t, home, config, append(append([]string{"podcast"}, args...), "--json")...)
			result := decodeCLI[struct {
				Data struct {
					actionResult
					Item contentSummary `json:"item"`
				} `json:"data"`
			}](t, stdout, stderr, err)
			if !result.Data.Accepted || result.Data.Action != "queue_podcast" || result.Data.Item.Path != podcastTestEP1 {
				t.Fatalf("unexpected action: %+v", result)
			}
			posts := f.postRequests()
			if len(posts) != 1 || posts[0].Path != "playlists:pl/addexternalitems" ||
				!strings.Contains(string(*posts[0].Value), `"mode":"append"`) ||
				!strings.Contains(string(*posts[0].Value), podcastTestEP1) {
				t.Fatalf("queue must only append selected episode: %+v", posts)
			}
		})
	}
}

func TestPodcastJSONFavorites(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		suffix string
	}{
		{[]string{"search", "--path", podcastTestShow, "--save-favorite"}, "/insert"},
		{[]string{"popular", "Serial", "--save-favorite"}, "/insert"},
		{[]string{"filter", "--path", podcastTestShow, "--action", "favorite"}, "/insert"},
		{[]string{"favorites", "Serial", "--remove"}, "/remove"},
		{[]string{"favorites", "--path", podcastTestShow, "--action", "remove"}, "/remove"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			f, home, config := podcastJSONFixture(t)
			stdout, stderr, err := runCLI(t, home, config, append(append([]string{"podcast"}, tc.args...), "--json")...)
			result := decodeCLI[struct {
				Data struct {
					actionResult
					Item contentSummary `json:"item"`
				} `json:"data"`
			}](t, stdout, stderr, err)
			posts := f.postRequests()
			wantAction := "add_podcast_favorite"
			if tc.suffix == "/remove" {
				wantAction = "remove_podcast_favorite"
			}
			if !result.Data.Accepted || result.Data.Item.Path != podcastTestShow || len(posts) != 1 ||
				result.Data.Action != wantAction ||
				!strings.HasSuffix(posts[0].Path, "/airable/feed/serial"+tc.suffix) {
				t.Fatalf("unexpected favorite result=%+v posts=%+v", result, posts)
			}
		})
	}
}

func TestPodcastJSONInvalidSelections(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"search", "Serial", "--queue"}, "explicit"},
		{[]string{"favorites", "--remove"}, "explicit"},
		{[]string{"popular", "--save-favorite"}, "explicit"},
		{[]string{"trending", "--queue"}, "explicit"},
		{[]string{"history", "--queue"}, "explicit"},
		{[]string{"browse", "--queue"}, "explicit"},
		{[]string{"filter", "Genres", "--queue"}, "explicit"},
		{[]string{"play", "Serial"}, "requires --latest"},
		{[]string{"play", "Serial", "--path", podcastTestShow}, "mutually exclusive"},
		{[]string{"play", "--episode-path", podcastTestEP2, "--latest"}, "mutually exclusive"},
		{[]string{"play", "--episode-path", podcastTestEP2, "--queue", "--action", "play"}, "mutually exclusive"},
		{[]string{"play", "--path", ""}, "must not be empty"},
		{[]string{"play", "--path", podcastTestEP2, "--latest"}, "--path must select"},
		{[]string{"play", "--episode-path", podcastTestShow}, "not an audio episode"},
		{[]string{"play", "--episode-path", podcastTestEP2, "--action", "bad"}, "unknown podcast action"},
		{[]string{"search", "--episode-path", podcastTestEP2, "--action", "favorite"}, "favorite actions select a show"},
		{[]string{"search", "Serial", "--path", podcastTestShow, "--save-favorite"}, "mutually exclusive"},
		{[]string{"favorites", "Serial/Episode One", "--remove"}, "episode name"},
		{[]string{"browse", "wrong"}, "unknown podcast category"},
		{[]string{"filter", "Genres/"}, "empty segment"},
		{[]string{"episodes"}, "requires --path"},
		{[]string{"play", "--path", podcastTestShow, "--episode-path", "missing"}, "not found"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			f, home, config := podcastJSONFixture(t)
			stdout, stderr, err := runCLI(t, home, config, append(append([]string{"podcast"}, tc.args...), "--json")...)
			requireCLIFailure(t, stdout, stderr, err, tc.want)
			if posts := f.postRequests(); len(posts) != 0 {
				t.Fatalf("invalid input mutated speaker: %+v", posts)
			}
		})
	}
}

func TestPodcastJSONAmbiguousNames(t *testing.T) {
	for _, titles := range [][]string{{"Serial", "SERIAL"}, {"Serial One", "Serial Two"}} {
		f, home, config := podcastJSONFixture(t)
		items := make([]kefw2.ContentItem, 0)
		for i, title := range titles {
			items = append(items, kefw2.ContentItem{Title: title, Type: "container", Path: string(rune('a' + i))})
		}
		podcastFixtureRows(t, f, "search", nil, items...)
		stdout, stderr, err := runCLI(t, home, config, "podcast", "play", "Serial", "--latest", "--json")
		requireCLIFailure(t, stdout, stderr, err, "ambiguous")
		if len(f.postRequests()) != 0 {
			t.Fatal("ambiguous show caused mutation")
		}
	}
	f, home, config := podcastJSONFixture(t)
	podcastFixtureRows(t, f, podcastTestShow+"/episodes", nil,
		kefw2.ContentItem{Title: "Same", Type: "audio", Path: podcastTestEP1},
		kefw2.ContentItem{Title: "Same", Type: "audio", Path: podcastTestEP2})
	stdout, stderr, err := runCLI(t, home, config, "podcast", "favorites", "Serial/Same", "--json")
	requireCLIFailure(t, stdout, stderr, err, "ambiguous")
	if len(f.postRequests()) != 0 {
		t.Fatal("ambiguous episode caused mutation")
	}
	t.Run("ambiguous filter", func(t *testing.T) {
		f, home, config := podcastJSONFixture(t)
		podcastFixtureRows(t, f, podcastTestBase+"/filter", nil,
			kefw2.ContentItem{Title: "Genres", Type: "container", Path: "one"},
			kefw2.ContentItem{Title: "GENRES", Type: "container", Path: "two"})
		stdout, stderr, err := runCLI(t, home, config, "podcast", "filter", "Genres", "--json")
		requireCLIFailure(t, stdout, stderr, err, "ambiguous")
		if len(f.postRequests()) != 0 {
			t.Fatal("ambiguous filter caused mutation")
		}
	})
}

func TestPodcastJSONSearchPagination(t *testing.T) {
	for _, mode := range []string{"list", "ambiguous", "error", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.mu.Lock()
			f.rows["airable:linkService_airable.feeds"] = `{"rowsRedirect":"` + podcastTestBase + `"}`
			f.mu.Unlock()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Query().Get("path"), "/search") {
					f.serveHTTP(w, r)
					return
				}
				if r.URL.Query().Get("from") != "0" {
					switch mode {
					case "error":
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					case "truncated":
						_, _ = w.Write([]byte(`{"rowsCount":2,"rows":[]}`))
						return
					}
				}
				_ = json.NewEncoder(w).Encode(kefw2.RowsResponse{
					RowsCount: 2, Rows: []kefw2.ContentItem{
						{Title: "Serial", Type: "container", Path: podcastTestShow},
					},
				})
			}))
			defer server.Close()
			speaker.IPAddress = strings.TrimPrefix(server.URL, "http://")
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			args := []string{"podcast", "play", "Serial", "--latest", "--json"}
			if mode == "list" {
				args = []string{"podcast", "search", "Serial", "--json"}
			}
			stdout, stderr, err := runCLI(t, home, config, args...)
			switch mode {
			case "list":
				result := decodeCLI[struct {
					Data contentPage `json:"data"`
				}](t, stdout, stderr, err)
				if len(result.Data.Items) != 2 || result.Data.HasMore {
					t.Fatalf("not all search pages returned: %+v", result)
				}
			case "ambiguous":
				requireCLIFailure(t, stdout, stderr, err, "ambiguous")
			case "error", "truncated":
				requireCLIFailure(t, stdout, stderr, err, "offset 1")
			}
			if len(f.postRequests()) != 0 {
				t.Fatal("search/list or incomplete/ambiguous results caused mutation")
			}
		})
	}
}

func TestPodcastJSONEmptyAndAPIErrors(t *testing.T) {
	t.Run("empty search", func(t *testing.T) {
		f, home, config := podcastJSONFixture(t)
		podcastFixtureRows(t, f, "search", nil)
		stdout, stderr, err := runCLI(t, home, config, "podcast", "search", "nothing", "--json")
		result := decodeCLI[struct {
			Data contentPage `json:"data"`
		}](t, stdout, stderr, err)
		if result.Data.Items == nil || len(result.Data.Items) != 0 || result.Data.HasMore {
			t.Fatalf("empty list must be []: %+v", result)
		}
	})
	t.Run("get failure", func(t *testing.T) {
		f, home, config := podcastJSONFixture(t)
		f.mu.Lock()
		f.failPath = podcastTestBase + "/favorites"
		f.mu.Unlock()
		stdout, stderr, err := runCLI(t, home, config, "podcast", "favorites", "--json")
		requireCLIFailure(t, stdout, stderr, err, "HTTP 503")
		if len(f.postRequests()) != 0 {
			t.Fatal("failed list caused mutation")
		}
	})
	t.Run("empty episodes", func(t *testing.T) {
		f, home, config := podcastJSONFixture(t)
		podcastFixtureRows(t, f, podcastTestShow+"/episodes", nil)
		stdout, stderr, err := runCLI(t, home, config, "podcast", "episodes", "--path", podcastTestShow, "--json")
		result := decodeCLI[struct {
			Data contentPage `json:"data"`
		}](t, stdout, stderr, err)
		if result.Data.Items == nil || len(result.Data.Items) != 0 {
			t.Fatalf("empty episodes must be []: %+v", result)
		}
		stdout, stderr, err = runCLI(t, home, config, "podcast", "play", "Serial", "--latest", "--json")
		requireCLIFailure(t, stdout, stderr, err, "no audio episodes")
		if len(f.postRequests()) != 0 {
			t.Fatal("empty episodes caused mutation")
		}
	})
	t.Run("episodes API failure", func(t *testing.T) {
		f, home, config := podcastJSONFixture(t)
		f.mu.Lock()
		f.failPath = podcastTestShow + "/episodes"
		f.mu.Unlock()
		stdout, stderr, err := runCLI(t, home, config, "podcast", "episodes", "--path", podcastTestShow, "--json")
		requireCLIFailure(t, stdout, stderr, err, "HTTP 503")
	})
	t.Run("mutation failure", func(t *testing.T) {
		f, home, config := podcastJSONFixture(t)
		f.mu.Lock()
		f.postFail = true
		f.mu.Unlock()
		stdout, stderr, err := runCLI(t, home, config, "podcast", "favorites", "Serial", "--remove", "--json")
		requireCLIFailure(t, stdout, stderr, err, "503")
	})
	t.Run("no arbitrary details fallback", func(t *testing.T) {
		f, home, config := podcastJSONFixture(t)
		podcastFixtureRows(t, f, "wrong", nil,
			kefw2.ContentItem{Title: "Other", Type: "audio", Path: podcastTestEP2})
		stdout, stderr, err := runCLI(t, home, config, "podcast", "play", "--episode-path", "wrong", "--json")
		requireCLIFailure(t, stdout, stderr, err, "no exact podcast details")
		if len(f.postRequests()) != 0 {
			t.Fatal("mismatched details caused mutation")
		}
	})
	t.Run("JSON-only options", func(t *testing.T) {
		_, home, config := podcastJSONFixture(t)
		stdout, stderr, err := runCLI(t, home, config, "podcast", "play", "Serial", "--latest")
		requireCLIFailure(t, stdout, stderr, err, "requires --json")
	})
}
