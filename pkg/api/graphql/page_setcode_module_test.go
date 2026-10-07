package graphql

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Page wrappers of the test doubles for the SetCode and module lists
// (keyset pagination); test doubles return no cursor.

func (m *mockStorageWithErrors) GetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	items, err := m.offsetGetSetCodeAuthorizationsByTarget(ctx, target, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) GetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	items, err := m.offsetGetSetCodeAuthorizationsByAuthority(ctx, authority, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) GetModulesByAccount(ctx context.Context, account common.Address, page port.Page) ([]*port.InstalledModule, string, error) {
	items, err := m.offsetGetModulesByAccount(ctx, account, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetModulesByAccount(ctx context.Context, account common.Address, page port.Page) ([]*port.InstalledModule, string, error) {
	items, err := m.offsetGetModulesByAccount(ctx, account, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) GetModulesByType(ctx context.Context, moduleType port.ModuleType, page port.Page) ([]*port.InstalledModule, string, error) {
	items, err := m.offsetGetModulesByType(ctx, moduleType, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetModulesByType(ctx context.Context, moduleType port.ModuleType, page port.Page) ([]*port.InstalledModule, string, error) {
	items, err := m.offsetGetModulesByType(ctx, moduleType, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) ListModuleStats(ctx context.Context, page port.Page) ([]*port.ModuleStats, string, error) {
	items, err := m.offsetListModuleStats(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) ListModuleStats(ctx context.Context, page port.Page) ([]*port.ModuleStats, string, error) {
	items, err := m.offsetListModuleStats(ctx, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetSetCodeAuthorizationsByTarget(a0 context.Context, a1 common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	items, err := m.offsetGetSetCodeAuthorizationsByTarget(a0, a1, page.Limit, page.Offset)
	return items, "", err
}

func (m *richMockStorage) GetSetCodeAuthorizationsByTarget(a0 context.Context, a1 common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	items, err := m.offsetGetSetCodeAuthorizationsByTarget(a0, a1, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetSetCodeAuthorizationsByAuthority(a0 context.Context, a1 common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	items, err := m.offsetGetSetCodeAuthorizationsByAuthority(a0, a1, page.Limit, page.Offset)
	return items, "", err
}

func (m *richMockStorage) GetSetCodeAuthorizationsByAuthority(a0 context.Context, a1 common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	items, err := m.offsetGetSetCodeAuthorizationsByAuthority(a0, a1, page.Limit, page.Offset)
	return items, "", err
}
