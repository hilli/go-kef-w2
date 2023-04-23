package kefw2

// Source represents the source of the audio signal (kefPhysicalSource)
type Source string

const (
	SourceAux       Source = "analog"
	SourceBluetooth Source = "bluetooth"
	SourceCoaxial   Source = "coaxial"
	SourceOptical   Source = "optical"
	SourceStandby   Source = "standby"
	SourceTV        Source = "tv"
	SourceUsb       Source = "usb"
	SourceWifi      Source = "wifi"
)

// String returns the string representation of the source
func (s *Source) String() string {
	return string(*s)
}
