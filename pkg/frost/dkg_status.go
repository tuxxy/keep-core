package frost

// DKGState is local storage state. KeyStored does not mean the operator has
// reloaded the completed key in a fresh worker or certified chain readiness.
type DKGState string

const (
	DKGInProgress DKGState = "InProgress"
	DKGKeyStored  DKGState = "KeyStored"
	DKGSeatsLost  DKGState = "SeatsLost"
)

// DKGStatus reports the original local seats, without renumbering them. A
// SeatsLost result is terminal for this domain and attempt. It comes from the
// durable claim plus the absence of a saved key and live ownership; it does not
// require a last write by a dying process. LostSeats includes every LocalSeat
// exactly when State is SeatsLost. No candidate payload is decoded to report it.
type DKGStatus struct {
	Domain     Domain
	Attempt    [32]byte
	State      DKGState
	LocalSeats []uint16
	LostSeats  []uint16
}
