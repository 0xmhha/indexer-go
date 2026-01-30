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
	for i, r := range receipts {
		receiptPtrs[i] = r
	}

	for _, processor := range processors {
		if err := processor.ProcessBlock(ctx, f.chainID, block, receiptPtrs); err != nil {
			f.logger.Warn("Block processor failed",
				zap.Error(err),
				zap.Uint64("height", block.NumberU64()),
			)
		}
	}
}

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

// fetchBlockAndReceiptsWithRetry fetches block and receipts with exponential backoff retry logic
func (f *Fetcher) fetchBlockAndReceiptsWithRetry(ctx context.Context, height uint64, startTime time.Time) (*types.Block, types.Receipts, bool, error) {
	var block *types.Block
	var receipts types.Receipts
	var err error
	var hadError bool

	// Retry logic with exponential backoff
	for attempt := 0; attempt <= f.config.MaxRetries; attempt++ {
		if attempt > 0 {
			backoffDelay := f.config.RetryDelay * time.Duration(1<<uint(attempt-1))
			f.logger.Warn("Retrying block fetch",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Int("max_retries", f.config.MaxRetries),
				zap.Duration("backoff_delay", backoffDelay),
			)
			time.Sleep(backoffDelay)
		}

		// Fetch block - use chain adapter if available (for EIP-4844 compatibility)
		if f.chainAdapter != nil {
			block, err = f.chainAdapter.BlockFetcher().GetBlockByNumber(ctx, height)
		} else {
			block, err = f.client.GetBlockByNumber(ctx, height)
		}
		if err != nil {
			hadError = true
			f.logger.Error("Failed to fetch block",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			f.metrics.RecordRequest(time.Since(startTime), true, false)
			if attempt == f.config.MaxRetries {
				return nil, nil, hadError, fmt.Errorf("failed to fetch block %d after %d attempts: %w", height, f.config.MaxRetries, err)
			}
			continue
		}

		// Fetch receipts - use chain adapter if available
		if f.chainAdapter != nil {
			receipts, err = f.chainAdapter.BlockFetcher().GetBlockReceipts(ctx, height)
		} else {
			receipts, err = f.client.GetBlockReceipts(ctx, height)
		}
		if err != nil {
			hadError = true
			f.logger.Error("Failed to fetch receipts",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			f.metrics.RecordRequest(time.Since(startTime), true, false)
			if attempt == f.config.MaxRetries {
				return nil, nil, hadError, fmt.Errorf("failed to fetch receipts for block %d after %d attempts: %w", height, f.config.MaxRetries, err)
			}
			continue
		}

		// Success - break retry loop
		break
	}

	return block, receipts, hadError, nil
}

// processFeeDelegationMetadata extracts and stores fee delegation metadata for a block
func (f *Fetcher) processFeeDelegationMetadata(ctx context.Context, height uint64) error {
	// Check if storage supports fee delegation
	fdStorage, ok := f.storage.(FeeDelegationStorage)
	if !ok {
		return nil // Storage doesn't support fee delegation, skip silently
	}

	// Check if client supports fee delegation metadata extraction
	fdClient, ok := f.client.(FeeDelegationClient)
	if !ok {
		return nil // Client doesn't support fee delegation metadata extraction, skip silently
	}

	// Fetch block with fee delegation metadata
	_, metas, err := fdClient.GetBlockWithFeeDelegationMeta(ctx, height)
	if err != nil {
		f.logger.Warn("Failed to fetch fee delegation metadata",
			zap.Uint64("height", height),
			zap.Error(err),
		)
		return nil // Don't fail block processing for fee delegation metadata extraction failure
	}

	// Store each fee delegation metadata
	for _, meta := range metas {
		storageMeta := &storagepkg.FeeDelegationTxMeta{
			TxHash:       meta.TxHash,
			BlockNumber:  meta.BlockNumber,
			OriginalType: meta.OriginalType,
			FeePayer:     meta.FeePayer,
			FeePayerV:    meta.FeePayerV,
			FeePayerR:    meta.FeePayerR,
			FeePayerS:    meta.FeePayerS,
		}
		if err := fdStorage.SetFeeDelegationTxMeta(ctx, storageMeta); err != nil {
			f.logger.Warn("Failed to store fee delegation metadata",
				zap.String("txHash", meta.TxHash.Hex()),
				zap.Uint64("height", height),
				zap.Error(err),
			)
			// Continue processing other metadata even if one fails
		}
	}

	if len(metas) > 0 {
		f.logger.Debug("Stored fee delegation metadata",
			zap.Uint64("height", height),
			zap.Int("count", len(metas)),
		)
	}

	return nil
}

// processBlockMetadata processes WBFT metadata, address indexing, balance tracking, and genesis initialization
func (f *Fetcher) processBlockMetadata(ctx context.Context, block *types.Block, receipts types.Receipts, height uint64) error {
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

	// Initialize genesis allocation balances (block 0 only)
	if height == 0 {
		if err := f.initializeGenesisBalances(ctx, block); err != nil {
			f.logger.Warn("Failed to initialize genesis balances",
				zap.Uint64("height", height),
				zap.Error(err),
			)
			// Don't fail the entire block processing for genesis balance initialization
		}
	}

	return nil
}

// storeAndProcessReceipts stores receipts and indexes logs using appropriate processing strategy
func (f *Fetcher) storeAndProcessReceipts(ctx context.Context, block *types.Block, receipts types.Receipts, height uint64) error {
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

		// Parse system contract events from large block receipts
		if f.systemContractEventParser != nil {
			for _, receipt := range receipts {
				if len(receipt.Logs) > 0 {
					if err := f.systemContractEventParser.ParseAndIndexLogs(ctx, receipt.Logs); err != nil {
						f.logger.Warn("failed to parse system contract events",
							zap.String("tx", receipt.TxHash.Hex()),
							zap.Int("logs", len(receipt.Logs)),
							zap.Error(err),
						)
					}
				}
			}
		}
	} else {
		// Standard sequential processing for normal blocks
		for _, receipt := range receipts {
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
				}
			}

			// Parse system contract events from this receipt
			if f.systemContractEventParser != nil && len(receipt.Logs) > 0 {
				if err := f.systemContractEventParser.ParseAndIndexLogs(ctx, receipt.Logs); err != nil {
					f.logger.Warn("failed to parse system contract events",
						zap.String("tx", receipt.TxHash.Hex()),
						zap.Int("logs", len(receipt.Logs)),
						zap.Error(err),
					)
				}
			}
		}
	}

	return nil
}

// publishBlockEvents publishes transaction and log events to the event bus
func (f *Fetcher) publishBlockEvents(block *types.Block, receipts types.Receipts, height uint64) {
	transactions := block.Transactions()

	// Build receipt map for O(1) lookup (avoids O(n²) matching)
	receiptMap := buildReceiptMap(receipts)

	// Publish transaction events
	for i, tx := range transactions {
		// O(1) receipt lookup
		receipt := receiptMap[tx.Hash()]

		// Create and publish transaction event
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

	// Publish log events
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

		// Fetch block - use chain adapter if available (for EIP-4844 compatibility)
		if f.chainAdapter != nil {
			block, err = f.chainAdapter.BlockFetcher().GetBlockByNumber(ctx, height)
		} else {
			block, err = f.client.GetBlockByNumber(ctx, height)
		}
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

		// Fetch receipts - use chain adapter if available
		if f.chainAdapter != nil {
			receipts, err = f.chainAdapter.BlockFetcher().GetBlockReceipts(ctx, height)
		} else {
			receipts, err = f.client.GetBlockReceipts(ctx, height)
		}
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

// ReceiptGapInfo contains information about missing receipts for a block
type ReceiptGapInfo struct {
	BlockNumber     uint64
	MissingReceipts []common.Hash
}

// DetectReceiptGaps scans stored blocks for missing receipts
func (f *Fetcher) DetectReceiptGaps(ctx context.Context, startHeight, endHeight uint64) ([]ReceiptGapInfo, error) {
	f.logger.Info("Scanning for receipt gaps",
		zap.Uint64("start", startHeight),
		zap.Uint64("end", endHeight),
	)

	var gaps []ReceiptGapInfo

	for height := startHeight; height <= endHeight; height++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return gaps, ctx.Err()
		default:
		}

		// Check if block exists first
		exists, err := f.storage.HasBlock(ctx, height)
		if err != nil {
			return nil, fmt.Errorf("failed to check block %d: %w", height, err)
		}
		if !exists {
			// Block doesn't exist, skip (will be caught by DetectGaps)
			continue
		}

		// Check for missing receipts in this block
		missingReceipts, err := f.storage.GetMissingReceipts(ctx, height)
		if err != nil {
			f.logger.Warn("failed to check missing receipts",
				zap.Uint64("height", height),
				zap.Error(err),
			)
			continue
		}

		if len(missingReceipts) > 0 {
			gaps = append(gaps, ReceiptGapInfo{
				BlockNumber:     height,
				MissingReceipts: missingReceipts,
			})
		}

		// Log progress periodically
		if (height-startHeight+1)%1000 == 0 {
			f.logger.Debug("Receipt gap detection progress",
				zap.Uint64("current", height),
				zap.Uint64("end", endHeight),
				zap.Int("blocks_with_missing_receipts", len(gaps)),
			)
		}
	}

	totalMissing := 0
	for _, gap := range gaps {
		totalMissing += len(gap.MissingReceipts)
	}

	f.logger.Info("Receipt gap detection completed",
		zap.Int("blocks_with_missing_receipts", len(gaps)),
		zap.Int("total_missing_receipts", totalMissing),
		zap.Uint64("start", startHeight),
		zap.Uint64("end", endHeight),
	)

	return gaps, nil
}

// FillReceiptGap fetches and stores missing receipts for a single block
func (f *Fetcher) FillReceiptGap(ctx context.Context, gap ReceiptGapInfo) error {
	f.logger.Info("Filling receipt gap",
		zap.Uint64("block", gap.BlockNumber),
		zap.Int("missing_count", len(gap.MissingReceipts)),
	)

	// Fetch receipts from RPC
	receipts, err := f.client.GetBlockReceipts(ctx, gap.BlockNumber)
	if err != nil {
		return fmt.Errorf("failed to fetch receipts for block %d: %w", gap.BlockNumber, err)
	}

	// Create a map for quick lookup
	receiptMap := make(map[common.Hash]*types.Receipt)
	for _, receipt := range receipts {
		receiptMap[receipt.TxHash] = receipt
	}

	// Store only the missing receipts
	storedCount := 0
	for _, txHash := range gap.MissingReceipts {
		receipt, exists := receiptMap[txHash]
		if !exists {
			f.logger.Warn("receipt not found from RPC",
				zap.String("tx_hash", txHash.Hex()),
				zap.Uint64("block", gap.BlockNumber),
			)
			continue
		}

		if err := f.storage.SetReceipt(ctx, receipt); err != nil {
			return fmt.Errorf("failed to store receipt for tx %s: %w", txHash.Hex(), err)
		}
		storedCount++
	}

	f.logger.Info("Receipt gap filled",
		zap.Uint64("block", gap.BlockNumber),
		zap.Int("stored", storedCount),
		zap.Int("expected", len(gap.MissingReceipts)),
	)

	return nil
}

// FillReceiptGaps fills all detected receipt gaps
func (f *Fetcher) FillReceiptGaps(ctx context.Context, gaps []ReceiptGapInfo) error {
	if len(gaps) == 0 {
		f.logger.Info("No receipt gaps to fill")
		return nil
	}

	totalMissing := 0
	for _, gap := range gaps {
		totalMissing += len(gap.MissingReceipts)
	}

	f.logger.Info("Starting receipt gap filling",
		zap.Int("blocks_with_gaps", len(gaps)),
		zap.Int("total_missing_receipts", totalMissing),
	)

	for i, gap := range gaps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		f.logger.Debug("Filling receipt gap",
			zap.Int("gap_num", i+1),
			zap.Int("total_gaps", len(gaps)),
			zap.Uint64("block", gap.BlockNumber),
			zap.Int("missing", len(gap.MissingReceipts)),
		)

		if err := f.FillReceiptGap(ctx, gap); err != nil {
			return fmt.Errorf("failed to fill receipt gap for block %d: %w", gap.BlockNumber, err)
		}
	}

	f.logger.Info("All receipt gaps filled successfully",
		zap.Int("blocks_processed", len(gaps)),
		zap.Int("receipts_recovered", totalMissing),
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

		// Check for block gaps
		gaps, err := f.DetectGaps(ctx, f.config.StartHeight, latestHeight)
		if err != nil {
			f.logger.Error("Failed to detect block gaps", zap.Error(err))
		} else if len(gaps) > 0 {
			f.logger.Info("Found block gaps in existing data, filling them first",
				zap.Int("gap_count", len(gaps)),
			)
			if err := f.FillGaps(ctx, gaps); err != nil {
				f.logger.Error("Failed to fill block gaps", zap.Error(err))
				// Continue anyway - gaps will be retried later
			}
		}

		// Check for receipt gaps (blocks exist but receipts missing)
		receiptGaps, err := f.DetectReceiptGaps(ctx, f.config.StartHeight, latestHeight)
		if err != nil {
			f.logger.Error("Failed to detect receipt gaps", zap.Error(err))
		} else if len(receiptGaps) > 0 {
			f.logger.Info("Found receipt gaps in existing data, filling them",
				zap.Int("blocks_with_missing_receipts", len(receiptGaps)),
			)
			if err := f.FillReceiptGaps(ctx, receiptGaps); err != nil {
				f.logger.Error("Failed to fill receipt gaps", zap.Error(err))
				// Continue anyway - gaps will be retried later
			}
		}
	}

	// Run normal fetching loop
	return f.Run(ctx)
}

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

// processWBFTMetadata parses and stores WBFT consensus metadata from block header
func (f *Fetcher) processWBFTMetadata(ctx context.Context, block *types.Block) error {
	// Check if chain adapter indicates non-WBFT consensus - skip silently
	if f.chainAdapter != nil {
		info := f.chainAdapter.Info()
		if info != nil && info.ConsensusType != chain.ConsensusTypeWBFT {
			// Not a WBFT chain, skip WBFT metadata processing
			return nil
		}
	}

	// Check if storage implements WBFTWriter
	wbftWriter, ok := f.storage.(storagepkg.WBFTWriter)
	if !ok {
		// Storage doesn't support WBFT metadata - skip silently
		return nil
	}

	// Parse WBFT Extra from block header
	wbftExtra, err := storagepkg.ParseWBFTExtra(block.Header())
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
		var signingActivities []*storagepkg.ValidatorSigningActivity

		// Extract prepare signers
		if wbftExtra.PreparedSeal != nil {
			preparers, err := storagepkg.ExtractSigners(
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
					activity := &storagepkg.ValidatorSigningActivity{
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
			committers, err := storagepkg.ExtractSigners(
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

	// Publish ConsensusBlockEvent to EventBus for WebSocket subscriptions
	if f.eventBus != nil {
		f.publishConsensusBlockEvent(block, wbftExtra)
	}

	return nil
}

// publishConsensusBlockEvent creates and publishes a ConsensusBlockEvent
func (f *Fetcher) publishConsensusBlockEvent(block *types.Block, wbftExtra *storagepkg.WBFTBlockExtra) {
	// Calculate validator counts
	validatorCount := 0
	prepareCount := 0
	commitCount := 0

	if wbftExtra.EpochInfo != nil {
		validatorCount = len(wbftExtra.EpochInfo.Candidates)
	}

	if wbftExtra.PreparedSeal != nil && wbftExtra.PreparedSeal.Sealers != nil {
		prepareCount = countBitsInBitmap(wbftExtra.PreparedSeal.Sealers)
	}

	if wbftExtra.CommittedSeal != nil && wbftExtra.CommittedSeal.Sealers != nil {
		commitCount = countBitsInBitmap(wbftExtra.CommittedSeal.Sealers)
	}

	// Calculate participation rate
	participationRate := 0.0
	missedValidatorRate := 0.0
	if validatorCount > 0 {
		participationRate = float64(commitCount) / float64(validatorCount) * 100.0
		missedValidatorRate = float64(validatorCount-commitCount) / float64(validatorCount) * 100.0
	}

	// Determine epoch boundary and extract epoch info
	isEpochBoundary := wbftExtra.EpochInfo != nil && len(wbftExtra.EpochInfo.Validators) > 0
	var epochNumber *uint64
	var epochValidators []common.Address

	if isEpochBoundary && wbftExtra.EpochInfo != nil {
		epochNum := wbftExtra.EpochInfo.EpochNumber
		epochNumber = &epochNum
		// Extract validator addresses from candidates using validator indices
		for _, idx := range wbftExtra.EpochInfo.Validators {
			if int(idx) < len(wbftExtra.EpochInfo.Candidates) {
				epochValidators = append(epochValidators, wbftExtra.EpochInfo.Candidates[idx].Address)
			}
		}
	}

	// Create consensus block event
	consensusEvent := events.NewConsensusBlockEvent(
		wbftExtra.BlockNumber,
		wbftExtra.BlockHash,
		wbftExtra.Timestamp,
		wbftExtra.Round,
		wbftExtra.PrevRound,
		block.Coinbase(),
		validatorCount,
		prepareCount,
		commitCount,
		participationRate,
		missedValidatorRate,
		isEpochBoundary,
		epochNumber,
		epochValidators,
	)

	// Publish to EventBus
	if !f.eventBus.Publish(consensusEvent) {
		f.logger.Warn("Failed to publish consensus block event (channel full)",
			zap.Uint64("height", block.NumberU64()),
		)
	}

	// Publish consensus error event if round changed (round > 0)
	if wbftExtra.Round > 0 {
		f.publishConsensusErrorEvent(block, wbftExtra, "round_change", "medium",
			fmt.Sprintf("Consensus required %d rounds to finalize block", wbftExtra.Round+1),
			validatorCount, commitCount, participationRate)
	}

	// Publish consensus error event if low participation (< 67%)
	if participationRate < 67.0 && validatorCount > 0 {
		f.publishConsensusErrorEvent(block, wbftExtra, "low_participation", "high",
			fmt.Sprintf("Low validator participation: %.2f%%", participationRate),
			validatorCount, commitCount, participationRate)
	}
}

// publishConsensusErrorEvent creates and publishes a ConsensusErrorEvent
func (f *Fetcher) publishConsensusErrorEvent(block *types.Block, wbftExtra *storagepkg.WBFTBlockExtra,
	errorType, severity, errorMessage string, expectedValidators, actualSigners int, participationRate float64) {

	// Extract missed validators
	var missedValidators []common.Address
	if wbftExtra.EpochInfo != nil && wbftExtra.CommittedSeal != nil {
		committers, err := storagepkg.ExtractSigners(
			wbftExtra.CommittedSeal.Sealers,
			wbftExtra.EpochInfo.Validators,
			wbftExtra.EpochInfo.Candidates,
		)
		if err == nil {
			for _, candidate := range wbftExtra.EpochInfo.Candidates {
				if !containsAddress(committers, candidate.Address) {
					missedValidators = append(missedValidators, candidate.Address)
				}
			}
		}
	}

	errorEvent := events.NewConsensusErrorEvent(
		wbftExtra.BlockNumber,
		wbftExtra.BlockHash,
		wbftExtra.Timestamp,
		errorType,
		severity,
		errorMessage,
		wbftExtra.Round,
		expectedValidators,
		actualSigners,
		missedValidators,
		participationRate,
		false, // consensusImpacted - block was still finalized
		nil,   // errorDetails
	)

	if !f.eventBus.Publish(errorEvent) {
		f.logger.Warn("Failed to publish consensus error event (channel full)",
			zap.Uint64("height", block.NumberU64()),
			zap.String("errorType", errorType),
		)
	}
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

// countBitsInBitmap counts the number of set bits in a bitmap byte slice
func countBitsInBitmap(bitmap []byte) int {
	count := 0
	for _, b := range bitmap {
		// Count bits using Brian Kernighan's algorithm
		for b != 0 {
			count++
			b &= b - 1
		}
	}
	return count
}

// processAddressIndexing parses and stores address indexing data from block and receipts
func (f *Fetcher) processAddressIndexing(ctx context.Context, block *types.Block, receipts types.Receipts) error {
	// Check if storage implements AddressIndexWriter
	addressWriter, ok := f.storage.(storagepkg.AddressIndexWriter)
	if !ok {
		// Storage doesn't support address indexing - skip silently
		return nil
	}

	// Check if storage implements Writer for transaction address indexing
	storageWriter, hasWriter := f.storage.(storagepkg.Writer)

	blockNumber := block.NumberU64()
	blockTime := block.Time()
	transactions := block.Transactions()

	// Build receipt map for O(1) lookup (avoids O(n²) matching)
	receiptMap := buildReceiptMap(receipts)

	// Fee Delegation transaction type constant (StableNet-specific)
	const FeeDelegateDynamicFeeTxType = 22

	// getFeePayer extracts fee payer from transaction if available
	// Returns nil for standard go-ethereum (Fee Delegation is StableNet-specific)
	getFeePayer := func(tx *types.Transaction) *common.Address {
		return nil // TODO: Implement when using go-stablenet client
	}

	// Process each transaction and its receipt
	for _, tx := range transactions {
		// O(1) receipt lookup
		receipt := receiptMap[tx.Hash()]
		if receipt == nil {
			continue
		}

		// 0. Index transaction addresses (from, to, feePayer) for transactionsByAddress query
		if hasWriter {
			txHash := tx.Hash()

			// Index 'from' address
			from := getTransactionSender(tx)
			if from != (common.Address{}) {
				if err := storageWriter.AddTransactionToAddressIndex(ctx, from, txHash); err != nil {
					f.logger.Warn("Failed to index transaction for from address",
						zap.Uint64("block", blockNumber),
						zap.String("tx", txHash.Hex()),
						zap.String("from", from.Hex()),
						zap.Error(err),
					)
				}
			}

			// Index 'to' address (if not contract creation)
			if tx.To() != nil {
				to := *tx.To()
				if to != from { // Avoid duplicate indexing for self-transfers
					if err := storageWriter.AddTransactionToAddressIndex(ctx, to, txHash); err != nil {
						f.logger.Warn("Failed to index transaction for to address",
							zap.Uint64("block", blockNumber),
							zap.String("tx", txHash.Hex()),
							zap.String("to", to.Hex()),
							zap.Error(err),
						)
					}
				}
			}

			// Index 'feePayer' address for Fee Delegation transactions (type 0x16)
			if tx.Type() == FeeDelegateDynamicFeeTxType {
				if feePayer := getFeePayer(tx); feePayer != nil {
					// Avoid duplicate indexing if feePayer is same as from or to
					if *feePayer != from && (tx.To() == nil || *feePayer != *tx.To()) {
						if err := storageWriter.AddTransactionToAddressIndex(ctx, *feePayer, txHash); err != nil {
							f.logger.Warn("Failed to index transaction for feePayer address",
								zap.Uint64("block", blockNumber),
								zap.String("tx", txHash.Hex()),
								zap.String("feePayer", feePayer.Hex()),
								zap.Error(err),
							)
						}
					}
				}
			}
		}

		// 1. Contract Creation Detection
		// Contract creation is indicated by tx.To() == nil
		if tx.To() == nil && receipt.ContractAddress != (common.Address{}) {
			creation := &storagepkg.ContractCreation{
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
			if log.Topics[0].Hex() != storagepkg.ERC20TransferTopic {
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

				transfer := &storagepkg.ERC20Transfer{
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

				transfer := &storagepkg.ERC721Transfer{
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

// ensureAddressBalanceInitialized checks if an address has balance history,
// and if not, fetches the current balance from RPC and initializes it
func (f *Fetcher) ensureAddressBalanceInitialized(ctx context.Context, histReader storagepkg.HistoricalReader, histWriter storagepkg.HistoricalWriter, addr common.Address, blockNumber uint64) error {
	// Check if address already has balance history
	currentBalance, err := histReader.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		return fmt.Errorf("failed to check address balance: %w", err)
	}

	// If balance is non-zero, address is already initialized
	if currentBalance.Sign() != 0 {
		return nil
	}

	// Check if there's any balance history (even if balance is 0)
	history, err := histReader.GetBalanceHistory(ctx, addr, 0, blockNumber, 1, 0)
	if err != nil {
		return fmt.Errorf("failed to check balance history: %w", err)
	}

	// If there's history, address is already initialized (balance might legitimately be 0)
	if len(history) > 0 {
		return nil
	}

	// No history found - this is the first time we see this address
	// Fetch the actual balance from RPC at the block BEFORE this transaction
	var rpcBlockNumber *big.Int
	if blockNumber > 0 {
		rpcBlockNumber = new(big.Int).SetUint64(blockNumber - 1)
	} else {
		// Genesis block - use block 0
		rpcBlockNumber = big.NewInt(0)
	}

	rpcBalance, err := f.client.BalanceAt(ctx, addr, rpcBlockNumber)
	if err != nil {
		// Log warning but don't fail - balance tracking is best-effort
		f.logger.Warn("Failed to fetch initial balance from RPC, starting from 0",
			zap.String("address", addr.Hex()),
			zap.Uint64("block", blockNumber),
			zap.Error(err),
		)
		// Set initial balance to 0
		rpcBalance = big.NewInt(0)
	}

	// Initialize the balance
	if rpcBalance.Sign() > 0 {
		f.logger.Debug("Initializing address balance from RPC",
			zap.String("address", addr.Hex()),
			zap.Uint64("block", blockNumber),
			zap.String("balance", rpcBalance.String()),
		)
	}

	// Set the initial balance
	return histWriter.SetBalance(ctx, addr, blockNumber, rpcBalance)
}

// initializeGenesisBalances initializes balances for addresses in genesis allocation
// This is called only for block 0 to handle addresses that received initial balance
// but haven't participated in any transactions yet
func (f *Fetcher) initializeGenesisBalances(ctx context.Context, block *types.Block) error {
	// Check if storage supports balance tracking
	histWriter, ok := f.storage.(storagepkg.HistoricalWriter)
	if !ok {
		return nil // Storage doesn't support balance tracking - skip
	}

	histReader, ok := f.storage.(storagepkg.HistoricalReader)
	if !ok {
		return nil // Storage doesn't support balance history - skip
	}

	// Get the block miner (validator) - this is typically a genesis allocation address
	miner := block.Coinbase()

	// Check if miner balance is already initialized
	currentBalance, err := histReader.GetAddressBalance(ctx, miner, 0)
	if err != nil {
		return fmt.Errorf("failed to check miner balance: %w", err)
	}

	// If miner already has a balance recorded, skip initialization
	if currentBalance.Sign() != 0 {
		f.logger.Debug("Genesis miner balance already initialized",
			zap.String("miner", miner.Hex()),
			zap.String("balance", currentBalance.String()),
		)
		return nil
	}

	// Check if there's any balance history for miner
	history, err := histReader.GetBalanceHistory(ctx, miner, 0, 0, 1, 0)
	if err != nil {
		return fmt.Errorf("failed to check miner balance history: %w", err)
	}

	// If there's already history, skip initialization
	if len(history) > 0 {
		f.logger.Debug("Genesis miner already has balance history",
			zap.String("miner", miner.Hex()),
		)
		return nil
	}

	// Fetch the actual balance from RPC at block 0
	rpcBalance, err := f.client.BalanceAt(ctx, miner, big.NewInt(0))
	if err != nil {
		f.logger.Warn("Failed to fetch genesis miner balance from RPC",
			zap.String("miner", miner.Hex()),
			zap.Error(err),
		)
		return err
	}

	// Initialize the balance if non-zero
	if rpcBalance.Sign() > 0 {
		f.logger.Info("Initializing genesis allocation balance",
			zap.String("address", miner.Hex()),
			zap.String("balance", rpcBalance.String()),
		)

		if err := histWriter.SetBalance(ctx, miner, 0, rpcBalance); err != nil {
			return fmt.Errorf("failed to set genesis miner balance: %w", err)
		}
	}

	return nil
}

// processBalanceTracking tracks native balance changes from ETH transfers
func (f *Fetcher) processBalanceTracking(ctx context.Context, block *types.Block, receipts types.Receipts) error {
	// Check if storage implements HistoricalWriter
	histWriter, ok := f.storage.(storagepkg.HistoricalWriter)
	if !ok {
		// Storage doesn't support balance tracking - skip silently
		return nil
	}

	// Also check for HistoricalReader (needed to check if address is initialized)
	histReader, ok := f.storage.(storagepkg.HistoricalReader)
	if !ok {
		// Storage doesn't support historical reading - skip silently
		return nil
	}

	blockNumber := block.NumberU64()
	transactions := block.Transactions()

	// Build receipt map for O(1) lookup (avoids O(n²) matching)
	receiptMap := buildReceiptMap(receipts)

	// Track balance changes for each transaction
	for _, tx := range transactions {
		// O(1) receipt lookup
		receipt := receiptMap[tx.Hash()]
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

		// Ensure sender address balance is initialized from RPC if first time seeing it
		if err := f.ensureAddressBalanceInitialized(ctx, histReader, histWriter, from, blockNumber); err != nil {
			f.logger.Warn("Failed to initialize sender balance",
				zap.String("address", from.Hex()),
				zap.Uint64("block", blockNumber),
				zap.Error(err),
			)
			// Continue - balance tracking is best-effort
		}

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
			// Ensure receiver address balance is initialized from RPC if first time seeing it
			if err := f.ensureAddressBalanceInitialized(ctx, histReader, histWriter, *to, blockNumber); err != nil {
				f.logger.Warn("Failed to initialize receiver balance",
					zap.String("address", to.Hex()),
					zap.Uint64("block", blockNumber),
					zap.Error(err),
				)
				// Continue - balance tracking is best-effort
			}

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

	// Use chain adapter's system contracts handler if available
	if f.chainAdapter != nil && f.chainAdapter.SystemContracts() != nil {
		f.detectSystemEventsWithAdapter(block, log)
		return
	}

	// Fallback to hardcoded logic for backward compatibility
	f.detectSystemEventsLegacy(block, log)
}

// detectSystemEventsWithAdapter uses the chain adapter to detect system events
func (f *Fetcher) detectSystemEventsWithAdapter(block *types.Block, log *types.Log) {
	systemContracts := f.chainAdapter.SystemContracts()

	// Check if this is a system contract
	if !systemContracts.IsSystemContract(log.Address) {
		return
	}

	if len(log.Topics) == 0 {
		return
	}

	// Parse the system contract event
	scEvent, err := systemContracts.ParseSystemContractEvent(log)
	if err != nil {
		f.logger.Debug("Failed to parse system contract event",
			zap.String("contract", log.Address.Hex()),
			zap.Error(err),
		)
		return
	}

	// Handle validator set changes
	if scEvent.EventName == "MemberAdded" || scEvent.EventName == "MemberRemoved" {
		var validatorAddr common.Address
		if member, ok := scEvent.Data["member"].(common.Address); ok {
			validatorAddr = member
		} else if len(log.Topics) >= 2 {
			validatorAddr = common.BytesToAddress(log.Topics[1].Bytes())
		}

		changeType := "added"
		if scEvent.EventName == "MemberRemoved" {
			changeType = "removed"
		}

		validatorEvent := events.NewValidatorSetEvent(
			block.NumberU64(),
			block.Hash(),
			changeType,
			validatorAddr,
			"",
			0,
		)

		if !f.eventBus.Publish(validatorEvent) {
			f.logger.Warn("Failed to publish validator set event (channel full)",
				zap.String("type", changeType),
				zap.String("validator", validatorAddr.Hex()),
				zap.Uint64("block", block.NumberU64()),
			)
		} else {
			f.logger.Info("Validator "+changeType,
				zap.String("validator", validatorAddr.Hex()),
				zap.Uint64("block", block.NumberU64()),
			)
		}
	}
}

// detectSystemEventsLegacy uses hardcoded logic for backward compatibility
func (f *Fetcher) detectSystemEventsLegacy(block *types.Block, log *types.Log) {
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

// StartPendingTxSubscription starts subscribing to pending transactions
// and publishes them to the EventBus. Returns an error channel that receives
// subscription errors. Should be run in a separate goroutine.
func (f *Fetcher) StartPendingTxSubscription(ctx context.Context) (<-chan error, error) {
	if f.eventBus == nil {
		return nil, fmt.Errorf("EventBus is not configured")
	}

	// Check if client supports pending transaction subscription
	pendingClient, ok := f.client.(PendingTxClient)
	if !ok {
		return nil, fmt.Errorf("client does not support pending transaction subscription")
	}

	f.logger.Info("starting pending transaction subscription")

	// Subscribe to pending transactions
	txHashCh, sub, err := pendingClient.SubscribePendingTransactions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to pending transactions: %w", err)
	}

	errCh := make(chan error, 1)

	// Start goroutine to process pending transactions
	go func() {
		defer sub.Unsubscribe()
		defer close(errCh)

		for {
			select {
			case <-ctx.Done():
				f.logger.Info("pending transaction subscription stopped")
				return

			case err := <-sub.Err():
				if err != nil {
					f.logger.Error("pending transaction subscription error", zap.Error(err))
					errCh <- err
					return
				}

			case txHash := <-txHashCh:
				// Fetch full transaction details
				tx, isPending, err := f.client.GetTransactionByHash(ctx, txHash)
				if err != nil {
					f.logger.Warn("failed to fetch pending transaction",
						zap.String("hash", txHash.Hex()),
						zap.Error(err),
					)
					continue
				}

				// Only process if still pending
				if !isPending {
					continue
				}

				// Extract sender address
				signer := types.LatestSignerForChainID(tx.ChainId())
				from, err := signer.Sender(tx)
				if err != nil {
					f.logger.Warn("failed to extract sender",
						zap.String("hash", txHash.Hex()),
						zap.Error(err),
					)
					continue
				}

				// Create transaction event
				txEvent := events.NewTransactionEvent(
					tx,
					0,             // No block number for pending tx
					common.Hash{}, // No block hash for pending tx
					0,             // No index for pending tx
					from,
					nil, // No receipt for pending tx
				)

				// Publish to EventBus
				if !f.eventBus.Publish(txEvent) {
					f.logger.Warn("EventBus channel full, pending transaction dropped",
						zap.String("hash", txHash.Hex()),
					)
				} else {
					f.logger.Debug("published pending transaction event",
						zap.String("hash", txHash.Hex()),
						zap.String("from", from.Hex()),
					)
				}
			}
		}
	}()

	return errCh, nil
}
