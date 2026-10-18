package facts

import (
	"context"

	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// The legacy fetch Client shape is also DB-only, so passing this source as
// both NewFetcher's client and SetSource cannot accidentally retain an RPC
// fallback. These geth values are views; identity comes from the model source.
func (s *Source) GetLatestBlockNumber(ctx context.Context) (uint64, error) { return s.Head(ctx) }
func (s *Source) GetBlockByNumber(ctx context.Context, n uint64) (*types.Block, error) {
	b, e := s.Block(ctx, n)
	if e != nil {
		return nil, e
	}
	return gethconv.BlockToGeth(b)
}
func (s *Source) GetBlockReceipts(ctx context.Context, n uint64) (types.Receipts, error) {
	_, r, e := s.BlockWithReceipts(ctx, n)
	if e != nil {
		return nil, e
	}
	return gethconv.ReceiptsToGeth(r), nil
}
func (s *Source) GetTransactionByHash(ctx context.Context, _ common.Hash) (*types.Transaction, bool, error) {
	return nil, false, missingState(ctx, "global transaction lookup")
}

// Close is the legacy client's no-op: DB, objects and canonical RPC are
// borrowed. The owner closes them only after the fetcher has cancelled/joined.
func (s *Source) Close() {}
