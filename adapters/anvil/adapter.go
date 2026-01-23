// Package anvil provides an adapter implementation for Foundry's Anvil local testnet.
// Anvil is an EVM-compatible chain with PoA (Clique) consensus that supports
// additional development features like impersonation, snapshot/revert, and time manipulation.
package anvil

import (
	"context"
	"math/big"

	"github.com/0xmhha/indexer-go/adapters/evm"
	"github.com/0xmhha/indexer-go/consensus"
	_ "github.com/0xmhha/indexer-go/consensus/poa" // Register PoA parser
	"github.com/0xmhha/indexer-go/types/chain"
	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"
)

// Default Anvil configuration
const (
	// DefaultChainID is the default Anvil chain ID
	DefaultChainID = 31337

	// DefaultNativeCurrency is the native token symbol
	DefaultNativeCurrency = "ETH"

	// DefaultNativeDecimals is the native token decimals
	DefaultNativeDecimals = 18
)

// Ensure Adapter implements chain.Adapter
var _ chain.Adapter = (*Adapter)(nil)

// Config holds configuration for the Anvil adapter
type Config struct {
	// ChainID is the chain identifier (default: 31337)
	ChainID *big.Int

	// RPCEndpoint is the Anvil RPC endpoint
	RPCEndpoint string

	// NativeCurrency is the native token symbol (default: ETH)
	NativeCurrency string

	// EnableAnvilFeatures enables Anvil-specific RPC methods
	EnableAnvilFeatures bool
}

// DefaultConfig returns default Anvil configuration
func DefaultConfig() *Config {
	return &Config{
		ChainID:             big.NewInt(DefaultChainID),
		NativeCurrency:      DefaultNativeCurrency,
		EnableAnvilFeatures: true,
	}
}

// Adapter implements chain.Adapter for Anvil local testnet
// It extends the base EVM adapter with PoA consensus and Anvil-specific features
type Adapter struct {
	*evm.Adapter
	config          *Config
	logger          *zap.Logger
	consensusParser chain.ConsensusParser
	rpcClient       *rpc.Client // For Anvil-specific RPC methods
}

// NewAdapter creates a new Anvil adapter
func NewAdapter(client evm.Client, config *Config, logger *zap.Logger) (*Adapter, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Create EVM config
	evmConfig := &evm.Config{
		ChainID:        config.ChainID,
		ChainName:      "Anvil",
		NativeCurrency: config.NativeCurrency,
		Decimals:       DefaultNativeDecimals,
		ConsensusType:  chain.ConsensusTypePoA,
	}

	// Create base EVM adapter
	evmAdapter := evm.NewAdapter(client, evmConfig, logger)

	adapter := &Adapter{
		Adapter: evmAdapter,
		config:  config,
		logger:  logger,
	}

	// Get PoA consensus parser from registry
	consensusConfig := &consensus.Config{
		ChainID: config.ChainID.Uint64(),
	}
	consensusParser, err := consensus.Get(chain.ConsensusTypePoA, consensusConfig, logger)
	if err != nil {
		logger.Warn("Failed to get PoA parser from registry, consensus parsing disabled",
			zap.Error(err),
		)
	}
	adapter.consensusParser = consensusParser

	// Set the consensus parser on the base adapter
	if adapter.consensusParser != nil {
		evmAdapter.SetConsensusParser(adapter.consensusParser)
	}

	logger.Info("Anvil adapter initialized",
		zap.String("chain_id", config.ChainID.String()),
		zap.Bool("anvil_features", config.EnableAnvilFeatures),
		zap.Bool("consensus_parser", adapter.consensusParser != nil),
	)

	return adapter, nil
}

// NewAdapterWithRPC creates a new Anvil adapter with RPC client for advanced features
func NewAdapterWithRPC(client evm.Client, rpcClient *rpc.Client, config *Config, logger *zap.Logger) (*Adapter, error) {
	adapter, err := NewAdapter(client, config, logger)
	if err != nil {
		return nil, err
	}
	adapter.rpcClient = rpcClient
	return adapter, nil
}

// Info returns chain metadata (overrides base EVM adapter)
func (a *Adapter) Info() *chain.ChainInfo {
	return &chain.ChainInfo{
		ChainID:        a.config.ChainID,
		ChainType:      chain.ChainTypeEVM,
		ConsensusType:  chain.ConsensusTypePoA,
		Name:           "Anvil",
		NativeCurrency: a.config.NativeCurrency,
		Decimals:       DefaultNativeDecimals,
	}
}

// ConsensusParser returns the PoA consensus parser
func (a *Adapter) ConsensusParser() chain.ConsensusParser {
	return a.consensusParser
}

// SystemContracts returns nil as Anvil doesn't have system contracts
func (a *Adapter) SystemContracts() chain.SystemContractsHandler {
	return nil
}

// =============================================================================
// Anvil-specific features
// =============================================================================

// AnvilClient provides access to Anvil-specific RPC methods
type AnvilClient struct {
	rpcClient *rpc.Client
	logger    *zap.Logger
}

// GetAnvilClient returns an AnvilClient for Anvil-specific operations
// Returns nil if RPC client is not available
func (a *Adapter) GetAnvilClient() *AnvilClient {
	if a.rpcClient == nil {
		return nil
	}
	return &AnvilClient{
		rpcClient: a.rpcClient,
		logger:    a.logger,
	}
}

// Mine mines a specified number of blocks
func (c *AnvilClient) Mine(ctx context.Context, numBlocks uint64) error {
	return c.rpcClient.CallContext(ctx, nil, "anvil_mine", numBlocks)
}

// SetBalance sets the balance of an address
func (c *AnvilClient) SetBalance(ctx context.Context, address string, balance *big.Int) error {
	balanceHex := "0x" + balance.Text(16)
	return c.rpcClient.CallContext(ctx, nil, "anvil_setBalance", address, balanceHex)
}

// SetCode sets the bytecode at an address
func (c *AnvilClient) SetCode(ctx context.Context, address string, code []byte) error {
	codeHex := "0x" + string(code)
	return c.rpcClient.CallContext(ctx, nil, "anvil_setCode", address, codeHex)
}

// Snapshot creates a snapshot of the current blockchain state
// Returns the snapshot ID
func (c *AnvilClient) Snapshot(ctx context.Context) (string, error) {
	var snapshotID string
	err := c.rpcClient.CallContext(ctx, &snapshotID, "evm_snapshot")
	return snapshotID, err
}

// Revert reverts to a previous snapshot
func (c *AnvilClient) Revert(ctx context.Context, snapshotID string) error {
	var result bool
	err := c.rpcClient.CallContext(ctx, &result, "evm_revert", snapshotID)
	return err
}

// SetNextBlockTimestamp sets the timestamp of the next block
func (c *AnvilClient) SetNextBlockTimestamp(ctx context.Context, timestamp uint64) error {
	return c.rpcClient.CallContext(ctx, nil, "evm_setNextBlockTimestamp", timestamp)
}

// IncreaseTime increases the blockchain time by the specified seconds
func (c *AnvilClient) IncreaseTime(ctx context.Context, seconds uint64) error {
	return c.rpcClient.CallContext(ctx, nil, "evm_increaseTime", seconds)
}

// ImpersonateAccount starts impersonating an account
func (c *AnvilClient) ImpersonateAccount(ctx context.Context, address string) error {
	return c.rpcClient.CallContext(ctx, nil, "anvil_impersonateAccount", address)
}

// StopImpersonatingAccount stops impersonating an account
func (c *AnvilClient) StopImpersonatingAccount(ctx context.Context, address string) error {
	return c.rpcClient.CallContext(ctx, nil, "anvil_stopImpersonatingAccount", address)
}

// SetAutomine sets the automine status
func (c *AnvilClient) SetAutomine(ctx context.Context, enabled bool) error {
	return c.rpcClient.CallContext(ctx, nil, "evm_setAutomine", enabled)
}

// GetAutomine returns the current automine status
func (c *AnvilClient) GetAutomine(ctx context.Context) (bool, error) {
	var result bool
	err := c.rpcClient.CallContext(ctx, &result, "anvil_getAutomine")
	return result, err
}

// Reset resets the blockchain to a fresh state
func (c *AnvilClient) Reset(ctx context.Context, forkURL string, forkBlockNumber uint64) error {
	params := map[string]interface{}{}
	if forkURL != "" {
		params["forking"] = map[string]interface{}{
			"jsonRpcUrl":  forkURL,
			"blockNumber": forkBlockNumber,
		}
	}
	return c.rpcClient.CallContext(ctx, nil, "anvil_reset", params)
}

// NodeInfo returns information about the Anvil node
func (c *AnvilClient) NodeInfo(ctx context.Context) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.rpcClient.CallContext(ctx, &result, "anvil_nodeInfo")
	return result, err
}
