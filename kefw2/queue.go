package kefw2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// Play mode constants for queue playback.
const (
	PlayModeNormal           = "normal"
	PlayModeRepeatOne        = "repeatOne"
	PlayModeRepeatAll        = "repeatAll"
	PlayModeShuffle          = "shuffle"
	PlayModeShuffleRepeatOne = "shuffleRepeatOne"
	PlayModeShuffleRepeatAll = "shuffleRepeatAll"
)

// AddToQueue adds tracks to the end of the queue without clearing it.
// If startIfEmpty is true and the queue is currently empty, playback will start.
// Cancellation after the append was sent does not mean the append was undone.
func (a *AirableClient) AddToQueue(tracks []ContentItem, startIfEmpty bool) error {
	if len(tracks) == 0 {
		return fmt.Errorf("no tracks provided")
	}
	ctx := a.context()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("preparing tracks for queue: %w", err)
	}

	// Check if queue is empty before adding
	queueWasEmpty := false
	if startIfEmpty {
		queue, err := a.GetPlayQueue()
		if err != nil {
			return fmt.Errorf("checking queue before adding tracks: %w", err)
		}
		queueWasEmpty = len(queue.Rows) == 0
	}

	// Build the items array with nsdkRoles JSON strings
	items := make([]map[string]interface{}, len(tracks))
	for i, track := range tracks {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("preparing tracks for queue: %w", err)
		}
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
		fieldPath: "playlists:pl/addexternalitems",
		fieldRole: roleActivate,
		fieldValue: map[string]interface{}{
			fieldItems:      items,
			fieldPlaylistID: "0",
			"mode":          "append",
		},
	}

	resp, err := a.SetData(addPayload)
	if err != nil {
		return fmt.Errorf("failed to add tracks to queue: %w", err)
	}
	_ = resp

	// If queue was empty and startIfEmpty is true, wait for tracks to appear then start playback
	if queueWasEmpty && startIfEmpty {
		// Poll until queue has items (speaker needs time to process the add)
		for attempt := 0; attempt < 20; attempt++ {
			timer := time.NewTimer(250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("waiting for added tracks: %w", ctx.Err())
			case <-timer.C:
			}
			q, err := a.GetPlayQueue()
			if err != nil {
				return fmt.Errorf("waiting for added tracks: %w", err)
			}
			if len(q.Rows) > 0 {
				return a.PlayQueueIndex(0, &tracks[0])
			}
		}
		return fmt.Errorf("added tracks did not appear in the queue before playback timeout")
	}

	return nil
}

// RemoveFromQueue removes tracks at the specified indices (0-based).
func (a *AirableClient) RemoveFromQueue(indices []int) error {
	if len(indices) == 0 {
		return fmt.Errorf("no indices provided")
	}

	payload := map[string]interface{}{
		fieldPath: "playlists:pl/removeitems",
		fieldRole: roleActivate,
		fieldValue: map[string]interface{}{
			fieldPlaylistID: 0,
			fieldItems:      indices,
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
		fieldPath: "playlists:pl/moveitem",
		fieldRole: roleActivate,
		fieldValue: map[string]interface{}{
			fieldPlaylistID: 0,
			fieldItems:      []int{fromIndex},
			"to":            toIndex,
		},
	}

	if _, err := a.SetData(payload); err != nil {
		return fmt.Errorf("failed to move queue item: %w", err)
	}

	return nil
}

// PlayQueueIndex starts playback from a specific position in the queue.
// index is 0-based. track is optional - if nil, will be fetched from queue.
func (a *AirableClient) PlayQueueIndex(index int, track *ContentItem) error {
	// If track not provided, fetch it from the queue
	if track == nil {
		queue, err := a.GetPlayQueue()
		if err != nil {
			return fmt.Errorf("failed to get queue: %w", err)
		}
		if index < 0 || index >= len(queue.Rows) {
			return fmt.Errorf("queue index %d out of range (queue has %d items)", index, len(queue.Rows))
		}
		track = &queue.Rows[index]
	}

	trackRoles := buildUPnPTrackRoles(track)

	// Use player:player/control with itemInContainer to play from queue context
	// This preserves the queue so the speaker advances to next track
	playPayload := map[string]interface{}{
		fieldPath: pathPlayerControl,
		fieldRole: roleActivate,
		fieldValue: map[string]interface{}{
			"trackRoles": trackRoles,
			fieldType:    "itemInContainer",
			"index":      index,
			fieldControl: controlPlay,
			fieldMediaRoles: map[string]interface{}{
				fieldType:          ContentTypeContainer,
				fieldContainerType: containerTypeNone,
				fieldTitle:         "PlayQueue tracks",
				"mediaData": map[string]interface{}{
					fieldMetaData: map[string]interface{}{
						"playLogicPath": "playlists:playlogic",
					},
				},
				fieldPath: "playlists:pq/getitems",
			},
		},
	}

	if _, err := a.SetData(playPayload); err != nil {
		return fmt.Errorf("failed to play from queue index: %w", err)
	}

	return nil
}

// GetPlayMode returns the current shuffle/repeat combination.
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
		return "", fmt.Errorf("speaker returned no play mode")
	}
	mode := response[0].PlayerPlayMode
	switch mode {
	case PlayModeNormal, PlayModeRepeatOne, PlayModeRepeatAll,
		PlayModeShuffle, PlayModeShuffleRepeatOne, PlayModeShuffleRepeatAll:
		return mode, nil
	default:
		return "", fmt.Errorf("speaker returned unsupported play mode %q", mode)
	}
}

// SetPlayMode sets the shuffle/repeat combination.
func (a *AirableClient) SetPlayMode(mode string) error {
	// Validate mode
	validModes := map[string]bool{
		PlayModeNormal:           true,
		PlayModeRepeatOne:        true,
		PlayModeRepeatAll:        true,
		PlayModeShuffle:          true,
		PlayModeShuffleRepeatOne: true,
		PlayModeShuffleRepeatAll: true,
	}

	if !validModes[mode] {
		return fmt.Errorf("invalid play mode: %s (valid: normal, repeatOne, repeatAll, shuffle, shuffleRepeatOne, shuffleRepeatAll)", mode)
	}

	payload := map[string]interface{}{
		fieldPath: "settings:/mediaPlayer/playMode",
		fieldRole: roleValue,
		fieldValue: map[string]interface{}{
			fieldType:        "playerPlayMode",
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
	hasRepeatOne := currentMode == PlayModeRepeatOne || currentMode == PlayModeShuffleRepeatOne

	var newMode string
	if enabled {
		switch {
		case hasRepeatAll:
			newMode = PlayModeShuffleRepeatAll
		case hasRepeatOne:
			newMode = PlayModeShuffleRepeatOne
		default:
			newMode = PlayModeShuffle
		}
	} else {
		switch {
		case hasRepeatAll:
			newMode = PlayModeRepeatAll
		case hasRepeatOne:
			newMode = PlayModeRepeatOne
		default:
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
	case PlayModeRepeatOne, PlayModeShuffleRepeatOne:
		return "one", nil
	case PlayModeRepeatAll, PlayModeShuffleRepeatAll:
		return "all", nil
	default:
		return "off", nil
	}
}

// GetCurrentQueueIndex returns the 0-based index of the currently playing track
// in the queue, or -1 if not playing from queue or unable to determine.
// It matches by comparing the now-playing path against queue item paths,
// since TrackRoles.Path contains an internal item ID, not the display index.
func (a *AirableClient) GetCurrentQueueIndex() (int, error) {
	// Get player data
	ctx := a.context()
	playerData, err := a.Speaker.PlayerData(ctx)
	if err != nil {
		return -1, airableSpeakerError(ctx, err)
	}

	// Check if playing from queue (path starts with "playlists:item/")
	nowPlayingPath := playerData.TrackRoles.Path
	if !strings.HasPrefix(nowPlayingPath, "playlists:item/") {
		return -1, nil // Not playing from queue
	}

	// Get queue and match by path
	queue, err := a.GetPlayQueue()
	if err != nil {
		return -1, fmt.Errorf("failed to get queue for index lookup: %w", err)
	}

	for i, item := range queue.Rows {
		if item.Path == nowPlayingPath {
			return i, nil
		}
	}

	// Fallback: match by title
	nowPlayingTitle := playerData.TrackRoles.Title
	if nowPlayingTitle != "" {
		for i, item := range queue.Rows {
			if item.Title == nowPlayingTitle {
				return i, nil
			}
		}
	}

	return -1, nil
}

// PlayAction describes what action PlayOrResumeFromQueue took.
type PlayAction string

const (
	// PlayActionResumed means playback was resumed from a paused state.
	PlayActionResumed PlayAction = "resumed"
	// PlayActionStartedFromQueue means playback was started from the queue (transport was stopped).
	PlayActionStartedFromQueue PlayAction = "startedFromQueue"
	// PlayActionNothingToPlay means the transport was stopped and the queue was empty.
	PlayActionNothingToPlay PlayAction = "nothingToPlay"
	// PlayActionAlreadyPlaying means the speaker was already playing.
	PlayActionAlreadyPlaying PlayAction = "alreadyPlaying"
)

// PlayResult contains the outcome of a PlayOrResumeFromQueue call.
type PlayResult struct {
	Action          PlayAction   // What action was taken
	Track           *ContentItem // The track that was started (only when Action == PlayActionStartedFromQueue)
	Index           int          // 0-based queue index of the track
	Shuffled        bool         // True if a random track was picked due to shuffle mode
	WokeFromStandby bool         // True if the speaker was woken from standby before playing
}

func (a *AirableClient) switchToWiFi(ctx context.Context, retrySourceReads bool) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := a.Speaker.SetSource(ctx, SourceWiFi); err != nil {
		return fmt.Errorf("failed to switch to WiFi source: %w", airableSpeakerError(ctx, err))
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var lastSourceErr error
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for WiFi source: %w", errors.Join(ctx.Err(), lastSourceErr))
		case <-ticker.C:
			source, err := a.Speaker.Source(ctx)
			if err != nil {
				err = airableSpeakerError(ctx, err)
				if ctx.Err() != nil {
					return fmt.Errorf("checking WiFi source: %w", errors.Join(err, lastSourceErr))
				}
				if retrySourceReads {
					lastSourceErr = err
					continue
				}
				return fmt.Errorf("checking WiFi source: %w", err)
			}
			lastSourceErr = nil
			if source == SourceWiFi {
				// The player subsystem initializes after the source has changed.
				ready := time.NewTimer(time.Second)
				defer ready.Stop()
				select {
				case <-ctx.Done():
					return fmt.Errorf("waiting for player initialization: %w", ctx.Err())
				case <-ready.C:
					return nil
				}
			}
		}
	}
}

// PlayOrResumeFromQueue intelligently resumes or starts playback.
//   - If the speaker is in standby, it switches to WiFi source and waits for
//     the speaker to wake up before proceeding.
//   - If already playing, it returns PlayActionAlreadyPlaying (no-op).
//   - If paused, it toggles play/pause to resume (PlayActionResumed).
//   - If stopped and the queue has tracks, it starts playback from the queue.
//     When shuffle is enabled, a random track is picked; otherwise it plays from
//     the top. Returns PlayActionStartedFromQueue with track details.
//   - If stopped and the queue is empty, returns PlayActionNothingToPlay.
//
// The operation keeps ctx's values and deadline and is canceled when either ctx
// or the context set by WithContext ends; the earlier deadline applies. Nil,
// Background, and TODO use the client context. Cancellation errors retain
// their causes.
func (a *AirableClient) PlayOrResumeFromQueue(ctx context.Context) (PlayResult, error) {
	clientCtx := a.context()
	if err := clientCtx.Err(); err != nil {
		return PlayResult{}, playContextError(clientCtx, err)
	}
	if ctx == nil || ctx == context.Background() || ctx == context.TODO() {
		ctx = clientCtx
	}
	if err := ctx.Err(); err != nil {
		return PlayResult{}, playContextError(ctx, err)
	}

	if clientCtx.Done() != nil && ctx.Done() != clientCtx.Done() {
		combined, cancel := context.WithCancelCause(ctx)
		stop := context.AfterFunc(clientCtx, func() { cancel(context.Cause(clientCtx)) })
		defer func() {
			stop()
			cancel(nil)
		}()
		if clientCtx.Err() != nil {
			cancel(context.Cause(clientCtx))
		}
		ctx = combined
		if deadline, ok := clientCtx.Deadline(); ok {
			operationDeadline, hasDeadline := ctx.Deadline()
			if !hasDeadline || deadline.Before(operationDeadline) {
				ctx = playDeadlineContext{Context: ctx, deadline: deadline}
			}
		}
	}

	client := *a
	client.bindContext(ctx, false)
	result, err := client.playOrResumeFromQueue(ctx)
	return result, playContextError(clientCtx, playContextError(ctx, err))
}

// Expose the earlier deadline without racing the client's cancellation cause
// against a second deadline timer.
type playDeadlineContext struct {
	context.Context
	deadline time.Time
}

func (c playDeadlineContext) Deadline() (time.Time, bool) {
	return c.deadline, true
}

func playContextError(ctx context.Context, err error) error {
	if err == nil || ctx.Err() == nil {
		return err
	}
	if ctxErr := ctx.Err(); !errors.Is(err, ctxErr) {
		err = errors.Join(err, ctxErr)
	}
	if cause := context.Cause(ctx); cause != nil && !errors.Is(err, cause) {
		err = errors.Join(err, cause)
	}
	return err
}

func (a *AirableClient) playOrResumeFromQueue(ctx context.Context) (PlayResult, error) {
	wokeFromStandby := false

	// Check if speaker is in standby and wake it by switching to WiFi
	source, err := a.Speaker.Source(ctx)
	if err != nil {
		return PlayResult{}, fmt.Errorf("failed to get speaker source: %w", airableSpeakerError(ctx, err))
	}
	if source == SourceStandby {
		if err := a.switchToWiFi(ctx, true); err != nil {
			return PlayResult{}, err
		}
		wokeFromStandby = true
	}

	pd, err := a.Speaker.PlayerData(ctx)
	if err != nil {
		return PlayResult{}, fmt.Errorf("failed to get player data: %w", airableSpeakerError(ctx, err))
	}

	switch pd.State {
	case PlayerStatePlaying:
		return PlayResult{Action: PlayActionAlreadyPlaying, WokeFromStandby: wokeFromStandby}, nil

	case PlayerStatePaused:
		if err := a.Speaker.PlayPause(ctx); err != nil {
			return PlayResult{}, fmt.Errorf("failed to resume playback: %w", airableSpeakerError(ctx, err))
		}
		return PlayResult{Action: PlayActionResumed, WokeFromStandby: wokeFromStandby}, nil

	default: // stopped or any other state
		queue, err := a.GetPlayQueue()
		if err != nil {
			return PlayResult{}, fmt.Errorf("failed to get play queue: %w", err)
		}
		if len(queue.Rows) == 0 {
			return PlayResult{Action: PlayActionNothingToPlay, WokeFromStandby: wokeFromStandby}, nil
		}

		shuffled, err := a.IsShuffleEnabled()
		if err != nil {
			return PlayResult{}, fmt.Errorf("failed to check shuffle mode: %w", err)
		}

		index := 0
		if shuffled {
			index = rand.Intn(len(queue.Rows)) //nolint:gosec // Queue shuffling is not security-sensitive.
		}

		track := queue.Rows[index]
		if err := a.PlayQueueIndex(index, &track); err != nil {
			return PlayResult{}, fmt.Errorf("failed to play from queue: %w", err)
		}

		return PlayResult{
			Action:          PlayActionStartedFromQueue,
			Track:           &track,
			Index:           index,
			Shuffled:        shuffled,
			WokeFromStandby: wokeFromStandby,
		}, nil
	}
}
