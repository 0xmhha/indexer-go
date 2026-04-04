package graphql

import (
	"fmt"
	"strconv"

	"github.com/0xmhha/indexer-go/internal/constants"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// resolveAccountModules resolves all modules for a smart account, grouped by type
func (s *Schema) resolveAccountModules(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	address := common.HexToAddress(addressStr)

	// Cast storage to ModuleIndexReader
	moduleReader, ok := s.storage.(storagepkg.ModuleIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support Module queries")
	}

	accountModules, err := moduleReader.GetAccountModules(ctx, address)
	if err != nil {
		s.logger.Error("failed to get account modules",
			zap.String("address", addressStr),
			zap.Error(err))
		return nil, err
	}

	validators := make([]interface{}, len(accountModules.Validators))
	for i, m := range accountModules.Validators {
		validators[i] = s.installedModuleToMap(&m)
	}

	executors := make([]interface{}, len(accountModules.Executors))
	for i, m := range accountModules.Executors {
		executors[i] = s.installedModuleToMap(&m)
	}

	fallbacks := make([]interface{}, len(accountModules.Fallbacks))
	for i, m := range accountModules.Fallbacks {
		fallbacks[i] = s.installedModuleToMap(&m)
	}

	hooks := make([]interface{}, len(accountModules.Hooks))
	for i, m := range accountModules.Hooks {
		hooks[i] = s.installedModuleToMap(&m)
	}

	return map[string]interface{}{
		"account":    addressStr,
		"validators": validators,
		"executors":  executors,
		"fallbacks":  fallbacks,
		"hooks":      hooks,
	}, nil
}

// resolveInstalledModules resolves installed modules with optional filtering by account and type
func (s *Schema) resolveInstalledModules(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Cast storage to ModuleIndexReader
	moduleReader, ok := s.storage.(storagepkg.ModuleIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support Module queries")
	}

	// Get pagination parameters
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			limit = l
			if limit > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	var records []*storagepkg.InstalledModule
	var err error

	// Check for account filter
	if accountStr, ok := p.Args["account"].(string); ok && accountStr != "" {
		account := common.HexToAddress(accountStr)
		records, err = moduleReader.GetModulesByAccount(ctx, account, limit, offset)
	} else if moduleTypeStr, ok := p.Args["moduleType"].(string); ok && moduleTypeStr != "" {
		// Filter by module type
		moduleType := parseModuleType(moduleTypeStr)
		records, err = moduleReader.GetModulesByType(ctx, moduleType, limit, offset)
	} else {
		// Get recent module events
		records, err = moduleReader.GetRecentModuleEvents(ctx, limit)
	}

	if err != nil {
		s.logger.Error("failed to get installed modules",
			zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(records))
	for i, record := range records {
		nodes[i] = s.installedModuleToMap(record)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(records),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(records) == limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveModuleStats resolves aggregate statistics for a module contract
func (s *Schema) resolveModuleStats(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	moduleStr, ok := p.Args["module"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid module address")
	}

	moduleAddr := common.HexToAddress(moduleStr)

	// Cast storage to ModuleIndexReader
	moduleReader, ok := s.storage.(storagepkg.ModuleIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support Module queries")
	}

	stats, err := moduleReader.GetModuleStats(ctx, moduleAddr)
	if err != nil {
		s.logger.Error("failed to get module stats",
			zap.String("module", moduleStr),
			zap.Error(err))
		return nil, err
	}

	return s.moduleStatsToMap(stats), nil
}

// resolveListModuleStats resolves a paginated list of module stats
func (s *Schema) resolveListModuleStats(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Cast storage to ModuleIndexReader
	moduleReader, ok := s.storage.(storagepkg.ModuleIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support Module queries")
	}

	// Get pagination parameters
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			limit = l
			if limit > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	statsList, err := moduleReader.ListModuleStats(ctx, limit, offset)
	if err != nil {
		s.logger.Error("failed to list module stats",
			zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(statsList))
	for i, stats := range statsList {
		nodes[i] = s.moduleStatsToMap(stats)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(statsList),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(statsList) == limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveModuleEventCount resolves the total count of module events
func (s *Schema) resolveModuleEventCount(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Cast storage to ModuleIndexReader
	moduleReader, ok := s.storage.(storagepkg.ModuleIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support Module queries")
	}

	count, err := moduleReader.GetModuleEventCount(ctx)
	if err != nil {
		s.logger.Error("failed to get module event count",
			zap.Error(err))
		return nil, err
	}

	return count, nil
}

// installedModuleToMap converts an InstalledModule to a GraphQL-compatible map
func (s *Schema) installedModuleToMap(record *storagepkg.InstalledModule) map[string]interface{} {
	result := map[string]interface{}{
		"account":     record.Account.Hex(),
		"module":      record.Module.Hex(),
		"moduleType":  record.ModuleType.String(),
		"installedAt": strconv.FormatUint(record.InstalledAt, 10),
		"installedTx": record.InstalledTx.Hex(),
		"active":      record.Active,
		"timestamp":   strconv.FormatInt(record.Timestamp.Unix(), 10),
	}

	if record.RemovedAt != nil {
		result["removedAt"] = strconv.FormatUint(*record.RemovedAt, 10)
	}

	if record.RemovedTx != nil {
		result["removedTx"] = record.RemovedTx.Hex()
	}

	return result
}

// moduleStatsToMap converts ModuleStats to a GraphQL-compatible map
func (s *Schema) moduleStatsToMap(stats *storagepkg.ModuleStats) map[string]interface{} {
	return map[string]interface{}{
		"module":         stats.Module.Hex(),
		"moduleType":     stats.ModuleType.String(),
		"totalInstalls":  strconv.FormatUint(stats.TotalInstalls, 10),
		"activeInstalls": strconv.FormatUint(stats.ActiveInstalls, 10),
	}
}

// parseModuleType converts a string module type to ModuleType
func parseModuleType(s string) storagepkg.ModuleType {
	switch s {
	case "VALIDATOR", "validator":
		return storagepkg.ModuleTypeValidator
	case "EXECUTOR", "executor":
		return storagepkg.ModuleTypeExecutor
	case "FALLBACK", "fallback":
		return storagepkg.ModuleTypeFallback
	case "HOOK", "hook":
		return storagepkg.ModuleTypeHook
	default:
		return storagepkg.ModuleType(0)
	}
}
