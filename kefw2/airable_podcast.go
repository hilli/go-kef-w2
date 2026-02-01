package kefw2

import (
	"fmt"
	"net/url"
	"strings"
)

// Podcast-specific methods for the AirableClient.
// Podcasts are accessed via the Airable feeds service.

// Podcast represents a podcast with episodes.
type Podcast struct {
	ContentItem
}

// PodcastEpisode represents a single podcast episode.
type PodcastEpisode struct {
	ContentItem
}

// GetPodcastMenu returns the top-level podcast menu.
// Entry point: ui:/airablefeeds
func (a *AirableClient) GetPodcastMenu() (*RowsResponse, error) {
	resp, err := a.GetRows("ui:/airablefeeds", 0, 19)
	if err != nil {
		return nil, err
	}

	// Follow the redirect to get the actual menu
	if resp.RowsRedirect != "" {
		a.PodcastBaseURL = resp.RowsRedirect
		return a.GetRows(resp.RowsRedirect, 0, 19)
	}

	return resp, nil
}

// ensurePodcastBaseURL ensures the podcast base URL is discovered.
// The API uses a redirect: airable:linkService_airable.feeds -> airable:https://...
// We need to follow the redirect to get the final airable:https:// URL.
func (a *AirableClient) ensurePodcastBaseURL() error {
	// Already have the final URL
	if a.PodcastBaseURL != "" && strings.HasPrefix(a.PodcastBaseURL, "airable:https://") {
		return nil
	}

	// Follow redirects starting from the link service entry point
	// Note: ui:/airablefeeds doesn't work, must use the link service directly
	path := "airable:linkService_airable.feeds"
	for i := 0; i < 5; i++ { // Max 5 redirects to prevent infinite loops
		resp, err := a.GetRows(path, 0, 19)
		if err != nil {
			return fmt.Errorf("failed to discover podcast base URL: %w", err)
		}

		// No more redirects - we're at the final destination
		if resp.RowsRedirect == "" {
			break
		}

		path = resp.RowsRedirect

		// Found the final airable:https:// URL
		if strings.HasPrefix(path, "airable:https://") {
			a.PodcastBaseURL = path
			return nil
		}
	}

	// If we get here without finding airable:https://, use whatever we have
	if a.PodcastBaseURL == "" && path != "airable:linkService_airable.feeds" {
		a.PodcastBaseURL = path
	}

	if a.PodcastBaseURL == "" {
		return fmt.Errorf("could not discover podcast base URL")
	}

	return nil
}

// getPodcastBaseURLPath extracts the base path for podcast API calls.
// Converts "airable:https://8448239770.airable.io/airable/feeds" to "airable:https://8448239770.airable.io"
func (a *AirableClient) getPodcastBaseURLPath() (string, error) {
	if err := a.ensurePodcastBaseURL(); err != nil {
		return "", err
	}

	// Extract base URL (remove /airable/feeds suffix if present)
	baseURL := a.PodcastBaseURL
	if idx := strings.Index(baseURL, "/airable/feeds"); idx != -1 {
		baseURL = baseURL[:idx]
	}
	return baseURL, nil
}

// SearchPodcasts searches for podcasts using direct URL pattern.
// Pattern: airable:{baseURL}/airable/feeds/search\?q\={query}
func (a *AirableClient) SearchPodcasts(query string) (*RowsResponse, error) {
	baseURL, err := a.getPodcastBaseURLPath()
	if err != nil {
		return nil, err
	}

	// Build search path with escaped query parameter
	searchPath := fmt.Sprintf("%s/airable/feeds/search\\?q\\=%s", baseURL, url.QueryEscape(query))

	return a.GetRows(searchPath, 0, 23)
}

// GetPodcastFavorites returns the user's favorite podcasts.
func (a *AirableClient) GetPodcastFavorites() (*RowsResponse, error) {
	baseURL, err := a.getPodcastBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetRows(fmt.Sprintf("%s/airable/feeds/favorites", baseURL), 0, 50)
}

// GetPodcastHistory returns recently played podcasts.
func (a *AirableClient) GetPodcastHistory() (*RowsResponse, error) {
	baseURL, err := a.getPodcastBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetRows(fmt.Sprintf("%s/airable/feeds/history", baseURL), 0, 50)
}

// GetPodcastPopular returns popular podcasts.
func (a *AirableClient) GetPodcastPopular() (*RowsResponse, error) {
	baseURL, err := a.getPodcastBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetRows(fmt.Sprintf("%s/airable/feeds/popular", baseURL), 0, 50)
}

// GetPodcastTrending returns trending podcasts.
func (a *AirableClient) GetPodcastTrending() (*RowsResponse, error) {
	baseURL, err := a.getPodcastBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetRows(fmt.Sprintf("%s/airable/feeds/trending", baseURL), 0, 50)
}

// GetPodcastFilter returns the podcast filter menu (genres/languages).
func (a *AirableClient) GetPodcastFilter() (*RowsResponse, error) {
	baseURL, err := a.getPodcastBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetRows(fmt.Sprintf("%s/airable/feeds/filter", baseURL), 0, 50)
}

// GetPodcastFavoritesAll returns all favorite podcasts (paginated and cached).
func (a *AirableClient) GetPodcastFavoritesAll() (*RowsResponse, error) {
	baseURL, err := a.getPodcastBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/feeds/favorites", baseURL))
}

// GetPodcastHistoryAll returns all podcast history (paginated and cached).
func (a *AirableClient) GetPodcastHistoryAll() (*RowsResponse, error) {
	baseURL, err := a.getPodcastBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/feeds/history", baseURL))
}

// GetPodcastPopularAll returns all popular podcasts (paginated and cached).
func (a *AirableClient) GetPodcastPopularAll() (*RowsResponse, error) {
	baseURL, err := a.getPodcastBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/feeds/popular", baseURL))
}

// GetPodcastTrendingAll returns all trending podcasts (paginated and cached).
func (a *AirableClient) GetPodcastTrendingAll() (*RowsResponse, error) {
	baseURL, err := a.getPodcastBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/feeds/trending", baseURL))
}

// GetPodcastEpisodesAll returns all episodes for a podcast (paginated and cached).
// The podcastPath should be either:
// - The podcast's direct path (e.g., .../id/airable/feed/123) - will discover episodes container
// - The episodes container path directly (e.g., .../airable/feed/123/episodes)
func (a *AirableClient) GetPodcastEpisodesAll(podcastPath string) (*RowsResponse, error) {
	// If path already ends with /episodes, use it directly
	if strings.HasSuffix(podcastPath, "/episodes") {
		return a.GetAllRows(podcastPath)
	}

	// Otherwise, fetch the podcast content to find the Episodes container
	resp, err := a.GetRows(podcastPath, 0, 10)
	if err != nil {
		return nil, fmt.Errorf("failed to get podcast content: %w", err)
	}

	// Find the Episodes container
	for _, item := range resp.Rows {
		if item.Title == "Episodes" && item.Type == "container" {
			return a.GetAllRows(item.Path)
		}
	}

	return nil, fmt.Errorf("no Episodes container found for podcast at path: %s", podcastPath)
}

// GetLatestEpisode returns the latest (first) episode of a podcast.
func (a *AirableClient) GetLatestEpisode(podcast *ContentItem) (*ContentItem, error) {
	// First, get the podcast content to find the Episodes container
	// The podcast path (e.g., .../id/airable/feed/123) doesn't directly
	// support /episodes - we need to find the actual episodes container path
	resp, err := a.GetRows(podcast.Path, 0, 10)
	if err != nil {
		return nil, fmt.Errorf("failed to get podcast content: %w", err)
	}

	// Find the Episodes container in the response
	var episodesPath string
	for _, item := range resp.Rows {
		if item.Title == "Episodes" && item.Type == "container" {
			episodesPath = item.Path
			break
		}
	}

	if episodesPath == "" {
		return nil, fmt.Errorf("no Episodes container found for podcast: %s", podcast.Title)
	}

	// Get the first episode from the episodes container
	episodes, err := a.GetRows(episodesPath, 0, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to get episodes: %w", err)
	}

	if len(episodes.Rows) == 0 {
		return nil, fmt.Errorf("no episodes found for podcast: %s", podcast.Title)
	}

	return &episodes.Rows[0], nil
}

// GetPodcastEpisodes returns episodes for a specific podcast.
func (a *AirableClient) GetPodcastEpisodes(podcastPath string) (*RowsResponse, error) {
	return a.GetRows(podcastPath, 0, 50)
}

// GetPodcastDetails fetches full details for a podcast.
func (a *AirableClient) GetPodcastDetails(podcastPath string) (*ContentItem, error) {
	resp, err := a.GetRows(podcastPath, 0, 19)
	if err != nil {
		return nil, err
	}

	// The roles field often contains the podcast details
	if resp.Roles != nil {
		return resp.Roles, nil
	}

	// Or return the first row if available
	if len(resp.Rows) > 0 {
		return &resp.Rows[0], nil
	}

	return nil, fmt.Errorf("no podcast details found for path: %s", podcastPath)
}

// PlayPodcastEpisode plays a podcast episode using the player:player/control pattern.
// Similar to radio playback but for podcast episodes.
func (a *AirableClient) PlayPodcastEpisode(episode *ContentItem) error {
	// First clear the playlist
	if err := a.ClearPlaylist(); err != nil {
		return fmt.Errorf("failed to clear playlist: %w", err)
	}

	// Build the mediaRoles payload (same structure as radio)
	mediaRoles := buildPodcastMediaRoles(episode)

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
		return fmt.Errorf("failed to play podcast episode: %w", err)
	}

	return nil
}

// buildPodcastMediaRoles constructs the mediaRoles payload for podcast playback.
func buildPodcastMediaRoles(episode *ContentItem) map[string]interface{} {
	roles := map[string]interface{}{
		"containerType":     episode.ContainerType,
		"containerPlayable": episode.ContainerPlayable,
		"title":             episode.Title,
		"path":              episode.Path,
		"type":              episode.Type,
	}

	if episode.AudioType != "" {
		roles["audioType"] = episode.AudioType
	}

	if episode.Icon != "" {
		roles["icon"] = episode.Icon
	}

	if episode.ID != "" {
		roles["id"] = episode.ID
	}

	if episode.LongDescription != "" {
		roles["longDescription"] = episode.LongDescription
	}

	// Add media data if available
	if episode.MediaData != nil {
		mediaData := map[string]interface{}{}

		// Metadata
		metaData := map[string]interface{}{
			"serviceID": episode.MediaData.MetaData.ServiceID,
		}
		if episode.MediaData.MetaData.ContentPlayContextPath != "" {
			metaData["contentPlayContextPath"] = episode.MediaData.MetaData.ContentPlayContextPath
		}
		if episode.MediaData.MetaData.PrePlayPath != "" {
			metaData["prePlayPath"] = episode.MediaData.MetaData.PrePlayPath
		}
		if episode.MediaData.MetaData.MaximumRetryCount > 0 {
			metaData["maximumRetryCount"] = episode.MediaData.MetaData.MaximumRetryCount
		}
		mediaData["metaData"] = metaData

		// Resources
		if len(episode.MediaData.Resources) > 0 {
			resources := make([]map[string]interface{}, len(episode.MediaData.Resources))
			for i, res := range episode.MediaData.Resources {
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
				if res.Duration > 0 {
					resources[i]["duration"] = res.Duration
				}
			}
			mediaData["resources"] = resources
		}

		roles["mediaData"] = mediaData
	}

	// Add images if available
	if episode.Images != nil && len(episode.Images.Images) > 0 {
		images := make([]map[string]interface{}, len(episode.Images.Images))
		for i, img := range episode.Images.Images {
			images[i] = map[string]interface{}{
				"url":    img.URL,
				"width":  img.Width,
				"height": img.Height,
			}
		}
		roles["images"] = map[string]interface{}{"images": images}
	}

	// Add context if available
	if episode.Context != nil {
		roles["context"] = map[string]interface{}{
			"path": episode.Context.Path,
		}
	}

	return roles
}

// PlayPodcastByPath plays a podcast episode by its path.
// This fetches the episode details first, then plays it.
func (a *AirableClient) PlayPodcastByPath(episodePath string) error {
	episode, err := a.GetPodcastDetails(episodePath)
	if err != nil {
		return fmt.Errorf("failed to get episode details: %w", err)
	}

	return a.PlayPodcastEpisode(episode)
}

// AddPodcastFavorite adds a podcast to the user's favorites.
// It constructs the action path using the podcast's ID and the discovered base URL.
func (a *AirableClient) AddPodcastFavorite(podcast *ContentItem) error {
	// Extract podcast ID from podcast.ID (e.g., "airable://airable/feed/12345")
	podcastID := extractPodcastID(podcast.ID)
	if podcastID == "" {
		return fmt.Errorf("could not extract podcast ID from: %s", podcast.ID)
	}

	// Get the base URL path (strips /airable/feeds suffix)
	baseURL, err := a.getPodcastBaseURLPath()
	if err != nil {
		return fmt.Errorf("failed to get podcast base URL: %w", err)
	}

	// Convert base URL to action URL format
	// "airable:https://8448239770.airable.io" -> "airable:action:https://8448239770.airable.io"
	// Note: uses "feed" (singular) not "feeds" in the action path
	actionBase := strings.TrimPrefix(baseURL, "airable:")
	actionPath := fmt.Sprintf("airable:action:%s/actions/favorites/airable/feed/%s/insert", actionBase, podcastID)

	payload := map[string]interface{}{
		"path":  actionPath,
		"role":  "activate",
		"value": true,
	}

	_, err = a.SetData(payload)
	if err != nil {
		return fmt.Errorf("failed to add podcast to favorites: %w", err)
	}

	return nil
}

// RemovePodcastFavorite removes a podcast from the user's favorites.
// It constructs the action path using the podcast's ID and the discovered base URL.
func (a *AirableClient) RemovePodcastFavorite(podcast *ContentItem) error {
	// Extract podcast ID from podcast.ID (e.g., "airable://airable/feed/12345")
	podcastID := extractPodcastID(podcast.ID)
	if podcastID == "" {
		return fmt.Errorf("could not extract podcast ID from: %s", podcast.ID)
	}

	// Get the base URL path (strips /airable/feeds suffix)
	baseURL, err := a.getPodcastBaseURLPath()
	if err != nil {
		return fmt.Errorf("failed to get podcast base URL: %w", err)
	}

	// Convert base URL to action URL format
	// Note: uses "feed" (singular) not "feeds" in the action path
	actionBase := strings.TrimPrefix(baseURL, "airable:")
	actionPath := fmt.Sprintf("airable:action:%s/actions/favorites/airable/feed/%s/remove", actionBase, podcastID)

	payload := map[string]interface{}{
		"path":  actionPath,
		"role":  "activate",
		"value": true,
	}

	_, err = a.SetData(payload)
	if err != nil {
		return fmt.Errorf("failed to remove podcast from favorites: %w", err)
	}

	return nil
}

// extractPodcastID extracts the numeric/alphanumeric ID from a podcast ID string.
// Input formats:
//   - "airable://airable.feeds/podcast/12345"
//   - "airable://airable/feeds/12345"
//
// Output: "12345"
func extractPodcastID(id string) string {
	// Split by "/" and find the last non-empty segment
	parts := strings.Split(id, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		if part != "" && part != "podcast" && part != "feeds" {
			return part
		}
	}
	return ""
}

// BrowsePodcastByDisplayPath browses using a display path (title-based).
// It walks the hierarchy to resolve titles to actual API paths.
// Example: "Favorites" or "Popular/Tech"
func (a *AirableClient) BrowsePodcastByDisplayPath(displayPath string) (*RowsResponse, error) {
	if err := a.ensurePodcastBaseURL(); err != nil {
		return nil, err
	}

	// Empty path = top-level menu
	if displayPath == "" {
		return a.GetRows(a.PodcastBaseURL, 0, 100)
	}

	// Parse the display path into segments
	segments := parsePodcastDisplayPath(displayPath)
	if len(segments) == 0 {
		return a.GetRows(a.PodcastBaseURL, 0, 100)
	}

	// Walk the hierarchy, resolving each segment
	currentPath := a.PodcastBaseURL

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

// parsePodcastDisplayPath splits a display path into segments, handling %2F escapes.
func parsePodcastDisplayPath(path string) []string {
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
