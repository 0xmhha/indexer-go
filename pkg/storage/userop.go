package storage

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// UserOperationRecord represents an EIP-4337 UserOperation event indexed from
// the EntryPoint contract's UserOperationEvent log.
type UserOperationRecord struct {
	// UserOp identity
	UserOpHash common.Hash `json:"userOpHash"`

	// Transaction reference
	TxHash      common.Hash `json:"txHash"`
	BlockNumber uint64      `json:"blockNumber"`
	BlockHash   common.Hash `json:"blockHash"`
	TxIndex     uint64      `json:"txIndex"`
	LogIndex    uint64      `json:"logIndex"`

	// Core fields from UserOperationEvent
	Sender              common.Address `json:"sender"`
	Paymaster           common.Address `json:"paymaster"` // zero address if no paymaster
	Nonce               *big.Int       `json:"nonce"`
	Success             bool           `json:"success"`
	ActualGasCost       *big.Int       `json:"actualGasCost"`
	ActualUserOpFeePerGas *big.Int     `json:"actualUserOpFeePerGas"`

	// Bundler info (tx.from of the handleOps transaction)
	Bundler common.Address `json:"bundler"`

	// EntryPoint contract address that emitted the event
	EntryPoint common.Address `json:"entryPoint"`

	// Timestamp
	Timestamp time.Time `json:"timestamp"`
}

// AccountDeployedRecord represents an EIP-4337 account deployment event.
type AccountDeployedRecord struct {
	UserOpHash common.Hash    `json:"userOpHash"`
	Sender     common.Address `json:"sender"`
	Factory    common.Address `json:"factory"`
	Paymaster  common.Address `json:"paymaster"`

	// Transaction reference
	TxHash      common.Hash `json:"txHash"`
	BlockNumber uint64      `json:"blockNumber"`
	LogIndex    uint64      `json:"logIndex"`

	Timestamp time.Time `json:"timestamp"`
}

// UserOpRevertRecord represents a UserOperation revert reason event.
type UserOpRevertRecord struct {
	UserOpHash   common.Hash    `json:"userOpHash"`
	Sender       common.Address `json:"sender"`
	Nonce        *big.Int       `json:"nonce"`
	RevertReason []byte         `json:"revertReason"`

	// Transaction reference
	TxHash      common.Hash `json:"txHash"`
	BlockNumber uint64      `json:"blockNumber"`
	LogIndex    uint64      `json:"logIndex"`

	// Type: "execution" for UserOperationRevertReason, "postop" for PostOpRevertReason
	RevertType string `json:"revertType"`

	Timestamp time.Time `json:"timestamp"`
}

// BundlerStats represents aggregated statistics for a bundler address.
type BundlerStats struct {
	Address           common.Address `json:"address"`
	TotalOps          int            `json:"totalOps"`
	SuccessfulOps     int            `json:"successfulOps"`
	FailedOps         int            `json:"failedOps"`
	TotalGasSponsored *big.Int       `json:"totalGasSponsored"`
	LastActivityBlock uint64         `json:"lastActivityBlock"`
	LastActivityTime  time.Time      `json:"lastActivityTime"`
}

// PaymasterStats represents aggregated statistics for a paymaster address.
type PaymasterStats struct {
	Address           common.Address `json:"address"`
	TotalOps          int            `json:"totalOps"`
	SuccessfulOps     int            `json:"successfulOps"`
	FailedOps         int            `json:"failedOps"`
	TotalGasSponsored *big.Int       `json:"totalGasSponsored"`
	LastActivityBlock uint64         `json:"lastActivityBlock"`
	LastActivityTime  time.Time      `json:"lastActivityTime"`
}

// UserOpIndexReader defines read operations for EIP-4337 UserOperation indexing
type UserOpIndexReader interface {
	// GetUserOp retrieves a UserOperation by its hash.
	GetUserOp(ctx context.Context, userOpHash common.Hash) (*UserOperationRecord, error)

	// GetUserOpsByTx retrieves all UserOperations in a transaction.
	GetUserOpsByTx(ctx context.Context, txHash common.Hash) ([]*UserOperationRecord, error)

	// GetUserOpsBySender retrieves UserOperations by sender address.
	// Results are ordered by block number descending (newest first).
	GetUserOpsBySender(ctx context.Context, sender common.Address, limit, offset int) ([]*UserOperationRecord, error)

	// GetUserOpsByBundler retrieves UserOperations by bundler address.
	// Results are ordered by block number descending (newest first).
	GetUserOpsByBundler(ctx context.Context, bundler common.Address, limit, offset int) ([]*UserOperationRecord, error)

	// GetUserOpsByPaymaster retrieves UserOperations by paymaster address.
	// Results are ordered by block number descending (newest first).
	GetUserOpsByPaymaster(ctx context.Context, paymaster common.Address, limit, offset int) ([]*UserOperationRecord, error)

	// GetUserOpsByBlock retrieves all UserOperations in a specific block.
	GetUserOpsByBlock(ctx context.Context, blockNumber uint64) ([]*UserOperationRecord, error)

	// GetUserOpsByEntryPoint retrieves UserOperations by EntryPoint address.
	GetUserOpsByEntryPoint(ctx context.Context, entryPoint common.Address, limit, offset int) ([]*UserOperationRecord, error)

	// GetAccountDeployment retrieves an account deployment record by userOpHash.
	GetAccountDeployment(ctx context.Context, userOpHash common.Hash) (*AccountDeployedRecord, error)

	// GetAccountDeploymentsByFactory retrieves deployments by factory address.
	GetAccountDeploymentsByFactory(ctx context.Context, factory common.Address, limit, offset int) ([]*AccountDeployedRecord, error)

	// GetUserOpRevert retrieves a revert reason by userOpHash.
	GetUserOpRevert(ctx context.Context, userOpHash common.Hash) (*UserOpRevertRecord, error)

	// GetBundlerStats retrieves aggregated statistics for a bundler.
	GetBundlerStats(ctx context.Context, bundler common.Address) (*BundlerStats, error)

	// GetPaymasterStats retrieves aggregated statistics for a paymaster.
	GetPaymasterStats(ctx context.Context, paymaster common.Address) (*PaymasterStats, error)

	// GetUserOpCount returns the total count of UserOperations indexed.
	GetUserOpCount(ctx context.Context) (int, error)

	// GetUserOpsCountBySender returns the count of UserOperations for a sender.
	GetUserOpsCountBySender(ctx context.Context, sender common.Address) (int, error)

	// GetUserOpsCountByBundler returns the count of UserOperations for a bundler.
	GetUserOpsCountByBundler(ctx context.Context, bundler common.Address) (int, error)

	// GetUserOpsCountByPaymaster returns the count of UserOperations for a paymaster.
	GetUserOpsCountByPaymaster(ctx context.Context, paymaster common.Address) (int, error)

	// GetRecentUserOps retrieves the most recent UserOperations.
	GetRecentUserOps(ctx context.Context, limit int) ([]*UserOperationRecord, error)

	// GetAllBundlerStats retrieves all bundler statistics with pagination.
	// Results are ordered by total ops descending.
	GetAllBundlerStats(ctx context.Context, limit, offset int) ([]*BundlerStats, error)

	// GetAllBundlerStatsCount returns the total count of known bundlers.
	GetAllBundlerStatsCount(ctx context.Context) (int, error)

	// GetAllPaymasterStats retrieves all paymaster statistics with pagination.
	// Results are ordered by total ops descending.
	GetAllPaymasterStats(ctx context.Context, limit, offset int) ([]*PaymasterStats, error)

	// GetAllPaymasterStatsCount returns the total count of known paymasters.
	GetAllPaymasterStatsCount(ctx context.Context) (int, error)
}

// UserOpIndexWriter defines write operations for EIP-4337 UserOperation indexing
type UserOpIndexWriter interface {
	// SaveUserOp saves a UserOperation record with all necessary indexes.
	SaveUserOp(ctx context.Context, record *UserOperationRecord) error

	// SaveUserOps saves multiple UserOperation records in a batch.
	SaveUserOps(ctx context.Context, records []*UserOperationRecord) error

	// SaveAccountDeployed saves an account deployment record.
	SaveAccountDeployed(ctx context.Context, record *AccountDeployedRecord) error

	// SaveUserOpRevert saves a UserOperation revert reason record.
	SaveUserOpRevert(ctx context.Context, record *UserOpRevertRecord) error

	// IncrementBundlerStats increments bundler statistics.
	IncrementBundlerStats(ctx context.Context, bundler common.Address, success bool, gasCost *big.Int, blockNumber uint64) error

	// IncrementPaymasterStats increments paymaster statistics.
	IncrementPaymasterStats(ctx context.Context, paymaster common.Address, success bool, gasCost *big.Int, blockNumber uint64) error
}

// UserOp revert type constants
const (
	UserOpRevertTypeExecution = "execution"
	UserOpRevertTypePostOp   = "postop"
)
