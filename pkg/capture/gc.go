package capture

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/window"
)

// DeleteIntent is retained for lifecycle recovery after an interrupted or
// failed deletion. References identify the exact raw store/segment/input set;
// the token alone is never treated as evidence that file deletion finished.
type DeleteIntent struct {
	Token      common.Hash        `json:"token"`
	Generation uint64             `json:"generation"`
	References []window.Reference `json:"references"`
}

func (s *Store) PendingDeletion(ctx context.Context) (DeleteIntent, error) {
	var intent DeleteIntent
	err := s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		st, err := loadState(tx)
		if err != nil {
			return err
		}
		if st.PendingDelete == (common.Hash{}) {
			return port.ErrNotFound
		}
		if err := getJSON(tx, "deletion_intent", &intent); err != nil {
			return fmt.Errorf("%w: cannot read pending deletion intent: %v", ErrConflict, err)
		}
		if intent.Token != st.PendingDelete || intent.Generation != st.Generation || len(intent.References) == 0 {
			return ErrConflict
		}
		return nil
	})
	return intent, err
}

// WithDeletePermit implements window.DeletionGuard. All referenced payloads
// must exist in the durable DB with the exact digest/codec, complete required
// plan, canonical/finalized hash and every configured raw-only consumer ACK.
//
// A DURABLE generation fence is committed before calling deleteFiles. A lost
// PostgreSQL connection cannot silently release it during file deletion.
// The fence is cleared only after the synchronous callback succeeds.
// Crash/uncertain DB commit leaves it pinned and all mutations fail closed.
// RecoverDeletion joins an orphaned fence with the exclusively locked raw
// store and its exact durable completion receipt before releasing the fence.
func (s *Store) WithDeletePermit(ctx context.Context, refs []window.Reference, deleteFiles func() error) error {
	if len(refs) == 0 || len(refs) > 8192 || deleteFiles == nil {
		return window.ErrUnsafeGC
	}
	for _, r := range refs {
		if r.StoreID == (common.Hash{}) || r.Segment == 0 || r.StoreID != refs[0].StoreID || r.Segment != refs[0].Segment {
			return window.ErrUnsafeGC
		}
	}
	var token common.Hash
	if _, err := rand.Read(token[:]); err != nil {
		return err
	}
	err := s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		st, err := loadState(tx)
		if err != nil {
			return err
		}
		if err := mutable(st, st.Generation); err != nil {
			return err
		}
		if err := s.verifyDeletion(ctx, tx, st, refs); err != nil {
			return err
		}
		st.PendingDelete = token
		if err := putJSON(tx, "deletion_intent", DeleteIntent{Token: token, Generation: st.Generation, References: refs}); err != nil {
			return err
		}
		return putJSON(tx, "state", st)
	})
	if err != nil {
		return err
	} // including uncertain commit: never delete
	// A panic deliberately leaves the fence persisted, as does process death.
	deleteErr := deleteFiles()
	// A failed window callback may already have a durable .gc intent.
	// Releasing here would let a later restart finish deleting across a
	// generation change. Preserve the fence until lifecycle recovery.
	if deleteErr != nil {
		return deleteErr
	}
	// Cancellation cannot release a fence while a callback is active. After
	// it returns, a short independent cleanup transaction may release it.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	err = s.db.CaptureUpdate(cleanup, s.chain, func(tx port.CaptureWriter) error {
		st, err := loadState(tx)
		if err != nil {
			return err
		}
		if st.PendingDelete != token {
			return ErrConflict
		}
		st.PendingDelete = common.Hash{}
		return putJSON(tx, "state", st)
	})
	return err
}

var _ window.DeletionGuard = (*Store)(nil)

func (s *Store) verifyDeletion(ctx context.Context, tx port.CaptureReader, st State, refs []window.Reference) error {
	if st.Finalized == nil {
		return window.ErrUnsafeGC
	}
	type storedDataset struct {
		codec  string
		digest common.Hash
		size   int64
	}
	// Bounded by the segment's reference count; keep metadata, never payloads.
	// Each required block dataset is read/hashed once in THIS transaction, not
	// once for every raw reference and never cached across transactions.
	verified := make(map[model.FeedBlock]map[model.DatasetKind]storedDataset)
	stateChecked := make(map[uint64]bool)
	for _, r := range refs {
		if r.Block.Chain != s.chain || r.Block.Number > st.Finalized.Number {
			return window.ErrUnsafeGC
		}
		datasets, ok := verified[r.Block]
		if !ok {
			if err := s.requiredDeletion(tx, st, r.Block, stateChecked); err != nil {
				return err
			}
			id, err := canonicalID(tx, st, r.Block)
			if err != nil {
				return err
			}
			var in Input
			if err := getJSON(tx, factsKey(id), &in); err != nil {
				return err
			}
			if in.Manifest.Identity != id {
				return ErrConflict
			}
			if err := st.Plan.ValidateComplete(in.Manifest); err != nil {
				return err
			}
			datasets = make(map[model.DatasetKind]storedDataset, len(in.Manifest.Datasets))
			for _, e := range in.Manifest.Datasets {
				if e.Status == model.DatasetNotApplicable {
					continue
				}
				size, err := s.verifyDataset(ctx, tx, in, e)
				if err != nil {
					return err
				}
				datasets[e.Kind] = storedDataset{codec: e.Codec, digest: e.Digest, size: size}
			}
			if err := verifyRows(tx, id, in.Normalized); err != nil {
				return err
			}
			for _, c := range st.RawConsumers {
				v, err := tx.Get([]byte(ackKey(id, c)))
				if err != nil {
					return err
				}
				if len(v) != 1 || v[0] != 1 {
					return window.ErrUnsafeGC
				}
			}
			verified[r.Block] = datasets
		}
		d, ok := datasets[r.Kind]
		if !ok || d.codec != r.Codec || d.digest != r.Digest || d.size != r.PayloadBytes {
			return window.ErrUnsafeGC
		}
	}
	return nil
}
