package kefw2

import (
	"fmt"
	"net/url"
	"strings"
)

// Radio-specific methods for the AirableClient.
// Internet radio stations are accessed via the Airable service.

// RadioStation represents an internet radio station with full playback information.
type RadioStation struct {
	ContentItem
}

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

// ensureRadioBaseURL ensures the radio base URL is discovered.
// The API uses a two-level redirect: ui:/airableradios -> linkService -> airable:https://...
// We need to follow all redirects until we get the final airable:https:// URL.
func (a *AirableClient) ensureRadioBaseURL() error {
	// Already have the final URL
	if a.RadioBaseURL != "" && strings.HasPrefix(a.RadioBaseURL, "airable:https://") {
		return nil
	}

	// Follow redirects starting from the UI entry point
	path := "ui:/airableradios"
	for i := 0; i < 5; i++ { // Max 5 redirects to prevent infinite loops
		resp, err := a.GetRows(path, 0, 19)
		if err != nil {
			return fmt.Errorf("failed to discover radio base URL: %w", err)
		}

		// No more redirects - we're at the final destination
		if resp.RowsRedirect == "" {
			break
		}

		path = resp.RowsRedirect

		// Found the final airable:https:// URL
		if strings.HasPrefix(path, "airable:https://") {
			a.RadioBaseURL = path
			return nil
		}
	}

	// If we get here without finding airable:https://, use whatever we have
	if a.RadioBaseURL == "" && path != "ui:/airableradios" {
		a.RadioBaseURL = path
	}

	if a.RadioBaseURL == "" {
		return fmt.Errorf("could not discover radio base URL")
	}

	return nil
}

// getRadioBaseURLPath extracts the base path for radio API calls.
// Converts "airable:https://8448239770.airable.io/airable/radios" to "airable:https://8448239770.airable.io"
func (a *AirableClient) getRadioBaseURLPath() (string, error) {
	if err := a.ensureRadioBaseURL(); err != nil {
		return "", err
	}

	// Extract base URL (remove /airable/radios suffix if present)
	baseURL := a.RadioBaseURL
	if idx := strings.Index(baseURL, "/airable/radios"); idx != -1 {
		baseURL = baseURL[:idx]
	}
	return baseURL, nil
}

// SearchRadio searches for radio stations using direct URL pattern.
// This is the correct pattern discovered from HAR: airable:{baseURL}/airable/radios/search\?q\={query}
func (a *AirableClient) SearchRadio(query string) (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
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
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetRows(fmt.Sprintf("%s/airable/radios/favorites", baseURL), 0, 50)
}

// GetRadioFavoritesAll returns all favorite radio stations (paginated and cached).
func (a *AirableClient) GetRadioFavoritesAll() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/radios/favorites", baseURL))
}

// GetRadioHistory returns recently played radio stations.
func (a *AirableClient) GetRadioHistory() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetRows(fmt.Sprintf("%s/airable/radios/history", baseURL), 0, 50)
}

// GetRadioLocal returns local radio stations based on geolocation.
func (a *AirableClient) GetRadioLocal() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetRows(fmt.Sprintf("%s/airable/radios/local", baseURL), 0, 50)
}

// GetRadioLocalAll returns all local radio stations (paginated and cached).
func (a *AirableClient) GetRadioLocalAll() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/radios/local", baseURL))
}

// GetRadioPopular returns popular radio stations.
func (a *AirableClient) GetRadioPopular() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetRows(fmt.Sprintf("%s/airable/radios/popular", baseURL), 0, 50)
}

// GetRadioPopularAll returns all popular radio stations (paginated and cached).
func (a *AirableClient) GetRadioPopularAll() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/radios/popular", baseURL))
}

// GetRadioTrending returns trending radio stations.
func (a *AirableClient) GetRadioTrending() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetRows(fmt.Sprintf("%s/airable/radios/trending", baseURL), 0, 50)
}

// GetRadioTrendingAll returns all trending radio stations (paginated and cached).
func (a *AirableClient) GetRadioTrendingAll() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/radios/trending", baseURL))
}

// GetRadioRecommendations returns recommended radio stations.
func (a *AirableClient) GetRadioRecommendations() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetRows(fmt.Sprintf("%s/airable/radios/recommendations", baseURL), 0, 50)
}

// GetRadioHQ returns high quality radio stations.
func (a *AirableClient) GetRadioHQ() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetRows(fmt.Sprintf("%s/airable/radios/hq", baseURL), 0, 50)
}

// GetRadioHQAll returns all high quality radio stations (paginated and cached).
func (a *AirableClient) GetRadioHQAll() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/radios/hq", baseURL))
}

// GetRadioNew returns new radio stations.
func (a *AirableClient) GetRadioNew() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetRows(fmt.Sprintf("%s/airable/radios/new", baseURL), 0, 50)
}

// GetRadioNewAll returns all new radio stations (paginated and cached).
func (a *AirableClient) GetRadioNewAll() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/radios/new", baseURL))
}

// GetRadioStationDetails fetches full details for a radio station.
func (a *AirableClient) GetRadioStationDetails(stationPath string) (*ContentItem, error) {
	resp, err := a.GetRows(stationPath, 0, 19)
	if err != nil {
		return nil, err
	}

	// The roles field often contains the station details
	if resp.Roles != nil {
		return resp.Roles, nil
	}

	// Or return the first row if available
	if len(resp.Rows) > 0 {
		return &resp.Rows[0], nil
	}

	return nil, fmt.Errorf("no station details found for path: %s", stationPath)
}

// PlayRadioStation plays a radio station using the correct player:player/control pattern.
// This uses the full mediaRoles payload as discovered from HAR analysis.
func (a *AirableClient) PlayRadioStation(station *ContentItem) error {
	// First clear the playlist
	if err := a.ClearPlaylist(); err != nil {
		return fmt.Errorf("failed to clear playlist: %w", err)
	}

	// Build the mediaRoles payload
	mediaRoles := buildRadioMediaRoles(station)

	// Send play command
	payload := map[string]interface{}{
		"path": "player:player/control",
		"role": "activate",
		"value": map[string]interface{}{
			"type":       "none",
			"control":    "play",
			"mediaRoles": mediaRoles,
		},
	}

	_, err := a.SetData(payload)
	if err != nil {
		return fmt.Errorf("failed to play radio station: %w", err)
	}

	return nil
}

// AddRadioFavorite adds a radio station to the user's favorites.
// It constructs the action path using the station's ID and the discovered base URL.
func (a *AirableClient) AddRadioFavorite(station *ContentItem) error {
	// Extract station ID from station.ID (e.g., "airable://airable.radios/station/1022963300812989")
	stationID := extractStationID(station.ID)
	if stationID == "" {
		return fmt.Errorf("could not extract station ID from: %s", station.ID)
	}

	// Get the base URL path (strips /airable/radios suffix)
	baseURLPath, err := a.getRadioBaseURLPath()
	if err != nil {
		return fmt.Errorf("failed to get radio base URL: %w", err)
	}

	// Convert base URL path to action URL format
	// "airable:https://8448239770.airable.io" -> "airable:action:https://8448239770.airable.io"
	baseURL := strings.TrimPrefix(baseURLPath, "airable:")
	actionPath := fmt.Sprintf("airable:action:%s/actions/favorites/airable/radio/%s/insert", baseURL, stationID)

	payload := map[string]interface{}{
		"path":  actionPath,
		"role":  "activate",
		"value": true,
	}

	_, err = a.SetData(payload)
	if err != nil {
		return fmt.Errorf("failed to add radio station to favorites: %w", err)
	}

	return nil
}

// RemoveRadioFavorite removes a radio station from the user's favorites.
// It constructs the action path using the station's ID and the discovered base URL.
func (a *AirableClient) RemoveRadioFavorite(station *ContentItem) error {
	// Extract station ID from station.ID (e.g., "airable://airable.radios/station/1022963300812989")
	stationID := extractStationID(station.ID)
	if stationID == "" {
		return fmt.Errorf("could not extract station ID from: %s", station.ID)
	}

	// Get the base URL path (strips /airable/radios suffix)
	baseURLPath, err := a.getRadioBaseURLPath()
	if err != nil {
		return fmt.Errorf("failed to get radio base URL: %w", err)
	}

	// Convert base URL path to action URL format
	// "airable:https://8448239770.airable.io" -> "airable:action:https://8448239770.airable.io"
	baseURL := strings.TrimPrefix(baseURLPath, "airable:")
	actionPath := fmt.Sprintf("airable:action:%s/actions/favorites/airable/radio/%s/remove", baseURL, stationID)

	payload := map[string]interface{}{
		"path":  actionPath,
		"role":  "activate",
		"value": true,
	}

	_, err = a.SetData(payload)
	if err != nil {
		return fmt.Errorf("failed to remove radio station from favorites: %w", err)
	}

	return nil
}

// extractStationID extracts the numeric ID from a station ID string.
// Input formats:
//   - "airable://airable.radios/station/1022963300812989"
//   - "airable://airable/radio/1022963300812989"
//
// Output: "1022963300812989"
func extractStationID(id string) string {
	// Split by "/" and find the last numeric segment
	parts := strings.Split(id, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		if part == "" {
			continue
		}
		// Check if this part is all digits
		if isNumericString(part) {
			return part
		}
	}
	return ""
}

// isNumericString checks if a string contains only digits.
func isNumericString(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// buildRadioMediaRoles constructs the mediaRoles payload for radio playback.
func buildRadioMediaRoles(station *ContentItem) map[string]interface{} {
	roles := map[string]interface{}{
		"containerType":     "none",
		"audioType":         station.AudioType,
		"containerPlayable": station.ContainerPlayable,
		"title":             station.Title,
		"path":              station.Path,
		"type":              "audio",
	}

	if station.Icon != "" {
		roles["icon"] = station.Icon
	}

	if station.ID != "" {
		roles["id"] = station.ID
	}

	if station.LongDescription != "" {
		roles["longDescription"] = station.LongDescription
	}

	// Add media data if available
	if station.MediaData != nil {
		mediaData := map[string]interface{}{}

		// Metadata
		metaData := map[string]interface{}{
			"live":      station.MediaData.MetaData.Live,
			"serviceID": station.MediaData.MetaData.ServiceID,
		}
		if station.MediaData.MetaData.ContentPlayContextPath != "" {
			metaData["contentPlayContextPath"] = station.MediaData.MetaData.ContentPlayContextPath
		}
		if station.MediaData.MetaData.PrePlayPath != "" {
			metaData["prePlayPath"] = station.MediaData.MetaData.PrePlayPath
		}
		if station.MediaData.MetaData.MaximumRetryCount > 0 {
			metaData["maximumRetryCount"] = station.MediaData.MetaData.MaximumRetryCount
		}
		mediaData["metaData"] = metaData

		// Resources
		if len(station.MediaData.Resources) > 0 {
			resources := make([]map[string]interface{}, len(station.MediaData.Resources))
			for i, res := range station.MediaData.Resources {
				resources[i] = map[string]interface{}{
					"uri":      res.URI,
					"mimeType": res.MimeType,
				}
				if res.BitRate > 0 {
					resources[i]["bitRate"] = res.BitRate
				}
				if res.Codec != "" {
					resources[i]["codec"] = res.Codec
				}
			}
			mediaData["resources"] = resources
		}

		roles["mediaData"] = mediaData
	}

	// Add images if available
	if station.Images != nil && len(station.Images.Images) > 0 {
		images := make([]map[string]interface{}, len(station.Images.Images))
		for i, img := range station.Images.Images {
			images[i] = map[string]interface{}{
				"url":    img.URL,
				"width":  img.Width,
				"height": img.Height,
			}
		}
		roles["images"] = map[string]interface{}{"images": images}
	}

	// Add context if available
	if station.Context != nil {
		roles["context"] = map[string]interface{}{
			"path": station.Context.Path,
		}
	}

	return roles
}

// PlayRadioByPath plays a radio station by its path.
// This fetches the station details first, then plays it.
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
// This function navigates into the station (following same-named entries) until
// it finds an item with MediaData, then plays it.
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
		// Priority: 1) item with MediaData, 2) item with same title, 3) first playable item
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
				// Try to play this item anyway
				return a.PlayRadioStation(nextItem)
			}
			currentPath = nextItem.Path
			station = nextItem // Update station for title matching
			continue
		}

		// No matching item found - try to play the first containerPlayable item
		for i := range resp.Rows {
			item := &resp.Rows[i]
			if item.ContainerPlayable && item.AudioType == "audioBroadcast" {
				return a.PlayRadioStation(item)
			}
		}

		// Nothing playable found
		break
	}

	// Fallback: try playing the original station directly
	return a.PlayRadioStation(station)
}

// BrowseRadioPath browses the radio hierarchy at the given path.
// If browsePath is empty, returns the top-level radio menu categories.
// Path segments are separated by "/" - use %2F for literal slashes in names.
//
// Example paths:
//   - "" (empty) -> top-level categories
//   - "by Genre" -> list of genres
//   - "by Genre/Jazz" -> Jazz stations
//   - "by Genre/Rock/AC%2FDC" -> AC/DC stations (escaped slash)
func (a *AirableClient) BrowseRadioPath(browsePath string) (*RowsResponse, error) {
	if err := a.ensureRadioBaseURL(); err != nil {
		return nil, err
	}

	// Empty path = top-level menu
	if browsePath == "" {
		return a.GetRows(a.RadioBaseURL, 0, 100)
	}

	// Unescape %2F back to / for the API call
	apiPath := strings.ReplaceAll(browsePath, "%2F", "/")

	// Build full API path from browse path
	fullPath := a.RadioBaseURL
	if !strings.HasSuffix(fullPath, "/") {
		fullPath += "/"
	}
	fullPath += apiPath

	return a.GetRows(fullPath, 0, 100)
}

// BrowseRadioByItemPath browses using a ContentItem's Path field directly.
// Use this when you have a ContentItem from a previous browse and want to navigate into it.
func (a *AirableClient) BrowseRadioByItemPath(itemPath string) (*RowsResponse, error) {
	return a.GetRows(itemPath, 0, 100)
}

// BrowseRadioByDisplayPath browses using a display path (title-based).
// It walks the hierarchy to resolve titles to actual API paths.
// Example: "Favorites" or "by Genre/Jazz"
func (a *AirableClient) BrowseRadioByDisplayPath(displayPath string) (*RowsResponse, error) {
	if err := a.ensureRadioBaseURL(); err != nil {
		return nil, err
	}

	// Empty path = top-level menu
	if displayPath == "" {
		return a.GetRows(a.RadioBaseURL, 0, 100)
	}

	// Parse the display path into segments
	segments := parseDisplayPath(displayPath)
	if len(segments) == 0 {
		return a.GetRows(a.RadioBaseURL, 0, 100)
	}

	// Walk the hierarchy, resolving each segment
	currentPath := a.RadioBaseURL

	for _, segment := range segments {
		// Get items at current level
		resp, err := a.GetRows(currentPath, 0, 100)
		if err != nil {
			return nil, fmt.Errorf("failed to browse %s: %w", currentPath, err)
		}

		// Find the item matching this segment title
		found := false
		for _, item := range resp.Rows {
			if item.Title == segment {
				currentPath = item.Path
				found = true
				break
			}
		}

		if !found {
			return nil, fmt.Errorf("item not found: %s", segment)
		}
	}

	// Return items at the resolved path
	return a.GetRows(currentPath, 0, 100)
}

// parseDisplayPath splits a display path into segments, handling %2F escapes.
func parseDisplayPath(path string) []string {
	if path == "" {
		return nil
	}

	// Use a placeholder for escaped slashes, split, then unescape
	const placeholder = "\x00"
	escaped := strings.ReplaceAll(path, "%2F", placeholder)
	segments := strings.Split(escaped, "/")

	// Unescape and clean up
	result := make([]string, 0, len(segments))
	for _, seg := range segments {
		seg = strings.ReplaceAll(seg, placeholder, "/")
		if seg != "" {
			result = append(result, seg)
		}
	}
	return result
}
