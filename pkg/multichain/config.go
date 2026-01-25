package multichain

import (
	"errors"
	"fmt"
	"time"
)

// ChainConfig defines the configuration for a single blockchain connection.
type ChainConfig struct {
	// ID is a unique identifier for this chain instance (e.g., "stableone-mainnet").
	ID string `yaml:"id" json:"id"`
	// Name is a human-readable name for the chain (e.g., "StableOne Mainnet").
	Name string `yaml:"name" json:"name"`
	// RPCEndpoint is the HTTP(S) JSON-RPC endpoint URL.
	RPCEndpoint string `yaml:"rpc_endpoint" json:"rpcEndpoint"`
	// WSEndpoint is the optional WebSocket endpoint URL for real-time subscriptions.
	WSEndpoint string `yaml:"ws_endpoint,omitempty" json:"wsEndpoint,omitempty"`
	// ChainID is the numeric chain ID (e.g., 1 for Ethereum mainnet).
	ChainID uint64 `yaml:"chain_id" json:"chainId"`
	// AdapterType specifies which adapter to use: "auto", "evm", "stableone", "anvil".
	AdapterType string `yaml:"adapter_type" json:"adapterType"`
	// StartHeight is the block height to start indexing from (0 for genesis).
	StartHeight uint64 `yaml:"start_height" json:"startHeight"`
	// Enabled indicates whether this chain should be active.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Workers is the number of concurrent fetch workers (default: from global config).
	Workers int `yaml:"workers,omitempty" json:"workers,omitempty"`
	// BatchSize is the number of blocks to fetch per batch (default: from global config).
	BatchSize int `yaml:"batch_size,omitempty" json:"batchSize,omitempty"`
	// RPCTimeout is the timeout for RPC calls (default: 30s).
	RPCTimeout time.Duration `yaml:"rpc_timeout,omitempty" json:"rpcTimeout,omitempty"`
}

// ManagerConfig defines the configuration for the ChainManager.
type ManagerConfig struct {
	// Enabled indicates whether multi-chain mode is active.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Chains is the list of chain configurations.
	Chains []ChainConfig `yaml:"chains" json:"chains"`
	// HealthCheckInterval is how often to check chain health (default: 30s).
	HealthCheckInterval time.Duration `yaml:"health_check_interval,omitempty" json:"healthCheckInterval,omitempty"`
	// MaxUnhealthyDuration is how long a chain can be unhealthy before stopping (default: 5m).
	MaxUnhealthyDuration time.Duration `yaml:"max_unhealthy_duration,omitempty" json:"maxUnhealthyDuration,omitempty"`
	// AutoRestart indicates whether to automatically restart failed chains.
	AutoRestart bool `yaml:"auto_restart" json:"autoRestart"`
	// AutoRestartDelay is the delay before auto-restarting a failed chain (default: 30s).
	AutoRestartDelay time.Duration `yaml:"auto_restart_delay,omitempty" json:"autoRestartDelay,omitempty"`
}

// DefaultManagerConfig returns the default manager configuration.
func DefaultManagerConfig() *ManagerConfig {
	return &ManagerConfig{
		Enabled:              false,
		Chains:               []ChainConfig{},
		HealthCheckInterval:  30 * time.Second,
		MaxUnhealthyDuration: 5 * time.Minute,
		AutoRestart:          true,
		AutoRestartDelay:     30 * time.Second,
	}
}

// DefaultChainConfig returns a chain config with sensible defaults.
func DefaultChainConfig() *ChainConfig {
	return &ChainConfig{
		AdapterType: "auto",
		StartHeight: 0,
		Enabled:     true,
		Workers:     4,
		BatchSize:   100,
		RPCTimeout:  30 * time.Second,
	}
}

// Validate validates the manager configuration.
func (c *ManagerConfig) Validate() error {
	if !c.Enabled {
		return nil // Skip validation if disabled
	}

	if len(c.Chains) == 0 {
		return errors.New("multichain enabled but no chains configured")
	}

	// Check for duplicate chain IDs
	seenIDs := make(map[string]bool)
	for i, chain := range c.Chains {
		if err := chain.Validate(); err != nil {
			return fmt.Errorf("chain[%d] (%s): %w", i, chain.ID, err)
		}
		if seenIDs[chain.ID] {
			return fmt.Errorf("duplicate chain ID: %s", chain.ID)
		}
		seenIDs[chain.ID] = true
	}

	if c.HealthCheckInterval <= 0 {
		c.HealthCheckInterval = 30 * time.Second
	}

	if c.MaxUnhealthyDuration <= 0 {
		c.MaxUnhealthyDuration = 5 * time.Minute
	}

	if c.AutoRestartDelay <= 0 {
		c.AutoRestartDelay = 30 * time.Second
	}

	return nil
}

// Validate validates a single chain configuration.
func (c *ChainConfig) Validate() error {
	if c.ID == "" {
		return errors.New("id is required")
	}
	if c.Name == "" {
		return errors.New("name is required")
	}
	if c.RPCEndpoint == "" {
		return errors.New("rpc_endpoint is required")
	}
	if c.ChainID == 0 {
		return errors.New("chain_id is required")
	}

	// Validate adapter type
	validAdapters := map[string]bool{
		"auto":      true,
		"evm":       true,
		"stableone": true,
		"anvil":     true,
	}
	if c.AdapterType == "" {
		c.AdapterType = "auto"
	}
	if !validAdapters[c.AdapterType] {
		return fmt.Errorf("invalid adapter_type: %s (valid: auto, evm, stableone, anvil)", c.AdapterType)
	}

	// Set defaults
	if c.Workers <= 0 {
		c.Workers = 4
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.RPCTimeout <= 0 {
		c.RPCTimeout = 30 * time.Second
	}

	return nil
}

// GetEnabledChains returns only the enabled chain configurations.
func (c *ManagerConfig) GetEnabledChains() []ChainConfig {
	var enabled []ChainConfig
	for _, chain := range c.Chains {
		if chain.Enabled {
			enabled = append(enabled, chain)
		}
	}
	return enabled
}

// GetChainByID returns the chain configuration by its ID.
func (c *ManagerConfig) GetChainByID(id string) *ChainConfig {
	for i := range c.Chains {
		if c.Chains[i].ID == id {
			return &c.Chains[i]
		}
	}
	return nil
}
