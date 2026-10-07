package graphql

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/userop"
)

// Page methods of the UserOperation test doubles; each reads the offset
// page and returns no cursor.

func (m *mockStorageWithErrors) GetUserOpsBySender(ctx context.Context, sender common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	items, err := m.offsetGetUserOpsBySender(ctx, sender, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetUserOpsBySender(ctx context.Context, sender common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	items, err := m.offsetGetUserOpsBySender(ctx, sender, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) GetUserOpsByBundler(ctx context.Context, bundler common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	items, err := m.offsetGetUserOpsByBundler(ctx, bundler, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetUserOpsByBundler(ctx context.Context, bundler common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	items, err := m.offsetGetUserOpsByBundler(ctx, bundler, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) GetUserOpsByPaymaster(ctx context.Context, paymaster common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	items, err := m.offsetGetUserOpsByPaymaster(ctx, paymaster, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetUserOpsByPaymaster(ctx context.Context, paymaster common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	items, err := m.offsetGetUserOpsByPaymaster(ctx, paymaster, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) GetUserOpsByFactory(ctx context.Context, factory common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	items, err := m.offsetGetUserOpsByFactory(ctx, factory, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetUserOpsByFactory(ctx context.Context, factory common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	items, err := m.offsetGetUserOpsByFactory(ctx, factory, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) ListBundlers(ctx context.Context, page port.Page) ([]*userop.BundlerStats, string, error) {
	items, err := m.offsetListBundlers(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) ListBundlers(ctx context.Context, page port.Page) ([]*userop.BundlerStats, string, error) {
	items, err := m.offsetListBundlers(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) ListFactories(ctx context.Context, page port.Page) ([]*userop.FactoryStats, string, error) {
	items, err := m.offsetListFactories(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) ListFactories(ctx context.Context, page port.Page) ([]*userop.FactoryStats, string, error) {
	items, err := m.offsetListFactories(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) ListPaymasters(ctx context.Context, page port.Page) ([]*userop.PaymasterStats, string, error) {
	items, err := m.offsetListPaymasters(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) ListPaymasters(ctx context.Context, page port.Page) ([]*userop.PaymasterStats, string, error) {
	items, err := m.offsetListPaymasters(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) ListSmartAccounts(ctx context.Context, page port.Page) ([]*userop.SmartAccount, string, error) {
	items, err := m.offsetListSmartAccounts(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) ListSmartAccounts(ctx context.Context, page port.Page) ([]*userop.SmartAccount, string, error) {
	items, err := m.offsetListSmartAccounts(ctx, page.Limit, page.Offset)
	return items, "", err
}
