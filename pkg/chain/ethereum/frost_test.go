package ethereum

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

func TestFrostViewRetriesOnlyIncoherentSnapshot(t *testing.T) {
	calls := 0
	got, err := readStableFrostView(context.Background(), func() (dkg.View, error) {
		calls++
		if calls < 3 {
			return dkg.View{Head: 99}, fmt.Errorf("%w: returned 100, requested 99", errFrostSnapshotBlock)
		}
		return dkg.View{Head: 100}, nil
	})
	if err != nil || calls != 3 || got.Head != 100 {
		t.Fatalf("incoherent read accepted: calls=%d view=%+v error=%v", calls, got, err)
	}
}
func TestFrostViewBoundsReadRetries(t *testing.T) {
	calls := 0
	_, err := readStableFrostView(context.Background(), func() (dkg.View, error) { calls++; return dkg.View{}, errFrostSnapshotBlock })
	if calls != 5 || !errors.Is(err, dkg.ErrQuarantined) {
		t.Fatalf("read retries not bounded and fail closed: %d %v", calls, err)
	}
}
func TestFrostViewDoesNotRetryReorg(t *testing.T) {
	calls := 0
	_, err := readStableFrostView(context.Background(), func() (dkg.View, error) { calls++; return dkg.View{}, dkg.ErrQuarantined })
	if calls != 1 || !errors.Is(err, dkg.ErrQuarantined) {
		t.Fatalf("reorg was retried: %d %v", calls, err)
	}
}
