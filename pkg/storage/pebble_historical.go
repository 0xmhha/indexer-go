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
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
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
			txHash := common.BytesToHash(value)

			// Get transaction and location
			tx, location, err := s.GetTransaction(ctx, txHash)
			if err != nil {
				if errors.Is(err, port.ErrNotFound) {
					return nil, false, nil
				}
				return nil, false, fmt.Errorf("failed to get transaction: %w", err)
			}

			// Get receipt
			receipt, err := s.GetReceipt(ctx, txHash)
			if err != nil {
				if !errors.Is(err, port.ErrNotFound) {
					return nil, false, fmt.Errorf("failed to get receipt: %w", err)
				}
				receipt = nil // Continue without receipt (optional)
			}

			// Apply filter
			if !filter.MatchTransaction(tx, receipt, location, addr) {
				return nil, false, nil
			}
			// Check fee delegation filter
			if filter.IsFeeDelegated != nil {
				isFD, err := s.isFeeDelegated(ctx, txHash)
				if err != nil {
					return nil, false, err
				}
				if *filter.IsFeeDelegated != isFD {
					return nil, false, nil
				}
			}
			return &port.TransactionWithReceipt{
				Transaction: tx,
				Receipt:     receipt,
				Location:    location,
			}, true, nil
		})
}

// isFeeDelegated reports whether a stored transaction has its gas paid by a
// fee payer, as the chain profile decoded it (chains.FeeDelegationOf).
func (s *PebbleStorage) isFeeDelegated(ctx context.Context, txHash common.Hash) (bool, error) {
	tx, _, err := s.GetTransaction(ctx, txHash)
	if err != nil {
		return false, fmt.Errorf("read transaction %s: %w", txHash.Hex(), err)
	}
	_, ok := chains.FeeDelegationOf(tx)
	return ok, nil
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

	// Get the latest height
	latestHeight, err := s.GetLatestHeight(ctx)
	if err != nil {
		if err == port.ErrNotFound {
			return []port.MinerStats{}, nil
		}
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Determine block range
	startBlock, endBlock, valid := determineBlockRange(fromBlock, toBlock, latestHeight)
	if !valid {
		return []port.MinerStats{}, nil
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

	stats := &port.AddressStats{
		Address:            addr,
		TotalGasCost:       big.NewInt(0),
		TotalValueSent:     big.NewInt(0),
		TotalValueReceived: big.NewInt(0),
	}

	uniqueAddresses := make(map[common.Address]bool)

	// Iterate all transactions for this address
	prefix := AddressTransactionKeyPrefix(addr)
	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		txHash := common.BytesToHash(iter.Value())

		// The model keeps the sender and hash the chain reports, and the
		// fee payer of fee delegation transactions.
		tx, location, err := s.GetTransaction(ctx, txHash)
		if err != nil {
			continue
		}
		receipt, _ := s.GetReceipt(ctx, txHash)
		from := tx.From
		value := tx.Value
		if value == nil {
			value = new(big.Int)
		}
		succeeded := receipt != nil && receipt.Status == model.ReceiptStatusSuccessful

		stats.TotalTransactions++

		// Sent vs Received. A failed transaction moves no value.
		to := tx.To
		if from == addr {
			stats.SentCount++
			if succeeded {
				stats.TotalValueSent.Add(stats.TotalValueSent, value)
			}
			if to != nil {
				uniqueAddresses[*to] = true
			}
		}
		if to != nil && *to == addr {
			stats.ReceivedCount++
			if succeeded {
				stats.TotalValueReceived.Add(stats.TotalValueReceived, value)
			}
			uniqueAddresses[from] = true
		}

		// Success vs Failed
		if receipt != nil {
			if succeeded {
				stats.SuccessCount++
			} else {
				stats.FailedCount++
			}
			// Gas is counted for the account that paid it: the sender, or
			// the fee payer of a fee delegation transaction.
			if chains.GasPayer(tx) == addr {
				stats.TotalGasUsed += receipt.GasUsed
				if receipt.EffectiveGasPrice != nil {
					cost := new(big.Int).Mul(new(big.Int).SetUint64(receipt.GasUsed), receipt.EffectiveGasPrice)
					stats.TotalGasCost.Add(stats.TotalGasCost, cost)
				}
			}
		}

		// Contract interaction (has input data and a target address)
		if to != nil && len(tx.Input) > 0 {
			stats.ContractInteractionCount++
		}

		// Timestamps
		if location != nil {
			block, err := s.GetBlock(ctx, location.BlockHeight)
			if err == nil && block != nil {
				ts := block.Time
				if stats.FirstTransactionTimestamp == 0 || ts < stats.FirstTransactionTimestamp {
					stats.FirstTransactionTimestamp = ts
				}
				if ts > stats.LastTransactionTimestamp {
					stats.LastTransactionTimestamp = ts
				}
			}
		}
	}

	stats.UniqueAddressCount = uint64(len(uniqueAddresses))

	return stats, nil
}
