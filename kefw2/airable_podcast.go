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
	path := "airable:linkService_airable.feeds"

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
	baseURL, err := a.getServiceBaseURLPath(ServicePodcast)
	if err != nil {
		return nil, err
	}

	// Build search path with escaped query parameter
	searchPath := fmt.Sprintf("%s/airable/feeds/search\\?q\\=%s", baseURL, url.QueryEscape(query))

	return a.GetRows(searchPath, 0, 23)
}

// GetPodcastFavorites returns the user's favorite podcasts.
func (a *AirableClient) GetPodcastFavorites() (*RowsResponse, error) {
	return a.getServiceEndpoint(ServicePodcast, "favorites")
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

// GetPodcastEpisodesAll returns all episodes for a podcast (paginated and cached).
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
		if item.Title == "Episodes" && item.Type == ContentTypeContainer {
			return a.GetAllRows(item.Path)
		}
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
		if item.Title == "Episodes" && item.Type == ContentTypeContainer {
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
		Title:             "Episodes",
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
