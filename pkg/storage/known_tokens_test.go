package storage

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// TestKnownTokenMetadataApplies: a token registered by a chain package
// gets its registered metadata without a stored or fetched one.
func TestKnownTokenMetadataApplies(t *testing.T) {
	addr := common.HexToAddress("0x00000000000000000000000000000000000c0de1")
	RegisterKnownToken(addr, KnownToken{Name: "Coin", Symbol: "CN", Decimals: 18})

	s := newTestPebble(t)
	tb := &port.TokenBalance{}
	s.applyTokenMetadata(context.Background(), tb, addr)
	require.Equal(t, "Coin", tb.Name)
	require.Equal(t, "CN", tb.Symbol)
	require.NotNil(t, tb.Decimals)
	require.Equal(t, 18, *tb.Decimals)
	require.Panics(t, func() { RegisterKnownToken(addr, KnownToken{}) })
}
