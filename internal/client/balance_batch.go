package client

import (
	"context"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/internal/rpcbatch"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

// BalanceAtHashBatch returns results and errors in address order. A successful
// envelope does not imply every member succeeded. No latest/zero fallback is used.
func (c *Client) BalanceAtHashBatch(ctx context.Context, addresses []common.Address, hash common.Hash) ([]*big.Int, []error, error) {
	if len(addresses) == 0 {
		return nil, nil, nil
	}
	if len(addresses) > 64 {
		return nil, nil, fmt.Errorf("state batch exceeds 64 addresses")
	}
	if c.stateBatchUnavailable.Load() {
		return nil, nil, rpcbatch.ErrUnsupported
	}
	decoded := make([]*hexutil.Big, len(addresses))
	batch := make([]rpc.BatchElem, len(addresses))
	for i, address := range addresses {
		batch[i] = rpc.BatchElem{Method: "eth_getBalance", Args: []any{address, rpc.BlockNumberOrHashWithHash(hash, false)}, Result: &decoded[i]}
	}
	err := c.rpcClient.BatchCallContext(ctx, batch)
	if rpcbatch.Unsupported(err, batch) {
		c.stateBatchUnavailable.Store(true)
		return nil, nil, rpcbatch.ErrUnsupported
	}
	if err != nil {
		return nil, nil, err
	}
	values, failures := make([]*big.Int, len(addresses)), make([]error, len(addresses))
	for i, elem := range batch {
		failures[i] = elem.Error
		if elem.Error == nil {
			if decoded[i] == nil {
				failures[i] = fmt.Errorf("balance batch has no result for %s", addresses[i])
			} else {
				values[i] = new(big.Int).Set((*big.Int)(decoded[i]))
			}
		}
	}
	return values, failures, nil
}
