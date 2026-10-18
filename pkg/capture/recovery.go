package capture

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/window"
)

// RecoverDeletion resumes a durable permit before acquisition/GC starts.
// Window's process writer lock and gcMu exclude an active deletion, even if
// another capture Store uses the same DB. The DB token/generation and facts
// are revalidated AFTER acquiring that exclusion. Missing files without the
// matching completion receipt, another volume/store, active leases, damage,
// cancellation or any DB error leave the fence intact. No TTL/force unlock.
func (s *Store) RecoverDeletion(ctx context.Context, raw *window.Store) error {
	if raw == nil {
		return port.ErrInvalidData
	}
	intent, err := s.PendingDeletion(ctx)
	if errors.Is(err, port.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return raw.RecoverDeletion(ctx, intent.References, func() error {
		return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
			st, err := checkDeleteIntent(tx, intent)
			if err != nil {
				return err
			}
			return s.verifyDeletion(ctx, tx, st, intent.References)
		})
	}, func() error {
		return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
			st, err := checkDeleteIntent(tx, intent)
			if err != nil {
				return err
			}
			st.PendingDelete = common.Hash{}
			return putJSON(tx, "state", st)
		})
	})
}

func checkDeleteIntent(tx port.CaptureReader, expected DeleteIntent) (State, error) {
	st, err := loadState(tx)
	if err != nil {
		return st, err
	}
	if st.PendingDelete != expected.Token || st.Generation != expected.Generation {
		return st, ErrStale
	}
	var actual DeleteIntent
	if err := getJSON(tx, "deletion_intent", &actual); err != nil {
		return st, err
	}
	if !equalJSON(actual, expected) {
		return st, ErrConflict
	}
	return st, nil
}

// OpenWindow checks the DB permit before Open resumes even a partial .gc
// deletion, then finishes clearing the exact fence using the durable receipt.
// Use this coordinated entry point for capture sessions, rather than raw Open.
func (s *Store) OpenWindow(ctx context.Context, opts window.Options) (*window.Store, error) {
	if opts.Chain != s.chain {
		return nil, port.ErrInvalidData
	}
	raw, err := window.OpenChecked(opts, func(record window.DeletionRecord) error {
		return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
			st, err := loadState(tx)
			if err != nil {
				return err
			}
			if st.PendingDelete == (common.Hash{}) {
				return ErrConflict
			}
			var intent DeleteIntent
			if err := getJSON(tx, "deletion_intent", &intent); err != nil {
				return err
			}
			if _, err := checkDeleteIntent(tx, intent); err != nil {
				return err
			}
			if err := record.Match(intent.References); err != nil {
				return err
			}
			return s.verifyDeletion(ctx, tx, st, intent.References)
		})
	})
	if err != nil {
		return nil, err
	}
	if err := s.RecoverDeletion(ctx, raw); err != nil {
		return nil, errors.Join(err, raw.Close())
	}
	return raw, nil
}
