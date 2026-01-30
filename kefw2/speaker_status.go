package kefw2

// SpeakerStatus represents the current power/operational state of the speaker.
// It corresponds to the "kefSpeakerStatus" type in the KEF API.
type SpeakerStatus string

// Available speaker status values.
const (
	// SpeakerStatusStandby indicates the speaker is in low-power standby mode.
	SpeakerStatusStandby SpeakerStatus = "standby"
	// SpeakerStatusOn indicates the speaker is powered on and operational.
	SpeakerStatusOn SpeakerStatus = "powerOn"
	// SpeakerInNetworkSetup indicates the speaker is in network configuration mode.
	SpeakerInNetworkSetup SpeakerStatus = "networkSetup"
	// SpeakerInFirmwareUpgrade indicates a firmware update is in progress.
	SpeakerInFirmwareUpgrade SpeakerStatus = "firmwareUpgrade"
)

// String returns the string representation of the speaker status.
func (s *SpeakerStatus) String() string {
	return string(*s)
}
