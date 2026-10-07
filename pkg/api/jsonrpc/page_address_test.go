package jsonrpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Page wrappers of the address index test doubles (the doubles page by
// offset and return no cursor).

func (m *mockAddressIndexStorage) GetContractsByCreator(ctx context.Context, creator common.Address, page port.Page) ([]common.Address, string, error) {
	items, err := m.offsetGetContractsByCreator(ctx, creator, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockAddressIndexStorage) ListContracts(ctx context.Context, page port.Page) ([]*port.ContractCreation, string, error) {
	items, err := m.offsetListContracts(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockAddressIndexStorage) GetInternalTransactionsByAddress(ctx context.Context, address common.Address, isFrom bool, page port.Page) ([]*port.InternalTransaction, string, error) {
	items, err := m.offsetGetInternalTransactionsByAddress(ctx, address, isFrom, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockAddressIndexStorage) GetERC20TransfersByToken(ctx context.Context, tokenAddress common.Address, page port.Page) ([]*port.ERC20Transfer, string, error) {
	items, err := m.offsetGetERC20TransfersByToken(ctx, tokenAddress, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockAddressIndexStorage) GetERC20TransfersByAddress(ctx context.Context, address common.Address, isFrom bool, page port.Page) ([]*port.ERC20Transfer, string, error) {
	items, err := m.offsetGetERC20TransfersByAddress(ctx, address, isFrom, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockAddressIndexStorage) GetERC721TransfersByToken(ctx context.Context, tokenAddress common.Address, page port.Page) ([]*port.ERC721Transfer, string, error) {
	items, err := m.offsetGetERC721TransfersByToken(ctx, tokenAddress, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockAddressIndexStorage) GetERC721TransfersByAddress(ctx context.Context, address common.Address, isFrom bool, page port.Page) ([]*port.ERC721Transfer, string, error) {
	items, err := m.offsetGetERC721TransfersByAddress(ctx, address, isFrom, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockAddressIndexStorage) GetNFTsByOwner(ctx context.Context, owner common.Address, page port.Page) ([]*port.NFTOwnership, string, error) {
	items, err := m.offsetGetNFTsByOwner(ctx, owner, page.Limit, page.Offset)
	return items, "", err
}

// cursorAddressStorage pages ERC20 transfers by cursor: it records the page
// it was asked for, returns the cursor "next" and rejects the cursor "bad".
type cursorAddressStorage struct {
	*mockAddressIndexStorage
	got port.Page
}

func (m *cursorAddressStorage) GetERC20TransfersByToken(_ context.Context, _ common.Address, page port.Page) ([]*port.ERC20Transfer, string, error) {
	m.got = page
	if page.After == "bad" {
		return nil, "", port.ErrInvalidCursor
	}
	return []*port.ERC20Transfer{}, "next", nil
}

// TestAddressListAfterParam: list methods pass `after` to storage, return
// the next page's cursor as nextCursor and reject an invalid cursor as an
// invalid parameter.
func TestAddressListAfterParam(t *testing.T) {
	ctx := context.Background()
	store := &cursorAddressStorage{mockAddressIndexStorage: &mockAddressIndexStorage{mockStorage: &mockStorage{}}}
	server := NewServer(store, zap.NewNop())

	result, rpcErr := server.HandleMethodDirect(ctx, "getERC20TransfersByToken", json.RawMessage(`{"token": "0x01", "limit": 5, "offset": 3, "after": "prev"}`))
	require.Nil(t, rpcErr)
	assert.Equal(t, port.Page{After: "prev", Limit: 5, Offset: 3}, store.got)
	assert.Equal(t, "next", result.(map[string]interface{})["nextCursor"])

	_, rpcErr = server.HandleMethodDirect(ctx, "getERC20TransfersByToken", json.RawMessage(`{"token": "0x01", "after": "bad"}`))
	require.NotNil(t, rpcErr)
	assert.Equal(t, InvalidParams, rpcErr.Code)
	assert.Equal(t, "invalid pagination cursor", rpcErr.Message)

	// A list with no further page returns a null nextCursor.
	result, rpcErr = server.HandleMethodDirect(ctx, "getERC20TransfersByAddress", json.RawMessage(`{"address": "0x01", "isFrom": true}`))
	require.Nil(t, rpcErr)
	assert.Nil(t, result.(map[string]interface{})["nextCursor"])
}
