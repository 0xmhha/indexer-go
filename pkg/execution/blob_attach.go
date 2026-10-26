package execution

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob/proof"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/blobdata"
	"github.com/ethereum/go-ethereum/rlp"
)

// AttachBlobInput composes a new immutable input BEFORE spool admission/ACK.
// It never changes generation or replaces a previously attached bundle.
func AttachBlobInput(ctx context.Context, p chains.BinaryProfile, chain execwire.Chain, payload []byte, provider string, response []byte) ([]byte, error) {
	prepared, e := PrepareContext(ctx, p, chain, payload, false)
	if e != nil {
		return nil, e
	}
	if prepared.result.BlobBundle != nil {
		return nil, port.ErrExecutionConflict
	}
	refs, e := proof.References(prepared.block)
	if e != nil {
		return nil, e
	}
	if len(response) == 0 {
		if len(refs) > 0 {
			return nil, proof.ErrUnavailable
		}
		response, e = rlp.EncodeToBytes(proof.Response{Version: 1, Block: blobdata.Block(chain, prepared.Ref()), Entries: []proof.Entry{}})
		if e != nil {
			return nil, e
		}
	}
	r := prepared.result
	r.BlobBundle = &execwire.BlobBundle{Version: 1, Format: execwire.FullBlobFormat, Profile: p.ID(), Provider: provider, Response: append([]byte{}, response...)}
	if _, e = blobdata.Validate(ctx, p, chain, r, prepared.block, true); e != nil {
		return nil, e
	}
	return execwire.MarshalResult(r)
}
