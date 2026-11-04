package ethereum

import (
	"context"
	"fmt"
	"math/big"
	stdnet "net"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/keep-network/keep-common/pkg/chain/ethereum/ethutil"
)

// Adversarial review ADV-spv (2026-11-04), not a frozen test. The FROST
// Ethereum bindings send from the operator key, the key the base chain's
// contract wrappers also send from. Invariant (base keep-core
// baseChain.transactionMutex): one account's transactions are serialized
// through nonce choice and submission only, never through mining, and every
// sender of the operator key shares that serialization.

// frostSendAnvil starts a local Anvil (fixture: a local dev chain with its
// public dev account 0, test funds only) and returns a client and that key.
func frostSendAnvil(t *testing.T, args ...string) (*ethclient.Client, *keystore.Key) {
	t.Helper()
	path, err := exec.LookPath("anvil")
	if err != nil {
		t.Skip("anvil not installed")
	}
	listener, err := stdnet.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*stdnet.TCPAddr).Port
	listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, path, append([]string{"--silent", "--port", fmt.Sprint(port), "--chain-id", fmt.Sprint(200000 + port)}, args...)...)
	if err = command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	var client *ethclient.Client
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		client, err = ethclient.Dial(fmt.Sprintf("http://127.0.0.1:%d", port))
		if err == nil {
			if _, err = client.ChainID(context.Background()); err == nil {
				break
			}
			client.Close()
		}
		if time.Now().After(deadline) {
			t.Fatalf("anvil startup: %v", err)
		}
	}
	t.Cleanup(client.Close)
	private, err := crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	if err != nil {
		t.Fatal(err)
	}
	return client, &keystore.Key{PrivateKey: private, Address: crypto.PubkeyToAddress(private.PublicKey)}
}

// frostSendTransfer is a plain value transfer through bind's own nonce and
// fee logic, the same path the generated FROST Bridge methods take.
func frostSendTransfer(client *ethclient.Client) func(*bind.TransactOpts) (*types.Transaction, error) {
	to := common.HexToAddress("0x00000000000000000000000000000000000f2057")
	return func(o *bind.TransactOpts) (*types.Transaction, error) {
		// An EOA has no code for bind's estimate: a transfer's fixed gas.
		o.GasLimit = 21000
		return bind.NewBoundContract(to, abi.ABI{}, client, client, client).Transfer(o)
	}
}

func frostSendPending(t *testing.T, client *ethclient.Client, account common.Address) uint64 {
	t.Helper()
	n, err := client.PendingNonceAt(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A FROST send waiting for its receipt must not block the next FROST send of
// the same binding: with no block mined, the second send is submitted and
// returns at its own deadline.
func TestFrostSendDoesNotHoldTheLockWhileMining(t *testing.T) {
	client, key := frostSendAnvil(t, "--no-mining")
	chain, err := NewFrostRedemptionChain(context.Background(), client, key.PrivateKey, common.HexToAddress("0x000000000000000000000000000000000000b51d"))
	if err != nil {
		t.Fatal(err)
	}
	firstCtx, cancelFirst := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancelFirst()
	first := make(chan error, 1)
	go func() { first <- chain.send(firstCtx, frostSendTransfer(client)) }()
	for deadline := time.Now().Add(5 * time.Second); frostSendPending(t, client, key.Address) < 1; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("first FROST send was never submitted")
		}
	}

	secondCtx, cancelSecond := context.WithTimeout(context.Background(), time.Second)
	defer cancelSecond()
	start := time.Now()
	err = chain.send(secondCtx, frostSendTransfer(client))
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Fatalf("second FROST send waited %v behind the first send's mining wait (own deadline 1s)", elapsed)
	}
	if err == nil {
		t.Fatal("second FROST send reported success with no block mined")
	}
	if n := frostSendPending(t, client, key.Address); n != 2 {
		t.Fatalf("second FROST send not submitted while the first was unmined: pending nonce %d, want 2", n)
	}
	cancelFirst()
	<-first
}

// The FROST Bridge binding a node binds through TbtcChain and a base
// contract wrapper send from the same operator key at the same moment. Each
// must get its own nonce: both are mined.
func TestFrostSendSharesTheOperatorNonceWithBaseSends(t *testing.T) {
	client, key := frostSendAnvil(t)
	id, err := client.ChainID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mutex := &sync.Mutex{}
	nonces := ethutil.NewNonceManager(client, key.Address)
	tc := &TbtcChain{
		baseChain:     &baseChain{key: key, chainID: id, ethClient: client, nonceManager: nonces, transactionMutex: mutex},
		bridgeAddress: common.HexToAddress("0x000000000000000000000000000000000000b51d"),
	}
	frost, err := tc.frostRedemptionChain(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Both senders meet after choosing their nonce and before submitting,
	// or proceed alone after 300 ms when the other cannot run concurrently.
	var arrived sync.WaitGroup
	arrived.Add(2)
	meet := func() {
		arrived.Done()
		all := make(chan struct{})
		go func() { arrived.Wait(); close(all) }()
		select {
		case <-all:
		case <-time.After(300 * time.Millisecond):
		}
	}
	transfer := frostSendTransfer(client)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var frostErr, baseErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		frostErr = frost.send(ctx, func(o *bind.TransactOpts) (*types.Transaction, error) {
			if o.Nonce == nil {
				n, err := client.PendingNonceAt(ctx, key.Address)
				if err != nil {
					return nil, err
				}
				o.Nonce = new(big.Int).SetUint64(n)
			}
			meet()
			return transfer(o)
		})
	}()
	go func() {
		defer wg.Done()
		// Fixture: the shape of a generated base contract wrapper method
		// (pkg/chain/ethereum/tbtc/gen/contract): transactionMutex, then the
		// nonce manager's nonce, submission, IncrementNonce.
		tx, err := func() (*types.Transaction, error) {
			mutex.Lock()
			defer mutex.Unlock()
			n, err := nonces.CurrentNonce()
			if err != nil {
				return nil, err
			}
			opts, err := bind.NewKeyedTransactorWithChainID(key.PrivateKey, id)
			if err != nil {
				return nil, err
			}
			// A different payload: a distinct transaction, not a duplicate.
			opts.Context, opts.Nonce, opts.Value = ctx, new(big.Int).SetUint64(n), big.NewInt(1)
			meet()
			tx, err := transfer(opts)
			if err == nil {
				nonces.IncrementNonce()
			}
			return tx, err
		}()
		if err != nil {
			baseErr = err
			return
		}
		receipt, err := bind.WaitMined(ctx, client, tx)
		if err == nil && receipt.Status != types.ReceiptStatusSuccessful {
			err = fmt.Errorf("base send reverted")
		}
		baseErr = err
	}()
	wg.Wait()
	if frostErr != nil || baseErr != nil {
		t.Fatalf("FROST send and base send raced on one operator nonce: frost=%v base=%v", frostErr, baseErr)
	}
	if n := frostSendPending(t, client, key.Address); n != 2 {
		t.Fatalf("operator account nonce %d after two sends, want 2", n)
	}
}
