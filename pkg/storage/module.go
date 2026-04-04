package storage

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// ModuleType represents ERC-7579 module types (mirrors module.ModuleType)
type ModuleType uint8

const (
	// ModuleTypeValidator is a module that validates user operations (type 1)
	ModuleTypeValidator ModuleType = 1
	// ModuleTypeExecutor is a module that executes operations on behalf of the account (type 2)
	ModuleTypeExecutor ModuleType = 2
	// ModuleTypeFallback is a module that handles fallback calls (type 3)
	ModuleTypeFallback ModuleType = 3
	// ModuleTypeHook is a module that provides pre/post execution hooks (type 4)
	ModuleTypeHook ModuleType = 4
)

// String returns the string representation of a ModuleType
func (m ModuleType) String() string {
	switch m {
	case ModuleTypeValidator:
		return "validator"
	case ModuleTypeExecutor:
		return "executor"
	case ModuleTypeFallback:
		return "fallback"
	case ModuleTypeHook:
		return "hook"
	default:
		return "unknown"
	}
}

// InstalledModule represents a module installed on an ERC-7579 modular smart account
type InstalledModule struct {
	// The smart account address (log emitter)
	Account common.Address `json:"account"`
	// The module contract address
	Module common.Address `json:"module"`
	// The type of module (validator, executor, fallback, hook)
	ModuleType ModuleType `json:"moduleType"`
	// Block number where the module was installed
	InstalledAt uint64 `json:"installedAt"`
	// Transaction hash of the install event
	InstalledTx common.Hash `json:"installedTx"`
	// Whether the module is currently active (not uninstalled)
	Active bool `json:"active"`
	// Block number where the module was removed (nil if still active)
	RemovedAt *uint64 `json:"removedAt,omitempty"`
	// Transaction hash of the uninstall event (nil if still active)
	RemovedTx *common.Hash `json:"removedTx,omitempty"`
	// Timestamp of the install event
	Timestamp time.Time `json:"timestamp"`
}

// ModuleStats represents aggregate statistics for a specific module contract
type ModuleStats struct {
	Module         common.Address `json:"module"`
	ModuleType     ModuleType     `json:"moduleType"`
	TotalInstalls  uint64         `json:"totalInstalls"`
	ActiveInstalls uint64         `json:"activeInstalls"`
}

// AccountModules represents all modules installed on a smart account, grouped by type
type AccountModules struct {
	Account    common.Address    `json:"account"`
	Validators []InstalledModule `json:"validators"`
	Executors  []InstalledModule `json:"executors"`
	Fallbacks  []InstalledModule `json:"fallbacks"`
	Hooks      []InstalledModule `json:"hooks"`
}

// ModuleIndexReader defines read operations for ERC-7579 module indexing
type ModuleIndexReader interface {
	// GetInstalledModule retrieves a specific installed module by account and module address.
	// Returns ErrNotFound if the module is not found.
	GetInstalledModule(ctx context.Context, account, module common.Address) (*InstalledModule, error)

	// GetModulesByAccount retrieves all modules installed on a specific account.
	// Results are ordered by block number descending (newest first).
	GetModulesByAccount(ctx context.Context, account common.Address, limit, offset int) ([]*InstalledModule, error)

	// GetModulesByType retrieves modules by their type across all accounts.
	// Results are ordered by block number descending (newest first).
	GetModulesByType(ctx context.Context, moduleType ModuleType, limit, offset int) ([]*InstalledModule, error)

	// GetModuleStats retrieves aggregate statistics for a module contract.
	// Returns zero-value stats if the module has no install activity.
	GetModuleStats(ctx context.Context, module common.Address) (*ModuleStats, error)

	// GetAccountModules retrieves all modules for an account, grouped by type.
	GetAccountModules(ctx context.Context, account common.Address) (*AccountModules, error)

	// GetRecentModuleEvents retrieves the most recent module install/uninstall events.
	// Results are ordered by block number descending (newest first).
	GetRecentModuleEvents(ctx context.Context, limit int) ([]*InstalledModule, error)

	// GetModuleEventCount returns the total count of module events indexed.
	GetModuleEventCount(ctx context.Context) (int, error)

	// ListModuleStats retrieves module stats with pagination.
	ListModuleStats(ctx context.Context, limit, offset int) ([]*ModuleStats, error)
}

// ModuleIndexWriter defines write operations for ERC-7579 module indexing
type ModuleIndexWriter interface {
	// SaveInstalledModule saves a module installation record.
	// Creates all necessary indexes (account, type, block).
	SaveInstalledModule(ctx context.Context, record *InstalledModule) error

	// RemoveModule marks a module as uninstalled.
	// Sets Active=false and updates RemovedAt/RemovedTx fields.
	RemoveModule(ctx context.Context, account, module common.Address, blockNumber uint64, txHash common.Hash) error

	// UpdateModuleStats updates the aggregate stats for a module.
	UpdateModuleStats(ctx context.Context, stats *ModuleStats) error
}
