package kefw2

import (
	"fmt"
	"net/url"
	"strings"
)

// Deezer-specific methods for the AirableClient.
// Deezer music streaming is accessed via the Airable service.

// GetDeezerMenu returns the top-level Deezer menu.
// Entry point: ui:/airabledeezer.
// Follows multiple redirects until reaching the actual menu content.
func (a *AirableClient) GetDeezerMenu() (*RowsResponse, error) {
	path := "ui:/airabledeezer"

	// Follow redirects (up to 5 to prevent infinite loops)
	for i := 0; i < 5; i++ {
		resp, err := a.GetRows(path, 0, 100)
		if err != nil {
			return nil, err
		}

		// If we got rows, we're at the final destination
		if len(resp.Rows) > 0 {
			// Cache the base URL if we found the final airable URL
			if strings.HasPrefix(path, "airable:https://") {
				a.DeezerBaseURL = path
			}
			return resp, nil
		}

		// If there's a redirect, follow it
		if resp.RowsRedirect != "" {
			path = resp.RowsRedirect
			continue
		}

		// No rows and no redirect - return what we have
		return resp, nil
	}

	return nil, fmt.Errorf("too many redirects when getting Deezer menu")
}

// SearchDeezer searches for content on Deezer using direct URL pattern.
func (a *AirableClient) SearchDeezer(query string) (*RowsResponse, error) {
	baseURL, err := a.getServiceBaseURLPath(ServiceDeezer)
	if err != nil {
		return nil, err
	}

	config := serviceConfigs[ServiceDeezer]
	// Deezer search uses: /deezer/search\?q\=query&t\=all
	searchPath := fmt.Sprintf("%s%s/search\\?q\\=%s&t\\=all", baseURL, config.urlPath, url.QueryEscape(query))

	return a.GetRows(searchPath, 0, 50)
}

// GetDeezerCharts returns the Deezer charts menu (tracks, albums, etc.).
func (a *AirableClient) GetDeezerCharts() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceDeezer, "charts")
}

// GetDeezerChartsAll returns all Deezer charts items (paginated and cached).
func (a *AirableClient) GetDeezerChartsAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceDeezer, "charts")
}

// GetDeezerChartsTracks returns chart tracks from Deezer.
func (a *AirableClient) GetDeezerChartsTracks() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceDeezer, "charts/tracks")
}

// GetDeezerChartsTracksAll returns all chart tracks (paginated and cached).
func (a *AirableClient) GetDeezerChartsTracksAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceDeezer, "charts/tracks")
}

// GetDeezerChartsAlbums returns chart albums from Deezer.
func (a *AirableClient) GetDeezerChartsAlbums() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceDeezer, "charts/albums")
}

// GetDeezerChartsAlbumsAll returns all chart albums (paginated and cached).
func (a *AirableClient) GetDeezerChartsAlbumsAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceDeezer, "charts/albums")
}

// GetDeezerRecommendations returns Deezer recommendations.
func (a *AirableClient) GetDeezerRecommendations() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceDeezer, "listen")
}

// GetDeezerRecommendationsAll returns all Deezer recommendations (paginated and cached).
func (a *AirableClient) GetDeezerRecommendationsAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceDeezer, "listen")
}

// GetDeezerMixes returns Deezer mixes/programs.
func (a *AirableClient) GetDeezerMixes() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceDeezer, "programs")
}

// GetDeezerMixesAll returns all Deezer mixes (paginated and cached).
func (a *AirableClient) GetDeezerMixesAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceDeezer, "programs")
}

// GetDeezerGenres returns the Deezer genres listing.
func (a *AirableClient) GetDeezerGenres() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceDeezer, "genres")
}

// GetDeezerGenresAll returns all Deezer genres (paginated and cached).
func (a *AirableClient) GetDeezerGenresAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceDeezer, "genres")
}

// GetDeezerLibrary returns the Deezer library menu (tracks, albums, playlists, etc.).
func (a *AirableClient) GetDeezerLibrary() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceDeezer, "library")
}

// GetDeezerLibraryTracks returns the user's library tracks.
func (a *AirableClient) GetDeezerLibraryTracks() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceDeezer, "library/tracks")
}

// GetDeezerLibraryTracksAll returns all library tracks (paginated and cached).
func (a *AirableClient) GetDeezerLibraryTracksAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceDeezer, "library/tracks")
}

// GetDeezerLibraryAlbums returns the user's library albums.
func (a *AirableClient) GetDeezerLibraryAlbums() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceDeezer, "library/albums")
}

// GetDeezerLibraryAlbumsAll returns all library albums (paginated and cached).
func (a *AirableClient) GetDeezerLibraryAlbumsAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceDeezer, "library/albums")
}

// GetDeezerLibraryPlaylists returns the user's library playlists.
func (a *AirableClient) GetDeezerLibraryPlaylists() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceDeezer, "library/playlists")
}

// GetDeezerLibraryPlaylistsAll returns all library playlists (paginated and cached).
func (a *AirableClient) GetDeezerLibraryPlaylistsAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceDeezer, "library/playlists")
}

// GetDeezerLibraryHistory returns the user's listening history.
func (a *AirableClient) GetDeezerLibraryHistory() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceDeezer, "library/history")
}

// GetDeezerLibraryHistoryAll returns all listening history (paginated and cached).
func (a *AirableClient) GetDeezerLibraryHistoryAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceDeezer, "library/history")
}

// GetDeezerMoods returns available mood streams (Flow, Happy, Workout, etc.)
// by fetching the main menu and filtering for audio items with audioBroadcast type.
func (a *AirableClient) GetDeezerMoods() ([]ContentItem, error) {
	menu, err := a.GetDeezerMenu()
	if err != nil {
		return nil, fmt.Errorf("failed to get Deezer menu: %w", err)
	}

	var moods []ContentItem
	for _, item := range menu.Rows {
		if item.Type == "audio" && item.AudioType == "audioBroadcast" {
			moods = append(moods, item)
		}
	}

	return moods, nil
}

// PlayDeezerTrack plays a Deezer track using the player:player/control pattern.
func (a *AirableClient) PlayDeezerTrack(track *ContentItem) error {
	// Audio broadcasts (Flow, moods) are streams — play directly via mediaRoles
	if track.AudioType == "audioBroadcast" {
		if err := a.playItem(track); err != nil {
			return fmt.Errorf("failed to play Deezer stream: %w", err)
		}
		return nil
	}

	// Regular tracks use queue-based playback (clear → add → play with trackRoles)
	if err := a.ClearPlaylist(); err != nil {
		return fmt.Errorf("failed to clear playlist: %w", err)
	}
	if err := a.AddToQueue([]ContentItem{*track}, true); err != nil {
		return fmt.Errorf("failed to play Deezer track: %w", err)
	}
	return nil
}

// PlayDeezerMood finds and plays a Deezer mood stream by name.
// If moodName is empty, plays the default "Flow" mood.
func (a *AirableClient) PlayDeezerMood(moodName string) error {
	if moodName == "" {
		moodName = "Flow"
	}

	moods, err := a.GetDeezerMoods()
	if err != nil {
		return err
	}

	// Find the mood by name (case-insensitive)
	for i := range moods {
		if strings.EqualFold(moods[i].Title, moodName) {
			return a.PlayDeezerTrack(&moods[i])
		}
	}

	// Try substring match
	for i := range moods {
		if strings.Contains(strings.ToLower(moods[i].Title), strings.ToLower(moodName)) {
			return a.PlayDeezerTrack(&moods[i])
		}
	}

	available := make([]string, len(moods))
	for i, m := range moods {
		available[i] = m.Title
	}
	return fmt.Errorf("mood '%s' not found. Available moods: %s", moodName, strings.Join(available, ", "))
}

// AddDeezerFavorite adds an item to the user's Deezer favorites.
// Deezer uses a different action path pattern than radio/podcasts:
// https://.../actions/deezer/{type}/{id}/favorites/insert
func (a *AirableClient) AddDeezerFavorite(item *ContentItem) error {
	return a.modifyDeezerFavorite(item, true)
}

// RemoveDeezerFavorite removes an item from the user's Deezer favorites.
func (a *AirableClient) RemoveDeezerFavorite(item *ContentItem) error {
	return a.modifyDeezerFavorite(item, false)
}

// modifyDeezerFavorite adds or removes an item from Deezer favorites.
// Deezer favorites use: airable:action:https://.../actions/deezer/{type}/{id}/favorites/{action}
func (a *AirableClient) modifyDeezerFavorite(item *ContentItem, add bool) error {
	// Extract the item type and ID from the item's ID field
	// Format: airable://deezer/{type}/{id} (e.g., "airable://deezer/track/97311944")
	itemType, itemID := parseDeezerID(item.ID)
	if itemID == "" {
		return fmt.Errorf("could not extract Deezer item ID from: %s", item.ID)
	}

	baseURLPath, err := a.getServiceBaseURLPath(ServiceDeezer)
	if err != nil {
		return fmt.Errorf("failed to get Deezer base URL: %w", err)
	}

	action := "insert"
	if !add {
		action = "remove"
	}

	// Convert base URL path to action URL format
	baseURL := strings.TrimPrefix(baseURLPath, "airable:")
	actionPath := fmt.Sprintf("airable:action:%s/actions/deezer/%s/%s/favorites/%s", baseURL, itemType, itemID, action)

	payload := map[string]interface{}{
		"path":  actionPath,
		"role":  "activate",
		"value": true,
	}

	_, err = a.SetData(payload)
	if err != nil {
		actionName := "add"
		if !add {
			actionName = "remove"
		}
		return fmt.Errorf("failed to %s Deezer favorite: %w", actionName, err)
	}

	return nil
}

// parseDeezerID extracts the type and ID from a Deezer item ID.
// Input: "airable://deezer/track/97311944" → ("track", "97311944")
// Input: "airable://deezer/album/847240702" → ("album", "847240702")
// Input: "airable://deezer/artist/537" → ("artist", "537")
func parseDeezerID(id string) (string, string) {
	// Remove the airable:// prefix
	trimmed := strings.TrimPrefix(id, "airable://deezer/")
	if trimmed == id {
		return "", "" // Not a Deezer ID
	}

	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}

	return parts[0], parts[1]
}

// GetDeezerItemDetails fetches full details for a Deezer item.
func (a *AirableClient) GetDeezerItemDetails(itemPath string) (*ContentItem, error) {
	return a.getItemDetails(itemPath)
}

// BrowseDeezerByDisplayPath browses using a display path (title-based).
// Example: "Charts/Tracks" or "Genres/Rock".
func (a *AirableClient) BrowseDeezerByDisplayPath(displayPath string) (*RowsResponse, error) {
	return a.browseByDisplayPath(ServiceDeezer, displayPath)
}

// ResolveAndPlayDeezerItem resolves an item to its playable form and plays it.
// Audio streams (moods) are played directly via mediaRoles.
// Audio tracks use queue-based playback with trackRoles.
// Containers (albums, playlists) are opened, tracks fetched, and queued.
func (a *AirableClient) ResolveAndPlayDeezerItem(item *ContentItem) error {
	// Audio items can be played directly
	if item.Type == "audio" {
		return a.PlayDeezerTrack(item)
	}

	// Containers: navigate in, collect audio tracks, and play them all
	resp, err := a.GetRows(item.Path, 0, 100)
	if err != nil {
		return fmt.Errorf("failed to browse Deezer item: %w", err)
	}

	var tracks []ContentItem
	for i := range resp.Rows {
		if resp.Rows[i].Type == "audio" && resp.Rows[i].AudioType != "audioBroadcast" {
			tracks = append(tracks, resp.Rows[i])
		}
	}

	if len(tracks) > 0 {
		if err := a.ClearPlaylist(); err != nil {
			return fmt.Errorf("failed to clear playlist: %w", err)
		}
		return a.AddToQueue(tracks, true)
	}

	// Fallback: try to play the first audio item (even if broadcast)
	for i := range resp.Rows {
		if resp.Rows[i].Type == "audio" {
			return a.PlayDeezerTrack(&resp.Rows[i])
		}
	}

	return fmt.Errorf("no playable content found in: %s", item.Title)
}
