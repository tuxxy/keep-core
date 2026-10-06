package frost

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"unicode/utf8"
)

// Domain binds a wallet (or an unfunded DKG epoch) to one deployment. Wallet is
// zero only before a DKG has assigned an identity. Epoch is never a retry block.
type Domain struct {
	Network  string
	Chain    [32]byte
	Registry [20]byte
	Wallet   [32]byte
	Epoch    uint64
}

func (d Domain) Validate() error {
	if len(d.Network) == 0 || len(d.Network) > 128 || !utf8.ValidString(d.Network) || d.Chain == ([32]byte{}) || d.Registry == ([20]byte{}) || d.Epoch == 0 {
		return errors.New("invalid FROST deployment domain")
	}
	return nil
}

// Bytes is a versioned host encoding, separate from all frozen worker codecs.
func (d Domain) Bytes() []byte {
	b := []byte("keep-core/snowfall/domain/v1")
	b = binary.BigEndian.AppendUint16(b, uint16(len(d.Network)))
	b = append(b, d.Network...)
	b = append(b, d.Chain[:]...)
	b = append(b, d.Registry[:]...)
	b = append(b, d.Wallet[:]...)
	b = binary.BigEndian.AppendUint64(b, d.Epoch)
	return b
}
func (d Domain) ID() [32]byte { return sha256.Sum256(d.Bytes()) }

// NewAttempt constructs agreed channel/session bytes. session must be agreed by
// all operators; a fresh start block or session is required after any claim.
func (d Domain) NewAttempt(purpose string, session [32]byte, start uint64) (Attempt, error) {
	if err := d.Validate(); err != nil {
		return Attempt{}, err
	}
	if (purpose != "dkg" && purpose != "sign") || session == ([32]byte{}) || start == 0 {
		return Attempt{}, errors.New("invalid FROST attempt context")
	}
	channel := append(d.Bytes(), []byte("/"+purpose)...)
	return Attempt{Channel: channel, Session: append([]byte("keep-core/snowfall/session/v1/"), session[:]...), StartBlock: start}, nil
}
func (d Domain) ValidateAttempt(a Attempt, purpose string) error {
	if len(a.Session) < 32 {
		return errors.New("invalid FROST session")
	}
	var session [32]byte
	copy(session[:], a.Session[len(a.Session)-32:])
	want, err := d.NewAttempt(purpose, session, a.StartBlock)
	if err != nil {
		return err
	}
	if !bytes.Equal(want.Channel, a.Channel) || !bytes.Equal(want.Session, a.Session) {
		return errors.New("attempt is outside its storage domain")
	}
	return nil
}

// Journal is required by the guarded engine. A claim is irrevocable even if
// worker startup fails. Implementations must hold exclusive ownership until
// the operation ends; closing the journal while an engine uses it is invalid.
type Journal interface {
	Store
	Domain() Domain
	Claim(context.Context, [32]byte, string, []byte) (release func(), err error)
	SaveKey(context.Context, KeyReady) error
	LoadKey(context.Context) (KeyReady, error)
}
