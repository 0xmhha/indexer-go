package capture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

// Supplement is optional, immutable input bound to an already captured block.
// It does not expand the capture plan, advance Through or grant raw GC rights.
// This layer verifies ownership/digest/canonicality, NOT codec semantics. A
// codec's supplier and reader must independently verify its commitments.
type Supplement struct {
	Identity  model.CaptureIdentity `json:"identity"`
	Codec     string                `json:"codec"`
	Selection common.Hash           `json:"selection"`
	Digest    common.Hash           `json:"digest"`
	Data      []byte                `json:"data"`
}

const MaxSupplementBytes = 8 << 20

func supplementKey(id model.CaptureIdentity, codec string, selection common.Hash) string {
	return fmt.Sprintf("supplement/%x/%x/%x", rowOwner(id), sha256.Sum256([]byte(codec)), selection)
}
func validSupplement(in Supplement) bool {
	return validProjectionConsumer(in.Codec) && in.Selection != (common.Hash{}) && len(in.Data) > 0 && len(in.Data) <= MaxSupplementBytes && in.Digest == sha256.Sum256(in.Data)
}

// CommitSupplement checks the current epoch and fresh canonical hash inside
// the same durable transaction. An identical retry is revalidated; a changed
// value cannot replace an earlier immutable record.
func (s *Store) CommitSupplement(ctx context.Context, epoch uint64, in Supplement) error {
	if in.Identity.Block.Chain != s.chain || !validSupplement(in) {
		return port.ErrInvalidData
	}
	return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		if e := checkProjection(tx, epoch, in.Identity); e != nil {
			return e
		}
		key := supplementKey(in.Identity, in.Codec, in.Selection)
		var prior Supplement
		e := getJSON(tx, key, &prior)
		if e == nil && (prior.Identity != in.Identity || prior.Codec != in.Codec || prior.Selection != in.Selection || prior.Digest != in.Digest || !bytes.Equal(prior.Data, in.Data)) {
			return ErrConflict
		}
		if e != nil && !errors.Is(e, port.ErrNotFound) {
			return e
		}
		if e := s.fresh(ctx, in.Identity.Block); e != nil {
			return e
		}
		if e == nil {
			return nil
		}
		return putJSON(tx, key, in)
	})
}

// ReadSupplement is a current-generation lookup, including retained canonical
// ancestors. Old fork records stay in the DB but are not served as current.
func (s *Store) ReadSupplement(ctx context.Context, epoch uint64, id model.CaptureIdentity, codec string, selection common.Hash) (out Supplement, err error) {
	if id.Block.Chain != s.chain || !validProjectionConsumer(codec) || selection == (common.Hash{}) {
		return out, port.ErrInvalidData
	}
	err = s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		if e := checkProjection(tx, epoch, id); e != nil {
			return e
		}
		if e := getJSON(tx, supplementKey(id, codec, selection), &out); e != nil {
			return e
		}
		if out.Identity != id || out.Codec != codec || out.Selection != selection || !validSupplement(out) {
			return ErrConflict
		}
		return nil
	})
	return out, err
}
