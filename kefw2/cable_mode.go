package kefw2

type CableMode string

const (
	Wired    CableMode = "wired"
	Wireless CableMode = "wireless"
)

// String returns the string representation of the source
func (s *CableMode) String() string {
	return string(*s)
}
