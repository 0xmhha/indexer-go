package storage

import (
	"context"
	"fmt"

	"github.com/cockroachdb/pebble"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

// Ensure PebbleStorage implements SearchReader
var _ port.SearchReader = (*PebbleStorage)(nil)

// Search performs a unified search across blocks, transactions, and
// addresses (port.RunSearch).
func (s *PebbleStorage) Search(ctx context.Context, query string, resultTypes []string, limit int) ([]port.SearchResult, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}
	return port.RunSearch(ctx, searchSource{s}, query, resultTypes, limit)
}

// searchSource gives port.RunSearch the address transaction count.
type searchSource struct{ *PebbleStorage }

func (s searchSource) CountAddressTransactions(ctx context.Context, addr common.Address) (int, error) {
	return s.countAddressTransactions(ctx, addr)
}

// countAddressTransactions counts the transactions in an address's
// transaction index without loading them.
func (s *PebbleStorage) countAddressTransactions(ctx context.Context, addr common.Address) (int, error) {
	prefix := AddressTransactionKeyPrefix(addr)
	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		count++
	}
	if err := iter.Error(); err != nil {
		return 0, fmt.Errorf("iterator error: %w", err)
	}
	return count, nil
}
