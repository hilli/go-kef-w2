package kefw2

import (
	"fmt"
	"net/url"
)

// Radio-specific methods for the AirableClient.
// Internet radio stations are accessed via the Airable service.

// GetRadioMenu returns the top-level radio menu.
// Entry point: ui:/airableradios
func (a *AirableClient) GetRadioMenu() (*RowsResponse, error) {
	resp, err := a.GetRows("ui:/airableradios", 0, 19)
	if err != nil {
		return nil, err
	}

	// Follow the redirect to get the actual menu
	if resp.RowsRedirect != "" {
		a.RadioBaseURL = resp.RowsRedirect
		return a.GetRows(resp.RowsRedirect, 0, 19)
	}

	return resp, nil
}

// SearchRadio searches for radio stations using direct URL pattern.
func (a *AirableClient) SearchRadio(query string) (*RowsResponse, error) {
	baseURL, err := a.getServiceBaseURLPath(ServiceRadio)
	if err != nil {
		return nil, err
	}

	// Build search path with escaped query parameter
	// The API uses backslash escaping: search\?q\=query
	searchPath := fmt.Sprintf("%s/airable/radios/search\\?q\\=%s", baseURL, url.QueryEscape(query))

	return a.GetRows(searchPath, 0, 23)
}

// GetRadioFavorites returns the user's favorite radio stations.
func (a *AirableClient) GetRadioFavorites() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceRadio, "favorites")
}

// GetRadioFavoritesAll returns all favorite radio stations (paginated and cached).
func (a *AirableClient) GetRadioFavoritesAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceRadio, "favorites")
}

// GetRadioLocal returns local radio stations based on geolocation.
func (a *AirableClient) GetRadioLocal() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceRadio, "local")
}

// GetRadioLocalAll returns all local radio stations (paginated and cached).
func (a *AirableClient) GetRadioLocalAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceRadio, "local")
}

// GetRadioPopular returns popular radio stations.
func (a *AirableClient) GetRadioPopular() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceRadio, "popular")
}

// GetRadioPopularAll returns all popular radio stations (paginated and cached).
func (a *AirableClient) GetRadioPopularAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceRadio, "popular")
}

// GetRadioTrending returns trending radio stations.
func (a *AirableClient) GetRadioTrending() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceRadio, "trending")
}

// GetRadioTrendingAll returns all trending radio stations (paginated and cached).
func (a *AirableClient) GetRadioTrendingAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceRadio, "trending")
}

// GetRadioHQ returns high quality radio stations.
func (a *AirableClient) GetRadioHQ() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceRadio, "hq")
}

// GetRadioHQAll returns all high quality radio stations (paginated and cached).
func (a *AirableClient) GetRadioHQAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceRadio, "hq")
}

// GetRadioNew returns new radio stations.
func (a *AirableClient) GetRadioNew() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServiceRadio, "new")
}

// GetRadioNewAll returns all new radio stations (paginated and cached).
func (a *AirableClient) GetRadioNewAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServiceRadio, "new")
}

// GetRadioStationDetails fetches full details for a radio station.
func (a *AirableClient) GetRadioStationDetails(stationPath string) (*ContentItem, error) {
	return a.getItemDetails(stationPath)
}

// PlayRadioStation plays a radio station using the player:player/control pattern.
func (a *AirableClient) PlayRadioStation(station *ContentItem) error {
	if err := a.playItem(station); err != nil {
		return fmt.Errorf("failed to play radio station: %w", err)
	}
	return nil
}

// AddRadioFavorite adds a radio station to the user's favorites.
func (a *AirableClient) AddRadioFavorite(station *ContentItem) error {
	return a.modifyFavorite(ServiceRadio, station, true)
}

// RemoveRadioFavorite removes a radio station from the user's favorites.
func (a *AirableClient) RemoveRadioFavorite(station *ContentItem) error {
	return a.modifyFavorite(ServiceRadio, station, false)
}

// PlayRadioByPath plays a radio station by its path.
func (a *AirableClient) PlayRadioByPath(stationPath string) error {
	station, err := a.GetRadioStationDetails(stationPath)
	if err != nil {
		return fmt.Errorf("failed to get station details: %w", err)
	}

	return a.PlayRadioStation(station)
}

// ResolveAndPlayRadioStation resolves a station to its playable form and plays it.
// Stations from list endpoints (hq, local, trending, etc.) are often containers
// that need to be browsed into multiple levels to find the actual playable stream.
func (a *AirableClient) ResolveAndPlayRadioStation(station *ContentItem) error {
	// If it already has MediaData with resources, play directly
	if station.MediaData != nil && len(station.MediaData.Resources) > 0 {
		return a.PlayRadioStation(station)
	}

	// Navigate into the station's path to find the playable item
	currentPath := station.Path
	maxDepth := 5 // Prevent infinite loops

	for depth := 0; depth < maxDepth; depth++ {
		resp, err := a.GetRows(currentPath, 0, 50)
		if err != nil {
			return fmt.Errorf("failed to browse station: %w", err)
		}

		// Check if the response has Roles with MediaData (this is the playable form)
		if resp.Roles != nil && resp.Roles.MediaData != nil && len(resp.Roles.MediaData.Resources) > 0 {
			return a.PlayRadioStation(resp.Roles)
		}

		// Look for a matching item to navigate into
		var nextItem *ContentItem
		for i := range resp.Rows {
			item := &resp.Rows[i]

			// If this item has MediaData, play it directly
			if item.MediaData != nil && len(item.MediaData.Resources) > 0 {
				return a.PlayRadioStation(item)
			}

			// If this item has the same title as our station, it's likely the path to follow
			if item.Title == station.Title && nextItem == nil {
				nextItem = item
			}

			// Or if it's a containerPlayable item, it might be playable
			if item.ContainerPlayable && item.AudioType == "audioBroadcast" && nextItem == nil {
				nextItem = item
			}
		}

		// If we found a next item to navigate into, continue
		if nextItem != nil {
			// If it's the same path, we're stuck
			if nextItem.Path == currentPath {
				return a.PlayRadioStation(nextItem)
			}
			currentPath = nextItem.Path
			station = nextItem
			continue
		}

		// No matching item found - try to play the first containerPlayable item
		for i := range resp.Rows {
			item := &resp.Rows[i]
			if item.ContainerPlayable && item.AudioType == "audioBroadcast" {
				return a.PlayRadioStation(item)
			}
		}

		break
	}

	// Fallback: try playing the original station directly
	return a.PlayRadioStation(station)
}

// BrowseRadioByDisplayPath browses using a display path (title-based).
// Example: "Favorites" or "by Genre/Jazz"
func (a *AirableClient) BrowseRadioByDisplayPath(displayPath string) (*RowsResponse, error) {
	return a.browseByDisplayPath(ServiceRadio, displayPath)
}

// BrowseRadioByItemPath browses using a ContentItem's Path field directly.
func (a *AirableClient) BrowseRadioByItemPath(itemPath string) (*RowsResponse, error) {
	return a.GetRows(itemPath, 0, 100)
}

// BrowseRadioPath browses the radio hierarchy at the given path.
// Deprecated: Use BrowseRadioByDisplayPath instead.
func (a *AirableClient) BrowseRadioPath(browsePath string) (*RowsResponse, error) {
	return a.BrowseRadioByDisplayPath(browsePath)
}
