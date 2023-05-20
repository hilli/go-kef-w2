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
	s.getId()
	s.getModelAndVersion()
	s.getMaxVolume()
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
	var groupData map[string]interface{}
	err = json.Unmarshal(data, &groupData)
	speakersets := groupData["rows"].([]interface{})
	for _, speakerset := range speakersets {
		speakerset := speakerset.(map[string]interface{})
		if speakerset["title"] == s.Name {
			s.Id = speakerset["id"].(string)
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

func (s *KEFSpeaker) Source() (source Source, err error) {
	src, err2 := JSONUnmarshalValue(s.getData("settings:/kef/play/physicalSource"))
	return src.(Source), err2
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

func (s *KEFSpeaker) getMaxVolume() error {
	path := "settings:/kef/host/maximumVolume"
	maxVolume, err := JSONIntValue(s.getData(path))
	s.MaxVolume = maxVolume
	return err
}

func (s *KEFSpeaker) IsPlaying() (bool, error) {
	pd, err := s.PlayerData()
	if err != nil {
		return false, err
	}
	return pd.State == "playing", nil
}
