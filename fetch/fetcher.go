package fetch

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/events"
	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/storage"
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
	GetLatestHeight(ctx context.Context) (uint64, error)
	GetBlock(ctx context.Context, height uint64) (*types.Block, error)
	GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error)
	SetLatestHeight(ctx context.Context, height uint64) error
	SetBlock(ctx context.Context, block *types.Block) error
	SetReceipt(ctx context.Context, receipt *types.Receipt) error
	HasBlock(ctx context.Context, height uint64) (bool, error)
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

	// NumWorkers is the number of concurrent workers for parallel fetching
	// If 0, defaults to 100
	NumWorkers int

	// EnableAdaptiveOptimization enables automatic adjustment of worker count and batch size
	EnableAdaptiveOptimization bool

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

// Fetcher handles fetching and indexing blockchain data
type Fetcher struct {
	client              Client
	storage             Storage
	config              *Config
	logger              *zap.Logger
	eventBus            *events.EventBus
	metrics             *RPCMetrics
	optimizer           *AdaptiveOptimizer
	largeBlockProcessor *LargeBlockProcessor
}

// NewFetcher creates a new Fetcher instance
// eventBus is optional - if nil, no events will be published
func NewFetcher(client Client, storage Storage, config *Config, logger *zap.Logger, eventBus *events.EventBus) *Fetcher {
	// Initialize metrics tracker
	metrics := NewRPCMetrics(constants.DefaultMetricsWindowSize, constants.DefaultRateLimitWindow)

	// Initialize large block processor
	largeBlockProcessor := NewLargeBlockProcessor(storage, logger)

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

	return &Fetcher{
		client:              client,
		storage:             storage,
		config:              config,
		logger:              logger,
		eventBus:            eventBus,
		metrics:             metrics,
		optimizer:           optimizer,
		largeBlockProcessor: largeBlockProcessor,
	}
}

// FetchBlock fetches a single block and its receipts and stores them
func (f *Fetcher) FetchBlock(ctx context.Context, height uint64) error {
	startTime := time.Now()
	var block *types.Block
	var receipts types.Receipts
	var err error
	var hadError bool

	// Retry logic for fetching block with exponential backoff
	for attempt := 0; attempt <= f.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: delay = baseDelay * 2^(attempt-1)
			backoffDelay := f.config.RetryDelay * time.Duration(1<<uint(attempt-1))
			f.logger.Warn("Retrying block fetch",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Int("max_retries", f.config.MaxRetries),
				zap.Duration("backoff_delay", backoffDelay),
			)
			time.Sleep(backoffDelay)
		}

		// Fetch block
		block, err = f.client.GetBlockByNumber(ctx, height)
		if err != nil {
			hadError = true
			f.logger.Error("Failed to fetch block",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			// Record metrics for failed request
			f.metrics.RecordRequest(time.Since(startTime), true, false)
			if attempt == f.config.MaxRetries {
				return fmt.Errorf("failed to fetch block %d after %d attempts: %w", height, f.config.MaxRetries, err)
			}
			continue
		}

		// Fetch receipts
		receipts, err = f.client.GetBlockReceipts(ctx, height)
		if err != nil {
			hadError = true
			f.logger.Error("Failed to fetch receipts",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			// Record metrics for failed request
			f.metrics.RecordRequest(time.Since(startTime), true, false)
			if attempt == f.config.MaxRetries {
				return fmt.Errorf("failed to fetch receipts for block %d after %d attempts: %w", height, f.config.MaxRetries, err)
			}
			continue
		}

		// Success - break retry loop
		break
	}

	// Record successful fetch metrics if no errors occurred
	if !hadError {
		f.metrics.RecordRequest(time.Since(startTime), false, false)
	}

	// Store block
	if err := f.storage.SetBlock(ctx, block); err != nil {
		return fmt.Errorf("failed to store block %d: %w", height, err)
	}

	// Process WBFT metadata
	if err := f.processWBFTMetadata(ctx, block); err != nil {
		return fmt.Errorf("failed to process WBFT metadata for block %d: %w", height, err)
	}

	// Process address indexing (contract creation, token transfers)
	if err := f.processAddressIndexing(ctx, block, receipts); err != nil {
		return fmt.Errorf("failed to process address indexing for block %d: %w", height, err)
	}

	// Process native balance tracking
	if err := f.processBalanceTracking(ctx, block, receipts); err != nil {
		return fmt.Errorf("failed to process balance tracking for block %d: %w", height, err)
	}

	// Publish block event if EventBus is configured
	if f.eventBus != nil {
		blockEvent := events.NewBlockEvent(block)
		if !f.eventBus.Publish(blockEvent) {
			f.logger.Warn("Failed to publish block event (channel full)",
				zap.Uint64("height", height),
			)
		}
	}

	// Store receipts and index logs
	// Use large block processor for blocks exceeding threshold
	if f.largeBlockProcessor.ShouldProcessInBatches(block, receipts) {
		f.logger.Info("Using parallel processing for large block",
			zap.Uint64("height", height),
			zap.Uint64("gas_used", block.GasUsed()),
			zap.Int("receipt_count", len(receipts)),
		)
		if err := f.largeBlockProcessor.ProcessReceiptsParallel(ctx, block, receipts); err != nil {
			return fmt.Errorf("failed to process large block receipts: %w", err)
		}
	} else {
		// Standard sequential processing for normal blocks
		for _, receipt := range receipts {
			if err := f.storage.SetReceipt(ctx, receipt); err != nil {
				return fmt.Errorf("failed to store receipt for tx %s: %w", receipt.TxHash.Hex(), err)
			}

			// Index logs from this receipt
			if logWriter, ok := f.storage.(storage.LogWriter); ok && len(receipt.Logs) > 0 {
				if err := logWriter.IndexLogs(ctx, receipt.Logs); err != nil {
					f.logger.Warn("failed to index logs",
						zap.String("tx", receipt.TxHash.Hex()),
						zap.Int("logs", len(receipt.Logs)),
						zap.Error(err),
					)
					// Continue processing - log indexing failure shouldn't block block indexing
				}
			}
		}
	}

	// Publish transaction and log events if EventBus is configured
	if f.eventBus != nil {
		transactions := block.Transactions()
		for i, tx := range transactions {
			// Find matching receipt
			var receipt *types.Receipt
			for _, r := range receipts {
				if r.TxHash == tx.Hash() {
					receipt = r
					break
				}
			}

			// Create transaction event
			txEvent := events.NewTransactionEvent(
				tx,
				block.NumberU64(),
				block.Hash(),
				uint(i),
				getTransactionSender(tx),
				receipt,
			)

			if !f.eventBus.Publish(txEvent) {
				f.logger.Warn("Failed to publish transaction event (channel full)",
					zap.String("tx_hash", tx.Hash().Hex()),
					zap.Uint64("block", height),
				)
			}
		}

		for _, receipt := range receipts {
			if receipt == nil {
				continue
			}
			for _, logEntry := range receipt.Logs {
				if logEntry == nil {
					continue
				}
				logEvent := events.NewLogEvent(logEntry)
				if !f.eventBus.Publish(logEvent) {
					f.logger.Warn("Failed to publish log event (channel full)",
						zap.String("tx_hash", logEntry.TxHash.Hex()),
						zap.Uint64("block", logEntry.BlockNumber),
						zap.Uint("log_index", uint(logEntry.Index)),
					)
				}

				// Detect system events from logs
				f.detectSystemEvents(block, logEntry)
			}
		}
	}

	if err := f.storage.SetLatestHeight(ctx, height); err != nil {
		return fmt.Errorf("failed to update latest height to %d: %w", height, err)
	}

	// Record block processing metrics
	f.metrics.RecordBlockProcessed(len(receipts))

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

// jobResult holds the result of fetching a single block
type jobResult struct {
	height   uint64
	block    *types.Block
	receipts types.Receipts
	err      error
}

// GapRange represents a range of missing blocks
type GapRange struct {
	Start uint64
	End   uint64
}

// Size returns the number of blocks in the gap
func (g GapRange) Size() uint64 {
	if g.End < g.Start {
		return 0
	}
	return g.End - g.Start + 1
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
				select {
				case <-ctx.Done():
					results <- &jobResult{height: height, err: ctx.Err()}
					return
				default:
				}

				// Fetch block and receipts with retry logic
				result := f.fetchBlockJob(ctx, height)
				results <- result
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
				// Store block
				if err := f.storage.SetBlock(ctx, res.block); err != nil {
					return fmt.Errorf("failed to store block %d: %w", nextHeight, err)
				}

				// Process WBFT metadata
				if err := f.processWBFTMetadata(ctx, res.block); err != nil {
					return fmt.Errorf("failed to process WBFT metadata for block %d: %w", nextHeight, err)
				}

				// Process address indexing (contract creation, token transfers)
				if err := f.processAddressIndexing(ctx, res.block, res.receipts); err != nil {
					return fmt.Errorf("failed to process address indexing for block %d: %w", nextHeight, err)
				}

				// Process native balance tracking
				if err := f.processBalanceTracking(ctx, res.block, res.receipts); err != nil {
					return fmt.Errorf("failed to process balance tracking for block %d: %w", nextHeight, err)
				}

				// Publish block event if EventBus is configured
				if f.eventBus != nil {
					blockEvent := events.NewBlockEvent(res.block)
					if !f.eventBus.Publish(blockEvent) {
						f.logger.Warn("Failed to publish block event (channel full)",
							zap.Uint64("height", nextHeight),
						)
					}
				}

				// Store receipts and index logs
				for _, receipt := range res.receipts {
					if err := f.storage.SetReceipt(ctx, receipt); err != nil {
						return fmt.Errorf("failed to store receipt for tx %s: %w", receipt.TxHash.Hex(), err)
					}

					// Index logs from this receipt
					if logWriter, ok := f.storage.(storage.LogWriter); ok && len(receipt.Logs) > 0 {
						if err := logWriter.IndexLogs(ctx, receipt.Logs); err != nil {
							f.logger.Warn("failed to index logs",
								zap.String("tx", receipt.TxHash.Hex()),
								zap.Int("logs", len(receipt.Logs)),
								zap.Error(err),
							)
							// Continue processing - log indexing failure shouldn't block block indexing
						}
					}
				}

				// Publish transaction events if EventBus is configured
				if f.eventBus != nil {
					transactions := res.block.Transactions()
					for i, tx := range transactions {
						// Find matching receipt
						var receipt *types.Receipt
						for _, r := range res.receipts {
							if r.TxHash == tx.Hash() {
								receipt = r
								break
							}
						}

						// Create transaction event
						txEvent := events.NewTransactionEvent(
							tx,
							res.block.NumberU64(),
							res.block.Hash(),
							uint(i),
							getTransactionSender(tx),
							receipt,
						)

						if !f.eventBus.Publish(txEvent) {
							f.logger.Warn("Failed to publish transaction event (channel full)",
								zap.String("tx_hash", tx.Hash().Hex()),
								zap.Uint64("block", nextHeight),
							)
						}
					}
				}

				if err := f.storage.SetLatestHeight(ctx, nextHeight); err != nil {
					return fmt.Errorf("failed to update latest height to %d: %w", nextHeight, err)
				}

				f.logger.Debug("Stored block",
					zap.Uint64("height", nextHeight),
					zap.String("hash", res.block.Hash().Hex()),
					zap.Int("txs", len(res.block.Transactions())),
					zap.Int("receipts", len(res.receipts)),
				)

				// Clean up and move to next height
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

// fetchBlockJob fetches a single block and its receipts with retry logic
func (f *Fetcher) fetchBlockJob(ctx context.Context, height uint64) *jobResult {
	var block *types.Block
	var receipts types.Receipts
	var err error

	// Retry logic for fetching block with exponential backoff
	for attempt := 0; attempt <= f.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: delay = baseDelay * 2^(attempt-1)
			backoffDelay := f.config.RetryDelay * time.Duration(1<<uint(attempt-1))
			f.logger.Warn("Retrying block fetch",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Int("max_retries", f.config.MaxRetries),
				zap.Duration("backoff_delay", backoffDelay),
			)
			time.Sleep(backoffDelay)
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return &jobResult{height: height, err: ctx.Err()}
		default:
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
				return &jobResult{
					height: height,
					err:    fmt.Errorf("failed to fetch block after %d attempts: %w", f.config.MaxRetries, err),
				}
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
				return &jobResult{
					height: height,
					err:    fmt.Errorf("failed to fetch receipts after %d attempts: %w", f.config.MaxRetries, err),
				}
			}
			continue
		}

		// Success - break retry loop
		break
	}

	return &jobResult{
		height:   height,
		block:    block,
		receipts: receipts,
		err:      nil,
	}
}

// GetNextHeight determines the next block height to fetch
func (f *Fetcher) GetNextHeight(ctx context.Context) uint64 {
	// Try to get the latest indexed height
	latestHeight, err := f.storage.GetLatestHeight(ctx)
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
	nextHeight := f.GetNextHeight(ctx)

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

// DetectGaps scans the storage for missing blocks and returns gap ranges
func (f *Fetcher) DetectGaps(ctx context.Context, startHeight, endHeight uint64) ([]GapRange, error) {
	f.logger.Info("Scanning for gaps",
		zap.Uint64("start", startHeight),
		zap.Uint64("end", endHeight),
	)

	var gaps []GapRange
	var gapStart uint64
	inGap := false

	for height := startHeight; height <= endHeight; height++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return gaps, ctx.Err()
		default:
		}

		// Check if block exists
		exists, err := f.storage.HasBlock(ctx, height)
		if err != nil {
			return nil, fmt.Errorf("failed to check block %d: %w", height, err)
		}

		if !exists {
			// Start or continue gap
			if !inGap {
				gapStart = height
				inGap = true
			}
		} else {
			// End gap if we were in one
			if inGap {
				gaps = append(gaps, GapRange{
					Start: gapStart,
					End:   height - 1,
				})
				inGap = false
			}
		}

		// Log progress periodically
		if (height-startHeight+1)%1000 == 0 {
			f.logger.Debug("Gap detection progress",
				zap.Uint64("current", height),
				zap.Uint64("end", endHeight),
				zap.Int("gaps_found", len(gaps)),
			)
		}
	}

	// Handle gap at the end
	if inGap {
		gaps = append(gaps, GapRange{
			Start: gapStart,
			End:   endHeight,
		})
	}

	f.logger.Info("Gap detection completed",
		zap.Int("total_gaps", len(gaps)),
		zap.Uint64("start", startHeight),
		zap.Uint64("end", endHeight),
	)

	return gaps, nil
}

// FillGap fills a single gap range by fetching missing blocks
func (f *Fetcher) FillGap(ctx context.Context, gap GapRange) error {
	f.logger.Info("Filling gap",
		zap.Uint64("start", gap.Start),
		zap.Uint64("end", gap.End),
		zap.Uint64("size", gap.Size()),
	)

	// Use concurrent fetching for larger gaps
	if gap.Size() > 10 {
		return f.FetchRangeConcurrent(ctx, gap.Start, gap.End)
	}

	// Use sequential fetching for small gaps
	return f.FetchRange(ctx, gap.Start, gap.End)
}

// FillGaps fills all detected gaps concurrently
func (f *Fetcher) FillGaps(ctx context.Context, gaps []GapRange) error {
	if len(gaps) == 0 {
		f.logger.Info("No gaps to fill")
		return nil
	}

	f.logger.Info("Starting gap filling",
		zap.Int("total_gaps", len(gaps)),
	)

	// Fill each gap sequentially to maintain order and prevent resource exhaustion
	for i, gap := range gaps {
		f.logger.Info("Filling gap",
			zap.Int("gap_num", i+1),
			zap.Int("total_gaps", len(gaps)),
			zap.Uint64("start", gap.Start),
			zap.Uint64("end", gap.End),
			zap.Uint64("size", gap.Size()),
		)

		if err := f.FillGap(ctx, gap); err != nil {
			return fmt.Errorf("failed to fill gap [%d-%d]: %w", gap.Start, gap.End, err)
		}

		f.logger.Info("Gap filled successfully",
			zap.Uint64("start", gap.Start),
			zap.Uint64("end", gap.End),
		)
	}

	f.logger.Info("All gaps filled successfully",
		zap.Int("total_gaps", len(gaps)),
	)

	return nil
}

// RunWithGapRecovery starts the fetcher with automatic gap detection and recovery
func (f *Fetcher) RunWithGapRecovery(ctx context.Context) error {
	f.logger.Info("Starting fetcher with gap recovery enabled",
		zap.Uint64("start_height", f.config.StartHeight),
		zap.Int("batch_size", f.config.BatchSize),
	)

	// First, check for gaps in existing data
	latestHeight, err := f.storage.GetLatestHeight(ctx)
	if err == nil && latestHeight > f.config.StartHeight {
		f.logger.Info("Checking for gaps in existing data",
			zap.Uint64("start", f.config.StartHeight),
			zap.Uint64("end", latestHeight),
		)

		gaps, err := f.DetectGaps(ctx, f.config.StartHeight, latestHeight)
		if err != nil {
			f.logger.Error("Failed to detect gaps", zap.Error(err))
		} else if len(gaps) > 0 {
			f.logger.Info("Found gaps in existing data, filling them first",
				zap.Int("gap_count", len(gaps)),
			)
			if err := f.FillGaps(ctx, gaps); err != nil {
				f.logger.Error("Failed to fill gaps", zap.Error(err))
				// Continue anyway - gaps will be retried later
			}
		}
	}

	// Run normal fetching loop
	return f.Run(ctx)
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

// processWBFTMetadata parses and stores WBFT consensus metadata from block header
func (f *Fetcher) processWBFTMetadata(ctx context.Context, block *types.Block) error {
	// Check if storage implements WBFTWriter
	wbftWriter, ok := f.storage.(storage.WBFTWriter)
	if !ok {
		// Storage doesn't support WBFT metadata - skip silently
		return nil
	}

	// Parse WBFT Extra from block header
	wbftExtra, err := storage.ParseWBFTExtra(block.Header())
	if err != nil {
		// Log warning but don't fail the entire block indexing
		f.logger.Warn("Failed to parse WBFT extra",
			zap.Uint64("height", block.NumberU64()),
			zap.String("hash", block.Hash().Hex()),
			zap.Error(err),
		)
		return nil
	}

	// Save WBFT block extra
	if err := wbftWriter.SaveWBFTBlockExtra(ctx, wbftExtra); err != nil {
		return fmt.Errorf("failed to save WBFT block extra: %w", err)
	}

	// Save epoch info if present
	if wbftExtra.EpochInfo != nil {
		if err := wbftWriter.SaveEpochInfo(ctx, wbftExtra.EpochInfo); err != nil {
			return fmt.Errorf("failed to save epoch info: %w", err)
		}
	}

	// Extract and save validator signing activities
	if wbftExtra.EpochInfo != nil && len(wbftExtra.EpochInfo.Candidates) > 0 {
		var signingActivities []*storage.ValidatorSigningActivity

		// Extract prepare signers
		if wbftExtra.PreparedSeal != nil {
			preparers, err := storage.ExtractSigners(
				wbftExtra.PreparedSeal.Sealers,
				wbftExtra.EpochInfo.Validators,
				wbftExtra.EpochInfo.Candidates,
			)
			if err != nil {
				f.logger.Warn("Failed to extract prepare signers",
					zap.Uint64("height", block.NumberU64()),
					zap.Error(err),
				)
			} else {
				// Create signing activities for preparers
				for i, validator := range wbftExtra.EpochInfo.Candidates {
					activity := &storage.ValidatorSigningActivity{
						BlockNumber:      wbftExtra.BlockNumber,
						BlockHash:        wbftExtra.BlockHash,
						ValidatorAddress: validator.Address,
						ValidatorIndex:   uint32(i),
						SignedPrepare:    containsAddress(preparers, validator.Address),
						SignedCommit:     false, // Will be updated below
						Round:            wbftExtra.Round,
						Timestamp:        wbftExtra.Timestamp,
					}
					signingActivities = append(signingActivities, activity)
				}
			}
		}

		// Extract commit signers
		if wbftExtra.CommittedSeal != nil {
			committers, err := storage.ExtractSigners(
				wbftExtra.CommittedSeal.Sealers,
				wbftExtra.EpochInfo.Validators,
				wbftExtra.EpochInfo.Candidates,
			)
			if err != nil {
				f.logger.Warn("Failed to extract commit signers",
					zap.Uint64("height", block.NumberU64()),
					zap.Error(err),
				)
			} else {
				// Update commit status for existing activities
				for _, activity := range signingActivities {
					activity.SignedCommit = containsAddress(committers, activity.ValidatorAddress)
				}
			}
		}

		// Save validator signing activities
		if len(signingActivities) > 0 {
			if err := wbftWriter.UpdateValidatorSigningStats(ctx, wbftExtra.BlockNumber, signingActivities); err != nil {
				return fmt.Errorf("failed to update validator signing stats: %w", err)
			}
		}
	}

	f.logger.Debug("Processed WBFT metadata",
		zap.Uint64("height", block.NumberU64()),
		zap.Uint32("round", wbftExtra.Round),
		zap.Bool("has_epoch_info", wbftExtra.EpochInfo != nil),
	)

	return nil
}

// containsAddress checks if an address is in a slice of addresses
func containsAddress(addresses []common.Address, target common.Address) bool {
	for _, addr := range addresses {
		if addr == target {
			return true
		}
	}
	return false
}

// processAddressIndexing parses and stores address indexing data from block and receipts
func (f *Fetcher) processAddressIndexing(ctx context.Context, block *types.Block, receipts types.Receipts) error {
	// Check if storage implements AddressIndexWriter
	addressWriter, ok := f.storage.(storage.AddressIndexWriter)
	if !ok {
		// Storage doesn't support address indexing - skip silently
		return nil
	}

	blockNumber := block.NumberU64()
	blockTime := block.Time()
	transactions := block.Transactions()

	// Process each transaction and its receipt
	for _, tx := range transactions {
		// Find matching receipt
		var receipt *types.Receipt
		for _, r := range receipts {
			if r.TxHash == tx.Hash() {
				receipt = r
				break
			}
		}
		if receipt == nil {
			continue
		}

		// 1. Contract Creation Detection
		// Contract creation is indicated by tx.To() == nil
		if tx.To() == nil && receipt.ContractAddress != (common.Address{}) {
			creation := &storage.ContractCreation{
				ContractAddress: receipt.ContractAddress,
				Creator:         getTransactionSender(tx),
				TransactionHash: tx.Hash(),
				BlockNumber:     blockNumber,
				Timestamp:       blockTime,
				BytecodeSize:    len(receipt.ContractAddress.Bytes()), // This is simplified
			}

			if err := addressWriter.SaveContractCreation(ctx, creation); err != nil {
				f.logger.Warn("Failed to save contract creation",
					zap.Uint64("block", blockNumber),
					zap.String("tx", tx.Hash().Hex()),
					zap.String("contract", receipt.ContractAddress.Hex()),
					zap.Error(err),
				)
			}
		}

		// 2. Parse ERC20/ERC721 Transfer Events from Logs
		for _, log := range receipt.Logs {
			if log == nil || len(log.Topics) == 0 {
				continue
			}

			// Check if this is a Transfer event
			// Transfer event topic: keccak256("Transfer(address,address,uint256)")
			if log.Topics[0].Hex() != storage.ERC20TransferTopic {
				continue
			}

			// ERC20: Transfer(indexed from, indexed to, uint256 value) - 3 topics
			// ERC721: Transfer(indexed from, indexed to, indexed tokenId) - 4 topics
			// Note: First topic is the event signature, so total topics are 3 or 4

			if len(log.Topics) == 3 {
				// ERC20 Transfer Event
				if len(log.Topics) < 3 || len(log.Data) < 32 {
					continue
				}

				from := common.BytesToAddress(log.Topics[1].Bytes())
				to := common.BytesToAddress(log.Topics[2].Bytes())
				value := new(big.Int).SetBytes(log.Data)

				transfer := &storage.ERC20Transfer{
					ContractAddress: log.Address,
					From:            from,
					To:              to,
					Value:           value,
					TransactionHash: log.TxHash,
					BlockNumber:     log.BlockNumber,
					LogIndex:        log.Index,
					Timestamp:       blockTime,
				}

				if err := addressWriter.SaveERC20Transfer(ctx, transfer); err != nil {
					f.logger.Warn("Failed to save ERC20 transfer",
						zap.Uint64("block", blockNumber),
						zap.String("tx", tx.Hash().Hex()),
						zap.String("token", log.Address.Hex()),
						zap.Error(err),
					)
				}

			} else if len(log.Topics) == 4 {
				// ERC721 Transfer Event
				from := common.BytesToAddress(log.Topics[1].Bytes())
				to := common.BytesToAddress(log.Topics[2].Bytes())
				tokenId := new(big.Int).SetBytes(log.Topics[3].Bytes())

				transfer := &storage.ERC721Transfer{
					ContractAddress: log.Address,
					From:            from,
					To:              to,
					TokenId:         tokenId,
					TransactionHash: log.TxHash,
					BlockNumber:     log.BlockNumber,
					LogIndex:        log.Index,
					Timestamp:       blockTime,
				}

				if err := addressWriter.SaveERC721Transfer(ctx, transfer); err != nil {
					f.logger.Warn("Failed to save ERC721 transfer",
						zap.Uint64("block", blockNumber),
						zap.String("tx", tx.Hash().Hex()),
						zap.String("token", log.Address.Hex()),
						zap.String("tokenId", tokenId.String()),
						zap.Error(err),
					)
				}
			}
		}
	}

	f.logger.Debug("Processed address indexing",
		zap.Uint64("height", blockNumber),
		zap.Int("transactions", len(transactions)),
	)

	return nil
}

// processBalanceTracking tracks native balance changes from ETH transfers
func (f *Fetcher) processBalanceTracking(ctx context.Context, block *types.Block, receipts types.Receipts) error {
	// Check if storage implements HistoricalWriter
	histWriter, ok := f.storage.(storage.HistoricalWriter)
	if !ok {
		// Storage doesn't support balance tracking - skip silently
		return nil
	}

	blockNumber := block.NumberU64()
	transactions := block.Transactions()

	// Track balance changes for each transaction
	for _, tx := range transactions {
		// Find matching receipt
		var receipt *types.Receipt
		for _, r := range receipts {
			if r.TxHash == tx.Hash() {
				receipt = r
				break
			}
		}
		if receipt == nil {
			continue
		}

		// Get sender address
		from := getTransactionSender(tx)
		if from == (common.Address{}) {
			// Cannot determine sender, skip
			continue
		}

		// Calculate gas cost (gas used * effective gas price)
		gasUsed := new(big.Int).SetUint64(receipt.GasUsed)
		gasPrice := tx.GasPrice()
		if gasPrice == nil {
			gasPrice = big.NewInt(0)
		}
		gasCost := new(big.Int).Mul(gasUsed, gasPrice)

		// Calculate total deduction from sender: value + gas cost
		value := tx.Value()
		if value == nil {
			value = big.NewInt(0)
		}
		totalDeduction := new(big.Int).Add(value, gasCost)

		// Update sender balance (deduct value + gas)
		senderDelta := new(big.Int).Neg(totalDeduction)
		if err := histWriter.UpdateBalance(ctx, from, blockNumber, senderDelta, tx.Hash()); err != nil {
			f.logger.Warn("Failed to update sender balance",
				zap.Uint64("block", blockNumber),
				zap.String("tx", tx.Hash().Hex()),
				zap.String("from", from.Hex()),
				zap.String("delta", senderDelta.String()),
				zap.Error(err),
			)
			// Continue processing - balance tracking failure shouldn't block indexing
		}

		// Update receiver balance (add value only, not gas)
		// Note: For contract creation, tx.To() is nil, so receiver is the contract address
		to := tx.To()
		if to == nil && receipt.ContractAddress != (common.Address{}) {
			// Contract creation - credit the contract address
			to = &receipt.ContractAddress
		}

		if to != nil && value.Sign() > 0 {
			// Only update if there's actual value transfer
			if err := histWriter.UpdateBalance(ctx, *to, blockNumber, value, tx.Hash()); err != nil {
				f.logger.Warn("Failed to update receiver balance",
					zap.Uint64("block", blockNumber),
					zap.String("tx", tx.Hash().Hex()),
					zap.String("to", to.Hex()),
					zap.String("delta", value.String()),
					zap.Error(err),
				)
				// Continue processing
			}
		}
	}

	f.logger.Debug("Processed balance tracking",
		zap.Uint64("height", blockNumber),
		zap.Int("transactions", len(transactions)),
	)

	return nil
}

// GetMetrics returns current performance metrics
func (f *Fetcher) GetMetrics() MetricsSnapshot {
	return f.metrics.GetStats()
}

// LogPerformanceMetrics logs current performance metrics
func (f *Fetcher) LogPerformanceMetrics() {
	stats := f.metrics.GetStats()

	f.logger.Info("Performance Metrics",
		zap.Uint64("total_requests", stats.TotalRequests),
		zap.Uint64("success_requests", stats.SuccessRequests),
		zap.Uint64("error_requests", stats.ErrorRequests),
		zap.Float64("error_rate", stats.ErrorRate),
		zap.Float64("recent_error_rate", stats.RecentErrorRate),
		zap.Uint64("avg_response_ms", stats.AverageResponseTime),
		zap.Uint64("recent_avg_response_ms", stats.RecentAvgResponseTime),
		zap.Uint64("min_response_ms", stats.MinResponseTime),
		zap.Uint64("max_response_ms", stats.MaxResponseTime),
		zap.Uint64("rate_limit_errors", stats.RateLimitErrors),
		zap.Bool("rate_limited", stats.RateLimitDetected),
		zap.Uint64("consecutive_errors", stats.ConsecutiveErrors),
		zap.Uint64("blocks_processed", stats.BlocksProcessed),
		zap.Uint64("receipts_processed", stats.ReceiptsProcessed),
		zap.Float64("throughput_bps", stats.Throughput),
		zap.Int("optimal_workers", stats.OptimalWorkerCount),
		zap.Int("optimal_batch_size", stats.OptimalBatchSize),
		zap.Duration("uptime", stats.Uptime),
	)
}

// OptimizeParameters runs the adaptive optimizer if enabled
func (f *Fetcher) OptimizeParameters() {
	if f.optimizer != nil {
		f.optimizer.Optimize()
	}
}

// GetOptimalWorkerCount returns the recommended worker count
func (f *Fetcher) GetOptimalWorkerCount() int {
	if f.optimizer != nil {
		return f.optimizer.GetRecommendedWorkers()
	}
	return f.config.NumWorkers
}

// GetOptimalBatchSize returns the recommended batch size
func (f *Fetcher) GetOptimalBatchSize() int {
	if f.optimizer != nil {
		return f.optimizer.GetRecommendedBatchSize()
	}
	return f.config.BatchSize
}

// detectSystemEvents detects and publishes system events from logs
func (f *Fetcher) detectSystemEvents(block *types.Block, log *types.Log) {
	if f.eventBus == nil {
		return
	}

	// Check if this is a GovValidator contract event
	if log.Address != events.GovValidatorAddress {
		return
	}

	if len(log.Topics) == 0 {
		return
	}

	eventSig := log.Topics[0]

	// Detect validator set changes
	switch eventSig {
	case events.EventSigMemberAdded:
		// MemberAdded(address,uint256,uint32)
		if len(log.Topics) >= 2 {
			validatorAddr := common.BytesToAddress(log.Topics[1].Bytes())

			validatorEvent := events.NewValidatorSetEvent(
				block.NumberU64(),
				block.Hash(),
				"added",
				validatorAddr,
				"", // validator info from data field if needed
				0,  // set size would need to be tracked separately
			)

			if !f.eventBus.Publish(validatorEvent) {
				f.logger.Warn("Failed to publish validator set event (channel full)",
					zap.String("type", "added"),
					zap.String("validator", validatorAddr.Hex()),
					zap.Uint64("block", block.NumberU64()),
				)
			} else {
				f.logger.Info("Validator added",
					zap.String("validator", validatorAddr.Hex()),
					zap.Uint64("block", block.NumberU64()),
				)
			}
		}

	case events.EventSigMemberRemoved:
		// MemberRemoved(address,uint256,uint32)
		if len(log.Topics) >= 2 {
			validatorAddr := common.BytesToAddress(log.Topics[1].Bytes())

			validatorEvent := events.NewValidatorSetEvent(
				block.NumberU64(),
				block.Hash(),
				"removed",
				validatorAddr,
				"", // validator info from data field if needed
				0,  // set size would need to be tracked separately
			)

			if !f.eventBus.Publish(validatorEvent) {
				f.logger.Warn("Failed to publish validator set event (channel full)",
					zap.String("type", "removed"),
					zap.String("validator", validatorAddr.Hex()),
					zap.Uint64("block", block.NumberU64()),
				)
			} else {
				f.logger.Info("Validator removed",
					zap.String("validator", validatorAddr.Hex()),
					zap.Uint64("block", block.NumberU64()),
				)
			}
		}
	}
}
