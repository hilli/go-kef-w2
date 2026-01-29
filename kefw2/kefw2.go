package kefw2

import (
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

	if err := speaker.UpdateInfo(); err != nil {
		return nil, fmt.Errorf("connecting to speaker: %w", err)
	}

	return speaker, nil
}

// UpdateInfo refreshes all speaker information from the device.
func (s *KEFSpeaker) UpdateInfo() error {
	var err error

	s.MacAddress, err = s.getMACAddress()
	if err != nil {
		return fmt.Errorf("getting MAC address: %w", err)
	}

	s.Name, err = s.getName()
	if err != nil {
		return fmt.Errorf("getting name: %w", err)
	}

	if err = s.getID(); err != nil {
		return fmt.Errorf("getting speaker ID: %w", err)
	}

	if err = s.getModelAndVersion(); err != nil {
		return fmt.Errorf("getting model info: %w", err)
	}

	// Fetch max volume (stores in struct as side effect)
	if _, err = s.GetMaxVolume(); err != nil {
		return fmt.Errorf("getting max volume: %w", err)
	}

	return nil
}

func (s *KEFSpeaker) getMACAddress() (string, error) {
	data, err := s.getData("settings:/system/primaryMacAddress")
	if err != nil {
		return "", err
	}
	return parseJSONString(data)
}

// NetworkOperationMode returns whether the speaker is in wired or wireless mode.
func (s *KEFSpeaker) NetworkOperationMode() (CableMode, error) {
	data, err := s.getData("settings:/kef/host/cableMode")
	if err != nil {
		return "", err
	}
	val, err := parseJSONValue(data)
	if err != nil {
		return "", err
	}
	mode, ok := val.(CableMode)
	if !ok {
		return "", fmt.Errorf("unexpected type for cable mode: %T", val)
	}
	return mode, nil
}

func (s *KEFSpeaker) getName() (string, error) {
	data, err := s.getData("settings:/deviceName")
	if err != nil {
		return "", err
	}
	return parseJSONString(data)
}

func (s *KEFSpeaker) getID() error {
	params := map[string]string{
		"roles": "@all",
		"from":  "0",
		"to":    "19",
	}
	data, err := s.getRows("grouping:members", params)
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

func (s *KEFSpeaker) getModelAndVersion() error {
	data, err := s.getData("settings:/releasetext")
	if err != nil {
		return err
	}

	releaseText, err := parseJSONString(data)
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
func (s *KEFSpeaker) PlayPause() error {
	return s.setActivate("player:player/control", "control", "pause")
}

// GetVolume returns the current volume level.
func (s *KEFSpeaker) GetVolume() (int, error) {
	data, err := s.getData("player:volume")
	if err != nil {
		return 0, err
	}
	return parseJSONInt(data)
}

// SetVolume sets the volume to the specified level.
func (s *KEFSpeaker) SetVolume(volume int) error {
	return s.setTypedValue("player:volume", volume)
}

// Mute mutes the speaker.
func (s *KEFSpeaker) Mute() error {
	return s.setTypedValue("settings:/mediaPlayer/mute", true)
}

// Unmute unmutes the speaker.
func (s *KEFSpeaker) Unmute() error {
	return s.setTypedValue("settings:/mediaPlayer/mute", false)
}

// IsMuted returns whether the speaker is currently muted.
func (s *KEFSpeaker) IsMuted() (bool, error) {
	data, err := s.getData("settings:/mediaPlayer/mute")
	if err != nil {
		return false, err
	}
	val, err := parseJSONValue(data)
	if err != nil {
		return false, err
	}
	muted, ok := val.(bool)
	if !ok {
		return false, fmt.Errorf("unexpected type for mute state: %T", val)
	}
	return muted, nil
}

// PowerOff puts the speaker into standby mode.
func (s *KEFSpeaker) PowerOff() error {
	return s.SetSource(SourceStandby)
}

// SetSource changes the audio source.
func (s *KEFSpeaker) SetSource(source Source) error {
	return s.setTypedValue("settings:/kef/play/physicalSource", source)
}

// Source returns the current audio source.
func (s *KEFSpeaker) Source() (Source, error) {
	data, err := s.getData("settings:/kef/play/physicalSource")
	if err != nil {
		return SourceStandby, fmt.Errorf("getting speaker source: %w", err)
	}
	val, err := parseJSONValue(data)
	if err != nil {
		return SourceStandby, err
	}
	src, ok := val.(Source)
	if !ok {
		return SourceStandby, fmt.Errorf("unexpected type for source: %T", val)
	}
	return src, nil
}

// CanControlPlayback returns true if the current source supports playback control.
func (s *KEFSpeaker) CanControlPlayback() (bool, error) {
	source, err := s.Source()
	if err != nil {
		return false, err
	}
	return source == SourceWiFi || source == SourceBluetooth, nil
}

// IsPoweredOn returns true if the speaker is powered on (not in standby).
func (s *KEFSpeaker) IsPoweredOn() (bool, error) {
	status, err := s.SpeakerState()
	if err != nil {
		return false, err
	}
	return status == SpeakerStatusOn, nil
}

// SpeakerState returns the current speaker status.
func (s *KEFSpeaker) SpeakerState() (SpeakerStatus, error) {
	data, err := s.getData("settings:/kef/host/speakerStatus")
	if err != nil {
		return SpeakerStatusStandby, err
	}
	val, err := parseJSONValue(data)
	if err != nil {
		return SpeakerStatusStandby, err
	}
	status, ok := val.(SpeakerStatus)
	if !ok {
		return SpeakerStatusStandby, fmt.Errorf("unexpected type for speaker status: %T", val)
	}
	return status, nil
}

// GetMaxVolume returns the maximum volume setting.
func (s *KEFSpeaker) GetMaxVolume() (int, error) {
	data, err := s.getData("settings:/kef/host/maximumVolume")
	if err != nil {
		return 0, err
	}
	maxVolume, err := parseJSONInt(data)
	if err != nil {
		return 0, err
	}
	s.MaxVolume = maxVolume
	return maxVolume, nil
}

// SetMaxVolume sets the maximum volume limit.
func (s *KEFSpeaker) SetMaxVolume(maxVolume int) error {
	s.MaxVolume = maxVolume
	return s.setTypedValue("settings:/kef/host/maximumVolume", maxVolume)
}

// IsPlaying returns true if the speaker is currently playing audio.
func (s *KEFSpeaker) IsPlaying() (bool, error) {
	pd, err := s.PlayerData()
	if err != nil {
		return false, err
	}
	return pd.State == "playing", nil
}

// NextTrack skips to the next track (only works in WiFi mode).
func (s *KEFSpeaker) NextTrack() error {
	return s.setActivate("player:player/control", "control", "next")
}

// PreviousTrack goes to the previous track (only works in WiFi mode).
func (s *KEFSpeaker) PreviousTrack() error {
	return s.setActivate("player:player/control", "control", "previous")
}

// SongProgress returns the current playback position as "minutes:seconds".
func (s *KEFSpeaker) SongProgress() (string, error) {
	playMS, err := s.SongProgressMS()
	if err != nil {
		return "0:00", err
	}
	return fmt.Sprintf("%d:%02d", playMS/60000, (playMS/1000)%60), nil
}

// SongProgressMS returns the current playback position in milliseconds.
func (s *KEFSpeaker) SongProgressMS() (int, error) {
	data, err := s.getData("player:player/data/playTime")
	if err != nil {
		return 0, err
	}
	return parseJSONInt(data)
}
