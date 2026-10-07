package fetch

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/source"
)

// ============================================================================
// Interfaces
// ============================================================================

// Client defines the interface for RPC client operations
type Client interface {
	GetLatestBlockNumber(ctx context.Context) (uint64, error)
	GetBlockByNumber(ctx context.Context, number uint64) (*types.Block, error)
	GetBlockReceipts(ctx context.Context, blockNumber uint64) (types.Receipts, error)
	GetTransactionByHash(ctx context.Context, hash common.Hash) (*types.Transaction, bool, error)
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
	Close()
}

// PendingTxClient defines the interface for pending transaction subscription
// This is optional and separate from the main Client interface
type PendingTxClient interface {
	GetTransactionByHash(ctx context.Context, hash common.Hash) (*types.Transaction, bool, error)
	SubscribePendingTransactions(ctx context.Context) (<-chan common.Hash, Subscription, error)
}

// Subscription defines the interface for subscription management
type Subscription interface {
	Err() <-chan error
	Unsubscribe()
}

// Storage defines the interface for storage operations
type Storage interface {
	port.BlockReader
	port.BlockWriter
	GetLatestHeight(ctx context.Context) (uint64, error)
	SetLatestHeight(ctx context.Context, height uint64) error
	HasBlock(ctx context.Context, height uint64) (bool, error)
	HasReceipt(ctx context.Context, hash common.Hash) (bool, error)
	GetMissingReceipts(ctx context.Context, blockNumber uint64) ([]common.Hash, error)
	Close() error
}

// ============================================================================
// Config
// ============================================================================

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

	// NumWorkers is the number of concurrent workers for parallel fetching
	// If 0, defaults to 100
	NumWorkers int

	// EnableAdaptiveOptimization enables automatic adjustment of worker count and batch size
	EnableAdaptiveOptimization bool

	// RPCTimeout bounds each RPC call made while indexing (0 = no limit).
	RPCTimeout time.Duration

	// PollInterval is how long the live loop waits before asking the node for
	// a new head once caught up. 0 falls back to RetryDelay. It is separate
	// from RetryDelay (the backoff after errors) so the head can be followed
	// closely without retrying failures aggressively.
	PollInterval time.Duration


	// Finality and Confirmations choose the live loop's target height
	// (see targetHead). The zero value indexes up to the head.
	Finality      string
	Confirmations uint64

	// OptimizerConfig holds configuration for adaptive optimization (optional)
	OptimizerConfig *OptimizerConfig
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
	// NumWorkers can be 0 (will use default)
	return nil
}

// ============================================================================
// Fetcher Struct, Constructors, Setters/Getters
// ============================================================================

// Fetcher handles fetching and indexing blockchain data
type Fetcher struct {
	client              Client
	storage             Storage
	config              *Config
	logger              *zap.Logger
	eventBus            *events.EventBus
	metrics             *RPCMetrics
	optimizer           *AdaptiveOptimizer

	// chainID is the chain identifier for multi-chain support
	chainID string



	// features runs the handlers of the enabled features for every block.
	features *feature.Pipeline

	// writerInst is the goroutine that changes indexed state (writer.go),
	// started on first use.
	writerOnce sync.Once
	writerInst *writer

	// Background work (online backfill), stopped by Close.
	bgOnce   sync.Once
	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup

	noFinalizedWarned time.Time // last "no finalized block" warning (finality.go)

	// src, when set, reads blocks as raw JSON decoded by the chain profile
	// instead of through client (chain profile design, CP-3).
	src source.Source


	// txr opens per-block storage transactions (nil if the storage cannot).
	txr port.BlockTransactor
	// pendingEvents buffers events while a block transaction is open.
	pendingEvents *[]events.Event
	// beforeCommitHook is a fault-injection point for tests.
	beforeCommitHook func(height uint64) error
}

// NewFetcher creates a new Fetcher instance
// eventBus is optional - if nil, no events will be published
func NewFetcher(client Client, storage Storage, config *Config, logger *zap.Logger, eventBus *events.EventBus) *Fetcher {
	// Initialize metrics tracker
	metrics := NewRPCMetrics(constants.DefaultMetricsWindowSize, constants.DefaultRateLimitWindow)

	// Initialize adaptive optimizer if enabled
	var optimizer *AdaptiveOptimizer
	if config.EnableAdaptiveOptimization {
		optimizerConfig := config.OptimizerConfig
		if optimizerConfig == nil {
			optimizerConfig = DefaultOptimizerConfig()
		}
		optimizer = NewAdaptiveOptimizer(metrics, optimizerConfig, logger)

		logger.Info("Adaptive optimization enabled",
			zap.Int("min_workers", optimizerConfig.MinWorkers),
			zap.Int("max_workers", optimizerConfig.MaxWorkers),
			zap.Int("min_batch_size", optimizerConfig.MinBatchSize),
			zap.Int("max_batch_size", optimizerConfig.MaxBatchSize),
			zap.Duration("adjustment_interval", optimizerConfig.AdjustmentInterval),
		)
	}

	// Blocks are indexed in one storage transaction each; a storage without
	// block transactions cannot index (indexBlock fails).
	txr, _ := storage.(port.BlockTransactor)

	return &Fetcher{
		client:              client,
		storage:             storage,
		config:              config,
		logger:              logger,
		eventBus:            eventBus,
		metrics:             metrics,
		optimizer:           optimizer,
		txr:                 txr,
	}
}

// SetChainID sets the chain identifier for multi-chain support
func (f *Fetcher) SetChainID(chainID string) {
	f.chainID = chainID
}

// GetChainID returns the chain identifier
func (f *Fetcher) GetChainID() string {
	return f.chainID
}

// ============================================================================
// Core Fetching API
// ============================================================================

// FetchBlock fetches a single block and its receipts and stores them
func (f *Fetcher) FetchBlock(ctx context.Context, height uint64) error {
	// Fetch block and receipts with retry logic
	startTime := time.Now()
	fb, hadError, err := f.fetchBlockAndReceiptsWithRetry(ctx, height, startTime)
	if err != nil {
		return err
	}

	// Record successful fetch metrics
	if !hadError {
		f.metrics.RecordRequest(time.Since(startTime), false, false)
	}

	return f.indexBlock(ctx, fb)
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

// jobResult holds the result of fetching a single block
type jobResult struct {
	height uint64
	block  *fetchedBlock
	err    error
}

// FetchRangeConcurrent fetches a range of blocks concurrently using a worker pool
func (f *Fetcher) FetchRangeConcurrent(ctx context.Context, start, end uint64) error {
	// Check context cancellation before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	numWorkers := f.config.NumWorkers
	if numWorkers == 0 {
		numWorkers = constants.DefaultNumWorkers // Default worker pool size
	}

	f.logger.Info("Starting concurrent block range fetch",
		zap.Uint64("start", start),
		zap.Uint64("end", end),
		zap.Uint64("total", end-start+1),
		zap.Int("workers", numWorkers),
	)

	totalBlocks := end - start + 1

	// Cancel workers and the producer on any early return below.
	ctx, cancel := context.WithCancel(ctx)

	// Create channels for job distribution and result collection
	jobs := make(chan uint64, numWorkers)
	results := make(chan *jobResult, numWorkers)

	// Start worker pool
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for height := range jobs {
				// Check context cancellation
				if ctx.Err() != nil {
					return
				}

				// Fetch block and receipts with retry logic
				result := f.fetchBlockJob(ctx, height)
				select {
				case results <- result:
				case <-ctx.Done():
					return
				}
			}
		}(i)
	}

	// Send jobs to workers
	go func() {
		for height := start; height <= end; height++ {
			select {
			case <-ctx.Done():
				close(jobs)
				return
			case jobs <- height:
			}
		}
		close(jobs)
	}()

	// Wait for all workers to finish and close results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// On every return: stop the workers and the producer, then drain the
	// results channel (closed after all workers exit) so no goroutine is left
	// blocked on a send.
	defer func() {
		cancel()
		for range results {
		}
	}()

	// Collect results and store blocks in order
	resultMap := make(map[uint64]*jobResult)
	nextHeight := start
	processedCount := uint64(0)

	for result := range results {
		// Handle errors
		if result.err != nil {
			return fmt.Errorf("failed to fetch block %d: %w", result.height, result.err)
		}

		// Store result in map
		resultMap[result.height] = result

		// Process results in sequential order
		for {
			if res, ok := resultMap[nextHeight]; ok {
				if err := f.indexBlock(ctx, res.block); err != nil {
					return err
				}
				delete(resultMap, nextHeight)
				processedCount++
				nextHeight++

				// Log progress periodically
				if processedCount%100 == 0 || processedCount == totalBlocks {
					progress := float64(processedCount) / float64(totalBlocks) * 100
					f.logger.Info("Concurrent fetch progress",
						zap.Uint64("processed", processedCount),
						zap.Uint64("total", totalBlocks),
						zap.Float64("progress", progress),
					)
				}

				// Check if we're done
				if nextHeight > end {
					break
				}
			} else {
				// Next result not ready yet, wait for more results
				break
			}
		}
	}

	f.logger.Info("Completed concurrent block range fetch",
		zap.Uint64("start", start),
		zap.Uint64("end", end),
		zap.Uint64("total", totalBlocks),
		zap.Int("workers", numWorkers),
	)

	return nil
}

// Run starts the fetcher and continuously fetches new blocks
func (f *Fetcher) Run(ctx context.Context) error {
	f.logger.Info("Starting fetcher",
		zap.Uint64("start_height", f.config.StartHeight),
		zap.Int("batch_size", f.config.BatchSize),
	)

	// Get next height to fetch
	nextHeight := f.GetNextHeight(ctx)

	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			f.logger.Info("Fetcher stopped", zap.Error(ctx.Err()))
			return ctx.Err()
		default:
		}

		// Get the highest block to index under the finality policy
		latestChainBlock, ok, err := f.targetHead(ctx)
		if err != nil {
			f.logger.Error("Failed to get target block number", zap.Error(err))
			if err := sleepCtx(ctx, f.config.RetryDelay); err != nil {
				return err
			}
			continue
		}

		// Check if we're caught up
		if !ok || nextHeight > latestChainBlock {
			f.logger.Debug("Caught up with chain",
				zap.Uint64("next_height", nextHeight),
				zap.Uint64("latest_chain_block", latestChainBlock),
			)
			if err := sleepCtx(ctx, f.pollInterval()); err != nil {
				return err
			}
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
			var reorg *ReorgError
			if errors.As(err, &reorg) {
				fork, rerr := f.HandleReorg(ctx, reorg.Height)
				if errors.Is(rerr, ErrReorgTooDeep) {
					f.logger.Error("Stopping: reorganization cannot be rolled back; reindex", zap.Error(rerr))
					return rerr
				}
				if rerr == nil {
					nextHeight = fork + 1
					continue
				}
				err = rerr
			}
			f.logger.Error("Failed to fetch batch", zap.Error(err))
			if err := sleepCtx(ctx, f.config.RetryDelay); err != nil {
				return err
			}
			continue
		}

		// Update next height
		nextHeight = batchEnd + 1
	}
}

// ============================================================================
// Shared Helpers
// ============================================================================

// buildReceiptMap creates a map for O(1) receipt lookup by transaction hash
// This avoids O(n²) complexity when matching transactions to receipts
func buildReceiptMap(receipts types.Receipts) map[common.Hash]*types.Receipt {
	receiptMap := make(map[common.Hash]*types.Receipt, len(receipts))
	for _, receipt := range receipts {
		if receipt != nil {
			receiptMap[receipt.TxHash] = receipt
		}
	}
	return receiptMap
}

// getTransactionSender extracts the sender address from a transaction
// Returns zero address if sender cannot be determined
func getTransactionSender(tx *types.Transaction) common.Address {
	// Try to recover sender from transaction signature
	// This is a simplified version - in production, you'd want proper chain ID
	from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		// Return zero address if we can't recover sender
		return common.Address{}
	}
	return from
}

// pollInterval is the wait between head checks once caught up.
func (f *Fetcher) pollInterval() time.Duration {
	if f.config.PollInterval > 0 {
		return f.config.PollInterval
	}
	return f.config.RetryDelay
}
