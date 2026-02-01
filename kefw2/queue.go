package kefw2

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Play mode constants for queue playback
const (
	PlayModeNormal           = "normal"
	PlayModeRepeatOne        = "repeatOne"
	PlayModeRepeatAll        = "repeatAll"
	PlayModeShuffle          = "shuffle"
	PlayModeShuffleRepeatAll = "shuffleRepeatAll"
)

// AddToQueue adds tracks to the end of the queue without clearing it.
// If startIfEmpty is true and the queue is currently empty, playback will start.
func (a *AirableClient) AddToQueue(tracks []ContentItem, startIfEmpty bool) error {
	if len(tracks) == 0 {
		return fmt.Errorf("no tracks provided")
	}

	// Check if queue is empty before adding
	queueWasEmpty := false
	if startIfEmpty {
		queue, err := a.GetPlayQueue()
		if err == nil {
			queueWasEmpty = len(queue.Rows) == 0
		}
	}

	// Build the items array with nsdkRoles JSON strings
	items := make([]map[string]interface{}, len(tracks))
	for i, track := range tracks {
		nsdkRoles := buildUPnPTrackRoles(&track)
		nsdkRolesJSON, err := json.Marshal(nsdkRoles)
		if err != nil {
			return fmt.Errorf("failed to marshal track roles: %w", err)
		}
		items[i] = map[string]interface{}{
			"nsdkRoles": string(nsdkRolesJSON),
		}
	}

	// Add tracks to queue
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
		return fmt.Errorf("failed to add tracks to queue: %w", err)
	}

	// If queue was empty and startIfEmpty is true, start playback
	if queueWasEmpty && startIfEmpty {
		return a.PlayQueueIndex(0, &tracks[0])
	}

	return nil
}

// RemoveFromQueue removes tracks at the specified indices (0-based).
func (a *AirableClient) RemoveFromQueue(indices []int) error {
	if len(indices) == 0 {
		return fmt.Errorf("no indices provided")
	}

	payload := map[string]interface{}{
		"path": "playlists:pl/removeitems",
		"role": "activate",
		"value": map[string]interface{}{
			"plid":  0,
			"items": indices,
		},
	}

	if _, err := a.SetData(payload); err != nil {
		return fmt.Errorf("failed to remove items from queue: %w", err)
	}

	return nil
}

// MoveQueueItem moves a track from one position to another in the queue.
// Both fromIndex and toIndex are 0-based.
func (a *AirableClient) MoveQueueItem(fromIndex, toIndex int) error {
	payload := map[string]interface{}{
		"path": "playlists:pl/moveitem",
		"role": "activate",
		"value": map[string]interface{}{
			"plid":  0,
			"items": []int{fromIndex},
			"to":    toIndex,
		},
	}

	if _, err := a.SetData(payload); err != nil {
		return fmt.Errorf("failed to move queue item: %w", err)
	}

	return nil
}

// PlayQueueIndex starts playback from a specific position in the queue.
// index is 0-based. track should be the ContentItem at that index.
func (a *AirableClient) PlayQueueIndex(index int, track *ContentItem) error {
	if track == nil {
		return fmt.Errorf("track is required")
	}

	trackRoles := buildUPnPTrackRoles(track)

	playPayload := map[string]interface{}{
		"path": "player:player/control",
		"role": "activate",
		"value": map[string]interface{}{
			"trackRoles": trackRoles,
			"type":       "itemInContainer",
			"index":      index,
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
		return fmt.Errorf("failed to play from queue index: %w", err)
	}

	return nil
}

// GetPlayMode returns the current play mode (normal, repeatOne, repeatAll, shuffle, shuffleRepeatAll).
func (a *AirableClient) GetPlayMode() (string, error) {
	data, err := a.GetData("settings:/mediaPlayer/playMode")
	if err != nil {
		return "", fmt.Errorf("failed to get play mode: %w", err)
	}

	// Response format: [{"type":"playerPlayMode","playerPlayMode":"normal"}]
	var response []struct {
		Type           string `json:"type"`
		PlayerPlayMode string `json:"playerPlayMode"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("failed to parse play mode response: %w", err)
	}

	if len(response) == 0 {
		return PlayModeNormal, nil
	}

	return response[0].PlayerPlayMode, nil
}

// SetPlayMode sets the play mode (normal, repeatOne, repeatAll, shuffle, shuffleRepeatAll).
func (a *AirableClient) SetPlayMode(mode string) error {
	// Validate mode
	validModes := map[string]bool{
		PlayModeNormal:           true,
		PlayModeRepeatOne:        true,
		PlayModeRepeatAll:        true,
		PlayModeShuffle:          true,
		PlayModeShuffleRepeatAll: true,
	}

	if !validModes[mode] {
		return fmt.Errorf("invalid play mode: %s (valid: normal, repeatOne, repeatAll, shuffle, shuffleRepeatAll)", mode)
	}

	payload := map[string]interface{}{
		"path": "settings:/mediaPlayer/playMode",
		"role": "value",
		"value": map[string]interface{}{
			"type":           "playerPlayMode",
			"playerPlayMode": mode,
		},
	}

	if _, err := a.SetData(payload); err != nil {
		return fmt.Errorf("failed to set play mode: %w", err)
	}

	return nil
}

// SetShuffle enables or disables shuffle mode while preserving repeat settings.
func (a *AirableClient) SetShuffle(enabled bool) error {
	currentMode, err := a.GetPlayMode()
	if err != nil {
		return err
	}

	// Determine current repeat state
	hasRepeatAll := strings.Contains(currentMode, "RepeatAll") || currentMode == PlayModeRepeatAll
	hasRepeatOne := currentMode == PlayModeRepeatOne

	var newMode string
	if enabled {
		if hasRepeatAll {
			newMode = PlayModeShuffleRepeatAll
		} else {
			newMode = PlayModeShuffle
		}
	} else {
		if hasRepeatAll {
			newMode = PlayModeRepeatAll
		} else if hasRepeatOne {
			newMode = PlayModeRepeatOne
		} else {
			newMode = PlayModeNormal
		}
	}

	return a.SetPlayMode(newMode)
}

// SetRepeat sets the repeat mode: "off", "one", or "all".
// Preserves shuffle state when changing repeat mode.
func (a *AirableClient) SetRepeat(mode string) error {
	currentMode, err := a.GetPlayMode()
	if err != nil {
		return err
	}

	// Determine if shuffle is currently enabled
	hasShuffle := strings.HasPrefix(currentMode, "shuffle")

	var newMode string
	switch mode {
	case "off":
		if hasShuffle {
			newMode = PlayModeShuffle
		} else {
			newMode = PlayModeNormal
		}
	case "one":
		// Note: shuffleRepeatOne exists but is unusual, we'll just use repeatOne
		newMode = PlayModeRepeatOne
	case "all":
		if hasShuffle {
			newMode = PlayModeShuffleRepeatAll
		} else {
			newMode = PlayModeRepeatAll
		}
	default:
		return fmt.Errorf("invalid repeat mode: %s (valid: off, one, all)", mode)
	}

	return a.SetPlayMode(newMode)
}

// IsShuffleEnabled returns true if shuffle is currently enabled.
func (a *AirableClient) IsShuffleEnabled() (bool, error) {
	mode, err := a.GetPlayMode()
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(mode, "shuffle"), nil
}

// GetRepeatMode returns the current repeat mode: "off", "one", or "all".
func (a *AirableClient) GetRepeatMode() (string, error) {
	mode, err := a.GetPlayMode()
	if err != nil {
		return "", err
	}

	switch mode {
	case PlayModeRepeatOne:
		return "one", nil
	case PlayModeRepeatAll, PlayModeShuffleRepeatAll:
		return "all", nil
	default:
		return "off", nil
	}
}

// GetCurrentQueueIndex returns the 0-based index of the currently playing track
// in the queue, or -1 if not playing from queue or unable to determine.
func (a *AirableClient) GetCurrentQueueIndex() (int, error) {
	// Get player data
	playerData, err := a.Speaker.PlayerData(context.Background())
	if err != nil {
		return -1, err
	}

	// Check if playing from queue (path starts with "playlists:item/")
	path := playerData.TrackRoles.Path
	if !strings.HasPrefix(path, "playlists:item/") {
		return -1, nil // Not playing from queue
	}

	// Parse index from path like "playlists:item/1" (1-based in API, return 0-based)
	idxStr := strings.TrimPrefix(path, "playlists:item/")
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		return -1, fmt.Errorf("failed to parse queue index from path: %s", path)
	}

	return idx - 1, nil // Convert to 0-based
}
