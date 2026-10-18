package capture

import (
	"bytes"
	"context"
	"errors"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// ErrProjectionInputLimit marks a permanent descriptor too large to
// materialize for execution. It also wraps port.ErrInvalidData at the call
// site; callers can distinguish decoder coverage from corrupt/missing bytes.
var ErrProjectionInputLimit = errors.New("capture: projection input exceeds materialization limit")

// These three small records belong to this snapshot only. The large payloads
// keep their existing 8MiB-per-dataset limit and are never passed to GetMany.
type projectionRecords map[string][]byte

func (r projectionRecords) Get(key []byte) ([]byte, error) {
	v, ok := r[string(key)]
	if !ok {
		return nil, port.ErrNotFound
	}
	return bytes.Clone(v), nil
}
func projectionMetadata(tx port.CaptureReader, id model.CaptureIdentity) (port.CaptureReader, error) {
	keys := [][]byte{[]byte("state"), []byte(heightKey(id.Block.Number)), []byte(factsKey(id))}
	return projectionRecordBatch(tx, keys)
}

func projectionRecordBatch(tx port.CaptureReader, keys [][]byte) (projectionRecords, error) {
	var values map[string][]byte
	if batch, ok := tx.(port.CaptureBatchReader); ok {
		var e error
		values, e = batch.GetMany(keys)
		if e != nil {
			return nil, e
		}
	} else {
		values = map[string][]byte{}
		for _, key := range keys {
			v, e := tx.Get(key)
			if e != nil && !missingProjectionRecord(e) {
				return nil, e
			}
			if e == nil {
				values[string(key)] = v
			}
		}
	}
	// Apply the same metadata bound to Get-only backends. Missing records
	// are evaluated by projectionInput in the original state/epoch order.
	for _, v := range values {
		if len(v) > port.CaptureBatchValueLimit {
			return nil, port.ErrInvalidData
		}
	}
	return projectionRecords(values), nil
}

// A joined I/O/cancellation failure must not disappear behind ErrNotFound.
func missingProjectionRecord(err error) bool {
	for err != nil {
		if err == port.ErrNotFound {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// ReadProjectionInput reads the exact canonical input in ONE capture snapshot,
// including bound, verified permanent objects materialized as payloads. It is
// not a current-epoch guarantee after return: consumers must revalidate after
// profile decoding and still perform their ordinary fresh check before commit.
// Historical fork reads remain available through Read, with its original API.
func (s *Store) ReadProjectionInput(ctx context.Context, epoch uint64, id model.CaptureIdentity) (out Input, err error) {
	if id.Block.Chain != s.chain {
		return out, port.ErrInvalidData
	}
	err = s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		metadata, e := projectionMetadata(tx, id)
		if e != nil {
			return e
		}
		_, in, e := projectionInput(metadata, epoch, id)
		if e != nil {
			return e
		}
		total := int64(0)
		for _, evidence := range in.Manifest.Datasets {
			if evidence.Status == model.DatasetNotApplicable {
				continue
			}
			if ref, external := objectFor(in, evidence.Kind); external {
				if ref.Validate() != nil {
					return port.ErrInvalidData
				}
				if ref.Size > 8<<20 || total+ref.Size > maxBlockBytes {
					return errors.Join(port.ErrInvalidData, ErrProjectionInputLimit)
				}
				total += ref.Size
				continue
			}
			value, e := tx.Get([]byte(payloadKey(id, evidence.Kind)))
			if e != nil {
				return e
			}
			total += int64(len(value))
			if len(value) > 8<<20 || total > maxBlockBytes {
				return port.ErrInvalidData
			}
			in.Payloads = append(in.Payloads, Payload{evidence.Kind, value})
		}
		if e = validatePayloads(in); e != nil {
			return e
		}
		hadObjects := len(in.Objects) != 0
		for _, p := range in.Objects {
			if e = s.checkObjectBinding(tx, p.Ref, false); e != nil {
				return e
			}
			// The DB binding is checked in this view; Copy independently checks
			// the exact permanent descriptor, length, digest and volume lease.
			b := &projectionBuffer{limit: int(p.Ref.Size)}
			if e = s.objects.Copy(ctx, p.Ref, b); e != nil {
				return e
			}
			in.Payloads = append(in.Payloads, Payload{p.Kind, b.Bytes()})
		}
		in.Objects = nil
		if hadObjects {
			if e = validatePayloads(in); e != nil {
				return e
			}
		}
		out = in
		return ctx.Err()
	})
	if err != nil {
		return Input{}, err
	}
	return out, nil
}

type projectionBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *projectionBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buf.Len() {
		return 0, port.ErrInvalidData
	}
	return b.buf.Write(p)
}

func (b *projectionBuffer) Bytes() []byte { return b.buf.Bytes() }

func (r *Reader) ReadProjectionInput(ctx context.Context, epoch uint64, id model.CaptureIdentity) (Input, error) {
	return r.store.ReadProjectionInput(ctx, epoch, id)
}
