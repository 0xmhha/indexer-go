package capture

import (
	"context"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

// ValidateProjectionBlock checks the expected source scope, canonical pointer
// and complete manifest in ONE published-prefix view. The two bounded metadata
// reads belong only to that callback; no bytes or validation result are cached
// across calls, RPC, decode, or commits. A bound writer retains its SAME Store's
// generation fence through commit. Callers still need live fresh canonicality
// and the independent post-RPC epoch check; this method never issues RPC/ACKs.
// ErrGap with a returned state denotes bootstrap/older or not-yet-captured data.
func (s *Store) ValidateProjectionBlock(ctx context.Context, epoch uint64, plan common.Hash, block model.FeedBlock) (st State, err error) {
	if block.Chain != s.chain || epoch == 0 {
		return st, port.ErrInvalidData
	}
	err = s.projectionView(ctx, s.chain, func(tx port.CaptureReader) error {
		metadata, e := projectionRecordBatch(tx, [][]byte{[]byte("state"), []byte(heightKey(block.Number))})
		if e != nil {
			return e
		}
		st, e = loadState(metadata)
		if e != nil {
			return e
		}
		if st.Generation != epoch {
			return ErrStale
		}
		digest, e := st.Plan.Digest()
		if e != nil {
			return e
		}
		if digest != plan || st.Anchor.Chain != block.Chain {
			return ErrConflict
		}
		if block.Number <= st.Anchor.Number || block.Number > st.Through.Number {
			return ErrGap
		}
		var id model.CaptureIdentity
		if e := getJSON(metadata, heightKey(block.Number), &id); e != nil {
			return e
		}
		if id.Block.Chain != s.chain || id.Block.Number != block.Number {
			return ErrConflict
		}
		if id.Block.Hash != block.Hash {
			return ErrCanonical
		}
		facts, e := projectionRecordBatch(tx, [][]byte{[]byte(factsKey(id))})
		if e != nil {
			return e
		}
		for key, value := range facts {
			metadata[key] = value
		}
		return checkProjection(metadata, epoch, id)
	})
	return st, err
}
