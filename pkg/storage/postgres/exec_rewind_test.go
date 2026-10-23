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

func TestExecutionRewindMarkerFencesAndSurvivesRestart(t *testing.T) {
	s, cfg, p := publicationFixture(t, 7)
	_, err := s.CommitExecution(t.Context(), p, func(ctx context.Context) error { return s.Put(ctx, []byte("rewind-feature"), []byte("one")) }, func(context.Context) error { return nil })
	require.NoError(t, err)
	marker, err := s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.NoError(t, err)
	require.Equal(t, p.Ref, marker.From)
	require.Equal(t, uint64(2), marker.Target.Generation)
	require.Equal(t, cfg.Anchor.Hash, marker.Target.Hash)
	again, err := s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.NoError(t, err)
	require.Equal(t, marker, again)
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.True(t, c.Rewinding)
	err = s.ReadSnapshot(t.Context(), func(context.Context) error { t.Error("rewind exposed partial state"); return nil })
	require.ErrorIs(t, err, port.ErrExecutionRewinding)
	part := firstExecutionPart(port.ExecutionBlocks, cfg)
	require.ErrorIs(t, s.WriteExecutionPart(t.Context(), part, func(context.Context) error { return nil }), port.ErrExecutionRewinding)
	require.NoError(t, s.Close())
	reopened := openTestStore(t, s.schema, false)
	recovered, err := reopened.ExecutionRewindStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, marker, recovered)
	reorg, err := reopened.ResumeExecutionRewind(t.Context(), nil, func(ctx context.Context, ref execwire.Ref) error { require.Equal(t, marker.Target, ref); return nil })
	require.NoError(t, err)
	require.NotNil(t, reorg)
	c, err = reopened.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.False(t, c.Rewinding)
	require.Equal(t, marker.Target, c.Visible)
	require.Equal(t, cfg.AnchorRoot, c.Root)
	for _, w := range port.ExecutionWriters() {
		cursor, err := reopened.ExecutionCursor(t.Context(), w)
		require.NoError(t, err)
		require.Equal(t, marker.Target, cursor.Ref)
		require.Zero(t, cursor.Digest)
	}
	_, err = reopened.Get(t.Context(), []byte("rewind-feature"))
	require.ErrorIs(t, err, port.ErrNotFound)
	_, err = reopened.ExecutionRewindStatus(t.Context())
	require.ErrorIs(t, err, port.ErrNotFound)
}
func TestExecutionRewindFreshFailureKeepsBlockedOriginalData(t *testing.T) {
	s, cfg, p := publicationFixture(t, 7)
	_, err := s.CommitExecution(t.Context(), p, func(ctx context.Context) error { return s.Put(ctx, []byte("rewind-feature"), []byte("one")) }, func(context.Context) error { return nil })
	require.NoError(t, err)
	marker, err := s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.NoError(t, err)
	failure := errors.New("fresh target forked")
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(ctx context.Context, ref execwire.Ref) error {
		c, err := s.ExecutionStatus(ctx)
		require.NoError(t, err)
		require.False(t, c.Rewinding)
		require.Equal(t, ref, c.Visible)
		_, err = s.Get(ctx, []byte("rewind-feature"))
		require.ErrorIs(t, err, port.ErrNotFound)
		return failure
	})
	require.ErrorIs(t, err, failure)
	current, err := s.ExecutionRewindStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, marker, current)
	value, err := s.Get(t.Context(), []byte("rewind-feature"))
	require.NoError(t, err)
	require.Equal(t, []byte("one"), value)
	var blocks int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM blocks").Scan(&blocks))
	require.Equal(t, 2, blocks, "failed final fresh rolls back every data change")
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
}
func TestExecutionRewindRejectsMissingUndoWrongRootAndLegacyBypass(t *testing.T) {
	s, cfg, p := publicationFixture(t, 7)
	_, err := s.CommitExecution(t.Context(), p, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	require.NoError(t, err)
	wrong := cfg.AnchorRoot
	wrong[0] ^= 1
	_, err = s.BeginExecutionRewind(t.Context(), cfg.Anchor, wrong)
	require.ErrorIs(t, err, port.ErrExecutionConflict)
	_, err = s.pool.Exec(t.Context(), "DELETE FROM undo_blocks WHERE height=1")
	require.NoError(t, err)
	_, err = s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.ErrorIs(t, err, port.ErrNoUndo)
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.False(t, c.Rewinding)
	_, err = s.RollbackTo(t.Context(), 0, nil)
	require.ErrorIs(t, err, port.ErrInvalidData, "execution cannot bypass its generation fence through legacy rollback")
}
func TestExecutionRewindPurgesUnpublishedCoreEvenWhenVisibleUnchanged(t *testing.T) {
	s, cfg, _ := publicationFixture(t, 7)
	// All first-height rows are durable, but visibility is still the anchor.
	marker, err := s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.NoError(t, err)
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	var count int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM blocks").Scan(&count))
	require.Equal(t, 1, count)
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, marker.Target, c.Visible)
	// Replayed work must use the new generation and extend the rewind base.
	p := firstExecutionPart(port.ExecutionBlocks, cfg)
	p.Ref.Generation = marker.Target.Generation
	p.Ref.Hash = execwire.Hash{10}
	b := &model.Block{Number: 1, Hash: common.Hash(p.Ref.Hash), ParentHash: common.Hash(cfg.Anchor.Hash)}
	require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error { return s.ApplyExecutionCore(ctx, p.Writer, b, nil) }))
}
