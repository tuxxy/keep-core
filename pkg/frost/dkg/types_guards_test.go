package dkg

// Mutation batch M10-dkg: public-metadata guards of the FROST DKG host
// (types.go) and the attestation wire format (network.go).
//
// Each test names the invariant it pins. They pass on the base; the proof is
// that they fail at their own assertions under the listed mutants.
//
// FIXTURES (labelled, test-only):
//   - fixtureParams / fixtureSelection: a valid five-seat chain policy and a
//     selection whose seats one fixture operator owns;
//   - fixtureOutputKey: the x coordinate of the secp256k1 generator, a public
//     constant used only as a parseable x-only output key;
//   - operator keys are fresh crypto.GenerateKey keys; nothing secret is
//     decoded or printed.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/keep-network/keep-core/pkg/frost"
)

// fixtureOutputKey is secp256k1 G.x (public constant, FIXTURE).
func fixtureOutputKey(t *testing.T) [32]byte {
	t.Helper()
	raw, err := hex.DecodeString("79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
	if err != nil {
		t.Fatal(err)
	}
	var key [32]byte
	copy(key[:], raw)
	return key
}

// fixtureParams is a valid five-seat FROST chain policy (FIXTURE).
func fixtureParams() Parameters {
	return Parameters{ChainID: big.NewInt(1337), Registry: common.Address{0xA1}, Pool: common.Address{0xA2}, Bridge: common.Address{0xA3}, GroupSize: 5, Threshold: 3, ReadySeats: 4, FalseReadySeats: 1, Profile: 1, ChallengeBlocks: 2}
}

// fixtureKey is a fresh operator key (FIXTURE).
func fixtureKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// fixtureSelection gives every seat of p to one operator (FIXTURE). The
// registry may select one operator for several seats (KC-SF-V3 line 315).
func fixtureSelection(p Parameters, operator common.Address) Selection {
	s := Selection{Epoch: 77}
	for i := 0; i < int(p.GroupSize); i++ {
		s.Members = append(s.Members, 11)
		s.Operators = append(s.Operators, operator)
	}
	return s
}

func fixtureCandidate(t *testing.T, p Parameters, s Selection) frost.Candidate {
	t.Helper()
	return frost.Candidate{Epoch: s.Epoch, Descriptor: [32]byte{0xD1}, OutputKey: fixtureOutputKey(t), Roster: s.Roster(), Threshold: p.Threshold, Profile: frost.ApprovedProfile}
}

func fixtureDescriptor(t *testing.T) Descriptor {
	t.Helper()
	p := fixtureParams()
	s := fixtureSelection(p, crypto.PubkeyToAddress(fixtureKey(t).PublicKey))
	d, err := NewDescriptor(p, s, fixtureCandidate(t, p, s))
	if err != nil {
		t.Fatalf("fixture descriptor refused: %v", err)
	}
	return d
}

// Invariant: Parameters.Validate accepts exactly the supported FROST chain
// policy: a positive chain ID of at most 256 bits, non-zero registry, pool
// and bridge, profile 1, 1..100 seats, an honest-majority threshold of at
// most the group size, and ReadySeats between T+B+F and the group size, with
// a non-zero challenge period. Each refused case breaks one clause; each
// accepted case sits on a boundary. Rows types.go:37 (CB, AR), :38.
func TestParametersValidateEachClause(t *testing.T) {
	t.Parallel()
	refused := map[string]func(*Parameters){
		"nil-chain":            func(p *Parameters) { p.ChainID = nil },
		"zero-chain":           func(p *Parameters) { p.ChainID = big.NewInt(0) },
		"negative-chain":       func(p *Parameters) { p.ChainID = big.NewInt(-1) },
		"chain-257-bits":       func(p *Parameters) { p.ChainID = new(big.Int).Lsh(big.NewInt(1), 256) },
		"zero-registry":        func(p *Parameters) { p.Registry = common.Address{} },
		"zero-pool":            func(p *Parameters) { p.Pool = common.Address{} },
		"zero-bridge":          func(p *Parameters) { p.Bridge = common.Address{} },
		"profile-2":            func(p *Parameters) { p.Profile = 2 },
		"no-seats":             func(p *Parameters) { p.GroupSize, p.Threshold, p.ReadySeats, p.FalseReadySeats = 0, 0, 0, 0 },
		"101-seats":            func(p *Parameters) { p.GroupSize, p.Threshold, p.ReadySeats, p.FalseReadySeats = 101, 51, 51, 0 },
		"threshold-half":       func(p *Parameters) { p.Threshold, p.FalseReadySeats = 2, 0 },
		"threshold-over-group": func(p *Parameters) { p.Threshold, p.ReadySeats, p.FalseReadySeats = 6, 5, 0 },
		"ready-over-group":     func(p *Parameters) { p.ReadySeats = 6 },
		// T+B+F = 3+1+0 = 4 > 3 ready seats.
		"ready-below-t-plus-false": func(p *Parameters) { p.ReadySeats = 3 },
		"ready-below-t-plus-unavailable": func(p *Parameters) {
			p.ReadySeats, p.FalseReadySeats, p.UnavailableSeats = 3, 0, 1
		},
		"no-challenge-period": func(p *Parameters) { p.ChallengeBlocks = 0 },
	}
	for name, change := range refused {
		t.Run(name, func(t *testing.T) {
			p := fixtureParams()
			change(&p)
			if err := p.Validate(); err == nil {
				t.Fatalf("Validate accepted an unsupported FROST chain policy (%s)", name)
			}
		})
	}
	accepted := map[string]func(*Parameters){
		"fixture":            func(*Parameters) {},
		"threshold-is-group": func(p *Parameters) { p.Threshold, p.ReadySeats, p.FalseReadySeats = 5, 5, 0 },
		"100-seats":          func(p *Parameters) { p.GroupSize, p.Threshold, p.ReadySeats, p.FalseReadySeats = 100, 51, 51, 0 },
		"ready-is-t-plus-b-plus-f": func(p *Parameters) {
			p.ReadySeats, p.FalseReadySeats, p.UnavailableSeats = 5, 1, 1
		},
		"chain-256-bits": func(p *Parameters) {
			p.ChainID = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
		},
	}
	for name, change := range accepted {
		t.Run(name, func(t *testing.T) {
			p := fixtureParams()
			change(&p)
			if err := p.Validate(); err != nil {
				t.Fatalf("Validate refused a supported FROST chain policy (%s): %v", name, err)
			}
		})
	}
}

// Invariant: a selection is accepted only for a non-zero epoch with exactly
// GroupSize seats, a non-zero member ID and operator per seat, and a
// consistent ID-to-operator mapping (one operator may hold several seats).
// Row types.go:58.
func TestSelectionValidateEachClause(t *testing.T) {
	t.Parallel()
	p := fixtureParams()
	a := crypto.PubkeyToAddress(fixtureKey(t).PublicKey)
	b := crypto.PubkeyToAddress(fixtureKey(t).PublicKey)
	refused := map[string]func(*Selection){
		"epoch-zero":     func(s *Selection) { s.Epoch = 0 },
		"short":          func(s *Selection) { s.Members, s.Operators = s.Members[:4], s.Operators[:4] },
		"operators-long": func(s *Selection) { s.Operators = append(s.Operators, a) },
		"member-zero":    func(s *Selection) { s.Members[2] = 0 },
		"operator-zero":  func(s *Selection) { s.Operators[2] = common.Address{} },
		"id-two-operators": func(s *Selection) {
			s.Operators[3] = b
		},
		"operator-two-ids": func(s *Selection) { s.Members[3] = 12 },
	}
	for name, change := range refused {
		t.Run(name, func(t *testing.T) {
			s := fixtureSelection(p, a)
			change(&s)
			if err := s.Validate(p); err == nil {
				t.Fatalf("Selection.Validate accepted an invalid selection (%s)", name)
			}
		})
	}
	t.Run("distinct-operators", func(t *testing.T) {
		s := Selection{Epoch: 77, Members: []uint32{11, 12, 13, 14, 11}, Operators: []common.Address{a, b, {3}, {4}, a}}
		if err := s.Validate(p); err != nil {
			t.Fatalf("Selection.Validate refused a consistent selection: %v", err)
		}
	})
}

// Invariant: NewDescriptor binds a worker candidate to the frozen selection:
// it refuses any candidate whose profile, epoch, threshold, roster,
// SnowfallDescriptor or output key differs, each on its own. Row types.go:73.
func TestNewDescriptorRefusesEachCandidateMismatch(t *testing.T) {
	t.Parallel()
	p := fixtureParams()
	s := fixtureSelection(p, crypto.PubkeyToAddress(fixtureKey(t).PublicKey))
	refused := map[string]func(*frost.Candidate){
		"profile":         func(c *frost.Candidate) { c.Profile = "other" },
		"epoch":           func(c *frost.Candidate) { c.Epoch++ },
		"threshold":       func(c *frost.Candidate) { c.Threshold++ },
		"roster":          func(c *frost.Candidate) { c.Roster = []uint16{1, 2, 3, 4, 6} },
		"zero-descriptor": func(c *frost.Candidate) { c.Descriptor = [32]byte{} },
		"bad-output-key":  func(c *frost.Candidate) { c.OutputKey = [32]byte{} },
	}
	for name, change := range refused {
		t.Run(name, func(t *testing.T) {
			c := fixtureCandidate(t, p, s)
			change(&c)
			if _, err := NewDescriptor(p, s, c); err == nil {
				t.Fatalf("NewDescriptor accepted a candidate with a different %s", name)
			}
		})
	}
	t.Run("matching", func(t *testing.T) {
		c := fixtureCandidate(t, p, s)
		d, err := NewDescriptor(p, s, c)
		if err != nil || d.Epoch != s.Epoch || d.OutputKey != c.OutputKey || d.SnowfallDescriptor != c.Descriptor || d.Threshold != p.Threshold {
			t.Fatalf("NewDescriptor refused or changed a matching candidate: %+v %v", d, err)
		}
	})
}

// Invariant: the result a host submits names seats 1..n in roster order as
// its signing members, with seat 1 as submitter (the registry's validator
// requires signingMembersIndices[i] == i+1). Row types.go:113.
func TestNewResultNamesEverySeatInOrder(t *testing.T) {
	t.Parallel()
	d := fixtureDescriptor(t)
	r := NewResult(d)
	if len(r.SigningMembersIndices) != len(d.Members) {
		t.Fatalf("result names %d signing seats for %d members", len(r.SigningMembersIndices), len(d.Members))
	}
	for i, index := range r.SigningMembersIndices {
		if index.Cmp(big.NewInt(int64(i+1))) != 0 {
			t.Fatalf("signing member %d is seat %v, want %d", i, index, i+1)
		}
	}
	if r.SubmitterMemberIndex.Cmp(big.NewInt(1)) != 0 || len(r.MisbehavedMembersIndices) != 0 {
		t.Fatalf("result submitter/misbehaved: %v %v", r.SubmitterMemberIndex, r.MisbehavedMembersIndices)
	}
}

// signedBy runs SignedBy and turns a panic into a reported value: the
// function reads network input and must answer false, never panic.
func signedBy(signature []byte, digest [32]byte, operator common.Address) (ok bool, panicked any) {
	defer func() { panicked = recover() }()
	return SignedBy(signature, digest, operator), nil
}

// Invariant: SignedBy accepts only a 65-byte signature with v in {27, 28}
// that recovers to the operator, and answers false (never panics) for any
// other input. Row types.go:127.
func TestSignedByRefusesMalformedSignatures(t *testing.T) {
	t.Parallel()
	key := fixtureKey(t)
	operator := crypto.PubkeyToAddress(key.PublicKey)
	digest := [32]byte{0x5E}
	valid, err := Sign(key, digest)
	if err != nil {
		t.Fatal(err)
	}
	if ok, p := signedBy(valid, digest, operator); !ok || p != nil {
		t.Fatalf("positive control: a valid operator signature was refused (panic %v)", p)
	}
	cases := map[string][]byte{
		"empty":        nil,
		"64-bytes":     valid[:64],
		"66-bytes":     append(append([]byte(nil), valid...), 0),
		"v-29":         append(append([]byte(nil), valid[:64]...), 29),
		"v-0":          append(append([]byte(nil), valid[:64]...), 0),
		"other-signer": nil,
	}
	other, err := Sign(fixtureKey(t), digest)
	if err != nil {
		t.Fatal(err)
	}
	cases["other-signer"] = other
	for name, signature := range cases {
		t.Run(name, func(t *testing.T) {
			ok, p := signedBy(signature, digest, operator)
			if p != nil {
				t.Fatalf("SignedBy panicked on a %s signature: %v", name, p)
			}
			if ok {
				t.Fatalf("SignedBy accepted a %s signature", name)
			}
		})
	}
}

// Invariant: ValidateDescriptor accepts only scheme 2 / profile 1 metadata
// with a positive chain ID of at most 256 bits, a non-zero registry, epoch
// and SnowfallDescriptor, 1..100 members, an honest-majority threshold of at
// most the member count, a consistent operator mapping and a parseable
// x-only output key. Each refused case breaks one clause. Rows types.go:188
// (4), :195.
func TestValidateDescriptorEachClause(t *testing.T) {
	t.Parallel()
	refused := map[string]func(*Descriptor){
		"scheme-1":         func(d *Descriptor) { d.Scheme = 1 },
		"profile-2":        func(d *Descriptor) { d.Profile = 2 },
		"nil-chain":        func(d *Descriptor) { d.ChainId = nil },
		"zero-chain":       func(d *Descriptor) { d.ChainId = big.NewInt(0) },
		"negative-chain":   func(d *Descriptor) { d.ChainId = big.NewInt(-5) },
		"chain-257-bits":   func(d *Descriptor) { d.ChainId = new(big.Int).Lsh(big.NewInt(1), 256) },
		"zero-registry":    func(d *Descriptor) { d.Registry = common.Address{} },
		"zero-epoch":       func(d *Descriptor) { d.Epoch = 0 },
		"zero-snowfall":    func(d *Descriptor) { d.SnowfallDescriptor = [32]byte{} },
		"no-members":       func(d *Descriptor) { d.Members, d.Operators = nil, nil },
		"threshold-half":   func(d *Descriptor) { d.Threshold = 2 },
		"threshold-one":    func(d *Descriptor) { d.Threshold = 1 },
		"threshold-over-n": func(d *Descriptor) { d.Threshold = 6 },
		"operator-mapping": func(d *Descriptor) { d.Operators[1] = common.Address{0xEE} },
		"zero-output-key":  func(d *Descriptor) { d.OutputKey = [32]byte{} },
		"x-not-on-curve": func(d *Descriptor) {
			for i := range d.OutputKey {
				d.OutputKey[i] = 0xFF
			}
		},
		"101-members": func(d *Descriptor) {
			for len(d.Members) < 101 {
				d.Members = append(d.Members, d.Members[0])
				d.Operators = append(d.Operators, d.Operators[0])
			}
			d.Threshold = 51
		},
	}
	for name, change := range refused {
		t.Run(name, func(t *testing.T) {
			d := fixtureDescriptor(t)
			change(&d)
			if err := ValidateDescriptor(d); err == nil {
				t.Fatalf("ValidateDescriptor accepted invalid wallet metadata (%s)", name)
			}
		})
	}
	accepted := map[string]func(*Descriptor){
		"fixture":            func(*Descriptor) {},
		"threshold-majority": func(d *Descriptor) { d.Threshold = 3 },
		"threshold-is-n":     func(d *Descriptor) { d.Threshold = 5 },
	}
	for name, change := range accepted {
		t.Run(name, func(t *testing.T) {
			d := fixtureDescriptor(t)
			change(&d)
			if err := ValidateDescriptor(d); err != nil {
				t.Fatalf("ValidateDescriptor refused valid wallet metadata (%s): %v", name, err)
			}
		})
	}
}

func fixtureAttestation(seat uint16) Attestation {
	return Attestation{Stage: "result", Epoch: 77, Seat: seat, Digest: [32]byte{7}, Signature: bytes.Repeat([]byte{1}, 65)}
}

// Invariant: an attestation is encoded only for seats 1..100, a 65-byte
// signature and stage "result" or "ready"; each refused case breaks one
// clause. Rows network.go:25 (3).
func TestAttestationMarshalEachClause(t *testing.T) {
	t.Parallel()
	refused := map[string]func(*Attestation){
		"seat-0":       func(a *Attestation) { a.Seat = 0 },
		"seat-101":     func(a *Attestation) { a.Seat = 101 },
		"signature-64": func(a *Attestation) { a.Signature = a.Signature[:64] },
		"signature-66": func(a *Attestation) { a.Signature = append(a.Signature, 1) },
		"stage-other":  func(a *Attestation) { a.Stage = "approve" },
	}
	for name, change := range refused {
		t.Run(name, func(t *testing.T) {
			a := fixtureAttestation(3)
			change(&a)
			if _, err := a.Marshal(); err == nil {
				t.Fatalf("Marshal encoded an invalid attestation (%s)", name)
			}
		})
	}
	for _, seat := range []uint16{1, 100} {
		for _, stage := range []string{"result", "ready"} {
			a := fixtureAttestation(seat)
			a.Stage = stage
			if _, err := a.Marshal(); err != nil {
				t.Fatalf("Marshal refused a valid %s attestation for seat %d: %v", stage, seat, err)
			}
		}
	}
}

// padTo pads a JSON encoding with trailing whitespace to exactly n bytes.
func padTo(t *testing.T, raw []byte, n int) []byte {
	t.Helper()
	if len(raw) > n {
		t.Fatalf("fixture encoding is %d bytes, over %d", len(raw), n)
	}
	return append(raw, bytes.Repeat([]byte(" "), n-len(raw))...)
}

// Invariant: Unmarshal decodes an attestation of at most 2048 bytes and
// re-checks it; a longer input is refused before decoding. Rows network.go:31
// (2), :32.
func TestAttestationUnmarshalSizeBound(t *testing.T) {
	t.Parallel()
	a := fixtureAttestation(100)
	raw, err := a.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{len(raw), 2048} {
		var got Attestation
		if err := got.Unmarshal(padTo(t, append([]byte(nil), raw...), n)); err != nil {
			t.Fatalf("Unmarshal refused a valid %d-byte attestation: %v", n, err)
		}
		if got.Seat != 100 || got.Digest != a.Digest || !bytes.Equal(got.Signature, a.Signature) {
			t.Fatalf("Unmarshal of %d bytes changed the attestation: %+v", n, got)
		}
	}
	var got Attestation
	if err := got.Unmarshal(padTo(t, append([]byte(nil), raw...), 2049)); err == nil {
		t.Fatal("Unmarshal accepted a 2049-byte attestation")
	}
	bad, err := json.Marshal(Attestation{Stage: "result", Epoch: 77, Seat: 0, Signature: a.Signature})
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Unmarshal(bad); err == nil {
		t.Fatal("Unmarshal accepted an attestation for seat 0")
	}
}

// Invariant: the key-reload transport has no networking; every worker I/O
// attempt fails (KC-SF-V3 4.2: reload is local). Row executor.go:863.
func TestReloadTransportRefusesAllIO(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var tr reloadOnlyTransport
	if _, err := tr.Receive(ctx); err == nil {
		t.Fatal("reload transport delivered a message")
	}
	if err := tr.Broadcast(ctx, 1, []byte{1}); err == nil {
		t.Fatal("reload transport broadcast a message")
	}
	if err := tr.SendPrivate(ctx, 1, 2, []byte{1}); err == nil {
		t.Fatal("reload transport sent a private message")
	}
}
