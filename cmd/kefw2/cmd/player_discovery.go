// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hilli/go-kef-w2/kefw2"
)

type playerDiscoveryService string

const (
	playerDiscoveryRadio   playerDiscoveryService = "radio"
	playerDiscoveryPodcast playerDiscoveryService = "podcast"
	playerDiscoveryUPnP    playerDiscoveryService = "upnp"

	playerDiscoveryDefaultPageSize = 24
	playerDiscoveryMaxPageSize     = 100
	playerDiscoveryMaxOffset       = 1_000_000
	playerDiscoveryMaxRedirects    = 5
)

type playerDiscoveryRowsClient interface {
	GetRows(path string, from, to int) (*kefw2.RowsResponse, error)
}

type playerDiscoverySearchClient interface {
	playerDiscoveryRowsClient
	SearchRadioPage(query string, offset, limit int) (*kefw2.RowsResponse, error)
	SearchPodcastsPage(query string, offset, limit int) (*kefw2.RowsResponse, error)
}

type playerDiscoveryFavoritesClient interface {
	playerDiscoveryRowsClient
	GetRadioFavoritesPage(offset, limit int) (*kefw2.RowsResponse, error)
	GetPodcastFavoritesPage(offset, limit int) (*kefw2.RowsResponse, error)
}

type playerDiscoveryPlayClient interface {
	GetRadioStationByPath(path string) (*kefw2.ContentItem, error)
	PlayRadioStation(station *kefw2.ContentItem) error
	GetPodcastDetailsExact(path string) (*kefw2.ContentItem, error)
	RequestPodcastEpisodeExact(episode *kefw2.ContentItem) error
}

type playerDiscoveryFavoriteClient interface {
	AddRadioFavorite(station *kefw2.ContentItem) error
	RemoveRadioFavorite(station *kefw2.ContentItem) error
	AddPodcastFavorite(podcast *kefw2.ContentItem) error
	RemovePodcastFavorite(podcast *kefw2.ContentItem) error
}

type playerDiscoveryQueueClient interface {
	AddToQueue(tracks []kefw2.ContentItem, startIfEmpty bool) error
}

type playerDiscoverySource interface {
	Source(context.Context) (kefw2.Source, error)
	SetSource(context.Context, kefw2.Source) error
}

type playerDiscoveryPageKind string

const (
	playerDiscoverySearchPage    playerDiscoveryPageKind = "search"
	playerDiscoveryBrowsePage    playerDiscoveryPageKind = "browse"
	playerDiscoveryFavoritesPage playerDiscoveryPageKind = "favorites"
)

type playerDiscoveryPage struct {
	kind                 playerDiscoveryPageKind
	service              playerDiscoveryService
	query, path          string
	offset, limit, total int
	nextOffset           int
	hasMore              bool
	items                []kefw2.ContentItem
}

// The caller binds each client to ctx and the captured speaker with
// kefw2.WithContext(ctx); the client methods themselves have no context argument.
func playerDiscoverySearch(
	ctx context.Context, client playerDiscoverySearchClient, service playerDiscoveryService,
	query string, offset, limit int,
) (playerDiscoveryPage, error) {
	page := playerDiscoveryPage{
		kind: playerDiscoverySearchPage, service: service, query: query, offset: offset, limit: limit,
	}
	if err := playerDiscoveryPageArgs(ctx, offset, limit); err != nil {
		return page, err
	}
	if client == nil {
		return page, errors.New("discovery client is required")
	}
	if strings.TrimSpace(query) == "" {
		return page, errors.New("search query must not be empty")
	}

	var rows *kefw2.RowsResponse
	var err error
	switch service {
	case playerDiscoveryRadio:
		rows, err = client.SearchRadioPage(query, offset, limit)
	case playerDiscoveryPodcast:
		rows, err = client.SearchPodcastsPage(query, offset, limit)
	default:
		return page, fmt.Errorf("search is not supported for %q", service)
	}
	if err = playerDiscoveryRequestError(ctx, err); err != nil {
		return page, fmt.Errorf("searching %s: %w", service, err)
	}
	return playerDiscoveryReadPage(ctx, client, page, rows)
}

// A nil item selects the UPnP server root. Other pages use the exact path
// supplied by a previously returned container, never a title-derived path.
func playerDiscoveryBrowse(
	ctx context.Context, client playerDiscoveryRowsClient, service playerDiscoveryService,
	item *kefw2.ContentItem, offset, limit int,
) (playerDiscoveryPage, error) {
	page := playerDiscoveryPage{
		kind: playerDiscoveryBrowsePage, service: service, offset: offset, limit: limit,
	}
	if err := playerDiscoveryPageArgs(ctx, offset, limit); err != nil {
		return page, err
	}
	if client == nil {
		return page, errors.New("discovery client is required")
	}
	switch service {
	case playerDiscoveryRadio, playerDiscoveryPodcast, playerDiscoveryUPnP:
	default:
		return page, fmt.Errorf("browsing is not supported for %q", service)
	}
	if item == nil {
		if service != playerDiscoveryUPnP {
			return page, errors.New("a container is required")
		}
		page.path = "ui:/upnp"
	} else {
		if item.Type != kefw2.ContentTypeContainer || strings.TrimSpace(item.Path) == "" {
			return page, errors.New("a container with a nonempty path is required")
		}
		if service == playerDiscoveryUPnP && !playerDiscoveryUPnPPath(item.Path) {
			return page, errors.New("a speaker-provided UPnP container path is required")
		}
		page.path = item.Path
	}

	rows, err := client.GetRows(page.path, offset, offset+limit-1)
	if err = playerDiscoveryRequestError(ctx, err); err != nil {
		return page, fmt.Errorf("browsing %q: %w", page.path, err)
	}
	return playerDiscoveryReadPage(ctx, client, page, rows)
}

func playerDiscoveryFavorites(
	ctx context.Context, client playerDiscoveryFavoritesClient, service playerDiscoveryService, offset, limit int,
) (playerDiscoveryPage, error) {
	page := playerDiscoveryPage{
		kind: playerDiscoveryFavoritesPage, service: service, offset: offset, limit: limit,
	}
	if err := playerDiscoveryPageArgs(ctx, offset, limit); err != nil {
		return page, err
	}
	if client == nil {
		return page, errors.New("discovery client is required")
	}

	var rows *kefw2.RowsResponse
	var err error
	switch service {
	case playerDiscoveryRadio:
		rows, err = client.GetRadioFavoritesPage(offset, limit)
	case playerDiscoveryPodcast:
		rows, err = client.GetPodcastFavoritesPage(offset, limit)
	default:
		return page, fmt.Errorf("favorites are not supported for %q", service)
	}
	if err = playerDiscoveryRequestError(ctx, err); err != nil {
		return page, fmt.Errorf("loading %s favorites: %w", service, err)
	}
	return playerDiscoveryReadPage(ctx, client, page, rows)
}

func playerDiscoveryPageArgs(ctx context.Context, offset, limit int) error {
	if err := playerDiscoveryContext(ctx); err != nil {
		return err
	}
	if offset < 0 || offset > playerDiscoveryMaxOffset || limit < 1 || limit > playerDiscoveryMaxPageSize {
		return fmt.Errorf("offset must be 0..%d and limit 1..%d",
			playerDiscoveryMaxOffset, playerDiscoveryMaxPageSize)
	}
	return nil
}

func playerDiscoveryReadPage(
	ctx context.Context, client playerDiscoveryRowsClient, page playerDiscoveryPage, rows *kefw2.RowsResponse,
) (playerDiscoveryPage, error) {
	visited := make(map[string]bool)
	if page.path != "" {
		visited[page.path] = true
	}
	for redirects := 0; ; redirects++ {
		if rows == nil {
			return page, errors.New("service returned no page")
		}
		if len(rows.Rows) != 0 || rows.RowsRedirect == "" {
			break
		}
		path := rows.RowsRedirect
		if strings.TrimSpace(path) == "" || visited[path] {
			return page, fmt.Errorf("invalid or cyclic discovery redirect %q", path)
		}
		if redirects >= playerDiscoveryMaxRedirects {
			return page, errors.New("too many discovery redirects")
		}
		visited[path] = true
		var err error
		rows, err = client.GetRows(path, page.offset, page.offset+page.limit-1)
		if err = playerDiscoveryRequestError(ctx, err); err != nil {
			return page, fmt.Errorf("following discovery redirect %q: %w", path, err)
		}
	}

	if rows.RowsCount < 0 || len(rows.Rows) > page.limit {
		return page, errors.New("service returned an invalid page size or total")
	}
	next := page.offset + len(rows.Rows)
	if len(rows.Rows) == 0 && page.offset < rows.RowsCount {
		return page, fmt.Errorf("incomplete page at offset %d of %d", page.offset, rows.RowsCount)
	}
	if len(rows.Rows) > 0 && next > rows.RowsCount {
		return page, errors.New("service returned more rows than its reported total")
	}
	page.total, page.nextOffset = rows.RowsCount, next
	page.hasMore = next < rows.RowsCount
	page.items = make([]kefw2.ContentItem, len(rows.Rows))
	for i, item := range rows.Rows {
		page.items[i] = playerDiscoveryCopyItem(item)
	}
	return page, nil
}

func playerDiscoveryCopyItem(item kefw2.ContentItem) kefw2.ContentItem {
	if item.MediaData != nil {
		data := *item.MediaData
		data.Resources = append([]kefw2.MediaResource(nil), data.Resources...)
		item.MediaData = &data
	}
	if item.Images != nil {
		images := *item.Images
		images.Images = append([]kefw2.Image(nil), images.Images...)
		item.Images = &images
	}
	if item.Context != nil {
		value := *item.Context
		item.Context = &value
	}
	if item.Value != nil {
		value := *item.Value
		item.Value = &value
	}
	return item
}

type playerDiscoveryStatus string

const (
	playerDiscoveryLoading playerDiscoveryStatus = "loading"
	playerDiscoveryReady   playerDiscoveryStatus = "ready"
	playerDiscoveryEmpty   playerDiscoveryStatus = "empty"
	playerDiscoveryEnd     playerDiscoveryStatus = "end"
	playerDiscoveryError   playerDiscoveryStatus = "error"
)

type playerDiscoveryRequestID struct {
	generation uint64
	sequence   uint64
}

type playerDiscoveryState struct {
	request    playerDiscoveryRequestID
	hasRequest bool
	status     playerDiscoveryStatus
	page       playerDiscoveryPage
	hasPage    bool
	stale      bool
	err        error
}

// Use a separate state per pane. Sequence increases for every request in a
// speaker generation; generations increase even when returning to the same IP.
func playerDiscoveryBegin(state playerDiscoveryState, request playerDiscoveryRequestID) playerDiscoveryState {
	if state.hasRequest {
		if request.generation < state.request.generation ||
			(request.generation == state.request.generation && request.sequence <= state.request.sequence) {
			return state
		}
		if request.generation != state.request.generation {
			state = playerDiscoveryState{}
		}
	}
	state.request, state.hasRequest = request, true
	state.status, state.err, state.stale = playerDiscoveryLoading, nil, state.hasPage
	return state
}

func playerDiscoveryFinish(
	state playerDiscoveryState, request playerDiscoveryRequestID, page playerDiscoveryPage, err error,
) playerDiscoveryState {
	if !state.hasRequest || state.status != playerDiscoveryLoading || state.request != request {
		return state
	}
	if err != nil {
		state.status, state.err, state.stale = playerDiscoveryError, err, state.hasPage
		return state
	}
	state.page, state.hasPage, state.stale, state.err = page, true, false, nil
	switch {
	case len(page.items) > 0:
		state.status = playerDiscoveryReady
	case page.offset == 0:
		state.status = playerDiscoveryEmpty
	default:
		state.status = playerDiscoveryEnd
	}
	return state
}

// A loading or failed request may retain an older page for display, but its
// rows cannot be selected for an action.
func (state playerDiscoveryState) itemAt(index int) (kefw2.ContentItem, error) {
	if !state.hasRequest || !state.hasPage || state.status != playerDiscoveryReady || state.stale {
		return kefw2.ContentItem{}, errors.New("discovery results are not current")
	}
	if index < 0 || index >= len(state.page.items) {
		return kefw2.ContentItem{}, errors.New("discovery selection is out of range")
	}
	return playerDiscoveryCopyItem(state.page.items[index]), nil
}

type playerDiscoveryLocation struct {
	generation uint64
	page       playerDiscoveryPage
	selection  int
}

func playerDiscoveryRemember(
	state playerDiscoveryState, history []playerDiscoveryLocation, selection int,
) ([]playerDiscoveryLocation, error) {
	item, err := state.itemAt(selection)
	if err != nil {
		return history, err
	}
	if item.Type != kefw2.ContentTypeContainer {
		return history, errors.New("only a container can be entered")
	}
	if len(history) > 0 && history[len(history)-1].generation != state.request.generation {
		history = nil
	}
	next := append([]playerDiscoveryLocation(nil), history...)
	next = append(next, playerDiscoveryLocation{
		generation: state.request.generation, page: playerDiscoveryCopyPage(state.page), selection: selection,
	})
	return next, nil
}

// Back uses a new request ID so an in-flight child page cannot replace the
// restored parent. Clear the history when starting an unrelated search.
func playerDiscoveryBack(
	state playerDiscoveryState, history []playerDiscoveryLocation, request playerDiscoveryRequestID,
) (playerDiscoveryState, []playerDiscoveryLocation, int, bool) {
	if len(history) == 0 {
		return state, history, 0, false
	}
	if !state.hasRequest || state.request.generation != request.generation ||
		history[len(history)-1].generation != request.generation {
		return state, nil, 0, false
	}
	if request.sequence <= state.request.sequence {
		return state, history, 0, false
	}
	location := history[len(history)-1]
	state = playerDiscoveryBegin(state, request)
	state = playerDiscoveryFinish(state, request, playerDiscoveryCopyPage(location.page), nil)
	return state, append([]playerDiscoveryLocation(nil), history[:len(history)-1]...), location.selection, true
}

func playerDiscoveryCopyPage(page playerDiscoveryPage) playerDiscoveryPage {
	items := make([]kefw2.ContentItem, len(page.items))
	for i, item := range page.items {
		items[i] = playerDiscoveryCopyItem(item)
	}
	page.items = items
	return page
}

func playerDiscoveryFavorite(
	ctx context.Context, client playerDiscoveryFavoriteClient, service playerDiscoveryService,
	item kefw2.ContentItem, add bool,
) error {
	if err := playerDiscoveryContext(ctx); err != nil {
		return err
	}
	if client == nil {
		return errors.New("discovery client is required")
	}
	if strings.TrimSpace(item.Path) == "" || strings.TrimSpace(item.ID) == "" {
		return errors.New("a favorite requires an exact item path and ID")
	}
	selected := playerDiscoveryCopyItem(item)
	var err error
	switch service {
	case playerDiscoveryRadio:
		if !playerDiscoveryPlayableRadio(item) {
			return errors.New("only radio stations can be favorited")
		}
		if add {
			err = client.AddRadioFavorite(&selected)
		} else {
			err = client.RemoveRadioFavorite(&selected)
		}
	case playerDiscoveryPodcast:
		if item.Type != kefw2.ContentTypeContainer {
			return errors.New("only podcast series can be favorited")
		}
		if add {
			err = client.AddPodcastFavorite(&selected)
		} else {
			err = client.RemovePodcastFavorite(&selected)
		}
	default:
		return fmt.Errorf("favorites are not supported for %q", service)
	}
	if err = playerDiscoveryRequestError(ctx, err); err != nil {
		return fmt.Errorf("changing %s favorite: %w", service, err)
	}
	return nil
}

// client and source must both belong to the same captured speaker. The caller
// serializes this operation with other speaker mutations and binds client to ctx.
func playerDiscoveryPlay(
	ctx context.Context, client playerDiscoveryPlayClient, source playerDiscoverySource,
	service playerDiscoveryService, item kefw2.ContentItem,
) error {
	return playerDiscoveryPlayWithWait(ctx, client, source, service, item, playerDiscoveryWait)
}

func playerDiscoveryPlayWithWait(
	ctx context.Context, client playerDiscoveryPlayClient, source playerDiscoverySource,
	service playerDiscoveryService, item kefw2.ContentItem, wait func(context.Context, time.Duration) error,
) error {
	if err := playerDiscoveryContext(ctx); err != nil {
		return err
	}
	if client == nil || source == nil {
		return errors.New("discovery client and speaker are required")
	}
	if strings.TrimSpace(item.Path) == "" {
		return errors.New("a playable item with an exact path is required")
	}

	var resolved *kefw2.ContentItem
	var err error
	switch service {
	case playerDiscoveryRadio:
		if !playerDiscoveryPlayableRadio(item) {
			return errors.New("only a radio station can be played")
		}
		resolved, err = client.GetRadioStationByPath(item.Path)
	case playerDiscoveryPodcast:
		resolved, err = playerDiscoveryResolvePodcast(client, item)
	default:
		return fmt.Errorf("direct playback is not supported for %q", service)
	}
	if err = playerDiscoveryRequestError(ctx, err); err != nil {
		return fmt.Errorf("resolving %s item %q: %w", service, item.Path, err)
	}
	if resolved == nil || strings.TrimSpace(resolved.Path) == "" {
		return errors.New("service did not resolve a playable item")
	}
	if service == playerDiscoveryPodcast {
		if resolved.Path != item.Path || resolved.Type != kefw2.ContentTypeAudio {
			return errors.New("resolved podcast episode does not match the selected path")
		}
	} else if !playerDiscoveryPlayableRadio(*resolved) {
		return errors.New("resolved radio item is not a station")
	}
	selected := playerDiscoveryCopyItem(*resolved)
	if err := playerDiscoveryEnsureWiFi(ctx, source, wait); err != nil {
		return fmt.Errorf("preparing %s playback: %w", service, err)
	}
	if err := playerDiscoveryContext(ctx); err != nil {
		return err
	}

	// The existing station helper explicitly clears the playlist; the exact
	// podcast helper sends no explicit clear request.
	if service == playerDiscoveryRadio {
		err = client.PlayRadioStation(&selected)
	} else {
		err = client.RequestPodcastEpisodeExact(&selected)
	}
	if err = playerDiscoveryRequestError(ctx, err); err != nil {
		return fmt.Errorf("playing %s item %q: %w", service, item.Path, err)
	}
	return nil
}

func playerDiscoveryResolvePodcast(
	client playerDiscoveryPlayClient, item kefw2.ContentItem,
) (*kefw2.ContentItem, error) {
	if item.Type != kefw2.ContentTypeAudio {
		return nil, errors.New("only a podcast episode can be played")
	}
	// Episode paths may be playable but not browsable. Listing rows already
	// carrying the service metadata and stream must not be resolved again.
	if playerDiscoveryPodcastHasPlaybackData(item) {
		return &item, nil
	}
	return client.GetPodcastDetailsExact(item.Path)
}

func playerDiscoveryPodcastHasPlaybackData(item kefw2.ContentItem) bool {
	if item.MediaData == nil || strings.TrimSpace(item.MediaData.MetaData.ServiceID) == "" ||
		len(item.MediaData.Resources) == 0 {
		return false
	}
	for _, resource := range item.MediaData.Resources {
		if strings.TrimSpace(resource.URI) == "" || strings.TrimSpace(resource.MimeType) == "" {
			return false
		}
	}
	return true
}

func playerDiscoveryPlayableRadio(item kefw2.ContentItem) bool {
	if item.Type == kefw2.ContentTypeAudio {
		return true
	}
	if item.Type == kefw2.ContentTypeContainer && item.ContainerPlayable && item.AudioType == "audioBroadcast" {
		return true
	}
	return (item.Type == "" || item.Type == kefw2.ContentTypeContainer) &&
		item.MediaData != nil && len(item.MediaData.Resources) > 0
}

func playerDiscoveryEnsureWiFi(
	ctx context.Context, source playerDiscoverySource, wait func(context.Context, time.Duration) error,
) error {
	current, err := source.Source(ctx)
	if err = playerDiscoveryRequestError(ctx, err); err != nil {
		return fmt.Errorf("reading speaker source: %w", err)
	}
	if current == kefw2.SourceWiFi {
		return nil
	}

	switchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err = source.SetSource(switchCtx, kefw2.SourceWiFi)
	if err = playerDiscoveryRequestError(switchCtx, err); err != nil {
		return fmt.Errorf("switching to WiFi: %w", err)
	}
	for {
		if err := wait(switchCtx, 500*time.Millisecond); err != nil {
			return fmt.Errorf("waiting for WiFi: %w", playerDiscoveryRequestError(switchCtx, err))
		}
		current, err = source.Source(switchCtx)
		if err = playerDiscoveryRequestError(switchCtx, err); err != nil {
			return fmt.Errorf("checking WiFi source: %w", err)
		}
		if current != kefw2.SourceWiFi {
			continue
		}
		if err := wait(switchCtx, time.Second); err != nil {
			return fmt.Errorf("waiting for player initialization: %w", playerDiscoveryRequestError(switchCtx, err))
		}
		current, err = source.Source(switchCtx)
		if err = playerDiscoveryRequestError(switchCtx, err); err != nil {
			return fmt.Errorf("confirming WiFi source: %w", err)
		}
		if current == kefw2.SourceWiFi {
			return nil
		}
	}
}

func playerDiscoveryWait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

func playerDiscoveryEnqueue(
	ctx context.Context, client playerDiscoveryQueueClient, service playerDiscoveryService, item kefw2.ContentItem,
) error {
	if err := playerDiscoveryContext(ctx); err != nil {
		return err
	}
	if client == nil {
		return errors.New("discovery client is required")
	}
	if service != playerDiscoveryUPnP || item.Type != kefw2.ContentTypeAudio || !playerDiscoveryUPnPPath(item.Path) {
		return errors.New("only an exact UPnP audio track can be enqueued")
	}
	err := client.AddToQueue([]kefw2.ContentItem{playerDiscoveryCopyItem(item)}, false)
	if err = playerDiscoveryRequestError(ctx, err); err != nil {
		return fmt.Errorf("enqueuing UPnP track %q: %w", item.Path, err)
	}
	return nil
}

func playerDiscoveryUPnPPath(path string) bool {
	return strings.HasPrefix(path, "upnp:/") && strings.TrimSpace(strings.TrimPrefix(path, "upnp:/")) != ""
}

func playerDiscoveryContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("a discovery request context is required")
	}
	return ctx.Err()
}

func playerDiscoveryRequestError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		if err == nil {
			return ctxErr
		}
		if !errors.Is(err, ctxErr) {
			return errors.Join(err, ctxErr)
		}
	}
	return err
}
