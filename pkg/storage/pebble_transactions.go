package storage

import (
	"context"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ============================================================================
// Transaction Methods
// ============================================================================

// GetTransaction returns a transaction and its location by hash
func (s *PebbleStorage) GetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *TxLocation, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, nil, err
	}

	// Get transaction location
	locValue, closer, err := s.db.Get(TransactionHashIndexKey(hash))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("failed to get transaction location: %w", err)
	}
	defer closer.Close()

	location, err := DecodeTxLocation(locValue)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode location: %w", err)
	}

	// Get transaction data
	txValue, closer, err := s.db.Get(TransactionKey(location.BlockHeight, location.TxIndex))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("failed to get transaction: %w", err)
	}
	defer closer.Close()

	tx, err := DecodeTransaction(txValue)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode transaction: %w", err)
	}

	return tx, location, nil
}

// GetTransactions returns multiple transactions and their locations by hash (batch operation)
func (s *PebbleStorage) GetTransactions(ctx context.Context, hashes []common.Hash) ([]*types.Transaction, []*TxLocation, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, nil, err
	}

	txs := make([]*types.Transaction, len(hashes))
	locations := make([]*TxLocation, len(hashes))
	var firstError error

	for i, hash := range hashes {
		tx, loc, err := s.GetTransaction(ctx, hash)
		if err != nil {
			if firstError == nil {
				firstError = err
			}
			continue
		}
		txs[i] = tx
		locations[i] = loc
	}

	if firstError != nil {
		return txs, locations, firstError
	}

	return txs, locations, nil
}

// SetTransaction stores a transaction with its location
func (s *PebbleStorage) SetTransaction(ctx context.Context, tx *types.Transaction, location *TxLocation) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	if tx == nil {
		return fmt.Errorf("transaction cannot be nil")
	}
	if location == nil {
		return fmt.Errorf("location cannot be nil")
	}

	// Encode transaction
	encoded, err := EncodeTransaction(tx)
	if err != nil {
		return fmt.Errorf("failed to encode transaction: %w", err)
	}

	// Encode location
	locEncoded, err := EncodeTxLocation(location)
	if err != nil {
		return fmt.Errorf("failed to encode location: %w", err)
	}

	// Write transaction data - use NoSync for performance
	if err := s.db.Set(TransactionKey(location.BlockHeight, location.TxIndex), encoded, pebble.NoSync); err != nil {
		return fmt.Errorf("failed to set transaction: %w", err)
	}

	// Write transaction hash index
	if err := s.db.Set(TransactionHashIndexKey(tx.Hash()), locEncoded, pebble.NoSync); err != nil {
		return fmt.Errorf("failed to set transaction index: %w", err)
	}

	// Update transaction count using atomic counter (avoid DB read)
	newCount := s.txCount.Add(1)
	if err := s.db.Set(TransactionCountKey(), EncodeUint64(newCount), pebble.NoSync); err != nil {
		return fmt.Errorf("failed to update transaction count: %w", err)
	}

	return nil
}

// GetTransactionsByAddress returns transactions for an address with pagination
func (s *PebbleStorage) GetTransactionsByAddress(ctx context.Context, addr common.Address, limit, offset int) ([]common.Hash, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	prefix := AddressTransactionKeyPrefix(addr)
	// Create upper bound by copying prefix and appending 0xff
	// Must copy to avoid modifying the prefix slice
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

	var hashes []common.Hash
	count := 0

	for iter.First(); iter.Valid(); iter.Next() {
		if count < offset {
			count++
			continue
		}

		if len(hashes) >= limit {
			break
		}

		var hash common.Hash
		copy(hash[:], iter.Value())
		hashes = append(hashes, hash)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return hashes, nil
}

// AddTransactionToAddressIndex adds a transaction to an address index
func (s *PebbleStorage) AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get next sequence number for this address
	s.addrSeqMu.Lock()
	seq := s.addrSeq[addr]
	s.addrSeq[addr]++
	s.addrSeqMu.Unlock()

	key := AddressTransactionKey(addr, seq)
	// Use NoSync for performance - caller should use Sync() or batch commit for durability
	return s.db.Set(key, txHash[:], pebble.NoSync)
}

// HasTransaction checks if a transaction exists
func (s *PebbleStorage) HasTransaction(ctx context.Context, hash common.Hash) (bool, error) {
	if err := s.ensureNotClosed(); err != nil {
		return false, err
	}

	_, closer, err := s.db.Get(TransactionHashIndexKey(hash))
	if err != nil {
		if err == pebble.ErrNotFound {
			return false, nil
		}
		return false, err
	}
	closer.Close()
	return true, nil
}
