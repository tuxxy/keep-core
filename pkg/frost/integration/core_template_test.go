package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	geth "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
)

// fundedCore starts Core on a copy of a stopped regtest datadir whose wallet
// "k04" holds 101 blocks of coinbases, and returns it with the wallet's mining
// address, and 11 fresh blocks on top. Mining with a loaded wallet costs one
// disk sync per block, so the datadir is built once per machine and Core
// binary, as in pkg/tbtc's legacyCoreTemplate.
func fundedCore(t *testing.T, ctx context.Context) (*coreChain, string) {
	t.Helper()
	path, pin := pinnedCore(t)
	template, miner := coreTemplate(t, ctx, path, pin)
	dir := t.TempDir()
	copyDir(t, template, dir)
	c, _ := launchCore(t, ctx, path, pin, dir)
	if err := c.call(ctx, "loadwallet", []any{"k04"}, nil); err != nil {
		t.Fatal(err)
	}
	// Fresh blocks keep median-time-past at the current time, as when every
	// block was mined by the case itself.
	if err := c.call(ctx, "generatetoaddress", []any{11, miner}, nil); err != nil {
		t.Fatal(err)
	}
	return c, miner
}

func coreTemplate(t *testing.T, ctx context.Context, path, pin string) (string, string) {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(cache, "keep-core-frost-tests")
	final := filepath.Join(root, "core-"+pin[:16]+"-v1")
	if miner, err := os.ReadFile(filepath.Join(final, "miner")); err == nil {
		return final, string(miner)
	}
	if err = os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	building, err := os.MkdirTemp(root, "building-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(building)
	c, stop := launchCore(t, ctx, path, pin, building)
	var miner string
	if err = c.call(ctx, "createwallet", []any{"k04"}, nil); err != nil {
		t.Fatal(err)
	}
	if err = c.call(ctx, "getnewaddress", nil, &miner); err != nil {
		t.Fatal(err)
	}
	if err = c.call(ctx, "generatetoaddress", []any{101, miner}, nil); err != nil {
		t.Fatal(err)
	}
	stop()
	// The datadir is copied whole; drop the stopped process's log.
	_ = os.Remove(filepath.Join(building, "core.log"))
	if err = os.WriteFile(filepath.Join(building, "miner"), []byte(miner), 0o600); err != nil {
		t.Fatal(err)
	}
	// Another process may have finished first; either copy will do.
	if err = os.Rename(building, final); err != nil {
		own, readErr := os.ReadFile(filepath.Join(final, "miner"))
		if readErr != nil {
			t.Fatalf("Core template: %v", err)
		}
		miner = string(own)
	}
	return final, miner
}

// copyDir copies a stopped datadir.
func copyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("Core datadir copy: %v", err)
	}
}

// waitMined waits for tx's receipt. bind.WaitMined polls once a second,
// while Anvil mines at once.
func waitMined(ctx context.Context, b bind.DeployBackend, tx *types.Transaction) (*types.Receipt, error) {
	for {
		receipt, err := b.TransactionReceipt(ctx, tx.Hash())
		if err == nil {
			return receipt, nil
		}
		if !errors.Is(err, geth.NotFound) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
