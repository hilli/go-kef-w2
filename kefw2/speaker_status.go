package kefw2

type SpeakerStatus string

const (
	SpeakerStatusStandby     SpeakerStatus = "standby"
	SpeakerStatusOn          SpeakerStatus = "powerOn"
	SpeakerInNetworkSetup    SpeakerStatus = "networkSetup"
	SpeakerInFirmwareUpgrade SpeakerStatus = "firmwareUpgrade"
)

// String returns the string representation of the speaker status
func (s *SpeakerStatus) String() string {
	return string(*s)
}
