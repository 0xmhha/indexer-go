package orderbook

import (
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/testchain/venueabi"
)

// TestReconcileCallsMatchContractABIs checks every view the reconciler calls
// against the pinned contract ABIs: the function must exist under that
// signature and return at least the words the reconciler reads.
func TestReconcileCallsMatchContractABIs(t *testing.T) {
	calls := []struct {
		venue, file, signature string
		words                  int // words passed to chainReader.call
	}{
		{"uniswap_v2", "IUniswapV2Pair.json", sigGetReserves, 2},
		{"uniswap_v3", "IUniswapV3Pool.json", sigSlot0, 2},
		{"uniswap_v3", "IUniswapV3Pool.json", sigLiquidity, 1},
		{"uniswap_v3", "IUniswapV3Pool.json", sigTicks, 2},
		{"uniswap_v3", "IUniswapV3Pool.json", sigTickBitmap, 1},
		{"perp_orderbook", "OrderManager.json", sigGetOrder, orderWords},
	}
	dir := filepath.Join("..", "testdata", "venues")
	for _, c := range calls {
		contract, err := venueabi.Load(dir, c.venue, c.file)
		require.NoError(t, err)
		selector := crypto.Keccak256([]byte(c.signature))[:4]
		method, err := contract.MethodById(selector)
		require.NoErrorf(t, err, "%s/%s: no function %s", c.venue, c.file, c.signature)
		require.Equal(t, c.signature, method.Sig)
		require.LessOrEqualf(t, c.words, venueabi.ArgsWords(method.Outputs), "%s: return words read", c.signature)
	}
}

// TestGetOrderLayoutMatchesContractABI checks the word indexes the reconciler
// reads from getOrder against the Order struct field order.
func TestGetOrderLayoutMatchesContractABI(t *testing.T) {
	contract, err := venueabi.Load(filepath.Join("..", "testdata", "venues"), "perp_orderbook", "OrderManager.json")
	require.NoError(t, err)
	method, ok := contract.Methods["getOrder"]
	require.True(t, ok)
	require.Len(t, method.Outputs, 1)

	fields := method.Outputs[0].Type.TupleRawNames
	require.Len(t, fields, orderWords, "Order must stay a static tuple of orderWords fields")
	require.Equal(t, "status", fields[orderWordStatus])
	require.Equal(t, "size", fields[orderWordSize])
	require.Equal(t, "filledSize", fields[orderWordFilled])
	require.Equal(t, "price", fields[orderWordPrice])
}
