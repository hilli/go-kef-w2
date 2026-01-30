package kefw2

// CableMode represents the network connection type of the speaker.
// It corresponds to the "kefCableMode" type in the KEF API.
type CableMode string

// Available network connection modes.
const (
	// Wired indicates the speaker is connected via Ethernet cable.
	Wired CableMode = "wired"
	// Wireless indicates the speaker is connected via WiFi.
	Wireless CableMode = "wireless"
)

// String returns the string representation of the cable mode.
func (s *CableMode) String() string {
	return string(*s)
}
