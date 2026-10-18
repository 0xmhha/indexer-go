package capture

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

const MaxWorkerOutput = 64 << 10

// WorkerCursor belongs only to immutable block outputs in this namespace.
// It grants no permission for raw GC and never modifies projection undo.
type WorkerCursor struct {
	Name       string          `json:"name"`
	Definition string          `json:"definition"`
	Epoch      uint64          `json:"epoch"`
	Anchor     model.FeedBlock `json:"anchor"`
	Through    model.FeedBlock `json:"through"`
	Sequence   uint64          `json:"sequence"`
}
type WorkerOutput struct {
	Identity   model.CaptureIdentity `json:"identity"`
	Definition string                `json:"definition"`
	Data       json.RawMessage       `json:"data"`
}
type WorkerEvent struct {
	Sequence uint64          `json:"sequence"`
	Kind     string          `json:"kind"`
	Epoch    uint64          `json:"epoch"`
	Through  model.FeedBlock `json:"through"`
}

func workerPrefix(name string) string { return fmt.Sprintf("worker/%x/", sha256.Sum256([]byte(name))) }
func workerOutputKey(name, definition string, id model.CaptureIdentity) string {
	return fmt.Sprintf("%soutput/%x/%x", workerPrefix(name), sha256.Sum256([]byte(definition)), rowOwner(id))
}
func validWorker(name, definition string) bool {
	return validProjectionConsumer(name) && validProjectionConsumer(definition)
}
func (s *Store) InitializeWorker(ctx context.Context, name, definition string) (out WorkerCursor, err error) {
	if !validWorker(name, definition) {
		return out, port.ErrInvalidData
	}
	err = s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		key := workerPrefix(name) + "cursor"
		e = getJSON(tx, key, &out)
		if e == nil {
			if !validWorkerCursor(out) || out.Name != name || out.Definition != definition || out.Anchor != st.Anchor || out.Epoch > st.Generation {
				return ErrConflict
			}
			if out.Epoch == st.Generation {
				return nil
			}
			if out.Sequence == math.MaxUint64 {
				return port.ErrInvalidData
			}
			through, e := retainedWorkerPrefix(tx, st, out)
			if e != nil {
				return e
			}
			out.Epoch = st.Generation
			out.Through = through
			out.Sequence++
			if e = putJSON(tx, workerPrefix(name)+fmt.Sprintf("events/%020d", out.Sequence), WorkerEvent{out.Sequence, "reset", out.Epoch, out.Through}); e != nil {
				return e
			}
		} else if !errors.Is(e, port.ErrNotFound) {
			return e
		} else {
			out = WorkerCursor{Name: name, Definition: definition, Epoch: st.Generation, Anchor: st.Anchor, Through: st.Anchor}
		}
		return putJSON(tx, key, out)
	})
	return out, err
}

// CommitWorker stores immutable block-scoped output, contiguous cursor and its
// OWN outbox event atomically. Construction/decoding happens before this call.
// Reset reuses a verified immutable canonical prefix (bounded 128-height search),
// otherwise starts at the bootstrap. Old fork output remains historical.
func (s *Store) CommitWorker(ctx context.Context, name, definition string, epoch uint64, id model.CaptureIdentity, data []byte) error {
	if name == model.SourceLookupWorker || name == model.SourceSearchWorker || !validWorker(name, definition) || len(data) > MaxWorkerOutput || !json.Valid(data) || id.Block.Chain != s.chain {
		return port.ErrInvalidData
	}
	canonical, e := json.Marshal(json.RawMessage(data))
	if e != nil || len(canonical) > MaxWorkerOutput {
		return port.ErrInvalidData
	}
	data = canonical
	return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		return s.commitWorker(ctx, tx, name, definition, epoch, id, data)
	})
}

// commitWorker shares the transaction with derived index node writes.
func (s *Store) commitWorker(ctx context.Context, tx port.CaptureWriter, name, definition string, epoch uint64, id model.CaptureIdentity, data []byte) error {
	if e := checkProjection(tx, epoch, id); e != nil {
		return e
	}
	var cursor WorkerCursor
	if e := getJSON(tx, workerPrefix(name)+"cursor", &cursor); e != nil {
		return e
	}
	if !validWorkerCursor(cursor) || cursor.Name != name || cursor.Definition != definition || cursor.Epoch != epoch {
		return ErrStale
	}
	output := WorkerOutput{Identity: id, Definition: definition, Data: json.RawMessage(data)}
	key := workerOutputKey(name, definition, id)
	if cursor.Through == id.Block {
		var prior WorkerOutput
		if e := getJSON(tx, key, &prior); e != nil {
			return e
		}
		if !equalJSON(prior, output) {
			return ErrConflict
		}
		return s.fresh(ctx, id.Block)
	}
	if cursor.Through.Number == math.MaxUint64 || id.Block.Number != cursor.Through.Number+1 {
		return ErrGap
	}
	var input Input
	if e := getJSON(tx, factsKey(id), &input); e != nil {
		return e
	}
	if input.ParentHash != cursor.Through.Hash {
		return ErrGap
	}
	if e := s.fresh(ctx, id.Block); e != nil {
		return e
	}
	// Uncertain commits and a replay of retained ancestors must agree byte-for-byte.
	var prior WorkerOutput
	e := getJSON(tx, key, &prior)
	if e == nil && !equalJSON(prior, output) {
		return ErrConflict
	}
	if e != nil && !errors.Is(e, port.ErrNotFound) {
		return e
	}
	if cursor.Sequence == math.MaxUint64 {
		return port.ErrInvalidData
	}
	cursor.Through = id.Block
	cursor.Sequence++
	if e = putJSON(tx, key, output); e != nil {
		return e
	}
	if e = putJSON(tx, workerPrefix(name)+"cursor", cursor); e != nil {
		return e
	}
	return putJSON(tx, workerPrefix(name)+fmt.Sprintf("events/%020d", cursor.Sequence), WorkerEvent{cursor.Sequence, "output", epoch, id.Block})
}

func (s *Store) WorkerOutput(ctx context.Context, name, definition string, id model.CaptureIdentity) (out WorkerOutput, err error) {
	if !validWorker(name, definition) || id.Block.Chain != s.chain {
		return out, port.ErrInvalidData
	}
	err = s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		if e := getJSON(tx, workerOutputKey(name, definition, id), &out); e != nil {
			return e
		}
		if out.Identity != id || out.Definition != definition || len(out.Data) > MaxWorkerOutput || !json.Valid(out.Data) {
			return ErrConflict
		}
		return nil
	})
	return out, err
}
func (s *Store) WorkerEvents(ctx context.Context, name string, after uint64, limit int) (out []WorkerEvent, err error) {
	if !validProjectionConsumer(name) || limit < 1 || limit > 256 {
		return nil, port.ErrInvalidData
	}
	out = []WorkerEvent{}
	err = s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		var c WorkerCursor
		if e := getJSON(tx, workerPrefix(name)+"cursor", &c); e != nil {
			return e
		}
		if after > c.Sequence {
			return ErrConflict
		}
		for n := after; n < c.Sequence && len(out) < limit; {
			n++
			var e WorkerEvent
			if err := getJSON(tx, workerPrefix(name)+fmt.Sprintf("events/%020d", n), &e); err != nil {
				return err
			}
			if e.Sequence != n {
				return ErrConflict
			}
			out = append(out, e)
		}
		return nil
	})
	return out, err
}

func retainedWorkerPrefix(tx port.CaptureReader, st State, c WorkerCursor) (model.FeedBlock, error) {
	n := min(c.Through.Number, st.Through.Number)
	for checked := 0; n > st.Anchor.Number && checked < 128; checked++ {
		id, e := canonicalAtHeight(tx, st, n)
		if e != nil {
			return model.FeedBlock{}, e
		}
		var out WorkerOutput
		e = getJSON(tx, workerOutputKey(c.Name, c.Definition, id), &out)
		if e == nil {
			if out.Identity != id || out.Definition != c.Definition || len(out.Data) > MaxWorkerOutput || !json.Valid(out.Data) {
				return model.FeedBlock{}, ErrConflict
			}
			if e = checkProjection(tx, st.Generation, id); e != nil {
				return model.FeedBlock{}, e
			}
			return id.Block, nil
		}
		if e != port.ErrNotFound {
			return model.FeedBlock{}, e
		}
		n--
	}
	return st.Anchor, nil
}

func validWorkerCursor(c WorkerCursor) bool {
	return validWorker(c.Name, c.Definition) && c.Epoch > 0 && c.Anchor.Validate() == nil && c.Through.Validate() == nil && c.Through.Chain == c.Anchor.Chain && c.Through.Number >= c.Anchor.Number && (c.Through.Number != c.Anchor.Number || c.Through == c.Anchor)
}
