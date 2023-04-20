package kefw2

import (
	"encoding/json"
	"fmt"
)

type KEFSpeaker struct {
	IPAddress    string
	Name         string
	Model        string
	MacAddress   string
	Version      string
	SerialNumber string
}

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
	var nameData []map[string]interface{}
	data, err := s.getData("settings:/deviceName")
	if err != nil {
		return "", err
	}
	json.Unmarshal(data, &nameData)
	return nameData[0]["string_"].(string), nil
}

func (s KEFSpeaker) GetVolume() (int, error) {

	return 0, nil
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

func (s KEFSpeaker) SetSource(source string) error {
	return nil
}
