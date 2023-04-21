package kefw2

import (
	"encoding/json"
	"fmt"
	"strings"
)

type KEFSpeaker struct {
	IPAddress  string
	Name       string
	Model      string
	MacAddress string
	Id         string
}

type Source string

const (
	SourceStandby   Source = "standby"
	SourceOptical   Source = "optical"
	SourceCoaxial   Source = "coaxial"
	SourceBluetooth Source = "bluetooth"
	SourceAux       Source = "aux"
	SourceUsb       Source = "usb"
	SourceWifi      Source = "wifi"
)

var (
	Models = map[string]string{
		"lsx2":   "KEF LSX II",
		"ls502w": "KEF LS50 II Wireless",
		"ls60w":  "KEF LS60 Wireless",
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
	s.getModelAndId()
	return nil
}

func (s *KEFSpeaker) getMACAddress() (string, error) {
	var macAddressData []map[string]interface{}
	data, err := s.getData("settings:/system/primaryMacAddress")
	if err != nil {
		return "", err
	}
	json.Unmarshal(data, &macAddressData)
	return macAddressData[0]["string_"].(string), nil
}

func (s *KEFSpeaker) getName() (string, error) {
	return JSONStringValue(s.getData("settings:/deviceName"))
}

func (s *KEFSpeaker) getModelAndId() (err error) {
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
			modelpart := strings.Split(s.Id, "-")[0]
			s.Model = Models[modelpart]
		}
	}
	return err
}

func (s KEFSpeaker) GetVolume() (volume int, err error) {
	return JSONIntValue(s.getData("player:volume"))
}

func (s KEFSpeaker) SetVolume(volume int) error {
	return nil
}

func (s KEFSpeaker) Mute() error {
	return nil
}

func (s KEFSpeaker) Unmute() error {
	return nil
}

func (s KEFSpeaker) PowerOn(power bool) error {
	return nil
}

func (s KEFSpeaker) PowerOff(power bool) error {
	return nil
}

func (s KEFSpeaker) SetSource(source Source) error {
	fmt.Println("Source to be set:", source)
	return nil
}

func (s *KEFSpeaker) GetSource() (source Source, err error) {
	var src string
	data, err := s.getData("settings:/kef/play/physicalSource")
	src, err = JSONStringValueByKey(data, "kefPhysicalSource", err)
	return Source(src), err
}

func (s *KEFSpeaker) GetPowerState() (bool, error) {
	data, err := s.getData("settings:/kef/host/speakerStatus")
	powerState, err := JSONStringValueByKey(data, "kefSpeakerStatus", err)
	if powerState == "powerOn" {
		return true, err
	}
	return false, err
}
