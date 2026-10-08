package api

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/features/dex"
)

// TestTradeSubscription: the dexTrade subscription filters trades by market
// address and delivers them as the queries do.
func TestTradeSubscription(t *testing.T) {
	poolA := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	poolB := common.HexToAddress("0x00000000000000000000000000000000000000b1")
	trade := func(pool common.Address) *dex.TradeEvent {
		return &dex.TradeEvent{Trade: port.DexTrade{
			Market: port.DexMarketKey{Address: pool}, Venue: port.DexUniswapV3, BlockNumber: 9, Side: port.DexBuy,
			BaseAmount: big.NewInt(1), QuoteAmount: big.NewInt(2), Price: big.NewInt(3), Taker: poolA,
		}}
	}

	f, err := tradeFilter(map[string]interface{}{"markets": []interface{}{poolA.Hex()}})
	require.NoError(t, err)
	assert.True(t, f.Match(trade(poolA)))
	assert.False(t, f.Match(trade(poolB)))
	f, err = tradeFilter(map[string]interface{}{})
	require.NoError(t, err)
	assert.Nil(t, f, "no markets: every trade")
	_, err = tradeFilter(map[string]interface{}{"markets": []interface{}{"pool"}})
	assert.Error(t, err)

	v, ok := tradePayload(trade(poolA))
	require.True(t, ok)
	assert.Equal(t, TradeMap(&trade(poolA).Trade), v)
	_, ok = tradePayload(&events.BlockEvent{})
	assert.False(t, ok)
}
