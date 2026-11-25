// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hilli/go-kef-w2/kefw2"
)

func init() {
	newPlayerUPnPLibrary = func() playerUPnPLibrary { return (&playerUPnPTestLibrary{}).library() }
}

type playerUPnPTestLibrary struct {
	mu     sync.Mutex
	cfg    playerUPnPConfig
	index  *kefw2.TrackIndex
	saves  int
	builds []playerUPnPConfig
	build  func(context.Context, playerUPnPConfig, kefw2.IndexProgress) (*kefw2.TrackIndex, error)
}

func (l *playerUPnPTestLibrary) library() playerUPnPLibrary {
	return playerUPnPLibrary{
		config: func() playerUPnPConfig {
			l.mu.Lock()
			defer l.mu.Unlock()
			return l.cfg
		},
		setServer: func(title, path string) error {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.cfg.server, l.cfg.path = title, path
			return nil
		},
		load: func() (*kefw2.TrackIndex, error) {
			l.mu.Lock()
			defer l.mu.Unlock()
			return l.index, nil
		},
		build: func(
			ctx context.Context, _ playerSpeaker, cfg playerUPnPConfig, progress kefw2.IndexProgress,
		) (*kefw2.TrackIndex, error) {
			l.mu.Lock()
			l.builds = append(l.builds, cfg)
			build := l.build
			l.mu.Unlock()
			if build == nil {
				return nil, errors.New("no test build configured")
			}
			return build(ctx, cfg, progress)
		},
		save: func(index *kefw2.TrackIndex) error {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.index = index
			l.saves++
			return nil
		},
	}
}

func (l *playerUPnPTestLibrary) setBuild(
	build func(context.Context, playerUPnPConfig, kefw2.IndexProgress) (*kefw2.TrackIndex, error),
) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.build = build
}

func (l *playerUPnPTestLibrary) savedCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.saves
}

func playerUPnPTestIndex(cfg playerUPnPConfig, titles ...string) *kefw2.TrackIndex {
	index := &kefw2.TrackIndex{
		ServerName: cfg.server, ServerPath: cfg.path, IndexedAt: time.Now(), TrackCount: len(titles),
	}
	for i, title := range titles {
		index.Tracks = append(index.Tracks, kefw2.IndexedTrack{
			Title: title, Path: fmt.Sprintf("upnp:/track/%d", i), SearchField: strings.ToLower(title),
		})
	}
	return index
}

// playerUPnPTestCommands expands batches; a command still blocked after a short
// wait is a worker and is returned as its pending result channel.
func playerUPnPTestCommands(t *testing.T, command tea.Cmd) []<-chan tea.Msg {
	t.Helper()
	if command == nil {
		return nil
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- command() }()
	select {
	case msg := <-result:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var all []<-chan tea.Msg
			for _, cmd := range batch {
				all = append(all, playerUPnPTestCommands(t, cmd)...)
			}
			return all
		}
		done := make(chan tea.Msg, 1)
		done <- msg
		return []<-chan tea.Msg{done}
	case <-time.After(time.Second):
		return []<-chan tea.Msg{result}
	}
}

func playerUPnPTestAwait(t *testing.T, result <-chan tea.Msg) tea.Msg {
	t.Helper()
	select {
	case msg := <-result:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("UPnP command did not finish")
		return nil
	}
}

func playerUPnPTestModel(
	t *testing.T, lib *playerUPnPTestLibrary, rows []kefw2.ContentItem,
) (*playerModel, *playerDiscoveryUI) {
	t.Helper()
	backend := &playerDiscoveryUITestBackend{}
	backend.rows = func(_ context.Context, _ playerSpeaker, path string, _, _ int) (*kefw2.RowsResponse, error) {
		if path != playerDiscoveryUPnPRootPath {
			return nil, fmt.Errorf("unexpected browse path %q", path)
		}
		return &kefw2.RowsResponse{RowsCount: len(rows), Rows: rows}, nil
	}
	model, ui, _, _ := playerDiscoveryUITestModel(t, backend)
	ui.library = lib.library()
	p := ui.presentation()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoveryServiceMsg{generation: p.generation, service: playerDiscoveryUPnP}))
	return model, ui
}

func TestPlayerUPnPSetupChoosesServerBuildsIndexAndSearches(t *testing.T) {
	plex := kefw2.ContentItem{Title: "Plex", Type: kefw2.ContentTypeContainer, Path: "upnp:/plex?itemType=server"}
	minim := kefw2.ContentItem{Title: "Minim", Type: kefw2.ContentTypeContainer, Path: "upnp:/minim?itemType=server"}
	query := kefw2.ContentItem{Title: "Search", Type: "query", Path: "upnp:/search"}
	lib := &playerUPnPTestLibrary{}
	model, ui := playerUPnPTestModel(t, lib, []kefw2.ContentItem{plex, query, minim})
	p := ui.presentation()
	if p.upnp.phase != playerUPnPNoServer || p.searchable() {
		t.Fatalf("unconfigured UPnP did not ask for a server: %+v", p.upnp)
	}

	playerDiscoveryUITestSend(model, playerUPnPUseServerMsg{generation: p.generation, request: p.state.request, index: 1})
	if p = ui.presentation(); p.upnp.config.path != "" || p.action == nil || p.action.err == nil {
		t.Fatalf("a non-server row was accepted as the server: %+v", p.upnp.config)
	}
	playerDiscoveryUITestSend(model, playerUPnPUseServerMsg{generation: p.generation, request: p.state.request, index: 2})
	if p = ui.presentation(); p.upnp.phase != playerUPnPNoIndex || p.upnp.config.path != minim.Path {
		t.Fatalf("server choice was not saved: %+v", p.upnp)
	}
	p = playerUPnPTestBuildIndex(t, model, ui, lib)
	playerUPnPTestSearchPages(t, model, ui, p)
}

func playerUPnPTestBuildIndex(
	t *testing.T, model *playerModel, ui *playerDiscoveryUI, lib *playerUPnPTestLibrary,
) playerDiscoveryPresentation {
	t.Helper()
	p := ui.presentation()
	release := make(chan struct{})
	titles := make([]string, 30)
	for i := range titles {
		titles[i] = fmt.Sprintf("Blue %02d", i)
	}
	lib.setBuild(func(ctx context.Context, cfg playerUPnPConfig, progress kefw2.IndexProgress) (*kefw2.TrackIndex, error) {
		progress(3, 120, "Music")
		select {
		case <-release:
			return playerUPnPTestIndex(cfg, titles...), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	commands := playerUPnPTestCommands(t, playerDiscoveryUITestSend(model, playerUPnPIndexMsg{generation: p.generation}))
	if len(commands) != 2 {
		t.Fatalf("index start returned %d commands, want worker and tick", len(commands))
	}
	if p = ui.presentation(); p.upnp.phase != playerUPnPIndexing {
		t.Fatalf("index did not start: %+v", p.upnp)
	}
	if next := playerDiscoveryUITestSend(model, playerUPnPTickMsg{}); next != nil {
		t.Fatal("tick from another generation was accepted")
	}
	tick := playerUPnPTestAwait(t, commands[1])
	if _, next := model.Update(tick); next == nil {
		t.Fatal("indexing tick did not schedule the next frame")
	}
	if p = ui.presentation(); p.upnp.tracks != 120 || p.upnp.containers != 3 || p.upnp.frame != 1 {
		t.Fatalf("progress not visible: %+v", p.upnp)
	}
	close(release)
	model.Update(playerUPnPTestAwait(t, commands[0]))
	if p = ui.presentation(); p.upnp.phase != playerUPnPReady || !p.searchable() || lib.savedCount() != 1 {
		t.Fatalf("finished index is not ready: %+v", p.upnp)
	}
	if next := playerDiscoveryUITestSend(model, tick); next != nil {
		t.Fatal("tick kept running after the index finished")
	}
	return p
}

func playerUPnPTestSearchPages(
	t *testing.T, model *playerModel, ui *playerDiscoveryUI, p playerDiscoveryPresentation,
) {
	t.Helper()
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model,
		playerDiscoverySearchMsg{generation: p.generation, query: "blue"}))
	p = ui.presentation()
	if p.state.status != playerDiscoveryReady || len(p.state.page.items) != playerDiscoveryDefaultPageSize ||
		!p.state.page.hasMore || !playerDiscoveryCanEnqueue(p, 0) {
		t.Fatalf("UPnP search did not return enqueueable results: %+v", p.state)
	}
	playerDiscoveryUITestComplete(t, model, playerDiscoveryUITestSend(model, playerDiscoveryPageMsg{
		generation: p.generation, request: p.state.request, offset: p.state.page.nextOffset,
	}))
	if p = ui.presentation(); len(p.state.page.items) != 6 || p.state.page.hasMore || !p.hasPrevious {
		t.Fatalf("UPnP search did not page locally: %+v", p.state.page)
	}
}

func TestPlayerUPnPIndexCancelFailureAndRetry(t *testing.T) {
	cfg := playerUPnPConfig{server: "Plex", path: "upnp:/plex?itemType=server"}
	lib := &playerUPnPTestLibrary{cfg: cfg}
	model, ui := playerUPnPTestModel(t, lib, nil)
	p := ui.presentation()
	if p.upnp.phase != playerUPnPNoIndex {
		t.Fatalf("configured server without index: %+v", p.upnp)
	}

	lib.setBuild(func(ctx context.Context, _ playerUPnPConfig, _ kefw2.IndexProgress) (*kefw2.TrackIndex, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	commands := playerUPnPTestCommands(t, playerDiscoveryUITestSend(model, playerUPnPIndexMsg{generation: p.generation}))
	playerDiscoveryUITestSend(model, playerUPnPCancelIndexMsg{generation: p.generation})
	model.Update(playerUPnPTestAwait(t, commands[0]))
	if p = ui.presentation(); p.upnp.phase != playerUPnPCanceled || lib.savedCount() != 0 {
		t.Fatalf("cancel did not stop indexing: %+v", p.upnp)
	}

	lib.setBuild(func(context.Context, playerUPnPConfig, kefw2.IndexProgress) (*kefw2.TrackIndex, error) {
		return nil, errors.New("server went away")
	})
	commands = playerUPnPTestCommands(t, playerDiscoveryUITestSend(model, playerUPnPIndexMsg{generation: p.generation}))
	model.Update(playerUPnPTestAwait(t, commands[0]))
	if p = ui.presentation(); p.upnp.phase != playerUPnPFailed || !strings.Contains(p.upnp.err.Error(), "went away") {
		t.Fatalf("failure not reported: %+v", p.upnp)
	}

	lib.setBuild(func(_ context.Context, cfg playerUPnPConfig, _ kefw2.IndexProgress) (*kefw2.TrackIndex, error) {
		return playerUPnPTestIndex(cfg, "Track"), nil
	})
	commands = playerUPnPTestCommands(t, playerDiscoveryUITestSend(model, playerUPnPIndexMsg{generation: p.generation}))
	model.Update(playerUPnPTestAwait(t, commands[0]))
	if p = ui.presentation(); p.upnp.phase != playerUPnPReady || p.upnp.summary.tracks != 1 {
		t.Fatalf("retry did not recover: %+v", p.upnp)
	}
}

func TestPlayerUPnPReadyLibraryEnablesSearchKeys(t *testing.T) {
	m := newPlayerModel(context.Background(), playerDiscoveryBrowser, playerSelection{}, nil)
	m.generation = 3
	m.ui = newPlayerLayoutUI(playerDiscoveryBrowser, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.discovery = newPlayerDiscoveryUI()
	d := m.ui.discovery
	d.service, d.generation, d.hasTarget = playerDiscoveryUPnP, 3, true
	cfg := playerUPnPConfig{server: "Plex", path: "upnp:/plex"}
	d.upnp = playerUPnPSetup{config: cfg, checked: true, summary: playerUPnPSummarize(playerUPnPTestIndex(cfg, "A"))}
	m.ui.pane = playerPaneDiscover
	m.ui.focus.Set(playerPaneDiscover)
	m.ui.zoom = playerPaneDiscover
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if content := ansi.Strip(m.View().Content); !strings.Contains(content, "✓ LIBRARY Plex · 1 tracks") {
		t.Fatalf("ready library not shown: %q", content)
	}
	_, command := m.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
	messages := playerLayoutTestMessages(command)
	if len(messages) != 1 {
		t.Fatalf("I produced %d messages", len(messages))
	}
	if scoped, ok := messages[0].(playerScopedMsg); !ok || scoped.message != (playerUPnPIndexMsg{generation: 3}) {
		t.Fatalf("I did not request an index build: %#v", messages[0])
	}
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !m.ui.searchActive {
		t.Fatal("/ did not open search for an indexed UPnP library")
	}
}

func TestPlayerUPnPIndexForAnotherServerIsNotSearchable(t *testing.T) {
	cfg := playerUPnPConfig{server: "Plex", path: "upnp:/plex"}
	other := playerUPnPConfig{server: "Minim", path: "upnp:/minim"}
	setup := playerUPnPSetup{config: cfg, checked: true, summary: playerUPnPSummarize(playerUPnPTestIndex(other, "A"))}
	if p := setup.presentation(); p.phase != playerUPnPOtherServer || p.searchable {
		t.Fatalf("index for another server: phase %v searchable %v", p.phase, p.searchable)
	}
}
