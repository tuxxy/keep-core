package deposit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/txscript"
	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// The vectors are tbtc-v2 solidity/test/data/frost/deposit-profile-v1.json at
// the pinned C-05 revision, copied byte for byte. The contract tests check the
// same file against FrostDepositProfile.sol, so a match here is the Go side of
// the cross-language template check.
const vectorSHA256 = "27fd67242955d39843eea02c24939750b70d384204bb6b9620b13b1d93f21a41"

type vector struct {
	Name  string
	Terms struct {
		ChainID        string `json:"chainId"`
		Bridge         string
		BitcoinGenesis string
		FullWalletID   string `json:"fullWalletId"`
		DescriptorHash string
		Depositor      string
		Vault          string
		Blinding       string
		Amount         string
		RefundKey      string
		RefundLocktime uint32
		ExtraData      string
	}
	OutputKey string
	Expected  map[string]string
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func loadVectors(t *testing.T) []vector {
	t.Helper()
	raw, err := os.ReadFile("testdata/deposit-profile-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(raw); hex.EncodeToString(sum[:]) != vectorSHA256 {
		t.Fatal("deposit profile vectors changed; re-pin them from tbtc-v2")
	}
	var out []vector
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (v vector) terms(t *testing.T) Terms {
	chain, ok := new(big.Int).SetString(v.Terms.ChainID, 10)
	if !ok {
		t.Fatal("chain id")
	}
	amount, err := strconv.ParseUint(v.Terms.Amount, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	out := Terms{ChainID: chain, Amount: amount, RefundLocktime: v.Terms.RefundLocktime}
	copy(out.Bridge[:], unhex(t, v.Terms.Bridge))
	copy(out.BitcoinGenesis[:], unhex(t, v.Terms.BitcoinGenesis))
	copy(out.WalletID[:], unhex(t, v.Terms.FullWalletID))
	copy(out.DescriptorHash[:], unhex(t, v.Terms.DescriptorHash))
	copy(out.Depositor[:], unhex(t, v.Terms.Depositor))
	copy(out.Vault[:], unhex(t, v.Terms.Vault))
	copy(out.Blinding[:], unhex(t, v.Terms.Blinding))
	copy(out.RefundKey[:], unhex(t, v.Terms.RefundKey))
	copy(out.ExtraData[:], unhex(t, v.Terms.ExtraData))
	return out
}

func TestDepositProfileVectors(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			terms := v.terms(t)
			var key [32]byte
			copy(key[:], unhex(t, v.OutputKey))
			receipt, err := terms.Receipt()
			if err != nil {
				t.Fatal(err)
			}
			check := func(name string, got []byte) {
				t.Helper()
				if !bytes.Equal(got, unhex(t, v.Expected[name])) {
					t.Fatalf("%s: got %x want %s", name, got, v.Expected[name])
				}
			}
			check("receipt", receipt[:])
			wallet, refund := WalletLeaf(receipt, key), RefundLeaf(receipt, terms)
			check("walletLeaf", wallet)
			check("refundLeaf", refund)
			walletHash, refundHash := leafHash(wallet), leafHash(refund)
			check("walletLeafHash", walletHash[:])
			check("refundLeafHash", refundHash[:])
			root := branchHash(walletHash, refundHash)
			check("root", root[:])
			check("internalKey", InternalKey[:])
			check("walletControlEven", controlBlock(refundHash, 0))
			check("walletControlOdd", controlBlock(refundHash, 1))
			check("refundControlEven", controlBlock(walletHash, 0))
			check("refundControlOdd", controlBlock(walletHash, 1))
		})
	}
}

func TestInternalKeyIsBIP341NUMS(t *testing.T) {
	sum := sha256.Sum256(testKey(1).PubKey().SerializeUncompressed())
	if sum != InternalKey {
		t.Fatalf("H = %x, want SHA256(G) = %x", InternalKey, sum)
	}
}

func testKey(b byte) *btcec.PrivateKey {
	k := make([]byte, 32)
	k[31] = b
	key, _ := btcec.PrivKeyFromBytes(k)
	return key
}

func xonly(k *btcec.PrivateKey) (out [32]byte) {
	copy(out[:], schnorr.SerializePubKey(k.PubKey()))
	return out
}

func sampleTerms(refund *btcec.PrivateKey, wallet [32]byte) Terms {
	t := Terms{ChainID: big.NewInt(31337), Amount: 1_000_000, RefundLocktime: 1_900_000_000}
	t.Bridge[0], t.Depositor[0], t.Vault[0] = 1, 2, 3
	t.BitcoinGenesis[0], t.WalletID[0], t.DescriptorHash[0] = 4, 5, 6
	t.Blinding = [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	t.RefundKey = xonly(refund)
	t.WalletID = sha256.Sum256(wallet[:])
	return t
}

// TestBuildCommitsBothLeaves checks the tweak independently through btcd's
// leaf-commitment verifier for both leaves and both control blocks.
func TestBuildCommitsBothLeaves(t *testing.T) {
	wallet := xonly(testKey(7))
	terms := sampleTerms(testKey(9), wallet)
	c, err := Build(terms, wallet)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaf := range []struct {
		script  []byte
		control []byte
	}{{c.WalletLeaf, c.WalletControl}, {c.RefundLeaf, c.RefundControl}} {
		control, err := txscript.ParseControlBlock(leaf.control)
		if err != nil {
			t.Fatal(err)
		}
		if err = txscript.VerifyTaprootLeafCommitment(control, c.OutputKey[:], leaf.script); err != nil {
			t.Fatal(err)
		}
	}
	if c.OutputKey == wallet || c.Root == ([32]byte{}) {
		t.Fatal("deposit output must be the tweaked NUMS key, not Q")
	}
	// Every term changes the output key.
	other := terms
	other.Blinding[7]++
	d, err := Build(other, wallet)
	if err != nil || d.OutputKey == c.OutputKey {
		t.Fatal("blinding must change the deposit output")
	}
	// The wallet leaf names the given Q only.
	e, err := Build(terms, xonly(testKey(8)))
	if err != nil || e.OutputKey == c.OutputKey || bytes.Equal(e.WalletLeaf, c.WalletLeaf) {
		t.Fatal("wallet key must change the wallet leaf")
	}
}

func TestBuildRefusesInvalidTerms(t *testing.T) {
	wallet := xonly(testKey(7))
	good := sampleTerms(testKey(9), wallet)
	for name, mutate := range map[string]func(*Terms){
		"zero chain":       func(t *Terms) { t.ChainID = big.NewInt(0) },
		"nil chain":        func(t *Terms) { t.ChainID = nil },
		"zero amount":      func(t *Terms) { t.Amount = 0 },
		"amount above cap": func(t *Terms) { t.Amount = uint64(bitcoin.MaxSatoshi) + 1 },
		"height locktime":  func(t *Terms) { t.RefundLocktime = 499_999_999 },
		"zero bridge":      func(t *Terms) { t.Bridge = [20]byte{} },
		"zero depositor":   func(t *Terms) { t.Depositor = [20]byte{} },
		"off-curve refund": func(t *Terms) { t.RefundKey = [32]byte{} },
	} {
		terms := good
		mutate(&terms)
		if _, err := Build(terms, wallet); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Build(good, [32]byte{}); err == nil {
		t.Error("off-curve wallet key accepted")
	}
}

func TestOutpointKeyMatchesC04(t *testing.T) {
	o := bitcoin.TransactionOutpoint{OutputIndex: 0x01020304}
	o.TransactionHash[0] = 0xaa
	raw := append(append([]byte(nil), o.TransactionHash[:]...), 1, 2, 3, 4)
	want := sha3(raw)
	if OutpointKey(o) != want {
		t.Fatal("outpoint key encoding")
	}
}
