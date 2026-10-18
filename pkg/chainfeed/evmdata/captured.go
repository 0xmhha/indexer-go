package evmdata

import (
	"bytes"
	"context"
	"io"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

type CapturedReader interface {
	Read(context.Context, model.CaptureIdentity) (capture.Input, error)
	CopyPayload(context.Context, model.CaptureIdentity, model.DatasetKind, io.Writer) error
	ValidateProjection(context.Context, uint64, model.CaptureIdentity) error
}

// ProjectionInputReader is optional. It combines the initial canonical check
// and bounded, materialized payload reads in one consistent capture snapshot.
// The final post-decode epoch check is still required.
type ProjectionInputReader interface {
	ReadProjectionInput(context.Context, uint64, model.CaptureIdentity) (capture.Input, error)
}

// ReadCaptured verifies permanent source commitments through the selected
// profile. Reads never consult RPC or the raw window and never write back.
func ReadCaptured(ctx context.Context, r CapturedReader, p chains.BinaryProfile, epoch uint64, id model.CaptureIdentity) (*model.Block, []*model.Receipt, error) {
	if r == nil || p == nil {
		return nil, nil, port.ErrInvalidData
	}
	var in capture.Input
	var e error
	fast, materialized := r.(ProjectionInputReader)
	if materialized {
		in, e = fast.ReadProjectionInput(ctx, epoch, id)
	} else {
		if e = r.ValidateProjection(ctx, epoch, id); e != nil {
			return nil, nil, e
		}
		in, e = r.Read(ctx, id)
	}
	if e != nil {
		return nil, nil, e
	}
	if materialized && len(in.Objects) != 0 {
		return nil, nil, capture.ErrConflict
	}
	if in.Manifest.Identity != id || len(in.Manifest.Datasets) != 3 {
		return nil, nil, capture.ErrConflict
	}
	seen := map[model.DatasetKind]bool{}
	for _, v := range in.Manifest.Datasets {
		if Codec(v.Kind) == "" || seen[v.Kind] || v.Codec != Codec(v.Kind) || v.Verification != model.VerificationCommitment || v.Status != model.DatasetPersisted || v.Coverage.Scope != model.CoverageComplete {
			return nil, nil, capture.ErrConflict
		}
		seen[v.Kind] = true
	}
	data := map[model.DatasetKind][]byte{}
	for _, v := range in.Payloads {
		if !seen[v.Kind] || data[v.Kind] != nil {
			return nil, nil, capture.ErrConflict
		}
		data[v.Kind] = v.Bytes
	}
	for _, v := range in.Objects {
		if !seen[v.Kind] || data[v.Kind] != nil || v.Ref.Size > 8<<20 {
			return nil, nil, capture.ErrConflict
		}
		var b bytes.Buffer
		if e = r.CopyPayload(ctx, id, v.Kind, &b); e != nil {
			return nil, nil, e
		}
		data[v.Kind] = b.Bytes()
	}
	b, rs, e := Decode(p, id.Block, data[model.DatasetHeaders], data[model.DatasetBodies], data[model.DatasetReceipts])
	if e != nil {
		return nil, nil, e
	}
	if b.ParentHash != in.ParentHash {
		return nil, nil, capture.ErrConflict
	}
	if e = r.ValidateProjection(ctx, epoch, id); e != nil {
		return nil, nil, e
	}
	return b, rs, nil
}

// SourceRows retains the registered model codecs (including chain extensions).
// Callers must first validate encoded source inputs with Decode/ReadCaptured.
func SourceRows(b *model.Block, rs []*model.Receipt) ([]capture.Row, error) {
	return normalizedRows(b, rs)
}
