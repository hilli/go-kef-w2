package kefw2

import (
	"fmt"
	"net/url"
	"strings"
)

// Podcast-specific methods for the AirableClient.
// Podcasts are accessed via the Airable feeds service.

// GetPodcastMenu returns the top-level podcast menu.
// Entry point: ui:/airablefeeds.
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
func (a *AirableClient) PlayPodcastEpisode(episode *ContentItem) error {
	if err := a.playItem(episode); err != nil {
		return fmt.Errorf("failed to play podcast episode: %w", err)
	}
	return nil
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
