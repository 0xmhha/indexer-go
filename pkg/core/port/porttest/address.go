package porttest

import (
	"context"
	"fmt"
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
		common.HexToAddress("0x0000000000000000000000000000000000c0de04"),
	}
)

// testAddressIndex checks AddressIndexReader and AddressIndexWriter: contract
// creations, internal transactions and ERC-20/721 transfers come back as
// they were saved, through every index (creator, block, token, sender,
// recipient, owner) in a stable order with Page pagination (checkPaging); single
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

		first := port.FirstPage(10)
		creators, _, err := s.GetContractsByCreator(ctx, addrA, first)
		addrAssertEmpty(t, "GetContractsByCreator", len(creators), err)
		contracts, _, err := s.ListContracts(ctx, first)
		addrAssertEmpty(t, "ListContracts", len(contracts), err)
		internals, err := s.GetInternalTransactions(ctx, tx)
		addrAssertEmpty(t, "GetInternalTransactions", len(internals), err)
		internals, _, err = s.GetInternalTransactionsByAddress(ctx, addrA, true, first)
		addrAssertEmpty(t, "GetInternalTransactionsByAddress", len(internals), err)
		t20, _, err := s.GetERC20TransfersByToken(ctx, addrToken20, first)
		addrAssertEmpty(t, "GetERC20TransfersByToken", len(t20), err)
		t20, _, err = s.GetERC20TransfersByAddress(ctx, addrA, false, first)
		addrAssertEmpty(t, "GetERC20TransfersByAddress", len(t20), err)
		t721, _, err := s.GetERC721TransfersByToken(ctx, addrToken721, first)
		addrAssertEmpty(t, "GetERC721TransfersByToken", len(t721), err)
		t721, _, err = s.GetERC721TransfersByAddress(ctx, addrA, true, first)
		addrAssertEmpty(t, "GetERC721TransfersByAddress", len(t721), err)
		nfts, _, err := s.GetNFTsByOwner(ctx, addrA, first)
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

		// c3 is a third contract of A, at block 6.
		c3 := &port.ContractCreation{ContractAddress: addrContract[3], Creator: addrA, TransactionHash: fixtureHash("create", 6), BlockNumber: 6, Timestamp: baseTime + 72, BytecodeSize: 400}
		require.NoError(t, s.SaveContractCreation(ctx, c3))

		byCreator := func(creator common.Address) listPage[common.Address] {
			return func(page port.Page) ([]common.Address, string, error) {
				return s.GetContractsByCreator(ctx, creator, page)
			}
		}
		addrKey := func(a common.Address) common.Address { return a }
		checkPaging(t, []common.Address{addrContract[0], addrContract[1], addrContract[3]}, addrKey, byCreator(addrA))
		checkCursorFromOtherList(t, byCreator(addrA), byCreator(addrB))
		none, next, err := s.GetContractsByCreator(ctx, unknown, port.FirstPage(10))
		require.NoError(t, err)
		assert.Empty(t, none)
		assert.Empty(t, next)

		// Newest deployment first.
		list := func(page port.Page) ([]*port.ContractCreation, string, error) { return s.ListContracts(ctx, page) }
		checkPaging(t, []*port.ContractCreation{c3, creations[1], creations[2], creations[0]},
			func(c *port.ContractCreation) common.Address { return c.ContractAddress }, list)
		_, next, err = s.GetContractsByCreator(ctx, addrA, port.FirstPage(1))
		require.NoError(t, err)
		_, _, err = s.ListContracts(ctx, port.Page{After: next, Limit: 1})
		assert.ErrorIs(t, err, port.ErrInvalidCursor, "a creator list cursor is not a ListContracts cursor")

		n, err := s.GetContractsCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 4, n)
	})

	t.Run("ContractCreationIdempotent", func(t *testing.T) {
		s := open[addressIndexStore](t, newStore)
		require.NoError(t, s.SaveContractCreation(ctx, creations[0]))
		require.NoError(t, s.SaveContractCreation(ctx, creations[0]), "reprocessing a block saves the same creation again")
		n, err := s.GetContractsCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		all, _, err := s.ListContracts(ctx, port.FirstPage(10))
		require.NoError(t, err)
		assert.Len(t, all, 1)
		byA, _, err := s.GetContractsByCreator(ctx, addrA, port.FirstPage(10))
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
			got, next, err := s.GetInternalTransactionsByAddress(ctx, c.addr, c.isFrom, port.FirstPage(10))
			require.NoError(t, err, c.name)
			addrAssertInternals(t, c.want, got)
			assert.Empty(t, next, c.name)
		}

		// A page of one ends between tx1[0] and tx1[2], two calls of one
		// transaction: the cursor addresses a call, not an index key.
		byAddr := func(addr common.Address, isFrom bool) listPage[*port.InternalTransaction] {
			return func(page port.Page) ([]*port.InternalTransaction, string, error) {
				return s.GetInternalTransactionsByAddress(ctx, addr, isFrom, page)
			}
		}
		callKey := func(it *port.InternalTransaction) string {
			return fmt.Sprintf("%s/%d", it.TransactionHash.Hex(), it.Index)
		}
		checkPaging(t, []*port.InternalTransaction{internals1[0], internals1[2], internals2[0]}, callKey, byAddr(addrA, true))
		checkCursorFromOtherList(t, byAddr(addrA, true), byAddr(addrB, false))
		checkCursorFromOtherList(t, byAddr(addrA, true), byAddr(addrB, true))

		require.NoError(t, s.SaveInternalTransactions(ctx, fixtureHash("itx", 3), nil), "no internal calls is not an error")
	})

	t.Run("InternalTransactionsOffsetCountsEntries", func(t *testing.T) {
		s := open[addressIndexStore](t, newStore)
		saveInternals(t, s)
		// A's calls are tx1[0], tx1[2], tx2[0]; pages of two must cover all.
		got, _, err := s.GetInternalTransactionsByAddress(ctx, addrA, true, port.Page{Limit: 2, Offset: 2})
		require.NoError(t, err)
		addrAssertInternals(t, []*port.InternalTransaction{internals2[0]}, got)
		got, _, err = s.GetInternalTransactionsByAddress(ctx, addrA, true, port.Page{Limit: 10, Offset: 1})
		require.NoError(t, err)
		addrAssertInternals(t, []*port.InternalTransaction{internals1[2], internals2[0]}, got)
	})

	t.Run("InternalTransactionsResumeAfterAppend", func(t *testing.T) {
		s := open[addressIndexStore](t, newStore)
		saveInternals(t, s)
		// The first page ends inside tx1; a later transaction does not move
		// the rest of the list.
		_, next, err := s.GetInternalTransactionsByAddress(ctx, addrA, true, port.FirstPage(1))
		require.NoError(t, err)
		require.NotEmpty(t, next)
		tx4 := fixtureHash("itx", 4)
		internals4 := []*port.InternalTransaction{addrInternal(tx4, 7, 0, port.InternalTxTypeCall, addrA, addrC, 3)}
		require.NoError(t, s.SaveInternalTransactions(ctx, tx4, internals4))
		got, _, err := s.GetInternalTransactionsByAddress(ctx, addrA, true, port.Page{After: next, Limit: 10})
		require.NoError(t, err)
		addrAssertInternals(t, []*port.InternalTransaction{internals1[2], internals2[0], internals4[0]}, got)
	})

	// t1, t2 and t4 move token20 (blocks 2, 3 and 4), t3 moves other20 in
	// block 3 at a lower log index than t2. A sends t1, t3 and t4.
	erc20 := []*port.ERC20Transfer{
		{ContractAddress: addrToken20, From: addrA, To: addrB, Value: big.NewInt(100), TransactionHash: fixtureHash("t20", 1), BlockNumber: 2, LogIndex: 0, Timestamp: baseTime + 24},
		{ContractAddress: addrToken20, From: addrB, To: addrA, Value: big.NewInt(40), TransactionHash: fixtureHash("t20", 2), BlockNumber: 3, LogIndex: 1, Timestamp: baseTime + 36},
		{ContractAddress: addrOther20, From: addrA, To: addrB, Value: big.NewInt(0), TransactionHash: fixtureHash("t20", 3), BlockNumber: 3, LogIndex: 0, Timestamp: baseTime + 36},
		{ContractAddress: addrToken20, From: addrA, To: addrC, Value: big.NewInt(5), TransactionHash: fixtureHash("t20", 4), BlockNumber: 4, LogIndex: 2, Timestamp: baseTime + 48},
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

		erc20Key := func(tr *port.ERC20Transfer) string {
			return fmt.Sprintf("%s/%d", tr.TransactionHash.Hex(), tr.LogIndex)
		}
		byToken := func(token common.Address) listPage[*port.ERC20Transfer] {
			return func(page port.Page) ([]*port.ERC20Transfer, string, error) {
				return s.GetERC20TransfersByToken(ctx, token, page)
			}
		}
		token20 := []*port.ERC20Transfer{erc20[0], erc20[1], erc20[3]}
		got, _, err := s.GetERC20TransfersByToken(ctx, addrToken20, port.FirstPage(10))
		require.NoError(t, err)
		addrAssertERC20(t, token20, got)
		checkPaging(t, token20, erc20Key, byToken(addrToken20))
		checkCursorFromOtherList(t, byToken(addrToken20), byToken(addrOther20))
		got, _, err = s.GetERC20TransfersByToken(ctx, addrOther20, port.FirstPage(10))
		require.NoError(t, err)
		addrAssertERC20(t, erc20[2:3], got)
		got, _, err = s.GetERC20TransfersByToken(ctx, unknown, port.FirstPage(10))
		require.NoError(t, err)
		assert.Empty(t, got)

		for _, c := range []struct {
			name   string
			addr   common.Address
			isFrom bool
			want   []*port.ERC20Transfer
		}{
			{"from A", addrA, true, []*port.ERC20Transfer{erc20[0], erc20[2], erc20[3]}},
			{"to A", addrA, false, []*port.ERC20Transfer{erc20[1]}},
			{"from B", addrB, true, []*port.ERC20Transfer{erc20[1]}},
			{"to B", addrB, false, []*port.ERC20Transfer{erc20[0], erc20[2]}},
			{"to C", addrC, false, []*port.ERC20Transfer{erc20[3]}},
			{"to unknown", unknown, false, nil},
		} {
			got, _, err := s.GetERC20TransfersByAddress(ctx, c.addr, c.isFrom, port.FirstPage(10))
			require.NoError(t, err, c.name)
			addrAssertERC20(t, c.want, got)
		}
		byAddr := func(addr common.Address, isFrom bool) listPage[*port.ERC20Transfer] {
			return func(page port.Page) ([]*port.ERC20Transfer, string, error) {
				return s.GetERC20TransfersByAddress(ctx, addr, isFrom, page)
			}
		}
		checkPaging(t, []*port.ERC20Transfer{erc20[0], erc20[2], erc20[3]}, erc20Key, byAddr(addrA, true))
		checkCursorFromOtherList(t, byAddr(addrA, true), byAddr(addrA, false))
		checkCursorFromOtherList(t, byAddr(addrA, true), byToken(addrToken20))
	})

	t.Run("ERC20TransferIdempotent", func(t *testing.T) {
		s := open[addressIndexStore](t, newStore)
		require.NoError(t, s.SaveERC20Transfer(ctx, erc20[0]))
		require.NoError(t, s.SaveERC20Transfer(ctx, erc20[0]))
		got, _, err := s.GetERC20TransfersByToken(ctx, addrToken20, port.FirstPage(10))
		require.NoError(t, err)
		assert.Len(t, got, 1)
		got, _, err = s.GetERC20TransfersByAddress(ctx, addrA, true, port.FirstPage(10))
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

		got, _, err := s.GetERC721TransfersByToken(ctx, addrToken721, port.FirstPage(10))
		require.NoError(t, err)
		addrAssertERC721(t, erc721, got)
		got, _, err = s.GetERC721TransfersByToken(ctx, unknown, port.FirstPage(10))
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
			got, _, err := s.GetERC721TransfersByAddress(ctx, c.addr, c.isFrom, port.FirstPage(10))
			require.NoError(t, err, c.name)
			addrAssertERC721(t, c.want, got)
		}

		// Token 3 is minted to A in block 4: A receives three transfers.
		mint3 := &port.ERC721Transfer{ContractAddress: addrToken721, From: zero, To: addrA, TokenId: big.NewInt(3), TransactionHash: fixtureHash("t721", 4), BlockNumber: 4, LogIndex: 0, Timestamp: baseTime + 48}
		require.NoError(t, s.SaveERC721Transfer(ctx, mint3))
		erc721Key := func(tr *port.ERC721Transfer) string {
			return fmt.Sprintf("%s/%d", tr.TransactionHash.Hex(), tr.LogIndex)
		}
		byToken := func(token common.Address) listPage[*port.ERC721Transfer] {
			return func(page port.Page) ([]*port.ERC721Transfer, string, error) {
				return s.GetERC721TransfersByToken(ctx, token, page)
			}
		}
		byAddr := func(addr common.Address, isFrom bool) listPage[*port.ERC721Transfer] {
			return func(page port.Page) ([]*port.ERC721Transfer, string, error) {
				return s.GetERC721TransfersByAddress(ctx, addr, isFrom, page)
			}
		}
		checkPaging(t, append(append([]*port.ERC721Transfer{}, erc721...), mint3), erc721Key, byToken(addrToken721))
		checkCursorFromOtherList(t, byToken(addrToken721), byToken(unknown))
		checkPaging(t, []*port.ERC721Transfer{erc721[0], erc721[1], mint3}, erc721Key, byAddr(addrA, false))
		checkCursorFromOtherList(t, byAddr(addrA, false), byAddr(addrA, true))
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

		nfts, _, err := s.GetNFTsByOwner(ctx, addrA, port.FirstPage(10))
		require.NoError(t, err)
		assert.Equal(t, []*port.NFTOwnership{{ContractAddress: addrToken721, TokenId: big.NewInt(2), Owner: addrA}}, addrNormNFTs(nfts),
			"token 1 left A")
		nfts, _, err = s.GetNFTsByOwner(ctx, addrB, port.FirstPage(10))
		require.NoError(t, err)
		assert.Equal(t, []*port.NFTOwnership{{ContractAddress: addrToken721, TokenId: big.NewInt(1), Owner: addrB}}, addrNormNFTs(nfts))

		// Burning token 2 removes it from A.
		require.NoError(t, s.SaveERC721Transfer(ctx, &port.ERC721Transfer{
			ContractAddress: addrToken721, From: addrA, To: zero, TokenId: big.NewInt(2),
			TransactionHash: fixtureHash("t721", 3), BlockNumber: 4, LogIndex: 0, Timestamp: baseTime + 48,
		}))
		nfts, _, err = s.GetNFTsByOwner(ctx, addrA, port.FirstPage(10))
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
		all, _, err := s.GetNFTsByOwner(ctx, addrC, port.FirstPage(10))
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

		byOwner := func(owner common.Address) listPage[*port.NFTOwnership] {
			return func(page port.Page) ([]*port.NFTOwnership, string, error) {
				return s.GetNFTsByOwner(ctx, owner, page)
			}
		}
		nftKey := func(n *port.NFTOwnership) string { return n.ContractAddress.Hex() + "/" + n.TokenId.String() }
		checkPaging(t, all, nftKey, byOwner(addrC))
		checkCursorFromOtherList(t, byOwner(addrC), byOwner(addrA))

		// The NFT a cursor ended at leaves C: the cursor still continues
		// after it.
		first, next, err := s.GetNFTsByOwner(ctx, addrC, port.FirstPage(1))
		require.NoError(t, err)
		require.Len(t, first, 1)
		require.NoError(t, s.SaveERC721Transfer(ctx, &port.ERC721Transfer{
			ContractAddress: addrToken721, From: addrC, To: addrA, TokenId: first[0].TokenId,
			TransactionHash: fixtureHash("move", 1), BlockNumber: 2, LogIndex: 0,
		}))
		rest, next, err := s.GetNFTsByOwner(ctx, addrC, port.Page{After: next, Limit: 10})
		require.NoError(t, err)
		assert.Equal(t, all[1:], addrNormNFTs(rest))
		assert.Empty(t, next)
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
