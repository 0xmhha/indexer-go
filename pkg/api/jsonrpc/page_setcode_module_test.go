package jsonrpc

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Page wrappers of the test doubles for the SetCode and module lists
// (keyset pagination); test doubles return no cursor.

func (m *mockStorageWithNonNotFoundErrors) GetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	items, err := m.offsetGetSetCodeAuthorizationsByTarget(ctx, target, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) GetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	items, err := m.offsetGetSetCodeAuthorizationsByTarget(ctx, target, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	items, err := m.offsetGetSetCodeAuthorizationsByTarget(ctx, target, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithNonNotFoundErrors) GetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	items, err := m.offsetGetSetCodeAuthorizationsByAuthority(ctx, authority, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithErrors) GetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	items, err := m.offsetGetSetCodeAuthorizationsByAuthority(ctx, authority, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	items, err := m.offsetGetSetCodeAuthorizationsByAuthority(ctx, authority, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorageWithNonNotFoundErrors) GetModulesByAccount(ctx context.Context, account common.Address, page port.Page) ([]*port.InstalledModule, string, error) {
	items, err := m.offsetGetModulesByAccount(ctx, account, page.Limit, page.Offset)
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

func (m *mockStorageWithNonNotFoundErrors) GetModulesByType(ctx context.Context, moduleType port.ModuleType, page port.Page) ([]*port.InstalledModule, string, error) {
	items, err := m.offsetGetModulesByType(ctx, moduleType, page.Limit, page.Offset)
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

func (m *mockStorageWithNonNotFoundErrors) ListModuleStats(ctx context.Context, page port.Page) ([]*port.ModuleStats, string, error) {
	items, err := m.offsetListModuleStats(ctx, page.Limit, page.Offset)
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

func (m *mockSetCodeStorage) GetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	items, err := m.offsetGetSetCodeAuthorizationsByTarget(ctx, target, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockSetCodeStorage) GetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	items, err := m.offsetGetSetCodeAuthorizationsByAuthority(ctx, authority, page.Limit, page.Offset)
	return items, "", err
}
