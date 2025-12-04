package fetch

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/storage"
)

// LargeBlockProcessor handles efficient processing of large blocks (>50M gas)
type LargeBlockProcessor struct {
	logger  *zap.Logger
	storage Storage

	// Configuration
	largeBlockThreshold uint64 // Gas limit threshold for "large" blocks
	receiptBatchSize    int    // Number of receipts to process in each batch
	maxReceiptWorkers   int    // Maximum number of workers for parallel receipt processing
}

// NewLargeBlockProcessor creates a new large block processor
func NewLargeBlockProcessor(storage Storage, logger *zap.Logger) *LargeBlockProcessor {
	return &LargeBlockProcessor{
		logger:              logger,
		storage:             storage,
		largeBlockThreshold: 50000000, // 50M gas
		receiptBatchSize:    100,      // Process 100 receipts per batch
		maxReceiptWorkers:   10,       // Use up to 10 workers for receipt processing
	}
}

// IsLargeBlock returns true if the block's gas used exceeds the large block threshold
func (p *LargeBlockProcessor) IsLargeBlock(block *types.Block) bool {
	return block.GasUsed() >= p.largeBlockThreshold
}

// ProcessReceiptsParallel processes receipts in parallel batches
// This is optimized for large blocks with many receipts
func (p *LargeBlockProcessor) ProcessReceiptsParallel(ctx context.Context, block *types.Block, receipts types.Receipts) error {
	if len(receipts) == 0 {
		return nil
	}

	blockNumber := block.NumberU64()
	blockTime := block.Time()

	p.logger.Info("Processing large block receipts in parallel",
		zap.Uint64("block", blockNumber),
		zap.Uint64("gas_used", block.GasUsed()),
		zap.Int("receipt_count", len(receipts)),
		zap.Int("batch_size", p.receiptBatchSize),
		zap.Int("max_workers", p.maxReceiptWorkers),
	)

	// Calculate number of batches
	numBatches := (len(receipts) + p.receiptBatchSize - 1) / p.receiptBatchSize
	numWorkers := min(numBatches, p.maxReceiptWorkers)

	// Create channels for work distribution
	type receiptBatch struct {
		start int
		end   int
	}
	jobs := make(chan receiptBatch, numBatches)
	errors := make(chan error, numBatches)

	// Start worker pool
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for batch := range jobs {
				// Check context cancellation
				select {
				case <-ctx.Done():
					errors <- ctx.Err()
					return
				default:
				}

				// Process this batch of receipts
				if err := p.processBatch(ctx, block, receipts[batch.start:batch.end], blockTime); err != nil {
					errors <- fmt.Errorf("worker %d failed to process receipts [%d:%d]: %w",
						workerID, batch.start, batch.end, err)
					return
				}

				p.logger.Debug("Worker completed receipt batch",
					zap.Int("worker_id", workerID),
					zap.Uint64("block", blockNumber),
					zap.Int("batch_start", batch.start),
					zap.Int("batch_end", batch.end),
				)
			}
		}(i)
	}

	// Send batches to workers
	for i := 0; i < len(receipts); i += p.receiptBatchSize {
		end := i + p.receiptBatchSize
		if end > len(receipts) {
			end = len(receipts)
		}
		jobs <- receiptBatch{start: i, end: end}
	}
	close(jobs)

	// Wait for all workers to complete
	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		return err
	}

	p.logger.Info("Completed parallel receipt processing",
		zap.Uint64("block", blockNumber),
		zap.Int("total_receipts", len(receipts)),
		zap.Int("workers_used", numWorkers),
	)

	return nil
}

// processBatch processes a batch of receipts
func (p *LargeBlockProcessor) processBatch(ctx context.Context, block *types.Block, receipts types.Receipts, blockTime uint64) error {
	blockNumber := block.NumberU64()
	transactions := block.Transactions()

	for _, receipt := range receipts {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Store receipt
		if err := p.storage.SetReceipt(ctx, receipt); err != nil {
			return fmt.Errorf("failed to store receipt for tx %s: %w", receipt.TxHash.Hex(), err)
		}

		// Index logs from this receipt
		if logWriter, ok := p.storage.(storage.LogWriter); ok && len(receipt.Logs) > 0 {
			if err := logWriter.IndexLogs(ctx, receipt.Logs); err != nil {
				p.logger.Warn("failed to index logs",
					zap.String("tx", receipt.TxHash.Hex()),
					zap.Int("logs", len(receipt.Logs)),
					zap.Error(err),
				)
				// Continue processing - log indexing failure shouldn't block block indexing
			}
		}

		// Process address indexing for this transaction and receipt
		if addressWriter, ok := p.storage.(storage.AddressIndexWriter); ok {
			// Find the transaction for this receipt
			var tx *types.Transaction
			for _, t := range transactions {
				if t.Hash() == receipt.TxHash {
					tx = t
					break
				}
			}

			if tx != nil {
				if err := p.processAddressIndexing(ctx, tx, receipt, blockNumber, blockTime, addressWriter); err != nil {
					p.logger.Warn("failed to process address indexing",
						zap.String("tx", tx.Hash().Hex()),
						zap.Uint64("block", blockNumber),
						zap.Error(err),
					)
					// Continue processing
				}
			}
		}
	}

	return nil
}

// processAddressIndexing processes address indexing for a single transaction
func (p *LargeBlockProcessor) processAddressIndexing(
	ctx context.Context,
	tx *types.Transaction,
	receipt *types.Receipt,
	blockNumber uint64,
	blockTime uint64,
	addressWriter storage.AddressIndexWriter,
) error {
	// Fee Delegation transaction type constant
	const FeeDelegateDynamicFeeTxType = 22

	// 0. Index transaction addresses (from, to, feePayer) for transactionsByAddress query
	if storageWriter, ok := p.storage.(storage.Writer); ok {
		txHash := tx.Hash()

		// Index 'from' address
		from := getTransactionSender(tx)
		if from != (common.Address{}) {
			if err := storageWriter.AddTransactionToAddressIndex(ctx, from, txHash); err != nil {
				p.logger.Warn("Failed to index transaction for from address",
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
					p.logger.Warn("Failed to index transaction for to address",
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
			if feePayer := tx.FeePayer(); feePayer != nil {
				// Avoid duplicate indexing if feePayer is same as from or to
				if *feePayer != from && (tx.To() == nil || *feePayer != *tx.To()) {
					if err := storageWriter.AddTransactionToAddressIndex(ctx, *feePayer, txHash); err != nil {
						p.logger.Warn("Failed to index transaction for feePayer address",
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
	if tx.To() == nil && receipt.ContractAddress != (common.Address{}) {
		creation := &storage.ContractCreation{
			ContractAddress: receipt.ContractAddress,
			Creator:         getTransactionSender(tx),
			TransactionHash: tx.Hash(),
			BlockNumber:     blockNumber,
			Timestamp:       blockTime,
			BytecodeSize:    len(receipt.ContractAddress.Bytes()),
		}

		if err := addressWriter.SaveContractCreation(ctx, creation); err != nil {
			return fmt.Errorf("failed to save contract creation: %w", err)
		}
	}

	// 2. Parse ERC20/ERC721 Transfer Events from Logs
	for _, log := range receipt.Logs {
		if log == nil || len(log.Topics) == 0 {
			continue
		}

		// Check if this is a Transfer event
		if log.Topics[0].Hex() != storage.ERC20TransferTopic {
			continue
		}

		if len(log.Topics) == 3 {
			// ERC20 Transfer Event
			if len(log.Data) < 32 {
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
				return fmt.Errorf("failed to save ERC20 transfer: %w", err)
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
				return fmt.Errorf("failed to save ERC721 transfer: %w", err)
			}
		}
	}

	return nil
}

// EstimateMemoryUsage estimates the memory usage for processing a block
func (p *LargeBlockProcessor) EstimateMemoryUsage(block *types.Block, receipts types.Receipts) uint64 {
	// Rough estimation:
	// - Block header: ~500 bytes
	// - Each transaction: ~200 bytes (average)
	// - Each receipt: ~300 bytes (average)
	// - Each log: ~150 bytes (average)

	var totalLogs int
	for _, receipt := range receipts {
		totalLogs += len(receipt.Logs)
	}

	headerSize := uint64(500)
	txSize := uint64(len(block.Transactions())) * 200
	receiptSize := uint64(len(receipts)) * 300
	logSize := uint64(totalLogs) * 150

	total := headerSize + txSize + receiptSize + logSize

	return total
}

// ShouldProcessInBatches returns true if the block should be processed in batches
// to avoid excessive memory usage
func (p *LargeBlockProcessor) ShouldProcessInBatches(block *types.Block, receipts types.Receipts) bool {
	// If it's a large block, use batching
	if p.IsLargeBlock(block) {
		return true
	}

	// If there are many receipts, use batching
	if len(receipts) > 1000 {
		return true
	}

	// Estimate memory usage
	estimatedMemory := p.EstimateMemoryUsage(block, receipts)

	// If estimated memory exceeds 100MB, use batching
	if estimatedMemory > 100*constants.BytesPerMB {
		p.logger.Info("Using batched processing due to high memory estimate",
			zap.Uint64("block", block.NumberU64()),
			zap.Uint64("estimated_memory_bytes", estimatedMemory),
			zap.Float64("estimated_memory_mb", float64(estimatedMemory)/float64(constants.BytesPerMB)),
		)
		return true
	}

	return false
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
