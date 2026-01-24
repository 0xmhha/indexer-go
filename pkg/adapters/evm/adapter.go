// Package evm provides an adapter implementation for EVM-compatible blockchains.
// This adapter can be used as a base for any Ethereum-compatible chain
// (Ethereum mainnet, BSC, Polygon, Avalanche C-Chain, etc.)
package evm

import (
	"context"
	"math/big"

	"github.com/0xmhha/indexer-go/pkg/types/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// Ensure EVMAdapter implements chain.Adapter
var _ chain.Adapter = (*Adapter)(nil)

// Client defines the interface for EVM RPC operations.
// This matches the existing fetch.Client interface for compatibility.
type Client interface {
	GetLatestBlockNumber(ctx context.Context) (uint64, error)
	GetBlockByNumber(ctx context.Context, number uint64) (*types.Block, error)
	GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error)
	GetBlockReceipts(ctx context.Context, blockNumber uint64) (types.Receipts, error)
	GetTransactionByHash(ctx context.Context, hash common.Hash) (*types.Transaction, bool, error)
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
	Close()
}

// Config holds configuration for the EVM adapter
type Config struct {
	// ChainID is the chain identifier
	ChainID *big.Int

	// ChainName is a human-readable name
	ChainName string

	// NativeCurrency is the native token symbol
	NativeCurrency string

	// Decimals for native currency (default 18)
	Decimals int

	// ConsensusType for this EVM chain (default PoW)
	ConsensusType chain.ConsensusType
}

// DefaultConfig returns default EVM configuration
func DefaultConfig() *Config {
	return &Config{
		ChainID:        big.NewInt(1), // Ethereum mainnet
		ChainName:      "Ethereum",
		NativeCurrency: "ETH",
		Decimals:       18,
		ConsensusType:  chain.ConsensusTypePoS,
	}
}

// Adapter implements chain.Adapter for EVM-compatible chains
type Adapter struct {
	client            Client
	config            *Config
	logger            *zap.Logger
	blockFetcher      *BlockFetcher
	transactionParser *TransactionParser
	consensusParser   chain.ConsensusParser // Optional, can be set by extending adapters
	systemContracts   chain.SystemContractsHandler
}

// NewAdapter creates a new EVM adapter
func NewAdapter(client Client, config *Config, logger *zap.Logger) *Adapter {
	if config == nil {
		config = DefaultConfig()
	}
	if config.Decimals == 0 {
		config.Decimals = 18
	}

	adapter := &Adapter{
		client: client,
		config: config,
		logger: logger,
	}

	// Initialize block fetcher
	adapter.blockFetcher = &BlockFetcher{
		client: client,
		logger: logger,
	}

	// Initialize transaction parser
	adapter.transactionParser = &TransactionParser{
		logger: logger,
	}

	return adapter
}

// Info returns chain metadata
func (a *Adapter) Info() *chain.ChainInfo {
	return &chain.ChainInfo{
		ChainID:        a.config.ChainID,
		ChainType:      chain.ChainTypeEVM,
		ConsensusType:  a.config.ConsensusType,
		Name:           a.config.ChainName,
		NativeCurrency: a.config.NativeCurrency,
		Decimals:       a.config.Decimals,
	}
}

// BlockFetcher returns the block fetching interface
func (a *Adapter) BlockFetcher() chain.BlockFetcher {
	return a.blockFetcher
}

// TransactionParser returns the transaction parsing interface
func (a *Adapter) TransactionParser() chain.TransactionParser {
	return a.transactionParser
}

// ConsensusParser returns the consensus parser (nil for generic EVM)
func (a *Adapter) ConsensusParser() chain.ConsensusParser {
	return a.consensusParser
}

// SetConsensusParser allows extending adapters to set a custom consensus parser
func (a *Adapter) SetConsensusParser(parser chain.ConsensusParser) {
	a.consensusParser = parser
}

// SystemContracts returns the system contracts handler (nil for generic EVM)
func (a *Adapter) SystemContracts() chain.SystemContractsHandler {
	return a.systemContracts
}

// SetSystemContracts allows extending adapters to set a custom system contracts handler
func (a *Adapter) SetSystemContracts(handler chain.SystemContractsHandler) {
	a.systemContracts = handler
}

// Close releases resources
func (a *Adapter) Close() error {
	if a.client != nil {
		a.client.Close()
	}
	return nil
}

// GetClient returns the underlying RPC client
// This is useful for extending adapters that need direct client access
func (a *Adapter) GetClient() Client {
	return a.client
}

// =============================================================================
// Block Fetcher Implementation
// =============================================================================

// BlockFetcher implements chain.BlockFetcher for EVM chains
type BlockFetcher struct {
	client Client
	logger *zap.Logger
}

// Ensure BlockFetcher implements chain.BlockFetcher
var _ chain.BlockFetcher = (*BlockFetcher)(nil)

// GetLatestBlockNumber returns the latest block number
func (f *BlockFetcher) GetLatestBlockNumber(ctx context.Context) (uint64, error) {
	return f.client.GetLatestBlockNumber(ctx)
}

// GetBlockByNumber retrieves a block by number
func (f *BlockFetcher) GetBlockByNumber(ctx context.Context, number uint64) (*types.Block, error) {
	return f.client.GetBlockByNumber(ctx, number)
}

// GetBlockByHash retrieves a block by hash
func (f *BlockFetcher) GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return f.client.GetBlockByHash(ctx, hash)
}

// GetBlockReceipts retrieves all receipts for a block
func (f *BlockFetcher) GetBlockReceipts(ctx context.Context, blockNumber uint64) (types.Receipts, error) {
	return f.client.GetBlockReceipts(ctx, blockNumber)
}

// GetTransactionByHash retrieves a transaction by hash
func (f *BlockFetcher) GetTransactionByHash(ctx context.Context, hash common.Hash) (*types.Transaction, bool, error) {
	return f.client.GetTransactionByHash(ctx, hash)
}

// BalanceAt returns the balance of an account
func (f *BlockFetcher) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	return f.client.BalanceAt(ctx, account, blockNumber)
}

// Close releases the client connection
func (f *BlockFetcher) Close() {
	f.client.Close()
}

// =============================================================================
// Transaction Parser Implementation
// =============================================================================

// TransactionParser implements chain.TransactionParser for EVM chains
type TransactionParser struct {
	logger *zap.Logger
}

// Ensure TransactionParser implements chain.TransactionParser
var _ chain.TransactionParser = (*TransactionParser)(nil)

// ParseTransaction extracts metadata from a transaction
func (p *TransactionParser) ParseTransaction(tx *types.Transaction, receipt *types.Receipt) (*chain.TransactionData, error) {
	if tx == nil {
		return nil, nil
	}

	// Get sender address
	signer := types.LatestSignerForChainID(tx.ChainId())
	from, err := types.Sender(signer, tx)
	if err != nil {
		// Try legacy signer as fallback
		from, err = types.Sender(types.HomesteadSigner{}, tx)
		if err != nil {
			return nil, err
		}
	}

	data := &chain.TransactionData{
		Hash:      tx.Hash(),
		From:      from,
		To:        tx.To(),
		Value:     tx.Value(),
		GasPrice:  tx.GasPrice(),
		GasLimit:  tx.Gas(),
		Nonce:     tx.Nonce(),
		InputData: tx.Data(),
		Metadata:  make(map[string]interface{}),
	}

	// Add receipt data if available
	if receipt != nil {
		data.GasUsed = receipt.GasUsed
		data.Status = receipt.Status
		data.BlockNumber = receipt.BlockNumber.Uint64()
		data.BlockHash = receipt.BlockHash
		data.TxIndex = receipt.TransactionIndex
		data.ContractAddress = p.GetContractAddress(tx, receipt)
	}

	return data, nil
}

// ParseLogs extracts event data from logs
func (p *TransactionParser) ParseLogs(logs []*types.Log) ([]*chain.EventData, error) {
	events := make([]*chain.EventData, 0, len(logs))

	for _, log := range logs {
		event := &chain.EventData{
			Address:     log.Address,
			Topics:      log.Topics,
			Data:        log.Data,
			BlockNumber: log.BlockNumber,
			TxHash:      log.TxHash,
			TxIndex:     log.TxIndex,
			LogIndex:    log.Index,
			Removed:     log.Removed,
		}
		events = append(events, event)
	}

	return events, nil
}

// IsContractCreation checks if a transaction creates a contract
func (p *TransactionParser) IsContractCreation(tx *types.Transaction) bool {
	return tx.To() == nil
}

// GetContractAddress returns the created contract address
func (p *TransactionParser) GetContractAddress(tx *types.Transaction, receipt *types.Receipt) *common.Address {
	if !p.IsContractCreation(tx) {
		return nil
	}
	if receipt == nil || receipt.ContractAddress == (common.Address{}) {
		return nil
	}
	addr := receipt.ContractAddress
	return &addr
}
