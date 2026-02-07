package kefw2

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// EventType identifies the type of event.
type EventType string

const (
	EventTypeVolume       EventType = "volume"
	EventTypeSource       EventType = "source"
	EventTypePower        EventType = "power"
	EventTypeMute         EventType = "mute"
	EventTypePlayTime     EventType = "play_time"
	EventTypePlayerData   EventType = "player_data"
	EventTypePlayMode     EventType = "play_mode"
	EventTypeEQProfile    EventType = "eq_profile"
	EventTypePlaylist     EventType = "playlist"
	EventTypeBluetooth    EventType = "bluetooth"
	EventTypeNetwork      EventType = "network"
	EventTypeFirmware     EventType = "firmware"
	EventTypeNotification EventType = "notification"
	EventTypeUnknown      EventType = "unknown"
)

// Event paths for subscriptions and parsing.
const (
	pathPlayerVolume   = "player:volume"
	pathPhysicalSource = "settings:/kef/play/physicalSource"
	pathSpeakerStatus  = "settings:/kef/host/speakerStatus"
	pathMute           = "settings:/mediaPlayer/mute"
	pathPlayTime       = "player:player/data/playTime"
	pathPlayerData     = "player:player/data"
	pathPlayMode       = "settings:/mediaPlayer/playMode"
	pathEQProfile      = "kef:eqProfile/v2"
	pathPlaylist       = "playlists:pq/getitems"
	pathBluetooth      = "bluetooth:state"
	pathNetwork        = "network:info"
	pathFirmware       = "firmwareupdate:updateStatus"
	pathNotification   = "notifications:/display/queue"
)

// Event is the interface implemented by all event types.
type Event interface {
	// Type returns the event type
	Type() EventType
	// Path returns the original event path from the speaker
	Path() string
	// Timestamp returns when the event was received
	Timestamp() time.Time
}

// baseEvent contains common fields for all events.
type baseEvent struct {
	eventType EventType
	path      string
	timestamp time.Time
}

func (e baseEvent) Type() EventType      { return e.eventType }
func (e baseEvent) Path() string         { return e.path }
func (e baseEvent) Timestamp() time.Time { return e.timestamp }

// VolumeEvent is emitted when volume changes.
type VolumeEvent struct {
	baseEvent
	Volume int
}

// SourceEvent is emitted when the audio source changes.
type SourceEvent struct {
	baseEvent
	Source Source
}

// PowerEvent is emitted when power state changes.
type PowerEvent struct {
	baseEvent
	Status SpeakerStatus
}

// MuteEvent is emitted when mute state changes.
type MuteEvent struct {
	baseEvent
	Muted bool
}

// PlayTimeEvent is emitted when playback position updates.
type PlayTimeEvent struct {
	baseEvent
	PositionMS int64 // Position in milliseconds, -1 if stopped
}

// PlayerDataEvent is emitted when track/player state changes.
// Track info is automatically fetched from the speaker.
type PlayerDataEvent struct {
	baseEvent
	State    string // PlayerStatePlaying, PlayerStatePaused, PlayerStateStopped, etc.
	Title    string // Track title
	Artist   string // Artist name
	Album    string // Album name
	Duration int    // Track duration in milliseconds
	Icon     string // URL to album art / track icon
}

// PlayModeEvent is emitted when play mode changes (shuffle, repeat, etc.)
type PlayModeEvent struct {
	baseEvent
	Mode string
}

// EQProfileEvent is emitted when EQ profile changes.
type EQProfileEvent struct {
	baseEvent
	Profile EQProfileV2
}

// PlaylistChange represents a single change in the playlist.
type PlaylistChange struct {
	Type  string // "add", "remove", "update"
	Index int
}

// PlaylistEvent is emitted when the playlist changes.
type PlaylistEvent struct {
	baseEvent
	Changes []PlaylistChange
	Version int
}

// BluetoothState represents the bluetooth connection state.
type BluetoothState struct {
	State     string
	Connected bool
	Pairing   bool
}

// BluetoothEvent is emitted when bluetooth state changes.
type BluetoothEvent struct {
	baseEvent
	Bluetooth BluetoothState
}

// NetworkEvent is emitted when network info changes.
type NetworkEvent struct {
	baseEvent
}

// FirmwareEvent is emitted when firmware update status changes.
type FirmwareEvent struct {
	baseEvent
}

// NotificationEvent is emitted when display notifications change.
type NotificationEvent struct {
	baseEvent
}

// UnknownEvent is emitted for unrecognized event types.
type UnknownEvent struct {
	baseEvent
	RawPath string
}

// parseRawEvent converts a raw event from the speaker into a typed Event.
// This is a method on EventClient so it can fetch additional data when needed.
//
//nolint:gocyclo // High complexity is inherent to parsing many event types
func (ec *EventClient) parseRawEvent(ctx context.Context, raw rawEvent) Event {
	now := time.Now()
	path := raw.Path

	base := baseEvent{
		path:      path,
		timestamp: now,
	}

	switch {
	case path == pathPlayerVolume:
		base.eventType = EventTypeVolume
		value := parseTypedValue(raw.ItemValue)
		volume := 0
		if value != nil {
			volume = value.I32
		}
		return &VolumeEvent{baseEvent: base, Volume: volume}

	case path == pathPhysicalSource:
		base.eventType = EventTypeSource
		value := parseTypedValue(raw.ItemValue)
		source := SourceStandby
		if value != nil && value.KefPhysicalSource != "" {
			source = Source(value.KefPhysicalSource)
		}
		return &SourceEvent{baseEvent: base, Source: source}

	case path == pathSpeakerStatus:
		base.eventType = EventTypePower
		value := parseTypedValue(raw.ItemValue)
		status := SpeakerStatusStandby
		if value != nil && value.KefSpeakerStatus != "" {
			status = SpeakerStatus(value.KefSpeakerStatus)
		}
		return &PowerEvent{baseEvent: base, Status: status}

	case path == pathMute:
		base.eventType = EventTypeMute
		value := parseTypedValue(raw.ItemValue)
		muted := false
		if value != nil {
			muted = value.Bool
		}
		return &MuteEvent{baseEvent: base, Muted: muted}

	case path == pathPlayTime:
		base.eventType = EventTypePlayTime
		value := parseTypedValue(raw.ItemValue)
		positionMS := int64(-1)
		if value != nil {
			positionMS = value.I64
		}
		return &PlayTimeEvent{baseEvent: base, PositionMS: positionMS}

	case path == pathPlayerData:
		base.eventType = EventTypePlayerData
		event := &PlayerDataEvent{baseEvent: base}

		// Try to parse player data from the event's ItemValue first
		// (available when subscribed with "itemWithValue")
		var gotData bool
		if len(raw.ItemValue) > 0 {
			// ItemValue contains an array of PlayerData
			var playersData []PlayerData
			if err := json.Unmarshal(raw.ItemValue, &playersData); err == nil && len(playersData) > 0 {
				pd := playersData[0]
				event.State = pd.State
				event.Title = pd.TrackRoles.Title
				event.Artist = pd.TrackRoles.MediaData.MetaData.Artist
				event.Album = pd.TrackRoles.MediaData.MetaData.Album
				event.Duration = pd.Status.Duration
				// Fallback: check MediaData.Resources if Status.Duration is 0
				if event.Duration == 0 && len(pd.TrackRoles.MediaData.Resources) > 0 {
					event.Duration = pd.TrackRoles.MediaData.Resources[0].Duration
				}
				// Fallback: check ActiveResource if still 0
				if event.Duration == 0 {
					event.Duration = pd.TrackRoles.MediaData.ActiveResource.Duration
				}
				event.Icon = pd.TrackRoles.Icon
				gotData = true
			}
		}

		// Fall back to fetching from speaker if no inline data
		if !gotData {
			if pd, err := ec.speaker.PlayerData(ctx); err == nil {
				event.State = pd.State
				event.Title = pd.TrackRoles.Title
				event.Artist = pd.TrackRoles.MediaData.MetaData.Artist
				event.Album = pd.TrackRoles.MediaData.MetaData.Album
				event.Duration = pd.Status.Duration
				// Fallback: check MediaData.Resources if Status.Duration is 0
				if event.Duration == 0 && len(pd.TrackRoles.MediaData.Resources) > 0 {
					event.Duration = pd.TrackRoles.MediaData.Resources[0].Duration
				}
				// Fallback: check ActiveResource if still 0
				if event.Duration == 0 {
					event.Duration = pd.TrackRoles.MediaData.ActiveResource.Duration
				}
				event.Icon = pd.TrackRoles.Icon
			}
		}

		// Deduplicate: skip if state and track unchanged
		// (speaker may send multiple events during track changes)
		ec.mu.Lock()
		isDuplicate := event.State == ec.lastPlayerState &&
			event.Title == ec.lastTrackTitle &&
			event.Artist == ec.lastTrackArtist
		if !isDuplicate {
			ec.lastPlayerState = event.State
			ec.lastTrackTitle = event.Title
			ec.lastTrackArtist = event.Artist
		}
		ec.mu.Unlock()

		if isDuplicate {
			return nil // Skip duplicate event
		}
		return event

	case path == pathPlayMode:
		base.eventType = EventTypePlayMode
		value := parseTypedValue(raw.ItemValue)
		mode := ""
		if value != nil {
			mode = value.String
		}
		return &PlayModeEvent{baseEvent: base, Mode: mode}

	case path == pathEQProfile:
		base.eventType = EventTypeEQProfile
		value := parseTypedValue(raw.ItemValue)
		var profile EQProfileV2
		if value != nil && value.KefEqProfileV2 != nil {
			profile = *value.KefEqProfileV2
		}
		return &EQProfileEvent{baseEvent: base, Profile: profile}

	case strings.HasPrefix(path, "playlists:"):
		base.eventType = EventTypePlaylist
		changes := make([]PlaylistChange, 0, len(raw.RowsEvents))
		for _, re := range raw.RowsEvents {
			changes = append(changes, PlaylistChange(re))
		}
		return &PlaylistEvent{
			baseEvent: base,
			Changes:   changes,
			Version:   raw.RowsVersion,
		}

	case path == pathBluetooth:
		base.eventType = EventTypeBluetooth
		value := parseTypedValue(raw.ItemValue)
		bt := BluetoothState{}
		if value != nil && value.BluetoothState != nil {
			bt.State = value.BluetoothState.State
			bt.Connected = value.BluetoothState.Connected
			bt.Pairing = value.BluetoothState.Pairing
		} else if value != nil && value.String != "" {
			bt.State = value.String
		}
		return &BluetoothEvent{baseEvent: base, Bluetooth: bt}

	case path == pathNetwork:
		base.eventType = EventTypeNetwork
		return &NetworkEvent{baseEvent: base}

	case path == pathFirmware:
		base.eventType = EventTypeFirmware
		return &FirmwareEvent{baseEvent: base}

	case strings.HasPrefix(path, "notifications:"):
		base.eventType = EventTypeNotification
		return &NotificationEvent{baseEvent: base}

	default:
		base.eventType = EventTypeUnknown
		return &UnknownEvent{baseEvent: base, RawPath: path}
	}
}
