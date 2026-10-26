package execution

import (
	"context"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/proofdata"
)

// AttachStateProofInput authenticates retained selected block-end evidence and
// creates a NEW exact input before any spool admission or ACK. No RPC/EVM/GC.
func AttachStateProofInput(ctx context.Context, p chains.BinaryProfile, chain execwire.Chain, payload []byte, provider string, response []byte) ([]byte, error) {
	prepared, e := PrepareContext(ctx, p, chain, payload, false)
	if e != nil {
		return nil, e
	}
	if prepared.result.StateProofBundle != nil {
		return nil, port.ErrExecutionConflict
	}
	bundle, e := proofdata.Parse(response)
	if e != nil {
		return nil, e
	}
	canonical, e := json.Marshal(bundle)
	if e != nil {
		return nil, e
	}
	r := prepared.result
	r.StateProofBundle = &execwire.StateProofBundle{Version: 1, Format: execwire.StateProofFormat, Profile: p.ID(), Provider: provider, Response: canonical}
	if e = proofdata.Validate(ctx, p.ID(), chain, r, nil); e != nil {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	return execwire.MarshalResult(r)
}

// AttachStateProofFiles composes a new private durable input; originals remain.
func AttachStateProofFiles(ctx context.Context, p chains.BinaryProfile, chain execwire.Chain, input, proofs, output, provider string) (InputAttachment, error) {
	return composePrivateInput(ctx, input, proofs, output, execwire.MaxStateProofBytes, func(payload, response []byte) ([]byte, error) {
		return AttachStateProofInput(ctx, p, chain, payload, provider, response)
	})
}
