package kefw2

import (
	"context"
	"encoding/json"
	"fmt"
)

// Player states.
const (
	PlayerStatePlaying = "playing"
	PlayerStatePaused  = "paused"
	PlayerStateStopped = "stopped"
)

// PlayerData contains the current playback state and track information
// for WiFi streaming sources (AirPlay, Chromecast, Roon, etc.).
type PlayerData struct {
	State      string           `json:"state"`
	Status     PlayerResource   `json:"status"`
	TrackRoles PlayerTrackRoles `json:"trackRoles"`
	Controls   PlayerControls   `json:"controls"`
	MediaRoles PlayerMediaRoles `json:"mediaRoles"`
	PlayID     PlayerPlayID     `json:"playId"`
}

// PlayerResource contains duration information for a media resource.
type PlayerResource struct {
	Duration int `json:"duration"` // Duration in milliseconds
}

// PlayerTrackRoles contains track metadata including title, icon, and media data.
type PlayerTrackRoles struct {
	Path      string          `json:"path"`      // "playlists:item/{index}" when playing from queue (1-based)
	ID        string          `json:"id"`        // Queue index as string (1-based) when playing from queue
	Icon      string          `json:"icon"`      // URL to album art or track icon
	MediaData PlayerMediaData `json:"mediaData"` // Detailed media information
	Title     string          `json:"title"`     // Track title
}

// PlayerMediaData contains detailed media information including metadata and resources.
type PlayerMediaData struct {
	ActiveResource PlayerResource   `json:"activeResource"` // Currently playing resource
	MetaData       PlayerMetaData   `json:"metaData"`       // Artist and album information
	Resources      []PlayerResource `json:"resources"`      // Available resources
}

// PlayerMetaData contains artist and album information for a track.
type PlayerMetaData struct {
	Artist string `json:"artist"` // Artist name
	Album  string `json:"album"`  // Album name
}

// PlayerControls indicates which playback controls are available.
type PlayerControls struct {
	Previous bool `json:"previous"` // Previous track available
	Pause    bool `json:"pause"`    // Pause/play available
	Next     bool `json:"next_"`    // Next track available
}

// PlayerMediaRoles contains media role information and additional metadata.
type PlayerMediaRoles struct {
	AudioType  string                   `json:"audioType"`  // Type of audio content
	DoNotTrack bool                     `json:"doNotTrack"` // Privacy setting
	Type       string                   `json:"type"`       // Media type
	MediaData  PlayerMediaRolesMetaData `json:"mediaData"`  // Additional metadata
	Title      string                   `json:"title"`      // Media title
}

// PlayerMediaRolesMetaData contains streaming service metadata.
type PlayerMediaRolesMetaData struct {
	MetaData  PlayerMediaRolesMedieDataMetaData `json:"metaData"`  // Service-specific metadata
	Resources []PlayerMimeResource              `json:"resources"` // Available media resources
}

// PlayerMediaRolesMedieDataMetaData contains streaming service identification data.
type PlayerMediaRolesMedieDataMetaData struct {
	ServiceID     string `json:"serviceId"`     // Streaming service identifier
	Live          bool   `json:"live"`          // Whether this is a live stream
	PlayLogicPath string `json:"playLogicPath"` // Internal playback path
}

// PlayerMimeResource represents a media resource with its MIME type and URI.
type PlayerMimeResource struct {
	MimeType string `json:"mimeType"` // MIME type (e.g., "audio/flac")
	URI      string `json:"uri"`      // Resource URI
}

// PlayerPlayID uniquely identifies a playback session.
type PlayerPlayID struct {
	TimeStamp      int    `json:"timestamp"`      // Unix timestamp of session start
	SystemMemberID string `json:"systemMemberId"` // System member identifier
}

// PlayerData returns the current playback state and track information.
// This is only applicable when the speaker is using a WiFi source.
func (s *KEFSpeaker) PlayerData(ctx context.Context) (PlayerData, error) {
	var playersData []PlayerData
	var err error
	playersJSON, err := s.getData(ctx, "player:player/data")
	if err != nil {
		return PlayerData{}, fmt.Errorf("error getting player data: %w", err)
	}
	err = json.Unmarshal(playersJSON, &playersData)
	if err != nil {
		return PlayerData{}, fmt.Errorf("error unmarshaling player data: %w", err)
	}
	playerData := playersData[0]
	return playerData, nil
}

// String returns the duration in minutes:seconds format instead of milliseconds.
func (p PlayerResource) String() string {
	str := fmt.Sprintf("%d:%02d", p.Duration/60000, (p.Duration/1000)%60)
	return str
}
