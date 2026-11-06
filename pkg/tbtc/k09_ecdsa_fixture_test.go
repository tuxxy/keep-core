package tbtc

import (
	"crypto/ecdsa"
	"math/big"
	"testing"
)

// TestK09LegacyGroupFixture checks the K-09 legacy group FIXTURE itself
// (k09_ecdsa_export_test.go): base's signing executor, over the shares of
// group tweaks 0 and 1, produces signatures that verify under that group's
// public key, and the two groups' keys differ. Harness self-check for
// tuxxy/keep-core#40; it says nothing about production code.
func TestK09LegacyGroupFixture(t *testing.T) {
	message := big.NewInt(0x0940)
	keys := map[int64]*ecdsa.PublicKey{}
	for _, d := range []int64{0, 1} {
		key, err := K09LegacyGroupPublicKey(d)
		if err != nil {
			t.Fatal(err)
		}
		keys[d] = key
		signature := K09SignWithLegacyGroup(t, d, []*big.Int{message})[0]
		if !ecdsa.Verify(key, message.FillBytes(make([]byte, 32)), signature.R, signature.S) {
			t.Fatalf("group tweak %d: the group's signature does not verify under its public key", d)
		}
	}
	if keys[0].X.Cmp(keys[1].X) == 0 {
		t.Fatal("group tweaks 0 and 1 have the same public key")
	}
}
