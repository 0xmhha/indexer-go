package storage

import (
	"context"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ============================================================================
// Analytics Methods
// ============================================================================

// GetGasStatsByBlockRange returns gas usage statistics for a block range
func (s *PebbleStorage) GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*GasStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", fromBlock, toBlock)
	}

	stats := &GasStats{
		TotalGasUsed:     0,
		TotalGasLimit:    0,
		AverageGasUsed:   0,
		AverageGasPrice:  big.NewInt(0),
		BlockCount:       0,
		TransactionCount: 0,
	}

	totalGasPrice := big.NewInt(0)
	gasPriceCount := uint64(0)

	// Iterate through blocks
	for height := fromBlock; height <= toBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			continue // Skip missing blocks
		}

		stats.BlockCount++
		stats.TotalGasLimit += block.GasLimit()
		stats.TotalGasUsed += block.GasUsed()

		// Get receipts to calculate actual gas used and gas prices
		receipts, err := s.GetReceiptsByBlockNumber(ctx, height)
		if err != nil {
			continue
		}

		stats.TransactionCount += uint64(len(receipts))

		// Calculate gas prices
		txs := block.Transactions()
		for i, tx := range txs {
			if i < len(receipts) {
				gasPrice := tx.GasPrice()
				if gasPrice != nil && gasPrice.Sign() > 0 {
					totalGasPrice.Add(totalGasPrice, gasPrice)
					gasPriceCount++
				}
			}
		}
	}

	// Calculate averages
	if stats.BlockCount > 0 {
		stats.AverageGasUsed = stats.TotalGasUsed / stats.BlockCount
	}

	if gasPriceCount > 0 {
		stats.AverageGasPrice.Div(totalGasPrice, big.NewInt(int64(gasPriceCount)))
	}

	return stats, nil
}

// GetGasStatsByAddress returns gas usage statistics for a specific address
func (s *PebbleStorage) GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*AddressGasStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", fromBlock, toBlock)
	}

	stats := &AddressGasStats{
		Address:          addr,
		TotalGasUsed:     0,
		TransactionCount: 0,
		AverageGasPerTx:  0,
		TotalFeesPaid:    big.NewInt(0),
	}

	// Iterate through blocks
	for height := fromBlock; height <= toBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			continue
		}

		receipts, err := s.GetReceiptsByBlockNumber(ctx, height)
		if err != nil {
			continue
		}

		txs := block.Transactions()
		for i, tx := range txs {
			// Check if transaction is from this address
			sender, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
			if err != nil {
				continue
			}

			if sender == addr && i < len(receipts) {
				receipt := receipts[i]
				stats.TotalGasUsed += receipt.GasUsed
				stats.TransactionCount++

				// Calculate fees paid (gasUsed * gasPrice)
				gasPrice := tx.GasPrice()
				if gasPrice != nil {
					fee := new(big.Int).Mul(big.NewInt(int64(receipt.GasUsed)), gasPrice)
					stats.TotalFeesPaid.Add(stats.TotalFeesPaid, fee)
				}
			}
		}
	}

	// Calculate average
	if stats.TransactionCount > 0 {
		stats.AverageGasPerTx = stats.TotalGasUsed / stats.TransactionCount
	}

	return stats, nil
}

// GetTopAddressesByGasUsed returns the top addresses by total gas used
func (s *PebbleStorage) GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]AddressGasStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}

	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", fromBlock, toBlock)
	}

	// Map to track gas usage by address
	addressMap := make(map[common.Address]*AddressGasStats)

	// Iterate through blocks
	for height := fromBlock; height <= toBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			continue
		}

		receipts, err := s.GetReceiptsByBlockNumber(ctx, height)
		if err != nil {
			continue
		}

		txs := block.Transactions()
		for i, tx := range txs {
			sender, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
			if err != nil {
				continue
			}

			if i < len(receipts) {
				receipt := receipts[i]

				stats, exists := addressMap[sender]
				if !exists {
					stats = &AddressGasStats{
						Address:          sender,
						TotalGasUsed:     0,
						TransactionCount: 0,
						AverageGasPerTx:  0,
						TotalFeesPaid:    big.NewInt(0),
					}
					addressMap[sender] = stats
				}

				stats.TotalGasUsed += receipt.GasUsed
				stats.TransactionCount++

				// Calculate fees
				gasPrice := tx.GasPrice()
				if gasPrice != nil {
					fee := new(big.Int).Mul(big.NewInt(int64(receipt.GasUsed)), gasPrice)
					stats.TotalFeesPaid.Add(stats.TotalFeesPaid, fee)
				}
			}
		}
	}

	// Convert map to slice
	result := make([]AddressGasStats, 0, len(addressMap))
	for _, stats := range addressMap {
		// Calculate average
		if stats.TransactionCount > 0 {
			stats.AverageGasPerTx = stats.TotalGasUsed / stats.TransactionCount
		}
		result = append(result, *stats)
	}

	// Sort by total gas used (descending)
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].TotalGasUsed > result[i].TotalGasUsed {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	// Apply limit
	if len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}

// GetTopAddressesByTxCount returns the top addresses by transaction count
func (s *PebbleStorage) GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]AddressActivityStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}

	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", fromBlock, toBlock)
	}

	// Map to track activity by address
	addressMap := make(map[common.Address]*AddressActivityStats)

	// Iterate through blocks
	for height := fromBlock; height <= toBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			continue
		}

		receipts, err := s.GetReceiptsByBlockNumber(ctx, height)
		if err != nil {
			continue
		}

		txs := block.Transactions()
		for i, tx := range txs {
			sender, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
			if err != nil {
				continue
			}

			if i < len(receipts) {
				receipt := receipts[i]

				stats, exists := addressMap[sender]
				if !exists {
					stats = &AddressActivityStats{
						Address:            sender,
						TransactionCount:   0,
						TotalGasUsed:       0,
						LastActivityBlock:  0,
						FirstActivityBlock: height,
					}
					addressMap[sender] = stats
				}

				stats.TransactionCount++
				stats.TotalGasUsed += receipt.GasUsed

				if height > stats.LastActivityBlock {
					stats.LastActivityBlock = height
				}
				if height < stats.FirstActivityBlock {
					stats.FirstActivityBlock = height
				}
			}
		}
	}

	// Convert map to slice
	result := make([]AddressActivityStats, 0, len(addressMap))
	for _, stats := range addressMap {
		result = append(result, *stats)
	}

	// Sort by transaction count (descending)
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].TransactionCount > result[i].TransactionCount {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	// Apply limit
	if len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}

// GetNetworkMetrics returns network activity metrics for a time range
func (s *PebbleStorage) GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*NetworkMetrics, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if fromTime > toTime {
		return nil, fmt.Errorf("fromTime (%d) cannot be greater than toTime (%d)", fromTime, toTime)
	}

	metrics := &NetworkMetrics{
		TPS:               0,
		BlockTime:         0,
		TotalBlocks:       0,
		TotalTransactions: 0,
		AverageBlockSize:  0,
		TimePeriod:        toTime - fromTime,
	}

	totalGasUsed := uint64(0)
	var firstBlockTime, lastBlockTime uint64

	// Get blocks by time range
	blocks, err := s.GetBlocksByTimeRange(ctx, fromTime, toTime, 10000, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get blocks: %w", err)
	}

	if len(blocks) == 0 {
		return metrics, nil
	}

	metrics.TotalBlocks = uint64(len(blocks))
	firstBlockTime = blocks[0].Time()
	lastBlockTime = blocks[len(blocks)-1].Time()

	// Calculate metrics
	for _, block := range blocks {
		metrics.TotalTransactions += uint64(block.Transactions().Len())
		totalGasUsed += block.GasUsed()
	}

	// Calculate averages
	if metrics.TotalBlocks > 0 {
		metrics.AverageBlockSize = totalGasUsed / metrics.TotalBlocks
	}

	// Calculate block time (in seconds)
	if metrics.TotalBlocks > 1 {
		timeDiff := lastBlockTime - firstBlockTime
		if timeDiff > 0 {
			metrics.BlockTime = float64(timeDiff) / float64(metrics.TotalBlocks-1)
			// Calculate TPS
			if timeDiff > 0 {
				metrics.TPS = float64(metrics.TotalTransactions) / float64(timeDiff)
			}
		}
	}

	return metrics, nil
}
