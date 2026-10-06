package porttest

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Accounts used only by the address index contract.
var (
	addrToken20  = common.HexToAddress("0x00000000000000000000000000000000000020a1")
	addrOther20  = common.HexToAddress("0x00000000000000000000000000000000000020b2")
	addrToken721 = common.HexToAddress("0x00000000000000000000000000000000000721a1")
	addrContract = []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000c0de01"),
		common.HexToAddress("0x0000000000000000000000000000000000c0de02"),
		common.HexToAddress("0x0000000000000000000000000000000000c0de03"),
	}
)

// testAddressIndex checks AddressIndexReader and AddressIndexWriter: contract
// creations, internal transactions and ERC-20/721 transfers come back as
// they were saved, through every index (creator, block, token, sender,
// recipient, owner) in a stable order with limit/offset pagination; single
// lookups of missing entries are port.ErrNotFound and list lookups are empty.
func testAddressIndex(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("NotFoundOnEmptyStore", func(t *testing.T) {
		s := open[addressIndexStore](t, newStore)
		tx := fixtureHash("tx", 1)

		_, err := s.GetContractCreation(ctx, addrContract[0])
		assert.ErrorIs(t, err, port.ErrNotFound)
		_, err = s.GetERC20Transfer(ctx, tx, 0)
		assert.ErrorIs(t, err, port.ErrNotFound)
		_, err = s.GetERC721Transfer(ctx, tx, 0)
		assert.ErrorIs(t, err, port.ErrNotFound)
		_, err = s.GetERC721Owner(ctx, addrToken721, big.NewInt(1))
		assert.ErrorIs(t, err, port.ErrNotFound)

		n, err := s.GetContractsCount(ctx)
		require.NoError(t, err)
		assert.Zero(t, n)

		creators, err := s.GetContractsByCreator(ctx, addrA, 10, 0)
		addrAssertEmpty(t, "GetContractsByCreator", len(creators), err)
		contracts, err := s.ListContracts(ctx, 10, 0)
		addrAssertEmpty(t, "ListContracts", len(contracts), err)
		internals, err := s.GetInternalTransactions(ctx, tx)
		addrAssertEmpty(t, "GetInternalTransactions", len(internals), err)
		internals, err = s.GetInternalTransactionsByAddress(ctx, addrA, true, 10, 0)
		addrAssertEmpty(t, "GetInternalTransactionsByAddress", len(internals), err)
		t20, err := s.GetERC20TransfersByToken(ctx, addrToken20, 10, 0)
		addrAssertEmpty(t, "GetERC20TransfersByToken", len(t20), err)
		t20, err = s.GetERC20TransfersByAddress(ctx, addrA, false, 10, 0)
		addrAssertEmpty(t, "GetERC20TransfersByAddress", len(t20), err)
		t721, err := s.GetERC721TransfersByToken(ctx, addrToken721, 10, 0)
		addrAssertEmpty(t, "GetERC721TransfersByToken", len(t721), err)
		t721, err = s.GetERC721TransfersByAddress(ctx, addrA, true, 10, 0)
		addrAssertEmpty(t, "GetERC721TransfersByAddress", len(t721), err)
		nfts, err := s.GetNFTsByOwner(ctx, addrA, 10, 0)
		addrAssertEmpty(t, "GetNFTsByOwner", len(nfts), err)
	})

	t.Run("RejectsNil", func(t *testing.T) {
		s := open[addressIndexStore](t, newStore)
		assert.Error(t, s.SaveContractCreation(ctx, nil))
		assert.Error(t, s.SaveERC20Transfer(ctx, nil))
		assert.Error(t, s.SaveERC721Transfer(ctx, nil))
	})

	// Contract c0 and c1 are created by A at blocks 3 and 5, c2 by B at block 4.
	creations := []*port.ContractCreation{
		{ContractAddress: addrContract[0], Creator: addrA, TransactionHash: fixtureHash("create", 3), BlockNumber: 3, Timestamp: baseTime + 36, BytecodeSize: 100},
		{ContractAddress: addrContract[1], Creator: addrA, TransactionHash: fixtureHash("create", 5), BlockNumber: 5, Timestamp: baseTime + 60, BytecodeSize: 200},
		{ContractAddress: addrContract[2], Creator: addrB, TransactionHash: fixtureHash("create", 4), BlockNumber: 4, Timestamp: baseTime + 48, BytecodeSize: 300},
	}

	t.Run("ContractCreations", func(t *testing.T) {
		s := open[addressIndexStore](t, newStore)
		for _, c := range creations {
			require.NoError(t, s.SaveContractCreation(ctx, c))
		}
		for _, want := range creations {
			got, err := s.GetContractCreation(ctx, want.ContractAddress)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		}

		byA, err := s.GetContractsByCreator(ctx, addrA, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []common.Address{addrContract[0], addrContract[1]}, byA, "in block order")
		page, err := s.GetContractsByCreator(ctx, addrA, 1, 1)
		require.NoError(t, err)
		assert.Equal(t, []common.Address{addrContract[1]}, page, "limit and offset")
		page, err = s.GetContractsByCreator(ctx, addrA, 10, 2)
		require.NoError(t, err)
		assert.Empty(t, page, "offset past the end")
		none, err := s.GetContractsByCreator(ctx, unknown, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, none)

		all, err := s.ListContracts(ctx, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []common.Address{addrContract[1], addrContract[2], addrContract[0]}, addrContractAddrs(all),
			"newest deployment first")
		page2, err := s.ListContracts(ctx, 1, 1)
		require.NoError(t, err)
		assert.Equal(t, []common.Address{addrContract[2]}, addrContractAddrs(page2), "limit and offset")
		page2, err = s.ListContracts(ctx, 10, 3)
		require.NoError(t, err)
		assert.Empty(t, page2, "offset past the end")

		n, err := s.GetContractsCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 3, n)
	})

	t.Run("ContractCreationIdempotent", func(t *testing.T) {
		s := open[addressIndexStore](t, newStore)
		require.NoError(t, s.SaveContractCreation(ctx, creations[0]))
		require.NoError(t, s.SaveContractCreation(ctx, creations[0]), "reprocessing a block saves the same creation again")
		n, err := s.GetContractsCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		all, err := s.ListContracts(ctx, 10, 0)
		require.NoError(t, err)
		assert.Len(t, all, 1)
		byA, err := s.GetContractsByCreator(ctx, addrA, 10, 0)
		require.NoError(t, err)
		assert.Len(t, byA, 1)
	})

	// tx1 (block 5) makes three internal calls, tx2 (block 6) one; A is the
	// caller of three of them, in block order tx1[0], tx1[2], tx2[0].
	tx1, tx2 := fixtureHash("itx", 1), fixtureHash("itx", 2)
	internals1 := []*port.InternalTransaction{
		addrInternal(tx1, 5, 0, port.InternalTxTypeCall, addrA, addrB, 7),
		addrInternal(tx1, 5, 1, port.InternalTxTypeDelegateCall, addrB, addrC, 0),
		addrInternal(tx1, 5, 2, port.InternalTxTypeCall, addrA, addrC, 9),
	}
	internals2 := []*port.InternalTransaction{
		addrInternal(tx2, 6, 0, port.InternalTxTypeCall, addrA, addrB, 1),
	}
	saveInternals := func(t *testing.T, s addressIndexStore) {
		t.Helper()
		require.NoError(t, s.SaveInternalTransactions(ctx, tx1, internals1))
		require.NoError(t, s.SaveInternalTransactions(ctx, tx2, internals2))
	}

	t.Run("InternalTransactions", func(t *testing.T) {
		s := open[addressIndexStore](t, newStore)
		saveInternals(t, s)

		got, err := s.GetInternalTransactions(ctx, tx1)
		require.NoError(t, err)
		addrAssertInternals(t, internals1, got)
		got, err = s.GetInternalTransactions(ctx, fixtureHash("itx", 9))
		require.NoError(t, err)
		assert.Empty(t, got)

		for _, c := range []struct {
			name   string
			addr   common.Address
			isFrom bool
			want   []*port.InternalTransaction
		}{
			{"from A", addrA, true, []*port.InternalTransaction{internals1[0], internals1[2], internals2[0]}},
			{"to C", addrC, false, []*port.InternalTransaction{internals1[1], internals1[2]}},
			{"from B", addrB, true, []*port.InternalTransaction{internals1[1]}},
			{"to B", addrB, false, []*port.InternalTransaction{internals1[0], internals2[0]}},
			{"from C", addrC, true, nil},
		} {
			got, err := s.GetInternalTransactionsByAddress(ctx, c.addr, c.isFrom, 10, 0)
			require.NoError(t, err, c.name)
			addrAssertInternals(t, c.want, got)
		}

		got, err = s.GetInternalTransactionsByAddress(ctx, addrA, true, 2, 0)
		require.NoError(t, err)
		addrAssertInternals(t, []*port.InternalTransaction{internals1[0], internals1[2]}, got)

		require.NoError(t, s.SaveInternalTransactions(ctx, fixtureHash("itx", 3), nil), "no internal calls is not an error")
	})

	t.Run("InternalTransactionsOffsetCountsEntries", func(t *testing.T) {
		knownDefect(t, "GetInternalTransactionsByAddress applies offset to transactions but limit to internal calls")
		s := open[addressIndexStore](t, newStore)
		saveInternals(t, s)
		// A's calls are tx1[0], tx1[2], tx2[0]; pages of two must cover all.
		got, err := s.GetInternalTransactionsByAddress(ctx, addrA, true, 2, 2)
		require.NoError(t, err)
		addrAssertInternals(t, []*port.InternalTransaction{internals2[0]}, got)
		got, err = s.GetInternalTransactionsByAddress(ctx, addrA, true, 10, 1)
		require.NoError(t, err)
		addrAssertInternals(t, []*port.InternalTransaction{internals1[2], internals2[0]}, got)
	})

	// t1 and t2 move token20 (blocks 2 and 3), t3 moves other20 in block 3
	// at a lower log index than t2.
	erc20 := []*port.ERC20Transfer{
		{ContractAddress: addrToken20, From: addrA, To: addrB, Value: big.NewInt(100), TransactionHash: fixtureHash("t20", 1), BlockNumber: 2, LogIndex: 0, Timestamp: baseTime + 24},
		{ContractAddress: addrToken20, From: addrB, To: addrA, Value: big.NewInt(40), TransactionHash: fixtureHash("t20", 2), BlockNumber: 3, LogIndex: 1, Timestamp: baseTime + 36},
		{ContractAddress: addrOther20, From: addrA, To: addrB, Value: big.NewInt(0), TransactionHash: fixtureHash("t20", 3), BlockNumber: 3, LogIndex: 0, Timestamp: baseTime + 36},
	}

	t.Run("ERC20Transfers", func(t *testing.T) {
		s := open[addressIndexStore](t, newStore)
		for _, tr := range erc20 {
			require.NoError(t, s.SaveERC20Transfer(ctx, tr))
		}
		for _, want := range erc20 {
			got, err := s.GetERC20Transfer(ctx, want.TransactionHash, want.LogIndex)
			require.NoError(t, err)
			addrAssertERC20(t, []*port.ERC20Transfer{want}, []*port.ERC20Transfer{got})
		}
		_, err := s.GetERC20Transfer(ctx, erc20[0].TransactionHash, 5)
		assert.ErrorIs(t, err, port.ErrNotFound, "another log index of the same transaction")

		got, err := s.GetERC20TransfersByToken(ctx, addrToken20, 10, 0)
		require.NoError(t, err)
		addrAssertERC20(t, erc20[:2], got)
		got, err = s.GetERC20TransfersByToken(ctx, addrToken20, 1, 1)
		require.NoError(t, err)
		addrAssertERC20(t, erc20[1:2], got)
		got, err = s.GetERC20TransfersByToken(ctx, addrOther20, 10, 0)
		require.NoError(t, err)
		addrAssertERC20(t, erc20[2:], got)
		got, err = s.GetERC20TransfersByToken(ctx, unknown, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, got)

		for _, c := range []struct {
			name   string
			addr   common.Address
			isFrom bool
			want   []*port.ERC20Transfer
		}{
			{"from A", addrA, true, []*port.ERC20Transfer{erc20[0], erc20[2]}},
			{"to A", addrA, false, []*port.ERC20Transfer{erc20[1]}},
			{"from B", addrB, true, []*port.ERC20Transfer{erc20[1]}},
			{"to B", addrB, false, []*port.ERC20Transfer{erc20[0], erc20[2]}},
			{"to unknown", unknown, false, nil},
		} {
			got, err := s.GetERC20TransfersByAddress(ctx, c.addr, c.isFrom, 10, 0)
			require.NoError(t, err, c.name)
			addrAssertERC20(t, c.want, got)
		}
		got, err = s.GetERC20TransfersByAddress(ctx, addrA, true, 1, 1)
		require.NoError(t, err)
		addrAssertERC20(t, erc20[2:], got)
		got, err = s.GetERC20TransfersByAddress(ctx, addrA, true, 10, 2)
		require.NoError(t, err)
		assert.Empty(t, got, "offset past the end")
	})

	t.Run("ERC20TransferIdempotent", func(t *testing.T) {
		s := open[addressIndexStore](t, newStore)
		require.NoError(t, s.SaveERC20Transfer(ctx, erc20[0]))
		require.NoError(t, s.SaveERC20Transfer(ctx, erc20[0]))
		got, err := s.GetERC20TransfersByToken(ctx, addrToken20, 10, 0)
		require.NoError(t, err)
		assert.Len(t, got, 1)
		got, err = s.GetERC20TransfersByAddress(ctx, addrA, true, 10, 0)
		require.NoError(t, err)
		assert.Len(t, got, 1)
	})

	// Token 1 and 2 are minted to A in block 2; token 1 moves to B in block 3.
	zero := common.Address{}
	erc721 := []*port.ERC721Transfer{
		{ContractAddress: addrToken721, From: zero, To: addrA, TokenId: big.NewInt(1), TransactionHash: fixtureHash("t721", 1), BlockNumber: 2, LogIndex: 0, Timestamp: baseTime + 24},
		{ContractAddress: addrToken721, From: zero, To: addrA, TokenId: big.NewInt(2), TransactionHash: fixtureHash("t721", 1), BlockNumber: 2, LogIndex: 1, Timestamp: baseTime + 24},
		{ContractAddress: addrToken721, From: addrA, To: addrB, TokenId: big.NewInt(1), TransactionHash: fixtureHash("t721", 2), BlockNumber: 3, LogIndex: 0, Timestamp: baseTime + 36},
	}

	t.Run("ERC721Transfers", func(t *testing.T) {
		s := open[addressIndexStore](t, newStore)
		for _, tr := range erc721 {
			require.NoError(t, s.SaveERC721Transfer(ctx, tr))
		}
		for _, want := range erc721 {
			got, err := s.GetERC721Transfer(ctx, want.TransactionHash, want.LogIndex)
			require.NoError(t, err)
			addrAssertERC721(t, []*port.ERC721Transfer{want}, []*port.ERC721Transfer{got})
		}
		_, err := s.GetERC721Transfer(ctx, erc721[0].TransactionHash, 7)
		assert.ErrorIs(t, err, port.ErrNotFound)

		got, err := s.GetERC721TransfersByToken(ctx, addrToken721, 10, 0)
		require.NoError(t, err)
		addrAssertERC721(t, erc721, got)
		got, err = s.GetERC721TransfersByToken(ctx, addrToken721, 2, 1)
		require.NoError(t, err)
		addrAssertERC721(t, erc721[1:], got)
		got, err = s.GetERC721TransfersByToken(ctx, unknown, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, got)

		for _, c := range []struct {
			name   string
			addr   common.Address
			isFrom bool
			want   []*port.ERC721Transfer
		}{
			{"from A", addrA, true, erc721[2:]},
			{"to A", addrA, false, erc721[:2]},
			{"to B", addrB, false, erc721[2:]},
			{"from B", addrB, true, nil},
		} {
			got, err := s.GetERC721TransfersByAddress(ctx, c.addr, c.isFrom, 10, 0)
			require.NoError(t, err, c.name)
			addrAssertERC721(t, c.want, got)
		}
		got, err = s.GetERC721TransfersByAddress(ctx, addrA, false, 1, 1)
		require.NoError(t, err)
		addrAssertERC721(t, erc721[1:2], got)
	})

	t.Run("ERC721Ownership", func(t *testing.T) {
		s := open[addressIndexStore](t, newStore)
		for _, tr := range erc721 {
			require.NoError(t, s.SaveERC721Transfer(ctx, tr))
		}
		owner, err := s.GetERC721Owner(ctx, addrToken721, big.NewInt(1))
		require.NoError(t, err)
		assert.Equal(t, addrB, owner, "the latest transfer sets the owner")
		owner, err = s.GetERC721Owner(ctx, addrToken721, big.NewInt(2))
		require.NoError(t, err)
		assert.Equal(t, addrA, owner)
		_, err = s.GetERC721Owner(ctx, addrToken721, big.NewInt(3))
		assert.ErrorIs(t, err, port.ErrNotFound, "never transferred")
		_, err = s.GetERC721Owner(ctx, unknown, big.NewInt(1))
		assert.ErrorIs(t, err, port.ErrNotFound, "another contract")

		nfts, err := s.GetNFTsByOwner(ctx, addrA, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []*port.NFTOwnership{{ContractAddress: addrToken721, TokenId: big.NewInt(2), Owner: addrA}}, addrNormNFTs(nfts),
			"token 1 left A")
		nfts, err = s.GetNFTsByOwner(ctx, addrB, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []*port.NFTOwnership{{ContractAddress: addrToken721, TokenId: big.NewInt(1), Owner: addrB}}, addrNormNFTs(nfts))

		// Burning token 2 removes it from A.
		require.NoError(t, s.SaveERC721Transfer(ctx, &port.ERC721Transfer{
			ContractAddress: addrToken721, From: addrA, To: zero, TokenId: big.NewInt(2),
			TransactionHash: fixtureHash("t721", 3), BlockNumber: 4, LogIndex: 0, Timestamp: baseTime + 48,
		}))
		nfts, err = s.GetNFTsByOwner(ctx, addrA, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, nfts)
	})

	t.Run("NFTsByOwnerPagination", func(t *testing.T) {
		s := open[addressIndexStore](t, newStore)
		for id := int64(1); id <= 3; id++ {
			require.NoError(t, s.SaveERC721Transfer(ctx, &port.ERC721Transfer{
				ContractAddress: addrToken721, From: zero, To: addrC, TokenId: big.NewInt(id),
				TransactionHash: fixtureHash("mint", uint64(id)), BlockNumber: 1, LogIndex: uint(id),
			}))
		}
		all, err := s.GetNFTsByOwner(ctx, addrC, 10, 0)
		require.NoError(t, err)
		all = addrNormNFTs(all)
		require.Len(t, all, 3)
		var ids []int64
		for _, n := range all {
			assert.Equal(t, addrToken721, n.ContractAddress)
			assert.Equal(t, addrC, n.Owner)
			ids = append(ids, n.TokenId.Int64())
		}
		assert.ElementsMatch(t, []int64{1, 2, 3}, ids)

		page, err := s.GetNFTsByOwner(ctx, addrC, 2, 1)
		require.NoError(t, err)
		assert.Equal(t, all[1:3], addrNormNFTs(page), "pages follow the order of the full list")
		page, err = s.GetNFTsByOwner(ctx, addrC, 10, 3)
		require.NoError(t, err)
		assert.Empty(t, page)
	})
}

func addrInternal(tx common.Hash, block uint64, index int, typ string, from, to common.Address, value int64) *port.InternalTransaction {
	return &port.InternalTransaction{
		TransactionHash: tx, BlockNumber: block, Index: index, Type: typ,
		From: from, To: to, Value: big.NewInt(value), Gas: 50_000, GasUsed: 21_000 + uint64(index),
		Input: []byte{0xde, 0xad, byte(index)}, Output: []byte{0x01}, Depth: 1 + index%2,
	}
}

func addrContractAddrs(cs []*port.ContractCreation) []common.Address {
	out := make([]common.Address, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ContractAddress)
	}
	return out
}

// addrAssertInternals compares internal transactions in order; values are
// compared numerically.
func addrAssertInternals(t *testing.T, want, got []*port.InternalTransaction) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		w, g := *want[i], *got[i]
		assert.Zero(t, w.Value.Cmp(g.Value), "value of entry %d", i)
		w.Value, g.Value = nil, nil
		assert.Equal(t, w, g, "entry %d", i)
	}
}

// addrAssertERC20 compares ERC-20 transfers in order.
func addrAssertERC20(t *testing.T, want, got []*port.ERC20Transfer) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		w, g := *want[i], *got[i]
		assert.Zero(t, w.Value.Cmp(g.Value), "value of entry %d", i)
		w.Value, g.Value = nil, nil
		assert.Equal(t, w, g, "entry %d", i)
	}
}

// addrAssertERC721 compares ERC-721 transfers in order.
func addrAssertERC721(t *testing.T, want, got []*port.ERC721Transfer) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		w, g := *want[i], *got[i]
		assert.Zero(t, w.TokenId.Cmp(g.TokenId), "token id of entry %d", i)
		w.TokenId, g.TokenId = nil, nil
		assert.Equal(t, w, g, "entry %d", i)
	}
}

// addrNormNFTs rebuilds token ids so that equal ids compare equal.
func addrNormNFTs(nfts []*port.NFTOwnership) []*port.NFTOwnership {
	out := make([]*port.NFTOwnership, 0, len(nfts))
	for _, n := range nfts {
		c := *n
		if c.TokenId != nil {
			c.TokenId = big.NewInt(c.TokenId.Int64())
		}
		out = append(out, &c)
	}
	return out
}

// addrAssertEmpty checks that a list lookup succeeded with no entries.
func addrAssertEmpty(t *testing.T, method string, n int, err error) {
	t.Helper()
	require.NoError(t, err, method)
	assert.Zero(t, n, method)
}
