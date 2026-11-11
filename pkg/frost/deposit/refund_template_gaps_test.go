package deposit

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// TestReceiptRefusesEachMissingDomainField: the C-05 receipt binds a deposit
// to one chain, Bridge, Bitcoin network, wallet, descriptor and depositor;
// none may be absent. Invariant: each one zeroed alone (and a chain ID that is
// negative or wider than 256 bits) is refused, so a receipt never commits to
// a deposit that C-05 would not admit. Refs M5-deposit mutation batch
// (KC-SF-V3 K-06, D-04 template).
func TestReceiptRefusesEachMissingDomainField(t *testing.T) {
	good := sampleTerms(testKey(9), xonly(testKey(7)))
	if _, err := good.Receipt(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Terms)
	}{
		{"negative chain", func(t *Terms) { t.ChainID = big.NewInt(-1) }},
		{"chain wider than 256 bits", func(t *Terms) { t.ChainID = new(big.Int).Lsh(big.NewInt(1), 256) }},
		{"zero Bridge", func(t *Terms) { t.Bridge = [20]byte{} }},
		{"zero Bitcoin genesis", func(t *Terms) { t.BitcoinGenesis = [32]byte{} }},
		{"zero wallet", func(t *Terms) { t.WalletID = [32]byte{} }},
		{"zero descriptor", func(t *Terms) { t.DescriptorHash = [32]byte{} }},
		{"zero depositor", func(t *Terms) { t.Depositor = [20]byte{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			terms := good
			terms.ChainID = new(big.Int).Set(good.ChainID)
			tc.change(&terms)
			err := recovered(func() error { _, err := terms.Receipt(); return err })
			if err == nil {
				t.Fatalf("a receipt was computed with %s", tc.name)
			}
			if bytes.HasPrefix([]byte(err.Error()), []byte("panic:")) {
				t.Fatalf("receipt with %s crashed instead of refusing: %v", tc.name, err)
			}
		})
	}
}

// refundCase is one depositor refund request (P-K06-REFUND-RACE) built from
// the sample terms of template_test.go (a fixture).
type refundCase struct {
	terms       Terms
	c           Construction
	funding     bitcoin.TransactionOutpoint
	destination bitcoin.Script
	fee         int64
	key         *btcec.PrivateKey
}

func newRefundCase(t *testing.T) refundCase {
	t.Helper()
	refundKey := testKey(9)
	terms := sampleTerms(refundKey, xonly(testKey(7)))
	c, err := Build(terms, xonly(testKey(7)))
	if err != nil {
		t.Fatal(err)
	}
	funding := bitcoin.TransactionOutpoint{OutputIndex: 1}
	funding.TransactionHash[0] = 0x42
	return refundCase{terms: terms, c: c, funding: funding, destination: append(bitcoin.Script{0x00, 0x14}, make([]byte, 20)...), fee: 500, key: refundKey}
}

// TestRefundRefusesEachIncompleteRequest: the refund is signed only for a
// complete request whose construction belongs to its terms. Invariant: a
// missing key, an empty destination, a zero funding hash, terms without a
// receipt, a construction whose receipt alone differs, a malformed refund
// control block, an unsupported leaf version and a destination
// pkg/bitcoin cannot carry are each refused alone, with an error and no
// transaction, never a crash; the complete request is signed. Refs
// M5-deposit mutation batch (P-K06-REFUND-RACE).
func TestRefundRefusesEachIncompleteRequest(t *testing.T) {
	if base := newRefundCase(t); true {
		if _, err := Refund(base.terms, base.c, base.funding, base.destination, base.fee, base.key); err != nil {
			t.Fatal("the complete refund request was refused:", err)
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*refundCase)
	}{
		{"no key", func(r *refundCase) { r.key = nil }},
		{"empty destination", func(r *refundCase) { r.destination = nil }},
		{"zero funding hash", func(r *refundCase) { r.funding.TransactionHash = bitcoin.Hash{} }},
		{"terms without a receipt", func(r *refundCase) { r.terms.Depositor = [20]byte{} }},
		{"construction of another receipt", func(r *refundCase) { r.c.Receipt[0] ^= 1 }},
		{"malformed refund control block", func(r *refundCase) { r.c.RefundControl = r.c.RefundControl[:10] }},
		{"unsupported leaf version", func(r *refundCase) {
			r.c.RefundControl = append([]byte(nil), r.c.RefundControl...)
			r.c.RefundControl[0] = 0xc2 | (r.c.RefundControl[0] & 1)
		}},
		{"destination too large for a transaction", func(r *refundCase) { r.destination = make(bitcoin.Script, 10_001) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRefundCase(t)
			tc.change(&r)
			var tx *bitcoin.Transaction
			err := recovered(func() error {
				var err error
				tx, err = Refund(r.terms, r.c, r.funding, r.destination, r.fee, r.key)
				return err
			})
			if err == nil || tx != nil {
				t.Fatalf("a refund was signed with %s", tc.name)
			}
			if bytes.HasPrefix([]byte(err.Error()), []byte("panic:")) {
				t.Fatalf("a refund with %s crashed instead of refusing: %v", tc.name, err)
			}
		})
	}
}

// headerChain is a fixture Bitcoin chain of block headers with given times;
// it implements only the reads HeaderClock uses.
type headerChain struct {
	bitcoin.Chain
	times             []uint32
	failTip, failRead bool
}

func (c headerChain) GetLatestBlockHeight() (uint, error) {
	if c.failTip {
		return 0, errInjected
	}
	return uint(len(c.times) - 1), nil
}

func (c headerChain) GetBlockHeader(height uint) (*bitcoin.BlockHeader, error) {
	if c.failRead || height >= uint(len(c.times)) {
		return nil, errInjected
	}
	return &bitcoin.BlockHeader{Time: c.times[height]}, nil
}

// TestHeaderClockMedianTimePast: BIP113 median-time-past is the median of the
// last 11 block times (sorted; index len/2), or of every block when the chain
// is shorter, as Bitcoin Core's GetMedianTimePast. Invariant: the value is
// exact for a long chain with unordered times, for a short chain including
// genesis, and for a one-block chain; an unreadable tip or header fails.
// Times are fixture values. Refs M5-deposit mutation batch (A-K06-MTP).
func TestHeaderClockMedianTimePast(t *testing.T) {
	// Heights 0..19; the last 11 (9..19) are unordered, with the outside
	// values far away, so reading 10 or 12 headers moves the median.
	long := []uint32{2_000, 2_001, 2_002, 2_003, 2_004, 2_005, 2_006, 2_007, 2_008,
		1_000, 1_090, 1_020, 1_080, 1_030, 1_070, 1_040, 1_060, 1_050, 1_010, 1_100}
	for _, tc := range []struct {
		name  string
		chain headerChain
		want  uint64
	}{
		// Last 11: 1000 1090 1020 1080 1030 1070 1040 1060 1050 1010 1100 -> 1050.
		{"long chain", headerChain{times: long}, 1_050},
		// Four blocks 0..3: sorted 10 20 30 40, index 2 -> 30.
		{"short chain from genesis", headerChain{times: []uint32{30, 10, 40, 20}}, 30},
		{"one block", headerChain{times: []uint32{77}}, 77},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got uint64
			err := recovered(func() error {
				var err error
				got, err = HeaderClock{Chain: tc.chain}.MedianTimePast(ctx)
				return err
			})
			if err != nil || got != tc.want {
				t.Fatalf("median-time-past %d (%v), want %d", got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name  string
		chain headerChain
	}{
		{"tip unreadable", headerChain{times: long, failTip: true}},
		{"header unreadable", headerChain{times: long, failRead: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := recovered(func() error { _, err := (HeaderClock{Chain: tc.chain}).MedianTimePast(ctx); return err })
			if err == nil || bytes.HasPrefix([]byte(err.Error()), []byte("panic:")) {
				t.Fatalf("median-time-past with the %s did not fail as a read: %v", tc.name, err)
			}
		})
	}
}
