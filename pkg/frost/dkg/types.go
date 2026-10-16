// Package dkg connects the inactive FROST host to public chain approval and
// durable readiness. Production activation is outside this package.
package dkg

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"reflect"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	binding "github.com/keep-network/keep-core/pkg/chain/ethereum/frostabi"
	"github.com/keep-network/keep-core/pkg/frost"
)

var ErrQuarantined = errors.New("FROST approval quarantined")
var ErrExpired = errors.New("FROST pending wallet expired unfunded")

type Descriptor = binding.FrostTypesDescriptor
type Result = binding.FrostDkgValidatorResult
type Wallet = binding.FrostWalletRegistryWallet

type Parameters struct {
	ChainID                                                             *big.Int
	Registry, Pool, Bridge                                              common.Address
	GroupSize, Threshold, ReadySeats, FalseReadySeats, UnavailableSeats uint16
	Profile                                                             uint8
	ChallengeBlocks                                                     uint64
}

func (p Parameters) Validate() error {
	if p.ChainID == nil || p.ChainID.Sign() <= 0 || p.ChainID.BitLen() > 256 || p.Registry == (common.Address{}) || p.Pool == (common.Address{}) || p.Bridge == (common.Address{}) || p.Profile != 1 || p.GroupSize == 0 || p.GroupSize > 100 || p.Threshold <= p.GroupSize/2 || p.Threshold > p.GroupSize || p.ReadySeats > p.GroupSize || uint32(p.ReadySeats) < uint32(p.Threshold)+uint32(p.FalseReadySeats)+uint32(p.UnavailableSeats) || p.ChallengeBlocks == 0 {
		return errors.New("invalid FROST chain policy")
	}
	return nil
}

type Selection struct {
	Epoch     uint64
	Members   []uint32
	Operators []common.Address
}

func (s Selection) Roster() []uint16 {
	seats := make([]uint16, len(s.Members))
	for i := range seats {
		seats[i] = uint16(i + 1)
	}
	return seats
}
func (s Selection) Validate(p Parameters) error {
	if s.Epoch == 0 || len(s.Members) != int(p.GroupSize) || len(s.Operators) != len(s.Members) {
		return errors.New("invalid FROST selection")
	}
	for i, id := range s.Members {
		if id == 0 || s.Operators[i] == (common.Address{}) {
			return errors.New("invalid FROST seat")
		}
		for j := 0; j < i; j++ {
			if (id == s.Members[j]) != (s.Operators[i] == s.Operators[j]) {
				return errors.New("inconsistent FROST operator mapping")
			}
		}
	}
	return nil
}
func NewDescriptor(p Parameters, s Selection, c frost.Candidate) (Descriptor, error) {
	if p.Validate() != nil || s.Validate(p) != nil || c.Profile != frost.ApprovedProfile || c.Epoch != s.Epoch || c.Threshold != p.Threshold || !reflect.DeepEqual(c.Roster, s.Roster()) || c.Descriptor == ([32]byte{}) {
		return Descriptor{}, errors.New("candidate does not match frozen FROST selection")
	}
	if _, e := schnorr.ParsePubKey(c.OutputKey[:]); e != nil {
		return Descriptor{}, e
	}
	return Descriptor{Scheme: 2, Profile: 1, ChainId: new(big.Int).Set(p.ChainID), Registry: p.Registry, Epoch: s.Epoch, Members: append([]uint32(nil), s.Members...), Operators: append([]common.Address(nil), s.Operators...), Threshold: p.Threshold, SnowfallDescriptor: c.Descriptor, OutputKey: c.OutputKey}, nil
}
func WalletID(q [32]byte) [32]byte {
	return crypto.Keccak256Hash(crypto.Keccak256([]byte("tbtc-v2/frost-wallet-id/v1")), q[:])
}
func DescriptorHash(d Descriptor) ([32]byte, error) {
	a, e := binding.FrostWalletRegistryMetaData.GetAbi()
	if e != nil {
		return [32]byte{}, e
	}
	t := *a.Methods["wallet"].Outputs[0].Type.TupleElems[0]
	b32, _ := abi.NewType("bytes32", "", nil)
	raw, e := (abi.Arguments{{Type: b32}, {Type: t}}).Pack(crypto.Keccak256Hash([]byte("tbtc-v2/frost-descriptor/v1")), d)
	if e != nil {
		return [32]byte{}, e
	}
	return crypto.Keccak256Hash(raw), nil
}
func ResultHash(r Result) ([32]byte, error) {
	a, e := binding.FrostWalletRegistryMetaData.GetAbi()
	if e != nil {
		return [32]byte{}, e
	}
	raw, e := a.Methods["submitDkgResult"].Inputs.Pack(r)
	if e != nil {
		return [32]byte{}, e
	}
	return crypto.Keccak256Hash(raw), nil
}
func NewResult(d Descriptor) Result {
	payload := append([]byte{d.Profile}, d.OutputKey[:]...)
	payload = append(payload, d.SnowfallDescriptor[:]...)
	indices := make([]*big.Int, len(d.Members))
	for i := range indices {
		indices[i] = big.NewInt(int64(i + 1))
	}
	t, _ := abi.NewType("uint32[]", "", nil)
	raw, _ := (abi.Arguments{{Type: t}}).Pack(d.Members)
	return Result{SubmitterMemberIndex: big.NewInt(1), GroupPubKey: payload, MisbehavedMembersIndices: []uint8{}, SigningMembersIndices: indices, Members: d.Members, MembersHash: crypto.Keccak256Hash(raw)}
}
func Sign(key *ecdsa.PrivateKey, digest [32]byte) ([]byte, error) {
	b, e := crypto.Sign(accounts.TextHash(digest[:]), key)
	if e == nil {
		b[64] += 27
	}
	return b, e
}
func SignedBy(signature []byte, digest [32]byte, operator common.Address) bool {
	if len(signature) != 65 || (signature[64] != 27 && signature[64] != 28) {
		return false
	}
	sig := append([]byte(nil), signature...)
	sig[64] -= 27
	if !crypto.ValidateSignatureValues(sig[64], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:64]), true) {
		return false
	}
	key, e := crypto.SigToPub(accounts.TextHash(digest[:]), sig)
	return e == nil && crypto.PubkeyToAddress(*key) == operator
}

type View struct {
	Head                               uint64
	HeadHash                           common.Hash
	State                              uint8
	Epoch, SubmittedAt, ResultDeadline uint64
	SubmittedHash                      [32]byte
	Submitted                          *Result
	ApprovedID                         [32]byte
	Approved                           Wallet
	ApprovalHash                       common.Hash
}
type Chain interface {
	Parameters(context.Context) (Parameters, error)
	Selection(context.Context, uint64) (Selection, error)
	View(context.Context, uint64) (View, error)
	ResultDigest(context.Context, uint64, Result) ([32]byte, error)
	ReadinessDigest(context.Context, Wallet, uint16, [32]byte) ([32]byte, error)
	Validate(context.Context, uint64, Result) (bool, error)
	Submit(context.Context, Result) error
	Challenge(context.Context) error
	Approve(context.Context) error
	Ready(context.Context, [32]byte, []uint16, [][32]byte, []byte) error
	Expire(context.Context, [32]byte) error
	ExpireDKG(context.Context) error
}

// WalletRecord has no secret bytes. The immutable descriptor and approved
// receipt are retained separately from mutable lifecycle and quarantine state.
type WalletRecord struct {
	Descriptor                     Descriptor
	DescriptorHash, ID, ResultHash [32]byte
	ApprovalBlock                  uint64
	ApprovalHash                   common.Hash
	Deadline                       uint64
	State                          string
	Quarantined                    bool
}
type Journal interface {
	frost.Journal
	SaveWallet(context.Context, WalletRecord) error
	LoadWallet(context.Context) (WalletRecord, error)
}
type Engine interface {
	frost.Engine
	ReloadKey(context.Context, frost.SigningRequest, frost.Providers) error
}

// ValidateDescriptor checks public metadata without interpreting any secret record.
func ValidateDescriptor(d Descriptor) error {
	if d.Scheme != 2 || d.Profile != 1 || d.ChainId == nil || d.ChainId.Sign() <= 0 || d.ChainId.BitLen() > 256 || d.Registry == (common.Address{}) || d.Epoch == 0 || d.SnowfallDescriptor == ([32]byte{}) || len(d.Members) == 0 || len(d.Members) > 100 || int(d.Threshold) <= len(d.Members)/2 || int(d.Threshold) > len(d.Members) {
		return errors.New("invalid immutable wallet descriptor")
	}
	if err := (Selection{Epoch: d.Epoch, Members: d.Members, Operators: d.Operators}).Validate(Parameters{GroupSize: uint16(len(d.Members))}); err != nil {
		return err
	}
	if _, err := schnorr.ParsePubKey(d.OutputKey[:]); err != nil {
		return err
	}
	id := WalletID(d.OutputKey)
	var label [20]byte
	copy(label[:], id[:20])
	if label == ([20]byte{}) {
		return errors.New("zero wallet label")
	}
	return nil
}
