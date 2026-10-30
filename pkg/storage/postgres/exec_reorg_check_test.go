package postgres

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecutionReorgCheckFencesBeforeTargetSearchSurvivesRestartAndConvertsAtomically(t *testing.T) {
	s, cfg, p := publicationFixture(t, 7)
	_, err := s.CommitExecution(t.Context(), p, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	require.NoError(t, err)
	before, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	check, err := s.BeginExecutionReorgCheck(t.Context())
	require.NoError(t, err)
	require.Equal(t, port.ExecutionReorgCheck{Generation: before.Generation, From: before.Visible, Root: before.Root}, check)
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.True(t, c.ReorgPending)
	require.False(t, c.Rewinding)
	require.Equal(t, before.Generation, c.Generation, "pending search must not invent a generation or target")
	require.Equal(t, before.Visible, c.Visible)
	called := false
	require.ErrorIs(t, s.ReadSnapshot(t.Context(), func(context.Context) error { called = true; return nil }), port.ErrExecutionRewinding)
	require.False(t, called)
	_, err = s.CommitExecution(t.Context(), p, func(context.Context) error { called = true; return nil }, func(context.Context) error { called = true; return nil })
	require.ErrorIs(t, err, port.ErrExecutionRewinding)
	require.False(t, called)
	again, err := s.BeginExecutionReorgCheck(t.Context())
	require.NoError(t, err)
	require.Equal(t, check, again)
	require.NoError(t, s.Close())
	s = openTestStore(t, s.schema, false)
	retained, err := s.ExecutionReorgCheckStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, check, retained)
	wrong := cfg.Anchor
	wrong.Generation++
	_, err = s.BeginExecutionRewind(t.Context(), wrong, cfg.AnchorRoot)
	require.Error(t, err)
	retained, err = s.ExecutionReorgCheckStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, check, retained)
	marker, err := s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.NoError(t, err)
	require.Equal(t, before.Generation+1, marker.Target.Generation)
	_, err = s.ExecutionReorgCheckStatus(t.Context())
	require.ErrorIs(t, err, port.ErrNotFound)
	c, err = s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.False(t, c.ReorgPending)
	require.True(t, c.Rewinding)
	freshFailure := errors.New("new canonical target changed")
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return freshFailure })
	require.ErrorIs(t, err, freshFailure)
	c, err = s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.True(t, c.Rewinding)
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	require.NoError(t, s.ReadSnapshot(t.Context(), func(context.Context) error { return nil }))
}
func TestExecutionReorgCheckCorruptFenceCannotBecomeRewind(t *testing.T) {
	s, cfg, _ := publicationFixture(t, 7)
	check, err := s.BeginExecutionReorgCheck(t.Context())
	require.NoError(t, err)
	_, err = s.pool.Exec(t.Context(), "UPDATE exec_reorg_checks SET generation=$1 WHERE id", int64(check.Generation+1))
	require.NoError(t, err)
	_, err = s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.Error(t, err, "a pending search fence must exactly bind current control before replacement")
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.True(t, c.ReorgPending)
	require.False(t, c.Rewinding)
	require.Equal(t, check.Generation, c.Generation)
}
