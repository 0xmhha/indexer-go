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
