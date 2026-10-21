package fetch

import (
	"context"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/internal/rpcbatch"
	"github.com/ethereum/go-ethereum/common"
)

func (f *Fetcher) BalanceBatchAtHash(ctx context.Context, addresses []common.Address, hash common.Hash) ([]*big.Int, []error, error) {
	reader, ok := f.client.(interface {
		BalanceAtHashBatch(context.Context, []common.Address, common.Hash) ([]*big.Int, []error, error)
	})
	if !ok {
		return nil, nil, rpcbatch.ErrUnsupported
	}
	rctx, cancel := f.rpcCtx(ctx)
	defer cancel()
	values, failures, err := reader.BalanceAtHashBatch(rctx, addresses, hash)
	if err == nil && (len(values) != len(addresses) || len(failures) != len(addresses)) {
		return nil, nil, fmt.Errorf("state batch returned %d values/%d errors for %d addresses", len(values), len(failures), len(addresses))
	}
	return values, failures, err
}
