// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hilli/go-kef-w2/kefw2"
)

type playerDiscoveryUITestCall struct {
	target        playerSpeaker
	name, value   string
	offset, limit int
	item          kefw2.ContentItem
	start         bool
}

type playerDiscoveryUITestBackend struct {
	mu       sync.Mutex
	recorded []playerDiscoveryUITestCall
	search   func(context.Context, playerSpeaker, playerDiscoveryService, string, int, int) (*kefw2.RowsResponse, error)
	rows     func(context.Context, playerSpeaker, string, int, int) (*kefw2.RowsResponse, error)
	favorite func(context.Context, playerSpeaker, playerDiscoveryService, int, int) (*kefw2.RowsResponse, error)
	resolve  func(context.Context, playerSpeaker, playerDiscoveryService, string) (*kefw2.ContentItem, error)
	mutate   func(context.Context, playerSpeaker, string, kefw2.ContentItem) error
	source   func(context.Context, playerSpeaker) (kefw2.Source, error)
	set      func(context.Context, playerSpeaker, kefw2.Source) error
}

func (backend *playerDiscoveryUITestBackend) record(call playerDiscoveryUITestCall) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	call.item = playerDiscoveryCopyItem(call.item)
	backend.recorded = append(backend.recorded, call)
}

func (backend *playerDiscoveryUITestBackend) calls() []playerDiscoveryUITestCall {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return append([]playerDiscoveryUITestCall(nil), backend.recorded...)
}

func (backend *playerDiscoveryUITestBackend) factory(
	ctx context.Context, target playerSpeaker,
) (playerDiscoveryUIClient, playerDiscoverySource) {
	client := &playerDiscoveryUITestClient{ctx: ctx, target: target, backend: backend}
	return client, client
}

type playerDiscoveryUITestClient struct {
	ctx     context.Context
	target  playerSpeaker
	backend *playerDiscoveryUITestBackend
}

func (client *playerDiscoveryUITestClient) search(
	service playerDiscoveryService, query string, offset, limit int,
) (*kefw2.RowsResponse, error) {
	client.backend.record(playerDiscoveryUITestCall{
		target: client.target, name: "search-" + string(service), value: query, offset: offset, limit: limit,
	})
	if client.backend.search == nil {
		return nil, errors.New("unexpected search")
	}
	return client.backend.search(client.ctx, client.target, service, query, offset, limit)
}

func (client *playerDiscoveryUITestClient) SearchRadioPage(query string, offset, limit int) (*kefw2.RowsResponse, error) {
	return client.search(playerDiscoveryRadio, query, offset, limit)
}

func (client *playerDiscoveryUITestClient) SearchPodcastsPage(
	query string, offset, limit int,
) (*kefw2.RowsResponse, error) {
	return client.search(playerDiscoveryPodcast, query, offset, limit)
}

func (client *playerDiscoveryUITestClient) GetRows(path string, from, to int) (*kefw2.RowsResponse, error) {
	client.backend.record(playerDiscoveryUITestCall{
		target: client.target, name: "rows", value: path, offset: from, limit: to - from + 1,
	})
	if client.backend.rows == nil {
		return nil, errors.New("unexpected browse")
	}
	return client.backend.rows(client.ctx, client.target, path, from, to)
}

func (client *playerDiscoveryUITestClient) favorites(
	service playerDiscoveryService, offset, limit int,
) (*kefw2.RowsResponse, error) {
	client.backend.record(playerDiscoveryUITestCall{
		target: client.target, name: "favorites-" + string(service), offset: offset, limit: limit,
	})
	if client.backend.favorite == nil {
		return nil, errors.New("unexpected favorites")
	}
	return client.backend.favorite(client.ctx, client.target, service, offset, limit)
}

func (client *playerDiscoveryUITestClient) GetRadioFavoritesPage(offset, limit int) (*kefw2.RowsResponse, error) {
	return client.favorites(playerDiscoveryRadio, offset, limit)
}

func (client *playerDiscoveryUITestClient) GetPodcastFavoritesPage(offset, limit int) (*kefw2.RowsResponse, error) {
	return client.favorites(playerDiscoveryPodcast, offset, limit)
}

func (client *playerDiscoveryUITestClient) detail(
	service playerDiscoveryService, path string,
) (*kefw2.ContentItem, error) {
	client.backend.record(playerDiscoveryUITestCall{
		target: client.target, name: "resolve-" + string(service), value: path,
	})
	if client.backend.resolve != nil {
		return client.backend.resolve(client.ctx, client.target, service, path)
	}
	return &kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: path}, nil
}

func (client *playerDiscoveryUITestClient) GetRadioStationByPath(path string) (*kefw2.ContentItem, error) {
	return client.detail(playerDiscoveryRadio, path)
}

func (client *playerDiscoveryUITestClient) GetPodcastDetailsExact(path string) (*kefw2.ContentItem, error) {
	return client.detail(playerDiscoveryPodcast, path)
}

func (client *playerDiscoveryUITestClient) mutation(name string, item kefw2.ContentItem, start bool) error {
	client.backend.record(playerDiscoveryUITestCall{target: client.target, name: name, item: item, start: start})
	if client.backend.mutate != nil {
		return client.backend.mutate(client.ctx, client.target, name, item)
	}
	return nil
}

func (client *playerDiscoveryUITestClient) PlayRadioStation(item *kefw2.ContentItem) error {
	return client.mutation("play-radio", *item, false)
}

func (client *playerDiscoveryUITestClient) RequestPodcastEpisodeExact(item *kefw2.ContentItem) error {
	return client.mutation("play-podcast", *item, false)
}

func (client *playerDiscoveryUITestClient) AddRadioFavorite(item *kefw2.ContentItem) error {
	return client.mutation("add-radio", *item, false)
}

func (client *playerDiscoveryUITestClient) RemoveRadioFavorite(item *kefw2.ContentItem) error {
	return client.mutation("remove-radio", *item, false)
}

func (client *playerDiscoveryUITestClient) AddPodcastFavorite(item *kefw2.ContentItem) error {
	return client.mutation("add-podcast", *item, false)
}

func (client *playerDiscoveryUITestClient) RemovePodcastFavorite(item *kefw2.ContentItem) error {
	return client.mutation("remove-podcast", *item, false)
}

func (client *playerDiscoveryUITestClient) AddToQueue(items []kefw2.ContentItem, start bool) error {
	if len(items) != 1 {
		return fmt.Errorf("expected one queue item, got %d", len(items))
	}
	return client.mutation("enqueue", items[0], start)
}

func (client *playerDiscoveryUITestClient) Source(ctx context.Context) (kefw2.Source, error) {
	client.backend.record(playerDiscoveryUITestCall{target: client.target, name: "source"})
	if client.backend.source != nil {
		return client.backend.source(ctx, client.target)
	}
	return kefw2.SourceWiFi, nil
}

func (client *playerDiscoveryUITestClient) SetSource(ctx context.Context, source kefw2.Source) error {
	client.backend.record(playerDiscoveryUITestCall{target: client.target, name: "set-source", value: string(source)})
	if client.backend.set != nil {
		return client.backend.set(ctx, client.target, source)
	}
	return nil
}

func playerDiscoveryUITestModel(
	t *testing.T, backend *playerDiscoveryUITestBackend,
) (*playerModel, *playerDiscoveryUI, playerSpeaker, playerSpeaker) {
	t.Helper()
	model, ui, a, b := playerDiscoveryUITestConnectingModel(t, backend)
	playerDiscoveryUITestReady(model)
	return model, ui, a, b
}

func playerDiscoveryUITestConnectingModel(
	t *testing.T, backend *playerDiscoveryUITestBackend,
) (*playerModel, *playerDiscoveryUI, playerSpeaker, playerSpeaker) {
	t.Helper()
	a := playerSpeaker{Name: "Living", IPAddress: "10.0.0.1"}
	b := playerSpeaker{Name: "Bedroom", IPAddress: "10.0.0.2"}
	selection := playerSelection{configured: []playerSpeaker{a, b}, selected: &a}
	sessions := &fakePlayerFactory{autoFinish: true}
	model := newPlayerModel(context.Background(), playerDiscoveryBrowser, selection, sessions.newSession)
	ui := newPlayerDiscoveryUI()
	ui.factory = backend.factory
	model.addExtension(ui.extension())
	model.Init()
	t.Cleanup(func() {
		if err := model.shutdown(); err != nil {
			t.Error(err)
		}
	})
	return model, ui, a, b
}

func playerDiscoveryUITestReady(model *playerModel) {
	model.Update(playerScopedMsg{generation: model.generation, message: watchUpdate{kind: watchSnapshotUpdate}})
}

func playerDiscoveryUITestUnavailable(model *playerModel) {
	model.Update(playerScopedMsg{generation: model.generation, message: watchUpdate{
		kind: playerStatusUnavailableUpdate, err: errors.New("connection lost"),
	}})
}

func playerDiscoveryUITestSend(model *playerModel, msg tea.Msg) tea.Cmd {
	_, command := model.Update(msg)
	return command
}

func playerDiscoveryUITestComplete(t *testing.T, model *playerModel, command tea.Cmd) tea.Cmd {
	t.Helper()
	if command == nil {
		t.Fatal("expected a discovery command")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- command() }()
	select {
	case msg := <-result:
		if msg == nil {
			t.Fatal("discovery command returned no message")
		}
		_, next := model.Update(msg)
		return next
	case <-time.After(5 * time.Second):
		t.Fatal("discovery command did not finish")
		return nil
	}
}

func TestPlayerDiscoveryUIBrowseRootPagingAndReturn(t *testing.T) {
	for _, service := range []playerDiscoveryService{playerDiscoveryRadio, playerDiscoveryPodcast} {
		t.Run(string(service), func(t *testing.T) {
			root := "ui:/airableradios"
			if service == playerDiscoveryPodcast {
				root = "airable:linkService_airable.feeds"
			}
			backend := &playerDiscoveryUITestBackend{
				rows: func(_ context.Context, _ playerSpeaker, path string, from, to int) (*kefw2.RowsResponse, error) {
					switch path {
					case root:
						return &kefw2.RowsResponse{RowsRedirect: "opaque:/menu"}, nil
					case "opaque:/menu":
						count := min(to+1, 25) - from
						items := make([]kefw2.ContentItem, count)
						for i := range items {
							items[i] = kefw2.ContentItem{
								Title: fmt.Sprintf("Category %d", from+i), Type: kefw2.ContentTypeContainer, Path: "opaque:/child",
							}
						}
						return &kefw2.RowsResponse{RowsCount: 25, Rows: items}, nil
					case "opaque:/child":
						return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{{
							Title: "Nested result", Type: kefw2.ContentTypeAudio, Path: "opaque:/audio",
						}}}, nil
					default:
						return nil, fmt.Errorf("unexpected path %q", path)
					}
				},
			}
			model, ui, _, _ := playerDiscoveryUITestConnectingModel(t, backend)
			open := playerDiscoveryServiceMsg{generation: model.generation, service: service, browseRoot: true}
			playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model, open))
			p := ui.presentation()
			if p.state.status != playerDiscoveryReady || !p.state.page.hasMore || p.state.page.path != root {
				t.Fatalf("root redirect not loaded: %+v", p.state)
			}
			playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model, playerDiscoveryPageMsg{
				generation: p.generation, request: p.state.request, offset: p.state.page.nextOffset,
			}))
			p = ui.presentation()
			if len(p.state.page.items) != 1 || p.state.page.items[0].Title != "Category 24" || !p.hasPrevious {
				t.Fatalf("root pagination failed: %+v", p)
			}
			playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model, playerDiscoveryBrowseMsg{
				generation: p.generation, request: p.state.request, index: 0,
			}))
			if p = ui.presentation(); p.state.page.path != "opaque:/child" || len(ui.history) == 0 {
				t.Fatalf("category did not open: %+v", p)
			}
			playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model, open))
			p = ui.presentation()
			if p.state.status != playerDiscoveryReady || p.state.page.path != root ||
				p.state.page.offset != 0 || p.hasPrevious || len(ui.history) != 0 {
				t.Fatalf("selecting the same service did not return to its root: %+v", p)
			}
			playerDiscoveryUITestNoWrites(t, backend.calls())
		})
	}
}

func TestPlayerDiscoveryUIBrowseRootFailureCanRetry(t *testing.T) {
	var attempts atomic.Int32
	backend := &playerDiscoveryUITestBackend{
		rows: func(_ context.Context, _ playerSpeaker, path string, _, _ int) (*kefw2.RowsResponse, error) {
			if path != "ui:/airableradios" {
				return nil, fmt.Errorf("unexpected root %q", path)
			}
			if attempts.Add(1) == 1 {
				return nil, errors.New("menu unavailable")
			}
			return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{{Title: "Recovered menu"}}}, nil
		},
	}
	model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model, playerDiscoveryServiceMsg{
		generation: model.generation, service: playerDiscoveryRadio, browseRoot: true,
	}))
	p := ui.presentation()
	if p.state.status != playerDiscoveryError || p.state.err == nil ||
		!strings.Contains(p.state.err.Error(), "menu unavailable") || !p.canRetry {
		t.Fatalf("browse error was not surfaced with retry: %+v", p)
	}
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model, playerDiscoveryRetryMsg{
		generation: p.generation, request: p.state.request,
	}))
	if p = ui.presentation(); p.state.status != playerDiscoveryReady || p.state.page.items[0].Title != "Recovered menu" {
		t.Fatalf("root retry failed: %+v", p)
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}

func playerDiscoveryUITestMessages(command tea.Cmd) []tea.Msg {
	if command == nil {
		return nil
	}
	message := command()
	if commands, ok := message.(tea.BatchMsg); ok {
		var messages []tea.Msg
		for _, next := range commands {
			messages = append(messages, playerDiscoveryUITestMessages(next)...)
		}
		return messages
	}
	return []tea.Msg{message}
}

func playerDiscoveryUITestQueueRefresh(t *testing.T, message tea.Msg, generation uint64) {
	t.Helper()
	scoped, ok := message.(playerScopedMsg)
	if !ok || scoped.generation != generation {
		t.Fatalf("queue refresh was not scoped to the action session: %#v", message)
	}
	refresh, ok := scoped.message.(playerDiscoveryQueueChangedMsg)
	if !ok || refresh.generation != generation {
		t.Fatalf("wrong queue refresh message: %#v", scoped.message)
	}
}

func playerDiscoveryUITestStatusRefresh(t *testing.T, message tea.Msg, generation uint64) {
	t.Helper()
	scoped, ok := message.(playerScopedMsg)
	if !ok || scoped.generation != generation {
		t.Fatalf("status refresh was not scoped to the action session: %#v", message)
	}
	refresh, ok := scoped.message.(playerStatusRefreshRequest)
	if !ok || refresh.generation != generation {
		t.Fatalf("wrong status refresh message: %#v", scoped.message)
	}
}

func playerDiscoveryUITestSourceNotice(
	t *testing.T, messages []tea.Msg, generation uint64, target playerSpeaker, cause error,
) {
	t.Helper()
	if len(messages) != 2 {
		t.Fatalf("uncertain source change should refresh status and notify: %#v", messages)
	}
	var refreshed, noticed bool
	for _, message := range messages {
		switch notice := message.(type) {
		case playerScopedMsg:
			playerDiscoveryUITestStatusRefresh(t, notice, generation)
			refreshed = true
		case playerNoticeMsg:
			if notice.target != target || !errors.Is(notice.err, cause) || !strings.Contains(notice.action, "activate WiFi") {
				t.Fatalf("source uncertainty lost the target or cause: %+v", notice)
			}
			noticed = true
		}
	}
	if !refreshed || !noticed {
		t.Fatalf("missing source status refresh or uncertainty notice: %#v", messages)
	}
}

func playerDiscoveryUITestWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for discovery worker")
	}
}

func playerDiscoveryUITestSearch(t *testing.T, model *playerModel, ui *playerDiscoveryUI, query string) {
	t.Helper()
	p := ui.presentation()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoverySearchMsg{generation: p.generation, query: query}))
}

func playerDiscoveryUITestNoWrites(t *testing.T, calls []playerDiscoveryUITestCall) {
	t.Helper()
	for _, call := range calls {
		switch call.name {
		case "set-source", "play-radio", "play-podcast", "add-radio", "remove-radio",
			"add-podcast", "remove-podcast", "enqueue":
			t.Fatalf("preview or browse mutated the speaker: %+v", call)
		}
	}
}

func TestPlayerDiscoveryUILateSearchAndStalePageActions(t *testing.T) {
	oldStarted, releaseOld := make(chan struct{}), make(chan struct{})
	var release sync.Once
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, query string, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		if query == "old" {
			close(oldStarted)
			<-releaseOld
			return nil, errors.New("old search failed")
		}
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{{
			Title: "New", Type: kefw2.ContentTypeAudio, Path: "radio:/new",
		}}}, nil
	}
	model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
	t.Cleanup(func() { release.Do(func() { close(releaseOld) }) })
	generation := ui.presentation().generation
	oldCommand := playerDiscoveryUITestSend(model, playerDiscoverySearchMsg{generation: generation, query: "old"})
	oldRequest := ui.presentation().state.request
	playerDiscoveryUITestWait(t, oldStarted)
	if p := ui.presentation(); p.state.status != playerDiscoveryLoading || p.state.hasPage {
		t.Fatalf("search did not enter loading: %+v", p.state)
	}
	playerDiscoveryUITestSearch(t, model, ui, "new")
	p := ui.presentation()
	if p.query != "new" || p.state.status != playerDiscoveryReady || p.state.page.items[0].Path != "radio:/new" {
		t.Fatalf("new results were not selected: %+v", p)
	}
	p.state.page.items[0].Path = "changed"
	if ui.presentation().state.page.items[0].Path != "radio:/new" {
		t.Fatal("presentation exposed mutable result data")
	}
	if command := playerDiscoveryUITestSend(model,
		playerDiscoveryPlayMsg{generation: generation, request: oldRequest, index: 0}); command != nil {
		t.Fatal("a stale page action dispatched")
	}
	if ui.presentation().confirmation != nil {
		t.Fatal("a stale page action requested confirmation")
	}
	release.Do(func() { close(releaseOld) })
	playerDiscoveryUITestComplete(t, model, oldCommand)
	p = ui.presentation()
	if p.state.status != playerDiscoveryReady || p.state.err != nil || p.state.page.query != "new" {
		t.Fatalf("late search replaced new results: %+v", p.state)
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}

func TestPlayerDiscoveryUIAtoBtoARejectsRetiredResultsAndIntents(t *testing.T) {
	oldStarted, releaseOld := make(chan struct{}), make(chan struct{})
	var release sync.Once
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, target playerSpeaker, _ playerDiscoveryService, query string, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		if query == "old" {
			close(oldStarted)
			<-releaseOld
			return nil, errors.New("retired failure")
		}
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{{
			Type: kefw2.ContentTypeAudio, Path: target.IPAddress + ":" + query,
		}}}, nil
	}
	model, ui, a, b := playerDiscoveryUITestModel(t, backend)
	t.Cleanup(func() { release.Do(func() { close(releaseOld) }) })
	first := ui.presentation().generation
	oldCommand := playerDiscoveryUITestSend(model, playerDiscoverySearchMsg{generation: first, query: "old"})
	oldRequest := ui.presentation().state.request
	playerDiscoveryUITestWait(t, oldStarted)

	model.selectSpeaker(b, false)
	playerDiscoveryUITestSearch(t, model, ui, "bedroom")
	model.Update(playerSessionDoneMsg{generation: first})
	model.selectSpeaker(a, false)
	playerDiscoveryUITestSearch(t, model, ui, "fresh")
	p := ui.presentation()
	if p.target != a || p.generation == first || p.state.page.items[0].Path != a.IPAddress+":fresh" {
		t.Fatalf("returning to A did not start a fresh session: %+v", p)
	}
	before := len(backend.calls())
	for _, msg := range []tea.Msg{
		playerDiscoveryServiceMsg{generation: first, service: playerDiscoveryPodcast},
		playerDiscoverySearchMsg{generation: first, query: "stale intent"},
		playerDiscoveryFavoritesMsg{generation: first},
		playerDiscoveryPageMsg{generation: first, request: oldRequest, offset: 1},
		playerDiscoverySelectMsg{generation: first, request: oldRequest, index: 0},
		playerDiscoveryBrowseMsg{generation: first, request: oldRequest, index: 0},
		playerDiscoveryBackMsg{generation: first, request: oldRequest},
		playerDiscoveryPlayMsg{generation: first, request: oldRequest, index: 0},
		playerDiscoveryConfirmMsg{generation: first, request: oldRequest, accept: true},
		playerDiscoveryFavoriteMsg{generation: first, request: oldRequest, index: 0, add: true},
		playerDiscoveryEnqueueMsg{generation: first, request: oldRequest, index: 0},
	} {
		if command := playerDiscoveryUITestSend(model, msg); command != nil {
			t.Fatalf("retired intent dispatched: %T", msg)
		}
	}
	if len(backend.calls()) != before {
		t.Fatal("retired intent contacted the newly selected speaker")
	}
	release.Do(func() { close(releaseOld) })
	playerDiscoveryUITestComplete(t, model, oldCommand)
	p = ui.presentation()
	if p.target != a || p.state.status != playerDiscoveryReady || p.state.err != nil ||
		p.state.page.items[0].Path != a.IPAddress+":fresh" {
		t.Fatalf("retired A result leaked into new A session: %+v", p)
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}

func playerDiscoveryUITestPodcastBackend() (
	*playerDiscoveryUITestBackend, kefw2.ContentItem, kefw2.ContentItem, kefw2.ContentItem,
) {
	series := kefw2.ContentItem{Type: kefw2.ContentTypeContainer, ID: "s1", Title: "Same", Path: "podcast:/series/1"}
	season := kefw2.ContentItem{Type: kefw2.ContentTypeContainer, ID: "season", Path: "podcast:/series/1/season"}
	episode := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "ep", Path: "podcast:/series/1/episode"}
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, _ string, offset, _ int) (
		*kefw2.RowsResponse, error,
	) {
		if offset == 2 {
			return &kefw2.RowsResponse{RowsCount: 3, Rows: []kefw2.ContentItem{{
				Type: kefw2.ContentTypeContainer, ID: "s3", Title: "Same", Path: "podcast:/series/3",
			}}}, nil
		}
		return &kefw2.RowsResponse{RowsCount: 3, Rows: []kefw2.ContentItem{series, {
			Type: kefw2.ContentTypeContainer, ID: "s2", Title: "Same", Path: "podcast:/series/2",
		}}}, nil
	}
	backend.rows = func(_ context.Context, _ playerSpeaker, path string, _, _ int) (*kefw2.RowsResponse, error) {
		switch path {
		case series.Path:
			return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{season}}, nil
		case season.Path:
			return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{episode}}, nil
		default:
			return nil, fmt.Errorf("unexpected path %s", path)
		}
	}
	backend.favorite = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{series}}, nil
	}
	return backend, series, season, episode
}

func TestPlayerDiscoveryUIPodcastNavigationFavoritesAndPaging(t *testing.T) {
	backend, series, season, episode := playerDiscoveryUITestPodcastBackend()
	model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
	p := ui.presentation()
	playerDiscoveryUITestSend(model, playerDiscoveryServiceMsg{generation: p.generation, service: playerDiscoveryPodcast})
	playerDiscoveryUITestSearch(t, model, ui, "literal query")
	p = ui.presentation()
	if p.state.page.nextOffset != 2 || !p.state.page.hasMore || p.state.page.limit != playerDiscoveryDefaultPageSize {
		t.Fatalf("search did not preserve capped page metadata: %+v", p.state.page)
	}
	playerDiscoveryUITestSend(model,
		playerDiscoverySelectMsg{generation: p.generation, request: p.state.request, index: 1})
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryBrowseMsg{generation: p.generation, request: p.state.request, index: 0}))
	p = ui.presentation()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryBrowseMsg{generation: p.generation, request: p.state.request, index: 0}))
	p = ui.presentation()
	if p.state.page.path != season.Path || p.state.page.items[0].Path != episode.Path {
		t.Fatalf("series/season navigation lost episode identity: %+v", p.state.page)
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0}))
	p = ui.presentation()
	if p.action == nil || !p.action.accepted || p.action.uncertain {
		t.Fatalf("podcast request did not report acceptance: %+v", p.action)
	}
	for range 2 {
		p = ui.presentation()
		playerDiscoveryUITestSend(model, playerDiscoveryBackMsg{generation: p.generation, request: p.state.request})
	}
	p = ui.presentation()
	if p.state.page.kind != playerDiscoverySearchPage || p.selection != 0 || p.state.page.items[0].Path != series.Path {
		t.Fatalf("back did not restore the series page: %+v", p)
	}
	playerDiscoveryUITestPodcastFavoritesAndPages(t, model, ui, backend, series, episode)
}

func playerDiscoveryUITestPodcastFavoritesAndPages(
	t *testing.T, model *playerModel, ui *playerDiscoveryUI, backend *playerDiscoveryUITestBackend,
	series, episode kefw2.ContentItem,
) {
	t.Helper()
	p := ui.presentation()
	for _, add := range []bool{true, false} {
		p = ui.presentation()
		playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
			playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: add}))
	}
	p = ui.presentation()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryPageMsg{generation: p.generation, request: p.state.request, offset: p.state.page.nextOffset}))
	p = ui.presentation()
	if p.state.page.offset != 2 || p.state.page.items[0].Path != "podcast:/series/3" {
		t.Fatalf("paging skipped a capped result: %+v", p.state.page)
	}
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryFavoritesMsg{generation: p.generation}))
	if p = ui.presentation(); p.state.page.kind != playerDiscoveryFavoritesPage || p.state.page.items[0].ID != "s1" {
		t.Fatalf("favorites did not load the exact series: %+v", p.state.page)
	}
	var mutations []playerDiscoveryUITestCall
	for _, call := range backend.calls() {
		if call.name == "play-podcast" || call.name == "add-podcast" || call.name == "remove-podcast" {
			mutations = append(mutations, call)
		}
	}
	if len(mutations) != 3 || mutations[0].item.Path != episode.Path ||
		mutations[1].item.Path != series.Path || mutations[1].item.ID != series.ID ||
		mutations[2].item.Path != series.Path || mutations[2].item.ID != series.ID {
		t.Fatalf("explicit podcast actions lost their exact identities: %+v", mutations)
	}
}

func playerDiscoveryUITestConfirmRadio(
	t *testing.T, model *playerModel, ui *playerDiscoveryUI, backend *playerDiscoveryUITestBackend,
	station kefw2.ContentItem, target playerSpeaker,
) tea.Cmd {
	t.Helper()
	p := ui.presentation()
	playerDiscoveryUITestSend(model,
		playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0})
	p = ui.presentation()
	if p.confirmation == nil || !p.confirmation.clearsQueue || p.confirmation.target != target ||
		p.confirmation.item.Path != station.Path || !strings.Contains(p.confirmation.action, station.Path) {
		t.Fatalf("radio confirmation omitted target, exact item or clear warning: %+v", p.confirmation)
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
	playerDiscoveryUITestSend(model,
		playerDiscoveryConfirmMsg{generation: p.generation, request: p.state.request, accept: false})
	p = ui.presentation()
	playerDiscoveryUITestSend(model,
		playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0})
	return playerDiscoveryUITestSend(model,
		playerDiscoveryConfirmMsg{generation: p.generation, request: p.state.request, accept: true})
}

func playerDiscoveryUITestRetiredRadioNotice(
	t *testing.T, command tea.Cmd, backend *playerDiscoveryUITestBackend, target playerSpeaker, station kefw2.ContentItem,
) {
	t.Helper()
	if command == nil {
		t.Fatal("retired dispatched write did not create an uncertainty notice")
	}
	notice, ok := command().(playerNoticeMsg)
	if !ok || notice.target != target || !strings.Contains(notice.action, station.Path) ||
		notice.err == nil || !errors.Is(notice.err, context.Canceled) {
		t.Fatalf("wrong global notice: %+v", notice)
	}
	var plays int
	for _, call := range backend.calls() {
		if call.name == "play-radio" {
			plays++
			if call.target != target || call.item.Path != station.Path {
				t.Fatalf("write was retargeted or identity changed: %+v", call)
			}
		}
	}
	if plays != 1 {
		t.Fatalf("radio request was replayed %d times", plays)
	}
}

func TestPlayerDiscoveryUIRadioConfirmationAndRetiredWriteNotice(t *testing.T) {
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r1", Title: "Jazz", Path: "radio:/exact"}
	started, releasePlay := make(chan struct{}), make(chan struct{})
	favoriteStarted, releaseFavorite := make(chan struct{}), make(chan struct{})
	var release, releaseCurrent sync.Once
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, _ string, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{station}}, nil
	}
	backend.mutate = func(_ context.Context, _ playerSpeaker, name string, _ kefw2.ContentItem) error {
		switch name {
		case "play-radio":
			close(started)
			<-releasePlay
		case "add-radio":
			close(favoriteStarted)
			<-releaseFavorite
		}
		return nil
	}
	model, ui, a, b := playerDiscoveryUITestModel(t, backend)
	t.Cleanup(func() {
		release.Do(func() { close(releasePlay) })
		releaseCurrent.Do(func() { close(releaseFavorite) })
	})
	playerDiscoveryUITestSearch(t, model, ui, "jazz")
	command := playerDiscoveryUITestConfirmRadio(t, model, ui, backend, station, a)
	playerDiscoveryUITestWait(t, started)
	if p := ui.presentation(); !p.busy || p.action == nil || !p.action.pending || p.action.target != a {
		t.Fatalf("pending write lost its captured target: %+v", p.action)
	}
	model.selectSpeaker(b, false)
	playerDiscoveryUITestReady(model)
	if p := ui.presentation(); p.target != b || p.confirmation != nil || p.action != nil || p.state.hasPage {
		t.Fatalf("old target state leaked into the new session: %+v", p)
	}
	playerDiscoveryUITestSearch(t, model, ui, "bedroom")
	p := ui.presentation()
	current := playerDiscoveryUITestSend(model,
		playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true})
	playerDiscoveryUITestWait(t, favoriteStarted)
	release.Do(func() { close(releasePlay) })
	noticeCommand := playerDiscoveryUITestComplete(t, model, command)
	playerDiscoveryUITestRetiredRadioNotice(t, noticeCommand, backend, a, station)
	if p = ui.presentation(); !p.busy || p.action == nil || p.action.target != b || !p.action.pending {
		t.Fatalf("old completion changed the new target's pending action: %+v", p.action)
	}
	releaseCurrent.Do(func() { close(releaseFavorite) })
	playerDiscoveryUITestComplete(t, model, current)
}

func TestPlayerDiscoveryUIConfirmationExpiresOnSelectionOrQueryChange(t *testing.T) {
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, _ string, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		return &kefw2.RowsResponse{RowsCount: 2, Rows: []kefw2.ContentItem{
			{Type: kefw2.ContentTypeAudio, Path: "radio:/first"},
			{Type: kefw2.ContentTypeAudio, Path: "radio:/second"},
		}}, nil
	}
	model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "first query")
	p := ui.presentation()
	playerDiscoveryUITestSend(model,
		playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0})
	playerDiscoveryUITestSend(model,
		playerDiscoverySelectMsg{generation: p.generation, request: p.state.request, index: 1})
	if ui.presentation().confirmation != nil {
		t.Fatal("selection change retained an old station confirmation")
	}
	if command := playerDiscoveryUITestSend(model,
		playerDiscoveryConfirmMsg{generation: p.generation, request: p.state.request, accept: true}); command != nil {
		t.Fatal("an expired confirmation dispatched playback")
	}
	playerDiscoveryUITestSend(model,
		playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0})
	playerDiscoveryUITestSearch(t, model, ui, "second query")
	if command := playerDiscoveryUITestSend(model,
		playerDiscoveryConfirmMsg{generation: p.generation, request: p.state.request, accept: true}); command != nil {
		t.Fatal("an old query confirmation dispatched playback")
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}

func TestPlayerDiscoveryUICanceledBeforeDispatchDoesNotNotify(t *testing.T) {
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r1", Path: "radio:/exact"}
	started := make(chan struct{})
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, _ string, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{station}}, nil
	}
	backend.resolve = func(ctx context.Context, _ playerSpeaker, _ playerDiscoveryService, _ string) (
		*kefw2.ContentItem, error,
	) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	model, ui, _, b := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "station")
	p := ui.presentation()
	playerDiscoveryUITestSend(model,
		playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0})
	command := playerDiscoveryUITestSend(model,
		playerDiscoveryConfirmMsg{generation: p.generation, request: p.state.request, accept: true})
	playerDiscoveryUITestWait(t, started)
	model.selectSpeaker(b, false)
	if notice := playerDiscoveryUITestComplete(t, model, command); notice != nil {
		t.Fatal("canceled pre-dispatch read produced a false write notice")
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}

func TestPlayerDiscoveryUIFailedSourceNeverPlays(t *testing.T) {
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r1", Path: "radio:/exact"}
	sourceError := errors.New("source switch failed")
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, _ string, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{station}}, nil
	}
	backend.source = func(context.Context, playerSpeaker) (kefw2.Source, error) { return kefw2.SourceBluetooth, nil }
	backend.set = func(context.Context, playerSpeaker, kefw2.Source) error { return sourceError }
	model, ui, a, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "station")
	p := ui.presentation()
	playerDiscoveryUITestSend(model,
		playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0})
	noticeCommand := playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryConfirmMsg{generation: p.generation, request: p.state.request, accept: true}))
	p = ui.presentation()
	if p.action == nil || !p.action.uncertain || !errors.Is(p.action.err, sourceError) {
		t.Fatalf("source failure was not presented as uncertain: %+v", p.action)
	}
	playerDiscoveryUITestSourceNotice(t, playerDiscoveryUITestMessages(noticeCommand), p.generation, a, sourceError)
	for _, call := range backend.calls() {
		if call.name == "play-radio" {
			t.Fatalf("play was sent after a failed source switch: %+v", call)
		}
	}
}

func TestPlayerDiscoveryUIUPnPBrowseAndExplicitEnqueue(t *testing.T) {
	server := kefw2.ContentItem{Type: kefw2.ContentTypeContainer, Path: "upnp:/server/1"}
	track := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "track", Path: "upnp:/server/1/track"}
	folder := kefw2.ContentItem{Type: kefw2.ContentTypeContainer, Path: "upnp:/server/1/folder"}
	backend := &playerDiscoveryUITestBackend{}
	backend.rows = func(_ context.Context, _ playerSpeaker, path string, from, _ int) (*kefw2.RowsResponse, error) {
		switch {
		case path == "ui:/upnp" && from == 0:
			return &kefw2.RowsResponse{RowsCount: 2, Rows: []kefw2.ContentItem{server}}, nil
		case path == "ui:/upnp" && from == 1:
			return &kefw2.RowsResponse{RowsCount: 2, Rows: []kefw2.ContentItem{{
				Type: kefw2.ContentTypeContainer, Path: "upnp:/server/2",
			}}}, nil
		case path == server.Path:
			return &kefw2.RowsResponse{RowsCount: 2, Rows: []kefw2.ContentItem{track, folder}}, nil
		default:
			return nil, fmt.Errorf("unexpected browse %s at %d", path, from)
		}
	}
	model, ui, a, _ := playerDiscoveryUITestModel(t, backend)
	p := ui.presentation()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryServiceMsg{generation: p.generation, service: playerDiscoveryUPnP}))
	p = ui.presentation()
	if p.state.page.nextOffset != 1 || !p.state.page.hasMore {
		t.Fatalf("capped UPnP page metadata incorrect: %+v", p.state.page)
	}
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryPageMsg{generation: p.generation, request: p.state.request, offset: 1}))
	p = ui.presentation()
	if p.state.page.items[0].Path != "upnp:/server/2" {
		t.Fatalf("UPnP next page skipped a row: %+v", p.state.page)
	}
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryPageMsg{generation: p.generation, request: p.state.request, offset: 0}))
	p = ui.presentation()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryBrowseMsg{generation: p.generation, request: p.state.request, index: 0}))
	p = ui.presentation()
	playerDiscoveryUITestSend(model,
		playerDiscoverySelectMsg{generation: p.generation, request: p.state.request, index: 1})
	playerDiscoveryUITestSend(model,
		playerDiscoveryEnqueueMsg{generation: p.generation, request: p.state.request, index: 1})
	if p = ui.presentation(); p.action == nil || p.action.err == nil {
		t.Fatalf("enqueuing a container was not rejected: %+v", p.action)
	}
	playerDiscoveryUITestSend(model,
		playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0})
	playerDiscoveryUITestNoWrites(t, backend.calls())
	refreshCommand := playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryEnqueueMsg{generation: p.generation, request: p.state.request, index: 0}))
	messages := playerDiscoveryUITestMessages(refreshCommand)
	if len(messages) != 1 {
		t.Fatalf("successful enqueue returned %d follow-up messages", len(messages))
	}
	playerDiscoveryUITestQueueRefresh(t, messages[0], p.generation)
	p = ui.presentation()
	if p.action == nil || !p.action.accepted {
		t.Fatalf("enqueue did not report acceptance: %+v", p.action)
	}
	var enqueues []playerDiscoveryUITestCall
	for _, call := range backend.calls() {
		if call.name == "enqueue" {
			enqueues = append(enqueues, call)
		}
	}
	if len(enqueues) != 1 || enqueues[0].target != a || enqueues[0].item.Path != track.Path || enqueues[0].start {
		t.Fatalf("UPnP enqueue lost target, track identity, or no-autostart policy: %+v", enqueues)
	}
}

func TestPlayerDiscoveryUIResetCancelsWithoutWaitingAndStopJoins(t *testing.T) {
	started, canceled, releaseRead := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var release sync.Once
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(ctx context.Context, _ playerSpeaker, _ playerDiscoveryService, _ string, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-releaseRead
		return nil, ctx.Err()
	}
	model, ui, _, b := playerDiscoveryUITestModel(t, backend)
	t.Cleanup(func() { release.Do(func() { close(releaseRead) }) })
	p := ui.presentation()
	command := playerDiscoveryUITestSend(model, playerDiscoverySearchMsg{generation: p.generation, query: "slow"})
	playerDiscoveryUITestWait(t, started)
	switched := make(chan struct{})
	go func() {
		model.selectSpeaker(b, false)
		close(switched)
	}()
	playerDiscoveryUITestWait(t, switched)
	playerDiscoveryUITestWait(t, canceled)
	channels := ui.stop(model)
	if len(channels) != 1 {
		t.Fatalf("stop did not retain the retired worker: %d", len(channels))
	}
	select {
	case <-channels[0]:
		t.Fatal("stop reported a blocked worker finished")
	default:
	}
	release.Do(func() { close(releaseRead) })
	playerDiscoveryUITestWait(t, channels[0])
	playerDiscoveryUITestComplete(t, model, command)
}

func TestPlayerDiscoveryUIBoundsCanceledReads(t *testing.T) {
	started := make(chan struct{}, playerDiscoveryUIMaxReads)
	releaseRead := make(chan struct{})
	var release sync.Once
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, query string, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		if query != "recover" {
			started <- struct{}{}
			<-releaseRead
		}
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{{
			Type: kefw2.ContentTypeAudio, Path: "radio:/" + query,
		}}}, nil
	}
	model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
	t.Cleanup(func() { release.Do(func() { close(releaseRead) }) })
	commands := make([]tea.Cmd, 0, playerDiscoveryUIMaxReads)
	for i := range playerDiscoveryUIMaxReads {
		p := ui.presentation()
		command := playerDiscoveryUITestSend(model,
			playerDiscoverySearchMsg{generation: p.generation, query: fmt.Sprintf("slow-%d", i)})
		if command == nil {
			t.Fatal("read limit rejected a request prematurely")
		}
		commands = append(commands, command)
		playerDiscoveryUITestWait(t, started)
	}
	p := ui.presentation()
	if command := playerDiscoveryUITestSend(model,
		playerDiscoverySearchMsg{generation: p.generation, query: "overflow"}); command != nil {
		t.Fatal("too many canceled reads were allowed to accumulate")
	}
	if p = ui.presentation(); p.state.status != playerDiscoveryError || p.state.err == nil {
		t.Fatalf("read saturation was not surfaced: %+v", p.state)
	}
	done := ui.cancelWorkers()
	release.Do(func() { close(releaseRead) })
	for _, finished := range done {
		playerDiscoveryUITestWait(t, finished)
	}
	for _, command := range commands {
		playerDiscoveryUITestComplete(t, model, command)
	}
	p = ui.presentation()
	if !p.canRetry {
		t.Fatal("saturated read did not retain its retry request")
	}
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryRetryMsg{generation: p.generation, request: p.state.request}))
	if p = ui.presentation(); p.state.status != playerDiscoveryReady || p.state.page.query != "overflow" {
		t.Fatalf("discovery did not recover after old workers stopped: %+v", p.state)
	}
}

func TestPlayerDiscoveryUIDistinguishesEmptyErrorAndStale(t *testing.T) {
	searchErr := errors.New("unavailable")
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, query string, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		switch query {
		case "empty":
			return &kefw2.RowsResponse{}, nil
		case "error":
			return nil, searchErr
		default:
			return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{{
				Type: kefw2.ContentTypeAudio, Path: "radio:/ready",
			}}}, nil
		}
	}
	model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "empty")
	if p := ui.presentation(); p.state.status != playerDiscoveryEmpty || p.state.err != nil || p.canRetry {
		t.Fatalf("empty response looked like an error: %+v", p.state)
	}
	p := ui.presentation()
	if command := playerDiscoveryUITestSend(model,
		playerDiscoveryRetryMsg{generation: p.generation, request: p.state.request}); command != nil {
		t.Fatal("empty results were treated as a failed read")
	}
	playerDiscoveryUITestSearch(t, model, ui, "ready")
	p = ui.presentation()
	command := playerDiscoveryUITestSend(model, playerDiscoverySearchMsg{generation: p.generation, query: "error"})
	if p = ui.presentation(); p.state.status != playerDiscoveryLoading || !p.state.stale {
		t.Fatalf("loading did not mark the old page stale: %+v", p.state)
	}
	playerDiscoveryUITestComplete(t, model, command)
	p = ui.presentation()
	if p.state.status != playerDiscoveryError || !p.state.stale || !errors.Is(p.state.err, searchErr) {
		t.Fatalf("error and stale page were not distinguished: %+v", p.state)
	}
	playerDiscoveryUITestSend(model,
		playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0})
	if ui.presentation().confirmation != nil {
		t.Fatal("stale rows were actionable")
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}

func playerDiscoveryUITestRetrySearchBackend() *playerDiscoveryUITestBackend {
	var firstPage, nextPage atomic.Int32
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, _ string, offset, _ int) (
		*kefw2.RowsResponse, error,
	) {
		counter := &firstPage
		if offset == 2 {
			counter = &nextPage
		}
		if counter.Add(1) == 1 {
			return nil, errors.New("temporary search failure")
		}
		rows := []kefw2.ContentItem{
			{Type: kefw2.ContentTypeAudio, Path: "radio:/one"},
			{Type: kefw2.ContentTypeAudio, Path: "radio:/two"},
		}
		if offset == 2 {
			rows = []kefw2.ContentItem{{Type: kefw2.ContentTypeAudio, Path: "radio:/three"}}
		}
		return &kefw2.RowsResponse{RowsCount: 3, Rows: rows}, nil
	}
	return backend
}

func TestPlayerDiscoveryUIRetryPreservesSearchAndPage(t *testing.T) {
	backend := playerDiscoveryUITestRetrySearchBackend()
	model, ui, target, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "Jazz & Blues")
	p := ui.presentation()
	if !p.canRetry || p.state.status != playerDiscoveryError {
		t.Fatalf("failed search did not expose retry: %+v", p.state)
	}
	failed := p.state.request
	command := playerDiscoveryUITestSend(model,
		playerDiscoveryRetryMsg{generation: p.generation, request: failed})
	if p = ui.presentation(); p.canRetry || p.state.status != playerDiscoveryLoading || p.state.request == failed {
		t.Fatalf("retry did not start a fresh read: %+v", p.state)
	}
	if duplicate := playerDiscoveryUITestSend(model,
		playerDiscoveryRetryMsg{generation: p.generation, request: failed}); duplicate != nil {
		t.Fatal("duplicate retry launched another read")
	}
	playerDiscoveryUITestComplete(t, model, command)
	p = ui.presentation()
	if p.canRetry || p.state.page.query != "Jazz & Blues" || p.state.page.nextOffset != 2 {
		t.Fatalf("retried search lost its query or capped offset: %+v", p)
	}
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryPageMsg{generation: p.generation, request: p.state.request, offset: p.state.page.nextOffset}))
	p = ui.presentation()
	if !p.canRetry || !p.state.stale || p.state.status != playerDiscoveryError {
		t.Fatalf("failed page was not retryable: %+v", p.state)
	}
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryRetryMsg{generation: p.generation, request: p.state.request}))
	p = ui.presentation()
	if p.canRetry || p.state.page.offset != 2 || !p.hasPrevious || p.previousOffset != 0 {
		t.Fatalf("page retry lost paging history: %+v", p)
	}
	playerDiscoveryUITestRetrySearchCalls(t, backend.calls(), target)
}

func playerDiscoveryUITestRetrySearchCalls(t *testing.T, calls []playerDiscoveryUITestCall, target playerSpeaker) {
	t.Helper()
	if len(calls) != 4 {
		t.Fatalf("unexpected search count: %+v", calls)
	}
	for i, call := range calls {
		wantOffset := 0
		if i >= 2 {
			wantOffset = 2
		}
		if call.name != "search-radio" || call.target != target || call.value != "Jazz & Blues" ||
			call.offset != wantOffset || call.limit != playerDiscoveryDefaultPageSize {
			t.Fatalf("retry changed search identity: %+v", call)
		}
	}
	playerDiscoveryUITestNoWrites(t, calls)
}

func playerDiscoveryUITestRetryUPnPBackend(server kefw2.ContentItem) *playerDiscoveryUITestBackend {
	var rootAttempts, containerAttempts atomic.Int32
	backend := &playerDiscoveryUITestBackend{}
	backend.rows = func(_ context.Context, _ playerSpeaker, path string, _, _ int) (*kefw2.RowsResponse, error) {
		counter := &rootAttempts
		if path == server.Path {
			counter = &containerAttempts
		}
		if counter.Add(1) == 1 {
			return nil, errors.New("temporary browse failure")
		}
		if path == playerDiscoveryUPnPRootPath {
			return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{server}}, nil
		}
		if path == server.Path {
			return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{{
				Type: kefw2.ContentTypeAudio, Path: server.Path + "/track",
			}}}, nil
		}
		return nil, fmt.Errorf("unexpected browse path %q", path)
	}
	return backend
}

func TestPlayerDiscoveryUIRetryUPnPRootAndContainer(t *testing.T) {
	server := kefw2.ContentItem{Type: kefw2.ContentTypeContainer, Path: "upnp:/server/exact"}
	backend := playerDiscoveryUITestRetryUPnPBackend(server)
	model, ui, target, _ := playerDiscoveryUITestModel(t, backend)
	p := ui.presentation()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryServiceMsg{generation: p.generation, service: playerDiscoveryUPnP}))
	p = ui.presentation()
	if !p.canRetry || p.state.hasPage {
		t.Fatalf("failed UPnP root did not offer retry: %+v", p.state)
	}
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryRetryMsg{generation: p.generation, request: p.state.request}))
	p = ui.presentation()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryBrowseMsg{generation: p.generation, request: p.state.request, index: 0}))
	p = ui.presentation()
	if !p.canRetry || !p.state.stale {
		t.Fatalf("failed UPnP container did not offer retry: %+v", p.state)
	}
	failed := p.state.request
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryRetryMsg{generation: p.generation, request: failed}))
	p = ui.presentation()
	if p.canRetry || p.state.page.path != server.Path || p.state.page.items[0].Path != server.Path+"/track" {
		t.Fatalf("container retry lost its exact path: %+v", p.state.page)
	}
	playerDiscoveryUITestSend(model, playerDiscoveryBackMsg{generation: p.generation, request: p.state.request})
	if p = ui.presentation(); p.state.page.path != playerDiscoveryUPnPRootPath || p.canRetry {
		t.Fatalf("retry lost the parent breadcrumb: %+v", p)
	}
	if command := playerDiscoveryUITestSend(model,
		playerDiscoveryRetryMsg{generation: p.generation, request: failed}); command != nil {
		t.Fatal("stale container retry was accepted after back")
	}
	playerDiscoveryUITestRetryBrowseCalls(t, backend.calls(), target, server.Path)
}

func playerDiscoveryUITestRetryBrowseCalls(
	t *testing.T, calls []playerDiscoveryUITestCall, target playerSpeaker, container string,
) {
	t.Helper()
	if len(calls) != 4 {
		t.Fatalf("unexpected browse count: %+v", calls)
	}
	for i, call := range calls {
		path := playerDiscoveryUPnPRootPath
		if i >= 2 {
			path = container
		}
		if call.name != "rows" || call.target != target || call.value != path ||
			call.offset != 0 || call.limit != playerDiscoveryDefaultPageSize {
			t.Fatalf("retry changed browse identity: %+v", call)
		}
	}
	playerDiscoveryUITestNoWrites(t, calls)
}

func playerDiscoveryUITestRetryUPnPPageBackend(server kefw2.ContentItem) *playerDiscoveryUITestBackend {
	var nextPageAttempts atomic.Int32
	backend := &playerDiscoveryUITestBackend{}
	backend.rows = func(_ context.Context, _ playerSpeaker, path string, from, _ int) (*kefw2.RowsResponse, error) {
		if path == playerDiscoveryUPnPRootPath {
			return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{server}}, nil
		}
		if path != server.Path {
			return nil, fmt.Errorf("unexpected browse path %q", path)
		}
		if from == 1 && nextPageAttempts.Add(1) == 1 {
			return nil, errors.New("temporary page failure")
		}
		return &kefw2.RowsResponse{RowsCount: 2, Rows: []kefw2.ContentItem{{
			Type: kefw2.ContentTypeAudio, Path: fmt.Sprintf("%s/track/%d", path, from),
		}}}, nil
	}
	return backend
}

func TestPlayerDiscoveryUIRetryUPnPPageKeepsPathAndOffset(t *testing.T) {
	server := kefw2.ContentItem{Type: kefw2.ContentTypeContainer, Path: "upnp:/server/exact"}
	backend := playerDiscoveryUITestRetryUPnPPageBackend(server)
	model, ui, target, _ := playerDiscoveryUITestModel(t, backend)
	p := ui.presentation()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryServiceMsg{generation: p.generation, service: playerDiscoveryUPnP}))
	p = ui.presentation()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryBrowseMsg{generation: p.generation, request: p.state.request, index: 0}))
	p = ui.presentation()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryPageMsg{generation: p.generation, request: p.state.request, offset: p.state.page.nextOffset}))
	p = ui.presentation()
	if !p.canRetry || !p.state.stale {
		t.Fatalf("failed UPnP page was not retryable: %+v", p.state)
	}
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryRetryMsg{generation: p.generation, request: p.state.request}))
	p = ui.presentation()
	if p.state.page.path != server.Path || p.state.page.offset != 1 || p.state.page.items[0].Path != server.Path+"/track/1" ||
		!p.hasPrevious || p.previousOffset != 0 {
		t.Fatalf("UPnP page retry changed its path, offset or history: %+v", p)
	}
	calls := backend.calls()
	if len(calls) != 4 || calls[2].target != target || calls[3].target != target ||
		calls[2].value != server.Path || calls[3].value != server.Path ||
		calls[2].offset != 1 || calls[3].offset != 1 ||
		calls[2].limit != playerDiscoveryDefaultPageSize || calls[3].limit != playerDiscoveryDefaultPageSize {
		t.Fatalf("UPnP retry changed the page request: %+v", calls)
	}
	playerDiscoveryUITestNoWrites(t, calls)
}

func TestPlayerDiscoveryUIRetryFavoritesNeverReplaysMutation(t *testing.T) {
	var attempts atomic.Int32
	mutationError := errors.New("favorite request failed")
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r1", Path: "radio:/exact"}
	backend := &playerDiscoveryUITestBackend{}
	backend.favorite = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("temporary favorites failure")
		}
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{station}}, nil
	}
	backend.mutate = func(_ context.Context, _ playerSpeaker, _ string, _ kefw2.ContentItem) error {
		return mutationError
	}
	model, ui, target, _ := playerDiscoveryUITestModel(t, backend)
	p := ui.presentation()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryFavoritesMsg{generation: p.generation}))
	p = ui.presentation()
	if !p.canRetry {
		t.Fatal("failed favorites read did not offer retry")
	}
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryRetryMsg{generation: p.generation, request: p.state.request}))
	p = ui.presentation()
	if p.canRetry || p.state.page.kind != playerDiscoveryFavoritesPage || p.state.page.items[0].ID != station.ID {
		t.Fatalf("retry did not restore the favorites page: %+v", p)
	}
	notice := playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true}))
	if notice == nil {
		t.Fatal("failed dispatched favorite did not report uncertainty")
	}
	p = ui.presentation()
	if p.canRetry || p.action == nil || !errors.Is(p.action.err, mutationError) {
		t.Fatalf("mutation failure was incorrectly offered as a read retry: %+v", p)
	}
	if command := playerDiscoveryUITestSend(model,
		playerDiscoveryRetryMsg{generation: p.generation, request: p.state.request}); command != nil {
		t.Fatal("retry replayed a favorite mutation")
	}
	calls := backend.calls()
	if len(calls) != 3 || calls[0].name != "favorites-radio" || calls[1].name != "favorites-radio" ||
		calls[0].target != target || calls[1].target != target || calls[1].offset != 0 ||
		calls[1].limit != playerDiscoveryDefaultPageSize || calls[2].name != "add-radio" {
		t.Fatalf("favorites retry changed the read or replayed a mutation: %+v", calls)
	}
}

func TestPlayerDiscoveryUIRetryRejectedAfterAtoBtoA(t *testing.T) {
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, _ string, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		return nil, errors.New("temporary search failure")
	}
	model, ui, a, b := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "old")
	p := ui.presentation()
	oldGeneration, failed := p.generation, p.state.request
	if !p.canRetry {
		t.Fatal("expected a retryable failure")
	}
	model.selectSpeaker(b, false)
	model.Update(playerSessionDoneMsg{generation: oldGeneration})
	model.selectSpeaker(a, false)
	before := len(backend.calls())
	if command := playerDiscoveryUITestSend(model,
		playerDiscoveryRetryMsg{generation: oldGeneration, request: failed}); command != nil {
		t.Fatal("retired retry launched on a new session")
	}
	if p = ui.presentation(); p.canRetry || p.target != a || p.generation == oldGeneration {
		t.Fatalf("retired retry state leaked after A-to-B-to-A: %+v", p)
	}
	if len(backend.calls()) != before {
		t.Fatal("retired retry contacted the newly selected speaker")
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}

func TestPlayerDiscoveryUIBackwardsPagingUsesActualPageOffsets(t *testing.T) {
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, _ string, offset, _ int) (
		*kefw2.RowsResponse, error,
	) {
		count := min(2, 5-offset)
		rows := make([]kefw2.ContentItem, count)
		for i := range rows {
			rows[i] = kefw2.ContentItem{Type: kefw2.ContentTypeAudio, Path: fmt.Sprintf("radio:/%d", offset+i)}
		}
		return &kefw2.RowsResponse{RowsCount: 5, Rows: rows}, nil
	}
	model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "capped")
	for _, want := range []int{2, 4} {
		p := ui.presentation()
		playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
			playerDiscoveryPageMsg{generation: p.generation, request: p.state.request, offset: p.state.page.nextOffset}))
		if got := ui.presentation().state.page.offset; got != want {
			t.Fatalf("forward page offset = %d, want %d", got, want)
		}
	}
	for _, want := range []int{2, 0} {
		p := ui.presentation()
		if !p.hasPrevious || p.previousOffset != want {
			t.Fatalf("previous page = %d present=%v, want %d", p.previousOffset, p.hasPrevious, want)
		}
		playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
			playerDiscoveryPageMsg{generation: p.generation, request: p.state.request, offset: p.previousOffset}))
	}
	if p := ui.presentation(); p.hasPrevious || p.state.page.offset != 0 {
		t.Fatalf("first page has an unexpected predecessor: %+v", p)
	}
}

func TestPlayerDiscoveryUINewQueryDropsLateActionErrorButRefreshesQueue(t *testing.T) {
	playError := errors.New("connection dropped after play request")
	started, releasePlay := make(chan struct{}), make(chan struct{})
	var release sync.Once
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, query string, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{{
			Type: kefw2.ContentTypeAudio, Title: query, ID: query, Path: "radio:/" + query,
		}}}, nil
	}
	backend.mutate = func(_ context.Context, _ playerSpeaker, _ string, _ kefw2.ContentItem) error {
		close(started)
		<-releasePlay
		return playError
	}
	model, ui, target, _ := playerDiscoveryUITestModel(t, backend)
	t.Cleanup(func() { release.Do(func() { close(releasePlay) }) })
	playerDiscoveryUITestSearch(t, model, ui, "old")
	p := ui.presentation()
	playerDiscoveryUITestSend(model,
		playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0})
	action := playerDiscoveryUITestSend(model,
		playerDiscoveryConfirmMsg{generation: p.generation, request: p.state.request, accept: true})
	playerDiscoveryUITestWait(t, started)
	playerDiscoveryUITestSearch(t, model, ui, "new")
	release.Do(func() { close(releasePlay) })
	followup := playerDiscoveryUITestComplete(t, model, action)
	p = ui.presentation()
	if p.busy || p.action != nil || p.state.status != playerDiscoveryReady ||
		p.state.err != nil || p.state.page.query != "new" {
		t.Fatalf("old action error contaminated the new query: %+v", p)
	}
	messages := playerDiscoveryUITestMessages(followup)
	if len(messages) != 3 {
		t.Fatalf("uncertain radio write should refresh queue/status and notify: %#v", messages)
	}
	var sawQueue, sawStatus, sawNotice bool
	for _, message := range messages {
		switch notice := message.(type) {
		case playerScopedMsg:
			switch notice.message.(type) {
			case playerDiscoveryQueueChangedMsg:
				playerDiscoveryUITestQueueRefresh(t, notice, p.generation)
				sawQueue = true
			case playerStatusRefreshRequest:
				playerDiscoveryUITestStatusRefresh(t, notice, p.generation)
				sawStatus = true
			}
		case playerNoticeMsg:
			if notice.target != target || !errors.Is(notice.err, playError) ||
				!strings.Contains(notice.action, "radio:/old") {
				t.Fatalf("uncertainty notice lost the original action: %+v", notice)
			}
			sawNotice = true
		}
	}
	if !sawQueue || !sawStatus || !sawNotice {
		t.Fatalf("missing queue/status refresh or uncertainty notice: %#v", messages)
	}
}

func TestPlayerDiscoveryUIMutationsShareSessionFIFO(t *testing.T) {
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r1", Path: "radio:/exact"}
	started, releaseMutation := make(chan struct{}), make(chan struct{})
	var release sync.Once
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, _ string, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{station}}, nil
	}
	backend.mutate = func(_ context.Context, _ playerSpeaker, _ string, _ kefw2.ContentItem) error {
		close(started)
		<-releaseMutation
		return nil
	}
	model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "first")
	first := model.mutations.reserve()
	if err := first.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := ui.presentation()
	action := playerDiscoveryUITestSend(model,
		playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true})
	third := model.mutations.reserve()
	waitContext, cancelWait := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancelWait()
		first.release()
		third.release()
		release.Do(func() { close(releaseMutation) })
	})
	thirdReady := make(chan error, 1)
	go func() { thirdReady <- third.wait(waitContext) }()
	playerDiscoveryUITestSearch(t, model, ui, "read while queued")
	select {
	case <-started:
		t.Fatal("discovery mutation bypassed an earlier session transaction")
	default:
	}
	first.release()
	playerDiscoveryUITestWait(t, started)
	select {
	case err := <-thirdReady:
		t.Fatalf("later session transaction bypassed discovery: %v", err)
	default:
	}
	release.Do(func() { close(releaseMutation) })
	playerDiscoveryUITestComplete(t, model, action)
	select {
	case err := <-thirdReady:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("discovery did not release its session mutation ticket")
	}
	third.release()
}

func TestPlayerDiscoveryUICanceledGateWaitNeverDispatches(t *testing.T) {
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r1", Path: "radio:/exact"}
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, _ string, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{station}}, nil
	}
	model, ui, _, b := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "station")
	first := model.mutations.reserve()
	defer first.release()
	if err := first.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := ui.presentation()
	action := playerDiscoveryUITestSend(model,
		playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true})
	model.selectSpeaker(b, false)
	if notice := playerDiscoveryUITestComplete(t, model, action); notice != nil {
		t.Fatal("a mutation canceled before gate acquisition produced an uncertainty notice")
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}

func TestPlayerDiscoveryUINilMutationGateFailsClosed(t *testing.T) {
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r1", Path: "radio:/exact"}
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(_ context.Context, _ playerSpeaker, _ playerDiscoveryService, _ string, _, _ int) (
		*kefw2.RowsResponse, error,
	) {
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{station}}, nil
	}
	model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "station")
	ui.mutations = nil
	p := ui.presentation()
	command := playerDiscoveryUITestSend(model,
		playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true})
	if command != nil || ui.presentation().action == nil || ui.presentation().action.err == nil {
		t.Fatal("missing mutation gate did not fail closed")
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}

func playerDiscoveryUITestItemBackend(item kefw2.ContentItem) *playerDiscoveryUITestBackend {
	rows := func() *kefw2.RowsResponse {
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{item}}
	}
	return &playerDiscoveryUITestBackend{
		search: func(context.Context, playerSpeaker, playerDiscoveryService, string, int, int) (
			*kefw2.RowsResponse, error,
		) {
			return rows(), nil
		},
		rows: func(context.Context, playerSpeaker, string, int, int) (*kefw2.RowsResponse, error) {
			return rows(), nil
		},
	}
}

func playerDiscoveryUITestBlockedMutation(t *testing.T, model *playerModel, ui *playerDiscoveryUI, message tea.Msg) {
	t.Helper()
	if command := playerDiscoveryUITestSend(model, message); command != nil {
		t.Fatal("mutation was scheduled without fresh speaker status")
	}
	p := ui.presentation()
	if p.writeReady || p.action == nil || p.action.err == nil ||
		!strings.Contains(p.action.err.Error(), "status") {
		t.Fatalf("disabled write was not visibly rejected: %+v", p.action)
	}
}

func TestPlayerDiscoveryUIAllWritesRequireFreshStatus(t *testing.T) {
	tests := []struct {
		name    string
		service playerDiscoveryService
		item    kefw2.ContentItem
		message func(playerDiscoveryPresentation) tea.Msg
	}{
		{
			name: "radio play", service: playerDiscoveryRadio,
			item: kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r", Path: "radio:/exact"},
			message: func(p playerDiscoveryPresentation) tea.Msg {
				return playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0}
			},
		},
		{
			name: "radio favorite", service: playerDiscoveryRadio,
			item: kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r", Path: "radio:/exact"},
			message: func(p playerDiscoveryPresentation) tea.Msg {
				return playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true}
			},
		},
		{
			name: "podcast play", service: playerDiscoveryPodcast,
			item: kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "e", Path: "podcast:/episode"},
			message: func(p playerDiscoveryPresentation) tea.Msg {
				return playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0}
			},
		},
		{
			name: "podcast favorite", service: playerDiscoveryPodcast,
			item: kefw2.ContentItem{Type: kefw2.ContentTypeContainer, ID: "s", Path: "podcast:/series"},
			message: func(p playerDiscoveryPresentation) tea.Msg {
				return playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true}
			},
		},
		{
			name: "UPnP enqueue", service: playerDiscoveryUPnP,
			item: kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "u", Path: "upnp:/server/track"},
			message: func(p playerDiscoveryPresentation) tea.Msg {
				return playerDiscoveryEnqueueMsg{generation: p.generation, request: p.state.request, index: 0}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := playerDiscoveryUITestItemBackend(test.item)
			model, ui, _, _ := playerDiscoveryUITestConnectingModel(t, backend)
			command := playerDiscoveryUITestSend(model, playerDiscoveryServiceMsg{
				generation: model.generation, service: test.service,
			})
			if test.service == playerDiscoveryUPnP {
				playerDiscoveryUITestComplete(t, model, command)
			} else {
				playerDiscoveryUITestSearch(t, model, ui, "selection")
			}
			if model.connection != playerConnecting || ui.presentation().state.status != playerDiscoveryReady {
				t.Fatal("read-only browsing should work while the speaker is connecting")
			}
			playerDiscoveryUITestBlockedMutation(t, model, ui, test.message(ui.presentation()))
			playerDiscoveryUITestReady(model)
			playerDiscoveryUITestUnavailable(model)
			playerDiscoveryUITestBlockedMutation(t, model, ui, test.message(ui.presentation()))
			playerDiscoveryUITestNoWrites(t, backend.calls())
		})
	}
}

func TestPlayerDiscoveryUIRejectsIntentsAcrossReadinessEpochs(t *testing.T) {
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r1", Path: "radio:/exact"}
	backend := playerDiscoveryUITestItemBackend(station)
	model, ui, target, _ := playerDiscoveryUITestConnectingModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "station")
	p := ui.presentation()
	connecting := playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true}
	playerDiscoveryUITestReady(model)
	if !ui.presentation().writeReady || ui.presentation().state.request == connecting.request {
		t.Fatal("fresh status did not create a new write-ready request epoch")
	}
	if command := playerDiscoveryUITestSend(model, connecting); command != nil {
		t.Fatal("an intent issued before the first status was replayed")
	}
	p = ui.presentation()
	beforeDisconnect := playerDiscoveryFavoriteMsg{
		generation: p.generation, request: p.state.request, index: 0, add: true,
	}
	playerDiscoveryUITestUnavailable(model)
	playerDiscoveryUITestBlockedMutation(t, model, ui, beforeDisconnect)
	p = ui.presentation()
	offline := playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true}
	playerDiscoveryUITestBlockedMutation(t, model, ui, offline)
	playerDiscoveryUITestReady(model)
	for _, stale := range []playerDiscoveryFavoriteMsg{beforeDisconnect, offline} {
		if command := playerDiscoveryUITestSend(model, stale); command != nil {
			t.Fatal("a pre-reconnect intent was replayed")
		}
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
	p = ui.presentation()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true}))
	p = ui.presentation()
	if p.action == nil || !p.action.accepted || p.action.target != target {
		t.Fatalf("a new post-reconnect intent did not execute for the selected target: %+v", p.action)
	}
	var writes int
	for _, call := range backend.calls() {
		if call.name == "add-radio" {
			writes++
			if call.target != target || call.item.ID != station.ID || call.item.Path != station.Path {
				t.Fatalf("new intent lost its target or identity: %+v", call)
			}
		}
	}
	if writes != 1 {
		t.Fatalf("expected exactly one newly requested favorite, got %d", writes)
	}
}

func TestPlayerDiscoveryUIEventDisconnectCancelsQueuedMutationWithoutReplay(t *testing.T) {
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r1", Path: "radio:/exact"}
	backend := playerDiscoveryUITestItemBackend(station)
	model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "station")
	blocker := model.mutations.reserve()
	defer blocker.release()
	if err := blocker.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := ui.presentation()
	action := playerDiscoveryUITestSend(model,
		playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true})
	model.Update(playerScopedMsg{generation: model.generation, message: watchUpdate{
		kind: playerEventConnectionUpdate, err: errors.New("events disconnected"),
	}})
	p = ui.presentation()
	if model.connection != playerReady || !p.busy || p.action == nil || !p.action.pending || p.action.err == nil {
		t.Fatalf("disconnect did not surface the pending cancellation: %+v", p.action)
	}
	offline := playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true}
	playerDiscoveryUITestBlockedMutation(t, model, ui, offline)
	if followup := playerDiscoveryUITestComplete(t, model, action); followup != nil {
		t.Fatal("a mutation canceled before dispatch produced an uncertainty notice")
	}
	p = ui.presentation()
	if p.busy || p.action == nil || p.action.uncertain || p.action.err == nil ||
		!strings.Contains(p.action.err.Error(), "not sent") {
		t.Fatalf("unsent cancellation was not presented accurately: %+v", p.action)
	}
	playerDiscoveryUITestReady(model)
	blocker.release()
	if command := playerDiscoveryUITestSend(model, offline); command != nil {
		t.Fatal("offline mutation was replayed after reconnect")
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
	p = ui.presentation()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true}))
	var writes int
	for _, call := range backend.calls() {
		if call.name == "add-radio" {
			writes++
		}
	}
	if writes != 1 {
		t.Fatalf("expected only the explicitly re-requested favorite, got %d", writes)
	}
}

func TestPlayerDiscoveryUIRadioConfirmationExpiresOnDisconnect(t *testing.T) {
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r1", Path: "radio:/exact"}
	backend := playerDiscoveryUITestItemBackend(station)
	model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "station")
	p := ui.presentation()
	playerDiscoveryUITestSend(model,
		playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0})
	if ui.presentation().confirmation == nil {
		t.Fatal("radio play did not request confirmation")
	}
	confirm := playerDiscoveryConfirmMsg{generation: p.generation, request: p.state.request, accept: true}
	playerDiscoveryUITestUnavailable(model)
	if ui.presentation().confirmation != nil {
		t.Fatal("disconnect retained a destructive radio confirmation")
	}
	playerDiscoveryUITestReady(model)
	if command := playerDiscoveryUITestSend(model, confirm); command != nil {
		t.Fatal("an old radio confirmation survived reconnect")
	}
	p = ui.presentation()
	playerDiscoveryUITestSend(model,
		playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0})
	if ui.presentation().confirmation == nil {
		t.Fatal("a new radio request did not ask for a new confirmation")
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}

func TestPlayerDiscoveryUIPlaybackRequestsScopedStatusRefresh(t *testing.T) {
	tests := []struct {
		name        string
		service     playerDiscoveryService
		path        string
		queueChange bool
	}{
		{name: "radio", service: playerDiscoveryRadio, path: "radio:/exact", queueChange: true},
		{name: "podcast", service: playerDiscoveryPodcast, path: "podcast:/episode"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := playerDiscoveryUITestItemBackend(
				kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "track", Path: test.path})
			model, ui, _, b := playerDiscoveryUITestModel(t, backend)
			model.snapshot.Data.Source = kefw2.SourceBluetooth
			playerDiscoveryUITestSend(model, playerDiscoveryServiceMsg{
				generation: model.generation, service: test.service,
			})
			playerDiscoveryUITestSearch(t, model, ui, "selection")
			p := ui.presentation()
			command := playerDiscoveryUITestSend(model,
				playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0})
			if test.service == playerDiscoveryRadio {
				command = playerDiscoveryUITestSend(model,
					playerDiscoveryConfirmMsg{generation: p.generation, request: p.state.request, accept: true})
			}
			messages := playerDiscoveryUITestMessages(playerDiscoveryUITestComplete(t, model, command))
			status := playerDiscoveryUITestPlaybackRefreshes(t, messages, p.generation, test.queueChange)
			if ui.presentation().action == nil || !ui.presentation().action.accepted ||
				model.snapshot.Data.Source != kefw2.SourceBluetooth {
				t.Fatal("HTTP acceptance was treated as confirmed playback status")
			}
			session := model.session.(*fakePlayerSession)
			model.Update(status)
			sequence := session.refreshes.Load()
			if sequence == 0 || model.snapshot.Data.Source != kefw2.SourceBluetooth {
				t.Fatal("scoped refresh did not request a read without inferring the source")
			}
			model.Update(playerSessionMsg{generation: p.generation, message: playerStatusRefreshedResult{
				sequence: sequence, update: watchUpdate{
					kind: watchSnapshotUpdate, snapshot: statusSnapshot{Data: statusResponse{Source: kefw2.SourceWiFi}},
				},
			}})
			if model.snapshot.Data.Source != kefw2.SourceWiFi {
				t.Fatal("confirmed status read did not update the current speaker")
			}
			model.selectSpeaker(b, false)
			nextSession := model.session.(*fakePlayerSession)
			if _, command = model.Update(status); command != nil || model.snapshot != nil ||
				model.connection != playerConnecting || nextSession.refreshes.Load() != 0 {
				t.Fatal("old-target status refresh affected the newly selected speaker")
			}
		})
	}
}

func playerDiscoveryUITestPlaybackRefreshes(
	t *testing.T, messages []tea.Msg, generation uint64, queueChange bool,
) tea.Msg {
	t.Helper()
	expected := 1
	if queueChange {
		expected++
	}
	if len(messages) != expected {
		t.Fatalf("playback produced unexpected follow-up messages: %#v", messages)
	}
	var status tea.Msg
	var queue bool
	for _, message := range messages {
		scoped, ok := message.(playerScopedMsg)
		if !ok {
			t.Fatalf("playback follow-up was not scoped: %#v", message)
		}
		switch scoped.message.(type) {
		case playerDiscoveryQueueChangedMsg:
			playerDiscoveryUITestQueueRefresh(t, scoped, generation)
			queue = true
		case playerStatusRefreshRequest:
			playerDiscoveryUITestStatusRefresh(t, scoped, generation)
			status = scoped
		}
	}
	if status == nil || queue != queueChange {
		t.Fatalf("playback omitted a required status or queue refresh: %#v", messages)
	}
	return status
}

func TestPlayerDiscoveryUIFavoriteDoesNotRequestPlaybackStatus(t *testing.T) {
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r", Path: "radio:/exact"}
	backend := playerDiscoveryUITestItemBackend(station)
	model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "station")
	p := ui.presentation()
	followup := playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true}))
	if followup != nil || ui.presentation().action == nil || !ui.presentation().action.accepted {
		t.Fatal("favorite changed playback status or failed")
	}
}

func TestPlayerDiscoveryUIConnectionEventsRequireAnotherSnapshot(t *testing.T) {
	tests := []struct {
		name    string
		message tea.Msg
	}{
		{
			name: "unavailable status",
			message: watchUpdate{
				kind: playerStatusUnavailableUpdate, err: errors.New("status lost"),
			},
		},
		{
			name: "event stream disconnect",
			message: watchUpdate{
				kind: playerEventConnectionUpdate, err: errors.New("events lost"),
			},
		},
		{name: "session failure", message: playerSessionFailure{err: errors.New("session lost")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := playerDiscoveryUITestItemBackend(
				kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r", Path: "radio:/exact"})
			model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
			playerDiscoveryUITestSearch(t, model, ui, "station")
			model.Update(playerScopedMsg{generation: model.generation, message: test.message})
			p := ui.presentation()
			if p.writeReady {
				t.Fatal("connection loss did not revoke write readiness")
			}
			playerDiscoveryUITestBlockedMutation(t, model, ui, playerDiscoveryFavoriteMsg{
				generation: p.generation, request: p.state.request, index: 0, add: true,
			})
			model.Update(playerScopedMsg{generation: model.generation, message: watchUpdate{
				kind: playerEventConnectionUpdate,
			}})
			if ui.presentation().writeReady {
				t.Fatal("event connection alone revived stale status")
			}
			model.Update(playerScopedMsg{generation: model.generation, message: watchUpdate{
				kind: playerEventConnectionUpdate, err: errors.New("events still unavailable"),
			}})
			playerDiscoveryUITestReady(model)
			if !ui.presentation().writeReady || model.eventErr == nil {
				t.Fatal("a fresh status poll did not restore readiness while events were unavailable")
			}
			playerDiscoveryUITestNoWrites(t, backend.calls())
		})
	}
}

func TestPlayerDiscoveryUIStatusMustMatchTheCurrentGeneration(t *testing.T) {
	backend := playerDiscoveryUITestItemBackend(
		kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r", Path: "radio:/exact"})
	model, ui, _, b := playerDiscoveryUITestModel(t, backend)
	old := model.generation
	model.selectSpeaker(b, false)
	model.Update(playerScopedMsg{generation: old, message: watchUpdate{kind: watchSnapshotUpdate}})
	model.Update(watchUpdate{kind: watchSnapshotUpdate})
	playerDiscoveryUITestSend(model, playerDiscoveryFavoriteMsg{
		generation: old, request: playerDiscoveryRequestID{generation: old, sequence: 1}, index: 0, add: true,
	})
	if ui.presentation().writeReady {
		t.Fatal("retired or unscoped status enabled writes for a new speaker")
	}
	if ui.presentation().action != nil {
		t.Fatal("retired mutation intent contaminated the new speaker's presentation")
	}
	model.Update(playerScopedMsg{generation: model.generation, message: watchUpdate{
		kind: watchSnapshotUpdate, err: errors.New("invalid snapshot"),
	}})
	if ui.presentation().writeReady {
		t.Fatal("an errored snapshot enabled writes")
	}
	playerDiscoveryUITestReady(model)
	model.Update(playerScopedMsg{generation: old, message: watchUpdate{
		kind: playerStatusUnavailableUpdate, err: errors.New("old failure"),
	}})
	if !ui.presentation().writeReady || ui.presentation().target != b {
		t.Fatal("retired status revoked the current speaker's fresh status")
	}
}

func TestPlayerDiscoveryUISessionFailureClearsConfirmationAfterContextCancellation(t *testing.T) {
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r", Path: "radio:/exact"}
	backend := playerDiscoveryUITestItemBackend(station)
	model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "station")
	p := ui.presentation()
	playerDiscoveryUITestSend(model,
		playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0})
	if ui.presentation().confirmation == nil {
		t.Fatal("expected a pending radio confirmation")
	}
	model.session.cancel()
	model.Update(playerScopedMsg{generation: model.generation, message: playerSessionFailure{
		err: errors.New("status watcher stopped"),
	}})
	p = ui.presentation()
	if p.writeReady || p.confirmation != nil {
		t.Fatalf("canceled session retained a destructive confirmation: %+v", p.confirmation)
	}
	playerDiscoveryUITestReady(model)
	if ui.presentation().writeReady {
		t.Fatal("snapshot revived a canceled session")
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}

func TestPlayerDiscoveryUICanceledSessionVisiblyRejectsMutation(t *testing.T) {
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r", Path: "radio:/exact"}
	backend := playerDiscoveryUITestItemBackend(station)
	model, ui, target, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "station")
	p := ui.presentation()
	model.session.cancel()
	command := playerDiscoveryUITestSend(model,
		playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true})
	p = ui.presentation()
	if command != nil || p.writeReady || p.action == nil || p.action.target != target ||
		p.action.err == nil || !strings.Contains(p.action.err.Error(), "session") {
		t.Fatalf("canceled-session mutation was not visibly rejected: %+v", p.action)
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}

func TestPlayerDiscoveryUISessionFailureKeepsTheOriginalUncertainTarget(t *testing.T) {
	started, releaseMutation := make(chan struct{}), make(chan struct{})
	var release sync.Once
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r", Path: "radio:/exact"}
	backend := playerDiscoveryUITestItemBackend(station)
	backend.mutate = func(context.Context, playerSpeaker, string, kefw2.ContentItem) error {
		close(started)
		<-releaseMutation
		return nil
	}
	model, ui, target, _ := playerDiscoveryUITestModel(t, backend)
	t.Cleanup(func() { release.Do(func() { close(releaseMutation) }) })
	playerDiscoveryUITestSearch(t, model, ui, "station")
	p := ui.presentation()
	command := playerDiscoveryUITestSend(model,
		playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true})
	playerDiscoveryUITestWait(t, started)
	model.session.cancel()
	if another := playerDiscoveryUITestSend(model,
		playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true}); another != nil {
		t.Fatal("canceled session scheduled another mutation")
	}
	if p = ui.presentation(); p.action == nil || !p.action.pending || p.action.target != target ||
		p.action.err == nil || !strings.Contains(p.action.err.Error(), "session") {
		t.Fatalf("rejection lost the in-flight action or its target: %+v", p.action)
	}
	model.Update(playerScopedMsg{generation: model.generation, message: playerSessionFailure{
		err: errors.New("status watcher stopped"),
	}})
	release.Do(func() { close(releaseMutation) })
	messages := playerDiscoveryUITestMessages(playerDiscoveryUITestComplete(t, model, command))
	if len(messages) != 1 {
		t.Fatalf("stopped session did not report dispatched uncertainty: %#v", messages)
	}
	notice, ok := messages[0].(playerNoticeMsg)
	if !ok || notice.target != target || !errors.Is(notice.err, context.Canceled) ||
		!strings.Contains(notice.err.Error(), "session") || strings.Contains(notice.err.Error(), "switching") {
		t.Fatalf("stopped session notice was inaccurate: %#v", messages[0])
	}
}

func TestPlayerDiscoveryUIFailsClosedIfModelStatusBecomesStale(t *testing.T) {
	tests := []struct {
		name  string
		stale func(*playerModel)
	}{
		{name: "connecting", stale: func(m *playerModel) { m.connection = playerConnecting }},
		{name: "unavailable", stale: func(m *playerModel) { m.connection = playerUnavailable }},
		{name: "read error", stale: func(m *playerModel) { m.readErr = errors.New("stale") }},
		{name: "missing snapshot", stale: func(m *playerModel) { m.snapshot = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := playerDiscoveryUITestItemBackend(
				kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r", Path: "radio:/exact"})
			model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
			playerDiscoveryUITestSearch(t, model, ui, "station")
			p := ui.presentation()
			test.stale(model)
			playerDiscoveryUITestBlockedMutation(t, model, ui, playerDiscoveryFavoriteMsg{
				generation: p.generation, request: p.state.request, index: 0, add: true,
			})
			playerDiscoveryUITestNoWrites(t, backend.calls())
		})
	}
}

func TestPlayerDiscoveryUIReadSurvivesStatusEpochChangesAndCanRetry(t *testing.T) {
	started, releaseRead := make(chan struct{}), make(chan struct{})
	var release sync.Once
	var attempts atomic.Int32
	readError := errors.New("search failed")
	backend := &playerDiscoveryUITestBackend{}
	backend.search = func(context.Context, playerSpeaker, playerDiscoveryService, string, int, int) (
		*kefw2.RowsResponse, error,
	) {
		if attempts.Add(1) == 1 {
			close(started)
			<-releaseRead
			return nil, readError
		}
		return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{{
			Type: kefw2.ContentTypeAudio, Path: "radio:/new",
		}}}, nil
	}
	model, ui, _, _ := playerDiscoveryUITestConnectingModel(t, backend)
	t.Cleanup(func() { release.Do(func() { close(releaseRead) }) })
	search := playerDiscoveryUITestSend(model,
		playerDiscoverySearchMsg{generation: model.generation, query: "station"})
	playerDiscoveryUITestWait(t, started)
	request := ui.presentation().state.request
	playerDiscoveryUITestReady(model)
	playerDiscoveryUITestUnavailable(model)
	playerDiscoveryUITestReady(model)
	p := ui.presentation()
	if p.state.request == request || p.state.status != playerDiscoveryLoading {
		t.Fatalf("status transitions did not preserve the active read: %+v", p.state)
	}
	release.Do(func() { close(releaseRead) })
	playerDiscoveryUITestComplete(t, model, search)
	p = ui.presentation()
	if p.state.status != playerDiscoveryError || !p.canRetry || !errors.Is(p.state.err, readError) {
		t.Fatalf("the failed in-flight read was not retryable: %+v", p.state)
	}
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryRetryMsg{generation: p.generation, request: p.state.request}))
	p = ui.presentation()
	if p.state.status != playerDiscoveryReady || p.state.page.items[0].Path != "radio:/new" || attempts.Load() != 2 {
		t.Fatalf("retry after status changes did not use the original read: %+v", p.state)
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}

func TestPlayerDiscoveryUIStatusLossCancelsPreflightWithoutUncertainty(t *testing.T) {
	started := make(chan struct{})
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r", Path: "radio:/exact"}
	backend := playerDiscoveryUITestItemBackend(station)
	backend.resolve = func(ctx context.Context, _ playerSpeaker, _ playerDiscoveryService, _ string) (
		*kefw2.ContentItem, error,
	) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	model, ui, target, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "station")
	command := playerDiscoveryUITestConfirmRadio(t, model, ui, backend, station, target)
	playerDiscoveryUITestWait(t, started)
	playerDiscoveryUITestUnavailable(model)
	if followup := playerDiscoveryUITestComplete(t, model, command); followup != nil {
		t.Fatal("preflight cancellation produced a false write uncertainty")
	}
	if p := ui.presentation(); p.action == nil || p.action.uncertain || p.action.err == nil ||
		!strings.Contains(p.action.err.Error(), "not sent") {
		t.Fatalf("preflight cancellation was not shown as unsent: %+v", p.action)
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}

func TestPlayerDiscoveryUISourceDisconnectNeverContinuesPlayback(t *testing.T) {
	started := make(chan struct{})
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r", Path: "radio:/exact"}
	backend := playerDiscoveryUITestItemBackend(station)
	backend.source = func(context.Context, playerSpeaker) (kefw2.Source, error) {
		return kefw2.SourceBluetooth, nil
	}
	backend.set = func(ctx context.Context, _ playerSpeaker, _ kefw2.Source) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	model, ui, target, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "station")
	command := playerDiscoveryUITestConfirmRadio(t, model, ui, backend, station, target)
	playerDiscoveryUITestWait(t, started)
	playerDiscoveryUITestUnavailable(model)
	followup := playerDiscoveryUITestComplete(t, model, command)
	messages := playerDiscoveryUITestMessages(followup)
	playerDiscoveryUITestSourceNotice(t, messages, ui.presentation().generation, target, context.Canceled)
	if p := ui.presentation(); p.action == nil || !p.action.uncertain || p.action.target != target {
		t.Fatalf("in-flight source change was not shown as uncertain: %+v", p.action)
	}
	session := model.session.(*fakePlayerSession)
	for _, message := range messages {
		if _, ok := message.(playerScopedMsg); ok {
			model.Update(message)
		}
	}
	sequence := session.refreshes.Load()
	if sequence == 0 || ui.presentation().writeReady {
		t.Fatal("requesting a refresh revived writes without a confirmed snapshot")
	}
	model.Update(playerSessionMsg{generation: model.generation, message: playerStatusRefreshedResult{
		sequence: sequence, update: watchUpdate{kind: watchSnapshotUpdate},
	}})
	if !ui.presentation().writeReady {
		t.Fatal("fresh status read did not restore readiness after the source uncertainty")
	}
	for _, call := range backend.calls() {
		if call.name == "play-radio" {
			t.Fatalf("play was sent after a canceled source change: %+v", call)
		}
	}
}

func TestPlayerDiscoveryUIInFlightDisconnectReportsUncertaintyAfterReconnect(t *testing.T) {
	started, releasePlay := make(chan struct{}), make(chan struct{})
	var release sync.Once
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r", Path: "radio:/exact"}
	backend := playerDiscoveryUITestItemBackend(station)
	backend.mutate = func(context.Context, playerSpeaker, string, kefw2.ContentItem) error {
		close(started)
		<-releasePlay
		return nil
	}
	model, ui, target, _ := playerDiscoveryUITestModel(t, backend)
	t.Cleanup(func() { release.Do(func() { close(releasePlay) }) })
	playerDiscoveryUITestSearch(t, model, ui, "station")
	command := playerDiscoveryUITestConfirmRadio(t, model, ui, backend, station, target)
	playerDiscoveryUITestWait(t, started)
	model.Update(playerScopedMsg{generation: model.generation, message: watchUpdate{
		kind: playerEventConnectionUpdate, err: errors.New("events disconnected"),
	}})
	p := ui.presentation()
	if !p.busy || p.action == nil || p.action.target != target || p.action.err == nil {
		t.Fatalf("disconnect lost the pending target or status: %+v", p.action)
	}
	playerDiscoveryUITestBlockedMutation(t, model, ui,
		playerDiscoveryPlayMsg{generation: p.generation, request: p.state.request, index: 0})
	playerDiscoveryUITestReady(model)
	release.Do(func() { close(releasePlay) })
	messages := playerDiscoveryUITestMessages(playerDiscoveryUITestComplete(t, model, command))
	playerDiscoveryUITestUncertainRadio(t, messages, target, ui.presentation().generation)
	p = ui.presentation()
	if p.busy || p.action == nil || !p.action.uncertain || p.action.accepted || p.action.target != target {
		t.Fatalf("old-epoch completion appeared as a normal success: %+v", p.action)
	}
	var plays int
	for _, call := range backend.calls() {
		if call.name == "play-radio" {
			plays++
			if call.target != target || call.item.Path != station.Path {
				t.Fatalf("radio write changed target or item: %+v", call)
			}
		}
	}
	if plays != 1 {
		t.Fatalf("radio play was replayed after reconnect: %d writes", plays)
	}
}

func playerDiscoveryUITestUncertainRadio(t *testing.T, messages []tea.Msg, target playerSpeaker, generation uint64) {
	t.Helper()
	if len(messages) != 3 {
		t.Fatalf("uncertain radio write should refresh queue/status and notify: %#v", messages)
	}
	var queue, status, noticed bool
	for _, message := range messages {
		switch notice := message.(type) {
		case playerScopedMsg:
			switch notice.message.(type) {
			case playerDiscoveryQueueChangedMsg:
				playerDiscoveryUITestQueueRefresh(t, notice, generation)
				queue = true
			case playerStatusRefreshRequest:
				playerDiscoveryUITestStatusRefresh(t, notice, generation)
				status = true
			}
		case playerNoticeMsg:
			if notice.target != target || !errors.Is(notice.err, context.Canceled) ||
				!strings.Contains(notice.err.Error(), "status") || strings.Contains(notice.err.Error(), "switching") {
				t.Fatalf("status-loss notice was inaccurate: %+v", notice)
			}
			noticed = true
		}
	}
	if !queue || !status || !noticed {
		t.Fatalf("missing queue/status refresh or captured-target notice: %#v", messages)
	}
}

func TestPlayerDiscoveryUIUndeliveredSuccessAfterStatusLossIsUncertain(t *testing.T) {
	station := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r", Path: "radio:/exact"}
	backend := playerDiscoveryUITestItemBackend(station)
	model, ui, target, _ := playerDiscoveryUITestModel(t, backend)
	playerDiscoveryUITestSearch(t, model, ui, "station")
	p := ui.presentation()
	command := playerDiscoveryUITestSend(model,
		playerDiscoveryFavoriteMsg{generation: p.generation, request: p.state.request, index: 0, add: true})
	if command == nil {
		t.Fatal("favorite request was not scheduled")
	}
	message := command()
	result, ok := message.(playerDiscoveryUIActionResult)
	if !ok || !result.dispatched || result.err != nil {
		t.Fatalf("expected a successful dispatched result, got %#v", message)
	}
	playerDiscoveryUITestUnavailable(model)
	playerDiscoveryUITestReady(model)
	_, followup := model.Update(message)
	messages := playerDiscoveryUITestMessages(followup)
	if len(messages) != 1 {
		t.Fatalf("old-epoch success did not produce an uncertainty notice: %#v", messages)
	}
	notice, ok := messages[0].(playerNoticeMsg)
	if !ok || notice.target != target || notice.err == nil || !strings.Contains(notice.err.Error(), "status") ||
		strings.Contains(notice.err.Error(), "%!w") {
		t.Fatalf("successful but undelivered result lost its uncertainty: %#v", messages[0])
	}
	if p = ui.presentation(); p.action == nil || !p.action.uncertain || p.action.accepted {
		t.Fatalf("old-epoch success was incorrectly accepted: %+v", p.action)
	}
}

func TestPlayerDiscoveryUIRevokedPermitBlocksAnUndispatchedWrite(t *testing.T) {
	backend := &playerDiscoveryUITestBackend{}
	target := playerSpeaker{Name: "Living", IPAddress: "10.0.0.1"}
	ctx := context.Background()
	client, source := backend.factory(ctx, target)
	permit := &playerDiscoveryUIWritePermit{active: true}
	tracked := &playerDiscoveryUIMutation{
		playerDiscoveryUIClient: client, source: source, ctx: ctx, permit: permit,
	}
	item := kefw2.ContentItem{Type: kefw2.ContentTypeAudio, ID: "r", Path: "radio:/exact"}
	started := make(chan struct{})
	result := make(chan error, 1)
	permit.mu.Lock()
	go func() {
		close(started)
		result <- tracked.AddRadioFavorite(&item)
	}()
	playerDiscoveryUITestWait(t, started)
	permit.active = false
	permit.mu.Unlock()
	var err error
	select {
	case err = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("revoked permit did not unblock the mutation")
	}
	if !errors.Is(err, context.Canceled) || tracked.dispatched {
		t.Fatalf("revoked permit dispatched a pending write: sent=%v err=%v", tracked.dispatched, err)
	}
	playerDiscoveryUITestNoWrites(t, backend.calls())
}
