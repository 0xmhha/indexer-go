package capture

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
)

var ErrObjectsUnavailable = errors.New("capture: permanent objects unavailable")
var ErrObjectRetired = errors.New("capture: object upload permanently retired")

type ObjectPayload struct {
	Kind model.DatasetKind `json:"kind"`
	Ref  window.ObjectRef  `json:"ref"`
}

// NewWithObjects borrows a separately opened permanent area for this chain.
// Only its exact store ID can satisfy external payload references. A plain New
// handle fails closed on external inputs, including during raw GC/recovery.
func NewWithObjects(db port.CaptureDatabase, chain model.FeedChain, fresh source.CanonicalReader, objects *window.Objects) (*Store, error) {
	s, e := New(db, chain, fresh)
	if e != nil {
		return nil, e
	}
	if objects == nil || objects.Chain() != chain {
		return nil, port.ErrInvalidData
	}
	s.objects = objects
	return s, nil
}
func objectFor(in Input, k model.DatasetKind) (window.ObjectRef, bool) {
	for _, p := range in.Objects {
		if p.Kind == k {
			return p.Ref, true
		}
	}
	return window.ObjectRef{}, false
}
func objectKey(r window.ObjectRef) string { return "objects/" + r.StoreID.Hex() + "/" + r.ID.Hex() }

type objectBinding struct {
	Ref     window.ObjectRef `json:"ref"`
	Retired bool             `json:"retired,omitempty"`
}

func (s *Store) checkObjectBinding(tx port.CaptureReader, ref window.ObjectRef, bind bool) error {
	if s.objects == nil || ref.StoreID != s.objects.ID() {
		return ErrObjectsUnavailable
	}
	var b objectBinding
	e := getJSON(tx, objectKey(ref), &b)
	if e == nil {
		if b.Ref != ref {
			return ErrConflict
		}
		if b.Retired {
			return ErrObjectRetired
		}
	} else if !errors.Is(e, port.ErrNotFound) {
		return e
	} else if !bind {
		return model.ErrCaptureIncomplete
	}
	return nil
}

func (s *Store) checkObjects(ctx context.Context, tx port.CaptureReader, objects []ObjectPayload, bind bool) error {
	for _, p := range objects {
		if e := s.checkObjectBinding(tx, p.Ref, bind); e != nil {
			return e
		}
		var e error
		if e = s.objects.Verify(ctx, p.Ref); e != nil {
			return e
		}
		if bind {
			w, ok := tx.(port.CaptureWriter)
			if !ok {
				return port.ErrInvalidData
			}
			if e = putJSON(w, objectKey(p.Ref), objectBinding{Ref: p.Ref}); e != nil {
				return e
			}
		}
	}
	return nil
}

// WithObjectDeletePermit implements permanent orphan cleanup, not raw GC.
// A successful serialized DB transaction writes an irrevocable retirement
// BEFORE unlink. Later Commit retries reject this ID, including other handles.
// Unknown/failed commit never unlinks; a committed tombstone is restartable.
// Linked objects (including retained forks) cannot be retired automatically.
func (s *Store) WithObjectDeletePermit(ctx context.Context, r window.ObjectRef, remove func() error) error {
	if s.objects == nil || r.Validate() != nil || r.StoreID != s.objects.ID() || remove == nil {
		return window.ErrUnsafeGC
	}
	e := s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		if e = mutable(st, st.Generation); e != nil {
			return e
		}
		var b objectBinding
		e = getJSON(tx, objectKey(r), &b)
		if e == nil {
			if b.Ref != r {
				return ErrConflict
			}
			if !b.Retired {
				return window.ErrRetain
			}
			return nil
		}
		if !errors.Is(e, port.ErrNotFound) {
			return e
		}
		return putJSON(tx, objectKey(r), objectBinding{Ref: r, Retired: true})
	})
	if e != nil {
		return e
	}
	return remove()
}

// CollectObjects inspects at most one bounded candidate page. Linked objects
// are retained forever; no TTL, fork reset or raw-window expiry frees them.
// Resume with next; zero next means the scan reached the end. Upload racing a
// collector can safely lose its attempt and must re-upload with a new random ID.
func (s *Store) CollectObjects(ctx context.Context, after common.Hash, limit int) (next common.Hash, removed int, err error) {
	if s.objects == nil {
		return next, 0, ErrObjectsUnavailable
	}
	refs, e := s.objects.Candidates(ctx, after, limit)
	if e != nil {
		return next, 0, e
	}
	for _, r := range refs {
		e = s.objects.Collect(ctx, r, s)
		if e == nil {
			removed++
		} else if !errors.Is(e, window.ErrRetain) {
			return next, removed, e
		}
		next = r.ID
	}
	if len(refs) < limit {
		next = common.Hash{}
	}
	return next, removed, nil
}

// CopyPayload streams a verified permanent dataset. For inline datasets it
// checks the normal Read contract. Callers commit derived work only on success.
func (s *Store) CopyPayload(ctx context.Context, id model.CaptureIdentity, k model.DatasetKind, w io.Writer) error {
	if w == nil {
		return port.ErrInvalidData
	}
	// Read validates every required payload and object before returning metadata.
	in, e := s.Read(ctx, id)
	if e != nil {
		return e
	}
	if r, ok := objectFor(in, k); ok {
		return s.objects.Copy(ctx, r, w)
	}
	for _, p := range in.Payloads {
		if p.Kind == k {
			n, e := w.Write(p.Bytes)
			if e == nil && n != len(p.Bytes) {
				e = io.ErrShortWrite
			}
			return e
		}
	}
	return fmt.Errorf("%w: dataset %s", port.ErrNotFound, k)
}

func (s *Store) verifyDataset(ctx context.Context, tx port.CaptureReader, in Input, e model.DatasetEvidence) (int64, error) {
	if r, ok := objectFor(in, e.Kind); ok {
		if r.Digest != e.Digest {
			return 0, ErrConflict
		}
		if err := s.checkObjects(ctx, tx, []ObjectPayload{{Kind: e.Kind, Ref: r}}, false); err != nil {
			return 0, err
		}
		return r.Size, nil
	}
	v, err := tx.Get([]byte(payloadKey(in.Manifest.Identity, e.Kind)))
	if err != nil {
		return 0, err
	}
	if len(v) > 8<<20 || common.Hash(sha256.Sum256(v)) != e.Digest {
		return 0, ErrConflict
	}
	return int64(len(v)), nil
}
