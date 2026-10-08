package storage

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage/history"
)

// Ensure PebbleStorage implements HistoricalReader and HistoricalWriter
var _ port.HistoricalReader = (*PebbleStorage)(nil)
var _ port.HistoricalWriter = (*PebbleStorage)(nil)

// ============================================================================
// Historical Data Methods
// ============================================================================

// GetBlocksByTimeRange returns one page of the blocks within a time range,
// in the order of the timestamp index (time, then height). The cursor is the
// last block's timestamp index key; index entries whose block is missing are
// skipped.
func (s *PebbleStorage) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, page port.Page) ([]*model.Block, string, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, "", err
	}

	if fromTime > toTime {
		return nil, "", fmt.Errorf("fromTime (%d) cannot be greater than toTime (%d)", fromTime, toTime)
	}

	bounds := blockTimeRange(fromTime, toTime)
	return scanLoadedPage(ctx, s, bounds.LowerBound, bounds.UpperBound, false, page, pageLimit(page, constants.DefaultPaginationLimit),
		func(ctx context.Context, _, value []byte) (*model.Block, bool, error) {
			height, err := DecodeUint64(value)
			if err != nil {
				return nil, false, fmt.Errorf("failed to decode height: %w", err)
			}
			block, err := s.GetBlock(ctx, height)
			if errors.Is(err, port.ErrNotFound) {
				return nil, false, nil // Skip missing blocks
			}
			if err != nil {
				return nil, false, fmt.Errorf("failed to get block %d: %w", height, err)
			}
			return block, true, nil
		})
}

// blockTimeRange returns the iterator bounds of the timestamp index entries
// from fromTime to toTime inclusive. toTime+1 would wrap at the largest
// timestamp, so that range ends at the end of the index instead.
func blockTimeRange(fromTime, toTime uint64) *pebble.IterOptions {
	upper := prefixUpperBound(BlockTimestampKeyPrefix())
	if toTime < math.MaxUint64 {
		upper = BlockTimestampKey(toTime+1, 0)
	}
	return &pebble.IterOptions{LowerBound: BlockTimestampKey(fromTime, 0), UpperBound: upper}
}

// decodeBlockTimestampEntry returns the timestamp and height of a timestamp
// index entry.
func decodeBlockTimestampEntry(key, value []byte) (uint64, uint64, error) {
	prefix := len(BlockTimestampKeyPrefix())
	if len(key) < prefix+20 {
		return 0, 0, fmt.Errorf("invalid timestamp index key %q", key)
	}
	ts, err := strconv.ParseUint(string(key[prefix:prefix+20]), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to decode timestamp index key %q: %w", key, err)
	}
	height, err := DecodeUint64(value)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to decode height: %w", err)
	}
	return ts, height, nil
}

// GetBlockByTimestamp returns the block closest to the given timestamp
func (s *PebbleStorage) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*model.Block, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// The bounds keep the seeks below inside the timestamp index.
	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: BlockTimestampKeyPrefix(),
		UpperBound: prefixUpperBound(BlockTimestampKeyPrefix()),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// The first block at or after the timestamp, otherwise the last block.
	var closestHeight uint64
	var found bool

	positioned := iter.SeekGE(BlockTimestampKey(timestamp, 0))
	if !positioned {
		positioned = iter.Last()
	}
	if positioned {
		_, height, err := decodeBlockTimestampEntry(iter.Key(), iter.Value())
		if err != nil {
			return nil, err
		}
		closestHeight, found = height, true
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	if !found {
		return nil, port.ErrNotFound
	}

	return s.GetBlock(ctx, closestHeight)
}

// GetTransactionsByAddressFiltered returns one page of an address's
// transactions that match filter, in address index order. The filter is
// applied while scanning the address index, so the cursor is the address
// index key of the last match and a cursor page reads only the entries after
// it; Offset still counts matches from the start.
func (s *PebbleStorage) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *port.TransactionFilter, page port.Page) ([]*port.TransactionWithReceipt, string, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, "", err
	}

	if filter == nil {
		filter = port.DefaultTransactionFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, "", fmt.Errorf("invalid filter: %w", err)
	}

	prefix := AddressTransactionKeyPrefix(addr)
	return scanLoadedPage(ctx, s, prefix, prefixUpperBound(prefix), false, page, pageLimit(page, constants.DefaultPaginationLimit),
		func(ctx context.Context, _, value []byte) (*port.TransactionWithReceipt, bool, error) {
			return history.AddressTransaction(ctx, s, addr, filter, common.BytesToHash(value))
		})
}

// GetAddressBalance returns the balance of an address at a specific block
// getAddressBalance reads the stored balance without the lazy genesis lookup
// done by GetAddressBalance. Writers use it so that updating a balance never
// triggers an RPC call or a nested write.
func (s *PebbleStorage) getAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// If blockNumber is 0, get latest balance
	if blockNumber == 0 {
		value, closer, err := s.kv(ctx).Get(AddressBalanceLatestKey(addr))
		if err != nil {
			if err == pebble.ErrNotFound {
				return big.NewInt(0), nil // No balance recorded
			}
			return nil, fmt.Errorf("failed to get latest balance: %w", err)
		}
		defer closer.Close()

		return DecodeBigInt(value), nil
	}

	// Get balance at specific block by iterating history
	prefix := AddressBalanceKeyPrefix(addr)
	upperBound := make([]byte, len(prefix), len(prefix)+1)
	copy(upperBound, prefix)
	upperBound = append(upperBound, 0xff)

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upperBound,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var balance *big.Int = big.NewInt(0)

	// Iterate through all snapshots up to target block
	for iter.First(); iter.Valid(); iter.Next() {
		snapshot, err := DecodeBalanceSnapshot(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode snapshot: %w", err)
		}

		if snapshot.BlockNumber > blockNumber {
			break // Past target block
		}

		balance = snapshot.Balance
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return balance, nil
}

// GetBalanceHistory returns one page of an address's balance snapshots in
// the block range, in the order they were recorded. The cursor is the last
// snapshot's history key.
func (s *PebbleStorage) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, page port.Page) ([]port.BalanceSnapshot, string, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, "", err
	}

	if fromBlock > toBlock {
		return nil, "", fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", fromBlock, toBlock)
	}

	prefix := AddressBalanceKeyPrefix(addr)
	return scanLoadedPage(ctx, s, prefix, prefixUpperBound(prefix), false, page, pageLimit(page, constants.DefaultPaginationLimit),
		func(_ context.Context, _, value []byte) (port.BalanceSnapshot, bool, error) {
			snapshot, err := DecodeBalanceSnapshot(value)
			if err != nil {
				return port.BalanceSnapshot{}, false, fmt.Errorf("failed to decode snapshot: %w", err)
			}
			// Filter by block range
			if snapshot.BlockNumber < fromBlock || snapshot.BlockNumber > toBlock {
				return port.BalanceSnapshot{}, false, nil
			}
			return *snapshot, true, nil
		})
}

// GetBlockCount returns the total number of indexed blocks
func (s *PebbleStorage) GetBlockCount(ctx context.Context) (uint64, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}

	// Get latest block height
	height, err := s.GetLatestHeight(ctx)
	if err != nil {
		if err == port.ErrNotFound {
			return 0, nil // No blocks indexed yet
		}
		return 0, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Block count is height + 1 (blocks are indexed from 0)
	return height + 1, nil
}

// GetTransactionCount returns the total number of indexed transactions
// Uses cached atomic counter for high performance
func (s *PebbleStorage) GetTransactionCount(ctx context.Context) (uint64, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}

	// Use atomic counter if ready (much faster than DB read)
	if s.txCountReady.Load() {
		return s.txCount.Load(), nil
	}

	// Fallback to DB read if counter not initialized
	value, closer, err := s.kv(ctx).Get(TransactionCountKey())
	if err != nil {
		if err == pebble.ErrNotFound {
			return 0, nil // No transactions indexed yet
		}
		return 0, fmt.Errorf("failed to get transaction count: %w", err)
	}
	defer closer.Close()

	count, err := DecodeUint64(value)
	if err != nil {
		return 0, fmt.Errorf("failed to decode transaction count: %w", err)
	}

	return count, nil
}

// InitializeTransactionCount scans all blocks and initializes the transaction count
// This is useful for migrating existing databases that don't have the transaction count set
func (s *PebbleStorage) InitializeTransactionCount(ctx context.Context) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get latest height
	latestHeight, err := s.GetLatestHeight(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest height: %w", err)
	}

	// Count all transactions by iterating through blocks
	totalTxCount := uint64(0)
	for height := uint64(0); height <= latestHeight; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			if err == port.ErrNotFound {
				continue // Skip missing blocks
			}
			return fmt.Errorf("failed to get block %d: %w", height, err)
		}

		totalTxCount += uint64(len(block.Transactions))
	}

	// Set the transaction count
	if err := s.kv(ctx).Set(TransactionCountKey(), EncodeUint64(totalTxCount), pebble.Sync); err != nil {
		return fmt.Errorf("failed to set transaction count: %w", err)
	}

	// Update atomic counter
	s.txCount.Store(totalTxCount)
	s.txCountReady.Store(true)

	return nil
}

// GetTopMiners returns the top miners by block count
func (s *PebbleStorage) GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.MinerStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	return history.TopMiners(ctx, s, limit, fromBlock, toBlock)
}

// ============================================================================
// Historical Data Write Methods
// ============================================================================

// SetBlockTimestamp indexes a block by timestamp
func (s *PebbleStorage) SetBlockTimestamp(ctx context.Context, timestamp uint64, height uint64) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	value := EncodeUint64(height)
	return s.kv(ctx).Set(BlockTimestampKey(timestamp, height), value, pebble.Sync)
}

// UpdateBalance updates the balance for an address at a specific block
func (s *PebbleStorage) UpdateBalance(ctx context.Context, addr common.Address, blockNumber uint64, delta *big.Int, txHash common.Hash) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get current balance
	currentBalance, err := s.getAddressBalance(ctx, addr, 0) // Get latest
	if err != nil {
		return fmt.Errorf("failed to get current balance: %w", err)
	}

	// Calculate new balance
	newBalance := new(big.Int).Add(currentBalance, delta)
	if newBalance.Sign() < 0 {
		return fmt.Errorf("%w: %s at block %d (%s %+d)", port.ErrNegativeBalance, addr.Hex(), blockNumber, currentBalance, delta)
	}

	// Create snapshot
	snapshot := &port.BalanceSnapshot{
		BlockNumber: blockNumber,
		Balance:     newBalance,
		Delta:       delta,
		TxHash:      txHash,
	}

	// Encode snapshot
	encoded, err := EncodeBalanceSnapshot(snapshot)
	if err != nil {
		return fmt.Errorf("failed to encode snapshot: %w", err)
	}

	// Get next sequence number (simple counter, could be optimized)
	seq, err := s.nextAddrSeq(ctx, seqBalance, addr)
	if err != nil {
		return err
	}

	// Store history entry
	if err := s.kv(ctx).Set(AddressBalanceKey(addr, seq), encoded, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set balance history: %w", err)
	}

	// Update latest balance
	balanceBytes := EncodeBigInt(newBalance)
	if err := s.kv(ctx).Set(AddressBalanceLatestKey(addr), balanceBytes, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set latest balance: %w", err)
	}

	return nil
}

// HasBalanceRecord reports whether any balance was recorded for addr.
func (s *PebbleStorage) HasBalanceRecord(ctx context.Context, addr common.Address) (bool, error) {
	if err := s.ensureNotClosed(); err != nil {
		return false, err
	}
	_, closer, err := s.kv(ctx).Get(AddressBalanceLatestKey(addr))
	if errors.Is(err, pebble.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, closer.Close()
}

// SetBalance sets the balance for an address at a specific block
func (s *PebbleStorage) SetBalance(ctx context.Context, addr common.Address, blockNumber uint64, balance *big.Int) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get current balance to calculate delta
	currentBalance, err := s.getAddressBalance(ctx, addr, 0)
	if err != nil {
		return fmt.Errorf("failed to get current balance: %w", err)
	}

	// Calculate delta
	delta := new(big.Int).Sub(balance, currentBalance)

	// Use UpdateBalance
	return s.UpdateBalance(ctx, addr, blockNumber, delta, common.Hash{})
}

// GetAddressStats returns aggregated statistics for an address
func (s *PebbleStorage) GetAddressStats(ctx context.Context, addr common.Address) (*port.AddressStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Iterate all transactions for this address
	prefix := AddressTransactionKeyPrefix(addr)
	return history.AddressStats(ctx, s, addr, func(yield func(common.Hash) error) error {
		iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
			LowerBound: prefix,
			UpperBound: prefixUpperBound(prefix),
		})
		if err != nil {
			return fmt.Errorf("failed to create iterator: %w", err)
		}
		defer func() { _ = iter.Close() }()

		for iter.First(); iter.Valid(); iter.Next() {
			if err := yield(common.BytesToHash(iter.Value())); err != nil {
				return err
			}
		}
		return iter.Error()
	})
}
