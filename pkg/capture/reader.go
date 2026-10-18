package capture

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
	"io"
)

// Reader exposes only DB/object reads. It has no canonical RPC dependency or
// write methods and cannot initialize storage, ACK work or authorize deletion.
// Objects are borrowed; API callers must supply the configured permanent area
// when inputs are external. A missing area never falls back to window/RPC.
type Reader struct{ store *Store }

func NewReader(db port.CaptureDatabase, chain model.FeedChain, objects *window.Objects) (*Reader, error) {
	if db == nil || chain.Validate() != nil {
		return nil, port.ErrInvalidData
	}
	return &Reader{store: &Store{db: db, chain: chain, objects: objects}}, nil
}
func (r *Reader) Read(ctx context.Context, id model.CaptureIdentity) (Input, error) {
	return r.store.Read(ctx, id)
}
func (r *Reader) CopyPayload(ctx context.Context, id model.CaptureIdentity, k model.DatasetKind, w io.Writer) error {
	return r.store.CopyPayload(ctx, id, k, w)
}
func (r *Reader) ValidateProjection(ctx context.Context, epoch uint64, id model.CaptureIdentity) error {
	return r.store.ValidateProjection(ctx, epoch, id)
}
func (r *Reader) CanonicalAt(ctx context.Context, epoch, n uint64) (model.CaptureIdentity, error) {
	return r.store.CanonicalAt(ctx, epoch, n)
}
func (r *Reader) ReadSupplement(ctx context.Context, epoch uint64, id model.CaptureIdentity, codec string, selection common.Hash) (Supplement, error) {
	return r.store.ReadSupplement(ctx, epoch, id, codec, selection)
}
