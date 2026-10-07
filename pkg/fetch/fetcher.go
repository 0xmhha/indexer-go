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

	// NoOutbox publishes the events of indexed blocks directly after the
	// commit instead of through the storage's outbox (eventbus.outbox:
	// false).
	NoOutbox bool
	// OutboxRetain is how many delivered events the outbox keeps
	// (eventbus.outbox_retention); 0 keeps all.
	OutboxRetain uint64
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

	// heads wakes the live loop when the node reports a new head
	// (SetHeadNotifier); nil means polling only.
	heads <-chan struct{}



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
	// outbox records the events of indexed blocks and relays them (nil if
	// the storage has none; outbox.go).
	outbox *outboxState
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

	f := &Fetcher{
		client:              client,
		storage:             storage,
		config:              config,
		logger:              logger,
		eventBus:            eventBus,
		metrics:             metrics,
		optimizer:           optimizer,
		txr:                 txr,
	}
	if !config.NoOutbox {
		f.initOutbox(config.OutboxRetain)
	}
	return f
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

// FetchBlock fetches and indexes one block.
func (f *Fetcher) FetchBlock(ctx context.Context, height uint64) error {
	return f.indexRange(ctx, height, height, 1)
}

// FetchRange fetches and indexes blocks start..end in height order, fetching
// up to the configured number of workers concurrently (see indexRange).
func (f *Fetcher) FetchRange(ctx context.Context, start, end uint64) error {
	return f.indexRange(ctx, start, end, f.workers())
}

// workers returns the configured number of fetch workers.
func (f *Fetcher) workers() int {
	if f.config.NumWorkers > 0 {
		return f.config.NumWorkers
	}
	return constants.DefaultNumWorkers
}

// jobResult holds the result of fetching a single block
type jobResult struct {
	height uint64
	block  *fetchedBlock
	err    error
}

// FetchRangeConcurrent is FetchRange; gap recovery and the live loop share
// one pipeline (refactoring plan R2-2, D6).
func (f *Fetcher) FetchRangeConcurrent(ctx context.Context, start, end uint64) error {
	return f.FetchRange(ctx, start, end)
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
			if err := f.waitForHead(ctx); err != nil {
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
			if ctx.Err() != nil {
				return ctx.Err() // stopped, not failed
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
// SetHeadNotifier makes the live loop, once caught up, poll the node as
// soon as ch delivers instead of waiting for the poll interval. Polling
// stays the fallback: a missed or late notification only costs one
// interval.
func (f *Fetcher) SetHeadNotifier(ch <-chan struct{}) { f.heads = ch }

// waitForHead waits for the poll interval or a head notification.
func (f *Fetcher) waitForHead(ctx context.Context) error {
	d := f.pollInterval()
	if f.heads == nil || d <= 0 {
		return sleepCtx(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-f.heads:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (f *Fetcher) pollInterval() time.Duration {
	if f.config.PollInterval > 0 {
		return f.config.PollInterval
	}
	return f.config.RetryDelay
}
