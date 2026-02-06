package storage

import (
	"context"
	"fmt"
	"math/big"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Ensure PebbleStorage implements HistoricalReader and HistoricalWriter
var _ HistoricalReader = (*PebbleStorage)(nil)
var _ HistoricalWriter = (*PebbleStorage)(nil)

// ============================================================================
// Historical Data Methods
// ============================================================================

// GetBlocksByTimeRange returns blocks within a time range
func (s *PebbleStorage) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*types.Block, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if fromTime > toTime {
		return nil, fmt.Errorf("fromTime (%d) cannot be greater than toTime (%d)", fromTime, toTime)
	}

	// Create iterator for timestamp range
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: BlockTimestampKey(fromTime, 0),
		UpperBound: BlockTimestampKey(toTime+1, 0),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var blocks []*types.Block
	count := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip offset items
		if count < offset {
			count++
			continue
		}

		// Stop if limit reached
		if len(blocks) >= limit {
			break
		}

		// Extract height from value
		height, err := DecodeUint64(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode height: %w", err)
		}

		// Get block by height
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			if err == ErrNotFound {
				continue // Skip missing blocks
			}
			return nil, fmt.Errorf("failed to get block %d: %w", height, err)
		}

		blocks = append(blocks, block)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return blocks, nil
}

// GetBlockByTimestamp returns the block closest to the given timestamp
func (s *PebbleStorage) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Binary search for closest timestamp
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: BlockTimestampKeyPrefix(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Seek to the target timestamp
	iter.SeekGE(BlockTimestampKey(timestamp, 0))

	var closestHeight uint64
	var found bool

	if iter.Valid() {
		// Found exact or later timestamp
		height, err := DecodeUint64(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode height: %w", err)
		}
		closestHeight = height
		found = true
	} else {
		// Seek to last block before timestamp
		iter.Last()
		if iter.Valid() {
			height, err := DecodeUint64(iter.Value())
			if err != nil {
				return nil, fmt.Errorf("failed to decode height: %w", err)
			}
			closestHeight = height
			found = true
		}
	}

	if !found {
		return nil, ErrNotFound
	}

	return s.GetBlock(ctx, closestHeight)
}

// GetTransactionsByAddressFiltered returns filtered transactions for an address
func (s *PebbleStorage) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *TransactionFilter, limit, offset int) ([]*TransactionWithReceipt, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if filter == nil {
		filter = DefaultTransactionFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	// Get all transaction hashes for the address
	// We need to scan all because we don't have block-indexed address transactions
	prefix := AddressTransactionKeyPrefix(addr)
	upperBound := make([]byte, len(prefix), len(prefix)+1)
	copy(upperBound, prefix)
	upperBound = append(upperBound, 0xff)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upperBound,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var results []*TransactionWithReceipt
	count := 0

	for iter.First(); iter.Valid(); iter.Next() {
		if len(results) >= limit {
			break
		}

		var txHash common.Hash
		copy(txHash[:], iter.Value())

		// Get transaction and location
		tx, location, err := s.GetTransaction(ctx, txHash)
		if err != nil {
			if err == ErrNotFound {
				continue
			}
			return nil, fmt.Errorf("failed to get transaction: %w", err)
		}

		// Get receipt
		receipt, err := s.GetReceipt(ctx, txHash)
		if err != nil {
			if err == ErrNotFound {
				// Continue without receipt (optional)
				receipt = nil
			} else {
				return nil, fmt.Errorf("failed to get receipt: %w", err)
			}
		}

		// Apply filter
		if filter.MatchTransaction(tx, receipt, location, addr) {
			if count < offset {
				count++
				continue
			}

			results = append(results, &TransactionWithReceipt{
				Transaction: tx,
				Receipt:     receipt,
				Location:    location,
			})
			count++
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return results, nil
}

// GetAddressBalance returns the balance of an address at a specific block
func (s *PebbleStorage) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// If blockNumber is 0, get latest balance
	if blockNumber == 0 {
		value, closer, err := s.db.Get(AddressBalanceLatestKey(addr))
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

	iter, err := s.db.NewIter(&pebble.IterOptions{
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

// GetBalanceHistory returns the balance history for an address
func (s *PebbleStorage) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]BalanceSnapshot, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", fromBlock, toBlock)
	}

	prefix := AddressBalanceKeyPrefix(addr)
	upperBound := make([]byte, len(prefix), len(prefix)+1)
	copy(upperBound, prefix)
	upperBound = append(upperBound, 0xff)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upperBound,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var snapshots []BalanceSnapshot
	count := 0

	for iter.First(); iter.Valid(); iter.Next() {
		if len(snapshots) >= limit {
			break
		}

		snapshot, err := DecodeBalanceSnapshot(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode snapshot: %w", err)
		}

		// Filter by block range
		if snapshot.BlockNumber < fromBlock || snapshot.BlockNumber > toBlock {
			continue
		}

		if count < offset {
			count++
			continue
		}

		snapshots = append(snapshots, *snapshot)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return snapshots, nil
}

// GetBlockCount returns the total number of indexed blocks
func (s *PebbleStorage) GetBlockCount(ctx context.Context) (uint64, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}

	// Get latest block height
	height, err := s.GetLatestHeight(ctx)
	if err != nil {
		if err == ErrNotFound {
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
	value, closer, err := s.db.Get(TransactionCountKey())
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
			if err == ErrNotFound {
				continue // Skip missing blocks
			}
			return fmt.Errorf("failed to get block %d: %w", height, err)
		}

		totalTxCount += uint64(len(block.Transactions()))
	}

	// Set the transaction count
	if err := s.db.Set(TransactionCountKey(), EncodeUint64(totalTxCount), pebble.Sync); err != nil {
		return fmt.Errorf("failed to set transaction count: %w", err)
	}

	// Update atomic counter
	s.txCount.Store(totalTxCount)
	s.txCountReady.Store(true)

	return nil
}

// GetTopMiners returns the top miners by block count
func (s *PebbleStorage) GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]MinerStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Get the latest height
	latestHeight, err := s.GetLatestHeight(ctx)
	if err != nil {
		if err == ErrNotFound {
			return []MinerStats{}, nil
		}
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Determine block range
	startBlock, endBlock, valid := determineBlockRange(fromBlock, toBlock, latestHeight)
	if !valid {
		return []MinerStats{}, nil
	}

	// Aggregate miner stats
	minerMap, totalBlocks := s.aggregateMinerStats(ctx, startBlock, endBlock)

	// Calculate percentages
	calculateMinerPercentages(minerMap, totalBlocks)

	// Sort and apply limit
	return sortAndLimitMinerStats(minerMap, limit), nil
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
	return s.db.Set(BlockTimestampKey(timestamp, height), value, pebble.Sync)
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
	currentBalance, err := s.GetAddressBalance(ctx, addr, 0) // Get latest
	if err != nil {
		return fmt.Errorf("failed to get current balance: %w", err)
	}

	// Calculate new balance
	newBalance := new(big.Int).Add(currentBalance, delta)
	if newBalance.Sign() < 0 {
		return fmt.Errorf("balance cannot be negative")
	}

	// Create snapshot
	snapshot := &BalanceSnapshot{
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
	s.addrSeqMu.Lock()
	seq := s.addrSeq[addr]
	s.addrSeq[addr]++
	s.addrSeqMu.Unlock()

	// Store history entry
	if err := s.db.Set(AddressBalanceKey(addr, seq), encoded, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set balance history: %w", err)
	}

	// Update latest balance
	balanceBytes := EncodeBigInt(newBalance)
	if err := s.db.Set(AddressBalanceLatestKey(addr), balanceBytes, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set latest balance: %w", err)
	}

	return nil
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
	currentBalance, err := s.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		return fmt.Errorf("failed to get current balance: %w", err)
	}

	// Calculate delta
	delta := new(big.Int).Sub(balance, currentBalance)

	// Use UpdateBalance
	return s.UpdateBalance(ctx, addr, blockNumber, delta, common.Hash{})
}
