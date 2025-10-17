package fetch

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// Client defines the interface for RPC client operations
type Client interface {
	GetLatestBlockNumber(ctx context.Context) (uint64, error)
	GetBlockByNumber(ctx context.Context, number uint64) (*types.Block, error)
	GetBlockReceipts(ctx context.Context, blockNumber uint64) (types.Receipts, error)
	Close()
}

// Storage defines the interface for storage operations
type Storage interface {
	GetLatestHeight() (uint64, error)
	GetBlockByHeight(height uint64) (*types.Block, error)
	GetBlockByHash(hash common.Hash) (*types.Block, error)
	PutBlock(block *types.Block) error
	PutReceipt(receipt *types.Receipt) error
	Close() error
}

// Config holds fetcher configuration
type Config struct {
	// StartHeight is the block height to start indexing from
	StartHeight uint64

	// BatchSize is the number of blocks to fetch in each batch
	BatchSize int

	// MaxRetries is the maximum number of retry attempts for failed operations
	MaxRetries int

	// RetryDelay is the delay between retry attempts
	RetryDelay time.Duration
}

// Validate validates the fetcher configuration
func (c *Config) Validate() error {
	if c.BatchSize <= 0 {
		return fmt.Errorf("batch size must be positive")
	}
	if c.MaxRetries <= 0 {
		return fmt.Errorf("max retries must be positive")
	}
	if c.RetryDelay <= 0 {
		return fmt.Errorf("retry delay must be positive")
	}
	return nil
}

// Fetcher handles fetching and indexing blockchain data
type Fetcher struct {
	client  Client
	storage Storage
	config  *Config
	logger  *zap.Logger
}

// NewFetcher creates a new Fetcher instance
func NewFetcher(client Client, storage Storage, config *Config, logger *zap.Logger) *Fetcher {
	return &Fetcher{
		client:  client,
		storage: storage,
		config:  config,
		logger:  logger,
	}
}

// FetchBlock fetches a single block and its receipts and stores them
func (f *Fetcher) FetchBlock(ctx context.Context, height uint64) error {
	var block *types.Block
	var receipts types.Receipts
	var err error

	// Retry logic for fetching block
	for attempt := 0; attempt <= f.config.MaxRetries; attempt++ {
		if attempt > 0 {
			f.logger.Warn("Retrying block fetch",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Int("max_retries", f.config.MaxRetries),
			)
			time.Sleep(f.config.RetryDelay)
		}

		// Fetch block
		block, err = f.client.GetBlockByNumber(ctx, height)
		if err != nil {
			f.logger.Error("Failed to fetch block",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			if attempt == f.config.MaxRetries {
				return fmt.Errorf("failed to fetch block %d after %d attempts: %w", height, f.config.MaxRetries, err)
			}
			continue
		}

		// Fetch receipts
		receipts, err = f.client.GetBlockReceipts(ctx, height)
		if err != nil {
			f.logger.Error("Failed to fetch receipts",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			if attempt == f.config.MaxRetries {
				return fmt.Errorf("failed to fetch receipts for block %d after %d attempts: %w", height, f.config.MaxRetries, err)
			}
			continue
		}

		// Success - break retry loop
		break
	}

	// Store block
	if err := f.storage.PutBlock(block); err != nil {
		return fmt.Errorf("failed to store block %d: %w", height, err)
	}

	// Store receipts
	for _, receipt := range receipts {
		if err := f.storage.PutReceipt(receipt); err != nil {
			return fmt.Errorf("failed to store receipt for tx %s: %w", receipt.TxHash.Hex(), err)
		}
	}

	f.logger.Info("Successfully indexed block",
		zap.Uint64("height", height),
		zap.String("hash", block.Hash().Hex()),
		zap.Int("txs", len(block.Transactions())),
		zap.Int("receipts", len(receipts)),
	)

	return nil
}

// FetchRange fetches a range of blocks sequentially
func (f *Fetcher) FetchRange(ctx context.Context, start, end uint64) error {
	f.logger.Info("Starting block range fetch",
		zap.Uint64("start", start),
		zap.Uint64("end", end),
		zap.Uint64("total", end-start+1),
	)

	for height := start; height <= end; height++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled at block %d: %w", height, ctx.Err())
		default:
		}

		// Fetch and store block
		if err := f.FetchBlock(ctx, height); err != nil {
			return fmt.Errorf("failed to fetch block %d: %w", height, err)
		}

		// Log progress periodically
		if (height-start+1)%100 == 0 || height == end {
			progress := float64(height-start+1) / float64(end-start+1) * 100
			f.logger.Info("Fetch progress",
				zap.Uint64("current", height),
				zap.Uint64("end", end),
				zap.Float64("progress", progress),
			)
		}
	}

	f.logger.Info("Completed block range fetch",
		zap.Uint64("start", start),
		zap.Uint64("end", end),
		zap.Uint64("total", end-start+1),
	)

	return nil
}

// GetNextHeight determines the next block height to fetch
func (f *Fetcher) GetNextHeight() uint64 {
	// Try to get the latest indexed height
	latestHeight, err := f.storage.GetLatestHeight()
	if err != nil {
		// No blocks indexed yet, start from configured start height
		f.logger.Info("No blocks indexed yet, starting from configured height",
			zap.Uint64("start_height", f.config.StartHeight),
		)
		return f.config.StartHeight
	}

	// If configured start height is higher than latest indexed, use start height
	if f.config.StartHeight > latestHeight {
		f.logger.Info("Configured start height is higher than latest indexed",
			zap.Uint64("start_height", f.config.StartHeight),
			zap.Uint64("latest_height", latestHeight),
		)
		return f.config.StartHeight
	}

	// Continue from next block after latest indexed
	nextHeight := latestHeight + 1
	f.logger.Info("Continuing from latest indexed block",
		zap.Uint64("latest_height", latestHeight),
		zap.Uint64("next_height", nextHeight),
	)
	return nextHeight
}

// Run starts the fetcher and continuously fetches new blocks
func (f *Fetcher) Run(ctx context.Context) error {
	f.logger.Info("Starting fetcher",
		zap.Uint64("start_height", f.config.StartHeight),
		zap.Int("batch_size", f.config.BatchSize),
	)

	// Get next height to fetch
	nextHeight := f.GetNextHeight()

	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			f.logger.Info("Fetcher stopped", zap.Error(ctx.Err()))
			return ctx.Err()
		default:
		}

		// Get latest block from chain
		latestChainBlock, err := f.client.GetLatestBlockNumber(ctx)
		if err != nil {
			f.logger.Error("Failed to get latest block number", zap.Error(err))
			time.Sleep(f.config.RetryDelay)
			continue
		}

		// Check if we're caught up
		if nextHeight > latestChainBlock {
			f.logger.Debug("Caught up with chain",
				zap.Uint64("next_height", nextHeight),
				zap.Uint64("latest_chain_block", latestChainBlock),
			)
			time.Sleep(f.config.RetryDelay)
			continue
		}

		// Calculate batch end
		batchEnd := nextHeight + uint64(f.config.BatchSize) - 1
		if batchEnd > latestChainBlock {
			batchEnd = latestChainBlock
		}

		// Fetch batch
		f.logger.Info("Fetching batch",
			zap.Uint64("start", nextHeight),
			zap.Uint64("end", batchEnd),
			zap.Uint64("size", batchEnd-nextHeight+1),
		)

		if err := f.FetchRange(ctx, nextHeight, batchEnd); err != nil {
			f.logger.Error("Failed to fetch batch", zap.Error(err))
			time.Sleep(f.config.RetryDelay)
			continue
		}

		// Update next height
		nextHeight = batchEnd + 1
	}
}
