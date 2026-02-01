package kefw2

import (
	"encoding/json"
	"fmt"
	"strings"
)

// UPnP/DLNA media server methods for the AirableClient.
// Access local media servers like Plex, Sonos, etc.

// MediaServer represents a UPnP/DLNA media server.
type MediaServer struct {
	ContentItem
	UUID string // Extracted from path
}

// UPnPTrack represents a track from a UPnP media server.
type UPnPTrack struct {
	ContentItem
}

// GetMediaServers returns the list of available UPnP/DLNA media servers.
// Entry point: ui:/upnp
func (a *AirableClient) GetMediaServers() (*RowsResponse, error) {
	return a.GetRows("ui:/upnp", 0, 19)
}

// BrowseMediaServer browses the root of a media server.
// serverPath should be in format: upnp:/uuid:{uuid}?itemType=server
func (a *AirableClient) BrowseMediaServer(serverPath string) (*RowsResponse, error) {
	return a.GetRows(serverPath, 0, 50)
}

// BrowseContainer browses a container (folder/album) on a media server.
// containerPath should be in format: upnp:/uuid:{uuid}/{containerID}?itemType=container
func (a *AirableClient) BrowseContainer(containerPath string) (*RowsResponse, error) {
	return a.GetRows(containerPath, 0, 100)
}

// BrowseContainerPaged browses a container with pagination.
func (a *AirableClient) BrowseContainerPaged(containerPath string, from, to int) (*RowsResponse, error) {
	return a.GetRows(containerPath, from, to)
}

// SearchMediaServers searches across all media servers.
// Uses the built-in UPnP search functionality.
func (a *AirableClient) SearchMediaServers(query string, searchFields ...string) (*RowsResponse, error) {
	// Default search fields
	fields := "artist,album,title"
	if len(searchFields) > 0 {
		fields = ""
		for i, f := range searchFields {
			if i > 0 {
				fields += ","
			}
			fields += f
		}
	}

	// The search path format from HAR: upnp:searchOnServers?search=artist,album,title
	// Then we need to submit the search query
	searchPath := fmt.Sprintf("upnp:searchOnServers?search=%s", fields)

	// First get the search form
	resp, err := a.GetRows(searchPath, 0, 19)
	if err != nil {
		return nil, fmt.Errorf("failed to get search form: %w", err)
	}

	// TODO: Implement form-based search submission for UPnP
	// For now, return the search menu which may contain search input
	_ = query // Will be used in form submission

	return resp, nil
}

// GetPlayQueue returns the current play queue items.
func (a *AirableClient) GetPlayQueue() (*RowsResponse, error) {
	return a.GetRows("playlists:pq/getitems", 0, 32766)
}

// PlayUPnPContainer plays all tracks from a container (album/folder).
// This follows the pattern: clear playlist → add all items → play from index 0
func (a *AirableClient) PlayUPnPContainer(containerPath string) error {
	// Get the container contents
	resp, err := a.BrowseContainer(containerPath)
	if err != nil {
		return fmt.Errorf("failed to browse container: %w", err)
	}

	// Filter to only audio tracks
	var tracks []ContentItem
	for _, item := range resp.Rows {
		if item.Type == "audio" {
			tracks = append(tracks, item)
		}
	}

	if len(tracks) == 0 {
		return fmt.Errorf("no audio tracks found in container")
	}

	return a.PlayUPnPTracks(tracks)
}

// PlayUPnPTracks plays a list of UPnP tracks using playlist-based playback.
// Pattern: clear playlist → add items via pl/addexternalitems → play from player:player/control
func (a *AirableClient) PlayUPnPTracks(tracks []ContentItem) error {
	if len(tracks) == 0 {
		return fmt.Errorf("no tracks provided")
	}

	// Step 1: Clear the playlist
	if err := a.ClearPlaylist(); err != nil {
		return fmt.Errorf("failed to clear playlist: %w", err)
	}

	// Step 2: Add tracks to playlist
	items := make([]map[string]interface{}, len(tracks))
	for i, track := range tracks {
		// Build nsdkRoles as JSON string (this is how the API expects it)
		nsdkRoles := buildUPnPTrackRoles(&track)
		nsdkRolesJSON, err := json.Marshal(nsdkRoles)
		if err != nil {
			return fmt.Errorf("failed to marshal track roles: %w", err)
		}
		items[i] = map[string]interface{}{
			"nsdkRoles": string(nsdkRolesJSON),
		}
	}

	addPayload := map[string]interface{}{
		"path": "playlists:pl/addexternalitems",
		"role": "activate",
		"value": map[string]interface{}{
			"items": items,
			"plid":  "0",
			"mode":  "append",
		},
	}

	if _, err := a.SetData(addPayload); err != nil {
		return fmt.Errorf("failed to add tracks to playlist: %w", err)
	}

	// Step 3: Play from the playlist
	firstTrack := tracks[0]
	trackRoles := buildUPnPTrackRoles(&firstTrack)

	playPayload := map[string]interface{}{
		"path": "player:player/control",
		"role": "activate",
		"value": map[string]interface{}{
			"trackRoles": trackRoles,
			"type":       "itemInContainer",
			"index":      0,
			"control":    "play",
			"mediaRoles": map[string]interface{}{
				"type":          "container",
				"containerType": "none",
				"title":         "PlayQueue tracks",
				"mediaData": map[string]interface{}{
					"metaData": map[string]interface{}{
						"playLogicPath": "playlists:playlogic",
					},
				},
				"path": "playlists:pq/getitems",
			},
		},
	}

	if _, err := a.SetData(playPayload); err != nil {
		return fmt.Errorf("failed to play tracks: %w", err)
	}

	return nil
}

// buildUPnPTrackRoles constructs the track roles for UPnP playback.
func buildUPnPTrackRoles(track *ContentItem) map[string]interface{} {
	roles := map[string]interface{}{
		"path":  track.Path,
		"title": track.Title,
		"type":  track.Type,
	}

	if track.Icon != "" {
		roles["icon"] = track.Icon
	}

	// Add media data
	if track.MediaData != nil {
		mediaData := map[string]interface{}{}

		// Metadata
		metaData := map[string]interface{}{
			"serviceID": "UPnP",
		}
		if track.MediaData.MetaData.Artist != "" {
			metaData["artist"] = track.MediaData.MetaData.Artist
		}
		if track.MediaData.MetaData.Album != "" {
			metaData["album"] = track.MediaData.MetaData.Album
		}
		if track.MediaData.MetaData.Genre != "" {
			metaData["genre"] = track.MediaData.MetaData.Genre
		}
		if track.MediaData.MetaData.Composer != "" {
			metaData["composer"] = track.MediaData.MetaData.Composer
		}
		mediaData["metaData"] = metaData

		// Resources
		if len(track.MediaData.Resources) > 0 {
			resources := make([]map[string]interface{}, len(track.MediaData.Resources))
			for i, res := range track.MediaData.Resources {
				resources[i] = map[string]interface{}{
					"uri":      res.URI,
					"mimeType": res.MimeType,
				}
				if res.BitRate > 0 {
					resources[i]["bitRate"] = res.BitRate
				}
				if res.Duration > 0 {
					resources[i]["duration"] = res.Duration
				}
				if res.SampleFrequency > 0 {
					resources[i]["sampleFrequency"] = res.SampleFrequency
				}
			}
			mediaData["resources"] = resources
		}

		roles["mediaData"] = mediaData
	}

	// Add context
	if track.Context != nil {
		roles["context"] = map[string]interface{}{
			"type":          track.Context.Type,
			"title":         track.Context.Title,
			"containerType": track.Context.ContainerType,
			"path":          track.Context.Path,
		}
	}

	return roles
}

// PlayUPnPTrack plays a single UPnP track.
func (a *AirableClient) PlayUPnPTrack(track *ContentItem) error {
	return a.PlayUPnPTracks([]ContentItem{*track})
}

// PlayUPnPByPath plays content from a UPnP path.
// If it's a container, plays all tracks. If it's a track, plays that track.
func (a *AirableClient) PlayUPnPByPath(path string) error {
	resp, err := a.GetRows(path, 0, 100)
	if err != nil {
		return fmt.Errorf("failed to get content: %w", err)
	}

	// If we got tracks, play them
	var tracks []ContentItem
	for _, item := range resp.Rows {
		if item.Type == "audio" {
			tracks = append(tracks, item)
		}
	}

	if len(tracks) > 0 {
		return a.PlayUPnPTracks(tracks)
	}

	// If the path itself is a track (check roles)
	if resp.Roles != nil && resp.Roles.Type == "audio" {
		return a.PlayUPnPTrack(resp.Roles)
	}

	return fmt.Errorf("no playable content found at path: %s", path)
}

// GetMediaServerByName finds a UPnP media server by its display name.
// Skips "Search" entries (type="query").
func (a *AirableClient) GetMediaServerByName(name string) (*ContentItem, error) {
	resp, err := a.GetMediaServers()
	if err != nil {
		return nil, fmt.Errorf("failed to get media servers: %w", err)
	}

	for _, item := range resp.Rows {
		if item.Type == "query" {
			continue // Skip search entries
		}
		if item.Title == name {
			return &item, nil
		}
	}
	return nil, fmt.Errorf("media server not found: %s", name)
}

// BrowseUPnPByDisplayPath browses using human-readable title-based paths.
// displayPath: "Music/Albums/Abbey Road" (segments separated by /)
// serverPath: API path of the server to start from (e.g., from config).
//
//	If empty and displayPath is empty, returns the server list.
//	If empty and displayPath is non-empty, returns error.
func (a *AirableClient) BrowseUPnPByDisplayPath(displayPath string, serverPath string) (*RowsResponse, error) {
	// If no display path, return contents of serverPath (or server list)
	if displayPath == "" {
		if serverPath == "" {
			return a.GetMediaServers()
		}
		return a.BrowseContainer(serverPath)
	}

	// Need a server path to browse display paths
	if serverPath == "" {
		return nil, fmt.Errorf("no default server configured; set with: kefw2 config upnp server default <name>")
	}

	// Parse display path into segments (handle escaped slashes)
	segments := splitUPnPDisplayPath(displayPath)

	// Start from server root
	currentPath := serverPath

	// Navigate through each segment
	for _, segment := range segments {
		resp, err := a.BrowseContainer(currentPath)
		if err != nil {
			return nil, fmt.Errorf("failed to browse %s: %w", currentPath, err)
		}

		// Find matching item by title
		found := false
		for _, item := range resp.Rows {
			if item.Title == segment {
				currentPath = item.Path
				found = true
				break
			}
		}

		if !found {
			return nil, fmt.Errorf("path segment not found: %s", segment)
		}
	}

	// Return contents of final path
	return a.BrowseContainer(currentPath)
}

// splitUPnPDisplayPath splits a display path into segments, handling escaped slashes.
func splitUPnPDisplayPath(path string) []string {
	if path == "" {
		return nil
	}

	// Replace escaped slashes with placeholder, split, then restore
	const placeholder = "\x00SLASH\x00"
	escaped := strings.ReplaceAll(path, "%2F", placeholder)
	parts := strings.Split(escaped, "/")

	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		// Restore escaped slashes
		segment := strings.ReplaceAll(part, placeholder, "/")
		segments = append(segments, segment)
	}
	return segments
}
