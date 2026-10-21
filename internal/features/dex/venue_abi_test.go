package dex

import (
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/testchain/venueabi"
)

// venueEvent is one way the feature matches a log: e.is(topic, topics, words).
type venueEvent struct {
	venue, file string
	topic       common.Hash
	topics      int // 1 + indexed arguments
	words       int // data words read
}

// The (topics, words) pairs are the ones passed to e.is in pools.go and trades.go.
var venueEvents = []venueEvent{
	{"uniswap_v3", "IUniswapV3Factory.json", TopicV3PoolCreated, 4, 2},
	{"uniswap_v3", "IUniswapV3Pool.json", TopicV3Initialize, 1, 2},
	{"uniswap_v3", "IUniswapV3Pool.json", TopicV3Swap, 3, 5},
	{"uniswap_v3", "IUniswapV3Pool.json", TopicV3Mint, 4, 4},
	{"uniswap_v3", "IUniswapV3Pool.json", TopicV3Burn, 4, 3},
	{"uniswap_v2", "IUniswapV2Factory.json", TopicV2PairCreated, 3, 2},
	{"uniswap_v2", "IUniswapV2Pair.json", TopicV2Swap, 3, 4},
	{"uniswap_v2", "IUniswapV2Pair.json", TopicV2Sync, 1, 2},
	{"uniswap_v2", "IUniswapV2Pair.json", TopicV2Mint, 2, 2},
	{"uniswap_v2", "IUniswapV2Pair.json", TopicV2Burn, 3, 2},
	{"perp_orderbook", "PerpetualEngine.json", TopicPerpMarketCreated, 4, 1},
	{"perp_orderbook", "OrderManager.json", TopicPerpOrderCreated, 4, 4},
	{"perp_orderbook", "OrderManager.json", TopicPerpOrderPartiallyFill, 2, 4},
	{"perp_orderbook", "OrderManager.json", TopicPerpOrderCancelled, 3, 0},
	{"perp_orderbook", "OrderManager.json", TopicPerpOrderExpired, 3, 1},
	{"perp_orderbook", "OrderManager.json", TopicPerpOrderModified, 3, 2},
	{"perp_orderbook", "OrderManager.json", TopicPerpOrderTriggered, 2, 2},
	{"perp_orderbook", "OrderManager.json", TopicPerpMarketOrderExecuted, 3, 3},
	{"perp_orderbook", "OrderManager.json", TopicPerpOrdersMatched, 3, 2},
}

// TestVenueEventsMatchContractABIs checks every hard-coded topic against the
// pinned contract ABIs in testdata/venues: the event must exist, have the
// indexed-argument count the matcher expects, and carry at least the data
// words the decoder reads.
func TestVenueEventsMatchContractABIs(t *testing.T) {
	for _, want := range venueEvents {
		contract, err := venueabi.Load(filepath.Join("testdata", "venues"), want.venue, want.file)
		require.NoError(t, err)
		var found *abi.Event
		for _, ev := range contract.Events {
			if ev.ID == want.topic {
				found = &ev
				break
			}
		}
		require.NotNilf(t, found, "%s/%s: no event with topic %s", want.venue, want.file, want.topic)

		indexed, dataWords := 0, 0
		for _, in := range found.Inputs {
			if in.Indexed {
				indexed++
			} else {
				dataWords += venueabi.Words(in.Type)
			}
		}
		require.Equalf(t, want.topics, 1+indexed, "%s: topic count", found.Sig)
		require.LessOrEqualf(t, want.words, dataWords, "%s: data words read", found.Sig)
	}
}
