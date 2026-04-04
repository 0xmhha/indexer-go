package storage

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/userop"
)

// UserOpIndexReader defines read operations for ERC-4337 UserOperation indexing
type UserOpIndexReader interface {
	// GetUserOp retrieves a specific UserOperation by its hash.
	// Returns ErrNotFound if the UserOperation does not exist.
	GetUserOp(ctx context.Context, opHash common.Hash) (*userop.UserOperation, error)

	// GetUserOpsByTx retrieves all UserOperations in a transaction.
	// Returns empty slice if no UserOperations found.
	GetUserOpsByTx(ctx context.Context, txHash common.Hash) ([]*userop.UserOperation, error)

	// GetUserOpsBySender retrieves UserOperations sent by a specific address.
	// Results are ordered by block number descending (newest first).
	GetUserOpsBySender(ctx context.Context, sender common.Address, limit, offset int) ([]*userop.UserOperation, error)

	// GetUserOpsByBundler retrieves UserOperations bundled by a specific address.
	// Results are ordered by block number descending (newest first).
	GetUserOpsByBundler(ctx context.Context, bundler common.Address, limit, offset int) ([]*userop.UserOperation, error)

	// GetUserOpsByBlock retrieves all UserOperations in a specific block.
	GetUserOpsByBlock(ctx context.Context, blockNumber uint64) ([]*userop.UserOperation, error)

	// GetUserOpsByPaymaster retrieves UserOperations sponsored by a specific paymaster.
	// Results are ordered by block number descending (newest first).
	GetUserOpsByPaymaster(ctx context.Context, paymaster common.Address, limit, offset int) ([]*userop.UserOperation, error)

	// GetUserOpsByFactory retrieves UserOperations that deployed accounts via a specific factory.
	// Results are ordered by block number descending (newest first).
	GetUserOpsByFactory(ctx context.Context, factory common.Address, limit, offset int) ([]*userop.UserOperation, error)

	// GetBundlerStats retrieves statistics for a bundler address.
	// Returns zero-value stats if the bundler has no activity.
	GetBundlerStats(ctx context.Context, bundler common.Address) (*userop.BundlerStats, error)

	// GetFactoryStats retrieves statistics for a factory address.
	// Returns zero-value stats if the factory has no activity.
	GetFactoryStats(ctx context.Context, factory common.Address) (*userop.FactoryStats, error)

	// GetPaymasterStats retrieves statistics for a paymaster address.
	// Returns zero-value stats if the paymaster has no activity.
	GetPaymasterStats(ctx context.Context, paymaster common.Address) (*userop.PaymasterStats, error)

	// GetSmartAccount retrieves a smart account by address.
	// Returns ErrNotFound if the smart account does not exist.
	GetSmartAccount(ctx context.Context, address common.Address) (*userop.SmartAccount, error)

	// GetRecentUserOps retrieves the most recent UserOperations.
	// Results are ordered by block number descending (newest first).
	GetRecentUserOps(ctx context.Context, limit int) ([]*userop.UserOperation, error)

	// GetUserOpCount returns the total count of UserOperations indexed.
	GetUserOpCount(ctx context.Context) (int, error)

	// ListBundlers retrieves bundler stats with pagination.
	ListBundlers(ctx context.Context, limit, offset int) ([]*userop.BundlerStats, error)

	// ListFactories retrieves factory stats with pagination.
	ListFactories(ctx context.Context, limit, offset int) ([]*userop.FactoryStats, error)

	// ListPaymasters retrieves paymaster stats with pagination.
	ListPaymasters(ctx context.Context, limit, offset int) ([]*userop.PaymasterStats, error)

	// ListSmartAccounts retrieves smart accounts with pagination.
	ListSmartAccounts(ctx context.Context, limit, offset int) ([]*userop.SmartAccount, error)
}

// UserOpIndexWriter defines write operations for ERC-4337 UserOperation indexing
type UserOpIndexWriter interface {
	// SaveUserOp saves a UserOperation record.
	// Creates all necessary indexes (sender, bundler, block, tx, paymaster, factory).
	SaveUserOp(ctx context.Context, op *userop.UserOperation) error

	// SaveUserOps saves multiple UserOperation records in a batch.
	// More efficient than calling SaveUserOp multiple times.
	SaveUserOps(ctx context.Context, ops []*userop.UserOperation) error

	// UpdateBundlerStats updates statistics for a bundler address.
	UpdateBundlerStats(ctx context.Context, stats *userop.BundlerStats) error

	// UpdateFactoryStats updates statistics for a factory address.
	UpdateFactoryStats(ctx context.Context, stats *userop.FactoryStats) error

	// UpdatePaymasterStats updates statistics for a paymaster address.
	UpdatePaymasterStats(ctx context.Context, stats *userop.PaymasterStats) error

	// SaveSmartAccount saves or updates a smart account record.
	SaveSmartAccount(ctx context.Context, account *userop.SmartAccount) error
}
