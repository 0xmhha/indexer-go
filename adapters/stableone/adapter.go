// Package stableone provides an adapter implementation for the StableOne blockchain.
// StableOne is an EVM-compatible chain with WBFT (Weighted Byzantine Fault Tolerance)
// consensus and custom system contracts for governance.
package stableone

import (
	"context"
	"math/big"

	"github.com/0xmhha/indexer-go/adapters/evm"
	"github.com/0xmhha/indexer-go/consensus"
	_ "github.com/0xmhha/indexer-go/consensus/wbft" // Register WBFT parser
	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/types/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// Ensure Adapter implements chain.Adapter
var _ chain.Adapter = (*Adapter)(nil)

// Config holds configuration for the StableOne adapter
type Config struct {
	// ChainID is the chain identifier
	ChainID *big.Int

	// RPCEndpoint is the primary RPC endpoint
	RPCEndpoint string

	// WSEndpoint is the optional WebSocket endpoint for subscriptions
	WSEndpoint string

	// EpochLength is the number of blocks per epoch (default 10)
	EpochLength uint64
}

// DefaultConfig returns default StableOne configuration
func DefaultConfig() *Config {
	return &Config{
		ChainID:     big.NewInt(1),
		EpochLength: constants.DefaultEpochLength,
	}
}

// Adapter implements chain.Adapter for StableOne chain
// It extends the base EVM adapter with WBFT consensus and system contracts
type Adapter struct {
	*evm.Adapter
	config          *Config
	logger          *zap.Logger
	consensusParser chain.ConsensusParser
	systemContracts *SystemContractsHandler
}

// NewAdapter creates a new StableOne adapter
func NewAdapter(client evm.Client, config *Config, logger *zap.Logger) *Adapter {
	if config == nil {
		config = DefaultConfig()
	}

	// Create EVM config
	evmConfig := &evm.Config{
		ChainID:        config.ChainID,
		ChainName:      constants.DefaultNativeTokenName,
		NativeCurrency: constants.DefaultNativeTokenSymbol,
		Decimals:       constants.DefaultNativeTokenDecimals,
		ConsensusType:  chain.ConsensusTypeWBFT,
	}

	// Create base EVM adapter
	evmAdapter := evm.NewAdapter(client, evmConfig, logger)

	adapter := &Adapter{
		Adapter: evmAdapter,
		config:  config,
		logger:  logger,
	}

	// Get WBFT consensus parser from registry
	consensusConfig := &consensus.Config{
		EpochLength: config.EpochLength,
	}
	consensusParser, err := consensus.Get(chain.ConsensusTypeWBFT, consensusConfig, logger)
	if err != nil {
		logger.Error("Failed to get WBFT parser from registry, using nil",
			zap.Error(err),
		)
	}
	adapter.consensusParser = consensusParser

	// Initialize system contracts handler
	adapter.systemContracts = NewSystemContractsHandler(logger)

	// Set the consensus parser and system contracts on the base adapter
	evmAdapter.SetConsensusParser(adapter.consensusParser)
	evmAdapter.SetSystemContracts(adapter.systemContracts)

	return adapter
}

// Info returns chain metadata (overrides base EVM adapter)
func (a *Adapter) Info() *chain.ChainInfo {
	return &chain.ChainInfo{
		ChainID:        a.config.ChainID,
		ChainType:      chain.ChainTypeEVM,
		ConsensusType:  chain.ConsensusTypeWBFT,
		Name:           constants.DefaultNativeTokenName,
		NativeCurrency: constants.DefaultNativeTokenSymbol,
		Decimals:       constants.DefaultNativeTokenDecimals,
	}
}

// ConsensusParser returns the WBFT consensus parser
func (a *Adapter) ConsensusParser() chain.ConsensusParser {
	return a.consensusParser
}

// SystemContracts returns the system contracts handler
func (a *Adapter) SystemContracts() chain.SystemContractsHandler {
	return a.systemContracts
}

// GetEpochLength returns the configured epoch length
func (a *Adapter) GetEpochLength() uint64 {
	return a.config.EpochLength
}

// GetValidatorsAtBlock returns the validator set at a specific block
// This is a convenience method that delegates to the consensus parser
func (a *Adapter) GetValidatorsAtBlock(ctx context.Context, blockNumber uint64) ([]common.Address, error) {
	return a.consensusParser.GetValidators(ctx, blockNumber)
}

// IsEpochBoundary checks if a block is an epoch boundary
func (a *Adapter) IsEpochBoundary(block *types.Block) bool {
	return a.consensusParser.IsEpochBoundary(block)
}

// GetEpochNumber returns the epoch number for a block
func (a *Adapter) GetEpochNumber(blockNumber uint64) uint64 {
	return constants.CalculateEpochNumber(blockNumber, a.config.EpochLength)
}
