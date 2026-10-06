package porttest

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// searchTypes returns the Type of every result, in order.
func searchTypes(rs []port.SearchResult) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Type)
	}
	return out
}

// searchStoreWithChain returns a store holding the fixture chain.
func searchStoreWithChain(t *testing.T, newStore NewStore) (searchStore, *chain) {
	t.Helper()
	s := open[searchStore](t, newStore)
	c := newChain(5)
	c.write(t, s)
	return s, c
}

// searchWithABI returns the store as an ABI writer, skipping the test when
// the store keeps no ABIs (contracts are the addresses that have an ABI).
func searchWithABI(t *testing.T, s searchStore) port.ABIWriter {
	t.Helper()
	w, ok := s.(port.ABIWriter)
	if !ok {
		t.Skip("the store keeps no ABIs, so no address is reported as a contract")
	}
	return w
}

// testSearch checks SearchReader: a decimal number finds the block at that
// height, a 32-byte hash finds the block or transaction with that hash, and
// a 20-byte address yields an address result (and a contract result first
// when the address has an ABI). resultTypes restricts the result types,
// limit caps the results (limit <= 0 uses a default), and an empty query or
// one that matches nothing returns no results.
func testSearch(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("EmptyQuery", func(t *testing.T) {
		s, _ := searchStoreWithChain(t, newStore)
		rs, err := s.Search(ctx, "", nil, 10)
		require.NoError(t, err)
		assert.Empty(t, rs)
	})

	t.Run("BlockNumber", func(t *testing.T) {
		s, c := searchStoreWithChain(t, newStore)
		rs, err := s.Search(ctx, "3", nil, 10)
		require.NoError(t, err)
		require.Len(t, rs, 1)
		assert.Equal(t, "block", rs[0].Type)
		assert.Equal(t, "3", rs[0].Value)
		assert.Equal(t, c.Blocks[3].Hash.Hex(), rs[0].Metadata["hash"])
		assert.EqualValues(t, 3, rs[0].Metadata["number"])

		rs, err = s.Search(ctx, "0", nil, 10)
		require.NoError(t, err)
		require.Len(t, rs, 1, "block 0 is found")
		assert.Equal(t, "0", rs[0].Value)

		rs, err = s.Search(ctx, "99", nil, 10)
		require.NoError(t, err)
		assert.Empty(t, rs, "no block at height 99")
	})

	t.Run("HexBlockNumber", func(t *testing.T) {
		knownDefect(t, "Search treats a 0x-prefixed block number as block 0")
		s, _ := searchStoreWithChain(t, newStore)
		rs, err := s.Search(ctx, "0x3", nil, 10)
		require.NoError(t, err)
		require.Len(t, rs, 1)
		assert.Equal(t, "block", rs[0].Type)
		assert.Equal(t, "3", rs[0].Value)
	})

	t.Run("BlockHash", func(t *testing.T) {
		s, c := searchStoreWithChain(t, newStore)
		want := c.Blocks[2].Hash
		for _, q := range []string{want.Hex(), strings.TrimPrefix(want.Hex(), "0x"), "0x" + strings.ToUpper(want.Hex()[2:])} {
			rs, err := s.Search(ctx, q, nil, 10)
			require.NoError(t, err, q)
			require.Len(t, rs, 1, q)
			assert.Equal(t, "block", rs[0].Type)
			assert.Equal(t, want.Hex(), rs[0].Value)
			assert.EqualValues(t, 2, rs[0].Metadata["number"])
		}
	})

	t.Run("TransactionHash", func(t *testing.T) {
		s, c := searchStoreWithChain(t, newStore)
		tx := c.Blocks[4].Transactions[1]
		rs, err := s.Search(ctx, tx.Hash.Hex(), nil, 10)
		require.NoError(t, err)
		require.Len(t, rs, 1)
		assert.Equal(t, "transaction", rs[0].Type)
		assert.Equal(t, tx.Hash.Hex(), rs[0].Value)
		assert.Equal(t, addrB.Hex(), rs[0].Metadata["from"])
		assert.Equal(t, addrC.Hex(), rs[0].Metadata["to"])
		assert.EqualValues(t, 4, rs[0].Metadata["blockNumber"])
		assert.Equal(t, c.Blocks[4].Hash.Hex(), rs[0].Metadata["blockHash"])

		create := c.Blocks[3].Transactions[2]
		rs, err = s.Search(ctx, create.Hash.Hex(), nil, 10)
		require.NoError(t, err)
		require.Len(t, rs, 1)
		assert.Equal(t, created.Hex(), rs[0].Metadata["contractAddress"], "a creation reports the created contract")
	})

	t.Run("UnknownHash", func(t *testing.T) {
		s, _ := searchStoreWithChain(t, newStore)
		rs, err := s.Search(ctx, fixtureHash("missing", 0).Hex(), nil, 10)
		require.NoError(t, err)
		assert.Empty(t, rs)
	})

	t.Run("Address", func(t *testing.T) {
		s, _ := searchStoreWithChain(t, newStore)
		for _, q := range []string{addrA.Hex(), strings.ToLower(addrA.Hex()), strings.TrimPrefix(strings.ToLower(addrA.Hex()), "0x")} {
			rs, err := s.Search(ctx, q, nil, 10)
			require.NoError(t, err, q)
			require.Len(t, rs, 1, q)
			assert.Equal(t, "address", rs[0].Type)
			assert.Equal(t, addrA.Hex(), rs[0].Value, "the value is the checksummed address")
		}
	})

	t.Run("AddressTransactionCount", func(t *testing.T) {
		knownDefect(t, "Search reports at most 1 as an address's transactionCount")
		s, c := searchStoreWithChain(t, newStore)
		for _, b := range c.Blocks[1:] {
			require.NoError(t, s.AddTransactionToAddressIndex(ctx, addrA, b.Transactions[0].Hash))
		}
		rs, err := s.Search(ctx, addrA.Hex(), []string{"address"}, 10)
		require.NoError(t, err)
		require.Len(t, rs, 1)
		assert.EqualValues(t, len(c.Blocks)-1, rs[0].Metadata["transactionCount"])
	})

	t.Run("Contract", func(t *testing.T) {
		s, _ := searchStoreWithChain(t, newStore)
		w := searchWithABI(t, s)
		require.NoError(t, w.SetABI(ctx, addrC, []byte(`[]`)))

		rs, err := s.Search(ctx, addrC.Hex(), nil, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{"contract", "address"}, searchTypes(rs), "a contract is also an address")
		assert.Equal(t, addrC.Hex(), rs[0].Value)

		rs, err = s.Search(ctx, addrB.Hex(), nil, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{"address"}, searchTypes(rs), "an address without an ABI is not a contract")
	})

	t.Run("ResultTypes", func(t *testing.T) {
		s, c := searchStoreWithChain(t, newStore)
		cases := []struct {
			query string
			types []string
			want  []string
		}{
			{"3", []string{"transaction"}, []string{}},
			{"3", []string{"block", "address"}, []string{"block"}},
			{c.Blocks[2].Hash.Hex(), []string{"transaction"}, []string{}},
			{c.Blocks[2].Transactions[0].Hash.Hex(), []string{"block"}, []string{}},
			{c.Blocks[2].Transactions[0].Hash.Hex(), []string{"transaction"}, []string{"transaction"}},
			{addrA.Hex(), []string{"block", "transaction"}, []string{}},
			{addrA.Hex(), []string{"contract"}, []string{}},
			{addrA.Hex(), []string{}, []string{"address"}},
		}
		for _, tc := range cases {
			rs, err := s.Search(ctx, tc.query, tc.types, 10)
			require.NoError(t, err, "%s %v", tc.query, tc.types)
			assert.Equal(t, tc.want, searchTypes(rs), "%s %v", tc.query, tc.types)
		}

		w := searchWithABI(t, s)
		require.NoError(t, w.SetABI(ctx, addrC, []byte(`[]`)))
		rs, err := s.Search(ctx, addrC.Hex(), []string{"address"}, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{"address"}, searchTypes(rs))
		rs, err = s.Search(ctx, addrC.Hex(), []string{"contract"}, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{"contract"}, searchTypes(rs))
	})

	t.Run("Limit", func(t *testing.T) {
		s, _ := searchStoreWithChain(t, newStore)
		w := searchWithABI(t, s)
		require.NoError(t, w.SetABI(ctx, addrC, []byte(`[]`)))

		rs, err := s.Search(ctx, addrC.Hex(), nil, 1)
		require.NoError(t, err)
		assert.Equal(t, []string{"contract"}, searchTypes(rs))

		for _, limit := range []int{0, -1} {
			rs, err = s.Search(ctx, addrC.Hex(), nil, limit)
			require.NoError(t, err)
			assert.Equal(t, []string{"contract", "address"}, searchTypes(rs), "limit %d uses a default limit", limit)
		}
	})

	t.Run("UnrecognisedQuery", func(t *testing.T) {
		knownDefect(t, "Search returns an address result for a query that is not a number, hash or address")
		s, _ := searchStoreWithChain(t, newStore)
		for _, q := range []string{"hello", "0xabcd"} {
			rs, err := s.Search(ctx, q, nil, 10)
			require.NoError(t, err, q)
			assert.Empty(t, rs, q)
		}
	})
}
