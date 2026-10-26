package ethereum

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestFrostAdaptersSerializeOneOperator(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	var shared sync.Mutex
	dkg := (&FrostChain{key: key, chainID: big.NewInt(31337)}).WithTransactionMutex(&shared)
	duties := (&FrostRedemptionChain{key: key, chainID: big.NewInt(31337)}).WithTransactionMutex(&shared)
	entered, release, second := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan error, 2)
	stop := errors.New("controlled submission stop")
	go func() {
		done <- dkg.transact(context.Background(), func(*bind.TransactOpts) (*types.Transaction, error) { close(entered); <-release; return nil, stop })
	}()
	<-entered
	started := make(chan struct{})
	go func() {
		close(started)
		done <- duties.send(context.Background(), func(*bind.TransactOpts) (*types.Transaction, error) { close(second); return nil, stop })
	}()
	<-started
	select {
	case <-second:
		close(release)
		t.Fatal("operator nonce critical sections overlapped")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-done; !errors.Is(err, stop) {
			t.Fatal(err)
		}
	}
	select {
	case <-second:
	default:
		t.Fatal("unrelated duty did not resume")
	}
}
