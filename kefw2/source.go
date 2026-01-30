package kefw2

// Source represents an audio input source on the KEF speaker.
// It corresponds to the "kefPhysicalSource" type in the KEF API.
type Source string

// Available audio sources for KEF speakers.
const (
	// SourceAux is the 3.5mm auxiliary input.
	SourceAux Source = "analog"
	// SourceBluetooth is the Bluetooth input.
	SourceBluetooth Source = "bluetooth"
	// SourceCoaxial is the coaxial digital input.
	SourceCoaxial Source = "coaxial"
	// SourceOptical is the optical (TOSLINK) digital input.
	SourceOptical Source = "optical"
	// SourceStandby represents the speaker being in standby mode.
	SourceStandby Source = "standby"
	// SourceTV is the TV audio input (HDMI ARC/eARC on supported models).
	SourceTV Source = "tv"
	// SourceUSB is the USB audio input.
	SourceUSB Source = "usb"
	// SourceWiFi is the network streaming input (AirPlay, Chromecast, Roon, etc.).
	SourceWiFi Source = "wifi"
)

// String returns the string representation of the source.
func (s *Source) String() string {
	return string(*s)
}
