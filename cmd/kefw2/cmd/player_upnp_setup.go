// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/viper"

	"github.com/hilli/go-kef-w2/kefw2"
)

const (
	playerUPnPSearchMax    = 500
	playerUPnPTickInterval = 120 * time.Millisecond
)

type playerUPnPConfig struct {
	server, path, container string
}

// playerUPnPLibrary is the boundary to the configuration file and the on-disk
// track index; tests replace it so they never touch real user state.
type playerUPnPLibrary struct {
	config    func() playerUPnPConfig
	setServer func(title, path string) error
	load      func() (*kefw2.TrackIndex, error)
	build     func(context.Context, playerSpeaker, playerUPnPConfig, kefw2.IndexProgress) (*kefw2.TrackIndex, error)
	save      func(*kefw2.TrackIndex) error
}

// newPlayerUPnPLibrary is replaced in tests so they never read or write user files.
var newPlayerUPnPLibrary = playerUPnPDefaultLibrary

func playerUPnPDefaultLibrary() playerUPnPLibrary {
	return playerUPnPLibrary{
		config: func() playerUPnPConfig {
			return playerUPnPConfig{
				server:    viper.GetString("upnp.default_server"),
				path:      viper.GetString("upnp.default_server_path"),
				container: viper.GetString("upnp.index_container"),
			}
		},
		setServer: setDefaultUPnPServer,
		load:      kefw2.LoadTrackIndexCached,
		build: func(
			ctx context.Context, target playerSpeaker, cfg playerUPnPConfig, progress kefw2.IndexProgress,
		) (*kefw2.TrackIndex, error) {
			client := kefw2.NewAirableClient(target.newSpeaker(), kefw2.WithContext(ctx),
				kefw2.WithHTTPTimeout(60*time.Second))
			return kefw2.BuildTrackIndex(client, cfg.path, cfg.server, cfg.container, progress)
		},
		save: kefw2.SaveTrackIndex,
	}
}

type playerUPnPIndexSummary struct {
	server, path, container string
	tracks                  int
	indexedAt               time.Time
}

func playerUPnPSummarize(index *kefw2.TrackIndex) *playerUPnPIndexSummary {
	if index == nil {
		return nil
	}
	container := index.ContainerName
	if container == "" {
		container = index.ContainerPath
	}
	return &playerUPnPIndexSummary{
		server: index.ServerName, path: index.ServerPath, container: container,
		tracks: index.TrackCount, indexedAt: index.IndexedAt,
	}
}

// playerUPnPProgress is written by the indexing worker and read on the Tea loop.
type playerUPnPProgress struct {
	mu                 sync.Mutex
	containers, tracks int
	current            string
}

func (p *playerUPnPProgress) report(containers, tracks int, current string) {
	p.mu.Lock()
	p.containers, p.tracks, p.current = containers, tracks, current
	p.mu.Unlock()
}

func (p *playerUPnPProgress) snapshot() (int, int, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.containers, p.tracks, p.current
}

type playerUPnPSetup struct {
	config   playerUPnPConfig
	checked  bool
	summary  *playerUPnPIndexSummary
	loadErr  error
	job      uint64
	indexing bool
	cancel   context.CancelFunc
	progress *playerUPnPProgress
	started  time.Time
	elapsed  time.Duration
	frame    int
	err      error
	canceled bool
}

type playerUPnPPhase string

const (
	playerUPnPChecking    playerUPnPPhase = "checking"
	playerUPnPNoServer    playerUPnPPhase = "no-server"
	playerUPnPNoIndex     playerUPnPPhase = "no-index"
	playerUPnPOtherServer playerUPnPPhase = "other-server"
	playerUPnPIndexing    playerUPnPPhase = "indexing"
	playerUPnPFailed      playerUPnPPhase = "failed"
	playerUPnPCanceled    playerUPnPPhase = "canceled"
	playerUPnPReady       playerUPnPPhase = "ready"
)

type playerUPnPPresentation struct {
	phase              playerUPnPPhase
	config             playerUPnPConfig
	summary            *playerUPnPIndexSummary
	containers, tracks int
	current            string
	elapsed            time.Duration
	frame              int
	stale              bool
	searchable         bool
	err                error
}

func (setup *playerUPnPSetup) matches() bool {
	if setup.summary == nil || setup.config.path == "" {
		return false
	}
	if setup.summary.path != "" {
		return setup.summary.path == setup.config.path
	}
	return setup.summary.server == setup.config.server
}

func (setup *playerUPnPSetup) presentation() playerUPnPPresentation {
	p := playerUPnPPresentation{
		config: setup.config, frame: setup.frame, elapsed: setup.elapsed, searchable: setup.matches(),
	}
	if setup.summary != nil {
		summary := *setup.summary
		p.summary = &summary
		p.stale = !summary.indexedAt.IsZero() && time.Since(summary.indexedAt) > defaultIndexMaxAge
	}
	if setup.progress != nil {
		p.containers, p.tracks, p.current = setup.progress.snapshot()
	}
	switch {
	case setup.indexing:
		p.phase = playerUPnPIndexing
	case setup.err != nil:
		p.phase, p.err = playerUPnPFailed, setup.err
	case setup.canceled:
		p.phase = playerUPnPCanceled
	case !setup.checked:
		p.phase = playerUPnPChecking
	case setup.config.path == "":
		p.phase = playerUPnPNoServer
	case setup.loadErr != nil:
		p.phase, p.err = playerUPnPFailed, setup.loadErr
	case setup.summary == nil:
		p.phase = playerUPnPNoIndex
	case !setup.matches():
		p.phase = playerUPnPOtherServer
	default:
		p.phase = playerUPnPReady
	}
	return p
}

type playerUPnPUseServerMsg struct {
	generation uint64
	request    playerDiscoveryRequestID
	index      int
}
type playerUPnPIndexMsg struct{ generation uint64 }
type playerUPnPCancelIndexMsg struct{ generation uint64 }
type playerUPnPTickMsg struct {
	generation, job uint64
}
type playerUPnPIndexResult struct {
	generation, job uint64
	summary         *playerUPnPIndexSummary
	err             error
}

// playerUPnPStatus rides along with a UPnP root read so opening the tab never
// blocks the Tea loop on reading a potentially large index file.
type playerUPnPStatus struct {
	summary *playerUPnPIndexSummary
	err     error
}

func (ui *playerDiscoveryUI) updateUPnP(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case playerUPnPUseServerMsg:
		ui.useUPnPServer(msg.index)
		return nil, true
	case playerUPnPIndexMsg:
		return ui.startUPnPIndex(), true
	case playerUPnPCancelIndexMsg:
		ui.cancelUPnPIndex()
		return nil, true
	case playerUPnPTickMsg:
		return ui.tickUPnPIndex(msg), true
	case playerUPnPIndexResult:
		ui.finishUPnPIndex(msg)
		return nil, true
	default:
		return nil, false
	}
}

// upnpServerAt returns the media server row at index on the UPnP root page.
func (ui *playerDiscoveryUI) upnpServerAt(index int) (kefw2.ContentItem, error) {
	page := ui.state.page
	if ui.service != playerDiscoveryUPnP || !ui.state.hasPage || ui.state.stale ||
		page.kind != playerDiscoveryBrowsePage || page.path != playerDiscoveryUPnPRootPath {
		return kefw2.ContentItem{}, errors.New("choose a server from the UPnP server list")
	}
	item, err := ui.state.itemAt(index)
	if err != nil {
		return kefw2.ContentItem{}, err
	}
	if item.Type != kefw2.ContentTypeContainer || !playerDiscoveryUPnPPath(item.Path) {
		return kefw2.ContentItem{}, errors.New("the selected row is not a media server")
	}
	return item, nil
}

func (ui *playerDiscoveryUI) useUPnPServer(index int) {
	item, err := ui.upnpServerAt(index)
	if err != nil {
		ui.reject("use server", err)
		return
	}
	if ui.upnp.indexing {
		ui.reject("use server", errors.New("cancel indexing before switching servers"))
		return
	}
	if err := ui.library.setServer(item.Title, item.Path); err != nil {
		ui.reject("use server", err)
		return
	}
	ui.upnp.config = ui.library.config()
	ui.upnp.err, ui.upnp.canceled = nil, false
	if !ui.busy {
		ui.action = nil
	}
}

func (ui *playerDiscoveryUI) startUPnPIndex() tea.Cmd {
	if ui.upnp.indexing {
		return nil
	}
	if !ui.ready() {
		ui.reject("index", errors.New("no active speaker session"))
		return nil
	}
	ui.upnp.config = ui.library.config()
	cfg := ui.upnp.config
	if cfg.path == "" {
		ui.reject("index", errors.New("choose a UPnP server first: select it and press u"))
		return nil
	}
	ui.upnp.job++
	job, generation, target, library := ui.upnp.job, ui.generation, ui.target, ui.library
	progress := &playerUPnPProgress{}
	command, cancel, err := ui.startWorker(ui.session, true, func(ctx context.Context) tea.Msg {
		result := playerUPnPIndexResult{generation: generation, job: job}
		index, err := library.build(ctx, target, cfg, progress.report)
		if err == nil {
			err = ctx.Err()
		}
		if err == nil {
			err = library.save(index)
		}
		if err != nil {
			result.err = err
			return result
		}
		result.summary = playerUPnPSummarize(index)
		return result
	})
	if err != nil {
		ui.upnp.err = err
		return nil
	}
	ui.upnp.indexing, ui.upnp.cancel, ui.upnp.progress = true, cancel, progress
	ui.upnp.started, ui.upnp.elapsed, ui.upnp.frame = time.Now(), 0, 0
	ui.upnp.err, ui.upnp.canceled = nil, false
	return tea.Batch(playerScopedCmd(generation, command), ui.upnpTick())
}

func (ui *playerDiscoveryUI) upnpTick() tea.Cmd {
	tick := playerUPnPTickMsg{generation: ui.generation, job: ui.upnp.job}
	return playerScopedCmd(ui.generation, tea.Tick(playerUPnPTickInterval, func(time.Time) tea.Msg { return tick }))
}

func (ui *playerDiscoveryUI) tickUPnPIndex(msg playerUPnPTickMsg) tea.Cmd {
	if !ui.upnp.indexing || msg.job != ui.upnp.job {
		return nil
	}
	ui.upnp.frame++
	ui.upnp.elapsed = time.Since(ui.upnp.started)
	return ui.upnpTick()
}

func (ui *playerDiscoveryUI) cancelUPnPIndex() {
	if !ui.upnp.indexing {
		return
	}
	ui.upnp.cancel()
	ui.upnp.job++
	ui.upnp.indexing, ui.upnp.cancel, ui.upnp.canceled = false, nil, true
}

func (ui *playerDiscoveryUI) finishUPnPIndex(msg playerUPnPIndexResult) {
	if !ui.upnp.indexing || msg.job != ui.upnp.job {
		// A cancel can race a build that already saved; keep the summary truthful.
		if msg.err == nil && msg.summary != nil {
			ui.upnp.summary, ui.upnp.loadErr, ui.upnp.checked = msg.summary, nil, true
		}
		return
	}
	ui.upnp.indexing, ui.upnp.cancel = false, nil
	ui.upnp.elapsed = time.Since(ui.upnp.started)
	if msg.err != nil {
		ui.upnp.err = fmt.Errorf("building index: %w", msg.err)
		return
	}
	ui.upnp.summary, ui.upnp.loadErr, ui.upnp.checked = msg.summary, nil, true
}

func (ui *playerDiscoveryUI) resetUPnP() {
	if ui.upnp.cancel != nil {
		ui.upnp.cancel()
	}
	ui.upnp = playerUPnPSetup{config: ui.upnp.config, summary: ui.upnp.summary, checked: ui.upnp.checked,
		loadErr: ui.upnp.loadErr, job: ui.upnp.job + 1}
}

func playerDiscoveryUPnPSearch(
	ctx context.Context, load func() (*kefw2.TrackIndex, error), query string, offset, limit int,
) (playerDiscoveryPage, error) {
	page := playerDiscoveryPage{
		kind: playerDiscoverySearchPage, service: playerDiscoveryUPnP, query: query, offset: offset, limit: limit,
	}
	if err := playerDiscoveryPageArgs(ctx, offset, limit); err != nil {
		return page, err
	}
	if strings.TrimSpace(query) == "" {
		return page, errors.New("search query must not be empty")
	}
	index, err := load()
	if err != nil {
		return page, fmt.Errorf("loading UPnP search index: %w", err)
	}
	if index == nil {
		return page, errors.New("no UPnP search index; press I to build one")
	}
	tracks := kefw2.SearchTracks(index, query, playerUPnPSearchMax)
	page.total = len(tracks)
	end := min(len(tracks), offset+limit)
	for i := offset; i < end; i++ {
		page.items = append(page.items, kefw2.IndexedTrackToContentItem(&tracks[i]))
	}
	if end < len(tracks) {
		page.hasMore, page.nextOffset = true, end
	}
	return page, ctx.Err()
}
