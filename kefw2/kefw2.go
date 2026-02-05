// Package kefw2 provides a Go client library for controlling KEF W2 platform speakers.
//
// The W2 platform is used in KEF's wireless speaker lineup including the LSX II,
// LS50 Wireless II, and LS60 Wireless. This library communicates with the speakers
// over HTTP using KEF's undocumented REST API.
//
// # Basic Usage
//
// Create a speaker instance by providing its IP address:
//
//	speaker, err := kefw2.NewSpeaker("192.168.1.100")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Get current volume
//	ctx := context.Background()
//	volume, err := speaker.GetVolume(ctx)
//
//	// Set volume to 30
//	err = speaker.SetVolume(ctx, 30)
//
//	// Change source to WiFi
//	err = speaker.SetSource(ctx, kefw2.SourceWiFi)
//
// # Functional Options
//
// NewSpeaker accepts optional configuration:
//
//	speaker, err := kefw2.NewSpeaker("192.168.1.100",
//	    kefw2.WithTimeout(5*time.Second),
//	    kefw2.WithHTTPClient(customClient),
//	)
//
// # Speaker Discovery
//
// Speakers can be automatically discovered on the local network:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	speakers, err := kefw2.DiscoverSpeakers(ctx, 5*time.Second)
//
// # Event Streaming
//
// For real-time updates, use the EventClient:
//
//	client, err := speaker.NewEventClient()
//	go client.Start(ctx)
//	for event := range client.Events() {
//	    switch e := event.(type) {
//	    case *kefw2.VolumeEvent:
//	        fmt.Printf("Volume: %d\n", e.Volume)
//	    }
//	}
package kefw2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Common errors for speaker operations.
var (
	ErrEmptyIPAddress = errors.New("speaker IP address is empty")
	ErrInvalidModel   = errors.New("could not parse model information")
)

// KEFSpeaker represents a KEF W2 platform speaker.
type KEFSpeaker struct {
	IPAddress       string `mapstructure:"ip_address" json:"ip_address" yaml:"ip_address"`
	Name            string `mapstructure:"name" json:"name" yaml:"name"`
	Model           string `mapstructure:"model" json:"model" yaml:"model"`
	FirmwareVersion string `mapstructure:"firmware_version" json:"firmware_version" yaml:"firmware_version"`
	MacAddress      string `mapstructure:"mac_address" json:"mac_address" yaml:"mac_address"`
	ID              string `mapstructure:"id" json:"id" yaml:"id"`
	MaxVolume       int    `mapstructure:"max_volume" json:"max_volume" yaml:"max_volume"`

	// HTTP client configuration (not serialized)
	client  *http.Client  `json:"-" yaml:"-" mapstructure:"-"`
	timeout time.Duration `json:"-" yaml:"-" mapstructure:"-"`
}

// KEFGrouping represents speaker grouping information.
type KEFGrouping struct {
	GroupingMembers []KEFGroupingMember `json:"groupingMember"`
}

// KEFGroupingMember represents a single speaker group.
type KEFGroupingMember struct {
	Master   KEFGroupingData `json:"master"`
	Follower KEFGroupingData `json:"follower"`
}

// KEFGroupingData contains identification for a speaker in a group.
type KEFGroupingData struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Models maps internal model identifiers to human-readable names.
var Models = map[string]string{
	"lsxii":  "KEF LSX II",
	"ls502w": "KEF LS50 II Wireless",
	"ls60w":  "KEF LS60 Wireless",
	"LS60W":  "KEF LS60 Wireless",
}

// SpeakerOption is a functional option for configuring a KEFSpeaker.
type SpeakerOption func(*KEFSpeaker)

// WithTimeout sets the HTTP request timeout for the speaker.
func WithTimeout(d time.Duration) SpeakerOption {
	return func(s *KEFSpeaker) {
		s.timeout = d
	}
}

// WithHTTPClient sets a custom HTTP client for the speaker.
func WithHTTPClient(client *http.Client) SpeakerOption {
	return func(s *KEFSpeaker) {
		s.client = client
	}
}

// NewSpeaker creates a new KEFSpeaker instance and fetches its information.
// Returns a pointer to the speaker or an error if the connection fails.
func NewSpeaker(ipAddress string, opts ...SpeakerOption) (*KEFSpeaker, error) {
	if ipAddress == "" {
		return nil, ErrEmptyIPAddress
	}

	speaker := &KEFSpeaker{
		IPAddress: ipAddress,
		timeout:   DefaultTimeout,
	}

	// Apply options
	for _, opt := range opts {
		opt(speaker)
	}

	if err := speaker.UpdateInfo(context.Background()); err != nil {
		return nil, fmt.Errorf("connecting to speaker: %w", err)
	}

	return speaker, nil
}

// UpdateInfo refreshes all speaker information from the device.
func (s *KEFSpeaker) UpdateInfo(ctx context.Context) error {
	var err error

	s.MacAddress, err = s.getMACAddress(ctx)
	if err != nil {
		return fmt.Errorf("getting MAC address: %w", err)
	}

	s.Name, err = s.getName(ctx)
	if err != nil {
		return fmt.Errorf("getting name: %w", err)
	}

	if err = s.getID(ctx); err != nil {
		return fmt.Errorf("getting speaker ID: %w", err)
	}

	if err = s.getModelAndVersion(ctx); err != nil {
		return fmt.Errorf("getting model info: %w", err)
	}

	// Fetch max volume (stores in struct as side effect)
	if _, err = s.GetMaxVolume(ctx); err != nil {
		return fmt.Errorf("getting max volume: %w", err)
	}

	return nil
}

func (s *KEFSpeaker) getMACAddress(ctx context.Context) (string, error) {
	return parseTypedString(s.getData(ctx, "settings:/system/primaryMacAddress"))
}

// NetworkOperationMode returns whether the speaker is in wired or wireless mode.
func (s *KEFSpeaker) NetworkOperationMode(ctx context.Context) (CableMode, error) {
	return parseTypedCableMode(s.getData(ctx, "settings:/kef/host/cableMode"))
}

func (s *KEFSpeaker) getName(ctx context.Context) (string, error) {
	return parseTypedString(s.getData(ctx, "settings:/deviceName"))
}

func (s *KEFSpeaker) getID(ctx context.Context) error {
	params := map[string]string{
		"roles": "@all",
		"from":  "0",
		"to":    "19",
	}
	data, err := s.getRows(ctx, "grouping:members", params)
	if err != nil {
		return err
	}

	var groupData KEFGrouping
	if err := json.Unmarshal(data, &groupData); err != nil {
		return fmt.Errorf("parsing grouping data: %w", err)
	}

	for _, speakerSet := range groupData.GroupingMembers {
		if speakerSet.Master.Name == s.Name {
			s.ID = speakerSet.Master.ID
			break
		}
	}

	return nil
}

func (s *KEFSpeaker) getModelAndVersion(ctx context.Context) error {
	releaseText, err := parseTypedString(s.getData(ctx, "settings:/releasetext"))
	if err != nil {
		return err
	}

	parts := strings.SplitN(releaseText, "_", 2)
	if len(parts) < 2 {
		return fmt.Errorf("%w: %q", ErrInvalidModel, releaseText)
	}

	modelID := parts[0]
	if name, ok := Models[modelID]; ok {
		s.Model = name
	} else {
		s.Model = modelID
	}
	s.FirmwareVersion = parts[1]

	return nil
}

// PlayPause toggles playback state.
func (s *KEFSpeaker) PlayPause(ctx context.Context) error {
	return s.setActivate(ctx, "player:player/control", "control", "pause")
}

// GetVolume returns the current volume level.
func (s *KEFSpeaker) GetVolume(ctx context.Context) (int, error) {
	return parseTypedInt(s.getData(ctx, "player:volume"))
}

// SetVolume sets the volume to the specified level.
func (s *KEFSpeaker) SetVolume(ctx context.Context, volume int) error {
	return s.setTypedValue(ctx, "player:volume", volume)
}

// Mute mutes the speaker.
func (s *KEFSpeaker) Mute(ctx context.Context) error {
	return s.setTypedValue(ctx, "settings:/mediaPlayer/mute", true)
}

// Unmute unmutes the speaker.
func (s *KEFSpeaker) Unmute(ctx context.Context) error {
	return s.setTypedValue(ctx, "settings:/mediaPlayer/mute", false)
}

// IsMuted returns whether the speaker is currently muted.
func (s *KEFSpeaker) IsMuted(ctx context.Context) (bool, error) {
	return parseTypedBool(s.getData(ctx, "settings:/mediaPlayer/mute"))
}

// PowerOff puts the speaker into standby mode.
func (s *KEFSpeaker) PowerOff(ctx context.Context) error {
	return s.SetSource(ctx, SourceStandby)
}

// SetSource changes the audio source.
func (s *KEFSpeaker) SetSource(ctx context.Context, source Source) error {
	return s.setTypedValue(ctx, "settings:/kef/play/physicalSource", source)
}

// Source returns the current audio source.
func (s *KEFSpeaker) Source(ctx context.Context) (Source, error) {
	return parseTypedSource(s.getData(ctx, "settings:/kef/play/physicalSource"))
}

// CanControlPlayback returns true if the current source supports playback control.
func (s *KEFSpeaker) CanControlPlayback(ctx context.Context) (bool, error) {
	source, err := s.Source(ctx)
	if err != nil {
		return false, err
	}
	return source == SourceWiFi || source == SourceBluetooth, nil
}

// IsPoweredOn returns true if the speaker is powered on (not in standby).
func (s *KEFSpeaker) IsPoweredOn(ctx context.Context) (bool, error) {
	status, err := s.SpeakerState(ctx)
	if err != nil {
		return false, err
	}
	return status == SpeakerStatusOn, nil
}

// SpeakerState returns the current speaker status.
func (s *KEFSpeaker) SpeakerState(ctx context.Context) (SpeakerStatus, error) {
	return parseTypedSpeakerStatus(s.getData(ctx, "settings:/kef/host/speakerStatus"))
}

// GetMaxVolume returns the maximum volume setting.
func (s *KEFSpeaker) GetMaxVolume(ctx context.Context) (int, error) {
	maxVolume, err := parseTypedInt(s.getData(ctx, "settings:/kef/host/maximumVolume"))
	if err != nil {
		return 0, err
	}
	s.MaxVolume = maxVolume
	return maxVolume, nil
}

// SetMaxVolume sets the maximum volume limit.
func (s *KEFSpeaker) SetMaxVolume(ctx context.Context, maxVolume int) error {
	s.MaxVolume = maxVolume
	return s.setTypedValue(ctx, "settings:/kef/host/maximumVolume", maxVolume)
}

// IsPlaying returns true if the speaker is currently playing audio.
func (s *KEFSpeaker) IsPlaying(ctx context.Context) (bool, error) {
	pd, err := s.PlayerData(ctx)
	if err != nil {
		return false, err
	}
	return pd.State == PlayerStatePlaying, nil
}

// NextTrack skips to the next track (only works in WiFi mode).
func (s *KEFSpeaker) NextTrack(ctx context.Context) error {
	return s.setActivate(ctx, "player:player/control", "control", "next")
}

// PreviousTrack goes to the previous track (only works in WiFi mode).
func (s *KEFSpeaker) PreviousTrack(ctx context.Context) error {
	return s.setActivate(ctx, "player:player/control", "control", "previous")
}

// SongProgress returns the current playback position as "minutes:seconds".
func (s *KEFSpeaker) SongProgress(ctx context.Context) (string, error) {
	playMS, err := s.SongProgressMS(ctx)
	if err != nil {
		return "0:00", err
	}
	return fmt.Sprintf("%d:%02d", playMS/60000, (playMS/1000)%60), nil
}

// SongProgressMS returns the current playback position in milliseconds.
func (s *KEFSpeaker) SongProgressMS(ctx context.Context) (int, error) {
	return parseTypedInt(s.getData(ctx, "player:player/data/playTime"))
}

// SeekTo seeks to a specific position in the current track.
// positionMS is the position in milliseconds.
func (s *KEFSpeaker) SeekTo(ctx context.Context, positionMS int64) error {
	// Use the player control path with seekTime control
	// Format: {"control": "seekTime", "time": <milliseconds>}
	return s.setActivateMap(ctx, "player:player/control", map[string]any{
		"control": "seekTime",
		"time":    positionMS,
	})
}
