package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func publicationFixture(t *testing.T, parts int) (*Store, port.ExecutionConfig, port.ExecutionPublication) {
	t.Helper()
	s, cfg := executionFixture(t)
	require.NoError(t, s.InitExecution(t.Context(), cfg))
	require.NoError(t, s.SeedExecution(t.Context(), port.ExecutionBaseline{Ref: cfg.Anchor, Root: cfg.AnchorRoot}))
	p := firstExecutionPart(port.ExecutionBlocks, cfg)
	b := &model.Block{Number: p.Ref.Height, Hash: common.Hash(p.Ref.Hash), ParentHash: common.Hash(p.Parent), StateRoot: common.HexToHash("0x14")}
	for _, w := range port.ExecutionWriters()[:parts] {
		p.Writer = w
		require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error {
			if w == port.ExecutionBlocks {
				return s.ApplyExecutionCore(ctx, w, b, nil)
			}
			return nil
		}))
	}
	return s, cfg, port.ExecutionPublication{Ref: p.Ref, Parent: p.Parent, Digest: p.Digest, Root: execwire.Hash(b.StateRoot)}
}
func TestExecutionPublicationSevenPartsFreshAndAtomicVisibility(t *testing.T) {
	s, cfg, p := publicationFixture(t, 7)
	ctx := t.Context()
	applied, fresh := 0, 0
	apply := func(ctx context.Context) error {
		applied++
		if err := s.AppendOutbox(ctx, []port.OutboxEntry{{Type: "block", Data: []byte("one")}}); err != nil {
			return err
		}
		return s.Put(ctx, []byte("publication-feature"), []byte("one"))
	}
	validate := func(ctx context.Context) error {
		fresh++
		// All feature, visibility and undo writes are staged before the fresh call.
		value, err := s.Get(ctx, []byte("publication-feature"))
		require.NoError(t, err)
		require.Equal(t, []byte("one"), value)
		var undo int
		require.NoError(t, s.q(ctx).QueryRow(ctx, "SELECT count(*) FROM undo_blocks WHERE height=$1", int64(p.Ref.Height)).Scan(&undo))
		require.Equal(t, 1, undo)
		c, err := s.ExecutionStatus(ctx)
		require.NoError(t, err)
		require.Equal(t, p.Ref, c.Visible)
		external, err := s.ExecutionStatus(context.WithoutCancel(t.Context()))
		require.NoError(t, err)
		require.Equal(t, cfg.Anchor, external.Visible)
		return nil
	}
	committed, err := s.CommitExecution(ctx, p, apply, validate)
	require.NoError(t, err)
	require.True(t, committed)
	c, err := s.ExecutionStatus(ctx)
	require.NoError(t, err)
	require.Equal(t, p.Ref, c.Visible)
	require.Equal(t, p.Root, c.Root)
	committed, err = s.CommitExecution(ctx, p, apply, func(context.Context) error { fresh++; return nil })
	require.NoError(t, err)
	require.False(t, committed)
	require.Equal(t, 1, applied)
	require.Equal(t, 2, fresh)
	conflict := p
	conflict.Digest[0] ^= 1
	_, err = s.CommitExecution(ctx, conflict, apply, validate)
	require.ErrorIs(t, err, port.ErrExecutionConflict)
}
func TestExecutionPublicationFailureNeverPublishesOrLeavesFeature(t *testing.T) {
	s, cfg, p := publicationFixture(t, 7)
	failure := errors.New("fresh canonical failed")
	committed, err := s.CommitExecution(t.Context(), p, func(ctx context.Context) error { return s.Put(ctx, []byte("must-not-publish"), []byte("x")) }, func(context.Context) error { return failure })
	require.ErrorIs(t, err, failure)
	require.False(t, committed)
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, cfg.Anchor, c.Visible)
	_, err = s.Get(t.Context(), []byte("must-not-publish"))
	require.ErrorIs(t, err, port.ErrNotFound)
	var count int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM undo_blocks").Scan(&count))
	require.Zero(t, count)
	_, err = s.CommitExecution(t.Context(), p, func(context.Context) error { return nil }, nil)
	require.Error(t, err, "fresh callback is mandatory")
}
func TestExecutionPublicationIncompleteAndGenerationRejectedBeforeCallbacks(t *testing.T) {
	s, _, p := publicationFixture(t, 6)
	calls := 0
	callback := func(context.Context) error { calls++; return nil }
	_, err := s.CommitExecution(t.Context(), p, callback, callback)
	require.Error(t, err)
	require.Zero(t, calls)
	p.Ref.Generation++
	_, err = s.CommitExecution(t.Context(), p, callback, callback)
	require.ErrorIs(t, err, port.ErrExecutionStale)
	require.Zero(t, calls)
}

func TestExecutionPublicationPostFreshGenerationFenceAndQueuedError(t *testing.T) {
	s, cfg, p := publicationFixture(t, 7)
	_, err := s.CommitExecution(t.Context(), p, func(context.Context) error { return nil }, func(ctx context.Context) error {
		_, err := s.q(ctx).Exec(ctx, "UPDATE exec_control SET generation=generation+1 WHERE id")
		return err
	})
	require.ErrorIs(t, err, port.ErrExecutionStale)
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, cfg.Anchor, c.Visible)
	require.Equal(t, cfg.Anchor.Generation, c.Generation)
	calls := 0
	_, err = s.CommitExecution(t.Context(), p, func(ctx context.Context) error {
		s.boundTx(ctx).queue("INSERT INTO exec_control(id) VALUES(false)")
		return nil
	}, func(context.Context) error { calls++; return nil })
	require.Error(t, err)
	require.Zero(t, calls, "queued SQL errors fail before spending a fresh RPC")
}
