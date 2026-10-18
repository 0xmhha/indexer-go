package window

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

func TestRecoveryChecksExactSegmentAndReceipt(t *testing.T) {
	opts, clock := fixture(t)
	s := created(t, opts)
	ctx := context.Background()
	a, err := s.Append(ctx, envelope(opts, 1))
	require.NoError(t, err)
	b, err := s.Append(ctx, envelope(opts, 2))
	require.NoError(t, err)
	require.NoError(t, s.Seal(ctx))
	refs := []Reference{a, b}
	complete := false
	finish := func() error { complete = true; return nil }
	bad := append([]Reference(nil), refs...)
	bad[0].Digest = common.Hash{99}
	require.ErrorIs(t, s.RecoverDeletion(ctx, bad, func() error { t.Fatal("forged input authorized"); return nil }, finish), ErrConflict)
	require.ErrorIs(t, s.RecoverDeletion(ctx, refs[:1], func() error { t.Fatal("subset authorized"); return nil }, finish), ErrConflict)
	require.ErrorIs(t, s.RecoverDeletion(ctx, refs, func() error { return nil }, finish), ErrUnsafeGC)
	require.False(t, complete)
	clock.advance(25 * time.Hour)
	lease, err := s.Acquire(ctx, a)
	require.NoError(t, err)
	require.ErrorIs(t, s.RecoverDeletion(ctx, refs, func() error { return nil }, finish), ErrBusy)
	require.NoError(t, lease.Close())
	require.False(t, complete)
	failure := errors.New("DB revalidation failed")
	require.ErrorIs(t, s.RecoverDeletion(ctx, refs, func() error { return failure }, finish), failure)
	_, err = s.Lookup(ctx, a.Block, a.Kind)
	require.NoError(t, err)
	// The DB completion response can be lost AFTER files have gone. A second
	// recovery must use the exact durable receipt, not restart the deletion.
	require.ErrorIs(t, s.RecoverDeletion(ctx, refs, func() error { return nil }, func() error { return failure }), failure)
	require.NoError(t, s.RecoverDeletion(ctx, []Reference{b, a}, func() error { return nil }, finish))
	require.True(t, complete)
	require.ErrorIs(t, s.RecoverDeletion(ctx, refs[:1], func() error { return nil }, finish), ErrConflict)
}

func TestRecoveryMissingFilesAreNotProof(t *testing.T) {
	opts, clock := fixture(t)
	s := created(t, opts)
	ctx := context.Background()
	r, err := s.Append(ctx, envelope(opts, 1))
	require.NoError(t, err)
	require.NoError(t, s.Seal(ctx))
	clock.advance(25 * time.Hour)
	require.NoError(t, s.Close())
	for _, suffix := range []string{"raw", "idx", "seal"} {
		require.NoError(t, os.Remove(filepath.Join(opts.Path, segmentName(r.Segment, suffix))))
	}
	s2, err := Open(opts)
	require.NoError(t, err)
	defer func() { require.NoError(t, s2.Close()) }()
	err = s2.RecoverDeletion(ctx, []Reference{r}, func() error { t.Fatal("absence treated as authorization"); return nil }, func() error { t.Fatal("fence cleared without receipt"); return nil })
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCheckedOpenAuthorizesBeforeRecoveryUnlink(t *testing.T) {
	opts, clock := fixture(t)
	s := created(t, opts)
	ctx := context.Background()
	r, err := s.Append(ctx, envelope(opts, 1))
	require.NoError(t, err)
	require.NoError(t, s.Seal(ctx))
	clock.advance(25 * time.Hour)
	digest, err := referencesHash([]Reference{r})
	require.NoError(t, err)
	require.NoError(t, writeJSON(s.root, segmentName(r.Segment, "gc"), gcMarker{StoreID: s.id, Segment: r.Segment, ReferencesHash: digest}))
	crashClose(t, s)
	failure := errors.New("source DB unavailable")
	_, err = OpenChecked(opts, func(record DeletionRecord) error { require.NoError(t, record.Match([]Reference{r})); return failure })
	require.ErrorIs(t, err, failure)
	for _, suffix := range []string{"raw", "idx", "seal", "gc"} {
		_, err := os.Stat(filepath.Join(opts.Path, segmentName(r.Segment, suffix)))
		require.NoError(t, err)
	}
	reopened, err := OpenChecked(opts, func(record DeletionRecord) error { return record.Match([]Reference{r}) })
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.NoError(t, reopened.RecoverDeletion(ctx, []Reference{r}, func() error { return nil }, func() error { return nil }))
}

func TestGCReceiptProcessCrash(t *testing.T) {
	if root := os.Getenv("INDEXER_GC_RECEIPT_CRASH_ROOT"); root != "" {
		opts := receiptOptions(root)
		now := time.Now().Add(-25 * time.Hour)
		opts.Now = func() time.Time { return now }
		require.NoError(t, InitializeVolume(opts.Volume))
		s, err := Create(opts)
		require.NoError(t, err)
		r, err := s.Append(context.Background(), envelope(opts, 1))
		require.NoError(t, err)
		require.NoError(t, s.Seal(context.Background()))
		v, err := json.Marshal([]Reference{r})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(root, "refs.json"), v, 0600))
		now = now.Add(25 * time.Hour)
		if os.Getenv("INDEXER_GC_RECEIPT_PHASE") == "intent" {
			s.afterGCIntent = func() { os.Exit(17) }
		} else if os.Getenv("INDEXER_GC_RECEIPT_PHASE") == "unlink" {
			s.afterGCUnlink = func() { os.Exit(17) }
		} else {
			s.afterGCCompletion = func() { os.Exit(17) }
		}
		_, _ = s.Collect(context.Background(), approved)
		t.Fatal("crash boundary not reached")
	}
	for _, phase := range []string{"intent", "unlink", "receipt"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestGCReceiptProcessCrash$")
			cmd.Env = append(os.Environ(), "INDEXER_GC_RECEIPT_CRASH_ROOT="+root, "INDEXER_GC_RECEIPT_PHASE="+phase)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit, string(out))
			require.Equal(t, 17, exit.ExitCode(), string(out))
			data, err := os.ReadFile(filepath.Join(root, "refs.json"))
			require.NoError(t, err)
			var refs []Reference
			require.NoError(t, json.Unmarshal(data, &refs))
			s, err := Open(receiptOptions(root))
			require.NoError(t, err)
			defer func() { require.NoError(t, s.Close()) }()
			called := false
			require.NoError(t, s.RecoverDeletion(context.Background(), refs, func() error { return nil }, func() error { called = true; return nil }))
			require.True(t, called)
			_, err = s.Lookup(context.Background(), refs[0].Block, refs[0].Kind)
			require.ErrorIs(t, err, ErrNotFound)
		})
	}
}

func receiptOptions(root string) Options {
	return Options{Volume: VolumeSpec{Root: root, ID: "gc-crash", AllowDirectory: true}, Path: filepath.Join(root, "raw"), Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, MaxBytes: 2 << 20, SegmentBytes: 32 << 10, MaxPayloadBytes: 2048, MaxSegmentRecords: 2, MaxSegments: 16}
}
