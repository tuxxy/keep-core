package deposit

import (
	"context"
	"sort"

	"github.com/keep-network/keep-core/pkg/bitcoin"
)

// HeaderClock is Bitcoin's median-time-past (BIP113) read from a Bitcoin
// chain's block headers: the median of the last 11 block times.
type HeaderClock struct {
	Chain bitcoin.Chain
}

// MedianTimePast implements Clock.
func (c HeaderClock) MedianTimePast(context.Context) (uint64, error) {
	tip, err := c.Chain.GetLatestBlockHeight()
	if err != nil {
		return 0, err
	}
	var times []uint32
	for height := tip; len(times) < 11; height-- {
		header, err := c.Chain.GetBlockHeader(height)
		if err != nil {
			return 0, err
		}
		times = append(times, header.Time)
		if height == 0 {
			break
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	return uint64(times[len(times)/2]), nil
}
