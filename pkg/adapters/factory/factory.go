// Package factory provides automatic adapter creation based on node detection.
// It detects the connected node type (Anvil, StableOne, Geth, etc.) and creates
// the appropriate adapter with consensus parser and chain-specific features.
package factory

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/0xmhha/indexer-go/pkg/adapters/anvil"
	"github.com/0xmhha/indexer-go/pkg/adapters/detector"
	"github.com/0xmhha/indexer-go/pkg/adapters/evm"
	"github.com/0xmhha/indexer-go/pkg/adapters/stableone"
	"github.com/0xmhha/indexer-go/pkg/types/chain"
	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"
)

// Config holds configuration for adapter factory
type Config struct {
	// RPCEndpoint is the RPC URL to connect to
	RPCEndpoint string

	// WSEndpoint is the optional WebSocket endpoint
	WSEndpoint string

	// ForceAdapterType forces a specific adapter type instead of auto-detection
	// Values: "anvil", "stableone", "evm", "" (auto-detect)
	ForceAdapterType string

	// ChainID overrides the detected chain ID (optional)
	ChainID *big.Int

	// DetectionTimeout is the timeout for node detection (default: 10s)
	DetectionTimeout time.Duration

	// StableOneConfig is optional StableOne-specific configuration
	StableOneConfig *stableone.Config

	// AnvilConfig is optional Anvil-specific configuration
	AnvilConfig *anvil.Config

	// EVMConfig is optional generic EVM configuration
	EVMConfig *evm.Config
}

// DefaultConfig returns default factory configuration
func DefaultConfig(rpcEndpoint string) *Config {
	return &Config{
		RPCEndpoint:      rpcEndpoint,
		DetectionTimeout: 10 * time.Second,
	}
}

// Factory creates chain adapters based on node detection
type Factory struct {
	config *Config
	logger *zap.Logger
}

// NewFactory creates a new adapter factory
func NewFactory(config *Config, logger *zap.Logger) *Factory {
	if config.DetectionTimeout == 0 {
		config.DetectionTimeout = 10 * time.Second
	}
	return &Factory{
		config: config,
		logger: logger,
	}
}

// CreateResult holds the result of adapter creation
type CreateResult struct {
	// Adapter is the created chain adapter
	Adapter chain.Adapter

	// Client is the RPC client (for additional operations)
	Client evm.Client

	// NodeInfo contains detected node information
	NodeInfo *detector.NodeInfo

	// AdapterType is the type of adapter created
	AdapterType string
}

// Create detects the node type and creates the appropriate adapter
func (f *Factory) Create(ctx context.Context) (*CreateResult, error) {
	// Apply detection timeout
	ctx, cancel := context.WithTimeout(ctx, f.config.DetectionTimeout)
	defer cancel()

	// Connect to RPC
	rpcClient, err := rpc.DialContext(ctx, f.config.RPCEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

	// Create EVM client wrapper
	client := NewEVMClient(rpcClient)

	// Check if adapter type is forced
	if f.config.ForceAdapterType != "" {
		return f.createForced(ctx, client, rpcClient)
	}

	// Detect node type
	detect := detector.NewDetectorWithClient(rpcClient, f.logger)
	nodeInfo, err := detect.Detect(ctx)
	if err != nil {
		f.logger.Warn("Node detection failed, using generic EVM adapter",
			zap.Error(err),
		)
		nodeInfo = &detector.NodeInfo{
			Type: detector.NodeTypeUnknown,
		}
	}

	// Create adapter based on detected type
	result, err := f.createByNodeType(ctx, client, rpcClient, nodeInfo)
	if err != nil {
		return nil, err
	}

	result.NodeInfo = nodeInfo
	return result, nil
}

// createForced creates an adapter of the forced type
func (f *Factory) createForced(ctx context.Context, client *EVMClient, rpcClient *rpc.Client) (*CreateResult, error) {
	f.logger.Info("Creating forced adapter type",
		zap.String("type", f.config.ForceAdapterType),
	)

	nodeInfo := &detector.NodeInfo{
		Type: detector.NodeType(f.config.ForceAdapterType),
	}

	switch f.config.ForceAdapterType {
	case "anvil":
		nodeInfo.Type = detector.NodeTypeAnvil
	case "stableone":
		nodeInfo.Type = detector.NodeTypeStableOne
	case "geth":
		nodeInfo.Type = detector.NodeTypeGeth
	default:
		nodeInfo.Type = detector.NodeTypeUnknown
	}

	result, err := f.createByNodeType(ctx, client, rpcClient, nodeInfo)
	if err != nil {
		return nil, err
	}
	result.NodeInfo = nodeInfo
	return result, nil
}

// createByNodeType creates an adapter based on the detected node type
func (f *Factory) createByNodeType(ctx context.Context, client *EVMClient, rpcClient *rpc.Client, nodeInfo *detector.NodeInfo) (*CreateResult, error) {
	switch nodeInfo.Type {
	case detector.NodeTypeAnvil:
		return f.createAnvilAdapter(ctx, client, rpcClient, nodeInfo)

	case detector.NodeTypeStableOne:
		return f.createStableOneAdapter(ctx, client, nodeInfo)

	case detector.NodeTypeGeth, detector.NodeTypeHardhat, detector.NodeTypeGanache, detector.NodeTypeUnknown:
		return f.createEVMAdapter(ctx, client, nodeInfo)

	default:
		return f.createEVMAdapter(ctx, client, nodeInfo)
	}
}

// createAnvilAdapter creates an Anvil adapter
func (f *Factory) createAnvilAdapter(ctx context.Context, client *EVMClient, rpcClient *rpc.Client, nodeInfo *detector.NodeInfo) (*CreateResult, error) {
	config := f.config.AnvilConfig
	if config == nil {
		config = anvil.DefaultConfig()
	}

	// Override chain ID if detected
	if nodeInfo.ChainID != 0 && f.config.ChainID == nil {
		config.ChainID = big.NewInt(int64(nodeInfo.ChainID))
	} else if f.config.ChainID != nil {
		config.ChainID = f.config.ChainID
	}

	config.RPCEndpoint = f.config.RPCEndpoint
	config.EnableAnvilFeatures = nodeInfo.SupportsAnvilMethods

	adapter, err := anvil.NewAdapterWithRPC(client, rpcClient, config, f.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create Anvil adapter: %w", err)
	}

	f.logger.Info("Created Anvil adapter",
		zap.String("chain_id", config.ChainID.String()),
		zap.Bool("anvil_features", config.EnableAnvilFeatures),
	)

	return &CreateResult{
		Adapter:     adapter,
		Client:      client,
		AdapterType: "anvil",
	}, nil
}

// createStableOneAdapter creates a StableOne adapter
func (f *Factory) createStableOneAdapter(ctx context.Context, client *EVMClient, nodeInfo *detector.NodeInfo) (*CreateResult, error) {
	config := f.config.StableOneConfig
	if config == nil {
		config = stableone.DefaultConfig()
	}

	// Override chain ID if detected
	if nodeInfo.ChainID != 0 && f.config.ChainID == nil {
		config.ChainID = big.NewInt(int64(nodeInfo.ChainID))
	} else if f.config.ChainID != nil {
		config.ChainID = f.config.ChainID
	}

	config.RPCEndpoint = f.config.RPCEndpoint
	config.WSEndpoint = f.config.WSEndpoint

	adapter := stableone.NewAdapter(client, config, f.logger)

	f.logger.Info("Created StableOne adapter",
		zap.String("chain_id", config.ChainID.String()),
	)

	return &CreateResult{
		Adapter:     adapter,
		Client:      client,
		AdapterType: "stableone",
	}, nil
}

// createEVMAdapter creates a generic EVM adapter
func (f *Factory) createEVMAdapter(ctx context.Context, client *EVMClient, nodeInfo *detector.NodeInfo) (*CreateResult, error) {
	config := f.config.EVMConfig
	if config == nil {
		config = evm.DefaultConfig()
	}

	// Override chain ID if detected
	if nodeInfo.ChainID != 0 && f.config.ChainID == nil {
		config.ChainID = big.NewInt(int64(nodeInfo.ChainID))
	} else if f.config.ChainID != nil {
		config.ChainID = f.config.ChainID
	}

	// Set chain name based on detection
	switch nodeInfo.Type {
	case detector.NodeTypeGeth:
		config.ChainName = "Ethereum"
	case detector.NodeTypeHardhat:
		config.ChainName = "Hardhat"
	case detector.NodeTypeGanache:
		config.ChainName = "Ganache"
	default:
		config.ChainName = "EVM"
	}

	adapter := evm.NewAdapter(client, config, f.logger)

	f.logger.Info("Created generic EVM adapter",
		zap.String("chain_id", config.ChainID.String()),
		zap.String("chain_name", config.ChainName),
		zap.String("detected_type", string(nodeInfo.Type)),
	)

	return &CreateResult{
		Adapter:     adapter,
		Client:      client,
		AdapterType: "evm",
	}, nil
}

// =============================================================================
// Convenience functions
// =============================================================================

// CreateAdapter is a convenience function that creates an adapter with auto-detection
func CreateAdapter(ctx context.Context, rpcEndpoint string, logger *zap.Logger) (*CreateResult, error) {
	config := DefaultConfig(rpcEndpoint)
	factory := NewFactory(config, logger)
	return factory.Create(ctx)
}

// CreateAdapterWithConfig is a convenience function that creates an adapter with custom config
func CreateAdapterWithConfig(ctx context.Context, config *Config, logger *zap.Logger) (*CreateResult, error) {
	factory := NewFactory(config, logger)
	return factory.Create(ctx)
}

// MustCreateAdapter creates an adapter and panics on error
// Useful for testing and initialization
func MustCreateAdapter(ctx context.Context, rpcEndpoint string, logger *zap.Logger) *CreateResult {
	result, err := CreateAdapter(ctx, rpcEndpoint, logger)
	if err != nil {
		panic(fmt.Sprintf("failed to create adapter: %v", err))
	}
	return result
}
