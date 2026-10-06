package storage

import (
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Aliases of the ports moved to pkg/core/port (refactoring plan R1-1);
// removed once every consumer uses the port package.
type (
	ModuleType        = port.ModuleType
	InstalledModule   = port.InstalledModule
	ModuleStats       = port.ModuleStats
	AccountModules    = port.AccountModules
	ModuleIndexReader = port.ModuleIndexReader
	ModuleIndexWriter = port.ModuleIndexWriter
)

const (
	ModuleTypeValidator = port.ModuleTypeValidator
	ModuleTypeExecutor  = port.ModuleTypeExecutor
	ModuleTypeFallback  = port.ModuleTypeFallback
	ModuleTypeHook      = port.ModuleTypeHook
)
