package ethereum

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	eth "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	commonEthereum "github.com/keep-network/keep-common/pkg/chain/ethereum"
	chainutil "github.com/keep-network/keep-common/pkg/chain/ethereum/ethutil"
	"github.com/keep-network/keep-core/pkg/bitcoin"
	"github.com/keep-network/keep-core/pkg/chain/ethereum/frostbridgeabi"
	legacyabi "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/abi"
	legacycontract "github.com/keep-network/keep-core/pkg/chain/ethereum/tbtc/gen/contract"
)

// Control only the Ethereum RPC boundary. Signing, ABI encoding, nonce manager,
// error resolver, shared submission mutex and mining waiter are the shipped code.
type resetRPC struct {
	chainutil.EthereumClient
	mu            sync.Mutex
	reject, mined bool
	sent          chan *types.Transaction
	receipts      chan struct{}
	resolved      []byte
}

func (*resetRPC) PendingNonceAt(context.Context, common.Address) (uint64, error) { return 7, nil }
func (*resetRPC) PendingCodeAt(context.Context, common.Address) ([]byte, error) {
	return []byte{1}, nil
}
func (*resetRPC) CodeAt(context.Context, common.Address, *big.Int) ([]byte, error) {
	return []byte{1}, nil
}
func (*resetRPC) HeaderByNumber(context.Context, *big.Int) (*types.Header, error) {
	return &types.Header{Number: big.NewInt(11), Time: 12345}, nil
}
func (*resetRPC) SuggestGasPrice(context.Context) (*big.Int, error)        { return big.NewInt(1000), nil }
func (*resetRPC) EstimateGas(context.Context, eth.CallMsg) (uint64, error) { return 100000, nil }
func (b *resetRPC) SendTransaction(_ context.Context, tx *types.Transaction) error {
	b.mu.Lock()
	reject := b.reject
	b.mu.Unlock()
	if reject {
		return errors.New("submission rejected")
	}
	b.sent <- tx
	return nil
}
func (b *resetRPC) TransactionReceipt(context.Context, common.Hash) (*types.Receipt, error) {
	b.mu.Lock()
	mined := b.mined
	b.mu.Unlock()
	if mined {
		select {
		case b.receipts <- struct{}{}:
		default:
		}
		return &types.Receipt{Status: 1}, nil
	}
	return nil, eth.NotFound
}
func (b *resetRPC) CallContract(ctx context.Context, msg eth.CallMsg, at *big.Int) ([]byte, error) {
	a, _ := legacyabi.BridgeMetaData.GetAbi()
	f, _ := frostbridgeabi.FrostBridgeMetaData.GetAbi()
	if bytes.Equal(msg.Data, f.Methods["frostCreationMode"].ID) || bytes.Equal(msg.Data, a.Methods["liveWalletsCount"].ID) {
		return (&capabilityBackend{code: []byte{1}}).CallContract(ctx, msg, at)
	}
	b.mu.Lock()
	b.resolved = append([]byte(nil), msg.Data...)
	b.mu.Unlock()
	typ, _ := abi.NewType("string", "", nil)
	raw, _ := (abi.Arguments{{Type: typ}}).Pack("owning reset refusal")
	return append(common.FromHex("0x08c379a0"), raw...), nil
}
func TestTbtcChainResetSubmission(t *testing.T) {
	for _, extension := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "extension"}[extension], func(t *testing.T) {
			b := &resetRPC{reject: true, sent: make(chan *types.Transaction, 32), receipts: make(chan struct{}, 32)}
			key, err := crypto.GenerateKey()
			if err != nil {
				t.Fatal(err)
			}
			address := common.Address{1}
			mutex := &sync.Mutex{}
			nonce := chainutil.NewNonceManager(b, crypto.PubkeyToAddress(key.PublicKey))
			waiter := chainutil.NewMiningWaiter(b, commonEthereum.Config{MiningCheckInterval: 50 * time.Millisecond, MaxGasFeeCap: *commonEthereum.WrapWei(big.NewInt(2000))})
			chain := &TbtcChain{baseChain: &baseChain{key: &keystore.Key{PrivateKey: key}, client: b, chainID: big.NewInt(100123), nonceManager: nonce, miningWaiter: waiter, transactionMutex: mutex}, frostBridgeAddress: address}
			chain.bridge, err = legacycontract.NewBridge(address, chain.chainID, chain.key, b, nonce, waiter, nil, mutex)
			if err != nil {
				t.Fatal(err)
			}
			if extension {
				chain.frostBridge, err = frostbridgeabi.NewFrostBridgeCaller(address, b)
				if err != nil {
					t.Fatal(err)
				}
			}
			source := [20]byte{3}
			main := &bitcoin.UnspentTransactionOutput{Outpoint: &bitcoin.TransactionOutpoint{TransactionHash: bitcoin.Hash{4}, OutputIndex: 5}, Value: 90000}
			if extension {
				if err := chain.ResetLegacyMovingFundsTimeout(source, nil); err == nil {
					t.Fatal("missing main accepted")
				}
			}
			var expected []byte
			if extension {
				a, _ := frostbridgeabi.FrostBridgeMetaData.GetAbi()
				expected, err = a.Pack("resetLegacyMovingFundsTimeout", source, frostbridgeabi.BitcoinTxUTXO{TxHash: [32]byte(main.Outpoint.TransactionHash), TxOutputIndex: 5, TxOutputValue: 90000})
			} else {
				a, _ := legacyabi.BridgeMetaData.GetAbi()
				expected, err = a.Pack("resetMovingFundsTimeout", source)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = chain.ResetLegacyMovingFundsTimeout(source, main); err == nil || !strings.Contains(err.Error(), "owning reset refusal") {
				t.Fatal("control ResetResolver assertion: owning revert lost", err)
			}
			n, err := nonce.CurrentNonce()
			if err != nil || n != 7 {
				t.Fatal("failed submission advanced nonce", n, err)
			}
			b.mu.Lock()
			resolved := append([]byte(nil), b.resolved...)
			b.reject = false
			b.mu.Unlock()
			if !bytes.Equal(resolved, expected) {
				t.Fatal("error resolver used different calldata")
			}
			if now, err := chain.MovingFundsNow(); err != nil || now.Unix() != 12345 {
				t.Fatal("clock did not use chain time", now, err)
			}
			deadline := time.After(3 * time.Second)
			submitted := make(chan error, 1)
			go func() { submitted <- chain.ResetLegacyMovingFundsTimeout(source, main) }()
			select {
			case err = <-submitted:
				if err != nil {
					t.Fatal(err)
				}
			case <-deadline:
				t.Fatal("control ResetAsync assertion: submit waited for mining")
			}
			var first *types.Transaction
			select {
			case first = <-b.sent:
			case <-deadline:
				t.Fatal("missing first submission")
			}
			if first.Nonce() != 7 || !bytes.Equal(first.Data(), expected) {
				t.Fatal("wrong reset nonce/calldata")
			}
			if next, err := nonce.CurrentNonce(); err != nil || next != 8 {
				t.Fatal("control ResetNonce assertion: successful reset did not advance nonce exactly once", next, err)
			}
			// A different generated-wrapper transaction must use the SAME mutex/nonce
			// manager and return while the reset's receipt remains deliberately absent.
			go func() { _, err := chain.bridge.ResetMovingFundsTimeout([20]byte{8}); submitted <- err }()
			select {
			case err = <-submitted:
				if err != nil {
					t.Fatal(err)
				}
			case <-deadline:
				t.Fatal("control ResetMutex assertion: pending reset blocked another wallet")
			}
			seenNext, bumped := false, false
			for !seenNext || !bumped {
				select {
				case tx := <-b.sent:
					if tx.Nonce() == 8 {
						seenNext = true
					}
					if tx.Nonce() == 7 && tx.GasPrice().Cmp(first.GasPrice()) > 0 {
						if !bytes.Equal(tx.Data(), first.Data()) || tx.Gas() != first.Gas() {
							t.Fatal("resubmission changed reset")
						}
						if tx.GasPrice().Cmp(big.NewInt(2000)) > 0 {
							t.Fatal("configured fee cap ignored")
						}
						bumped = true
					}
				case <-deadline:
					t.Fatal("control ResetMining assertion: missing concurrent submission or configured gas bump")
				}
			}
			b.mu.Lock()
			b.mined = true
			b.mu.Unlock()
			// Both waiters observe their receipts; subsequent test cases have no mining job.
			for i := 0; i < 2; i++ {
				select {
				case <-b.receipts:
				case <-time.After(2 * time.Second):
					t.Fatal("mining waiter did not stop at receipt")
				}
			}
		})
	}
}
