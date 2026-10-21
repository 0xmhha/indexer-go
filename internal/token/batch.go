package token

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/0xmhha/indexer-go/internal/rpcbatch"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

// HashBatchEthClient optionally reads independent zero-value contract calls at
// one hash. Values/errors have the same order as the supplied calldata.
type HashBatchEthClient interface {
	CallContractBatchAtHash(context.Context, common.Address, [][]byte, common.Hash) ([][]byte, []error, error)
}

func (a *EthClientAdapter) CallContractBatchAtHash(ctx context.Context, address common.Address, inputs [][]byte, hash common.Hash) ([][]byte, []error, error) {
	if len(inputs) == 0 {
		return nil, nil, nil
	}
	if len(inputs) > 64 {
		return nil, nil, fmt.Errorf("state batch exceeds 64 calls")
	}
	if a.stateBatchUnavailable.Load() {
		return nil, nil, rpcbatch.ErrUnsupported
	}
	values := make([]hexutil.Bytes, len(inputs))
	batch := make([]rpc.BatchElem, len(inputs))
	for i, input := range inputs {
		args := map[string]any{"from": common.Address{}, "to": address, "input": hexutil.Bytes(input)}
		batch[i] = rpc.BatchElem{Method: "eth_call", Args: []any{args, rpc.BlockNumberOrHashWithHash(hash, false)}, Result: &values[i]}
	}
	err := a.client.Client().BatchCallContext(ctx, batch)
	if rpcbatch.Unsupported(err, batch) {
		a.stateBatchUnavailable.Store(true)
		return nil, nil, rpcbatch.ErrUnsupported
	}
	if err != nil {
		return nil, nil, err
	}
	out, failures := make([][]byte, len(inputs)), make([]error, len(inputs))
	for i, elem := range batch {
		out[i], failures[i] = values[i], elem.Error
	}
	return out, failures, nil
}

type preparedCall struct {
	value []byte
	err   error
}

// preloadMetadata requests only properties the existing fetcher will consume.
// Ordered URI fallbacks stay on their original path and are not speculated.
func (r *blockReader) preloadMetadata(ctx context.Context, address common.Address, standard TokenStandard, size int) error {
	reader, ok := r.client.(HashBatchEthClient)
	if !ok || r.hash == nil || size <= 1 {
		return nil
	}
	selectors := []string{SelectorName, SelectorSymbol}
	if standard == StandardERC20 {
		selectors = append(selectors, SelectorDecimals, SelectorTotalSupply)
	}
	inputs := make([][]byte, len(selectors))
	for i, selector := range selectors {
		input, err := hex.DecodeString(strings.TrimPrefix(selector, "0x"))
		if err != nil {
			return err
		}
		inputs[i] = input
	}
	r.preparedAddress = address
	r.preparedCalls = make(map[string]preparedCall, len(inputs))
	for from := 0; from < len(inputs); from += size {
		to := min(from+size, len(inputs))
		values, failures, err := reader.CallContractBatchAtHash(ctx, address, inputs[from:to], *r.hash)
		if errors.Is(err, rpcbatch.ErrUnsupported) {
			return nil // uncached properties keep the original per-call timeout/fallback
		}
		if err == nil && (len(values) != to-from || len(failures) != to-from) {
			err = fmt.Errorf("invalid contract batch result length")
		}
		for i := from; i < to; i++ {
			call := preparedCall{err: err}
			if err == nil {
				call.value, call.err = values[i-from], failures[i-from]
			}
			r.preparedCalls[string(inputs[i])] = call
		}
	}
	return nil
}
