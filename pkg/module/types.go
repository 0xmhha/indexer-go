package module

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// ModuleType represents ERC-7579 module types
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

// IsValid checks if the module type is a recognized ERC-7579 type
func (m ModuleType) IsValid() bool {
	switch m {
	case ModuleTypeValidator, ModuleTypeExecutor, ModuleTypeFallback, ModuleTypeHook:
		return true
	default:
		return false
	}
}

// ERC-7579 event signatures
var (
	// ModuleInstalledSig is the Keccak256 hash of ModuleInstalled(uint256,address)
	ModuleInstalledSig = crypto.Keccak256Hash([]byte("ModuleInstalled(uint256,address)"))
	// ModuleUninstalledSig is the Keccak256 hash of ModuleUninstalled(uint256,address)
	ModuleUninstalledSig = crypto.Keccak256Hash([]byte("ModuleUninstalled(uint256,address)"))
)

// InstalledModule represents a module installed on a smart account
type InstalledModule struct {
	Account     common.Address `json:"account"`
	Module      common.Address `json:"module"`
	ModuleType  ModuleType     `json:"moduleType"`
	InstalledAt uint64         `json:"installedAt"`
	InstalledTx common.Hash    `json:"installedTx"`
	Active      bool           `json:"active"`
	RemovedAt   *uint64        `json:"removedAt,omitempty"`
	RemovedTx   *common.Hash   `json:"removedTx,omitempty"`
	Timestamp   time.Time      `json:"timestamp"`
}

// ModuleStats represents aggregate statistics for a specific module address
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
