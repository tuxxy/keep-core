package ethereum

import (
	"context"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
)

// GetCurrentAndPrevEpochDifficulty reads the same relay used by the Bridge.
// It supplies the production accumulated-work policy to local node producers.
func (c *FrostRedemptionChain) GetCurrentAndPrevEpochDifficulty() (*big.Int, *big.Int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opts := &bind.CallOpts{Context: ctx}
	references, err := c.legacy.ContractReferences(opts)
	if err != nil {
		return nil, nil, err
	}
	contractABI, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"getCurrentEpochDifficulty","inputs":[],"outputs":[{"type":"uint256"}],"stateMutability":"view"},{"type":"function","name":"getPrevEpochDifficulty","inputs":[],"outputs":[{"type":"uint256"}],"stateMutability":"view"}]`))
	if err != nil {
		return nil, nil, err
	}
	relay := bind.NewBoundContract(references.Relay, contractABI, c.client, c.client, c.client)
	read := func(method string) (*big.Int, error) {
		var out []any
		if err := relay.Call(opts, &out, method); err != nil {
			return nil, err
		}
		return *abi.ConvertType(out[0], new(*big.Int)).(**big.Int), nil
	}
	current, err := read("getCurrentEpochDifficulty")
	if err != nil {
		return nil, nil, err
	}
	previous, err := read("getPrevEpochDifficulty")
	return current, previous, err
}
