package graphql

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/port"
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
	moduleReader, ok := s.storage.(port.ModuleIndexReader)
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
	moduleReader, ok := s.storage.(port.ModuleIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support Module queries")
	}

	pagination := parsePaginationParams(p, constants.DefaultMaxPaginationLimit)

	var records []*port.InstalledModule
	var next string
	var err error
	recent := false

	// Check for account filter
	if accountStr, ok := p.Args["account"].(string); ok && accountStr != "" {
		account := common.HexToAddress(accountStr)
		records, next, err = moduleReader.GetModulesByAccount(ctx, account, pagination.page())
	} else if moduleTypeStr, ok := p.Args["moduleType"].(string); ok && moduleTypeStr != "" {
		// Filter by module type
		moduleType := parseModuleType(moduleTypeStr)
		records, next, err = moduleReader.GetModulesByType(ctx, moduleType, pagination.page())
	} else {
		// Get recently installed modules (the first page only; this list
		// issues no cursors)
		if pagination.After != "" {
			return nil, fmt.Errorf("invalid pagination cursor")
		}
		recent = true
		records, err = moduleReader.GetRecentModuleEvents(ctx, pagination.Limit)
	}

	if err != nil {
		if errors.Is(err, port.ErrInvalidCursor) {
			return nil, fmt.Errorf("invalid pagination cursor")
		}
		s.logger.Error("failed to get installed modules",
			zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(records))
	for i, record := range records {
		nodes[i] = s.installedModuleToMap(record)
	}

	pageInfo := cursorPageInfo(pagination, next)
	if recent {
		pageInfo = map[string]interface{}{
			"hasNextPage":     len(records) == pagination.Limit,
			"hasPreviousPage": pagination.Offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		}
	}
	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(records),
		"pageInfo":   pageInfo,
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
	moduleReader, ok := s.storage.(port.ModuleIndexReader)
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
	moduleReader, ok := s.storage.(port.ModuleIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support Module queries")
	}

	pagination := parsePaginationParams(p, constants.DefaultMaxPaginationLimit)

	statsList, next, err := moduleReader.ListModuleStats(ctx, pagination.page())
	if err != nil {
		if errors.Is(err, port.ErrInvalidCursor) {
			return nil, fmt.Errorf("invalid pagination cursor")
		}
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
		"pageInfo":   cursorPageInfo(pagination, next),
	}, nil
}

// resolveModuleEventCount resolves the total count of module install records
func (s *Schema) resolveModuleEventCount(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Cast storage to ModuleIndexReader
	moduleReader, ok := s.storage.(port.ModuleIndexReader)
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
func (s *Schema) installedModuleToMap(record *port.InstalledModule) map[string]interface{} {
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
func (s *Schema) moduleStatsToMap(stats *port.ModuleStats) map[string]interface{} {
	return map[string]interface{}{
		"module":         stats.Module.Hex(),
		"moduleType":     stats.ModuleType.String(),
		"totalInstalls":  strconv.FormatUint(stats.TotalInstalls, 10),
		"activeInstalls": strconv.FormatUint(stats.ActiveInstalls, 10),
	}
}

// parseModuleType converts a string module type to ModuleType
func parseModuleType(s string) port.ModuleType {
	switch s {
	case "VALIDATOR", "validator":
		return port.ModuleTypeValidator
	case "EXECUTOR", "executor":
		return port.ModuleTypeExecutor
	case "FALLBACK", "fallback":
		return port.ModuleTypeFallback
	case "HOOK", "hook":
		return port.ModuleTypeHook
	default:
		return port.ModuleType(0)
	}
}
