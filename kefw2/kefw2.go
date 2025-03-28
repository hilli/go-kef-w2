package kefw2

import (
	"encoding/json"
	"fmt"
	"strings"
)

type KEFSpeaker struct {
	IPAddress       string `mapstructure:"ip_address" json:"ip_address" yaml:"ip_address"`
	Name            string `mapstructure:"name" json:"name" yaml:"name"`
	Model           string `mapstructure:"model" json:"model" yaml:"model"`
	FirmwareVersion string `mapstructure:"firmware_version" json:"firmware_version" yaml:"firmware_version"`
	MacAddress      string `mapstructure:"mac_address" json:"mac_address" yaml:"mac_address"`
	Id              string `mapstructure:"id" json:"id" yaml:"id"`
	MaxVolume       int    `mapstructure:"max_volume" json:"max_volume" yaml:"max_volume"`
}

type KEFGrouping struct {
	GroupingMembers []KEFGroupingmember `json:"groupingMember"`
}

type KEFGroupingmember struct {
	Master   KEFGroupingData `json:"master"`
	Follower KEFGroupingData `json:"follower"`
}

type KEFGroupingData struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

var (
	Models = map[string]string{
		"lsxii":  "KEF LSX II",
		"ls502w": "KEF LS50 II Wireless",
		"ls60w":  "KEF LS60 Wireless",
		"LS60W":  "KEF LS60 Wireless",
	}
)

func NewSpeaker(IPAddress string) (KEFSpeaker, error) {
	if IPAddress == "" {
		return KEFSpeaker{}, fmt.Errorf("KEF Speaker IP is empty")
	}
	speaker := KEFSpeaker{
		IPAddress: IPAddress,
	}
	err := speaker.UpdateInfo()
	if err != nil {
		return speaker, err
	}
	return speaker, nil
}

func (s *KEFSpeaker) UpdateInfo() (err error) {
	s.MacAddress, err = s.getMACAddress()
	if err != nil {
		return err
	}
	s.Name, err = s.getName()
	if err != nil {
		return err
	}
	err = s.getId()
	if err != nil {
		return fmt.Errorf("failed to get speaker IDs: %w", err)
	}
	err = s.getModelAndVersion()
	if err != nil {
		return fmt.Errorf("failed to get model and version information: %w", err)
	}
	_, err = s.GetMaxVolume() // Don't actually need the maxvol, but saving it to config.
	if err != nil {
		return fmt.Errorf("failed to get maxvol: %w", err)
	}
	return nil
}

func (s *KEFSpeaker) getMACAddress() (string, error) {
	return JSONStringValue(s.getData("settings:/system/primaryMacAddress"))
}

func (s *KEFSpeaker) NetworkOperationMode() (CableMode, error) {
	cableMode, err := JSONUnmarshalValue(s.getData("settings:/kef/host/cableMode"))
	return cableMode.(CableMode), err
}

func (s *KEFSpeaker) getName() (string, error) {
	return JSONStringValue(s.getData("settings:/deviceName"))
}

func (s *KEFSpeaker) getId() (err error) {
	params := map[string]string{
		"roles": "@all",
		"from":  "0",
		"to":    "19",
	}
	data, err := s.getRows("grouping:members", params)
	if err != nil {
		return err
	}
	groupData := KEFGrouping{}
	err = json.Unmarshal(data, &groupData)
	speakersets := groupData.GroupingMembers
	for _, speakerset := range speakersets {
		if speakerset.Master.Name == s.Name {
			s.Id = speakerset.Master.Id
		}
	}
	return err
}

func (s *KEFSpeaker) getModelAndVersion() error {
	model, err := JSONStringValue(s.getData("settings:/releasetext"))
	modelAndVersion := strings.Split(model, "_")
	s.Model = Models[modelAndVersion[0]]
	if s.Model == "" {
		s.Model = modelAndVersion[0]
	}
	s.FirmwareVersion = modelAndVersion[1]
	return err
}

func (s KEFSpeaker) PlayPause() error {
	return s.setActivate("player:player/control", "control", "pause")
}

func (s KEFSpeaker) GetVolume() (volume int, err error) {
	return JSONIntValue(s.getData("player:volume"))
}

func (s KEFSpeaker) SetVolume(volume int) error {
	path := "player:volume"
	return s.setTypedValue(path, volume)
}

func (s KEFSpeaker) Mute() error {
	path := "settings:/mediaPlayer/mute"
	return s.setTypedValue(path, true)
}

func (s KEFSpeaker) Unmute() error {
	path := "settings:/mediaPlayer/mute"
	return s.setTypedValue(path, false)
}

func (s KEFSpeaker) IsMuted() (bool, error) {
	path := "settings:/mediaPlayer/mute"
	muted, err := JSONUnmarshalValue(s.getData(path))
	return muted.(bool), err
}

// PowerOff set the speaker to standby mode
func (s KEFSpeaker) PowerOff() error {
	return s.SetSource(SourceStandby)
}

func (s KEFSpeaker) SetSource(source Source) error {
	path := "settings:/kef/play/physicalSource"
	return s.setTypedValue(path, source)
}

func (s *KEFSpeaker) Source() (Source, error) {
	data, err := s.getData("settings:/kef/play/physicalSource")
	if err != nil {
		return SourceStandby, fmt.Errorf("Failed getting speaker source: %w", err)
	}
	src, err2 := JSONUnmarshalValue(data, err)
	return src.(Source), err2
}

func (s *KEFSpeaker) CanControlPlayback() (bool, error) {
	source, err := s.Source()
	if err != nil {
		return false, err
	}
	return ((source == SourceWiFi) || (source == SourceBluetooth)), nil
}

func (s *KEFSpeaker) IsPoweredOn() (bool, error) {
	powerState, err := JSONUnmarshalValue(s.getData("settings:/kef/host/speakerStatus"))
	if powerState == SpeakerStatusOn {
		return true, err
	}
	return false, err
}

func (s *KEFSpeaker) SpeakerState() (SpeakerStatus, error) {
	speakerStatus, err := JSONUnmarshalValue(s.getData("settings:/kef/host/speakerStatus"))
	return SpeakerStatus(speakerStatus.(SpeakerStatus)), err
}

func (s *KEFSpeaker) GetMaxVolume() (int, error) {
	path := "settings:/kef/host/maximumVolume"
	maxVolume, err := JSONIntValue(s.getData(path))
	s.MaxVolume = maxVolume
	return maxVolume, err
}

func (s *KEFSpeaker) SetMaxVolume(maxVolume int) error {
	path := "settings:/kef/host/maximumVolume"
	s.MaxVolume = maxVolume
	return s.setTypedValue(path, maxVolume)
}

func (s *KEFSpeaker) IsPlaying() (bool, error) {
	pd, err := s.PlayerData()
	if err != nil {
		return false, err
	}
	return pd.State == "playing", nil
}

// NextTrack works only if the speaker is playing in wifi mode
func (s *KEFSpeaker) NextTrack() error {
	return s.setActivate("player:player/control", "control", "next")
}

// PreviousTrack works only if the speaker is playing in wifi mode
func (s *KEFSpeaker) PreviousTrack() error {
	return s.setActivate("player:player/control", "control", "previous")
}

// PlayerData returns the current song progress as a string: "minutes:seconds"
func (s *KEFSpeaker) SongProgress() (string, error) {
	playMs, err := s.SongProgressMS()
	if err != nil {
		fmt.Println("err", err)
		return "0:00", err
	}
	playTime := fmt.Sprintf("%d:%02d", playMs/60000, (playMs/1000)%60)
	return playTime, err
}

// SongProgressMS returns the current song progress in milliseconds
func (s *KEFSpeaker) SongProgressMS() (int, error) {
	path := "player:player/data/playTime"
	data, err := s.getData(path)
	playMS, err := JSONIntValue(data, err)
	if err != nil {
		fmt.Println("err", err)
		return 0, err
	}
	return playMS, err
}
