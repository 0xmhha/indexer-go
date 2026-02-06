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

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/events"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/types/chain"
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

// FeeDelegationMeta contains fee delegation metadata for a transaction
// This is copied from factory package to avoid circular import
type FeeDelegationMeta struct {
	TxHash       common.Hash
	BlockNumber  uint64
	OriginalType uint8
	FeePayer     common.Address
	FeePayerV    *big.Int
	FeePayerR    *big.Int
	FeePayerS    *big.Int
}

// FeeDelegationClient is an optional interface for clients that support
// extracting fee delegation metadata from blocks
type FeeDelegationClient interface {
	// GetBlockWithFeeDelegationMeta retrieves a block along with fee delegation metadata
	GetBlockWithFeeDelegationMeta(ctx context.Context, number uint64) (*types.Block, []*FeeDelegationMeta, error)
}

// BlockProcessor defines an interface for processing blocks after indexing
// This is used by external modules like watchlist to hook into block processing
type BlockProcessor interface {
	ProcessBlock(ctx context.Context, chainID string, block *types.Block, receipts []*types.Receipt) error
}

// TokenIndexer defines an interface for indexing token metadata
// This is called when a new contract is deployed to detect and store token metadata
type TokenIndexer interface {
	// IndexToken detects if the contract is a token and fetches/stores its metadata
	// Returns nil if the contract is not a token or if metadata cannot be fetched
	IndexToken(ctx context.Context, address common.Address, blockHeight uint64) error
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
	HasReceipt(ctx context.Context, hash common.Hash) (bool, error)
	GetMissingReceipts(ctx context.Context, blockNumber uint64) ([]common.Hash, error)
	Close() error
}

// FeeDelegationStorage is an optional interface for storages that support
// storing fee delegation transaction metadata
type FeeDelegationStorage interface {
	SetFeeDelegationTxMeta(ctx context.Context, meta *storagepkg.FeeDelegationTxMeta) error
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
	client                    Client
	storage                   Storage
	config                    *Config
	logger                    *zap.Logger
	eventBus                  *events.EventBus
	metrics                   *RPCMetrics
	optimizer                 *AdaptiveOptimizer
	largeBlockProcessor       *LargeBlockProcessor
	systemContractEventParser *events.SystemContractEventParser

	// chainAdapter provides chain-specific operations (optional)
	// When set, the fetcher will use the adapter for consensus parsing
	// and system contract event handling instead of hardcoded logic.
	chainAdapter chain.Adapter

	// chainID is the chain identifier for multi-chain support
	chainID string

	// blockProcessors are called after block processing to allow external modules
	// (like watchlist) to react to new blocks
	blockProcessors []BlockProcessor
	processorMu     sync.RWMutex

	// tokenIndexer is called when a new contract is deployed to index token metadata
	tokenIndexer TokenIndexer

	// setCodeProcessor handles EIP-7702 SetCode transaction indexing
	setCodeProcessor *SetCodeProcessor
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

	// Initialize system contract event parser
	var systemContractEventParser *events.SystemContractEventParser
	if scWriter, ok := storage.(storagepkg.SystemContractWriter); ok {
		systemContractEventParser = events.NewSystemContractEventParser(scWriter, logger)
		logger.Info("System contract event parser initialized")
	} else {
		logger.Warn("Storage does not support system contract event parsing - continuing without it")
	}

	return &Fetcher{
		client:                    client,
		storage:                   storage,
		config:                    config,
		logger:                    logger,
		eventBus:                  eventBus,
		metrics:                   metrics,
		optimizer:                 optimizer,
		largeBlockProcessor:       largeBlockProcessor,
		systemContractEventParser: systemContractEventParser,
	}
}

// NewFetcherWithAdapter creates a new Fetcher instance with a chain adapter.
// The chain adapter provides chain-specific operations for consensus parsing
// and system contract event handling.
func NewFetcherWithAdapter(client Client, storage Storage, config *Config, logger *zap.Logger, eventBus *events.EventBus, adapter chain.Adapter) *Fetcher {
	fetcher := NewFetcher(client, storage, config, logger, eventBus)
	fetcher.chainAdapter = adapter

	if adapter != nil {
		logger.Info("Fetcher initialized with chain adapter",
			zap.String("chain_type", string(adapter.Info().ChainType)),
			zap.String("consensus_type", string(adapter.Info().ConsensusType)),
		)
	}

	return fetcher
}

// SetChainAdapter sets the chain adapter for the fetcher.
// This allows setting the adapter after construction.
func (f *Fetcher) SetChainAdapter(adapter chain.Adapter) {
	f.chainAdapter = adapter
	if adapter != nil {
		f.logger.Info("Chain adapter set",
			zap.String("chain_type", string(adapter.Info().ChainType)),
			zap.String("consensus_type", string(adapter.Info().ConsensusType)),
		)
	}
}

// GetChainAdapter returns the current chain adapter (may be nil).
func (f *Fetcher) GetChainAdapter() chain.Adapter {
	return f.chainAdapter
}

// SetChainID sets the chain identifier for multi-chain support
func (f *Fetcher) SetChainID(chainID string) {
	f.chainID = chainID
}

// GetChainID returns the chain identifier
func (f *Fetcher) GetChainID() string {
	return f.chainID
}

// SetTokenIndexer sets the token indexer to be called when contracts are deployed
// This enables automatic detection and indexing of token metadata (name, symbol, decimals)
func (f *Fetcher) SetTokenIndexer(indexer TokenIndexer) {
	f.tokenIndexer = indexer
	// Also set on large block processor for consistency
	if f.largeBlockProcessor != nil {
		f.largeBlockProcessor.SetTokenIndexer(indexer)
	}
	f.logger.Info("Token indexer configured")
}

// SetSetCodeProcessor sets the SetCode processor for EIP-7702 transaction indexing
func (f *Fetcher) SetSetCodeProcessor(processor *SetCodeProcessor) {
	f.setCodeProcessor = processor
	// Also set on large block processor for consistency
	if f.largeBlockProcessor != nil {
		f.largeBlockProcessor.SetSetCodeProcessor(processor)
	}
	f.logger.Info("SetCode processor configured")
}

// AddBlockProcessor adds a block processor to be called after each block is indexed
// Block processors receive the block and receipts to process (e.g., watchlist, analytics)
func (f *Fetcher) AddBlockProcessor(processor BlockProcessor) {
	f.processorMu.Lock()
	defer f.processorMu.Unlock()
	f.blockProcessors = append(f.blockProcessors, processor)
	f.logger.Info("Block processor added", zap.Int("total_processors", len(f.blockProcessors)))
}

// RemoveBlockProcessor removes a block processor
func (f *Fetcher) RemoveBlockProcessor(processor BlockProcessor) {
	f.processorMu.Lock()
	defer f.processorMu.Unlock()
	for i, p := range f.blockProcessors {
		if p == processor {
			f.blockProcessors = append(f.blockProcessors[:i], f.blockProcessors[i+1:]...)
			break
		}
	}
}

// processBlockWithProcessors calls all registered block processors
func (f *Fetcher) processBlockWithProcessors(ctx context.Context, block *types.Block, receipts types.Receipts) {
	f.processorMu.RLock()
	processors := make([]BlockProcessor, len(f.blockProcessors))
	copy(processors, f.blockProcessors)
	f.processorMu.RUnlock()

	// Convert receipts to slice of pointers
	receiptPtrs := make([]*types.Receipt, len(receipts))
	copy(receiptPtrs, receipts)

	for _, processor := range processors {
		if err := processor.ProcessBlock(ctx, f.chainID, block, receiptPtrs); err != nil {
			f.logger.Warn("Block processor failed",
				zap.Error(err),
				zap.Uint64("height", block.NumberU64()),
			)
		}
	}
}

// ============================================================================
// Core Fetching API
// ============================================================================

// FetchBlock fetches a single block and its receipts and stores them
func (f *Fetcher) FetchBlock(ctx context.Context, height uint64) error {
	// Fetch block and receipts with retry logic
	startTime := time.Now()
	block, receipts, hadError, err := f.fetchBlockAndReceiptsWithRetry(ctx, height, startTime)
	if err != nil {
		return err
	}

	// Record successful fetch metrics
	if !hadError {
		f.metrics.RecordRequest(time.Since(startTime), false, false)
	}

	// Store block
	if err := f.storage.SetBlock(ctx, block); err != nil {
		return fmt.Errorf("failed to store block %d: %w", height, err)
	}

	// Process metadata and indexing
	if err := f.processBlockMetadata(ctx, block, receipts, height); err != nil {
		return err
	}

	// Process fee delegation metadata
	if err := f.processFeeDelegationMetadata(ctx, height); err != nil {
		// Log but don't fail block processing
		f.logger.Warn("Fee delegation metadata processing failed",
			zap.Uint64("height", height),
			zap.Error(err),
		)
	}

	// Publish block event
	if f.eventBus != nil {
		blockEvent := events.NewBlockEvent(block)
		if !f.eventBus.Publish(blockEvent) {
			f.logger.Warn("Failed to publish block event (channel full)",
				zap.Uint64("height", height),
			)
		}
	}

	// Store receipts and index logs
	if err := f.storeAndProcessReceipts(ctx, block, receipts, height); err != nil {
		return err
	}

	// Publish transaction and log events
	if f.eventBus != nil {
		f.publishBlockEvents(block, receipts, height)
	}

	// Process block with external processors (e.g., watchlist)
	f.processBlockWithProcessors(ctx, block, receipts)

	// Update latest height
	if err := f.storage.SetLatestHeight(ctx, height); err != nil {
		return fmt.Errorf("failed to update latest height to %d: %w", height, err)
	}

	// Record metrics and log success
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

				// Process fee delegation metadata
				if err := f.processFeeDelegationMetadata(ctx, nextHeight); err != nil {
					f.logger.Warn("Fee delegation metadata processing failed",
						zap.Uint64("height", nextHeight),
						zap.Error(err),
					)
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
					if logWriter, ok := f.storage.(storagepkg.LogWriter); ok && len(receipt.Logs) > 0 {
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
					// Build receipt map for O(1) lookup (avoids O(n²) matching)
					receiptMap := buildReceiptMap(res.receipts)
					for i, tx := range transactions {
						// O(1) receipt lookup
						receipt := receiptMap[tx.Hash()]

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
