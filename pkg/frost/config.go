package frost

import "errors"

// Config reserves an explicit default-off node switch. The local test harness
// constructs its engine directly. Node dispatch remains ECDSA in this tranche.
type Config struct{ Enabled bool }

func (c Config) ValidateNode() error {
	if c.Enabled {
		return errors.New("FROST node activation is unavailable: only the local K-01 harness is supported")
	}
	return nil
}
