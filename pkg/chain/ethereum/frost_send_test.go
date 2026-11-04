package ethereum

import (
	"context"
	"fmt"
	stdnet "net"
	"os/exec"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
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
