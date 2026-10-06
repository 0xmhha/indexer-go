// Package feedelegation computes StableNet fee delegation statistics: how
// many transactions had their gas paid by a fee payer and how much the fee
// payers paid.
package feedelegation

import (
	"context"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// FeeDelegationStats represents overall fee delegation statistics
type FeeDelegationStats struct {
	// TotalFeeDelegatedTxs is the total number of fee delegation transactions
	TotalFeeDelegatedTxs uint64
	// TotalFeesSaved is the total fees saved by users (paid by fee payers) in wei
	TotalFeesSaved *big.Int
	// AdoptionRate is the percentage of fee delegation transactions vs total transactions
	AdoptionRate float64
	// AvgFeeSaved is the average fee saved per fee delegation transaction in wei
	AvgFeeSaved *big.Int
}

// FeePayerStats represents statistics for a single fee payer
type FeePayerStats struct {
	// Address is the fee payer address
	Address common.Address
	// TxCount is the number of transactions sponsored by this fee payer
	TxCount uint64
	// TotalFeesPaid is the total fees paid by this fee payer in wei
	TotalFeesPaid *big.Int
	// Percentage is the percentage of total fee delegation transactions
	Percentage float64
}

// Backend is the storage the statistics read: stored blocks and receipts
// and the indexed height.
type Backend interface {
	port.ModelReader
	GetLatestHeight(ctx context.Context) (uint64, error)
}

// Stats computes fee delegation statistics over stored blocks.
type Stats struct {
	db Backend
}

// NewStats returns statistics over db.
func NewStats(db Backend) *Stats { return &Stats{db: db} }

// Open returns statistics over s, which must provide stored blocks (the
// Pebble storage does).
func Open(s any) (*Stats, error) {
	db, ok := s.(Backend)
	if !ok {
		return nil, fmt.Errorf("storage %T does not support fee delegation statistics", s)
	}
	return NewStats(db), nil
}

// sponsored is one fee delegation transaction.
type sponsored struct {
	payer common.Address
	fee   *big.Int // nil when the receipt is missing
}

// scan visits the fee delegation transactions of the stored blocks in
// [fromBlock, toBlock] (toBlock 0 or above the head: the head) and returns
// the number of transactions in the range.
func (s *Stats) scan(ctx context.Context, fromBlock, toBlock uint64, fn func(sponsored)) (uint64, error) {
	latestHeight, err := s.db.GetLatestHeight(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest height: %w", err)
	}
	if toBlock == 0 || toBlock > latestHeight {
		toBlock = latestHeight
	}
	var total uint64
	for height := fromBlock; height <= toBlock; height++ {
		block, err := s.db.GetModelBlock(ctx, height)
		if err != nil || block == nil {
			continue
		}
		total += uint64(len(block.Transactions))
		for _, tx := range block.Transactions {
			fd, ok := chains.FeeDelegationOf(tx)
			if !ok {
				continue
			}
			fn(sponsored{payer: fd.Payer, fee: s.fee(ctx, block, tx)})
		}
	}
	return total, nil
}

// fee returns gasUsed times the effective gas price of tx, or nil without a
// receipt.
func (s *Stats) fee(ctx context.Context, block *model.Block, tx *model.Transaction) *big.Int {
	receipt, err := s.db.GetModelReceipt(ctx, tx.Hash)
	if err != nil || receipt == nil {
		return nil
	}
	gasUsed := new(big.Int).SetUint64(receipt.GasUsed)
	if receipt.EffectiveGasPrice != nil {
		return gasUsed.Mul(gasUsed, receipt.EffectiveGasPrice)
	}
	// Fallback: calculate from block baseFee + tip
	if block.BaseFee != nil && tx.GasTipCap != nil {
		price := new(big.Int).Add(block.BaseFee, tx.GasTipCap)
		if tx.GasFeeCap != nil && price.Cmp(tx.GasFeeCap) > 0 {
			price = tx.GasFeeCap
		}
		return gasUsed.Mul(gasUsed, price)
	}
	if tx.GasPrice != nil {
		return gasUsed.Mul(gasUsed, tx.GasPrice)
	}
	return nil
}

// GetFeeDelegationStats returns overall fee delegation statistics
func (s *Stats) GetFeeDelegationStats(ctx context.Context, fromBlock, toBlock uint64) (*FeeDelegationStats, error) {
	stats := &FeeDelegationStats{TotalFeesSaved: big.NewInt(0), AvgFeeSaved: big.NewInt(0)}
	totalTxCount, err := s.scan(ctx, fromBlock, toBlock, func(tx sponsored) {
		stats.TotalFeeDelegatedTxs++
		if tx.fee != nil {
			stats.TotalFeesSaved.Add(stats.TotalFeesSaved, tx.fee)
		}
	})
	if err != nil {
		return nil, err
	}
	if totalTxCount > 0 {
		stats.AdoptionRate = float64(stats.TotalFeeDelegatedTxs) / float64(totalTxCount) * 100
	}
	if stats.TotalFeeDelegatedTxs > 0 {
		stats.AvgFeeSaved = new(big.Int).Div(stats.TotalFeesSaved, new(big.Int).SetUint64(stats.TotalFeeDelegatedTxs))
	}
	return stats, nil
}

// GetTopFeePayers returns the top fee payers by transaction count
func (s *Stats) GetTopFeePayers(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]FeePayerStats, uint64, error) {
	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	feePayerMap := make(map[common.Address]*FeePayerStats)
	var totalFeeDelegationTxs uint64
	_, err := s.scan(ctx, fromBlock, toBlock, func(tx sponsored) {
		totalFeeDelegationTxs++
		stats, ok := feePayerMap[tx.payer]
		if !ok {
			stats = &FeePayerStats{Address: tx.payer, TotalFeesPaid: big.NewInt(0)}
			feePayerMap[tx.payer] = stats
		}
		stats.TxCount++
		if tx.fee != nil {
			stats.TotalFeesPaid.Add(stats.TotalFeesPaid, tx.fee)
		}
	})
	if err != nil {
		return nil, 0, err
	}

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
	totalCount := uint64(len(feePayers))
	if len(feePayers) > limit {
		feePayers = feePayers[:limit]
	}
	return feePayers, totalCount, nil
}

// GetFeePayerStats returns statistics for a specific fee payer
func (s *Stats) GetFeePayerStats(ctx context.Context, feePayer common.Address, fromBlock, toBlock uint64) (*FeePayerStats, error) {
	stats := &FeePayerStats{Address: feePayer, TotalFeesPaid: big.NewInt(0)}
	var totalFeeDelegationTxs uint64
	_, err := s.scan(ctx, fromBlock, toBlock, func(tx sponsored) {
		totalFeeDelegationTxs++
		if tx.payer != feePayer {
			return
		}
		stats.TxCount++
		if tx.fee != nil {
			stats.TotalFeesPaid.Add(stats.TotalFeesPaid, tx.fee)
		}
	})
	if err != nil {
		return nil, err
	}
	if totalFeeDelegationTxs > 0 {
		stats.Percentage = float64(stats.TxCount) / float64(totalFeeDelegationTxs) * 100
	}
	return stats, nil
}
