package ethereum

// Acceptance tests: the FROST chain adapter must use the block hash the RPC
// returns, never one recomputed from a decoded header.
//
// Invariant: every block hash the FROST chain adapter uses (the EpochView
// read block, View.HeadHash, View.ApprovalHash, the head-stability recheck,
// redemption Head.Hash and HeaderHash) is the `hash` the Ethereum RPC returned
// for that block. On a Prague chain (mainnet and Sepolia since May 2025,
// anvil's default) go-ethereum v1.13.15 drops the header's requestsHash
// (EIP-7685), so types.Header.Hash() is not the block's hash.
// Pre-Prague (Cancun-shaped) behaviour is unchanged: every Cancun subtest
// pins the same results as today.
//
// Refs: chaosnet-terminal/FINDING-FROST-PRAGUE-HEADER-HASH.md (2026-11-18);
// decision (tux, 2026-11-18): use the RPC's block hash, no go-ethereum upgrade.
//
// The RPC is the in-process FIXTURE in frost_prague_rpc_fixture_test.go.

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	binding "github.com/keep-network/keep-core/pkg/chain/ethereum/frostabi"
	"github.com/keep-network/keep-core/pkg/chain/ethereum/frostbridgeabi"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
	"github.com/keep-network/keep-core/pkg/frost/redemption"
)

const (
	pragueTestHead          = uint64(10)
	pragueTestApprovalBlock = uint64(7)
	pragueTestEpoch         = uint64(2)
)

// Recorded vector (real data, not a fixture): block 1 of a fresh anvil 1.4.4
// (Prague by default) on loopback, 2026-11-18, header fields only.
const recordedAnvilPragueBlock1 = `{
 "hash":"0x7528b2cd72d5a2ba6085826cd08bd419a26845559f5c910e807a59c9e1c3db74",
 "parentHash":"0x771101a3c209f428b8b845d7295f80351db79a14cd6908c3819b854a0d4db0dc",
 "sha3Uncles":"0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
 "miner":"0x0000000000000000000000000000000000000000",
 "stateRoot":"0x9c8eaf493f8b4edce2ba1647343eadcc0989cf461e712c0a6253ff2ca1842bb7",
 "transactionsRoot":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
 "receiptsRoot":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
 "logsBloom":"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
 "difficulty":"0x0","number":"0x1","gasLimit":"0x1c9c380","gasUsed":"0x0","timestamp":"0x6afdc98b","extraData":"0x",
 "mixHash":"0x6e48e0b8bccc12e0c4abedb65fb6974268fefc54ce5baf8c55ceeacb676b11b9","nonce":"0x0000000000000000",
 "baseFeePerGas":"0x3b9aca00",
 "withdrawalsRoot":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
 "blobGasUsed":"0x0","excessBlobGas":"0x0",
 "parentBeaconBlockRoot":"0x0000000000000000000000000000000000000000000000000000000000000000",
 "requestsHash":"0xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
}`

// TestPragueFixtureHeaderHash checks the fixture's hash function, not the
// adapter (it passes before and after the fix). It reproduces a real Prague
// node's block hash from the recorded anvil header, shows go-ethereum
// v1.13.15's recomputed hash differs (the defect), and shows the fixture
// encodes a Cancun header exactly as go-ethereum does.
func TestPragueFixtureHeaderHash(t *testing.T) {
	t.Parallel()
	var header types.Header
	if err := json.Unmarshal([]byte(recordedAnvilPragueBlock1), &header); err != nil {
		t.Fatalf("decoding recorded header: %v", err)
	}
	var extra struct {
		Hash         common.Hash `json:"hash"`
		RequestsHash common.Hash `json:"requestsHash"`
	}
	if err := json.Unmarshal([]byte(recordedAnvilPragueBlock1), &extra); err != nil {
		t.Fatalf("decoding recorded hash: %v", err)
	}
	if got := trueBlockHash(&header, &extra.RequestsHash); got != extra.Hash {
		t.Fatalf("fixture hash of the recorded Prague header = %s, anvil returned %s", got, extra.Hash)
	}
	if header.Hash() == extra.Hash {
		t.Fatalf("go-ethereum's recomputed hash equals the Prague hash; the defect premise no longer holds")
	}
	if extra.RequestsHash != emptyRequestsHash {
		t.Fatalf("recorded requestsHash %s is not sha256 of no requests", extra.RequestsHash)
	}
	for _, b := range fixtureChain(false, 2) {
		if b.hash != b.header.Hash() {
			t.Fatalf("fixture Cancun hash %s differs from go-ethereum's %s", b.hash, b.header.Hash())
		}
	}
	for _, b := range fixtureChain(true, 2) {
		if b.hash == b.header.Hash() {
			t.Fatalf("fixture Prague block %d hash equals go-ethereum's recomputed hash", b.header.Number)
		}
	}
}

func pragueTestContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func dialFixture(t *testing.T, ctx context.Context, rpc *fixtureRPC) *ethclient.Client {
	t.Helper()
	client, err := ethclient.DialContext(ctx, rpc.start())
	if err != nil {
		t.Fatalf("fixture: dial: %v", err)
	}
	t.Cleanup(client.Close)
	return client
}

func shapeName(prague bool) string {
	if prague {
		return "prague"
	}
	return "cancun"
}

// ---------------------------------------------------------------- FrostChain

type frostViewFixture struct {
	rpc       *fixtureRPC
	blocks    []fixtureBlock
	chain     *FrostChain
	epochView [4]byte
	approved  binding.FrostWalletRegistryWallet
}

// newFrostViewFixture serves a FrostWalletRegistry and Bridge pair to
// NewFrostChain. With approved set, the epoch has an approved wallet whose
// approval block is pragueTestApprovalBlock and whose ResultApproved log
// carries approvalLogHash as its block hash (zero: the RPC's true hash).
func newFrostViewFixture(t *testing.T, ctx context.Context, prague, approved bool, approvalLogHash common.Hash) *frostViewFixture {
	t.Helper()
	registryABI, err := binding.FrostWalletRegistryMetaData.GetAbi()
	if err != nil {
		t.Fatal(err)
	}
	registry := common.HexToAddress("0x00000000000000000000000000000000000f2051")
	bridge := common.HexToAddress("0x00000000000000000000000000000000000b81d6")
	validator := common.HexToAddress("0x0000000000000000000000000000000000000a11")
	pool := common.HexToAddress("0x0000000000000000000000000000000000000b01")
	fx := &frostViewFixture{blocks: fixtureChain(prague, pragueTestHead), epochView: selectorOf(t, registryABI, "epochView")}
	fx.rpc = newFixtureRPC(t, fx.blocks)
	address := func(a common.Address) contractRead {
		return func([]byte, fixtureBlock) ([]byte, error) { return common.LeftPadBytes(a.Bytes(), 32), nil }
	}
	fx.rpc.serve(registry, selectorOf(t, registryABI, "walletOwner"), address(bridge))
	fx.rpc.serve(registry, selectorOf(t, registryABI, "validator"), address(validator))
	fx.rpc.serve(registry, selectorOf(t, registryABI, "sortitionPool"), address(pool))
	var frostRegistry [4]byte
	copy(frostRegistry[:], crypto.Keccak256([]byte("frostRegistry()")))
	fx.rpc.serve(bridge, frostRegistry, address(registry))

	snapshot := binding.FrostWalletRegistryEpochSnapshot{State: 2, Epoch: pragueTestEpoch, SubmittedAt: 0, ResultDeadline: 100}
	snapshot.Approved.Descriptor.ChainId = new(big.Int)
	if approved {
		// The epoch state value only matters to the adapter when it is 3
		// (a submitted result to decode); an approved epoch is not 3.
		snapshot.State = 0
		snapshot.ApprovedId = crypto.Keccak256Hash([]byte("approved wallet"))
		fx.approved = binding.FrostWalletRegistryWallet{
			Descriptor:     binding.FrostTypesDescriptor{ChainId: big.NewInt(31337), Registry: registry, Epoch: pragueTestEpoch},
			ResultHash:     crypto.Keccak256Hash([]byte("approved result")),
			DescriptorHash: crypto.Keccak256Hash([]byte("approved descriptor")),
			ApprovalBlock:  pragueTestApprovalBlock, Deadline: 100, State: 1,
		}
		snapshot.Approved = fx.approved
		event := registryABI.Events["ResultApproved"]
		data, err := event.Inputs.NonIndexed().Pack(fx.approved.DescriptorHash)
		if err != nil {
			t.Fatal(err)
		}
		if approvalLogHash == (common.Hash{}) {
			approvalLogHash = fx.blocks[pragueTestApprovalBlock].hash
		}
		fx.rpc.addLog(types.Log{
			Address: registry, Data: data, BlockNumber: pragueTestApprovalBlock, BlockHash: approvalLogHash,
			Topics: []common.Hash{event.ID, common.BigToHash(new(big.Int).SetUint64(pragueTestEpoch)), fx.approved.ResultHash, snapshot.ApprovedId},
			TxHash: crypto.Keccak256Hash([]byte("approval tx")),
		})
	}
	fx.rpc.serve(registry, fx.epochView, func(input []byte, at fixtureBlock) ([]byte, error) {
		s := snapshot
		// The contract reports the block it executed at.
		s.BlockNumber = at.header.Number.Uint64()
		return packOutputs(registryABI, "epochView", s)
	})
	fx.chain, err = NewFrostChain(ctx, dialFixture(t, ctx, fx.rpc), pragueTestKey(t), registry, bridge)
	if err != nil {
		t.Fatalf("NewFrostChain over the fixture: %v", err)
	}
	return fx
}

// requireEpochViewAtHash: every EpochView eth_call names its block by the
// RPC's hash want, not by number or tag and not by a recomputed hash.
func requireEpochViewAtHash(t *testing.T, fx *frostViewFixture, want fixtureBlock) {
	t.Helper()
	calls := fx.rpc.callsTo(fx.epochView)
	if len(calls) == 0 {
		t.Fatalf("EpochView eth_call never made")
	}
	for _, c := range calls {
		if !c.byHash {
			t.Fatalf("EpochView eth_call made at block %s, want block hash %s (the RPC's hash)", c.block, want.hash)
		}
		if c.hash != want.hash {
			t.Fatalf("EpochView eth_call made at block hash %s, want the RPC's hash %s (go-ethereum v1.13.15 recomputes %s)", c.hash, want.hash, want.header.Hash())
		}
	}
}

// Refs FINDING-FROST-PRAGUE-HEADER-HASH site frost.go ~141.
// Invariant: View reads EpochView at the head block hash the RPC returned and
// reports that hash as HeadHash.
func TestFrostViewReadsAtTheRPCBlockHash(t *testing.T) {
	t.Parallel()
	for _, prague := range []bool{true, false} {
		prague := prague
		t.Run(shapeName(prague), func(t *testing.T) {
			t.Parallel()
			ctx := pragueTestContext(t)
			fx := newFrostViewFixture(t, ctx, prague, false, common.Hash{})
			head := fx.blocks[pragueTestHead]
			view, err := fx.chain.View(ctx, pragueTestEpoch)
			requireEpochViewAtHash(t, fx, head)
			if err != nil {
				t.Fatalf("View failed: %v", err)
			}
			if view.Head != pragueTestHead || view.HeadHash != head.hash {
				t.Fatalf("View head = %d %s, want %d and the RPC's hash %s", view.Head, common.Hash(view.HeadHash), pragueTestHead, head.hash)
			}
			if view.State != 2 || view.Epoch != pragueTestEpoch || view.ApprovedID != ([32]byte{}) {
				t.Fatalf("View snapshot fields changed: state=%d epoch=%d", view.State, view.Epoch)
			}
		})
	}
}

// Refs FINDING-FROST-PRAGUE-HEADER-HASH site frost.go ~174.
// Invariant: ApprovalHash is the RPC's hash of the approval block, so an
// approval log from the canonical approval block matches it, and a log from
// any other block (including one differing only in requestsHash) is still
// quarantined.
func TestFrostViewApprovalHashIsTheRPCHash(t *testing.T) {
	t.Parallel()
	for _, prague := range []bool{true, false} {
		prague := prague
		t.Run(shapeName(prague)+"/canonical approval log", func(t *testing.T) {
			t.Parallel()
			ctx := pragueTestContext(t)
			fx := newFrostViewFixture(t, ctx, prague, true, common.Hash{})
			approval := fx.blocks[pragueTestApprovalBlock]
			view, err := fx.chain.View(ctx, pragueTestEpoch)
			requireEpochViewAtHash(t, fx, fx.blocks[pragueTestHead])
			if errors.Is(err, dkg.ErrQuarantined) {
				t.Fatalf("approved view quarantined although the approval log is from the canonical approval block %s: %v", approval.hash, err)
			}
			if err != nil {
				t.Fatalf("View failed: %v", err)
			}
			if view.ApprovalHash != approval.hash {
				t.Fatalf("ApprovalHash = %s, want the RPC's hash of block %d, %s", common.Hash(view.ApprovalHash), pragueTestApprovalBlock, approval.hash)
			}
			if view.ApprovedID == ([32]byte{}) || view.Approved.DescriptorHash != fx.approved.DescriptorHash || view.Approved.ApprovalBlock != pragueTestApprovalBlock {
				t.Fatalf("approved wallet fields changed")
			}
		})
		siblings := []struct {
			name         string
			requestsOnly bool
		}{{"approval log from another block", false}}
		if prague {
			siblings = append(siblings, struct {
				name         string
				requestsOnly bool
			}{"approval log from a block differing only in requestsHash", true})
		}
		for _, s := range siblings {
			s := s
			t.Run(shapeName(prague)+"/"+s.name, func(t *testing.T) {
				t.Parallel()
				ctx := pragueTestContext(t)
				other := siblingBlock(fixtureChain(prague, pragueTestHead)[pragueTestApprovalBlock], s.requestsOnly)
				fx := newFrostViewFixture(t, ctx, prague, true, other.hash)
				_, err := fx.chain.View(ctx, pragueTestEpoch)
				requireEpochViewAtHash(t, fx, fx.blocks[pragueTestHead])
				if !errors.Is(err, dkg.ErrQuarantined) {
					t.Fatalf("approval log from block %s accepted for canonical block %s: err=%v", other.hash, fx.blocks[pragueTestApprovalBlock].hash, err)
				}
			})
		}
	}
}

// Refs FINDING-FROST-PRAGUE-HEADER-HASH site frost.go ~195.
// Invariant: the head-stability recheck compares the RPC's hashes, so a reorg
// at the read height during the read is quarantined, including one that
// changes only requestsHash (identical go-ethereum v1.13.15 hashes).
func TestFrostViewHeadRecheckComparesRPCHashes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                 string
		prague, requestsOnly bool
	}{
		{"prague/reorg differing only in requestsHash", true, true},
		{"prague/reorg", true, false},
		{"cancun/reorg", false, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := pragueTestContext(t)
			fx := newFrostViewFixture(t, ctx, c.prague, false, common.Hash{})
			head := fx.blocks[pragueTestHead]
			reorged := siblingBlock(head, c.requestsOnly)
			fx.rpc.onCall(fx.epochView, func(f *fixtureRPC) { f.replace(reorged) })
			view, err := fx.chain.View(ctx, pragueTestEpoch)
			requireEpochViewAtHash(t, fx, head)
			if !errors.Is(err, dkg.ErrQuarantined) {
				t.Fatalf("reorg at height %d from %s to %s during the read not detected: view head hash %s, err=%v", pragueTestHead, head.hash, reorged.hash, common.Hash(view.HeadHash), err)
			}
		})
	}
}

// ------------------------------------------------------ FrostRedemptionChain

type frostRedemptionFixture struct {
	rpc         *fixtureRPC
	blocks      []fixtureBlock
	chain       *FrostRedemptionChain
	reservation [32]byte
	read        [4]byte
}

// newFrostRedemptionFixture serves one admitted reservation whose admission
// block is pragueTestApprovalBlock and whose FrostReservationAdmitted log
// carries admissionLogHash as its block hash (zero: the RPC's true hash).
func newFrostRedemptionFixture(t *testing.T, ctx context.Context, prague bool, admissionLogHash common.Hash) *frostRedemptionFixture {
	t.Helper()
	bridgeABI, err := frostbridgeabi.FrostBridgeMetaData.GetAbi()
	if err != nil {
		t.Fatal(err)
	}
	bridge := common.HexToAddress("0x00000000000000000000000000000000000b81d7")
	fx := &frostRedemptionFixture{blocks: fixtureChain(prague, pragueTestHead), reservation: crypto.Keccak256Hash([]byte("reservation")), read: selectorOf(t, bridgeABI, "frostReservation")}
	fx.rpc = newFixtureRPC(t, fx.blocks)
	wallet := crypto.Keccak256Hash([]byte("wallet"))
	transaction := crypto.Keccak256Hash([]byte("bitcoin transaction"))
	intent := frostbridgeabi.FrostRedemptionLedgerIntent{
		Certificate:   frostbridgeabi.FrostReservationCertificateReservation{WalletId: wallet, Generation: 1, StateVersion: 1, IntentCommitment: crypto.Keccak256Hash([]byte("intent")), ValidUntilBlock: 100},
		AdmittedBlock: pragueTestApprovalBlock, TransactionHash: transaction, State: 1,
	}
	fx.rpc.serve(bridge, fx.read, func([]byte, fixtureBlock) ([]byte, error) {
		return packOutputs(bridgeABI, "frostReservation", intent)
	})
	event := bridgeABI.Events["FrostReservationAdmitted"]
	data, err := event.Inputs.NonIndexed().Pack(intent.Certificate.IntentCommitment, transaction, uint64(1), uint64(1))
	if err != nil {
		t.Fatal(err)
	}
	if admissionLogHash == (common.Hash{}) {
		admissionLogHash = fx.blocks[pragueTestApprovalBlock].hash
	}
	fx.rpc.addLog(types.Log{
		Address: bridge, Data: data, BlockNumber: pragueTestApprovalBlock, BlockHash: admissionLogHash,
		Topics: []common.Hash{event.ID, fx.reservation, wallet}, TxHash: crypto.Keccak256Hash([]byte("admission tx")),
	})
	fx.chain, err = NewFrostRedemptionChain(ctx, dialFixture(t, ctx, fx.rpc), nil, bridge)
	if err != nil {
		t.Fatalf("NewFrostRedemptionChain over the fixture: %v", err)
	}
	return fx
}

// Refs FINDING-FROST-PRAGUE-HEADER-HASH sites frost_redemption.go ~72, ~80.
// Invariant: redemption Head and HeaderHash return the RPC's hashes, so an
// admission log from the canonical block equals the canonical hash the
// signing executor compares it with (executor.go: BlockHash ==
// CanonicalBlockHash), a log from another block does not, and a reorg at the
// head during a read is ErrInconsistentRead.
func TestFrostRedemptionReadsUseTheRPCBlockHash(t *testing.T) {
	t.Parallel()
	for _, prague := range []bool{true, false} {
		prague := prague
		t.Run(shapeName(prague)+"/head and header hashes", func(t *testing.T) {
			t.Parallel()
			ctx := pragueTestContext(t)
			fx := newFrostRedemptionFixture(t, ctx, prague, common.Hash{})
			head, err := fx.chain.Head(ctx)
			if err != nil {
				t.Fatalf("Head failed: %v", err)
			}
			if want := fx.blocks[pragueTestHead]; head.Number != pragueTestHead || head.Hash != want.hash {
				t.Fatalf("Head = %d %s, want %d and the RPC's hash %s", head.Number, common.Hash(head.Hash), pragueTestHead, want.hash)
			}
			for _, n := range []uint64{0, pragueTestApprovalBlock, pragueTestHead} {
				got, err := fx.chain.HeaderHash(ctx, n)
				if err != nil {
					t.Fatalf("HeaderHash(%d) failed: %v", n, err)
				}
				if got != fx.blocks[n].hash {
					t.Fatalf("HeaderHash(%d) = %s, want the RPC's hash %s", n, common.Hash(got), fx.blocks[n].hash)
				}
			}
		})
		t.Run(shapeName(prague)+"/canonical admission", func(t *testing.T) {
			t.Parallel()
			ctx := pragueTestContext(t)
			fx := newFrostRedemptionFixture(t, ctx, prague, common.Hash{})
			view, err := redemption.ChainReservations{Chain: fx.chain}.ReadReservation(ctx, fx.reservation)
			if err != nil {
				t.Fatalf("ReadReservation failed: %v", err)
			}
			admission := fx.blocks[pragueTestApprovalBlock]
			if view.CanonicalBlockHash != admission.hash {
				t.Fatalf("canonical hash of admission block %d = %s, want the RPC's hash %s", pragueTestApprovalBlock, common.Hash(view.CanonicalBlockHash), admission.hash)
			}
			if view.BlockHash != view.CanonicalBlockHash {
				t.Fatalf("canonical admission refused: log block hash %s != canonical %s", common.Hash(view.BlockHash), common.Hash(view.CanonicalBlockHash))
			}
			if view.Head != pragueTestHead || view.HeadHash != fx.blocks[pragueTestHead].hash || view.Block != pragueTestApprovalBlock {
				t.Fatalf("reservation view head %d %s block %d, want %d %s %d", view.Head, common.Hash(view.HeadHash), view.Block, pragueTestHead, fx.blocks[pragueTestHead].hash, pragueTestApprovalBlock)
			}
		})
		t.Run(shapeName(prague)+"/admission from another block", func(t *testing.T) {
			t.Parallel()
			ctx := pragueTestContext(t)
			other := siblingBlock(fixtureChain(prague, pragueTestHead)[pragueTestApprovalBlock], false)
			fx := newFrostRedemptionFixture(t, ctx, prague, other.hash)
			view, err := redemption.ChainReservations{Chain: fx.chain}.ReadReservation(ctx, fx.reservation)
			if err != nil {
				t.Fatalf("ReadReservation failed: %v", err)
			}
			if view.BlockHash == view.CanonicalBlockHash {
				t.Fatalf("admission log from block %s reads as canonical", other.hash)
			}
			if view.CanonicalBlockHash != fx.blocks[pragueTestApprovalBlock].hash {
				t.Fatalf("canonical hash of admission block %d = %s, want the RPC's hash %s", pragueTestApprovalBlock, common.Hash(view.CanonicalBlockHash), fx.blocks[pragueTestApprovalBlock].hash)
			}
		})
		t.Run(shapeName(prague)+"/reorg at the head during a read", func(t *testing.T) {
			t.Parallel()
			ctx := pragueTestContext(t)
			fx := newFrostRedemptionFixture(t, ctx, prague, common.Hash{})
			// On Prague, the reorged head differs only in requestsHash.
			reorged := siblingBlock(fx.blocks[pragueTestHead], prague)
			fx.rpc.onCall(fx.read, func(f *fixtureRPC) { f.replace(reorged) })
			_, err := redemption.ChainReservations{Chain: fx.chain}.ReadReservation(ctx, fx.reservation)
			if !errors.Is(err, redemption.ErrInconsistentRead) {
				t.Fatalf("reorg at height %d from %s to %s during the read not detected: err=%v", pragueTestHead, fx.blocks[pragueTestHead].hash, reorged.hash, err)
			}
		})
	}
}

// pragueTestKey is a throwaway operator key; FrostChain reads never use it.
func pragueTestKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}
