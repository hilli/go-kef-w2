/*
Copyright © 2023-2026 Jens Hilligsøe

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hilli/go-kef-w2/kefw2"
)

// IndexedTrack represents a track in the search index with pre-computed search fields.
type IndexedTrack struct {
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	Path        string `json:"path"`
	Icon        string `json:"icon,omitempty"`
	Duration    int    `json:"duration,omitempty"` // milliseconds
	SearchField string `json:"search_field"`       // Pre-computed lowercase "title artist album"
}

// TrackIndex represents the cached track index for a UPnP server.
type TrackIndex struct {
	ServerPath    string         `json:"server_path"`
	ServerName    string         `json:"server_name"`
	ContainerPath string         `json:"container_path,omitempty"` // Starting container (e.g., Music folder)
	ContainerName string         `json:"container_name,omitempty"` // e.g., "Music"
	Tracks        []IndexedTrack `json:"tracks"`
	IndexedAt     time.Time      `json:"indexed_at"`
	TrackCount    int            `json:"track_count"`
	IndexVersion  int            `json:"index_version"` // For future schema changes
}

const (
	trackIndexVersion  = 1
	trackIndexFilename = "upnp_track_index.json"
	defaultIndexMaxAge = 24 * time.Hour // Default: re-index if older than 24 hours
)

var (
	trackIndexMu sync.RWMutex
)

// getTrackIndexPath returns the path to the track index file.
func getTrackIndexPath() string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	return filepath.Join(cacheDir, "kefw2", trackIndexFilename)
}

// LoadTrackIndex loads the track index from disk.
func LoadTrackIndex() (*TrackIndex, error) {
	trackIndexMu.RLock()
	defer trackIndexMu.RUnlock()

	indexPath := getTrackIndexPath()
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No index yet
		}
		return nil, err
	}

	var index TrackIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}

	// Check version compatibility
	if index.IndexVersion != trackIndexVersion {
		return nil, nil // Index format changed, needs re-indexing
	}

	return &index, nil
}

// SaveTrackIndex saves the track index to disk.
func SaveTrackIndex(index *TrackIndex) error {
	trackIndexMu.Lock()
	defer trackIndexMu.Unlock()

	indexPath := getTrackIndexPath()

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(indexPath, data, 0644)
}

// IsTrackIndexFresh checks if the track index is fresh enough to use.
func IsTrackIndexFresh(index *TrackIndex, maxAge time.Duration) bool {
	if index == nil {
		return false
	}
	return time.Since(index.IndexedAt) < maxAge
}

// BuildTrackIndex scans a UPnP server and builds a searchable track index.
// If containerPath is provided (e.g., "Music/Hilli's Music/All Artists"), it will navigate to that container.
func BuildTrackIndex(client *kefw2.AirableClient, serverPath, serverName, containerPath string, progress kefw2.IndexProgress) (*TrackIndex, error) {
	// Determine the starting path
	startPath := serverPath
	actualContainerName := ""

	if containerPath != "" {
		// Navigate to the specified container
		resolvedPath, resolvedName, err := findContainerByPath(client, serverPath, containerPath)
		if err != nil {
			return nil, fmt.Errorf("could not find container '%s': %w", containerPath, err)
		}
		startPath = resolvedPath
		actualContainerName = resolvedName
	}

	tracks, err := client.GetAllServerTracks(startPath, progress)
	if err != nil {
		return nil, err
	}

	indexedTracks := make([]IndexedTrack, 0, len(tracks))
	for _, track := range tracks {
		it := IndexedTrack{
			Title: track.Title,
			Path:  track.Path,
			Icon:  track.Icon,
		}

		// Extract metadata
		if track.MediaData != nil {
			it.Artist = track.MediaData.MetaData.Artist
			it.Album = track.MediaData.MetaData.Album
			if len(track.MediaData.Resources) > 0 {
				it.Duration = track.MediaData.Resources[0].Duration
			}
		}

		// Build pre-computed search field (lowercase for fast matching)
		searchParts := []string{strings.ToLower(it.Title)}
		if it.Artist != "" {
			searchParts = append(searchParts, strings.ToLower(it.Artist))
		}
		if it.Album != "" {
			searchParts = append(searchParts, strings.ToLower(it.Album))
		}
		it.SearchField = strings.Join(searchParts, " ")

		indexedTracks = append(indexedTracks, it)
	}

	index := &TrackIndex{
		ServerPath:    serverPath,
		ServerName:    serverName,
		ContainerPath: startPath,
		ContainerName: actualContainerName,
		Tracks:        indexedTracks,
		IndexedAt:     time.Now(),
		TrackCount:    len(indexedTracks),
		IndexVersion:  trackIndexVersion,
	}

	return index, nil
}

// findContainerByPath navigates to a container by path like "Music/Hilli's Music/All Artists".
// Each path component is matched case-insensitively.
// If a component doesn't match exactly, it shows available options.
func findContainerByPath(client *kefw2.AirableClient, serverPath, containerPath string) (string, string, error) {
	// Split the path into components
	parts := strings.Split(containerPath, "/")
	// Remove empty parts (handles trailing slashes, etc.)
	var cleanParts []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			cleanParts = append(cleanParts, p)
		}
	}

	if len(cleanParts) == 0 {
		return serverPath, "", nil
	}

	currentPath := serverPath
	var resolvedParts []string

	for i, part := range cleanParts {
		resp, err := client.BrowseContainerAll(currentPath)
		if err != nil {
			return "", "", fmt.Errorf("failed to browse '%s': %w", strings.Join(resolvedParts, "/"), err)
		}

		partLower := strings.ToLower(part)
		var found bool
		var availableContainers []string

		for _, item := range resp.Rows {
			if item.Type == "container" {
				availableContainers = append(availableContainers, item.Title)
				if strings.ToLower(item.Title) == partLower {
					currentPath = item.Path
					resolvedParts = append(resolvedParts, item.Title)
					found = true
					break
				}
			}
		}

		if !found {
			// Build helpful error message
			pathSoFar := ""
			if len(resolvedParts) > 0 {
				pathSoFar = strings.Join(resolvedParts, "/") + "/"
			}
			return "", "", fmt.Errorf("container '%s' not found in '%s'\nAvailable: %s",
				part, pathSoFar, strings.Join(availableContainers, ", "))
		}

		// Only continue if there are more parts
		if i == len(cleanParts)-1 {
			break
		}
	}

	return currentPath, strings.Join(resolvedParts, "/"), nil
}

// listContainersAtPath returns the available containers at a given path (for tab completion).
// containerPath can be empty (returns root containers) or a partial path like "Music/Hilli's Music".
func listContainersAtPath(client *kefw2.AirableClient, serverPath, containerPath string) ([]string, error) {
	// Navigate to the container path first
	currentPath := serverPath
	if containerPath != "" {
		var err error
		currentPath, _, err = findContainerByPath(client, serverPath, containerPath)
		if err != nil {
			return nil, err
		}
	}

	// Browse the current path
	resp, err := client.BrowseContainerAll(currentPath)
	if err != nil {
		return nil, err
	}

	var containers []string
	for _, item := range resp.Rows {
		if item.Type == "container" {
			containers = append(containers, item.Title)
		}
	}

	return containers, nil
}

// SearchTracks searches the track index with fuzzy matching.
// Returns tracks where title, artist, or album matches the query.
func SearchTracks(index *TrackIndex, query string, maxResults int) []IndexedTrack {
	if index == nil || query == "" {
		return nil
	}

	query = strings.ToLower(query)
	queryParts := strings.Fields(query)

	var results []IndexedTrack
	for _, track := range index.Tracks {
		// Check if all query parts match (AND logic)
		allMatch := true
		for _, part := range queryParts {
			if !strings.Contains(track.SearchField, part) {
				// Try fuzzy match as fallback
				if !FuzzyMatch(track.SearchField, part) {
					allMatch = false
					break
				}
			}
		}

		if allMatch {
			results = append(results, track)
			if maxResults > 0 && len(results) >= maxResults {
				break
			}
		}
	}

	return results
}

// IndexedTrackToContentItem converts an IndexedTrack back to a ContentItem for playback.
func IndexedTrackToContentItem(track *IndexedTrack) kefw2.ContentItem {
	item := kefw2.ContentItem{
		Title: track.Title,
		Type:  "audio",
		Path:  track.Path,
		Icon:  track.Icon,
	}

	if track.Artist != "" || track.Album != "" || track.Duration > 0 {
		item.MediaData = &kefw2.MediaData{
			MetaData: kefw2.MediaMetaData{
				Artist:    track.Artist,
				Album:     track.Album,
				ServiceID: "UPnP",
			},
		}
		if track.Duration > 0 {
			item.MediaData.Resources = []kefw2.MediaResource{
				{Duration: track.Duration},
			}
		}
	}

	return item
}

// FormatDuration formats a duration in milliseconds as mm:ss.
func FormatDuration(ms int) string {
	if ms <= 0 {
		return ""
	}
	seconds := ms / 1000
	minutes := seconds / 60
	secs := seconds % 60
	return fmt.Sprintf("%d:%02d", minutes, secs)
}

// GetTrackIndexStatus returns info about the current track index.
func GetTrackIndexStatus() (exists bool, trackCount int, age time.Duration, serverName string) {
	index, err := LoadTrackIndex()
	if err != nil || index == nil {
		return false, 0, 0, ""
	}
	return true, index.TrackCount, time.Since(index.IndexedAt), index.ServerName
}
