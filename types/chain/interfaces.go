// Package chain defines interfaces for chain-agnostic blockchain indexing.
// These interfaces allow the indexer to support multiple blockchain types
// (EVM-based, Cosmos SDK, etc.) and different consensus mechanisms.
package chain

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ChainType identifies the type of blockchain
type ChainType string

const (
	// ChainTypeEVM represents EVM-compatible chains (Ethereum, BSC, Polygon, etc.)
	ChainTypeEVM ChainType = "evm"

	// ChainTypeCosmos represents Cosmos SDK-based chains
	ChainTypeCosmos ChainType = "cosmos"
)

// ConsensusType identifies the consensus mechanism
type ConsensusType string

const (
	// ConsensusTypeWBFT represents Weighted Byzantine Fault Tolerance
	ConsensusTypeWBFT ConsensusType = "wbft"

	// ConsensusTypePoA represents Proof of Authority
	ConsensusTypePoA ConsensusType = "poa"

	// ConsensusTypePoS represents Proof of Stake
	ConsensusTypePoS ConsensusType = "pos"

	// ConsensusTypeTendermint represents Tendermint BFT
	ConsensusTypeTendermint ConsensusType = "tendermint"

	// ConsensusTypePoW represents Proof of Work
	ConsensusTypePoW ConsensusType = "pow"
)

// ChainInfo provides metadata about a blockchain
type ChainInfo struct {
	// ChainID is the unique identifier for the chain
	ChainID *big.Int

	// ChainType identifies the blockchain type
	ChainType ChainType

	// ConsensusType identifies the consensus mechanism
	ConsensusType ConsensusType

	// Name is a human-readable name for the chain
	Name string

	// NativeCurrency is the native token symbol (e.g., "ETH", "STONE")
	NativeCurrency string

	// Decimals is the native token decimals (typically 18)
	Decimals int
}

// =============================================================================
// Core Chain Adapter Interface
// =============================================================================

// Adapter defines the main interface for chain-specific operations.
// Each blockchain type should implement this interface.
type Adapter interface {
	// Info returns metadata about the chain
	Info() *ChainInfo

	// BlockFetcher returns the block fetching interface
	BlockFetcher() BlockFetcher

	// TransactionParser returns the transaction parsing interface
	TransactionParser() TransactionParser

	// ConsensusParser returns the consensus data parser (optional)
	// Returns nil if the chain doesn't have special consensus data
	ConsensusParser() ConsensusParser

	// SystemContracts returns the system contracts handler (optional)
	// Returns nil if the chain doesn't have system contracts
	SystemContracts() SystemContractsHandler

	// Close releases any resources held by the adapter
	Close() error
}

// =============================================================================
// Block Fetching Interface
// =============================================================================

// BlockFetcher defines the interface for fetching blockchain data.
// This is the primary interface for retrieving blocks and transactions.
type BlockFetcher interface {
	// GetLatestBlockNumber returns the most recent block number
	GetLatestBlockNumber(ctx context.Context) (uint64, error)

	// GetBlockByNumber retrieves a block by its height
	// Returns EVM types.Block for EVM chains
	GetBlockByNumber(ctx context.Context, number uint64) (*types.Block, error)

	// GetBlockByHash retrieves a block by its hash
	GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error)

	// GetBlockReceipts retrieves all receipts for a block
	GetBlockReceipts(ctx context.Context, blockNumber uint64) (types.Receipts, error)

	// GetTransactionByHash retrieves a transaction by its hash
	// Returns (transaction, isPending, error)
	GetTransactionByHash(ctx context.Context, hash common.Hash) (*types.Transaction, bool, error)

	// BalanceAt returns the balance of an account at a specific block
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)

	// Close releases the connection
	Close()
}

// =============================================================================
// Transaction Parsing Interface
// =============================================================================

// TransactionParser defines the interface for parsing transaction data.
// Different chains may have different transaction formats and metadata.
type TransactionParser interface {
	// ParseTransaction extracts metadata from a transaction
	ParseTransaction(tx *types.Transaction, receipt *types.Receipt) (*TransactionData, error)

	// ParseLogs extracts event data from transaction logs
	ParseLogs(logs []*types.Log) ([]*EventData, error)

	// IsContractCreation checks if transaction creates a contract
	IsContractCreation(tx *types.Transaction) bool

	// GetContractAddress returns the created contract address
	GetContractAddress(tx *types.Transaction, receipt *types.Receipt) *common.Address
}

// TransactionData holds parsed transaction metadata
type TransactionData struct {
	// Basic transaction info
	Hash        common.Hash
	From        common.Address
	To          *common.Address
	Value       *big.Int
	GasPrice    *big.Int
	GasLimit    uint64
	GasUsed     uint64
	Nonce       uint64
	InputData   []byte
	BlockNumber uint64
	BlockHash   common.Hash
	TxIndex     uint

	// Status and results
	Status          uint64 // 1 = success, 0 = failed
	ContractAddress *common.Address

	// Chain-specific metadata (can be extended)
	Metadata map[string]interface{}
}

// EventData holds parsed event/log data
type EventData struct {
	Address     common.Address
	Topics      []common.Hash
	Data        []byte
	BlockNumber uint64
	TxHash      common.Hash
	TxIndex     uint
	LogIndex    uint
	Removed     bool

	// Decoded event data (if ABI available)
	EventName string
	Decoded   map[string]interface{}
}

// =============================================================================
// Consensus Parsing Interface
// =============================================================================

// ConsensusParser defines the interface for parsing consensus-specific data.
// This is optional and only needed for chains with special consensus mechanisms.
type ConsensusParser interface {
	// ConsensusType returns the type of consensus this parser handles
	ConsensusType() ConsensusType

	// ParseConsensusData extracts consensus information from a block
	ParseConsensusData(block *types.Block) (*ConsensusData, error)

	// GetValidators returns the current validator set (if applicable)
	GetValidators(ctx context.Context, blockNumber uint64) ([]common.Address, error)

	// IsEpochBoundary checks if the block is an epoch boundary
	IsEpochBoundary(block *types.Block) bool
}

// ConsensusData holds parsed consensus information
type ConsensusData struct {
	// Common consensus fields
	ConsensusType     ConsensusType
	BlockNumber       uint64
	BlockHash         common.Hash
	ProposerAddress   common.Address
	ParticipationRate float64

	// Validator information
	ValidatorCount   int
	SignedValidators []common.Address

	// Epoch information (for PoS/BFT chains)
	IsEpochBoundary bool
	EpochNumber     *uint64
	EpochValidators []common.Address

	// Chain-specific data (WBFT, Tendermint, etc.)
	ExtraData interface{}
}

// =============================================================================
// System Contracts Interface
// =============================================================================

// SystemContractsHandler defines the interface for handling system/precompiled contracts.
// This is optional and specific to chains with built-in governance contracts.
type SystemContractsHandler interface {
	// IsSystemContract checks if an address is a system contract
	IsSystemContract(addr common.Address) bool

	// GetSystemContractName returns the name of a system contract
	GetSystemContractName(addr common.Address) string

	// GetSystemContractAddresses returns all system contract addresses
	GetSystemContractAddresses() []common.Address

	// ParseSystemContractEvent parses an event from a system contract
	ParseSystemContractEvent(log *types.Log) (*SystemContractEvent, error)
}

// SystemContractEvent holds parsed system contract event data
type SystemContractEvent struct {
	ContractAddress common.Address
	ContractName    string
	EventName       string
	BlockNumber     uint64
	TxHash          common.Hash
	LogIndex        uint
	Data            map[string]interface{}
}

// =============================================================================
// Factory Interface
// =============================================================================

// AdapterFactory creates chain adapters based on configuration
type AdapterFactory interface {
	// CreateAdapter creates a new chain adapter
	CreateAdapter(config *AdapterConfig) (Adapter, error)

	// SupportedChains returns the list of supported chain types
	SupportedChains() []ChainType

	// SupportedConsensus returns the list of supported consensus types
	SupportedConsensus() []ConsensusType
}

// AdapterConfig holds configuration for creating a chain adapter
type AdapterConfig struct {
	// Chain identification
	ChainType     ChainType
	ConsensusType ConsensusType
	ChainID       *big.Int

	// Connection settings
	RPCEndpoint string
	WSEndpoint  string // Optional WebSocket endpoint

	// Chain-specific settings
	Settings map[string]interface{}
}
