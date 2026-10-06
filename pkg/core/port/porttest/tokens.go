package porttest

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Token contracts of the token fixtures.
var (
	tokensERC20a  = common.HexToAddress("0x0000000000000000000000000000000000002001")
	tokensERC20b  = common.HexToAddress("0x0000000000000000000000000000000000002002")
	tokensERC721  = common.HexToAddress("0x0000000000000000000000000000000000007201")
	tokensERC1155 = common.HexToAddress("0x0000000000000000000000000000000000011551")
	tokensUnknown = common.HexToAddress("0x0000000000000000000000000000000000000bad")
)

// tokensMetadata returns token metadata for addr.
func tokensMetadata(addr common.Address, std port.TokenStandard, name, symbol string) *port.TokenMetadata {
	m := &port.TokenMetadata{
		Address: addr, Standard: std, Name: name, Symbol: symbol,
		DetectedAt: 7, CreatedAt: time.Unix(int64(baseTime), 123).UTC(), UpdatedAt: time.Unix(int64(baseTime)+60, 456).UTC(),
		SupportsERC165: std != port.TokenStandardERC20, SupportsMetadata: true,
	}
	if std == port.TokenStandardERC20 {
		m.Decimals = 18
		m.TotalSupply, _ = new(big.Int).SetString("1000000000000000000000000", 10)
	} else {
		m.BaseURI = "ipfs://base/"
		m.SupportsEnumerable = std == port.TokenStandardERC721
	}
	return m
}

// tokensMetadataSet returns the metadata fixture: two ERC-20 tokens, one
// ERC-721 and one ERC-1155.
func tokensMetadataSet() []*port.TokenMetadata {
	return []*port.TokenMetadata{
		tokensMetadata(tokensERC20a, port.TokenStandardERC20, "Stable Coin", "USDX"),
		tokensMetadata(tokensERC20b, port.TokenStandardERC20, "Wrapped Ether", "WETH"),
		tokensMetadata(tokensERC721, port.TokenStandardERC721, "Punks", "PUNK"),
		tokensMetadata(tokensERC1155, port.TokenStandardERC1155, "Items", "ITEM"),
	}
}

// tokensSaveAll stores metadata through the port.
func tokensSaveAll(t *testing.T, s tokenMetadataStore, ms []*port.TokenMetadata) {
	t.Helper()
	for _, m := range ms {
		require.NoError(t, s.SaveTokenMetadata(context.Background(), m))
	}
}

// tokensAddresses returns the addresses of metadata records.
func tokensAddresses(ms []*port.TokenMetadata) []common.Address {
	out := make([]common.Address, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Address)
	}
	return out
}

// assertTokenMetadata compares two metadata records.
func assertTokenMetadata(t *testing.T, want, got *port.TokenMetadata) {
	t.Helper()
	require.NotNil(t, got)
	assert.True(t, want.CreatedAt.Equal(got.CreatedAt), "createdAt %v != %v", want.CreatedAt, got.CreatedAt)
	assert.True(t, want.UpdatedAt.Equal(got.UpdatedAt), "updatedAt %v != %v", want.UpdatedAt, got.UpdatedAt)
	if want.TotalSupply == nil {
		assert.Nil(t, got.TotalSupply)
	} else {
		require.NotNil(t, got.TotalSupply)
		assert.Zero(t, want.TotalSupply.Cmp(got.TotalSupply), "totalSupply %s != %s", want.TotalSupply, got.TotalSupply)
	}
	w, g := *want, *got
	w.CreatedAt, w.UpdatedAt, w.TotalSupply = time.Time{}, time.Time{}, nil
	g.CreatedAt, g.UpdatedAt, g.TotalSupply = time.Time{}, time.Time{}, nil
	assert.Equal(t, w, g)
}

// testTokenMetadata checks TokenMetadataReader and TokenMetadataWriter:
// metadata comes back as saved, a missing token is port.ErrNotFound, tokens
// are listed and counted by standard (all of them for an empty standard)
// with limit/offset pagination, SearchTokens matches names and symbols
// case-insensitively, and saving or deleting a token updates every reader.
func testTokenMetadata(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("NotFoundOnEmptyStore", func(t *testing.T) {
		s := open[tokenMetadataStore](t, newStore)
		_, err := s.GetTokenMetadata(ctx, tokensERC20a)
		assert.ErrorIs(t, err, port.ErrNotFound)
		list, err := s.ListTokensByStandard(ctx, "", 10, 0)
		require.NoError(t, err)
		assert.Empty(t, list)
		n, err := s.GetTokensCount(ctx, "")
		require.NoError(t, err)
		assert.Equal(t, 0, n)
		found, err := s.SearchTokens(ctx, "usdx", 10)
		require.NoError(t, err)
		assert.Empty(t, found)
	})

	t.Run("RoundTrip", func(t *testing.T) {
		s := open[tokenMetadataStore](t, newStore)
		ms := tokensMetadataSet()
		noSupply := tokensMetadata(tokensUnknown, port.TokenStandardERC20, "", "")
		noSupply.TotalSupply = nil
		tokensSaveAll(t, s, append(ms, noSupply))
		for _, want := range append(ms, noSupply) {
			got, err := s.GetTokenMetadata(ctx, want.Address)
			require.NoError(t, err, want.Address.Hex())
			assertTokenMetadata(t, want, got)
		}
	})

	t.Run("ListAndCountByStandard", func(t *testing.T) {
		s := open[tokenMetadataStore](t, newStore)
		ms := tokensMetadataSet()
		tokensSaveAll(t, s, ms)
		cases := []struct {
			std  port.TokenStandard
			want []common.Address
		}{
			{port.TokenStandardERC20, []common.Address{tokensERC20a, tokensERC20b}},
			{port.TokenStandardERC721, []common.Address{tokensERC721}},
			{port.TokenStandardERC1155, []common.Address{tokensERC1155}},
			{"", tokensAddresses(ms)},
		}
		for _, tc := range cases {
			list, err := s.ListTokensByStandard(ctx, tc.std, 10, 0)
			require.NoError(t, err, tc.std)
			assert.ElementsMatch(t, tc.want, tokensAddresses(list), "standard %q", tc.std)
			n, err := s.GetTokensCount(ctx, tc.std)
			require.NoError(t, err, tc.std)
			assert.Equal(t, len(tc.want), n, "standard %q", tc.std)
		}
		list, err := s.ListTokensByStandard(ctx, port.TokenStandardERC721, 10, 0)
		require.NoError(t, err)
		require.Len(t, list, 1)
		assertTokenMetadata(t, ms[2], list[0])
	})

	t.Run("UnknownStandardIsAStandard", func(t *testing.T) {
		knownDefect(t, "ListTokensByStandard and GetTokensCount treat the UNKNOWN standard as no filter")
		s := open[tokenMetadataStore](t, newStore)
		tokensSaveAll(t, s, tokensMetadataSet())
		tokensSaveAll(t, s, []*port.TokenMetadata{tokensMetadata(tokensUnknown, port.TokenStandardUnknown, "Odd", "ODD")})
		list, err := s.ListTokensByStandard(ctx, port.TokenStandardUnknown, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []common.Address{tokensUnknown}, tokensAddresses(list))
		n, err := s.GetTokensCount(ctx, port.TokenStandardUnknown)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
	})

	t.Run("Pagination", func(t *testing.T) {
		s := open[tokenMetadataStore](t, newStore)
		ms := tokensMetadataSet()
		tokensSaveAll(t, s, ms)
		all, err := s.ListTokensByStandard(ctx, "", 10, 0)
		require.NoError(t, err)
		require.Len(t, all, len(ms))

		var paged []common.Address
		for offset := 0; offset < len(ms); offset += 3 {
			page, err := s.ListTokensByStandard(ctx, "", 3, offset)
			require.NoError(t, err)
			paged = append(paged, tokensAddresses(page)...)
		}
		assert.Equal(t, tokensAddresses(all), paged, "pages follow the order of the full list")

		page, err := s.ListTokensByStandard(ctx, port.TokenStandardERC20, 1, 1)
		require.NoError(t, err)
		assert.Len(t, page, 1)
		page, err = s.ListTokensByStandard(ctx, "", 3, len(ms))
		require.NoError(t, err)
		assert.Empty(t, page, "offset past the end")
		page, err = s.ListTokensByStandard(ctx, "", 0, 0)
		require.NoError(t, err)
		assert.Len(t, page, len(ms), "limit 0 is no limit")
	})

	t.Run("SearchTokens", func(t *testing.T) {
		s := open[tokenMetadataStore](t, newStore)
		tokensSaveAll(t, s, tokensMetadataSet())
		cases := []struct {
			query string
			want  []common.Address
		}{
			{"Stable Coin", []common.Address{tokensERC20a}},
			{"stable coin", []common.Address{tokensERC20a}},
			{"usdx", []common.Address{tokensERC20a}},
			{"WeTh", []common.Address{tokensERC20b}},
			{"nothing", nil},
		}
		for _, tc := range cases {
			found, err := s.SearchTokens(ctx, tc.query, 10)
			require.NoError(t, err, tc.query)
			assert.ElementsMatch(t, tc.want, tokensAddresses(found), tc.query)
		}
		found, err := s.SearchTokens(ctx, "", 10)
		require.NoError(t, err)
		assert.Empty(t, found, "an empty query matches nothing")
	})

	t.Run("SearchTokensOncePerToken", func(t *testing.T) {
		s := open[tokenMetadataStore](t, newStore)
		tokensSaveAll(t, s, []*port.TokenMetadata{
			tokensMetadata(tokensERC20a, port.TokenStandardERC20, "Gold", "GOLD"),
			tokensMetadata(tokensERC20b, port.TokenStandardERC20, "gold", "GLD"),
			tokensMetadata(tokensERC721, port.TokenStandardERC721, "Nuggets", "gold"),
		})
		found, err := s.SearchTokens(ctx, "gold", 10)
		require.NoError(t, err)
		assert.ElementsMatch(t, []common.Address{tokensERC20a, tokensERC20b, tokensERC721}, tokensAddresses(found),
			"a token matching by name and symbol is returned once")
		found, err = s.SearchTokens(ctx, "gold", 2)
		require.NoError(t, err)
		assert.Len(t, found, 2, "limit caps the results")
	})

	t.Run("SearchTokensPartialMatch", func(t *testing.T) {
		knownDefect(t, "SearchTokens matches only a whole name or symbol, not part of one")
		s := open[tokenMetadataStore](t, newStore)
		tokensSaveAll(t, s, tokensMetadataSet())
		for _, q := range []string{"stab", "coin", "able co", "usd", "SDX"} {
			found, err := s.SearchTokens(ctx, q, 10)
			require.NoError(t, err, q)
			assert.Equal(t, []common.Address{tokensERC20a}, tokensAddresses(found), q)
		}
	})

	t.Run("SaveUpdates", func(t *testing.T) {
		s := open[tokenMetadataStore](t, newStore)
		tokensSaveAll(t, s, tokensMetadataSet())
		renamed := tokensMetadata(tokensERC20a, port.TokenStandardERC20, "Renamed", "RNM")
		renamed.Decimals = 6
		require.NoError(t, s.SaveTokenMetadata(ctx, renamed))

		got, err := s.GetTokenMetadata(ctx, tokensERC20a)
		require.NoError(t, err)
		assertTokenMetadata(t, renamed, got)
		for _, q := range []string{"stable coin", "usdx"} {
			found, err := s.SearchTokens(ctx, q, 10)
			require.NoError(t, err)
			assert.Empty(t, found, "the old %q no longer matches", q)
		}
		found, err := s.SearchTokens(ctx, "rnm", 10)
		require.NoError(t, err)
		assert.Equal(t, []common.Address{tokensERC20a}, tokensAddresses(found))
		n, err := s.GetTokensCount(ctx, "")
		require.NoError(t, err)
		assert.Equal(t, 4, n, "an update does not add a token")
		n, err = s.GetTokensCount(ctx, port.TokenStandardERC20)
		require.NoError(t, err)
		assert.Equal(t, 2, n)
	})

	t.Run("SaveChangesStandard", func(t *testing.T) {
		knownDefect(t, "saving a token under another standard keeps it listed and counted under the old one")
		s := open[tokenMetadataStore](t, newStore)
		tokensSaveAll(t, s, tokensMetadataSet())
		require.NoError(t, s.SaveTokenMetadata(ctx, tokensMetadata(tokensERC20b, port.TokenStandardERC721, "Wrapped Ether", "WETH")))
		list, err := s.ListTokensByStandard(ctx, port.TokenStandardERC20, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []common.Address{tokensERC20a}, tokensAddresses(list))
		n, err := s.GetTokensCount(ctx, port.TokenStandardERC20)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		list, err = s.ListTokensByStandard(ctx, port.TokenStandardERC721, 10, 0)
		require.NoError(t, err)
		assert.ElementsMatch(t, []common.Address{tokensERC721, tokensERC20b}, tokensAddresses(list))
	})

	t.Run("Delete", func(t *testing.T) {
		s := open[tokenMetadataStore](t, newStore)
		tokensSaveAll(t, s, tokensMetadataSet())
		require.NoError(t, s.DeleteTokenMetadata(ctx, tokensERC20a))

		_, err := s.GetTokenMetadata(ctx, tokensERC20a)
		assert.ErrorIs(t, err, port.ErrNotFound)
		list, err := s.ListTokensByStandard(ctx, port.TokenStandardERC20, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []common.Address{tokensERC20b}, tokensAddresses(list))
		n, err := s.GetTokensCount(ctx, "")
		require.NoError(t, err)
		assert.Equal(t, 3, n)
		found, err := s.SearchTokens(ctx, "usdx", 10)
		require.NoError(t, err)
		assert.Empty(t, found)

		assert.NoError(t, s.DeleteTokenMetadata(ctx, tokensERC20a), "deleting a missing token is not an error")
	})

	t.Run("RejectsNil", func(t *testing.T) {
		knownDefect(t, "SaveTokenMetadata(nil) panics instead of returning an error")
		s := open[tokenMetadataStore](t, newStore)
		var err error
		require.NotPanics(t, func() { err = s.SaveTokenMetadata(ctx, nil) })
		assert.Error(t, err)
	})
}

// Holders of the token holder fixtures.
var (
	tokensHolder1 = common.HexToAddress("0x0000000000000000000000000000000000001001")
	tokensHolder2 = common.HexToAddress("0x0000000000000000000000000000000000001002")
	tokensHolder3 = common.HexToAddress("0x0000000000000000000000000000000000001003")
	tokensHolder4 = common.HexToAddress("0x0000000000000000000000000000000000001004")
)

// tokensHolder returns a holder record.
func tokensHolder(token, holder common.Address, balance int64, block uint64) *port.TokenHolder {
	return &port.TokenHolder{TokenAddress: token, HolderAddress: holder, Balance: big.NewInt(balance), LastUpdatedAt: block}
}

// tokensHolderAddrs returns the holder addresses of records.
func tokensHolderAddrs(hs []*port.TokenHolder) []common.Address {
	out := make([]common.Address, 0, len(hs))
	for _, h := range hs {
		out = append(out, h.HolderAddress)
	}
	return out
}

// tokensHolderTokens returns the token addresses of records.
func tokensHolderTokens(hs []*port.TokenHolder) []common.Address {
	out := make([]common.Address, 0, len(hs))
	for _, h := range hs {
		out = append(out, h.TokenAddress)
	}
	return out
}

// assertTokenHolder compares two holder records.
func assertTokenHolder(t *testing.T, want, got *port.TokenHolder) {
	t.Helper()
	require.NotNil(t, got)
	assert.Equal(t, want.TokenAddress, got.TokenAddress)
	assert.Equal(t, want.HolderAddress, got.HolderAddress)
	require.NotNil(t, got.Balance)
	assert.Zero(t, want.Balance.Cmp(got.Balance), "balance %s != %s", want.Balance, got.Balance)
	assert.Equal(t, want.LastUpdatedAt, got.LastUpdatedAt)
}

// assertTokenBalance checks one holder's balance; want nil means no balance.
func assertTokenBalance(t *testing.T, s tokenHolderStore, token, holder common.Address, want *big.Int) {
	t.Helper()
	got, err := s.GetTokenBalance(context.Background(), token, holder)
	if want == nil {
		assert.ErrorIs(t, err, port.ErrNotFound, "holder %s has no balance", holder.Hex())
		return
	}
	require.NoError(t, err)
	assert.Zero(t, want.Cmp(got), "balance of %s: %s != %s", holder.Hex(), got, want)
}

// testTokenHolderIndex checks TokenHolderIndexReader and
// TokenHolderIndexWriter: holders are listed by balance (largest first) with
// limit/offset pagination, a zero balance removes the holder, the holder
// count follows the holders, a missing balance is port.ErrNotFound, and an
// ERC-20 transfer moves balances (mints and burns use the zero address) and
// updates the token's statistics.
func testTokenHolderIndex(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("EmptyStore", func(t *testing.T) {
		s := open[tokenHolderStore](t, newStore)
		hs, err := s.GetTokenHolders(ctx, tokensERC20a, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, hs)
		n, err := s.GetTokenHolderCount(ctx, tokensERC20a)
		require.NoError(t, err)
		assert.Equal(t, 0, n)
		assertTokenBalance(t, s, tokensERC20a, tokensHolder1, nil)
		hs, err = s.GetHolderTokens(ctx, tokensHolder1, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, hs)
	})

	t.Run("MissingStatsAreNil", func(t *testing.T) {
		// The port documents a nil result; Pebble and its callers also
		// report port.ErrNotFound, which the contract accepts.
		s := open[tokenHolderStore](t, newStore)
		stats, err := s.GetTokenHolderStats(ctx, tokensERC20a)
		if err != nil {
			assert.ErrorIs(t, err, port.ErrNotFound)
		}
		assert.Nil(t, stats)
	})

	t.Run("RoundTrip", func(t *testing.T) {
		s := open[tokenHolderStore](t, newStore)
		want := tokensHolder(tokensERC20a, tokensHolder1, 42, 9)
		require.NoError(t, s.UpdateTokenHolder(ctx, want))
		assertTokenBalance(t, s, tokensERC20a, tokensHolder1, big.NewInt(42))
		assertTokenBalance(t, s, tokensERC20b, tokensHolder1, nil)
		assertTokenBalance(t, s, tokensERC20a, tokensHolder2, nil)

		hs, err := s.GetTokenHolders(ctx, tokensERC20a, 10, 0)
		require.NoError(t, err)
		require.Len(t, hs, 1)
		assertTokenHolder(t, want, hs[0])
		hs, err = s.GetHolderTokens(ctx, tokensHolder1, 10, 0)
		require.NoError(t, err)
		require.Len(t, hs, 1)
		assertTokenHolder(t, want, hs[0])
	})

	t.Run("HoldersByBalanceDescending", func(t *testing.T) {
		s := open[tokenHolderStore](t, newStore)
		big1 := new(big.Int).Lsh(big.NewInt(1), 200) // beyond 64 bits
		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20a, tokensHolder1, 5, 1)))
		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20a, tokensHolder2, 50, 1)))
		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20a, tokensHolder3, 20, 1)))
		require.NoError(t, s.UpdateTokenHolder(ctx, &port.TokenHolder{TokenAddress: tokensERC20a, HolderAddress: tokensHolder4, Balance: big1, LastUpdatedAt: 1}))
		hs, err := s.GetTokenHolders(ctx, tokensERC20a, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []common.Address{tokensHolder4, tokensHolder2, tokensHolder3, tokensHolder1}, tokensHolderAddrs(hs))
		assert.Zero(t, big1.Cmp(hs[0].Balance))

		// A balance change moves the holder.
		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20a, tokensHolder1, 30, 2)))
		hs, err = s.GetTokenHolders(ctx, tokensERC20a, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []common.Address{tokensHolder4, tokensHolder2, tokensHolder1, tokensHolder3}, tokensHolderAddrs(hs),
			"an updated holder is listed once, at its new balance")
		assertTokenHolder(t, tokensHolder(tokensERC20a, tokensHolder1, 30, 2), hs[2])
	})

	t.Run("HoldersPagination", func(t *testing.T) {
		s := open[tokenHolderStore](t, newStore)
		for i, h := range []common.Address{tokensHolder1, tokensHolder2, tokensHolder3, tokensHolder4} {
			require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20a, h, int64(10*(i+1)), 1)))
		}
		want := []common.Address{tokensHolder4, tokensHolder3, tokensHolder2, tokensHolder1}
		hs, err := s.GetTokenHolders(ctx, tokensERC20a, 2, 0)
		require.NoError(t, err)
		assert.Equal(t, want[:2], tokensHolderAddrs(hs))
		hs, err = s.GetTokenHolders(ctx, tokensERC20a, 2, 2)
		require.NoError(t, err)
		assert.Equal(t, want[2:], tokensHolderAddrs(hs))
		hs, err = s.GetTokenHolders(ctx, tokensERC20a, 3, 3)
		require.NoError(t, err)
		assert.Equal(t, want[3:], tokensHolderAddrs(hs), "the last page is short")
		hs, err = s.GetTokenHolders(ctx, tokensERC20a, 2, 4)
		require.NoError(t, err)
		assert.Empty(t, hs, "offset past the end")
		hs, err = s.GetTokenHolders(ctx, tokensERC20a, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, want, tokensHolderAddrs(hs), "limit 0 is no limit")
	})

	t.Run("HolderTokens", func(t *testing.T) {
		s := open[tokenHolderStore](t, newStore)
		tokens := []common.Address{tokensERC20b, tokensERC20a, tokensERC721}
		for i, tok := range tokens {
			require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tok, tokensHolder1, int64(i+1), uint64(i))))
		}
		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC1155, tokensHolder2, 1, 1)))

		all, err := s.GetHolderTokens(ctx, tokensHolder1, 10, 0)
		require.NoError(t, err)
		assert.ElementsMatch(t, tokens, tokensHolderTokens(all), "only the holder's own tokens")
		for _, h := range all {
			assert.Equal(t, tokensHolder1, h.HolderAddress)
		}

		var paged []common.Address
		for offset := 0; offset < len(tokens); offset += 2 {
			page, err := s.GetHolderTokens(ctx, tokensHolder1, 2, offset)
			require.NoError(t, err)
			paged = append(paged, tokensHolderTokens(page)...)
		}
		assert.Equal(t, tokensHolderTokens(all), paged, "pages follow the order of the full list")
		page, err := s.GetHolderTokens(ctx, tokensHolder1, 2, len(tokens))
		require.NoError(t, err)
		assert.Empty(t, page, "offset past the end")
		page, err = s.GetHolderTokens(ctx, tokensHolder1, 0, 0)
		require.NoError(t, err)
		assert.Len(t, page, len(tokens), "limit 0 is no limit")
	})

	t.Run("ZeroBalanceRemovesHolder", func(t *testing.T) {
		s := open[tokenHolderStore](t, newStore)
		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20a, tokensHolder1, 5, 1)))
		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20a, tokensHolder2, 7, 1)))
		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20a, tokensHolder1, 0, 2)))
		require.NoError(t, s.UpdateTokenHolder(ctx, &port.TokenHolder{TokenAddress: tokensERC20a, HolderAddress: tokensHolder2, LastUpdatedAt: 2}))

		assertTokenBalance(t, s, tokensERC20a, tokensHolder1, nil)
		assertTokenBalance(t, s, tokensERC20a, tokensHolder2, nil)
		hs, err := s.GetTokenHolders(ctx, tokensERC20a, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, hs)
		hs, err = s.GetHolderTokens(ctx, tokensHolder1, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, hs)
		n, err := s.GetTokenHolderCount(ctx, tokensERC20a)
		require.NoError(t, err)
		assert.Equal(t, 0, n)

		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20a, tokensHolder3, 0, 3)),
			"a zero balance for an unknown holder is accepted")
		assertTokenBalance(t, s, tokensERC20a, tokensHolder3, nil)
	})

	t.Run("HolderCount", func(t *testing.T) {
		s := open[tokenHolderStore](t, newStore)
		count := func() int {
			t.Helper()
			n, err := s.GetTokenHolderCount(ctx, tokensERC20a)
			require.NoError(t, err)
			return n
		}
		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20a, tokensHolder1, 5, 1)))
		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20a, tokensHolder2, 5, 1)))
		assert.Equal(t, 2, count())
		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20a, tokensHolder1, 9, 2)))
		assert.Equal(t, 2, count(), "a balance change is not a new holder")
		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20b, tokensHolder3, 5, 2)))
		assert.Equal(t, 2, count(), "holders of another token are not counted")
		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20a, tokensHolder2, 0, 3)))
		assert.Equal(t, 1, count())
		require.NoError(t, s.UpdateTokenHolder(ctx, tokensHolder(tokensERC20a, tokensHolder2, 0, 4)))
		assert.Equal(t, 1, count(), "removing a removed holder changes nothing")
	})

	t.Run("Stats", func(t *testing.T) {
		s := open[tokenHolderStore](t, newStore)
		want := &port.TokenHolderStats{TokenAddress: tokensERC20a, HolderCount: 3, TransferCount: 12, LastActivityAt: 40}
		require.NoError(t, s.UpdateTokenHolderStats(ctx, want))
		got, err := s.GetTokenHolderStats(ctx, tokensERC20a)
		require.NoError(t, err)
		assert.Equal(t, want, got)

		want = &port.TokenHolderStats{TokenAddress: tokensERC20a, HolderCount: 4, TransferCount: 13, LastActivityAt: 41}
		require.NoError(t, s.UpdateTokenHolderStats(ctx, want))
		got, err = s.GetTokenHolderStats(ctx, tokensERC20a)
		require.NoError(t, err)
		assert.Equal(t, want, got, "stats are replaced")
		n, err := s.GetTokenHolderCount(ctx, tokensERC20a)
		require.NoError(t, err)
		assert.Equal(t, 4, n, "the holder count follows the recorded stats")

		stats, err := s.GetTokenHolderStats(ctx, tokensERC20b)
		if err != nil {
			assert.ErrorIs(t, err, port.ErrNotFound)
		}
		assert.Nil(t, stats, "stats are per token")
	})

	t.Run("ProcessERC20Transfer", func(t *testing.T) {
		s := open[tokenHolderStore](t, newStore)
		zero := common.Address{}
		transfer := func(from, to common.Address, value int64, block uint64) {
			t.Helper()
			require.NoError(t, s.ProcessERC20TransferForHolders(ctx, &port.ERC20Transfer{
				ContractAddress: tokensERC20a, From: from, To: to, Value: big.NewInt(value),
				TransactionHash: fixtureHash("erc20", block), BlockNumber: block,
			}))
		}
		transfer(zero, tokensHolder1, 100, 1) // mint
		transfer(tokensHolder1, tokensHolder2, 30, 2)
		transfer(tokensHolder2, tokensHolder2, 10, 3) // to self
		transfer(tokensHolder2, zero, 30, 4)          // burn

		assertTokenBalance(t, s, tokensERC20a, tokensHolder1, big.NewInt(70))
		assertTokenBalance(t, s, tokensERC20a, tokensHolder2, nil)
		assertTokenBalance(t, s, tokensERC20a, zero, nil)
		hs, err := s.GetTokenHolders(ctx, tokensERC20a, 10, 0)
		require.NoError(t, err)
		require.Len(t, hs, 1, "the zero address is never a holder")
		assertTokenHolder(t, tokensHolder(tokensERC20a, tokensHolder1, 70, 2), hs[0])

		stats, err := s.GetTokenHolderStats(ctx, tokensERC20a)
		require.NoError(t, err)
		assert.Equal(t, &port.TokenHolderStats{TokenAddress: tokensERC20a, HolderCount: 1, TransferCount: 4, LastActivityAt: 4}, stats)
		n, err := s.GetTokenHolderCount(ctx, tokensERC20a)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
	})

	t.Run("ProcessERC20TransferOverdraw", func(t *testing.T) {
		// A sender whose earlier balance was not indexed never goes
		// negative; the receiver gets the full value.
		s := open[tokenHolderStore](t, newStore)
		require.NoError(t, s.ProcessERC20TransferForHolders(ctx, &port.ERC20Transfer{
			ContractAddress: tokensERC20a, From: tokensHolder1, To: tokensHolder2, Value: big.NewInt(5), BlockNumber: 1,
		}))
		assertTokenBalance(t, s, tokensERC20a, tokensHolder1, nil)
		assertTokenBalance(t, s, tokensERC20a, tokensHolder2, big.NewInt(5))
	})

	t.Run("RejectsNil", func(t *testing.T) {
		knownDefect(t, "UpdateTokenHolder, UpdateTokenHolderStats and ProcessERC20TransferForHolders panic on nil instead of returning an error")
		s := open[tokenHolderStore](t, newStore)
		var err error
		require.NotPanics(t, func() { err = s.UpdateTokenHolder(ctx, nil) })
		assert.Error(t, err)
		require.NotPanics(t, func() { err = s.UpdateTokenHolderStats(ctx, nil) })
		assert.Error(t, err)
		require.NotPanics(t, func() { err = s.ProcessERC20TransferForHolders(ctx, nil) })
		assert.Error(t, err)
	})
}
