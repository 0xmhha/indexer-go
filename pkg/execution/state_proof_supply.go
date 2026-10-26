package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0xmhha/indexer-go/execwire"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

// StateProofCaller must be the chain's existing shared, rate-limited client.
// This helper has no dialer, retry, alternative endpoint or latest fallback.
type StateProofCaller interface {
	BatchCallContext(context.Context, []rpc.BatchElem) error
}

var ErrStateProofUnavailable = errors.New("execution: selected hash-bound proof unavailable")

func AcquireStateProofInput(ctx context.Context, profile chains.BinaryProfile, chain execwire.Chain, payload []byte, provider string, selection stateproof.Selection, caller StateProofCaller) ([]byte, error) {
	if caller == nil {
		return nil, port.ErrInvalidData
	}
	prepared, e := PrepareContext(ctx, profile, chain, payload, false)
	if e != nil {
		return nil, e
	}
	if prepared.result.StateProofBundle != nil {
		return nil, port.ErrExecutionConflict
	}
	if e = execwire.ValidateStateProofBundle(&execwire.StateProofBundle{Version: 1, Format: execwire.StateProofFormat, Profile: profile.ID(), Provider: provider, Response: []byte(`{}`)}); e != nil {
		return nil, e
	}
	selected, _, e := selection.Normalize()
	if e != nil {
		return nil, e
	}
	bundle := stateproof.Bundle{Version: 1, Selection: selected, Evidence: make([]stateproof.Evidence, len(selected.Targets))}
	selector := map[string]interface{}{"blockHash": common.Hash(prepared.Ref().Hash), "requireCanonical": true}
	batch := make([]rpc.BatchElem, 0, len(selected.Targets)*2)
	for i, target := range selected.Targets {
		keys := make([]string, 0, len(target.Slots))
		for _, key := range target.Slots {
			keys = append(keys, key.Hex())
		}
		batch = append(batch, rpc.BatchElem{Method: "eth_getProof", Args: []interface{}{target.Address, keys, selector}, Result: &bundle.Evidence[i].Proof})
		if target.Code {
			bundle.Evidence[i].Code = new(hexutil.Bytes)
			batch = append(batch, rpc.BatchElem{Method: "eth_getCode", Args: []interface{}{target.Address, selector}, Result: bundle.Evidence[i].Code})
		}
	}
	if e = caller.BatchCallContext(ctx, batch); e != nil {
		return nil, fmt.Errorf("%w: %w", ErrStateProofUnavailable, e)
	}
	for _, member := range batch {
		if member.Error != nil {
			return nil, fmt.Errorf("%w: %w", ErrStateProofUnavailable, member.Error)
		}
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	raw, e := json.Marshal(bundle)
	if e != nil {
		return nil, e
	}
	return AttachStateProofInput(ctx, profile, chain, payload, provider, raw)
}
