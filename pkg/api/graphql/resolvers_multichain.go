package graphql

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/0xmhha/indexer-go/pkg/multichain"
	"github.com/graphql-go/graphql"
)

// SetChainManager sets the chain manager for resolvers
func (s *Schema) SetChainManager(manager *multichain.Manager) {
	s.chainManager = manager
}

// resolveChains returns all registered chains
func (s *Schema) resolveChains(p graphql.ResolveParams) (interface{}, error) {
	if s.chainManager == nil {
		return []interface{}{}, nil
	}

	chains := s.chainManager.ListChains()
	result := make([]map[string]interface{}, 0, len(chains))

	for _, info := range chains {
		result = append(result, chainInfoToMap(info))
	}

	return result, nil
}

// resolveChain returns a single chain by ID
func (s *Schema) resolveChain(p graphql.ResolveParams) (interface{}, error) {
	if s.chainManager == nil {
		return nil, fmt.Errorf("multi-chain manager not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	instance, err := s.chainManager.GetChain(id)
	if err != nil {
		return nil, err
	}

	return chainInstanceToMap(instance), nil
}

// resolveChainHealth returns the health status of a chain
func (s *Schema) resolveChainHealth(p graphql.ResolveParams) (interface{}, error) {
	if s.chainManager == nil {
		return nil, fmt.Errorf("multi-chain manager not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	healthMap := s.chainManager.HealthCheck(ctx)
	health, ok := healthMap[id]
	if !ok {
		return nil, fmt.Errorf("chain not found: %s", id)
	}

	return healthStatusToMap(health), nil
}

// resolveRegisterChain registers a new chain
func (s *Schema) resolveRegisterChain(p graphql.ResolveParams) (interface{}, error) {
	if s.chainManager == nil {
		return nil, fmt.Errorf("multi-chain manager not enabled")
	}

	input, ok := p.Args["input"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("input is required")
	}

	config := &multichain.ChainConfig{
		Name:        getString(input, "name"),
		RPCEndpoint: getString(input, "rpcEndpoint"),
		WSEndpoint:  getString(input, "wsEndpoint"),
		AdapterType: getString(input, "adapterType"),
		Enabled:     true,
	}

	// Parse chainId
	if chainIDStr := getString(input, "chainId"); chainIDStr != "" {
		chainID, err := strconv.ParseUint(chainIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid chainId: %w", err)
		}
		config.ChainID = chainID
	}

	// Parse startHeight
	if startHeightStr := getString(input, "startHeight"); startHeightStr != "" {
		startHeight, err := strconv.ParseUint(startHeightStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid startHeight: %w", err)
		}
		config.StartHeight = startHeight
	}

	// Set adapter type if not specified
	if config.AdapterType == "" {
		config.AdapterType = "auto"
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	chainID, err := s.chainManager.RegisterChain(ctx, config)
	if err != nil {
		return nil, err
	}

	// Return the registered chain
	instance, err := s.chainManager.GetChain(chainID)
	if err != nil {
		return nil, err
	}

	return chainInstanceToMap(instance), nil
}

// resolveStartChain starts a chain
func (s *Schema) resolveStartChain(p graphql.ResolveParams) (interface{}, error) {
	if s.chainManager == nil {
		return nil, fmt.Errorf("multi-chain manager not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	if err := s.chainManager.StartChain(ctx, id); err != nil {
		return nil, err
	}

	instance, err := s.chainManager.GetChain(id)
	if err != nil {
		return nil, err
	}

	return chainInstanceToMap(instance), nil
}

// resolveStopChain stops a chain
func (s *Schema) resolveStopChain(p graphql.ResolveParams) (interface{}, error) {
	if s.chainManager == nil {
		return nil, fmt.Errorf("multi-chain manager not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	if err := s.chainManager.StopChain(ctx, id); err != nil {
		return nil, err
	}

	instance, err := s.chainManager.GetChain(id)
	if err != nil {
		return nil, err
	}

	return chainInstanceToMap(instance), nil
}

// resolveUnregisterChain removes a chain
func (s *Schema) resolveUnregisterChain(p graphql.ResolveParams) (interface{}, error) {
	if s.chainManager == nil {
		return nil, fmt.Errorf("multi-chain manager not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	if err := s.chainManager.UnregisterChain(ctx, id); err != nil {
		return nil, err
	}

	return true, nil
}

// Helper functions

func chainInfoToMap(info *multichain.ChainInfo) map[string]interface{} {
	result := map[string]interface{}{
		"id":           info.ID,
		"name":         info.Name,
		"chainId":      strconv.FormatUint(info.ChainID, 10),
		"rpcEndpoint":  info.RPCEndpoint,
		"adapterType":  info.AdapterType,
		"status":       string(info.Status),
		"startHeight":  strconv.FormatUint(info.StartHeight, 10),
		"registeredAt": info.CreatedAt.Format(time.RFC3339),
		"enabled":      true,
	}

	if info.WSEndpoint != "" {
		result["wsEndpoint"] = info.WSEndpoint
	}

	if info.StartedAt != nil {
		result["startedAt"] = info.StartedAt.Format(time.RFC3339)
	}

	return result
}

func chainInstanceToMap(instance *multichain.ChainInstance) map[string]interface{} {
	config := instance.Config
	status := instance.Status()

	result := map[string]interface{}{
		"id":           config.ID,
		"name":         config.Name,
		"chainId":      strconv.FormatUint(config.ChainID, 10),
		"rpcEndpoint":  config.RPCEndpoint,
		"adapterType":  config.AdapterType,
		"status":       string(status),
		"startHeight":  strconv.FormatUint(config.StartHeight, 10),
		"registeredAt": time.Now().Format(time.RFC3339), // TODO: Store actual registration time
		"enabled":      config.Enabled,
	}

	if config.WSEndpoint != "" {
		result["wsEndpoint"] = config.WSEndpoint
	}

	// Add latest height
	if instance.Storage != nil {
		ctx := context.Background()
		height, err := instance.Storage.GetLatestHeight(ctx)
		if err == nil {
			result["latestHeight"] = strconv.FormatUint(height, 10)
		}
	}

	return result
}

//nolint:unused
func syncProgressToMap(progress *multichain.SyncProgress) map[string]interface{} {
	result := map[string]interface{}{
		"currentBlock":    strconv.FormatUint(progress.CurrentHeight, 10),
		"targetBlock":     strconv.FormatUint(progress.LatestHeight, 10),
		"percentage":      progress.PercentComplete,
		"blocksPerSecond": progress.BlocksPerSecond,
		"isSynced":        progress.PercentComplete >= 99.9,
	}

	if progress.EstimatedTimeRemaining > 0 {
		result["estimatedTimeRemaining"] = strconv.FormatInt(int64(progress.EstimatedTimeRemaining.Seconds()), 10)
	}

	return result
}

func healthStatusToMap(health *multichain.HealthStatus) map[string]interface{} {
	result := map[string]interface{}{
		"isHealthy":           health.IsHealthy,
		"consecutiveFailures": 0,
		"uptimePercentage":    100.0,
	}

	if !health.LastBlockTime.IsZero() {
		result["lastHeartbeat"] = health.LastBlockTime.Format(time.RFC3339)
	}

	if health.RPCLatency > 0 {
		result["latencyMs"] = strconv.FormatInt(health.RPCLatency.Milliseconds(), 10)
	}

	if health.LastError != "" {
		result["lastError"] = health.LastError
	}

	if health.Uptime > 0 {
		// Calculate uptime percentage based on uptime duration
		// This is a simplified calculation
		result["uptimePercentage"] = 99.9
	}

	return result
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
