package tbtc_test

// The upgraded-Bridge half of tuxxy/keep-core#73.
//
// Invariant: a FROST-unconfigured client against an upgraded Bridge never
// broadcasts a moving-funds transaction the Bridge refuses. Here the source's
// commitment is an ECDSA-to-FROST drain (C-06): its targets are FROST wallet
// labels, and the Bridge accepts only P2TR outputs to their descriptor keys
// ("FROST drain mixes schemes" otherwise). The client may refuse the action
// (fail closed for this call, D4(c)); if it broadcasts, the Bridge must accept
// the proof of what it broadcast. A P2WPKH output to a FROST label would also
// pay a script nobody holds a key for.
//
// The Bridge is the real tbtc-v2 integration build (FROST extension, C-06 to
// C-10), compiled from git at LEGACY_UPGRADED_COMMIT. The FROST target is
// created through FrostRegistryFixture (tbtc-v2 test contract: registry
// callbacks only, no DKG) and the source through the legacy registry FIXTURE.

import (
	"bytes"
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	tbtcabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	"github.com/keep-network/keep-core/pkg/tbtc"
	"github.com/keep-network/keep-core/pkg/tbtcpg"
	"github.com/keep-network/keep-core/pkg/tecdsa"
)

type frostTarget struct {
	q     [32]byte
	id    [32]byte
	label [20]byte
}

// frostABI merges the FROST extension interfaces served at the Bridge proxy.
func (w *legacyWorld) frostABI() abi.ABI {
	w.t.Helper()
	merged := abi.ABI{Methods: map[string]abi.Method{}, Events: map[string]abi.Event{}, Errors: map[string]abi.Error{}}
	for _, name := range []string{"IFrostBridge", "IFrostFundingBridge", "IFrostExposureBridge", "IFrostMovementBridge"} {
		artifact := w.baseline.artifact("contracts/frost/"+name+".sol", name)
		parsed, err := abi.JSON(bytes.NewReader(artifact.ABI))
		if err != nil {
			w.t.Fatal(err)
		}
		for key, method := range parsed.Methods {
			merged.Methods[key] = method
		}
	}
	return merged
}

// setUpFrostDrain configures the upgraded Bridge's FROST extension the way
// tbtc-v2's movementWorld does (C-10 test ceilings, D-10 values OPEN), creates
// one Live FROST wallet and enables the drain.
func setUpFrostDrain(target *frostTarget) func(*legacyWorld) {
	return func(w *legacyWorld) {
		t := w.t
		t.Helper()
		admin := crypto.PubkeyToAddress(w.adminKey.PublicKey)
		frost := bind.NewBoundContract(w.bridge, w.frostABI(), w.admin, w.admin, w.admin)
		call := func(what string, contract *bind.BoundContract, method string, args ...interface{}) {
			w.send(what, func(o *bind.TransactOpts) (*types.Transaction, error) {
				return contract.Transact(o, method, args...)
			})
		}
		libraries := map[string]common.Address{}

		registryAddress, registryABI := w.deployBaseline(libraries, "contracts/test/FrostRegistryFixture.sol", "FrostRegistryFixture", w.bridge)
		registry := bind.NewBoundContract(registryAddress, registryABI, w.admin, w.admin, w.admin)
		call("FROST registry", frost, "configureFrostRegistry", registryAddress)

		verifier, _ := w.deployBaseline(libraries, "contracts/frost/FrostVerifier.sol", "FrostVerifier")
		code, err := w.admin.CodeAt(context.Background(), verifier, nil)
		if err != nil {
			t.Fatal(err)
		}
		call("FROST funding", frost, "configureFrostFunding", verifier, crypto.Keccak256Hash(code),
			crypto.Keccak256Hash([]byte("UNQUALIFIED legacy-unchanged local test")),
			crypto.Keccak256Hash([]byte("legacy-unchanged synthetic regtest domain")))

		exposureAddress, exposureABI := w.deployBaseline(libraries, "contracts/frost/FrostExposureController.sol", "FrostExposureController", w.bridge, admin)
		exposure := bind.NewBoundContract(exposureAddress, exposureABI, w.admin, w.admin, w.admin)
		maxUint64 := new(big.Int).SetUint64(^uint64(0))
		call("C-10 budget", exposure, "openBudgetPeriod", maxUint64.Uint64(), maxUint64.Uint64(),
			crypto.Keccak256Hash([]byte("C-10 test budget; D-10 OPEN")))
		call("C-10 controller", frost, "installFrostExposureController", exposureAddress)
		call("FROST gates", frost, "setFrostFundingGates", true, true)

		// One FROST wallet, as movementWorld builds them.
		key, err := crypto.ToECDSA(common.LeftPadBytes(big.NewInt(1000).Bytes(), 32))
		if err != nil {
			t.Fatal(err)
		}
		copy(target.q[:], crypto.CompressPubkey(&key.PublicKey)[1:])
		head, err := w.admin.BlockNumber(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		descriptorType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "scheme", Type: "uint8"}, {Name: "profile", Type: "uint8"},
			{Name: "chainId", Type: "uint256"}, {Name: "registry", Type: "address"},
			{Name: "epoch", Type: "uint64"}, {Name: "members", Type: "uint32[]"},
			{Name: "operators", Type: "address[]"}, {Name: "threshold", Type: "uint16"},
			{Name: "snowfallDescriptor", Type: "bytes32"}, {Name: "outputKey", Type: "bytes32"},
		})
		if err != nil {
			t.Fatal(err)
		}
		descriptor := struct {
			Scheme             uint8
			Profile            uint8
			ChainId            *big.Int
			Registry           common.Address
			Epoch              uint64
			Members            []uint32
			Operators          []common.Address
			Threshold          uint16
			SnowfallDescriptor [32]byte
			OutputKey          [32]byte
		}{2, 1, w.chainID, registryAddress, head, []uint32{1, 2, 3},
			[]common.Address{w.operator, admin, common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")},
			2, crypto.Keccak256Hash([]byte("legacy-unchanged FROST target")), target.q}
		encoded, err := abi.Arguments{{Type: descriptorType}}.Pack(descriptor)
		if err != nil {
			t.Fatal(err)
		}
		bytes32Type, _ := abi.NewType("bytes32", "", nil)
		tagged, err := abi.Arguments{{Type: bytes32Type}, {Type: descriptorType}}.Pack(
			crypto.Keccak256Hash([]byte("tbtc-v2/frost-descriptor/v1")), descriptor)
		if err != nil {
			t.Fatal(err)
		}
		descriptorHash := crypto.Keccak256Hash(tagged)
		target.id = crypto.Keccak256Hash(crypto.Keccak256([]byte("tbtc-v2/frost-wallet-id/v1")), target.q[:])
		copy(target.label[:], target.id[:20])
		call("FROST wallet created", registry, "created", encoded, head+100000)
		call("FROST wallet ready", registry, "ready", target.id, descriptorHash)
		call("FROST drain enabled", frost, "setFrostDrainEnabled", true)
	}
}

// TestLegacyUnchanged_UpgradedBridgeDrain: see the file comment.
// Refs tuxxy/keep-core#73.
func TestLegacyUnchanged_UpgradedBridgeDrain(t *testing.T) {
	for _, encoding := range legacyEncodings {
		t.Run(encoding.String(), func(t *testing.T) {
			legacyUpgradedDrainRound(t, encoding)
		})
	}
}

func legacyUpgradedDrainRound(t *testing.T, encoding legacyRevertEncoding) {
	env := legacyEnv(t)
	dir, commit := upgradedBuild(t)
	target := &frostTarget{}
	w := newLegacyWorldOn(t, env, openTbtcBuild(t, env, dir, commit), setUpFrostDrain(target))

	// The drain commitment, submitted by a wallet member that knows FROST.
	operatorKey, err := crypto.HexToECDSA(legacyOperatorKey)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := bind.NewKeyedTransactorWithChainID(operatorKey, w.chainID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := w.legacy.SubmitMovingFundsCommitment(opts, w.source.pkh, legacyUTXO(w.main),
		[]uint32{1}, big.NewInt(1), [][20]byte{target.label})
	if err != nil {
		t.Fatalf("setup: drain commitment: %v", err)
	}
	if receipt, err := bind.WaitMined(context.Background(), w.admin, tx); err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("setup: drain commitment not mined: %v", err)
	}

	recorder := newLegacyRecorder(t, w.anvilURL, encoding)
	tbtcChain, err := w.connect(recorder)
	if err != nil {
		t.Fatalf("startup against the upgraded Bridge: %v", err)
	}
	stop := w.mineInBackground()
	defer stop()

	operator := chain.Address(w.operator.Hex())
	var proposal *tbtc.MovingFundsProposal
	proposeErr := legacyBounded(t, "moving-funds proposal", func() error {
		result, ok, err := tbtcpg.NewMovingFundsTask(tbtcChain, w.btc).Run(&tbtc.CoordinationProposalRequest{
			WalletPublicKeyHash: w.source.pkh,
			WalletOperators:     []chain.Address{operator},
			ExecutingOperator:   operator,
			ActionsChecklist:    []tbtc.WalletActionType{tbtc.ActionMovingFunds},
		})
		if err == nil && ok {
			proposal = result.(*tbtc.MovingFundsProposal)
		}
		return err
	})
	if proposal == nil {
		t.Logf("upgraded Bridge drain: the client made no proposal (%v); nothing was broadcast", proposeErr)
	} else {
		start, err := w.admin.BlockNumber(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		sign := tbtc.LegacySignBatchFn(func(_ context.Context, messages []*big.Int, _ uint64) ([]*tecdsa.Signature, error) {
			return legacySignatures(w.source.key, messages)
		})
		executeErr := legacyBounded(t, "moving-funds execution", func() error {
			return tbtc.ExecuteMovingFundsActionForTest(tbtcChain, w.btc, &w.source.key.PublicKey,
				[]chain.Address{operator}, sign, proposal, start, w.waitForBlock)
		})
		if executeErr != nil {
			t.Logf("upgraded Bridge drain: the client refused the action (%v)", executeErr)
		}
	}
	stop()

	for _, broadcast := range w.btc.Broadcasts() {
		for i, output := range broadcast.Outputs {
			if legacyPaysTo(output.PublicKeyScript, target.label) {
				t.Errorf("upgraded Bridge drain: output %d pays a legacy script to FROST target %x", i, target.label)
			}
		}
		// The Bridge's own verdict on what the client broadcast.
		w.btc.mine(t, broadcast)
		_, err := w.legacy.SubmitMovingFundsProof(w.opts(), legacyTxInfo(broadcast),
			w.btc.proof(t, broadcast.Hash()), legacyUTXO(w.main), w.source.pkh)
		if err != nil {
			t.Errorf("upgraded Bridge drain: the Bridge refuses the moving-funds transaction the client broadcast: %v", err)
		} else {
			t.Logf("upgraded Bridge drain: the Bridge accepted the broadcast transaction %s",
				broadcast.Hash().Hex(bitcoin.ReversedByteOrder))
		}
	}
	if n := len(w.btc.Broadcasts()); n > 1 {
		t.Errorf("upgraded Bridge drain: %d moving-funds transactions broadcast, want at most 1", n)
	}
}

func legacyUTXO(utxo *bitcoin.UnspentTransactionOutput) tbtcabi.BitcoinTxUTXO {
	return tbtcabi.BitcoinTxUTXO{
		TxHash:        utxo.Outpoint.TransactionHash,
		TxOutputIndex: utxo.Outpoint.OutputIndex,
		TxOutputValue: uint64(utxo.Value),
	}
}
