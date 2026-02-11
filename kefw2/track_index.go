/*
Copyright 2023-2026 Jens Hilligsoe

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

package kefw2

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// IndexedTrack represents a track in the UPnP search index with pre-computed search fields.
type IndexedTrack struct {
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	Path        string `json:"path"`
	Icon        string `json:"icon,omitempty"`
	Duration    int    `json:"duration,omitempty"`  // milliseconds
	URI         string `json:"uri,omitempty"`       // Audio file URL (required for playback)
	MimeType    string `json:"mime_type,omitempty"` // e.g., "audio/flac"
	SearchField string `json:"search_field"`        // Pre-computed lowercase "title artist album"
}

// TrackIndex represents the cached track index for a UPnP server.
type TrackIndex struct {
	ServerPath    string         `json:"server_path"`
	ServerName    string         `json:"server_name"`
	ContainerPath string         `json:"container_path,omitempty"` // Starting container path
	ContainerName string         `json:"container_name,omitempty"` // e.g., "Music"
	Tracks        []IndexedTrack `json:"tracks"`
	IndexedAt     time.Time      `json:"indexed_at"`
	TrackCount    int            `json:"track_count"`
	IndexVersion  int            `json:"index_version"` // For schema versioning
}

// TrackIndexConfig configures the track index behavior.
type TrackIndexConfig struct {
	// CacheDir is the directory for storing the index file.
	// If empty or "auto", uses os.UserCacheDir()/kefw2
	CacheDir string

	// MemoryCacheTTL is how long to keep the index in memory before re-reading from disk.
	// Default: 1 minute
	MemoryCacheTTL time.Duration

	// MaxAge is how old an index can be before it's considered stale.
	// Default: 24 hours
	MaxAge time.Duration
}

// DefaultTrackIndexConfig returns the default track index configuration.
func DefaultTrackIndexConfig() TrackIndexConfig {
	return TrackIndexConfig{
		CacheDir:       "auto",
		MemoryCacheTTL: time.Minute,
		MaxAge:         24 * time.Hour,
	}
}

const (
	trackIndexVersion  = 2 // Bump when schema changes (v2: added URI and MimeType)
	trackIndexFilename = "upnp_track_index.json"
)

var (
	trackIndexMu       sync.RWMutex
	cachedIndex        *TrackIndex
	cachedIndexTime    time.Time
	trackIndexCacheDir string // Resolved cache directory
)

// initTrackIndexCacheDir resolves and caches the cache directory.
func initTrackIndexCacheDir() string {
	if trackIndexCacheDir != "" {
		return trackIndexCacheDir
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	trackIndexCacheDir = filepath.Join(cacheDir, "kefw2")
	return trackIndexCacheDir
}

// TrackIndexPath returns the path to the track index file.
func TrackIndexPath() string {
	return filepath.Join(initTrackIndexCacheDir(), trackIndexFilename)
}

// LoadTrackIndex loads the track index from disk (no caching).
func LoadTrackIndex() (*TrackIndex, error) {
	trackIndexMu.RLock()
	defer trackIndexMu.RUnlock()

	indexPath := TrackIndexPath()
	data, err := os.ReadFile(indexPath) //nolint:gosec // Path is from user's cache dir
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

// LoadTrackIndexCached loads the track index with in-memory caching.
// The cache is refreshed after MemoryCacheTTL (default 1 minute).
func LoadTrackIndexCached() (*TrackIndex, error) {
	return LoadTrackIndexCachedWithTTL(time.Minute)
}

// LoadTrackIndexCachedWithTTL loads the track index with configurable cache TTL.
func LoadTrackIndexCachedWithTTL(ttl time.Duration) (*TrackIndex, error) {
	trackIndexMu.RLock()
	if cachedIndex != nil && time.Since(cachedIndexTime) < ttl {
		defer trackIndexMu.RUnlock()
		return cachedIndex, nil
	}
	trackIndexMu.RUnlock()

	trackIndexMu.Lock()
	defer trackIndexMu.Unlock()

	// Double-check after acquiring write lock
	if cachedIndex != nil && time.Since(cachedIndexTime) < ttl {
		return cachedIndex, nil
	}

	indexPath := TrackIndexPath()
	data, err := os.ReadFile(indexPath) //nolint:gosec // Path is from user's cache dir
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var index TrackIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}

	if index.IndexVersion != trackIndexVersion {
		return nil, nil
	}

	cachedIndex = &index
	cachedIndexTime = time.Now()
	return cachedIndex, nil
}

// SaveTrackIndex saves the track index to disk.
func SaveTrackIndex(index *TrackIndex) error {
	trackIndexMu.Lock()
	defer trackIndexMu.Unlock()

	indexPath := TrackIndexPath()

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(indexPath), 0750); err != nil {
		return err
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(indexPath, data, 0600); err != nil {
		return err
	}

	// Update in-memory cache
	cachedIndex = index
	cachedIndexTime = time.Now()

	return nil
}

// ClearTrackIndexCache clears the in-memory cache, forcing next load to read from disk.
func ClearTrackIndexCache() {
	trackIndexMu.Lock()
	defer trackIndexMu.Unlock()
	cachedIndex = nil
	cachedIndexTime = time.Time{}
}

// IsTrackIndexFresh checks if the track index is fresh enough to use.
func IsTrackIndexFresh(index *TrackIndex, maxAge time.Duration) bool {
	if index == nil {
		return false
	}
	return time.Since(index.IndexedAt) < maxAge
}

// TrackIndexStatus returns information about the current track index.
func TrackIndexStatus() (exists bool, trackCount int, age time.Duration, serverName string, err error) {
	index, err := LoadTrackIndex()
	if err != nil {
		return false, 0, 0, "", err
	}
	if index == nil {
		return false, 0, 0, "", nil
	}
	return true, index.TrackCount, time.Since(index.IndexedAt), index.ServerName, nil
}

// BuildTrackIndex scans a UPnP server and builds a searchable track index.
// The progress callback (if not nil) is called with (containersScanned, tracksFound, currentContainer).
// If containerPath is provided (e.g., "Music/By Folder"), it navigates to that container first.
func BuildTrackIndex(client *AirableClient, serverPath, serverName, containerPath string, progress IndexProgress) (*TrackIndex, error) {
	startPath := serverPath
	actualContainerName := ""

	if containerPath != "" {
		resolvedPath, resolvedName, err := FindContainerByPath(client, serverPath, containerPath)
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
				it.URI = track.MediaData.Resources[0].URI
				it.MimeType = track.MediaData.Resources[0].MimeType
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

// FindContainerByPath navigates to a container by human-readable path like "Music/By Folder".
// Each path component is matched case-insensitively.
func FindContainerByPath(client *AirableClient, serverPath, containerPath string) (resolvedPath, resolvedName string, err error) {
	parts := strings.Split(containerPath, "/")
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
			if item.Type == ContentTypeContainer {
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
			pathSoFar := ""
			if len(resolvedParts) > 0 {
				pathSoFar = strings.Join(resolvedParts, "/") + "/"
			}
			return "", "", fmt.Errorf("container '%s' not found in '%s'\nAvailable: %s",
				part, pathSoFar, strings.Join(availableContainers, ", "))
		}

		if i == len(cleanParts)-1 {
			break
		}
	}

	return currentPath, strings.Join(resolvedParts, "/"), nil
}

// ListContainersAtPath returns the available containers at a given path (for tab completion).
func ListContainersAtPath(client *AirableClient, serverPath, containerPath string) ([]string, error) {
	currentPath := serverPath
	if containerPath != "" {
		var err error
		currentPath, _, err = FindContainerByPath(client, serverPath, containerPath)
		if err != nil {
			return nil, err
		}
	}

	resp, err := client.BrowseContainerAll(currentPath)
	if err != nil {
		return nil, err
	}

	var containers []string
	for _, item := range resp.Rows {
		if item.Type == ContentTypeContainer {
			containers = append(containers, item.Title)
		}
	}

	return containers, nil
}

// ============================================
// Search Functions
// ============================================

// Score constants for relevance ranking.
const (
	scoreExactField   = 100 // Exact match on entire field
	scoreExactWord    = 50  // Exact word match
	scoreWordPrefix   = 20  // Word starts with query
	scoreWordBoundary = 10  // Query at word boundary
	scoreArtistBonus  = 5   // Bonus for artist match
	scoreAlbumBonus   = 3   // Bonus for album match
	scoreTitleBonus   = 2   // Bonus for title match
)

type scoredTrack struct {
	track IndexedTrack
	score int
}

// SearchTracks searches the track index for matching tracks.
// Supports special query prefixes for filtered search:
//   - artist:"Artist Name" - filter by artist (exact match)
//   - album:"Album Name" - filter by album (exact match)
//
// Without a prefix, searches across title, artist, and album with relevance ranking.
func SearchTracks(index *TrackIndex, query string, maxResults int) []IndexedTrack {
	if index == nil || query == "" {
		return nil
	}

	queryLower := strings.ToLower(query)

	// Handle artist:"..." filter
	if strings.HasPrefix(queryLower, "artist:") {
		artistQuery := extractQuotedValue(query[7:])
		return FilterByArtist(index, artistQuery, maxResults)
	}

	// Handle album:"..." filter
	if strings.HasPrefix(queryLower, "album:") {
		albumQuery := extractQuotedValue(query[6:])
		return FilterByAlbum(index, albumQuery, maxResults)
	}

	// Standard search with relevance ranking
	queryParts := strings.Fields(queryLower)

	var scored []scoredTrack
	for _, track := range index.Tracks {
		score := scoreTrack(&track, queryParts)
		if score > 0 {
			scored = append(scored, scoredTrack{track: track, score: score})
		}
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Extract results up to maxResults
	results := make([]IndexedTrack, 0, min(len(scored), maxResults))
	for i := 0; i < len(scored) && (maxResults <= 0 || i < maxResults); i++ {
		results = append(results, scored[i].track)
	}

	return results
}

// extractQuotedValue extracts a value that may be quoted with double quotes.
func extractQuotedValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// FilterByArtist returns all tracks by the given artist (case-insensitive exact match).
// Results are sorted by album, then by title.
func FilterByArtist(index *TrackIndex, artist string, maxResults int) []IndexedTrack {
	if index == nil || artist == "" {
		return nil
	}

	artistLower := strings.ToLower(artist)
	var results []IndexedTrack

	for _, track := range index.Tracks {
		if strings.ToLower(track.Artist) == artistLower {
			results = append(results, track)
		}
	}

	// Sort by album, then title
	sort.Slice(results, func(i, j int) bool {
		if results[i].Album != results[j].Album {
			return results[i].Album < results[j].Album
		}
		return results[i].Title < results[j].Title
	})

	if maxResults > 0 && len(results) > maxResults {
		results = results[:maxResults]
	}

	return results
}

// ArtistAlbum represents a unique album found in artist search results.
type ArtistAlbum struct {
	Album      string // Album name
	Artist     string // Artist name (for display)
	Icon       string // Album art from first track
	TrackCount int    // Number of tracks in this album
}

// AlbumsForArtist extracts unique albums from a list of tracks (typically from FilterByArtist).
// The input tracks are expected to be sorted by album already.
// Returns albums in alphabetical order.
func AlbumsForArtist(tracks []IndexedTrack) []ArtistAlbum {
	if len(tracks) == 0 {
		return nil
	}

	albumMap := make(map[string]*ArtistAlbum)
	var albumOrder []string

	for _, track := range tracks {
		albumKey := strings.ToLower(track.Album)
		if albumKey == "" {
			albumKey = "(unknown album)"
		}
		if existing, ok := albumMap[albumKey]; ok {
			existing.TrackCount++
		} else {
			displayAlbum := track.Album
			if displayAlbum == "" {
				displayAlbum = "(Unknown Album)"
			}
			albumMap[albumKey] = &ArtistAlbum{
				Album:      displayAlbum,
				Artist:     track.Artist,
				Icon:       track.Icon,
				TrackCount: 1,
			}
			albumOrder = append(albumOrder, albumKey)
		}
	}

	// Sort alphabetically by album name
	sort.Strings(albumOrder)

	albums := make([]ArtistAlbum, 0, len(albumOrder))
	for _, key := range albumOrder {
		albums = append(albums, *albumMap[key])
	}

	return albums
}

// FilterByAlbum returns all tracks from the given album (case-insensitive exact match).
// Results are sorted by title.
func FilterByAlbum(index *TrackIndex, album string, maxResults int) []IndexedTrack {
	if index == nil || album == "" {
		return nil
	}

	albumLower := strings.ToLower(album)
	var results []IndexedTrack

	for _, track := range index.Tracks {
		if strings.ToLower(track.Album) == albumLower {
			results = append(results, track)
		}
	}

	// Sort by title
	sort.Slice(results, func(i, j int) bool {
		return results[i].Title < results[j].Title
	})

	if maxResults > 0 && len(results) > maxResults {
		results = results[:maxResults]
	}

	return results
}

// scoreTrack calculates a relevance score for a track.
// Returns 0 if the track doesn't match all query parts.
func scoreTrack(track *IndexedTrack, queryParts []string) int {
	totalScore := 0

	for _, part := range queryParts {
		partScore := scoreQueryPart(track, part)
		if partScore == 0 {
			return 0 // All parts must match
		}
		totalScore += partScore
	}

	return totalScore
}

// scoreQueryPart scores how well a single query part matches the track.
func scoreQueryPart(track *IndexedTrack, part string) int {
	bestScore := 0

	titleLower := strings.ToLower(track.Title)
	artistLower := strings.ToLower(track.Artist)
	albumLower := strings.ToLower(track.Album)

	// Exact field matches
	if artistLower == part {
		bestScore = max(bestScore, scoreExactField+scoreArtistBonus)
	}
	if albumLower == part {
		bestScore = max(bestScore, scoreExactField+scoreAlbumBonus)
	}
	if titleLower == part {
		bestScore = max(bestScore, scoreExactField+scoreTitleBonus)
	}

	// Word-level matching in title
	for _, word := range strings.Fields(titleLower) {
		cleanWord := strings.Trim(word, ".,!?\"'()[]{}:;")
		if cleanWord == part {
			bestScore = max(bestScore, scoreExactWord+scoreTitleBonus)
		} else if strings.HasPrefix(cleanWord, part) {
			bestScore = max(bestScore, scoreWordPrefix+scoreTitleBonus)
		}
	}

	// Word-level matching in artist
	for _, word := range strings.Fields(artistLower) {
		cleanWord := strings.Trim(word, ".,!?\"'()[]{}:;")
		if cleanWord == part {
			bestScore = max(bestScore, scoreExactWord+scoreArtistBonus)
		} else if strings.HasPrefix(cleanWord, part) {
			bestScore = max(bestScore, scoreWordPrefix+scoreArtistBonus)
		}
	}

	// Word-level matching in album
	for _, word := range strings.Fields(albumLower) {
		cleanWord := strings.Trim(word, ".,!?\"'()[]{}:;")
		if cleanWord == part {
			bestScore = max(bestScore, scoreExactWord+scoreAlbumBonus)
		} else if strings.HasPrefix(cleanWord, part) {
			bestScore = max(bestScore, scoreWordPrefix+scoreAlbumBonus)
		}
	}

	// Fallback: substring match on pre-computed search field
	if bestScore == 0 && strings.Contains(track.SearchField, part) {
		bestScore = scoreWordBoundary
	}

	return bestScore
}

// ============================================
// Conversion Helpers
// ============================================

// IndexedTrackToContentItem converts an IndexedTrack to a ContentItem for playback.
func IndexedTrackToContentItem(track *IndexedTrack) ContentItem {
	item := ContentItem{
		Title: track.Title,
		Type:  ContentTypeAudio,
		Path:  track.Path,
		Icon:  track.Icon,
	}

	if track.Artist != "" || track.Album != "" || track.Duration > 0 || track.URI != "" {
		item.MediaData = &MediaData{
			MetaData: MediaMetaData{
				Artist:    track.Artist,
				Album:     track.Album,
				ServiceID: "UPnP",
			},
		}
		if track.Duration > 0 || track.URI != "" {
			item.MediaData.Resources = []MediaResource{
				{
					Duration: track.Duration,
					URI:      track.URI,
					MimeType: track.MimeType,
				},
			}
		}
	}

	return item
}

// FormatTrackDuration formats a duration in milliseconds as mm:ss.
func FormatTrackDuration(ms int) string {
	if ms <= 0 {
		return ""
	}
	seconds := ms / 1000
	minutes := seconds / 60
	secs := seconds % 60
	return fmt.Sprintf("%d:%02d", minutes, secs)
}
