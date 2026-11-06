package tbtc

// K-09 legacy ECDSA signers (remediation batch 9, tuxxy/keep-core#40).
//
// FIXTURE: the legacy source wallets of the K-09 run are tECDSA groups made
// from base's pre-generated 3-of-5 key shares (pkg/internal/tecdsatest), as
// base's own tests use them, instead of a live legacy DKG (tss-lib
// pre-parameters take minutes per seat; K09-HARNESS-PLAN-v2 section 2). The
// shares are seeded into each node's base wallet storage as base's DKG does
// right after key generation (registerSigner -> walletStorage.saveSigner),
// and the node restores them at startup through walletStorage.loadSigners.
//
// A second legacy wallet needs a second key. The tecdsatest set is the only
// one, so group "tweak" d (d > 0) adds the public constant d to every share
// (and d*G to every public share and to the group key): the shares then form
// a valid sharing of the group secret plus d. Nothing here reads, prints or
// writes a share anywhere but into a node's own storage, and the group
// secret is never formed.
//
// These helpers add no behaviour to the node.

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/bnb-chain/tss-lib/crypto"
	"github.com/bnb-chain/tss-lib/ecdsa/keygen"

	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain"
	"github.com/keep-network/keep-core/pkg/chain/local_v1"
	"github.com/keep-network/keep-core/pkg/internal/tecdsatest"
	"github.com/keep-network/keep-core/pkg/net/local"
	"github.com/keep-network/keep-core/pkg/operator"
	"github.com/keep-network/keep-core/pkg/protocol/group"
	"github.com/keep-network/keep-core/pkg/storage"
	"github.com/keep-network/keep-core/pkg/tecdsa"
)

// K09LegacyGroupSize and K09LegacyHonestThreshold are the tecdsatest group's
// shape (3-of-5). A node signing with these shares needs these group
// parameters (base reads them from the EcdsaDkgValidator contract).
const (
	K09LegacyGroupSize       = 5
	K09LegacyGroupQuorum     = 4
	K09LegacyHonestThreshold = 3
)

// k09LegacyShares returns the tecdsatest shares of group tweak d.
func k09LegacyShares(d int64) ([]keygen.LocalPartySaveData, error) {
	shares, err := tecdsatest.LoadPrivateKeyShareTestFixtures(K09LegacyGroupSize)
	if err != nil {
		return nil, err
	}
	if d == 0 {
		return shares, nil
	}
	if d < 0 {
		return nil, fmt.Errorf("negative group tweak")
	}
	delta := big.NewInt(d)
	for i := range shares {
		s := &shares[i]
		curve := s.ECDSAPub.Curve()
		shift := crypto.ScalarBaseMult(curve, delta)
		s.Xi = new(big.Int).Mod(new(big.Int).Add(s.Xi, delta), curve.Params().N)
		for j := range s.BigXj {
			if s.BigXj[j], err = s.BigXj[j].Add(shift); err != nil {
				return nil, err
			}
		}
		if s.ECDSAPub, err = s.ECDSAPub.Add(shift); err != nil {
			return nil, err
		}
	}
	return shares, nil
}

// K09LegacyGroupPublicKey is the public key of group tweak d.
func K09LegacyGroupPublicKey(d int64) (*ecdsa.PublicKey, error) {
	shares, err := k09LegacyShares(d)
	if err != nil {
		return nil, err
	}
	return tecdsa.NewPrivateKeyShare(shares[0]).PublicKey(), nil
}

// K09SeedLegacySigners writes into the base wallet storage under storageDir
// (as cmd/start.go's storage.Initialize lays it out) the signers of group
// tweak d whose seat belongs to local. operators holds the operator of each
// seat, seat i+1 at index i. It returns the number of signers written.
func K09SeedLegacySigners(storageDir, password string, d int64, operators []chain.Address, local chain.Address) (int, error) {
	if len(operators) != K09LegacyGroupSize {
		return 0, fmt.Errorf("the group has %d seats, not %d", K09LegacyGroupSize, len(operators))
	}
	shares, err := k09LegacyShares(d)
	if err != nil {
		return 0, err
	}
	disk, err := storage.Initialize(storage.Config{Dir: storageDir}, password)
	if err != nil {
		return 0, err
	}
	keyStore, err := disk.InitializeKeyStorePersistence("tbtc")
	if err != nil {
		return 0, err
	}
	walletStorage := newWalletStorage(keyStore)
	written := 0
	for i, op := range operators {
		if op != local {
			continue
		}
		share := tecdsa.NewPrivateKeyShare(shares[i])
		s := &signer{
			wallet:                  wallet{publicKey: share.PublicKey(), signingGroupOperators: operators},
			signingGroupMemberIndex: group.MemberIndex(i + 1),
			privateKeyShare:         share,
		}
		if err := walletStorage.saveSigner(s); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

// K09SignWithLegacyGroup signs the hashes with group tweak d through base's
// signing executor, in this process, over a local chain and network (as
// setupSigningExecutor does). For harness setup only (funding a legacy
// source's main); never for a transaction under test.
func K09SignWithLegacyGroup(t *testing.T, d int64, hashes []*big.Int) []*tecdsa.Signature {
	t.Helper()
	parameters := &GroupParameters{GroupSize: K09LegacyGroupSize, GroupQuorum: K09LegacyGroupQuorum, HonestThreshold: K09LegacyHonestThreshold}
	operatorPrivateKey, operatorPublicKey, err := operator.GenerateKeyPair(local_v1.DefaultCurve)
	if err != nil {
		t.Fatal(err)
	}
	localChain := ConnectWithKey(operatorPrivateKey)
	localProvider := local.ConnectWithKey(operatorPublicKey)
	operatorAddress, err := localChain.Signing().PublicKeyToAddress(operatorPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	operators := make([]chain.Address, K09LegacyGroupSize)
	for i := range operators {
		operators[i] = operatorAddress
	}
	shares, err := k09LegacyShares(d)
	if err != nil {
		t.Fatal(err)
	}
	signers := make([]*signer, len(shares))
	for i := range shares {
		share := tecdsa.NewPrivateKeyShare(shares[i])
		signers[i] = &signer{
			wallet:                  wallet{publicKey: share.PublicKey(), signingGroupOperators: operators},
			signingGroupMemberIndex: group.MemberIndex(i + 1),
			privateKeyShare:         share,
		}
	}
	walletID, err := localChain.CalculateWalletID(signers[0].wallet.publicKey)
	if err != nil {
		t.Fatal(err)
	}
	localChain.setWallet(bitcoin.PublicKeyHash(signers[0].wallet.publicKey), &WalletChainData{EcdsaWalletID: walletID, State: StateLive})
	node, err := newNode(parameters, localChain, newLocalBitcoinChain(), localProvider,
		createMockKeyStorePersistence(t, signers...), &mockPersistenceHandle{}, newTestScheduler(t),
		&mockCoordinationProposalGenerator{}, Config{PreParamsPoolSize: 1, PreParamsGenerationTimeout: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	executor, ok, err := node.getSigningExecutor(signers[0].wallet.publicKey)
	if err != nil || !ok {
		t.Fatalf("setup: no signing executor for the legacy group: %v", err)
	}
	executor.signingAttemptsLimit *= 8
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	signatures, err := executor.signBatch(ctx, hashes, 0)
	if err != nil {
		t.Fatalf("setup: legacy group signing: %v", err)
	}
	return signatures
}
