package window

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOwnedRecoverySuppliesExactOwnerBeforeUnlink(t *testing.T) {
	o, _ := fixture(t)
	s, err := CreateOwned(o, ownerDomain, []byte(`{"Epoch":17}`))
	require.NoError(t, err)
	r, err := s.Append(t.Context(), envelope(o, 1))
	require.NoError(t, err)
	require.NoError(t, s.Seal(t.Context()))
	want, err := s.ReadOwner(t.Context())
	require.NoError(t, err)
	record, err := DescribeDeletion([]Reference{r})
	require.NoError(t, err)
	b, err := json.Marshal(record)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(o.Path, segmentName(r.Segment, "gc")), b, 0600))
	require.NoError(t, s.Close())
	denied := errors.New("SQL authorization absent")
	var observed Owner
	_, err = OpenOwnedAuthorized(o, ownerDomain, func(owner Owner, _ DeletionRecord) error { observed = owner; return denied })
	require.ErrorIs(t, err, denied)
	require.Equal(t, want, observed, "raw epoch/instance must be checked before recovery unlink")
	_, err = os.Stat(filepath.Join(o.Path, segmentName(r.Segment, "raw")))
	require.NoError(t, err)
	opened, err := OpenOwnedAuthorized(o, ownerDomain, func(owner Owner, got DeletionRecord) error {
		if string(owner.State) != string(want.State) || got != record {
			return ErrConflict
		}
		return nil
	})
	require.NoError(t, err)
	defer func() { _ = opened.Close() }()
	_, err = os.Stat(filepath.Join(o.Path, segmentName(r.Segment, "raw")))
	require.ErrorIs(t, err, os.ErrNotExist)
}

type recordGuard struct {
	apply func(context.Context, DeletionRecord, []Reference, func() error) error
}

func (recordGuard) WithDeletePermit(context.Context, []Reference, func() error) error {
	return ErrUnsafeGC // A complete-record consumer may never fall back.
}
func (g recordGuard) WithDeleteRecord(ctx context.Context, r DeletionRecord, refs []Reference, remove func() error) error {
	return g.apply(ctx, r, refs, remove)
}

func TestCollectionFullRecordPrecedesUnlinkAndExactCompletion(t *testing.T) {
	o, clock := fixture(t)
	s := created(t, o)
	r, err := s.Append(t.Context(), envelope(o, 1))
	require.NoError(t, err)
	require.NoError(t, s.Seal(t.Context()))
	clock.advance(24 * time.Hour)
	called := false
	n, err := s.Collect(t.Context(), recordGuard{apply: func(_ context.Context, record DeletionRecord, refs []Reference, remove func() error) error {
		called = true
		require.NoError(t, record.Match([]Reference{r}))
		require.Equal(t, []Reference{r}, refs)
		_, err := os.Stat(filepath.Join(o.Path, segmentName(r.Segment, "raw")))
		require.NoError(t, err, "durable DB authorization must precede this callback")
		if err := remove(); err != nil {
			return err
		}
		b, err := os.ReadFile(filepath.Join(o.Path, completedGC))
		require.NoError(t, err)
		var completed DeletionRecord
		require.NoError(t, json.Unmarshal(b, &completed))
		require.Equal(t, record, completed, "success carries the actual durable receipt")
		require.ErrorIs(t, remove(), ErrUnsafeGC)
		return nil
	}})
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, 1, n)
}

func TestCollectionFullRecordRejectsEscapedCallback(t *testing.T) {
	o, clock := fixture(t)
	s := created(t, o)
	r, err := s.Append(t.Context(), envelope(o, 1))
	require.NoError(t, err)
	require.NoError(t, s.Seal(t.Context()))
	clock.advance(24 * time.Hour)
	var late func() error
	_, err = s.Collect(t.Context(), recordGuard{apply: func(_ context.Context, _ DeletionRecord, _ []Reference, remove func() error) error {
		late = remove
		return nil
	}})
	require.ErrorIs(t, err, ErrUnsafeGC)
	require.NotNil(t, late, "full-record guard must receive the scoped callback")
	done := make(chan error, 1)
	go func() { done <- late() }()
	require.ErrorIs(t, <-done, ErrUnsafeGC)
	_, err = s.Lookup(t.Context(), r.Block, r.Kind)
	require.NoError(t, err)
}
