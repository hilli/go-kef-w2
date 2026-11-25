// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/hilli/go-kef-w2/kefw2"
)

type playerDiscoveryServiceMsg struct {
	generation uint64
	service    playerDiscoveryService
	browseRoot bool
}
type playerDiscoverySearchMsg struct {
	generation uint64
	query      string
}
type playerDiscoveryPageMsg struct {
	generation uint64
	request    playerDiscoveryRequestID
	offset     int
}
type playerDiscoveryRetryMsg struct {
	generation uint64
	request    playerDiscoveryRequestID
}
type playerDiscoverySelectMsg struct {
	generation uint64
	request    playerDiscoveryRequestID
	index      int
}
type playerDiscoveryBrowseMsg struct {
	generation uint64
	request    playerDiscoveryRequestID
	index      int
}
type playerDiscoveryBackMsg struct {
	generation uint64
	request    playerDiscoveryRequestID
}
type playerDiscoveryFavoritesMsg struct{ generation uint64 }
type playerDiscoveryPlayMsg struct {
	generation uint64
	request    playerDiscoveryRequestID
	index      int
}
type playerDiscoveryConfirmMsg struct {
	generation uint64
	request    playerDiscoveryRequestID
	accept     bool
}
type playerDiscoveryFavoriteMsg struct {
	generation uint64
	request    playerDiscoveryRequestID
	index      int
	add        bool
}
type playerDiscoveryEnqueueMsg struct {
	generation uint64
	request    playerDiscoveryRequestID
	index      int
}

// The queue should be reloaded for this session after a potentially applied
// discovery operation. A retired operation produces only a global notice.
type playerDiscoveryQueueChangedMsg struct{ generation uint64 }

type playerDiscoveryConfirmation struct {
	target      playerSpeaker
	action      string
	item        kefw2.ContentItem
	clearsQueue bool
	request     playerDiscoveryRequestID
}

// accepted means the request was accepted by the API, not that playback or a
// favorite change has been independently confirmed on the speaker.
type playerDiscoveryActionStatus struct {
	target    playerSpeaker
	action    string
	item      kefw2.ContentItem
	pending   bool
	accepted  bool
	uncertain bool
	err       error
}

type playerDiscoveryPresentation struct {
	service        playerDiscoveryService
	target         playerSpeaker
	hasTarget      bool
	generation     uint64
	query          string
	state          playerDiscoveryState
	selection      int
	hasPrevious    bool
	previousOffset int
	canRetry       bool
	confirmation   *playerDiscoveryConfirmation
	busy           bool
	writeReady     bool
	action         *playerDiscoveryActionStatus
	upnp           playerUPnPPresentation
}

// searchable reports whether the current service can run a search.
func (p playerDiscoveryPresentation) searchable() bool {
	return p.service != playerDiscoveryUPnP || p.upnp.searchable
}

type playerDiscoveryUIClient interface {
	playerDiscoverySearchClient
	playerDiscoveryFavoritesClient
	playerDiscoveryPlayClient
	playerDiscoveryFavoriteClient
	playerDiscoveryQueueClient
}

type playerDiscoveryUIFactory func(context.Context, playerSpeaker) (playerDiscoveryUIClient, playerDiscoverySource)

type playerDiscoveryUIWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
	read   bool
}

type playerDiscoveryUIWritePermit struct {
	mu     sync.Mutex
	active bool
}

type playerDiscoveryUIPageKey struct {
	kind    playerDiscoveryPageKind
	service playerDiscoveryService
	query   string
	path    string
}

type playerDiscoveryUIRetry struct {
	request playerDiscoveryRequestID
	input   playerDiscoveryUIReadRequest
}

type playerDiscoveryUI struct {
	service       playerDiscoveryService
	target        playerSpeaker
	hasTarget     bool
	generation    uint64
	session       context.Context
	cancel        context.CancelFunc
	mutations     *playerMutationGate
	writeContext  context.Context
	writeCancel   context.CancelFunc
	writePermit   *playerDiscoveryUIWritePermit
	writeEpoch    uint64
	readCancel    context.CancelFunc
	readRequest   playerDiscoveryRequestID
	sequence      uint64
	query         string
	state         playerDiscoveryState
	selection     int
	history       []playerDiscoveryLocation
	previousPages map[playerDiscoveryUIPageKey]map[int]int
	lastRead      *playerDiscoveryUIRetry
	confirmation  *playerDiscoveryConfirmation
	busy          bool
	action        *playerDiscoveryActionStatus
	actionRequest playerDiscoveryRequestID
	actionID      uint64
	factory       playerDiscoveryUIFactory
	library       playerUPnPLibrary
	upnp          playerUPnPSetup

	workerMu sync.Mutex
	workers  map[*playerDiscoveryUIWorker]struct{}
}

func newPlayerDiscoveryUI() *playerDiscoveryUI {
	return &playerDiscoveryUI{
		service: playerDiscoveryRadio, selection: -1,
		factory: func(ctx context.Context, target playerSpeaker) (playerDiscoveryUIClient, playerDiscoverySource) {
			speaker := target.newSpeaker()
			return kefw2.NewAirableClient(speaker, kefw2.WithContext(ctx)), speaker
		},
		library: newPlayerUPnPLibrary(),
		workers: make(map[*playerDiscoveryUIWorker]struct{}),
	}
}

func (ui *playerDiscoveryUI) extension() playerExtension {
	return playerExtension{update: ui.update, reset: ui.reset, stop: ui.stop}
}

// presentation is called on the Tea loop and returns independent item data.
func (ui *playerDiscoveryUI) presentation() playerDiscoveryPresentation {
	presentation := playerDiscoveryPresentation{
		service: ui.service, target: ui.target, hasTarget: ui.hasTarget, generation: ui.generation, query: ui.query,
		state: ui.state, selection: ui.selection, busy: ui.busy, canRetry: ui.canRetry(), writeReady: ui.writeReady(),
		upnp: ui.upnp.presentation(),
	}
	presentation.state.page = playerDiscoveryCopyPage(ui.state.page)
	if ui.state.hasPage && !ui.state.stale {
		presentation.previousOffset, presentation.hasPrevious =
			ui.previousPages[playerDiscoveryUIKey(ui.state.page)][ui.state.page.offset]
	}
	if ui.confirmation != nil {
		confirmation := *ui.confirmation
		confirmation.item = playerDiscoveryCopyItem(confirmation.item)
		presentation.confirmation = &confirmation
	}
	if ui.action != nil {
		action := *ui.action
		action.item = playerDiscoveryCopyItem(action.item)
		presentation.action = &action
	}
	return presentation
}

func (ui *playerDiscoveryUI) update(m *playerModel, msg tea.Msg) (tea.Cmd, bool) {
	if !ui.currentRequest(msg) {
		ui.rejectUnavailableIntent(msg)
		return nil, true
	}
	ui.reconcileWrites(m)
	switch msg := msg.(type) {
	case playerSessionStartedMsg, playerSessionStoppedMsg, watchUpdate, playerSessionFailure:
		return ui.updateSession(m, msg), false
	case playerDiscoveryServiceMsg:
		return ui.selectService(msg.service, msg.browseRoot), true
	case playerDiscoverySearchMsg:
		return ui.search(msg.query), true
	case playerDiscoveryPageMsg:
		return ui.page(msg.offset), true
	case playerDiscoveryRetryMsg:
		return ui.retry(), true
	case playerDiscoverySelectMsg:
		ui.selectItem(msg.index)
		return nil, true
	case playerDiscoveryBrowseMsg:
		return ui.browse(msg.index), true
	case playerDiscoveryBackMsg:
		ui.back()
		return nil, true
	case playerDiscoveryFavoritesMsg:
		return ui.favorites(), true
	case playerDiscoveryPlayMsg:
		return ui.play(msg.index), true
	case playerDiscoveryConfirmMsg:
		return ui.confirm(msg.accept), true
	case playerDiscoveryFavoriteMsg:
		return ui.favorite(msg.index, msg.add), true
	case playerDiscoveryEnqueueMsg:
		return ui.enqueue(msg.index), true
	case playerDiscoveryUIReadResult:
		ui.readResult(m, msg)
		return nil, true
	case playerDiscoveryUIActionResult:
		return ui.actionResult(m, msg), true
	default:
		return ui.updateUPnP(msg)
	}
}

func (ui *playerDiscoveryUI) updateSession(m *playerModel, message tea.Msg) tea.Cmd {
	switch msg := message.(type) {
	case playerSessionStartedMsg:
		return ui.sessionStarted(m, msg)
	case playerSessionStoppedMsg:
		if ui.hasTarget && ui.generation == msg.generation {
			ui.reset(m)
		}
	case watchUpdate, playerSessionFailure:
		ui.watch(m, msg)
	}
	return nil
}

func (ui *playerDiscoveryUI) currentRequest(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case playerDiscoveryServiceMsg:
		return ui.currentGeneration(msg.generation)
	case playerDiscoverySearchMsg:
		return ui.currentGeneration(msg.generation)
	case playerDiscoveryPageMsg:
		return ui.currentPage(msg.generation, msg.request)
	case playerDiscoveryRetryMsg:
		return ui.currentPage(msg.generation, msg.request)
	case playerDiscoverySelectMsg:
		return ui.currentPage(msg.generation, msg.request)
	case playerDiscoveryBrowseMsg:
		return ui.currentPage(msg.generation, msg.request)
	case playerDiscoveryBackMsg:
		return ui.currentPage(msg.generation, msg.request)
	case playerDiscoveryFavoritesMsg:
		return ui.currentGeneration(msg.generation)
	case playerDiscoveryPlayMsg:
		return ui.currentPage(msg.generation, msg.request)
	case playerDiscoveryConfirmMsg:
		return ui.currentPage(msg.generation, msg.request)
	case playerDiscoveryFavoriteMsg:
		return ui.currentPage(msg.generation, msg.request)
	case playerDiscoveryEnqueueMsg:
		return ui.currentPage(msg.generation, msg.request)
	case playerUPnPUseServerMsg:
		return ui.currentPage(msg.generation, msg.request)
	case playerUPnPIndexMsg:
		return ui.currentGeneration(msg.generation)
	case playerUPnPCancelIndexMsg:
		return ui.currentGeneration(msg.generation)
	case playerUPnPTickMsg:
		return ui.currentGeneration(msg.generation)
	case playerUPnPIndexResult:
		return ui.currentGeneration(msg.generation)
	default:
		return true
	}
}

func (ui *playerDiscoveryUI) rejectUnavailableIntent(message tea.Msg) {
	if !ui.hasTarget || ui.writeReady() {
		return
	}
	var generation uint64
	var action string
	switch msg := message.(type) {
	case playerDiscoveryPlayMsg:
		generation, action = msg.generation, string(playerDiscoveryUIPlay)
	case playerDiscoveryConfirmMsg:
		if !msg.accept {
			return
		}
		generation, action = msg.generation, string(playerDiscoveryUIPlay)
	case playerDiscoveryFavoriteMsg:
		generation, action = msg.generation, string(playerDiscoveryUIFavorite)
	case playerDiscoveryEnqueueMsg:
		generation, action = msg.generation, string(playerDiscoveryUIEnqueue)
	default:
		return
	}
	if generation == ui.generation {
		ui.reject(action, errors.New("speaker status is unavailable or stale; wait for a fresh status"))
	}
}

func (ui *playerDiscoveryUI) currentGeneration(generation uint64) bool {
	return ui.hasTarget && ui.generation == generation
}

func (ui *playerDiscoveryUI) currentPage(generation uint64, request playerDiscoveryRequestID) bool {
	return ui.currentGeneration(generation) && ui.state.hasRequest && ui.state.request == request
}

func (ui *playerDiscoveryUI) sessionStarted(m *playerModel, msg playerSessionStartedMsg) tea.Cmd {
	if m.session == nil || m.target == nil || m.generation != msg.generation || *m.target != msg.target {
		return nil
	}
	if ui.hasTarget && ui.generation == msg.generation {
		return nil
	}
	ui.reset(m)
	ui.target, ui.hasTarget, ui.generation = msg.target, true, msg.generation
	ui.session, ui.cancel = context.WithCancel(m.session.context())
	ui.mutations = m.mutations
	if ui.service == playerDiscoveryUPnP {
		return ui.load(playerDiscoveryUIReadRequest{
			kind: playerDiscoveryBrowsePage, service: playerDiscoveryUPnP, limit: playerDiscoveryDefaultPageSize,
		})
	}
	return nil
}

func (ui *playerDiscoveryUI) reset(_ *playerModel) {
	ui.resetUPnP()
	ui.cancelWrites()
	if ui.cancel != nil {
		ui.cancel()
	}
	ui.cancelWorkers()
	ui.target, ui.hasTarget, ui.generation = playerSpeaker{}, false, 0
	ui.session, ui.cancel, ui.readCancel = nil, nil, nil
	ui.writeEpoch = 0
	ui.readRequest, ui.actionRequest = playerDiscoveryRequestID{}, playerDiscoveryRequestID{}
	ui.mutations = nil
	ui.sequence, ui.query, ui.state, ui.selection = 0, "", playerDiscoveryState{}, -1
	ui.history, ui.previousPages, ui.confirmation, ui.busy, ui.action = nil, nil, nil, false, nil
	ui.lastRead = nil
}

func (ui *playerDiscoveryUI) stop(_ *playerModel) []<-chan struct{} {
	ui.cancelWrites()
	if ui.cancel != nil {
		ui.cancel()
	}
	return ui.cancelWorkers()
}

func (ui *playerDiscoveryUI) cancelWorkers() []<-chan struct{} {
	ui.workerMu.Lock()
	defer ui.workerMu.Unlock()
	done := make([]<-chan struct{}, 0, len(ui.workers))
	for worker := range ui.workers {
		worker.cancel()
		done = append(done, worker.done)
	}
	return done
}

func (ui *playerDiscoveryUI) ready() bool {
	return ui.hasTarget && ui.session != nil && ui.session.Err() == nil
}

func (ui *playerDiscoveryUI) writeReady() bool {
	return ui.ready() && ui.writeContext != nil && ui.writePermit != nil && ui.writeContext.Err() == nil
}

func (ui *playerDiscoveryUI) reconcileWrites(m *playerModel) {
	if ui.writeReady() && (m.session == nil || m.generation != ui.generation ||
		m.connection != playerReady || m.readErr != nil || m.snapshot == nil) {
		ui.suspendWrites()
	}
}

func (ui *playerDiscoveryUI) watch(m *playerModel, message tea.Msg) {
	if !ui.hasTarget || ui.generation != m.generation {
		return
	}
	switch msg := message.(type) {
	case watchUpdate:
		switch msg.kind {
		case watchSnapshotUpdate:
			if ui.ready() && msg.err == nil && m.connection == playerReady && m.readErr == nil && m.snapshot != nil {
				ui.enableWrites()
			} else {
				ui.suspendWrites()
			}
		case playerStatusUnavailableUpdate:
			ui.suspendWrites()
		case playerEventConnectionUpdate:
			if msg.err != nil {
				ui.suspendWrites()
			}
		}
	case playerSessionFailure:
		ui.suspendWrites()
	}
}

func (ui *playerDiscoveryUI) enableWrites() {
	if ui.writeReady() {
		return
	}
	ui.writeEpoch++
	ui.writeContext, ui.writeCancel = context.WithCancel(ui.session)
	ui.writePermit = &playerDiscoveryUIWritePermit{active: true}
	ui.rotateRequest()
}

func (ui *playerDiscoveryUI) suspendWrites() {
	if ui.cancelWrites() {
		ui.writeEpoch++
		ui.rotateRequest()
	}
	ui.confirmation = nil
	if ui.busy && ui.action != nil {
		ui.action.err = errors.New("speaker status is unavailable or stale; stopping the pending action")
	}
}

func (ui *playerDiscoveryUI) cancelWrites() bool {
	if ui.writeCancel == nil {
		return false
	}
	if ui.writePermit != nil {
		ui.writePermit.mu.Lock()
		ui.writePermit.active = false
		ui.writePermit.mu.Unlock()
	}
	ui.writeCancel()
	ui.writeContext, ui.writeCancel, ui.writePermit = nil, nil, nil
	return true
}

// Keep reads alive while making every already-issued row intent obsolete.
func (ui *playerDiscoveryUI) rotateRequest() {
	if !ui.state.hasRequest {
		return
	}
	previous := ui.state.request
	ui.sequence++
	ui.state.request = playerDiscoveryRequestID{generation: ui.generation, sequence: ui.sequence}
	if ui.lastRead != nil && ui.lastRead.request == previous {
		ui.lastRead.request = ui.state.request
	}
	if ui.actionRequest == previous {
		ui.actionRequest = ui.state.request
	}
}

func (ui *playerDiscoveryUI) reject(action string, err error) {
	if ui.busy {
		if !ui.writeReady() && ui.action != nil {
			ui.action.err = err
		}
		return
	}
	ui.action = &playerDiscoveryActionStatus{target: ui.target, action: action, err: err}
}

func (ui *playerDiscoveryUI) clearSelection() {
	ui.selection, ui.confirmation = -1, nil
	if !ui.busy {
		ui.action = nil
	}
}

func (ui *playerDiscoveryUI) cancelRead() {
	if ui.readCancel != nil {
		ui.readCancel()
		ui.readCancel = nil
	}
	ui.readRequest = playerDiscoveryRequestID{}
}

func (ui *playerDiscoveryUI) selectService(service playerDiscoveryService, browseRoot bool) tea.Cmd {
	switch service {
	case playerDiscoveryRadio, playerDiscoveryPodcast, playerDiscoveryUPnP:
	default:
		ui.reject("select service", fmt.Errorf("unsupported discovery service %q", service))
		return nil
	}
	if ui.service == service && !browseRoot {
		return nil
	}
	ui.cancelRead()
	ui.sequence++
	ui.service, ui.query, ui.state, ui.history = service, "", playerDiscoveryState{}, nil
	ui.previousPages = nil
	ui.lastRead = nil
	ui.clearSelection()
	if (browseRoot || service == playerDiscoveryUPnP) && ui.ready() {
		return ui.load(playerDiscoveryUIReadRequest{
			kind: playerDiscoveryBrowsePage, service: service, limit: playerDiscoveryDefaultPageSize,
		})
	}
	return nil
}

func (ui *playerDiscoveryUI) search(query string) tea.Cmd {
	ui.query, ui.history, ui.previousPages = query, nil, nil
	ui.clearSelection()
	return ui.load(playerDiscoveryUIReadRequest{
		kind: playerDiscoverySearchPage, service: ui.service, query: query, limit: playerDiscoveryDefaultPageSize,
	})
}

func (ui *playerDiscoveryUI) favorites() tea.Cmd {
	ui.query, ui.history, ui.previousPages = "", nil, nil
	ui.clearSelection()
	return ui.load(playerDiscoveryUIReadRequest{
		kind: playerDiscoveryFavoritesPage, service: ui.service, limit: playerDiscoveryDefaultPageSize,
	})
}

func (ui *playerDiscoveryUI) selectItem(index int) {
	if _, err := ui.state.itemAt(index); err != nil {
		ui.reject("select item", err)
		return
	}
	ui.selection, ui.confirmation = index, nil
	if !ui.busy {
		ui.action = nil
	}
}

func (ui *playerDiscoveryUI) browse(index int) tea.Cmd {
	item, err := ui.state.itemAt(index)
	if err != nil {
		ui.reject("browse", err)
		return nil
	}
	history, err := playerDiscoveryRemember(ui.state, ui.history, index)
	if err != nil {
		ui.reject("browse", err)
		return nil
	}
	ui.history = history
	ui.clearSelection()
	return ui.load(playerDiscoveryUIReadRequest{
		kind: playerDiscoveryBrowsePage, service: ui.service, item: &item, limit: playerDiscoveryDefaultPageSize,
	})
}

func (ui *playerDiscoveryUI) back() {
	if !ui.ready() || len(ui.history) == 0 {
		return
	}
	ui.cancelRead()
	ui.sequence++
	request := playerDiscoveryRequestID{generation: ui.generation, sequence: ui.sequence}
	state, history, selection, ok := playerDiscoveryBack(ui.state, ui.history, request)
	if !ok {
		ui.history = history
		return
	}
	ui.state, ui.history, ui.selection, ui.confirmation = state, history, selection, nil
	ui.lastRead = nil
	if !ui.busy {
		ui.action = nil
	}
}

func (ui *playerDiscoveryUI) page(offset int) tea.Cmd {
	if !ui.state.hasPage || ui.state.stale || ui.state.status == playerDiscoveryLoading {
		ui.reject("change page", errors.New("discovery results are not current"))
		return nil
	}
	page := ui.state.page
	if offset == page.offset {
		return nil
	}
	if offset < 0 || offset > playerDiscoveryMaxOffset {
		ui.reject("change page", errors.New("discovery page offset is out of range"))
		return nil
	}
	if offset > page.offset {
		if ui.previousPages == nil {
			ui.previousPages = make(map[playerDiscoveryUIPageKey]map[int]int)
		}
		key := playerDiscoveryUIKey(page)
		if ui.previousPages[key] == nil {
			ui.previousPages[key] = make(map[int]int)
		}
		ui.previousPages[key][offset] = page.offset
	}
	request := playerDiscoveryUIReadRequest{
		kind: page.kind, service: page.service, query: page.query, offset: offset, limit: page.limit,
	}
	if page.kind == playerDiscoveryBrowsePage && page.path != playerDiscoveryUPnPRootPath {
		request.item = &kefw2.ContentItem{Type: kefw2.ContentTypeContainer, Path: page.path}
	}
	ui.clearSelection()
	return ui.load(request)
}

func playerDiscoveryUIKey(page playerDiscoveryPage) playerDiscoveryUIPageKey {
	return playerDiscoveryUIPageKey{kind: page.kind, service: page.service, query: page.query, path: page.path}
}

func (ui *playerDiscoveryUI) canRetry() bool {
	return ui.ready() && ui.state.hasRequest && ui.state.status == playerDiscoveryError &&
		ui.lastRead != nil && ui.lastRead.request == ui.state.request
}

func (ui *playerDiscoveryUI) retry() tea.Cmd {
	if !ui.canRetry() {
		return nil
	}
	input := ui.lastRead.input
	ui.clearSelection()
	return ui.load(input)
}

type playerDiscoveryUIReadRequest struct {
	kind          playerDiscoveryPageKind
	service       playerDiscoveryService
	query         string
	item          *kefw2.ContentItem
	offset, limit int
}

type playerDiscoveryUIReadResult struct {
	request playerDiscoveryRequestID
	page    playerDiscoveryPage
	err     error
	upnp    *playerUPnPStatus
}

func (ui *playerDiscoveryUI) load(input playerDiscoveryUIReadRequest) tea.Cmd {
	if !ui.ready() {
		ui.reject("discovery", errors.New("no active speaker session"))
		return nil
	}
	ui.cancelRead()
	ui.sequence++
	request := playerDiscoveryRequestID{generation: ui.generation, sequence: ui.sequence}
	ui.state = playerDiscoveryBegin(ui.state, request)
	ui.readRequest = request
	target, factory, library := ui.target, ui.factory, ui.library
	upnpRoot := input.service == playerDiscoveryUPnP && input.kind == playerDiscoveryBrowsePage && input.item == nil
	if upnpRoot {
		ui.upnp.config = library.config()
	}
	if input.item != nil {
		item := playerDiscoveryCopyItem(*input.item)
		input.item = &item
	}
	ui.lastRead = &playerDiscoveryUIRetry{request: request, input: input}
	command, cancel, err := ui.startWorker(ui.session, true, func(ctx context.Context) tea.Msg {
		if err := ctx.Err(); err != nil {
			return playerDiscoveryUIReadResult{request: request, err: err}
		}
		if input.kind == playerDiscoverySearchPage && input.service == playerDiscoveryUPnP {
			page, err := playerDiscoveryUPnPSearch(ctx, library.load, input.query, input.offset, input.limit)
			return playerDiscoveryUIReadResult{request: request, page: page, err: err}
		}
		client, _ := factory(ctx, target)
		page, err := playerDiscoveryUIFetch(ctx, client, input)
		result := playerDiscoveryUIReadResult{request: request, page: page, err: err}
		if upnpRoot {
			index, err := library.load()
			result.upnp = &playerUPnPStatus{summary: playerUPnPSummarize(index), err: err}
		}
		return result
	})
	if err != nil {
		ui.readRequest = playerDiscoveryRequestID{}
		ui.state = playerDiscoveryFinish(ui.state, request, playerDiscoveryPage{}, err)
		return nil
	}
	ui.readCancel = cancel
	return playerScopedCmd(request.generation, command)
}

func playerDiscoveryUIFetch(
	ctx context.Context, client playerDiscoveryUIClient, request playerDiscoveryUIReadRequest,
) (playerDiscoveryPage, error) {
	switch request.kind {
	case playerDiscoverySearchPage:
		return playerDiscoverySearch(ctx, client, request.service, request.query, request.offset, request.limit)
	case playerDiscoveryBrowsePage:
		return playerDiscoveryBrowse(ctx, client, request.service, request.item, request.offset, request.limit)
	case playerDiscoveryFavoritesPage:
		return playerDiscoveryFavorites(ctx, client, request.service, request.offset, request.limit)
	default:
		return playerDiscoveryPage{}, fmt.Errorf("unsupported discovery page %q", request.kind)
	}
}

func (ui *playerDiscoveryUI) readResult(m *playerModel, result playerDiscoveryUIReadResult) {
	if !ui.ready() || m.generation != ui.generation || result.request != ui.readRequest ||
		ui.state.status != playerDiscoveryLoading {
		return
	}
	ui.readCancel = nil
	ui.readRequest = playerDiscoveryRequestID{}
	if result.upnp != nil {
		ui.upnp.summary, ui.upnp.loadErr, ui.upnp.checked = result.upnp.summary, result.upnp.err, true
	}
	ui.state = playerDiscoveryFinish(ui.state, ui.state.request, result.page, result.err)
	if result.err == nil {
		ui.lastRead = nil
	}
	ui.selection = -1
	if ui.state.status == playerDiscoveryReady {
		ui.selection = 0
	}
}

func (ui *playerDiscoveryUI) selected(index int, action string) (kefw2.ContentItem, bool) {
	if !ui.ready() {
		ui.reject(action, errors.New("no active speaker session"))
		return kefw2.ContentItem{}, false
	}
	if !ui.writeReady() {
		ui.reject(action, errors.New("speaker status is unavailable or stale; wait for a fresh status"))
		return kefw2.ContentItem{}, false
	}
	if ui.busy {
		return kefw2.ContentItem{}, false
	}
	if ui.state.page.service != ui.service {
		ui.reject(action, errors.New("discovery results belong to a different service"))
		return kefw2.ContentItem{}, false
	}
	item, err := ui.state.itemAt(index)
	if err != nil {
		ui.reject(action, err)
		return kefw2.ContentItem{}, false
	}
	return item, true
}

func (ui *playerDiscoveryUI) play(index int) tea.Cmd {
	item, ok := ui.selected(index, "play")
	if !ok {
		return nil
	}
	if strings.TrimSpace(item.Path) == "" {
		ui.reject("play", errors.New("a playable item with an exact path is required"))
		return nil
	}
	switch ui.service {
	case playerDiscoveryRadio:
		if !playerDiscoveryPlayableRadio(item) {
			ui.reject("play", errors.New("only a radio station can be played"))
			return nil
		}
		ui.confirmation = &playerDiscoveryConfirmation{
			target: ui.target, action: playerDiscoveryUIActionName("play radio", item), item: item,
			clearsQueue: true, request: ui.state.request,
		}
		return nil
	case playerDiscoveryPodcast:
		if item.Type != kefw2.ContentTypeAudio {
			ui.reject("play", errors.New("only a podcast episode can be played"))
			return nil
		}
		return ui.startAction(playerDiscoveryUIPlay, item, false)
	default:
		ui.reject("play", errors.New("direct playback is unsupported for this service"))
		return nil
	}
}

func (ui *playerDiscoveryUI) confirm(accept bool) tea.Cmd {
	confirmation := ui.confirmation
	ui.confirmation = nil
	if accept && !ui.writeReady() {
		ui.reject("play", errors.New("speaker status is unavailable or stale; wait for a fresh status"))
		return nil
	}
	if !accept || confirmation == nil || ui.busy ||
		confirmation.request != ui.state.request || confirmation.target != ui.target ||
		ui.state.status != playerDiscoveryReady || ui.state.stale {
		return nil
	}
	return ui.startAction(playerDiscoveryUIPlay, confirmation.item, false)
}

func (ui *playerDiscoveryUI) favorite(index int, add bool) tea.Cmd {
	item, ok := ui.selected(index, "favorite")
	if !ok {
		return nil
	}
	if strings.TrimSpace(item.Path) == "" || strings.TrimSpace(item.ID) == "" {
		ui.reject("favorite", errors.New("a favorite requires an exact item path and ID"))
		return nil
	}
	switch ui.service {
	case playerDiscoveryRadio:
		if !playerDiscoveryPlayableRadio(item) {
			ui.reject("favorite", errors.New("only radio stations can be favorited"))
			return nil
		}
	case playerDiscoveryPodcast:
		if item.Type != kefw2.ContentTypeContainer {
			ui.reject("favorite", errors.New("only podcast series can be favorited"))
			return nil
		}
	default:
		ui.reject("favorite", errors.New("favorites are unsupported for this service"))
		return nil
	}
	return ui.startAction(playerDiscoveryUIFavorite, item, add)
}

func (ui *playerDiscoveryUI) enqueue(index int) tea.Cmd {
	item, ok := ui.selected(index, "enqueue")
	if !ok {
		return nil
	}
	if ui.service != playerDiscoveryUPnP || item.Type != kefw2.ContentTypeAudio || !playerDiscoveryUPnPPath(item.Path) {
		ui.reject("enqueue", errors.New("only an exact UPnP audio track can be enqueued"))
		return nil
	}
	return ui.startAction(playerDiscoveryUIEnqueue, item, false)
}

type playerDiscoveryUIAction string

const (
	playerDiscoveryUIPlay     playerDiscoveryUIAction = "play"
	playerDiscoveryUIFavorite playerDiscoveryUIAction = "favorite"
	playerDiscoveryUIEnqueue  playerDiscoveryUIAction = "enqueue"
)

type playerDiscoveryUIActionResult struct {
	generation       uint64
	epoch            uint64
	request          playerDiscoveryRequestID
	id               uint64
	target           playerSpeaker
	action           string
	dispatched       bool
	queueDispatched  bool
	statusDispatched bool
	err              error
}

func playerDiscoveryUIActionName(action string, item kefw2.ContentItem) string {
	if item.Title == "" {
		return fmt.Sprintf("%s %s", action, item.Path)
	}
	return fmt.Sprintf("%s %q (%s)", action, item.Title, item.Path)
}

func (ui *playerDiscoveryUI) startAction(kind playerDiscoveryUIAction, item kefw2.ContentItem, add bool) tea.Cmd {
	label := string(kind)
	switch kind {
	case playerDiscoveryUIPlay:
		label += " " + string(ui.service)
	case playerDiscoveryUIFavorite:
		if add {
			label = "add favorite"
		} else {
			label = "remove favorite"
		}
	}
	action := playerDiscoveryUIActionName(label, item)
	if !ui.writeReady() {
		ui.reject(action, errors.New("speaker status is unavailable or stale; wait for a fresh status"))
		return nil
	}
	if ui.mutations == nil {
		ui.reject(action, errors.New("speaker mutation coordinator is unavailable"))
		return nil
	}
	ticket := ui.mutations.reserve()
	ui.actionID++
	id, request, generation, epoch := ui.actionID, ui.state.request, ui.generation, ui.writeEpoch
	target, service, factory, permit := ui.target, ui.service, ui.factory, ui.writePermit
	item = playerDiscoveryCopyItem(item)
	ui.busy = true
	ui.actionRequest = request
	ui.action = &playerDiscoveryActionStatus{target: target, action: action, item: item, pending: true}

	command, _, err := ui.startWorker(ui.writeContext, false, func(ctx context.Context) tea.Msg {
		defer ticket.release()
		result := playerDiscoveryUIActionResult{
			generation: generation, epoch: epoch, request: request, id: id, target: target, action: action,
		}
		if err := ticket.wait(ctx); err != nil {
			result.err = err
			return result
		}
		client, source := factory(ctx, target)
		tracked := &playerDiscoveryUIMutation{playerDiscoveryUIClient: client, source: source, ctx: ctx, permit: permit}
		switch kind {
		case playerDiscoveryUIPlay:
			result.err = playerDiscoveryPlay(ctx, tracked, tracked, service, item)
		case playerDiscoveryUIFavorite:
			result.err = playerDiscoveryFavorite(ctx, tracked, service, item, add)
		case playerDiscoveryUIEnqueue:
			result.err = playerDiscoveryEnqueue(ctx, tracked, service, item)
		}
		result.dispatched, result.queueDispatched = tracked.dispatched, tracked.queueDispatched
		result.statusDispatched = tracked.sourceDispatched || (kind == playerDiscoveryUIPlay && tracked.actionDispatched)
		if tracked.sourceDispatched && !tracked.actionDispatched {
			result.action = "activate WiFi for " + action
		}
		return result
	})
	if err != nil {
		ticket.release()
		ui.busy = false
		ui.action.pending, ui.action.err = false, err
		return nil
	}
	// Completion is unscoped only so a retired dispatched write can produce a
	// global uncertainty notice; it never carries old page or item state.
	return command
}

func (ui *playerDiscoveryUI) actionResult(m *playerModel, result playerDiscoveryUIActionResult) tea.Cmd {
	retired := !ui.ready() || ui.generation != result.generation || m.generation != result.generation
	if retired {
		if result.dispatched {
			reason := "speaker session became unavailable before the outcome was confirmed"
			if m.generation != result.generation {
				reason = "completed after switching speakers"
			}
			return playerDiscoveryUINotice(result, reason)
		}
		return nil
	}
	statusLost := result.epoch != ui.writeEpoch
	if !ui.finishAction(result, statusLost) && !statusLost {
		return nil
	}
	var refresh, status, notice tea.Cmd
	if result.queueDispatched {
		refresh = playerScopedCmd(result.generation, func() tea.Msg {
			return playerDiscoveryQueueChangedMsg{generation: result.generation}
		})
	}
	if result.statusDispatched {
		status = playerScopedCmd(result.generation, func() tea.Msg {
			return playerStatusRefreshRequest{generation: result.generation}
		})
	}
	if result.dispatched && (statusLost || result.err != nil) {
		reason := ""
		if statusLost {
			reason = "speaker status became unavailable before the outcome was confirmed"
		}
		notice = playerDiscoveryUINotice(result, reason)
	}
	return tea.Batch(refresh, status, notice)
}

func (ui *playerDiscoveryUI) finishAction(result playerDiscoveryUIActionResult, statusLost bool) bool {
	if !ui.busy || ui.actionID != result.id {
		return false
	}
	ui.busy = false
	if !ui.state.hasRequest || ui.state.request != ui.actionRequest {
		ui.action = nil
		return true
	}
	if ui.action == nil {
		return true
	}
	ui.action.pending = false
	ui.action.action = result.action
	ui.action.accepted = result.err == nil && !statusLost
	ui.action.uncertain = result.dispatched && (statusLost || result.err != nil)
	ui.action.err = result.err
	if statusLost {
		ui.action.err = errors.New("speaker status became unavailable; the pending action was not sent")
		if result.dispatched {
			ui.action.err = playerDiscoveryUIUncertainty(result,
				"speaker status became unavailable before the outcome was confirmed")
		}
	}
	return true
}

func playerDiscoveryUIUncertainty(result playerDiscoveryUIActionResult, reason string) error {
	message := "may have been accepted; review the target before retrying"
	if reason != "" {
		message = reason + "; " + message
	}
	if result.err != nil {
		return fmt.Errorf("%s: %w", message, result.err)
	}
	return errors.New(message)
}

func playerDiscoveryUINotice(result playerDiscoveryUIActionResult, reason string) tea.Cmd {
	err := playerDiscoveryUIUncertainty(result, reason)
	return func() tea.Msg {
		return playerNoticeMsg{target: result.target, action: result.action, err: err}
	}
}

const (
	playerDiscoveryUIMaxReads   = 4
	playerDiscoveryUIMaxActions = 2
	playerDiscoveryUPnPRootPath = "ui:/upnp"
)

func (ui *playerDiscoveryUI) startWorker(
	parent context.Context, read bool, run func(context.Context) tea.Msg,
) (tea.Cmd, context.CancelFunc, error) {
	if parent == nil || parent.Err() != nil {
		return nil, nil, errors.New("speaker session is no longer active")
	}
	ctx, cancel := context.WithCancel(parent)
	worker := &playerDiscoveryUIWorker{cancel: cancel, done: make(chan struct{}), read: read}
	ui.workerMu.Lock()
	count := 0
	for active := range ui.workers {
		if active.read == read {
			count++
		}
	}
	limit := playerDiscoveryUIMaxActions
	if read {
		limit = playerDiscoveryUIMaxReads
	}
	if count >= limit {
		ui.workerMu.Unlock()
		cancel()
		return nil, nil, errors.New("previous discovery requests are still stopping; retry")
	}
	ui.workers[worker] = struct{}{}
	ui.workerMu.Unlock()
	result := make(chan tea.Msg, 1)
	go func() {
		defer func() {
			cancel()
			ui.workerMu.Lock()
			delete(ui.workers, worker)
			close(worker.done)
			ui.workerMu.Unlock()
		}()
		result <- run(ctx)
	}()
	return func() tea.Msg { return <-result }, cancel, nil
}

type playerDiscoveryUIMutation struct {
	playerDiscoveryUIClient
	source           playerDiscoverySource
	ctx              context.Context
	permit           *playerDiscoveryUIWritePermit
	dispatched       bool
	sourceDispatched bool
	actionDispatched bool
	queueDispatched  bool
}

func (client *playerDiscoveryUIMutation) Source(ctx context.Context) (kefw2.Source, error) {
	return client.source.Source(ctx)
}

func (client *playerDiscoveryUIMutation) SetSource(ctx context.Context, source kefw2.Source) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return client.send(true, false, func() error { return client.source.SetSource(ctx, source) })
}

func (client *playerDiscoveryUIMutation) PlayRadioStation(item *kefw2.ContentItem) error {
	return client.send(false, true, func() error { return client.playerDiscoveryUIClient.PlayRadioStation(item) })
}

func (client *playerDiscoveryUIMutation) RequestPodcastEpisodeExact(item *kefw2.ContentItem) error {
	return client.send(false, false, func() error { return client.playerDiscoveryUIClient.RequestPodcastEpisodeExact(item) })
}

func (client *playerDiscoveryUIMutation) AddRadioFavorite(item *kefw2.ContentItem) error {
	return client.send(false, false, func() error { return client.playerDiscoveryUIClient.AddRadioFavorite(item) })
}

func (client *playerDiscoveryUIMutation) RemoveRadioFavorite(item *kefw2.ContentItem) error {
	return client.send(false, false, func() error { return client.playerDiscoveryUIClient.RemoveRadioFavorite(item) })
}

func (client *playerDiscoveryUIMutation) AddPodcastFavorite(item *kefw2.ContentItem) error {
	return client.send(false, false, func() error { return client.playerDiscoveryUIClient.AddPodcastFavorite(item) })
}

func (client *playerDiscoveryUIMutation) RemovePodcastFavorite(item *kefw2.ContentItem) error {
	return client.send(false, false, func() error { return client.playerDiscoveryUIClient.RemovePodcastFavorite(item) })
}

func (client *playerDiscoveryUIMutation) AddToQueue(items []kefw2.ContentItem, start bool) error {
	return client.send(false, true, func() error { return client.playerDiscoveryUIClient.AddToQueue(items, start) })
}

func (client *playerDiscoveryUIMutation) send(source, queue bool, operation func() error) error {
	if client.permit == nil {
		return errors.New("speaker status is unavailable or stale")
	}
	client.permit.mu.Lock()
	if !client.permit.active {
		client.permit.mu.Unlock()
		return context.Canceled
	}
	if err := client.ctx.Err(); err != nil {
		client.permit.mu.Unlock()
		return err
	}
	client.dispatched = true
	client.sourceDispatched = client.sourceDispatched || source
	client.actionDispatched = client.actionDispatched || !source
	client.queueDispatched = client.queueDispatched || queue
	client.permit.mu.Unlock()
	return operation()
}
