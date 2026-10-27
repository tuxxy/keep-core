package store

import (
	"context"
	"testing"
)

func TestSigningClaimSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	config := testConfig(t)
	root, journal := openTest(t, config)
	id := [32]byte{88}
	if exists, err := journal.HasClaim(ctx, id); err != nil || exists {
		t.Fatal("fresh claim exists", err)
	}
	release, err := journal.Claim(ctx, id, "sign", []byte("public signing attempt"))
	must(t, err)
	release()
	must(t, root.Close())
	root, journal = openTest(t, config)
	defer root.Close()
	if exists, err := journal.HasClaim(ctx, id); err != nil || !exists {
		t.Fatal("control DurableAttempt assertion: interrupted claim disappeared", err)
	}
}
