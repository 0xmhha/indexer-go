package jsonrpc

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Page methods of the test doubles: they read by offset and return no cursor.

func (m *mockHistoricalStorage) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *port.TransactionFilter, page port.Page) ([]*port.TransactionWithReceipt, string, error) {
	items, err := m.offsetGetTransactionsByAddressFiltered(ctx, addr, filter, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithNonNotFoundErrors) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *port.TransactionFilter, page port.Page) ([]*port.TransactionWithReceipt, string, error) {
	items, err := m.offsetGetTransactionsByAddressFiltered(ctx, addr, filter, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *port.TransactionFilter, page port.Page) ([]*port.TransactionWithReceipt, string, error) {
	items, err := m.offsetGetTransactionsByAddressFiltered(ctx, addr, filter, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *port.TransactionFilter, page port.Page) ([]*port.TransactionWithReceipt, string, error) {
	items, err := m.offsetGetTransactionsByAddressFiltered(ctx, addr, filter, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockHistoricalStorage) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, page port.Page) ([]port.BalanceSnapshot, string, error) {
	items, err := m.offsetGetBalanceHistory(ctx, addr, fromBlock, toBlock, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithNonNotFoundErrors) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, page port.Page) ([]port.BalanceSnapshot, string, error) {
	items, err := m.offsetGetBalanceHistory(ctx, addr, fromBlock, toBlock, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, page port.Page) ([]port.BalanceSnapshot, string, error) {
	items, err := m.offsetGetBalanceHistory(ctx, addr, fromBlock, toBlock, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, page port.Page) ([]port.BalanceSnapshot, string, error) {
	items, err := m.offsetGetBalanceHistory(ctx, addr, fromBlock, toBlock, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockHistoricalStorage) ListVerifiedContracts(ctx context.Context, page port.Page) ([]common.Address, string, error) {
	items, err := m.offsetListVerifiedContracts(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockAddressIndexStorage) ListVerifiedContracts(ctx context.Context, page port.Page) ([]common.Address, string, error) {
	items, err := m.offsetListVerifiedContracts(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithNonNotFoundErrors) ListVerifiedContracts(ctx context.Context, page port.Page) ([]common.Address, string, error) {
	items, err := m.offsetListVerifiedContracts(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) ListVerifiedContracts(ctx context.Context, page port.Page) ([]common.Address, string, error) {
	items, err := m.offsetListVerifiedContracts(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) ListVerifiedContracts(ctx context.Context, page port.Page) ([]common.Address, string, error) {
	items, err := m.offsetListVerifiedContracts(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockHistoricalStorage) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, page port.Page) ([]*model.Block, string, error) {
	items, err := m.offsetGetBlocksByTimeRange(ctx, fromTime, toTime, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithNonNotFoundErrors) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, page port.Page) ([]*model.Block, string, error) {
	items, err := m.offsetGetBlocksByTimeRange(ctx, fromTime, toTime, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, page port.Page) ([]*model.Block, string, error) {
	items, err := m.offsetGetBlocksByTimeRange(ctx, fromTime, toTime, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, page port.Page) ([]*model.Block, string, error) {
	items, err := m.offsetGetBlocksByTimeRange(ctx, fromTime, toTime, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithNonNotFoundErrors) ListTokensByStandard(ctx context.Context, standard port.TokenStandard, page port.Page) ([]*port.TokenMetadata, string, error) {
	items, err := m.offsetListTokensByStandard(ctx, standard, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) ListTokensByStandard(ctx context.Context, standard port.TokenStandard, page port.Page) ([]*port.TokenMetadata, string, error) {
	items, err := m.offsetListTokensByStandard(ctx, standard, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) ListTokensByStandard(ctx context.Context, standard port.TokenStandard, page port.Page) ([]*port.TokenMetadata, string, error) {
	items, err := m.offsetListTokensByStandard(ctx, standard, page.Limit, page.Offset)
	return items, "", err
}
