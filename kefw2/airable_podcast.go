package kefw2

import (
	"fmt"
	"net/url"
	"strings"
)

// Podcast-specific methods for the AirableClient.
// Podcasts are accessed via the Airable feeds service.

// GetPodcastMenu returns the top-level podcast menu.
// Entry point: airable:linkService_airable.feeds.
// Follows multiple redirects until reaching the actual menu content.
func (a *AirableClient) GetPodcastMenu() (*RowsResponse, error) {
	return a.getPodcastMenu(false)
}

// GetPodcastMenuAll returns the complete menu, following podcast redirects.
func (a *AirableClient) GetPodcastMenuAll() (*RowsResponse, error) {
	return a.getPodcastMenu(true)
}

func (a *AirableClient) getPodcastMenu(all bool) (*RowsResponse, error) {
	path := "airable:linkService_airable.feeds"

	// Follow redirects (up to 5 to prevent infinite loops)
	for i := 0; i < 5; i++ {
		var resp *RowsResponse
		var err error
		if all {
			resp, err = a.GetAllRows(path)
		} else {
			resp, err = a.GetRows(path, 0, 100)
		}
		if err != nil {
			return nil, err
		}

		// If we got rows, we're at the final destination
		if len(resp.Rows) > 0 {
			// Cache the base URL if we found the final airable URL
			if strings.HasPrefix(path, "airable:https://") {
				a.PodcastBaseURL = path
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

	return nil, fmt.Errorf("too many redirects when getting podcast menu")
}

// SearchPodcasts searches for podcasts using direct URL pattern.
func (a *AirableClient) SearchPodcasts(query string) (*RowsResponse, error) {
	return a.SearchPodcastsPage(query, 0, 24)
}

// SearchPodcastsPage searches a bounded page of raw service rows.
// offset must be between 0 and 1,000,000; limit must be between 1 and 100.
func (a *AirableClient) SearchPodcastsPage(query string, offset, limit int) (*RowsResponse, error) {
	if offset < 0 || offset > 1_000_000 || limit < 1 || limit > 100 {
		return nil, fmt.Errorf("podcast search requires offset 0..1000000 and limit 1..100")
	}
	baseURL, err := a.getServiceBaseURLPath(ServicePodcast)
	if err != nil {
		return nil, err
	}

	// Build search path with escaped query parameter
	searchPath := fmt.Sprintf("%s/airable/feeds/search\\?q\\=%s", baseURL, url.QueryEscape(query))

	return a.GetRows(searchPath, offset, offset+limit-1)
}

// SearchPodcastsAll searches every result page, failing rather than returning
// incomplete results that could make name selection ambiguous.
func (a *AirableClient) SearchPodcastsAll(query string) (*RowsResponse, error) {
	baseURL, err := a.getServiceBaseURLPath(ServicePodcast)
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/feeds/search\\?q\\=%s", baseURL, url.QueryEscape(query)))
}

// GetPodcastDetailsExact resolves only the requested path. Unlike generic item
// lookup, a container's first child is never treated as the requested item.
func (a *AirableClient) GetPodcastDetailsExact(path string) (*ContentItem, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("podcast path must not be empty")
	}
	resp, err := a.GetAllRows(path)
	if err != nil {
		return nil, err
	}
	if resp.Roles != nil && resp.Roles.Path == path {
		return resp.Roles, nil
	}
	var match *ContentItem
	for i := range resp.Rows {
		if resp.Rows[i].Path == path {
			if match != nil {
				return nil, fmt.Errorf("duplicate podcast details for path: %s", path)
			}
			match = &resp.Rows[i]
		}
	}
	if match == nil {
		return nil, fmt.Errorf("no exact podcast details for path: %s", path)
	}
	return match, nil
}

// PlayPodcastEpisodeExact requests the selected audio item, never its parent
// container (which could start a different episode). Acceptance is not proof
// that the speaker has resolved the stream or started physical playback. The
// playlist is explicitly cleared before the play request.
func (a *AirableClient) PlayPodcastEpisodeExact(episode *ContentItem) error {
	return a.playPodcastEpisodeExact(episode, true)
}

// RequestPodcastEpisodeExact requests the selected audio item without
// explicitly clearing the playlist. Acceptance does not prove playback began.
func (a *AirableClient) RequestPodcastEpisodeExact(episode *ContentItem) error {
	return a.playPodcastEpisodeExact(episode, false)
}

func (a *AirableClient) playPodcastEpisodeExact(episode *ContentItem, clearPlaylist bool) error {
	if episode == nil || episode.Type != ContentTypeAudio || strings.TrimSpace(episode.Path) == "" {
		return fmt.Errorf("an exact audio episode with a nonempty path is required")
	}
	item := *episode
	item.ContainerPlayable = true
	if clearPlaylist {
		return a.playItem(&item)
	}
	return a.requestItemPlayback(&item)
}

// GetPodcastFavorites returns the user's favorite podcasts.
func (a *AirableClient) GetPodcastFavorites() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServicePodcast, "favorites")
}

// GetPodcastFavoritesPage returns a bounded page of favorite podcasts.
// offset must be between 0 and 1,000,000; limit must be between 1 and 100.
func (a *AirableClient) GetPodcastFavoritesPage(offset, limit int) (*RowsResponse, error) {
	return a.getServiceEndpointPage(ServicePodcast, "favorites", offset, limit)
}

// GetPodcastFavoritesAll returns all favorite podcasts (paginated and cached).
func (a *AirableClient) GetPodcastFavoritesAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServicePodcast, "favorites")
}

// GetPodcastHistory returns recently played podcasts.
func (a *AirableClient) GetPodcastHistory() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServicePodcast, "history")
}

// GetPodcastHistoryAll returns all podcast history (paginated and cached).
func (a *AirableClient) GetPodcastHistoryAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServicePodcast, "history")
}

// GetPodcastPopular returns popular podcasts.
func (a *AirableClient) GetPodcastPopular() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServicePodcast, "popular")
}

// GetPodcastPopularAll returns all popular podcasts (paginated and cached).
func (a *AirableClient) GetPodcastPopularAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServicePodcast, "popular")
}

// GetPodcastTrending returns trending podcasts.
func (a *AirableClient) GetPodcastTrending() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServicePodcast, "trending")
}

// GetPodcastTrendingAll returns all trending podcasts (paginated and cached).
func (a *AirableClient) GetPodcastTrendingAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServicePodcast, "trending")
}

// GetPodcastFilter returns the podcast filter menu (genres/languages).
func (a *AirableClient) GetPodcastFilter() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServicePodcast, "filter")
}

// GetPodcastFilterAll returns every podcast filter menu row.
func (a *AirableClient) GetPodcastFilterAll() (*RowsResponse, error) {
	return a.getAllServiceEndpoint(ServicePodcast, "filter")
}

// GetPodcastEpisodesAll returns all episodes for a podcast (paginated and cached).
func (a *AirableClient) GetPodcastEpisodesAll(podcastPath string) (*RowsResponse, error) {
	// If path already ends with /episodes, use it directly
	if strings.HasSuffix(podcastPath, "/episodes") {
		return a.GetAllRows(podcastPath)
	}

	// Otherwise, fetch the podcast content to find the Episodes container
	resp, err := a.GetAllRows(podcastPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get podcast content: %w", err)
	}

	// Find the Episodes container
	var episodesPath string
	for _, item := range resp.Rows {
		if item.Title == episodesTitle && item.Type == ContentTypeContainer {
			if strings.TrimSpace(item.Path) == "" {
				return nil, fmt.Errorf("episodes container has an empty path for podcast: %s", podcastPath)
			}
			if episodesPath != "" {
				return nil, fmt.Errorf("multiple Episodes containers for podcast at path: %s", podcastPath)
			}
			episodesPath = item.Path
		}
	}
	if episodesPath != "" {
		return a.GetAllRows(episodesPath)
	}

	return nil, fmt.Errorf("no Episodes container found for podcast at path: %s", podcastPath)
}

// GetLatestEpisode returns the latest (first) episode of a podcast.
func (a *AirableClient) GetLatestEpisode(podcast *ContentItem) (*ContentItem, error) {
	resp, err := a.GetRows(podcast.Path, 0, 10)
	if err != nil {
		return nil, fmt.Errorf("failed to get podcast content: %w", err)
	}

	// Find the Episodes container in the response
	var episodesPath string
	for _, item := range resp.Rows {
		if item.Title == episodesTitle && item.Type == ContentTypeContainer {
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
	return a.getItemDetails(podcastPath)
}

// PlayPodcastEpisode plays a podcast episode using the player:player/control pattern.
// Due to Airable's authentication requirements, podcast episodes must be played
// through their parent container (episodes listing). This function plays the
// container, which starts playback from the first episode.
//
// Note: Unlike radio stations, individual podcast episode paths cannot be resolved
// directly by the speaker. The speaker needs to resolve streams through the Airable
// session established with the container.
func (a *AirableClient) PlayPodcastEpisode(episode *ContentItem) error {
	// Try to extract or derive the episodes container path from the episode
	containerPath := a.getEpisodesContainerPath(episode)
	if containerPath == "" {
		// Fallback: try direct playback (may fail with "Request was canceled")
		episode.ContainerPlayable = true
		if episode.Type == "" {
			episode.Type = ContentTypeAudio
		}
		return a.playItem(episode)
	}

	// Play the episodes container - the speaker will resolve streams through Airable
	container := &ContentItem{
		Path:              containerPath,
		Title:             episodesTitle,
		Type:              ContentTypeContainer,
		ContainerPlayable: true,
		MediaData: &MediaData{
			MetaData: MediaMetaData{
				ServiceID: "airablePodcasts",
			},
		},
	}

	if err := a.playItem(container); err != nil {
		return fmt.Errorf("failed to play podcast episodes container: %w", err)
	}
	return nil
}

// getEpisodesContainerPath extracts the episodes container path from an episode.
// Episode paths look like: airable:https://xxx.airable.io/id/airable/feed.episode/123
// We need the container: airable:https://xxx.airable.io/airable/feed/PODCAST_ID/episodes
//
// We try to find the container path from:
// 1. The contentPlayContextPath metadata (if it points to a container)
// 2. Cached/known podcast feed paths
// 3. Pattern matching on the episode path.
func (a *AirableClient) getEpisodesContainerPath(episode *ContentItem) string {
	// The episode path format is: airable:https://xxx.airable.io/id/airable/feed.episode/EPISODE_ID
	// We can't directly derive the podcast feed ID from this.
	//
	// However, the contentPlayContextPath often contains the episode path.
	// For now, we return empty and let the caller handle fallback.
	//
	// In the future, we could:
	// 1. Maintain a cache of episode ID -> container path mappings
	// 2. Store the container path when browsing episodes
	// 3. Ask the user to browse to the podcast first

	// Check if we have context information
	if episode.Context != nil && episode.Context.Path != "" {
		// If context points to an episodes container, use it
		if strings.Contains(episode.Context.Path, "/episodes") {
			return episode.Context.Path
		}
	}

	return ""
}

// PlayPodcastByPath plays a podcast episode by its path.
func (a *AirableClient) PlayPodcastByPath(episodePath string) error {
	episode, err := a.GetPodcastDetails(episodePath)
	if err != nil {
		return fmt.Errorf("failed to get episode details: %w", err)
	}

	return a.PlayPodcastEpisode(episode)
}

// AddPodcastFavorite adds a podcast to the user's favorites.
func (a *AirableClient) AddPodcastFavorite(podcast *ContentItem) error {
	return a.modifyFavorite(ServicePodcast, podcast, true)
}

// RemovePodcastFavorite removes a podcast from the user's favorites.
func (a *AirableClient) RemovePodcastFavorite(podcast *ContentItem) error {
	return a.modifyFavorite(ServicePodcast, podcast, false)
}

// BrowsePodcastByDisplayPath browses using a display path (title-based).
// Example: "Favorites" or "Popular/Tech".
func (a *AirableClient) BrowsePodcastByDisplayPath(displayPath string) (*RowsResponse, error) {
	return a.browseByDisplayPath(ServicePodcast, displayPath)
}
