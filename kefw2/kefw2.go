package kefw2

import (
	"fmt"
	"time"
)

type KEFSpeaker struct {
	KEFSpeakerIP string
	Name				 string
	Model				 string
	MacAddress	 string
	IPAddress		 string
	Version			 string
	SerialNumber string
	pollingQueue string
	lastPollTime time.Time
}

func NewSpeaker(KEFSpeakerIP string) (KEFSpeaker, error) {
	if KEFSpeakerIP == "" {
		return KEFSpeaker{}, fmt.Errorf("KEF Speaker IP is empty")
	}
	return KEFSpeaker{
		KEFSpeakerIP: KEFSpeakerIP,
	}, nil
}

func (s KEFSpeaker) UpdateInfo() (error) {
	return nil
}

func (s KEFSpeaker) SetVolume(volume int) (error) {
	return nil
}

func (s KEFSpeaker) Mute() (error) {
	return nil
}

func (s KEFSpeaker) Unmute() (error) {
	return nil
}

func (s KEFSpeaker) PowerOn(power bool) (error) {
	return nil
}

func (s KEFSpeaker) PowerOff(power bool) (error) {
	return nil
}

func (s KEFSpeaker) SetSource(source string) (error) {
	return nil
}

