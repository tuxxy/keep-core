package signing

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"testing"
)

type intentVector struct {
	Name           string          `json:"name"`
	OutputKey      string          `json:"outputKey"`
	RawTransaction string          `json:"rawTransaction"`
	Previous       []vectorPrevout `json:"previous"`
	Snapshot       string          `json:"snapshot"`
	WalletID       string          `json:"walletID"`
	DescriptorHash string          `json:"descriptorHash"`
	Genesis        string          `json:"genesis"`
	Generation     string          `json:"generation"`
	StateVersion   string          `json:"stateVersion"`
	Requests       []vectorRequest `json:"requests"`
	ConflictHash   string          `json:"conflictHash"`
	ConflictIndex  uint32          `json:"conflictIndex"`
	Fees           []string        `json:"fees"`
	Preimage       string          `json:"preimage"`
	Commitment     string          `json:"commitment"`
}
type vectorPrevout struct {
	Value  string `json:"value"`
	Script string `json:"script"`
}
type vectorRequest struct {
	ID     string `json:"id"`
	Value  string `json:"value"`
	Script string `json:"script"`
}

func vectorPlan(t *testing.T, index int) *Plan {
	p, _, _ := planFixture(t)
	if index == 1 {
		p.Generation = math.MaxUint64
		p.StateVersion = 9007199254740993
	}
	plan, err := NewPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
func TestCanonicalIntentV2(t *testing.T) {
	raw, err := os.ReadFile("testdata/intent-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []intentVector
	if err = json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 2 {
		t.Fatal("missing canonical controls")
	}
	for n, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			plan := vectorPlan(t, n)
			if hex.EncodeToString(plan.CommitmentPreimage()) != v.Preimage || hex.EncodeToString(plan.hash[:]) != v.Commitment {
				t.Fatal("frozen binary commitment changed")
			}
			if sha256.Sum256(plan.CommitmentPreimage()) != plan.Commitment() {
				t.Fatal("hash is not binary-preimage SHA256")
			}
			original := plan.CommitmentPreimage()
			changed := plan.CommitmentPreimage()
			changed[0] ^= 1
			if !bytes.Equal(original, plan.CommitmentPreimage()) {
				t.Fatal("caller changed commitment bytes")
			}
			var recovery Intent
			if err := json.Unmarshal(plan.IntentBytes(), &recovery); err != nil {
				t.Fatal(err)
			}
			// Whitespace and JSON field layout are deliberately not a wire dependency.
			pretty, err := json.MarshalIndent(recovery, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if sha256.Sum256(pretty) == plan.hash || sha256.Sum256(plan.IntentBytes()) == plan.hash {
				t.Fatal("commitment still hashes JSON")
			}
			if !bytes.Equal(canonicalIntentPreimage(recovery), original) {
				t.Fatal("public recovery fields do not reconstruct commitment")
			}
		})
	}
}
func TestCanonicalIntentBindsEveryField(t *testing.T) {
	original := vectorPlan(t, 0)
	for _, tc := range []struct {
		name   string
		change func(*Intent)
	}{
		{"version", func(i *Intent) { i.Version++ }},
		{"snapshot", func(i *Intent) { i.TransactionHash[0] ^= 1 }},
		{"wallet", func(i *Intent) { i.WalletID[0] ^= 1 }},
		{"descriptor", func(i *Intent) { i.DescriptorHash[0] ^= 1 }},
		{"genesis", func(i *Intent) { i.BitcoinGenesis[0] ^= 1 }},
		{"generation", func(i *Intent) { i.Generation++ }},
		{"state version", func(i *Intent) { i.StateVersion++ }},
		{"request count", func(i *Intent) { i.Requests = append(i.Requests, i.Requests[0]) }},
		{"request ID", func(i *Intent) { i.Requests[0].ID[0] ^= 1 }},
		{"request value", func(i *Intent) { i.Requests[0].Value++ }},
		{"request script", func(i *Intent) { i.Requests[0].Script[2] ^= 1 }},
		{"conflict hash", func(i *Intent) { i.ConflictInput.TransactionHash[0] ^= 1 }},
		{"conflict index", func(i *Intent) { i.ConflictInput.OutputIndex++ }},
		{"minimum fee", func(i *Intent) { i.Fees.Minimum++ }},
		{"maximum fee", func(i *Intent) { i.Fees.Maximum++ }},
		{"maximum rate", func(i *Intent) { i.Fees.MaximumSatPerVByte++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var i Intent
			if err := json.Unmarshal(original.IntentBytes(), &i); err != nil {
				t.Fatal(err)
			}
			tc.change(&i)
			if sha256.Sum256(canonicalIntentPreimage(i)) == original.hash {
				t.Fatal("field missing from commitment")
			}
		})
	}
}
