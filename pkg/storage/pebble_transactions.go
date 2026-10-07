package storage

import (
	"context"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// ============================================================================
// Transaction Methods
// ============================================================================

// GetTransactions returns multiple transactions and their locations by hash (batch operation)
func (s *PebbleStorage) GetTransactions(ctx context.Context, hashes []common.Hash) ([]*model.Transaction, []*port.TxLocation, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, nil, err
	}

	txs := make([]*model.Transaction, len(hashes))
	locations := make([]*port.TxLocation, len(hashes))
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

// GetTransactionsByAddress returns one page of an address's transactions,
// in the order they were indexed.
func (s *PebbleStorage) GetTransactionsByAddress(ctx context.Context, addr common.Address, page port.Page) ([]common.Hash, string, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, "", err
	}
	prefix := AddressTransactionKeyPrefix(addr)
	entries, next, err := s.scanPage(ctx, prefix, prefixUpperBound(prefix), false, page, pageLimit(page, constants.DefaultPaginationLimit), nil)
	if err != nil {
		return nil, "", err
	}
	hashes := make([]common.Hash, len(entries))
	for i, e := range entries {
		hashes[i] = common.BytesToHash(e.Value)
	}
	return hashes, next, nil
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
	seq, err := s.nextAddrSeq(ctx, seqAddrTx, addr)
	if err != nil {
		return err
	}

	key := AddressTransactionKey(addr, seq)
	// Use NoSync for performance - caller should use Sync() or batch commit for durability
	return s.kv(ctx).Set(key, txHash[:], pebble.NoSync)
}

// HasTransaction checks if a transaction exists
func (s *PebbleStorage) HasTransaction(ctx context.Context, hash common.Hash) (bool, error) {
	if err := s.ensureNotClosed(); err != nil {
		return false, err
	}

	_, closer, err := s.kv(ctx).Get(TransactionHashIndexKey(hash))
	if err != nil {
		if err == pebble.ErrNotFound {
			return false, nil
		}
		return false, err
	}
	closer.Close()
	return true, nil
}
