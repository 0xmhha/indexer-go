package storage

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// TransactionType represents the type of transaction relative to an address
type TransactionType int

const (
	// TxTypeAll includes both sent and received transactions
	TxTypeAll TransactionType = iota
	// TxTypeSent includes only transactions sent from the address
	TxTypeSent
	// TxTypeReceived includes only transactions received by the address
	TxTypeReceived
)

// TransactionFilter provides filtering options for transaction queries
type TransactionFilter struct {
	// FromBlock is the starting block number (inclusive)
	FromBlock uint64
	// ToBlock is the ending block number (inclusive)
	ToBlock uint64
	// MinValue is the minimum transaction value (inclusive)
	MinValue *big.Int
	// MaxValue is the maximum transaction value (inclusive)
	MaxValue *big.Int
	// TxType filters by transaction direction
	TxType TransactionType
	// SuccessOnly filters for successful transactions only
	SuccessOnly bool
}

// TransactionWithReceipt combines a transaction with its receipt and location
type TransactionWithReceipt struct {
	// Transaction is the transaction data
	Transaction *types.Transaction
	// Receipt is the transaction receipt
	Receipt *types.Receipt
	// Location is the transaction location (block height, index)
	Location *TxLocation
}

// BalanceSnapshot represents a balance at a specific block
type BalanceSnapshot struct {
	// BlockNumber is the block number for this snapshot
	BlockNumber uint64
	// Balance is the account balance at this block
	Balance *big.Int
	// Delta is the change in balance (positive or negative)
	Delta *big.Int
	// TxHash is the transaction that caused the balance change (may be empty)
	TxHash common.Hash
}

// MinerStats represents mining statistics for a miner address
type MinerStats struct {
	// Address is the miner's address
	Address common.Address
	// BlockCount is the number of blocks mined
	BlockCount uint64
	// LastBlockNumber is the most recent block mined
	LastBlockNumber uint64
	// LastBlockTime is the timestamp of the last block mined
	LastBlockTime uint64
	// Percentage is the percentage of total blocks mined
	Percentage float64
	// TotalRewards is the total mining rewards in Wei
	TotalRewards *big.Int
}

// TokenBalance represents a token balance for an address
type TokenBalance struct {
	// ContractAddress is the token contract address
	ContractAddress common.Address
	// TokenType is the token standard (ERC20, ERC721, ERC1155)
	TokenType string
	// Balance is the token balance (for ERC20) or count (for ERC721)
	Balance *big.Int
	// TokenID is the token ID (for ERC721/ERC1155, empty string for ERC20)
	TokenID string
	// Name is the token name (e.g., "Wrapped Ether")
	Name string
	// Symbol is the token symbol (e.g., "WETH")
	Symbol string
	// Decimals is the number of decimals (for ERC20 only, nil for NFTs)
	Decimals *int
	// Metadata is additional token information as JSON string
	Metadata string
}

// GasStats represents gas usage statistics
type GasStats struct {
	// TotalGasUsed is the total gas used in the range
	TotalGasUsed uint64
	// TotalGasLimit is the total gas limit in the range
	TotalGasLimit uint64
	// AverageGasUsed is the average gas used per block
	AverageGasUsed uint64
	// AverageGasPrice is the average gas price
	AverageGasPrice *big.Int
	// BlockCount is the number of blocks in the range
	BlockCount uint64
	// TransactionCount is the number of transactions in the range
	TransactionCount uint64
}

// AddressGasStats represents gas usage statistics for a specific address
type AddressGasStats struct {
	// Address is the address
	Address common.Address
	// TotalGasUsed is the total gas used by this address
	TotalGasUsed uint64
	// TransactionCount is the number of transactions
	TransactionCount uint64
	// AverageGasPerTx is the average gas per transaction
	AverageGasPerTx uint64
	// TotalFeesPaid is the total fees paid (gas * gasPrice)
	TotalFeesPaid *big.Int
}

// NetworkMetrics represents network activity metrics
type NetworkMetrics struct {
	// TPS is the transactions per second
	TPS float64
	// BlockTime is the average block time in seconds
	BlockTime float64
	// TotalBlocks is the total number of blocks
	TotalBlocks uint64
	// TotalTransactions is the total number of transactions
	TotalTransactions uint64
	// AverageBlockSize is the average block size in gas
	AverageBlockSize uint64
	// TimePeriod is the time period for this metric (in seconds)
	TimePeriod uint64
}

// AddressActivityStats represents activity statistics for an address
type AddressActivityStats struct {
	// Address is the address
	Address common.Address
	// TransactionCount is the total number of transactions
	TransactionCount uint64
	// TotalGasUsed is the total gas used
	TotalGasUsed uint64
	// LastActivityBlock is the most recent block with activity
	LastActivityBlock uint64
	// FirstActivityBlock is the first block with activity
	FirstActivityBlock uint64
}

// HistoricalReader provides read-only access to historical blockchain data
type HistoricalReader interface {
	// GetBlocksByTimeRange returns blocks within a time range
	GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*types.Block, error)

	// GetBlockByTimestamp returns the block closest to the given timestamp
	GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error)

	// GetTransactionsByAddressFiltered returns filtered transactions for an address
	GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *TransactionFilter, limit, offset int) ([]*TransactionWithReceipt, error)

	// GetAddressBalance returns the balance of an address at a specific block
	// If blockNumber is 0, returns the latest balance
	GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error)

	// GetBalanceHistory returns the balance history for an address
	GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]BalanceSnapshot, error)

	// GetBlockCount returns the total number of indexed blocks
	GetBlockCount(ctx context.Context) (uint64, error)

	// GetTransactionCount returns the total number of indexed transactions
	GetTransactionCount(ctx context.Context) (uint64, error)

	// GetTopMiners returns the top miners by block count
	// If fromBlock and toBlock are both 0, returns all-time statistics
	GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]MinerStats, error)

	// GetTokenBalances returns token balances for an address by scanning Transfer events
	// If tokenType is not empty, filters by token type ("ERC20", "ERC721", "ERC1155")
	GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]TokenBalance, error)

	// GetGasStatsByBlockRange returns gas usage statistics for a block range
	GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*GasStats, error)

	// GetGasStatsByAddress returns gas usage statistics for a specific address
	GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*AddressGasStats, error)

	// GetTopAddressesByGasUsed returns the top addresses by total gas used
	GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]AddressGasStats, error)

	// GetTopAddressesByTxCount returns the top addresses by transaction count
	GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]AddressActivityStats, error)

	// GetNetworkMetrics returns network activity metrics for a time range
	GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*NetworkMetrics, error)
}

// HistoricalWriter provides write access for historical blockchain data
type HistoricalWriter interface {
	// SetBlockTimestamp indexes a block by timestamp
	SetBlockTimestamp(ctx context.Context, timestamp uint64, height uint64) error

	// UpdateBalance updates the balance for an address at a specific block
	UpdateBalance(ctx context.Context, addr common.Address, blockNumber uint64, delta *big.Int, txHash common.Hash) error

	// SetBalance sets the balance for an address at a specific block
	SetBalance(ctx context.Context, addr common.Address, blockNumber uint64, balance *big.Int) error
}

// HistoricalStorage combines historical read and write interfaces
type HistoricalStorage interface {
	HistoricalReader
	HistoricalWriter
}

// Validate validates the transaction filter
func (f *TransactionFilter) Validate() error {
	if f.FromBlock > f.ToBlock {
		return fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", f.FromBlock, f.ToBlock)
	}

	if f.MinValue != nil && f.MaxValue != nil {
		if f.MinValue.Cmp(f.MaxValue) > 0 {
			return fmt.Errorf("minValue cannot be greater than maxValue")
		}
	}

	if f.MinValue != nil && f.MinValue.Sign() < 0 {
		return fmt.Errorf("minValue cannot be negative")
	}

	if f.MaxValue != nil && f.MaxValue.Sign() < 0 {
		return fmt.Errorf("maxValue cannot be negative")
	}

	return nil
}

// MatchTransaction checks if a transaction matches the filter criteria
func (f *TransactionFilter) MatchTransaction(tx *types.Transaction, receipt *types.Receipt, location *TxLocation, targetAddr common.Address) bool {
	// Check block range
	if location.BlockHeight < f.FromBlock || location.BlockHeight > f.ToBlock {
		return false
	}

	// Check transaction type
	from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		return false
	}

	to := tx.To()
	switch f.TxType {
	case TxTypeSent:
		if from != targetAddr {
			return false
		}
	case TxTypeReceived:
		if to == nil || *to != targetAddr {
			return false
		}
	case TxTypeAll:
		if from != targetAddr && (to == nil || *to != targetAddr) {
			return false
		}
	}

	// Check value range
	if f.MinValue != nil && tx.Value().Cmp(f.MinValue) < 0 {
		return false
	}

	if f.MaxValue != nil && tx.Value().Cmp(f.MaxValue) > 0 {
		return false
	}

	// Check success status
	if f.SuccessOnly && (receipt == nil || receipt.Status != types.ReceiptStatusSuccessful) {
		return false
	}

	return true
}

// DefaultTransactionFilter returns a default filter with no restrictions
func DefaultTransactionFilter() *TransactionFilter {
	return &TransactionFilter{
		FromBlock:   0,
		ToBlock:     ^uint64(0), // Max uint64
		MinValue:    nil,
		MaxValue:    nil,
		TxType:      TxTypeAll,
		SuccessOnly: false,
	}
}

// FeeDelegationStats represents overall fee delegation statistics
type FeeDelegationStats struct {
	// TotalFeeDelegatedTxs is the total number of fee delegation transactions
	TotalFeeDelegatedTxs uint64
	// TotalFeesSaved is the total fees saved by users (paid by fee payers) in wei
	TotalFeesSaved *big.Int
	// AdoptionRate is the percentage of fee delegation transactions vs total transactions
	AdoptionRate float64
	// AvgFeeSaved is the average fee saved per fee delegation transaction in wei
	AvgFeeSaved *big.Int
}

// FeePayerStats represents statistics for a single fee payer
type FeePayerStats struct {
	// Address is the fee payer address
	Address common.Address
	// TxCount is the number of transactions sponsored by this fee payer
	TxCount uint64
	// TotalFeesPaid is the total fees paid by this fee payer in wei
	TotalFeesPaid *big.Int
	// Percentage is the percentage of total fee delegation transactions
	Percentage float64
}

// FeeDelegationTxMeta stores metadata for Fee Delegation transactions (type 0x16)
// This is stored separately because go-ethereum doesn't support the Fee Delegation type
type FeeDelegationTxMeta struct {
	// TxHash is the transaction hash
	TxHash common.Hash
	// BlockNumber is the block number containing this transaction
	BlockNumber uint64
	// OriginalType is the original transaction type (0x16 for Fee Delegation)
	OriginalType uint8
	// FeePayer is the address that paid the gas fee
	FeePayer common.Address
	// FeePayerV is the V value of fee payer signature
	FeePayerV *big.Int
	// FeePayerR is the R value of fee payer signature
	FeePayerR *big.Int
	// FeePayerS is the S value of fee payer signature
	FeePayerS *big.Int
}

// FeeDelegationReader provides read access to fee delegation statistics
type FeeDelegationReader interface {
	// GetFeeDelegationStats returns overall fee delegation statistics
	// If fromBlock and toBlock are both 0, returns all-time statistics
	GetFeeDelegationStats(ctx context.Context, fromBlock, toBlock uint64) (*FeeDelegationStats, error)

	// GetTopFeePayers returns the top fee payers by transaction count
	// If fromBlock and toBlock are both 0, returns all-time statistics
	GetTopFeePayers(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]FeePayerStats, uint64, error)

	// GetFeePayerStats returns statistics for a specific fee payer
	GetFeePayerStats(ctx context.Context, feePayer common.Address, fromBlock, toBlock uint64) (*FeePayerStats, error)

	// GetFeeDelegationTxMeta returns fee delegation metadata for a transaction
	// Returns nil if the transaction is not a fee delegation transaction
	GetFeeDelegationTxMeta(ctx context.Context, txHash common.Hash) (*FeeDelegationTxMeta, error)

	// GetFeeDelegationTxsByFeePayer returns transaction hashes of fee delegation txs by fee payer
	GetFeeDelegationTxsByFeePayer(ctx context.Context, feePayer common.Address, limit, offset int) ([]common.Hash, error)
}

// FeeDelegationWriter provides write access to fee delegation data
type FeeDelegationWriter interface {
	// SetFeeDelegationTxMeta stores fee delegation metadata for a transaction
	SetFeeDelegationTxMeta(ctx context.Context, meta *FeeDelegationTxMeta) error
}
