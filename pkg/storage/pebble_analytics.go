package storage

import (
	"context"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage/history"
	"github.com/ethereum/go-ethereum/common"
)

// ============================================================================
// Analytics Methods
// ============================================================================

// The statistics are computed from the ports by pkg/storage/history, which
// every store shares.

// GetGasStatsByBlockRange returns gas usage statistics for a block range
func (s *PebbleStorage) GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*port.GasStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}
	return history.GasStatsByBlockRange(ctx, s, fromBlock, toBlock)
}

// GetGasStatsByAddress returns gas usage statistics for a specific address
func (s *PebbleStorage) GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*port.AddressGasStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}
	return history.GasStatsByAddress(ctx, s, addr, fromBlock, toBlock)
}

// GetTopAddressesByGasUsed returns the top addresses by total gas used
func (s *PebbleStorage) GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.AddressGasStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}
	return history.TopAddressesByGasUsed(ctx, s, limit, fromBlock, toBlock)
}

// GetTopAddressesByTxCount returns the top addresses by transaction count
func (s *PebbleStorage) GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.AddressActivityStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}
	return history.TopAddressesByTxCount(ctx, s, limit, fromBlock, toBlock)
}

// GetNetworkMetrics returns network activity metrics for a time range
func (s *PebbleStorage) GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*port.NetworkMetrics, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}
	if fromTime > toTime {
		return nil, fmt.Errorf("fromTime (%d) cannot be greater than toTime (%d)", fromTime, toTime)
	}
	// Read every block in the range from the timestamp index; the totals
	// are exact, so there is no cap on the number of blocks.
	return history.NetworkMetrics(ctx, fromTime, toTime, func(yield func(*model.Block) error) error {
		iter, err := s.kv(ctx).NewIter(blockTimeRange(fromTime, toTime))
		if err != nil {
			return fmt.Errorf("failed to create iterator: %w", err)
		}
		defer func() { _ = iter.Close() }()

		for iter.First(); iter.Valid(); iter.Next() {
			height, err := DecodeUint64(iter.Value())
			if err != nil {
				return fmt.Errorf("failed to decode height: %w", err)
			}
			block, err := s.GetBlock(ctx, height)
			if err != nil {
				if err == port.ErrNotFound {
					continue // Skip missing blocks
				}
				return fmt.Errorf("failed to get block %d: %w", height, err)
			}
			if err := yield(block); err != nil {
				return err
			}
		}
		if err := iter.Error(); err != nil {
			return fmt.Errorf("iterator error: %w", err)
		}
		return nil
	})
}
