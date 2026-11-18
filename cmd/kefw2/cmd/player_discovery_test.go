// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hilli/go-kef-w2/kefw2"
)

var (
	_ playerDiscoverySearchClient    = (*kefw2.AirableClient)(nil)
	_ playerDiscoveryFavoritesClient = (*kefw2.AirableClient)(nil)
	_ playerDiscoveryFavoriteClient  = (*kefw2.AirableClient)(nil)
	_ playerDiscoveryQueueClient     = (*kefw2.AirableClient)(nil)
	_ playerDiscoverySource          = (*kefw2.KEFSpeaker)(nil)
)

type playerDiscoveryTestCall struct {
	action, value string
	first, second int
	item          kefw2.ContentItem
	start         bool
}

type fakePlayerDiscovery struct {
	calls          []playerDiscoveryTestCall
	search         func(playerDiscoveryService, string, int, int) (*kefw2.RowsResponse, error)
	favorites      func(playerDiscoveryService, int, int) (*kefw2.RowsResponse, error)
	rows           func(string, int, int) (*kefw2.RowsResponse, error)
	radioDetails   func(string) (*kefw2.ContentItem, error)
	podcastDetails func(string) (*kefw2.ContentItem, error)
	onPlay         func(string, *kefw2.ContentItem) error
	onFavorite     func() error
	onQueue        func() error
	favoriteErr    error
	queueErr       error
}

func (f *fakePlayerDiscovery) SearchRadioPage(query string, offset, limit int) (*kefw2.RowsResponse, error) {
	f.calls = append(f.calls, playerDiscoveryTestCall{"search-radio", query, offset, limit, kefw2.ContentItem{}, false})
	if f.search == nil {
		return nil, errors.New("unexpected radio search")
	}
	return f.search(playerDiscoveryRadio, query, offset, limit)
}

func (f *fakePlayerDiscovery) SearchPodcastsPage(query string, offset, limit int) (*kefw2.RowsResponse, error) {
	f.calls = append(f.calls, playerDiscoveryTestCall{"search-podcast", query, offset, limit, kefw2.ContentItem{}, false})
	if f.search == nil {
		return nil, errors.New("unexpected podcast search")
	}
	return f.search(playerDiscoveryPodcast, query, offset, limit)
}

func (f *fakePlayerDiscovery) GetRadioFavoritesPage(offset, limit int) (*kefw2.RowsResponse, error) {
	f.calls = append(f.calls, playerDiscoveryTestCall{action: "favorites-radio", first: offset, second: limit})
	if f.favorites == nil {
		return nil, errors.New("unexpected radio favorites")
	}
	return f.favorites(playerDiscoveryRadio, offset, limit)
}

func (f *fakePlayerDiscovery) GetPodcastFavoritesPage(offset, limit int) (*kefw2.RowsResponse, error) {
	f.calls = append(f.calls, playerDiscoveryTestCall{action: "favorites-podcast", first: offset, second: limit})
	if f.favorites == nil {
		return nil, errors.New("unexpected podcast favorites")
	}
	return f.favorites(playerDiscoveryPodcast, offset, limit)
}

func (f *fakePlayerDiscovery) GetRows(path string, from, to int) (*kefw2.RowsResponse, error) {
	f.calls = append(f.calls, playerDiscoveryTestCall{action: "rows", value: path, first: from, second: to})
	if f.rows == nil {
		return nil, errors.New("unexpected browse")
	}
	return f.rows(path, from, to)
}

func (f *fakePlayerDiscovery) GetRadioStationByPath(path string) (*kefw2.ContentItem, error) {
	f.calls = append(f.calls, playerDiscoveryTestCall{action: "resolve-radio", value: path})
	if f.radioDetails == nil {
		return nil, errors.New("unexpected radio resolution")
	}
	return f.radioDetails(path)
}

func (f *fakePlayerDiscovery) GetPodcastDetailsExact(path string) (*kefw2.ContentItem, error) {
	f.calls = append(f.calls, playerDiscoveryTestCall{action: "resolve-podcast", value: path})
	if f.podcastDetails == nil {
		return nil, errors.New("unexpected podcast resolution")
	}
	return f.podcastDetails(path)
}

func (f *fakePlayerDiscovery) PlayRadioStation(item *kefw2.ContentItem) error {
	f.calls = append(f.calls, playerDiscoveryTestCall{action: "play-radio", item: playerDiscoveryCopyItem(*item)})
	if f.onPlay != nil {
		return f.onPlay("radio", item)
	}
	return nil
}

func (f *fakePlayerDiscovery) RequestPodcastEpisodeExact(item *kefw2.ContentItem) error {
	f.calls = append(f.calls, playerDiscoveryTestCall{action: "play-podcast", item: playerDiscoveryCopyItem(*item)})
	if f.onPlay != nil {
		return f.onPlay("podcast", item)
	}
	return nil
}

func (f *fakePlayerDiscovery) AddRadioFavorite(item *kefw2.ContentItem) error {
	return f.recordFavorite("add-radio", item)
}

func (f *fakePlayerDiscovery) RemoveRadioFavorite(item *kefw2.ContentItem) error {
	return f.recordFavorite("remove-radio", item)
}

func (f *fakePlayerDiscovery) AddPodcastFavorite(item *kefw2.ContentItem) error {
	return f.recordFavorite("add-podcast", item)
}

func (f *fakePlayerDiscovery) RemovePodcastFavorite(item *kefw2.ContentItem) error {
	return f.recordFavorite("remove-podcast", item)
}

func (f *fakePlayerDiscovery) recordFavorite(action string, item *kefw2.ContentItem) error {
	f.calls = append(f.calls, playerDiscoveryTestCall{action: action, item: playerDiscoveryCopyItem(*item)})
	if f.onFavorite != nil {
		return f.onFavorite()
	}
	return f.favoriteErr
}

func (f *fakePlayerDiscovery) AddToQueue(items []kefw2.ContentItem, start bool) error {
	if len(items) != 1 {
		return fmt.Errorf("expected one track, got %d", len(items))
	}
	f.calls = append(f.calls, playerDiscoveryTestCall{
		action: "enqueue", item: playerDiscoveryCopyItem(items[0]), start: start,
	})
	if f.onQueue != nil {
		return f.onQueue()
	}
	return f.queueErr
}

type fakePlayerDiscoverySource struct {
	current   kefw2.Source
	calls     []string
	sourceErr error
	setErr    error
	onSource  func(context.Context) (kefw2.Source, error)
	onSet     func(context.Context, kefw2.Source) error
}

func (s *fakePlayerDiscoverySource) Source(ctx context.Context) (kefw2.Source, error) {
	s.calls = append(s.calls, "source")
	if s.onSource != nil {
		return s.onSource(ctx)
	}
	return s.current, s.sourceErr
}

func (s *fakePlayerDiscoverySource) SetSource(ctx context.Context, source kefw2.Source) error {
	s.calls = append(s.calls, "set:"+string(source))
	if s.onSet != nil {
		return s.onSet(ctx, source)
	}
	if s.setErr == nil {
		s.current = source
	}
	return s.setErr
}

func playerDiscoveryTestPlayClient(t *testing.T, client *kefw2.AirableClient) playerDiscoveryPlayClient {
	t.Helper()
	playClient, ok := any(client).(playerDiscoveryPlayClient)
	if !ok {
		t.Fatal("AirableClient does not implement exact no-clear podcast playback")
	}
	return playClient
}

func TestPlayerDiscoverySearchPages(t *testing.T) {
	for _, service := range []playerDiscoveryService{playerDiscoveryRadio, playerDiscoveryPodcast} {
		t.Run(string(service), func(t *testing.T) {
			rows := &kefw2.RowsResponse{RowsCount: 30, Rows: []kefw2.ContentItem{
				{Title: "Same", Path: "selected:first", MediaData: &kefw2.MediaData{
					Resources: []kefw2.MediaResource{{URI: "original"}},
				}},
				{Title: "Same", Path: "selected:second"},
			}}
			fake := &fakePlayerDiscovery{search: func(got playerDiscoveryService, query string, offset, limit int) (*kefw2.RowsResponse, error) {
				if got != service || query != "Jazz s q & Blues" || offset != 24 || limit != 10 {
					t.Errorf("search changed request: %s %q %d %d", got, query, offset, limit)
				}
				return rows, nil
			}}
			page, err := playerDiscoverySearch(context.Background(), fake, service, "Jazz s q & Blues", 24, 10)
			if err != nil {
				t.Fatal(err)
			}
			if page.query != "Jazz s q & Blues" || page.offset != 24 || page.limit != 10 ||
				page.total != 30 || page.nextOffset != 26 || !page.hasMore {
				t.Fatalf("incorrect page metadata: %+v", page)
			}
			if len(page.items) != 2 || page.items[1].Path != "selected:second" {
				t.Fatalf("duplicate titles lost their identities: %+v", page.items)
			}
			rows.Rows[0].MediaData.Resources[0].URI = "changed"
			if page.items[0].MediaData.Resources[0].URI != "original" {
				t.Fatal("returned page retained mutable service data")
			}
			if len(fake.calls) != 1 || !strings.HasPrefix(fake.calls[0].action, "search-") {
				t.Fatalf("search performed an unrelated action: %+v", fake.calls)
			}
		})
	}
}

func TestPlayerDiscoveryRejectsInvalidPageRequests(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		service playerDiscoveryService
		query   string
		offset  int
		limit   int
	}{
		{"nil context", nil, playerDiscoveryRadio, "x", 0, 1},
		{"negative offset", context.Background(), playerDiscoveryRadio, "x", -1, 1},
		{"oversized offset", context.Background(), playerDiscoveryRadio, "x", 1_000_001, 1},
		{"zero limit", context.Background(), playerDiscoveryRadio, "x", 0, 0},
		{"oversized limit", context.Background(), playerDiscoveryPodcast, "x", 0, 101},
		{"blank query", context.Background(), playerDiscoveryRadio, " \t", 0, 1},
		{"unsupported service", context.Background(), playerDiscoveryUPnP, "x", 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakePlayerDiscovery{}
			if _, err := playerDiscoverySearch(tc.ctx, fake, tc.service, tc.query, tc.offset, tc.limit); err == nil {
				t.Fatal("invalid request was accepted")
			}
			if len(fake.calls) != 0 {
				t.Fatalf("invalid request reached the service: %+v", fake.calls)
			}
		})
	}

	fake := &fakePlayerDiscovery{search: func(_ playerDiscoveryService, _ string, offset, limit int) (*kefw2.RowsResponse, error) {
		if offset != 1_000_000 || limit != 100 {
			t.Fatalf("maximum valid page changed: %d %d", offset, limit)
		}
		return &kefw2.RowsResponse{RowsCount: 1_000_000}, nil
	}}
	page, err := playerDiscoverySearch(context.Background(), fake, playerDiscoveryRadio, "x", 1_000_000, 100)
	if err != nil || page.hasMore || page.nextOffset != 1_000_000 {
		t.Fatalf("maximum valid page: page=%+v err=%v", page, err)
	}
}

func TestPlayerDiscoveryBrowseUsesServicePathsAndNeverPlays(t *testing.T) {
	data := map[string][]kefw2.ContentItem{
		"show:second": {
			{Title: "Details", Type: "text"},
			{Title: "Episodes", Type: kefw2.ContentTypeContainer, Path: "opaque:series-two/episodes"},
		},
		"opaque:series-two/episodes": {
			{Title: "Episode", Type: kefw2.ContentTypeAudio, Path: "episode:first"},
			{Title: "Episode", Type: kefw2.ContentTypeAudio, Path: "episode:second"},
			{Title: "Episode", Type: kefw2.ContentTypeAudio, Path: "episode:third"},
		},
		"ui:/upnp": {
			{Title: "Search", Type: "query"},
			{Title: "Library", Type: kefw2.ContentTypeContainer, Path: "upnp:/server-a"},
			{Title: "Library", Type: kefw2.ContentTypeContainer, Path: "upnp:/server-b"},
		},
		"upnp:/server-b": {{Title: "Music", Type: kefw2.ContentTypeContainer, Path: "upnp:/server-b/music"}},
	}
	fake := &fakePlayerDiscovery{rows: func(path string, from, to int) (*kefw2.RowsResponse, error) {
		items, ok := data[path]
		if !ok {
			return nil, fmt.Errorf("unexpected path %q", path)
		}
		start := min(from, len(items))
		end := min(to+1, len(items))
		return &kefw2.RowsResponse{RowsCount: len(items), Rows: items[start:end]}, nil
	}}
	ctx := context.Background()
	show := &kefw2.ContentItem{Title: "Same", Type: kefw2.ContentTypeContainer, Path: "show:second"}
	series, err := playerDiscoveryBrowse(ctx, fake, playerDiscoveryPodcast, show, 0, 2)
	if err != nil || len(series.items) != 2 {
		t.Fatalf("browsing series: page=%+v err=%v", series, err)
	}
	episodes, err := playerDiscoveryBrowse(ctx, fake, playerDiscoveryPodcast, &series.items[1], 1, 1)
	if err != nil || len(episodes.items) != 1 || episodes.items[0].Path != "episode:second" ||
		episodes.nextOffset != 2 || !episodes.hasMore {
		t.Fatalf("browsing exact episode page: page=%+v err=%v", episodes, err)
	}
	root, err := playerDiscoveryBrowse(ctx, fake, playerDiscoveryUPnP, nil, 1, 2)
	if err != nil || len(root.items) != 2 || root.items[1].Path != "upnp:/server-b" || root.hasMore {
		t.Fatalf("browsing server page: page=%+v err=%v", root, err)
	}
	server, err := playerDiscoveryBrowse(ctx, fake, playerDiscoveryUPnP, &root.items[1], 0, 1)
	if err != nil || server.items[0].Path != "upnp:/server-b/music" {
		t.Fatalf("browsing chosen duplicate-title server: page=%+v err=%v", server, err)
	}
	want := []playerDiscoveryTestCall{
		{action: "rows", value: "show:second", first: 0, second: 1},
		{action: "rows", value: "opaque:series-two/episodes", first: 1, second: 1},
		{action: "rows", value: "ui:/upnp", first: 1, second: 2},
		{action: "rows", value: "upnp:/server-b", first: 0, second: 0},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("browse made unexpected calls: %+v", fake.calls)
	}
	for _, item := range []*kefw2.ContentItem{
		{Type: kefw2.ContentTypeAudio, Path: "upnp:/track"},
		{Type: kefw2.ContentTypeContainer, Path: "airable:/not-upnp"},
	} {
		if _, err := playerDiscoveryBrowse(ctx, fake, playerDiscoveryUPnP, item, 0, 1); err == nil {
			t.Fatalf("unsupported browse item accepted: %+v", item)
		}
	}
	if len(fake.calls) != len(want) {
		t.Fatal("invalid browse reached the service")
	}
}

func TestPlayerDiscoveryFollowsBoundedRedirects(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		fake := &fakePlayerDiscovery{
			search: func(_ playerDiscoveryService, _ string, _, _ int) (*kefw2.RowsResponse, error) {
				return &kefw2.RowsResponse{RowsRedirect: "service:redirect"}, nil
			},
			rows: func(path string, from, to int) (*kefw2.RowsResponse, error) {
				if path != "service:redirect" || from != 7 || to != 9 {
					t.Errorf("redirect changed the page: %q %d..%d", path, from, to)
				}
				return &kefw2.RowsResponse{RowsCount: 9, Rows: []kefw2.ContentItem{{Path: "one"}}}, nil
			},
		}
		page, err := playerDiscoverySearch(context.Background(), fake, playerDiscoveryPodcast, "query", 7, 3)
		if err != nil || page.nextOffset != 8 || !page.hasMore || page.items[0].Path != "one" {
			t.Fatalf("redirect page=%+v err=%v", page, err)
		}
	})
	t.Run("cycle", func(t *testing.T) {
		fake := &fakePlayerDiscovery{rows: func(_ string, _, _ int) (*kefw2.RowsResponse, error) {
			return &kefw2.RowsResponse{RowsRedirect: "same"}, nil
		}}
		item := &kefw2.ContentItem{Type: kefw2.ContentTypeContainer, Path: "same"}
		if _, err := playerDiscoveryBrowse(context.Background(), fake, playerDiscoveryPodcast, item, 0, 3); err == nil {
			t.Fatal("cyclic redirect accepted")
		}
		if len(fake.calls) != 1 {
			t.Fatalf("cycle was followed: %+v", fake.calls)
		}
	})
	t.Run("too many", func(t *testing.T) {
		count := 0
		fake := &fakePlayerDiscovery{rows: func(_ string, _, _ int) (*kefw2.RowsResponse, error) {
			count++
			return &kefw2.RowsResponse{RowsRedirect: fmt.Sprintf("next-%d", count)}, nil
		}}
		item := &kefw2.ContentItem{Type: kefw2.ContentTypeContainer, Path: "start"}
		if _, err := playerDiscoveryBrowse(context.Background(), fake, playerDiscoveryPodcast, item, 0, 3); err == nil {
			t.Fatal("unbounded redirect chain accepted")
		}
		if len(fake.calls) != 1+playerDiscoveryMaxRedirects {
			t.Fatalf("redirect count not bounded: %+v", fake.calls)
		}
	})
}

func TestPlayerDiscoveryDistinguishesLoadingErrorsAndStaleResults(t *testing.T) {
	state := playerDiscoveryBegin(playerDiscoveryState{}, playerDiscoveryRequestID{generation: 1, sequence: 1})
	if state.status != playerDiscoveryLoading || state.hasPage {
		t.Fatalf("initial loading state: %+v", state)
	}
	old := playerDiscoveryPage{query: "old s", items: []kefw2.ContentItem{
		{Title: "Duplicate", Path: "old:one"},
		{Title: "Duplicate", Path: "old:two"},
	}}
	state = playerDiscoveryFinish(state, playerDiscoveryRequestID{1, 1}, old, nil)
	selected, err := state.itemAt(1)
	if err != nil || selected.Path != "old:two" {
		t.Fatalf("selection lost exact identity: %+v %v", selected, err)
	}
	if _, err := state.itemAt(2); err == nil {
		t.Fatal("out-of-range selection accepted")
	}

	state = playerDiscoveryBegin(state, playerDiscoveryRequestID{1, 2})
	if state.status != playerDiscoveryLoading || !state.stale || state.page.query != "old s" {
		t.Fatalf("old results not labeled during load: %+v", state)
	}
	if _, err := state.itemAt(0); err == nil {
		t.Fatal("old-query row could start an action")
	}
	stale := playerDiscoveryFinish(state, playerDiscoveryRequestID{1, 1},
		playerDiscoveryPage{query: "late"}, nil)
	if !reflect.DeepEqual(stale, state) {
		t.Fatal("late query replaced the current request")
	}
	failure := errors.New("service unavailable")
	state = playerDiscoveryFinish(state, playerDiscoveryRequestID{1, 2}, playerDiscoveryPage{}, failure)
	if state.status != playerDiscoveryError || !state.stale || !errors.Is(state.err, failure) {
		t.Fatalf("failure looks like empty/success: %+v", state)
	}
	if _, err := state.itemAt(0); err == nil {
		t.Fatal("failed request allowed an old result to start an action")
	}
}

func TestPlayerDiscoveryLateSearchCannotReplaceNewQuery(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{}, 1)
	defer func() {
		select {
		case release <- struct{}{}:
		default:
		}
	}()
	slow := &fakePlayerDiscovery{search: func(_ playerDiscoveryService, _ string, _, _ int) (*kefw2.RowsResponse, error) {
		close(started)
		<-release
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{{Path: "old:station"}}}, nil
	}}
	fast := &fakePlayerDiscovery{search: func(_ playerDiscoveryService, _ string, _, _ int) (*kefw2.RowsResponse, error) {
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{{Path: "new:station"}}}, nil
	}}
	type result struct {
		page playerDiscoveryPage
		err  error
	}
	results := make(chan result, 1)
	state := playerDiscoveryBegin(playerDiscoveryState{}, playerDiscoveryRequestID{1, 1})
	go func() {
		page, err := playerDiscoverySearch(context.Background(), slow, playerDiscoveryRadio, "old", 0, 5)
		results <- result{page, err}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("old search did not start")
	}
	state = playerDiscoveryBegin(state, playerDiscoveryRequestID{1, 2})
	page, err := playerDiscoverySearch(context.Background(), fast, playerDiscoveryRadio, "new", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	state = playerDiscoveryFinish(state, playerDiscoveryRequestID{1, 2}, page, nil)
	release <- struct{}{}
	var late result
	select {
	case late = <-results:
	case <-time.After(2 * time.Second):
		t.Fatal("old search did not return")
	}
	state = playerDiscoveryFinish(state, playerDiscoveryRequestID{1, 1}, late.page, late.err)
	item, err := state.itemAt(0)
	if err != nil || state.page.query != "new" || item.Path != "new:station" {
		t.Fatalf("late search changed the selected result: item=%+v state=%+v err=%v", item, state, err)
	}
}

func TestPlayerDiscoveryDistinguishesEmptyAndEnd(t *testing.T) {
	state := playerDiscoveryBegin(playerDiscoveryState{}, playerDiscoveryRequestID{1, 1})
	state = playerDiscoveryFinish(state, playerDiscoveryRequestID{1, 1}, playerDiscoveryPage{offset: 0}, nil)
	if state.status != playerDiscoveryEmpty || state.stale || state.err != nil {
		t.Fatalf("empty search state: %+v", state)
	}
	state = playerDiscoveryBegin(state, playerDiscoveryRequestID{1, 2})
	state = playerDiscoveryFinish(state, playerDiscoveryRequestID{1, 2}, playerDiscoveryPage{offset: 4}, nil)
	if state.status != playerDiscoveryEnd {
		t.Fatalf("end-of-results state: %+v", state)
	}
}

func TestPlayerDiscoveryRejectsRetiredSpeakerResults(t *testing.T) {
	old := playerDiscoveryPage{query: "old", items: []kefw2.ContentItem{{Path: "old:one"}}}
	state := playerDiscoveryBegin(playerDiscoveryState{}, playerDiscoveryRequestID{1, 1})
	state = playerDiscoveryFinish(state, playerDiscoveryRequestID{1, 1}, old, nil)
	state = playerDiscoveryBegin(state, playerDiscoveryRequestID{1, 2})
	state = playerDiscoveryBegin(state, playerDiscoveryRequestID{2, 1})
	if state.status != playerDiscoveryLoading || state.hasPage || state.stale {
		t.Fatalf("speaker switch retained an old page: %+v", state)
	}
	if got := playerDiscoveryFinish(state, playerDiscoveryRequestID{1, 2}, old, nil); !reflect.DeepEqual(got, state) {
		t.Fatal("retired speaker response was accepted")
	}
	if got := playerDiscoveryBegin(state, playerDiscoveryRequestID{1, 3}); !reflect.DeepEqual(got, state) {
		t.Fatal("retired speaker request was restarted")
	}
	state = playerDiscoveryFinish(state, playerDiscoveryRequestID{2, 1}, old, nil)
	state = playerDiscoveryBegin(state, playerDiscoveryRequestID{3, 1})
	if state.hasPage || state.stale {
		t.Fatal("returning to a previous speaker reused its old generation")
	}
}

func playerDiscoveryTestPendingEpisodes(t *testing.T) (playerDiscoveryState, []playerDiscoveryLocation) {
	t.Helper()
	state := playerDiscoveryBegin(playerDiscoveryState{}, playerDiscoveryRequestID{3, 1})
	search := playerDiscoveryPage{
		kind: playerDiscoverySearchPage, service: playerDiscoveryPodcast, query: "same",
		items: []kefw2.ContentItem{
			{Title: "Same", Type: kefw2.ContentTypeContainer, Path: "show:1"},
			{Title: "Same", Type: kefw2.ContentTypeContainer, Path: "show:2"},
		},
	}
	state = playerDiscoveryFinish(state, playerDiscoveryRequestID{3, 1}, search, nil)
	history, err := playerDiscoveryRemember(state, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	search.items[1].Path = "changed:outside"

	state = playerDiscoveryBegin(state, playerDiscoveryRequestID{3, 2})
	series := playerDiscoveryPage{
		kind: playerDiscoveryBrowsePage, service: playerDiscoveryPodcast, path: "show:2",
		items: []kefw2.ContentItem{
			{Title: "Description", Type: "text"},
			{Title: "Episodes", Type: kefw2.ContentTypeContainer, Path: "show:2/episodes"},
		},
	}
	state = playerDiscoveryFinish(state, playerDiscoveryRequestID{3, 2}, series, nil)
	history, err = playerDiscoveryRemember(state, history, 1)
	if err != nil {
		t.Fatal(err)
	}
	state = playerDiscoveryBegin(state, playerDiscoveryRequestID{3, 3})
	return state, history
}

func TestPlayerDiscoveryBackRejectsLateEpisodes(t *testing.T) {
	state, history := playerDiscoveryTestPendingEpisodes(t)
	restored, unchanged, _, ok := playerDiscoveryBack(state, history, playerDiscoveryRequestID{3, 3})
	if ok || !reflect.DeepEqual(restored, state) || len(unchanged) != 2 {
		t.Fatal("back accepted a reused request ID")
	}
	state, history, selection, ok := playerDiscoveryBack(state, history, playerDiscoveryRequestID{3, 4})
	if !ok || selection != 1 || state.page.path != "show:2" || len(history) != 1 {
		t.Fatalf("series selection not restored: state=%+v selection=%d history=%+v", state, selection, history)
	}
	if item, err := state.itemAt(selection); err != nil || item.Path != "show:2/episodes" {
		t.Fatalf("wrong series row restored: item=%+v err=%v", item, err)
	}
	late := playerDiscoveryPage{path: "show:2/episodes", items: []kefw2.ContentItem{{Path: "late"}}}
	if got := playerDiscoveryFinish(state, playerDiscoveryRequestID{3, 3}, late, nil); !reflect.DeepEqual(got, state) {
		t.Fatal("late child page replaced restored series")
	}
}

func TestPlayerDiscoveryBackRestoresSearchSelection(t *testing.T) {
	state, history := playerDiscoveryTestPendingEpisodes(t)
	state, history, _, ok := playerDiscoveryBack(state, history, playerDiscoveryRequestID{3, 4})
	if !ok {
		t.Fatal("could not return to series")
	}
	state, history, selection, ok := playerDiscoveryBack(state, history, playerDiscoveryRequestID{3, 5})
	if !ok || selection != 1 || state.page.query != "same" || len(history) != 0 {
		t.Fatalf("search selection not restored: state=%+v selection=%d", state, selection)
	}
	if item, err := state.itemAt(selection); err != nil || item.Path != "show:2" {
		t.Fatalf("back selected another duplicate title: item=%+v err=%v", item, err)
	}
}

func TestPlayerDiscoveryBackRejectsRetiredHistory(t *testing.T) {
	state, history := playerDiscoveryTestPendingEpisodes(t)
	state = playerDiscoveryBegin(state, playerDiscoveryRequestID{4, 1})
	state, history, _, ok := playerDiscoveryBack(state, history, playerDiscoveryRequestID{4, 2})
	if ok || len(history) != 0 || state.hasPage {
		t.Fatal("back reused another speaker generation's history")
	}

	audio := playerDiscoveryPage{items: []kefw2.ContentItem{{Type: kefw2.ContentTypeAudio, Path: "episode"}}}
	state = playerDiscoveryFinish(state, playerDiscoveryRequestID{4, 1}, audio, nil)
	if _, err := playerDiscoveryRemember(state, nil, 0); err == nil {
		t.Fatal("an episode was treated as a browsable container")
	}
}

func TestPlayerDiscoveryRejectsIncompletePages(t *testing.T) {
	failure := errors.New("offline")
	for _, tc := range []struct {
		name string
		rows *kefw2.RowsResponse
		err  error
	}{
		{"unavailable", nil, failure},
		{"nil response", nil, nil},
		{"missing rows before end", &kefw2.RowsResponse{RowsCount: 2}, nil},
		{"too many rows", &kefw2.RowsResponse{RowsCount: 2, Rows: []kefw2.ContentItem{{}, {}}}, nil},
		{"inconsistent total", &kefw2.RowsResponse{Rows: []kefw2.ContentItem{{}}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakePlayerDiscovery{search: func(_ playerDiscoveryService, _ string, _, _ int) (*kefw2.RowsResponse, error) {
				return tc.rows, tc.err
			}}
			_, err := playerDiscoverySearch(context.Background(), fake, playerDiscoveryRadio, "test", 0, 1)
			if err == nil || tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("incomplete page treated as success: %v", err)
			}
		})
	}
}

func TestPlayerDiscoveryFavoritesPagingAndMutations(t *testing.T) {
	fake := &fakePlayerDiscovery{favorites: func(_ playerDiscoveryService, offset, limit int) (*kefw2.RowsResponse, error) {
		if offset != 50 || limit != 5 {
			t.Errorf("favorites page changed: %d %d", offset, limit)
		}
		return &kefw2.RowsResponse{RowsCount: 60, Rows: []kefw2.ContentItem{{Path: "fav:51"}}}, nil
	}}
	for _, service := range []playerDiscoveryService{playerDiscoveryRadio, playerDiscoveryPodcast} {
		page, err := playerDiscoveryFavorites(context.Background(), fake, service, 50, 5)
		if err != nil || page.nextOffset != 51 || !page.hasMore || page.items[0].Path != "fav:51" {
			t.Fatalf("%s favorites page=%+v err=%v", service, page, err)
		}
	}

	radio := kefw2.ContentItem{Title: "Same", Type: kefw2.ContentTypeAudio, Path: "station:2", ID: "radio/id:2"}
	show := kefw2.ContentItem{Title: "Same", Type: kefw2.ContentTypeContainer, Path: "show:2", ID: "podcast/id:2"}
	for _, tc := range []struct {
		service playerDiscoveryService
		item    kefw2.ContentItem
		add     bool
		action  string
	}{
		{playerDiscoveryRadio, radio, true, "add-radio"},
		{playerDiscoveryRadio, radio, false, "remove-radio"},
		{playerDiscoveryPodcast, show, true, "add-podcast"},
		{playerDiscoveryPodcast, show, false, "remove-podcast"},
	} {
		if err := playerDiscoveryFavorite(context.Background(), fake, tc.service, tc.item, tc.add); err != nil {
			t.Fatal(err)
		}
		call := fake.calls[len(fake.calls)-1]
		if call.action != tc.action || call.item.Path != tc.item.Path || call.item.ID != tc.item.ID {
			t.Fatalf("favorite used a different identity: %+v", call)
		}
	}
	failure := errors.New("favorite failed")
	fake.favoriteErr = failure
	if err := playerDiscoveryFavorite(context.Background(), fake, playerDiscoveryRadio, radio, true); !errors.Is(err, failure) {
		t.Fatalf("favorite failure lost: %v", err)
	}
	calls := len(fake.calls)
	for _, tc := range []struct {
		service playerDiscoveryService
		item    kefw2.ContentItem
	}{
		{playerDiscoveryPodcast, kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: "episode", ID: "id"}},
		{playerDiscoveryRadio, kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: "station"}},
		{playerDiscoveryUPnP, show},
	} {
		if err := playerDiscoveryFavorite(context.Background(), fake, tc.service, tc.item, true); err == nil {
			t.Fatalf("unsupported favorite accepted: %+v", tc)
		}
	}
	if len(fake.calls) != calls {
		t.Fatal("invalid favorite reached the service")
	}
}

func TestPlayerDiscoveryExactPlaybackAndWiFi(t *testing.T) {
	for _, service := range []playerDiscoveryService{playerDiscoveryRadio, playerDiscoveryPodcast} {
		t.Run(string(service), func(t *testing.T) {
			item := kefw2.ContentItem{Title: "Duplicate", Type: kefw2.ContentTypeAudio, Path: "chosen:2"}
			otherClient := &fakePlayerDiscovery{}
			otherSource := &fakePlayerDiscoverySource{current: kefw2.SourceBluetooth}
			source := &fakePlayerDiscoverySource{current: kefw2.SourceBluetooth}
			fake := &fakePlayerDiscovery{
				radioDetails: func(path string) (*kefw2.ContentItem, error) {
					if path != item.Path {
						t.Errorf("resolved wrong station: %q", path)
					}
					resolved := item
					return &resolved, nil
				},
				podcastDetails: func(path string) (*kefw2.ContentItem, error) {
					if path != item.Path {
						t.Errorf("resolved wrong episode: %q", path)
					}
					resolved := item
					return &resolved, nil
				},
				onPlay: func(_ string, played *kefw2.ContentItem) error {
					if source.current != kefw2.SourceWiFi || played.Path != item.Path {
						t.Errorf("played wrong target/item: source=%s item=%+v", source.current, played)
					}
					return nil
				},
			}
			var waits []time.Duration
			wait := func(ctx context.Context, delay time.Duration) error {
				waits = append(waits, delay)
				return ctx.Err()
			}
			if err := playerDiscoveryPlayWithWait(context.Background(), fake, source, service, item, wait); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(source.calls, []string{"source", "set:wifi", "source", "source"}) ||
				!reflect.DeepEqual(waits, []time.Duration{500 * time.Millisecond, time.Second}) {
				t.Fatalf("WiFi was not confirmed and settled: calls=%v waits=%v", source.calls, waits)
			}
			if len(fake.calls) != 2 || fake.calls[1].item.Path != "chosen:2" ||
				len(otherClient.calls) != 0 || len(otherSource.calls) != 0 {
				t.Fatalf("playback crossed targets or identities: %+v", fake.calls)
			}
		})
	}
}

func TestPlayerDiscoveryDoesNotPlayAfterFailedOrCanceledSource(t *testing.T) {
	failure := errors.New("source failed")
	for _, tc := range []struct {
		name string
		set  func(context.CancelFunc, *fakePlayerDiscoverySource)
		want error
	}{
		{"read failed", func(_ context.CancelFunc, s *fakePlayerDiscoverySource) { s.sourceErr = failure }, failure},
		{"switch failed", func(_ context.CancelFunc, s *fakePlayerDiscoverySource) { s.setErr = failure }, failure},
		{"canceled during switch", func(cancel context.CancelFunc, s *fakePlayerDiscoverySource) {
			s.onSet = func(context.Context, kefw2.Source) error { cancel(); return nil }
		}, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			source := &fakePlayerDiscoverySource{current: kefw2.SourceBluetooth}
			tc.set(cancel, source)
			item := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: "episode:2"}
			fake := &fakePlayerDiscovery{podcastDetails: func(string) (*kefw2.ContentItem, error) {
				return &item, nil
			}}
			err := playerDiscoveryPlay(ctx, fake, source, playerDiscoveryPodcast, item)
			if !errors.Is(err, tc.want) {
				t.Fatalf("lost source failure/cancellation: %v", err)
			}
			if len(fake.calls) != 1 || fake.calls[0].action != "resolve-podcast" {
				t.Fatalf("played after source failure/cancellation: %+v", fake.calls)
			}
		})
	}

	t.Run("canceled while waiting", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		source := &fakePlayerDiscoverySource{current: kefw2.SourceBluetooth}
		source.onSet = func(context.Context, kefw2.Source) error { return nil }
		item := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: "station:2"}
		fake := &fakePlayerDiscovery{radioDetails: func(string) (*kefw2.ContentItem, error) { return &item, nil }}
		err := playerDiscoveryPlay(ctx, fake, source, playerDiscoveryRadio, item)
		if !errors.Is(err, context.DeadlineExceeded) || len(fake.calls) != 1 {
			t.Fatalf("played after WiFi wait cancellation: calls=%+v err=%v", fake.calls, err)
		}
	})
}

func TestPlayerDiscoveryRejectsWrongOrUnsupportedPlayback(t *testing.T) {
	fake := &fakePlayerDiscovery{podcastDetails: func(string) (*kefw2.ContentItem, error) {
		return &kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: "other-episode"}, nil
	}}
	source := &fakePlayerDiscoverySource{current: kefw2.SourceWiFi}
	selected := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: "selected-episode"}
	if err := playerDiscoveryPlay(context.Background(), fake, source, playerDiscoveryPodcast, selected); err == nil {
		t.Fatal("mismatched podcast resolution was played")
	}
	if len(fake.calls) != 1 || len(source.calls) != 0 {
		t.Fatalf("mismatch touched playback or source: client=%+v source=%v", fake.calls, source.calls)
	}
	for _, tc := range []struct {
		service playerDiscoveryService
		item    kefw2.ContentItem
	}{
		{playerDiscoveryPodcast, kefw2.ContentItem{Type: kefw2.ContentTypeContainer, Path: "series"}},
		{playerDiscoveryRadio, kefw2.ContentItem{Type: "query", Path: "query"}},
		{playerDiscoveryUPnP, selected},
		{playerDiscoveryPodcast, kefw2.ContentItem{Type: kefw2.ContentTypeAudio}},
	} {
		if err := playerDiscoveryPlay(context.Background(), fake, source, tc.service, tc.item); err == nil {
			t.Fatalf("unsupported playback accepted: %+v", tc)
		}
	}
	if len(fake.calls) != 1 || len(source.calls) != 0 {
		t.Fatalf("invalid playback touched the speaker: client=%+v source=%v", fake.calls, source.calls)
	}
}

func TestPlayerDiscoverySpeakerSwitchCancelsOldPlayback(t *testing.T) {
	oldCtx, cancelOld := context.WithCancel(context.Background())
	started := make(chan struct{})
	oldItem := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: "old:episode"}
	oldClient := &fakePlayerDiscovery{podcastDetails: func(string) (*kefw2.ContentItem, error) {
		close(started)
		<-oldCtx.Done()
		return &oldItem, nil
	}}
	oldSource := &fakePlayerDiscoverySource{current: kefw2.SourceWiFi}
	result := make(chan error, 1)
	go func() {
		result <- playerDiscoveryPlay(oldCtx, oldClient, oldSource, playerDiscoveryPodcast, oldItem)
	}()
	<-started
	cancelOld()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("old speaker operation did not cancel: %v", err)
	}
	if len(oldClient.calls) != 1 || len(oldSource.calls) != 0 {
		t.Fatalf("old speaker continued after switch: client=%+v source=%v", oldClient.calls, oldSource.calls)
	}

	newItem := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: "new:episode"}
	newClient := &fakePlayerDiscovery{podcastDetails: func(string) (*kefw2.ContentItem, error) {
		return &newItem, nil
	}}
	newSource := &fakePlayerDiscoverySource{current: kefw2.SourceWiFi}
	if err := playerDiscoveryPlay(context.Background(), newClient, newSource, playerDiscoveryPodcast, newItem); err != nil {
		t.Fatal(err)
	}
	if len(newClient.calls) != 2 || newClient.calls[1].item.Path != "new:episode" || len(oldClient.calls) != 1 {
		t.Fatalf("new selection targeted the wrong speaker: old=%+v new=%+v", oldClient.calls, newClient.calls)
	}
}

func TestPlayerDiscoveryEnqueuesOnlyUPnPTracksWithoutPlaying(t *testing.T) {
	fake := &fakePlayerDiscovery{}
	item := kefw2.ContentItem{Title: "Same", Type: kefw2.ContentTypeAudio, Path: "upnp:/server-b/track"}
	if err := playerDiscoveryEnqueue(context.Background(), fake, playerDiscoveryUPnP, item); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 || fake.calls[0].action != "enqueue" || fake.calls[0].start ||
		fake.calls[0].item.Path != item.Path {
		t.Fatalf("enqueue changed playback or selected a different track: %+v", fake.calls)
	}
	for _, tc := range []struct {
		service playerDiscoveryService
		item    kefw2.ContentItem
	}{
		{playerDiscoveryUPnP, kefw2.ContentItem{Type: kefw2.ContentTypeContainer, Path: "upnp:/folder"}},
		{playerDiscoveryUPnP, kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: "airable:/episode"}},
		{playerDiscoveryUPnP, kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: "upnp:/"}},
		{playerDiscoveryUPnP, kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: "upnp:/  "}},
		{playerDiscoveryPodcast, item},
	} {
		if err := playerDiscoveryEnqueue(context.Background(), fake, tc.service, tc.item); err == nil {
			t.Fatalf("unsupported enqueue accepted: %+v", tc)
		}
	}
	if len(fake.calls) != 1 {
		t.Fatalf("invalid enqueue reached the speaker: %+v", fake.calls)
	}
	failure := errors.New("queue unavailable")
	fake.queueErr = failure
	if err := playerDiscoveryEnqueue(context.Background(), fake, playerDiscoveryUPnP, item); !errors.Is(err, failure) {
		t.Fatalf("queue failure lost: %v", err)
	}
}

func TestPlayerDiscoveryReportsCanceledRequestsWithoutReplay(t *testing.T) {
	t.Run("search", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		fake := &fakePlayerDiscovery{search: func(_ playerDiscoveryService, _ string, _, _ int) (*kefw2.RowsResponse, error) {
			cancel()
			return &kefw2.RowsResponse{RowsRedirect: "must-not-follow"}, nil
		}}
		_, err := playerDiscoverySearch(ctx, fake, playerDiscoveryRadio, "query", 0, 5)
		if !errors.Is(err, context.Canceled) || len(fake.calls) != 1 {
			t.Fatalf("canceled search continued: calls=%+v err=%v", fake.calls, err)
		}
	})
	t.Run("favorite", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		fake := &fakePlayerDiscovery{onFavorite: func() error { cancel(); return nil }}
		item := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: "station", ID: "station-id"}
		err := playerDiscoveryFavorite(ctx, fake, playerDiscoveryRadio, item, true)
		if !errors.Is(err, context.Canceled) || len(fake.calls) != 1 {
			t.Fatalf("canceled favorite looked confirmed or replayed: calls=%+v err=%v", fake.calls, err)
		}
	})
	t.Run("enqueue", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		fake := &fakePlayerDiscovery{onQueue: func() error { cancel(); return nil }}
		item := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: "upnp:/track"}
		err := playerDiscoveryEnqueue(ctx, fake, playerDiscoveryUPnP, item)
		if !errors.Is(err, context.Canceled) || len(fake.calls) != 1 {
			t.Fatalf("canceled enqueue looked confirmed or replayed: calls=%+v err=%v", fake.calls, err)
		}
	})
	t.Run("play", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		item := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: "episode"}
		fake := &fakePlayerDiscovery{
			podcastDetails: func(string) (*kefw2.ContentItem, error) { return &item, nil },
			onPlay:         func(string, *kefw2.ContentItem) error { cancel(); return nil },
		}
		source := &fakePlayerDiscoverySource{current: kefw2.SourceWiFi}
		err := playerDiscoveryPlay(ctx, fake, source, playerDiscoveryPodcast, item)
		if !errors.Is(err, context.Canceled) || len(fake.calls) != 2 {
			t.Fatalf("canceled playback looked confirmed or replayed: calls=%+v err=%v", fake.calls, err)
		}
	})
}

func TestPlayerDiscoveryPodcastPlaybackSendsNoExplicitClear(t *testing.T) {
	var mu sync.Mutex
	var posts []struct {
		Path  string `json:"path"`
		Value struct {
			MediaRoles struct {
				Path string `json:"path"`
			} `json:"mediaRoles"`
		} `json:"value"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/getRows":
			if r.URL.Query().Get("path") != "episode:exact" {
				t.Errorf("resolved a different podcast path: %s", r.URL)
			}
			_ = json.NewEncoder(w).Encode(kefw2.RowsResponse{
				Roles: &kefw2.ContentItem{Path: "episode:exact", Type: kefw2.ContentTypeAudio},
			})
		case "/api/getData":
			_, _ = fmt.Fprint(w, `[{"type":"kefPhysicalSource","kefPhysicalSource":"wifi"}]`)
		case "/api/setData":
			var payload struct {
				Path  string `json:"path"`
				Value struct {
					MediaRoles struct {
						Path string `json:"path"`
					} `json:"mediaRoles"`
				} `json:"value"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode playback request: %v", err)
			}
			mu.Lock()
			posts = append(posts, payload)
			mu.Unlock()
			_, _ = fmt.Fprint(w, `{}`)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	speaker := &kefw2.KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")}
	client := playerDiscoveryTestPlayClient(t, kefw2.NewAirableClient(speaker, kefw2.WithContext(ctx)))
	item := kefw2.ContentItem{Path: "episode:exact", Type: kefw2.ContentTypeAudio}
	if err := playerDiscoveryPlay(ctx, client, speaker, playerDiscoveryPodcast, item); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(posts) != 1 || posts[0].Path != "player:player/control" || posts[0].Value.MediaRoles.Path != item.Path {
		t.Fatalf("podcast playback cleared queue or selected another item: %+v", posts)
	}
}

func TestPlayerDiscoveryCanceledSourceSwitchSendsNoPlayHTTP(t *testing.T) {
	for _, service := range []playerDiscoveryService{playerDiscoveryRadio, playerDiscoveryPodcast} {
		t.Run(string(service), func(t *testing.T) {
			started := make(chan struct{}, 1)
			release := make(chan struct{})
			var mu sync.Mutex
			var posts []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/getRows":
					_ = json.NewEncoder(w).Encode(kefw2.RowsResponse{
						Roles: &kefw2.ContentItem{
							Path: "chosen", Type: kefw2.ContentTypeAudio, AudioType: "audioBroadcast",
						},
					})
				case "/api/getData":
					_, _ = fmt.Fprint(w, `[{"type":"kefPhysicalSource","kefPhysicalSource":"bluetooth"}]`)
				case "/api/setData":
					var payload struct {
						Path string `json:"path"`
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Errorf("decode source request: %v", err)
					}
					mu.Lock()
					posts = append(posts, payload.Path)
					mu.Unlock()
					if payload.Path == "settings:/kef/play/physicalSource" {
						started <- struct{}{}
						select {
						case <-r.Context().Done():
						case <-release:
						}
					}
					_, _ = fmt.Fprint(w, `{}`)
				default:
					http.Error(w, "unexpected request", http.StatusNotFound)
				}
			}))
			defer server.Close()
			defer close(release)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			speaker := &kefw2.KEFSpeaker{IPAddress: strings.TrimPrefix(server.URL, "http://")}
			client := playerDiscoveryTestPlayClient(t, kefw2.NewAirableClient(speaker, kefw2.WithContext(ctx)))
			item := kefw2.ContentItem{Path: "chosen", Type: kefw2.ContentTypeAudio}
			result := make(chan error, 1)
			go func() { result <- playerDiscoveryPlay(ctx, client, speaker, service, item) }()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("source switch did not start")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("source switch did not report cancellation: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("source switch did not stop")
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(posts, []string{"settings:/kef/play/physicalSource"}) {
				t.Fatalf("canceled source switch sent playback or queue commands: %v", posts)
			}
		})
	}
}
