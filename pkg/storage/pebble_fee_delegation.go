package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
)

// Ensure PebbleStorage implements FeeDelegationReader and FeeDelegationWriter
var _ FeeDelegationReader = (*PebbleStorage)(nil)
var _ FeeDelegationWriter = (*PebbleStorage)(nil)

// ============================================================================
// FeeDelegationReader interface implementation
// ============================================================================

// GetFeeDelegationStats returns overall fee delegation statistics
func (s *PebbleStorage) GetFeeDelegationStats(ctx context.Context, fromBlock, toBlock uint64) (*FeeDelegationStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Get block range
	latestHeight, err := s.GetLatestHeight(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	if toBlock == 0 || toBlock > latestHeight {
		toBlock = latestHeight
	}

	stats := &FeeDelegationStats{
		TotalFeeDelegatedTxs: 0,
		TotalFeesSaved:       big.NewInt(0),
		AdoptionRate:         0,
		AvgFeeSaved:          big.NewInt(0),
	}

	var totalTxCount uint64

	// Iterate through blocks in the range
	for height := fromBlock; height <= toBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			continue
		}
		if block == nil {
			continue
		}

		txs := block.Transactions()
		totalTxCount += uint64(len(txs))

		for _, tx := range txs {
			// Check if this transaction has fee delegation metadata
			meta, err := s.GetFeeDelegationTxMeta(ctx, tx.Hash())
			if err != nil || meta == nil {
				continue // Not a fee delegation transaction
			}

			stats.TotalFeeDelegatedTxs++

			// Get receipt to calculate fee
			receipt, err := s.GetReceipt(ctx, tx.Hash())
			if err != nil || receipt == nil {
				continue
			}

			// Calculate fee: gasUsed * effectiveGasPrice
			var fee *big.Int
			if receipt.EffectiveGasPrice != nil {
				fee = new(big.Int).Mul(
					big.NewInt(int64(receipt.GasUsed)),
					receipt.EffectiveGasPrice,
				)
			} else {
				// Fallback: calculate from block baseFee + tip
				baseFee := block.BaseFee()
				if baseFee != nil && tx.GasTipCap() != nil {
					effectiveGasPrice := new(big.Int).Add(baseFee, tx.GasTipCap())
					if tx.GasFeeCap() != nil && effectiveGasPrice.Cmp(tx.GasFeeCap()) > 0 {
						effectiveGasPrice = tx.GasFeeCap()
					}
					fee = new(big.Int).Mul(big.NewInt(int64(receipt.GasUsed)), effectiveGasPrice)
				} else if tx.GasPrice() != nil {
					fee = new(big.Int).Mul(big.NewInt(int64(receipt.GasUsed)), tx.GasPrice())
				}
			}

			if fee != nil {
				stats.TotalFeesSaved.Add(stats.TotalFeesSaved, fee)
			}
		}
	}

	// Calculate adoption rate
	if totalTxCount > 0 {
		stats.AdoptionRate = float64(stats.TotalFeeDelegatedTxs) / float64(totalTxCount) * 100
	}

	// Calculate average fee saved
	if stats.TotalFeeDelegatedTxs > 0 {
		stats.AvgFeeSaved = new(big.Int).Div(stats.TotalFeesSaved, big.NewInt(int64(stats.TotalFeeDelegatedTxs)))
	}

	return stats, nil
}

// GetTopFeePayers returns the top fee payers by transaction count
func (s *PebbleStorage) GetTopFeePayers(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]FeePayerStats, uint64, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, 0, err
	}

	// Get block range
	latestHeight, err := s.GetLatestHeight(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get latest height: %w", err)
	}

	if toBlock == 0 || toBlock > latestHeight {
		toBlock = latestHeight
	}

	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}

	// Map to aggregate fee payer stats
	feePayerMap := make(map[common.Address]*FeePayerStats)
	var totalFeeDelegationTxs uint64

	// Iterate through blocks in the range
	for height := fromBlock; height <= toBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			continue
		}
		if block == nil {
			continue
		}

		for _, tx := range block.Transactions() {
			// Check if this transaction has fee delegation metadata
			meta, err := s.GetFeeDelegationTxMeta(ctx, tx.Hash())
			if err != nil || meta == nil {
				continue // Not a fee delegation transaction
			}

			totalFeeDelegationTxs++
			feePayer := meta.FeePayer

			// Initialize or update fee payer stats
			if _, exists := feePayerMap[feePayer]; !exists {
				feePayerMap[feePayer] = &FeePayerStats{
					Address:       feePayer,
					TxCount:       0,
					TotalFeesPaid: big.NewInt(0),
					Percentage:    0,
				}
			}

			feePayerMap[feePayer].TxCount++

			// Get receipt to calculate fee
			receipt, err := s.GetReceipt(ctx, tx.Hash())
			if err != nil || receipt == nil {
				continue
			}

			// Calculate fee
			var fee *big.Int
			if receipt.EffectiveGasPrice != nil {
				fee = new(big.Int).Mul(
					big.NewInt(int64(receipt.GasUsed)),
					receipt.EffectiveGasPrice,
				)
			} else {
				baseFee := block.BaseFee()
				if baseFee != nil && tx.GasTipCap() != nil {
					effectiveGasPrice := new(big.Int).Add(baseFee, tx.GasTipCap())
					if tx.GasFeeCap() != nil && effectiveGasPrice.Cmp(tx.GasFeeCap()) > 0 {
						effectiveGasPrice = tx.GasFeeCap()
					}
					fee = new(big.Int).Mul(big.NewInt(int64(receipt.GasUsed)), effectiveGasPrice)
				} else if tx.GasPrice() != nil {
					fee = new(big.Int).Mul(big.NewInt(int64(receipt.GasUsed)), tx.GasPrice())
				}
			}

			if fee != nil {
				feePayerMap[feePayer].TotalFeesPaid.Add(feePayerMap[feePayer].TotalFeesPaid, fee)
			}
		}
	}

	// Convert map to slice and calculate percentages
	feePayers := make([]FeePayerStats, 0, len(feePayerMap))
	for _, stats := range feePayerMap {
		if totalFeeDelegationTxs > 0 {
			stats.Percentage = float64(stats.TxCount) / float64(totalFeeDelegationTxs) * 100
		}
		feePayers = append(feePayers, *stats)
	}

	// Sort by transaction count (descending)
	sort.Slice(feePayers, func(i, j int) bool {
		return feePayers[i].TxCount > feePayers[j].TxCount
	})

	// Apply limit
	totalCount := uint64(len(feePayers))
	if len(feePayers) > limit {
		feePayers = feePayers[:limit]
	}

	return feePayers, totalCount, nil
}

// GetFeePayerStats returns statistics for a specific fee payer
func (s *PebbleStorage) GetFeePayerStats(ctx context.Context, feePayer common.Address, fromBlock, toBlock uint64) (*FeePayerStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Get block range
	latestHeight, err := s.GetLatestHeight(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	if toBlock == 0 || toBlock > latestHeight {
		toBlock = latestHeight
	}

	stats := &FeePayerStats{
		Address:       feePayer,
		TxCount:       0,
		TotalFeesPaid: big.NewInt(0),
		Percentage:    0,
	}

	var totalFeeDelegationTxs uint64

	// Iterate through blocks in the range
	for height := fromBlock; height <= toBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			continue
		}
		if block == nil {
			continue
		}

		for _, tx := range block.Transactions() {
			// Check if this transaction has fee delegation metadata
			meta, err := s.GetFeeDelegationTxMeta(ctx, tx.Hash())
			if err != nil || meta == nil {
				continue // Not a fee delegation transaction
			}

			totalFeeDelegationTxs++

			// Check if this transaction's fee payer matches
			if meta.FeePayer != feePayer {
				continue
			}

			stats.TxCount++

			// Get receipt to calculate fee
			receipt, err := s.GetReceipt(ctx, tx.Hash())
			if err != nil || receipt == nil {
				continue
			}

			// Calculate fee
			var fee *big.Int
			if receipt.EffectiveGasPrice != nil {
				fee = new(big.Int).Mul(
					big.NewInt(int64(receipt.GasUsed)),
					receipt.EffectiveGasPrice,
				)
			} else {
				baseFee := block.BaseFee()
				if baseFee != nil && tx.GasTipCap() != nil {
					effectiveGasPrice := new(big.Int).Add(baseFee, tx.GasTipCap())
					if tx.GasFeeCap() != nil && effectiveGasPrice.Cmp(tx.GasFeeCap()) > 0 {
						effectiveGasPrice = tx.GasFeeCap()
					}
					fee = new(big.Int).Mul(big.NewInt(int64(receipt.GasUsed)), effectiveGasPrice)
				} else if tx.GasPrice() != nil {
					fee = new(big.Int).Mul(big.NewInt(int64(receipt.GasUsed)), tx.GasPrice())
				}
			}

			if fee != nil {
				stats.TotalFeesPaid.Add(stats.TotalFeesPaid, fee)
			}
		}
	}

	// Calculate percentage
	if totalFeeDelegationTxs > 0 {
		stats.Percentage = float64(stats.TxCount) / float64(totalFeeDelegationTxs) * 100
	}

	return stats, nil
}

// ============================================================================
// FeeDelegationWriter interface implementation
// ============================================================================

// SetFeeDelegationTxMeta stores fee delegation metadata for a transaction
func (s *PebbleStorage) SetFeeDelegationTxMeta(ctx context.Context, meta *FeeDelegationTxMeta) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	if meta == nil {
		return fmt.Errorf("meta cannot be nil")
	}

	// Serialize metadata
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal fee delegation meta: %w", err)
	}

	// Store metadata by tx hash
	key := FeeDelegationMetaKey(meta.TxHash)
	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store fee delegation meta: %w", err)
	}

	// Create index by fee payer
	indexKey := FeeDelegationPayerIndexKey(meta.FeePayer, meta.BlockNumber, meta.TxHash)
	if err := s.db.Set(indexKey, meta.TxHash.Bytes(), pebble.Sync); err != nil {
		return fmt.Errorf("failed to store fee payer index: %w", err)
	}

	return nil
}

// GetFeeDelegationTxMeta returns fee delegation metadata for a transaction
// Returns nil if the transaction is not a fee delegation transaction
func (s *PebbleStorage) GetFeeDelegationTxMeta(ctx context.Context, txHash common.Hash) (*FeeDelegationTxMeta, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := FeeDelegationMetaKey(txHash)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil // Not a fee delegation tx
		}
		return nil, fmt.Errorf("failed to get fee delegation meta: %w", err)
	}
	defer closer.Close()

	var meta FeeDelegationTxMeta
	if err := json.Unmarshal(value, &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fee delegation meta: %w", err)
	}

	return &meta, nil
}

// GetFeeDelegationTxsByFeePayer returns transaction hashes of fee delegation txs by fee payer
func (s *PebbleStorage) GetFeeDelegationTxsByFeePayer(ctx context.Context, feePayer common.Address, limit, offset int) ([]common.Hash, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 100
	}

	prefix := FeeDelegationPayerPrefix(feePayer)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var hashes []common.Hash
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Handle offset
		if skipped < offset {
			skipped++
			continue
		}

		// Get tx hash from value
		if len(iter.Value()) == 32 {
			hash := common.BytesToHash(iter.Value())
			hashes = append(hashes, hash)
		}

		// Check limit
		if len(hashes) >= limit {
			break
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return hashes, nil
}
