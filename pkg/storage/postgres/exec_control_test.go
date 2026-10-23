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

func executionFixture(t *testing.T) (*Store, port.ExecutionConfig) {
	t.Helper()
	s := newTestStore(t)
	genesis, root := execwire.Hash{1}, execwire.Hash{9}
	cfg := port.ExecutionConfig{Chain: execwire.Chain{ID: "8283", Genesis: genesis}, Anchor: execwire.Ref{Generation: 1, Hash: genesis}, AnchorRoot: root, PartitionBlocks: 2}
	require.NoError(t, s.SetBlock(t.Context(), &model.Block{Number: 0, Hash: common.Hash(genesis), StateRoot: common.Hash(root)}))
	return s, cfg
}

func firstExecutionPart(writer port.ExecutionWriter, cfg port.ExecutionConfig) port.ExecutionPart {
	return port.ExecutionPart{Writer: writer, Ref: execwire.Ref{Generation: cfg.Anchor.Generation, Height: cfg.Anchor.Height + 1, Hash: execwire.Hash{2}}, Parent: cfg.Anchor.Hash, Digest: execwire.Hash{3}}
}

func TestExecutionInitializationIdentityAndPool(t *testing.T) {
	s, cfg := executionFixture(t)
	ctx := t.Context()
	_, err := s.ExecutionStatus(ctx)
	require.ErrorIs(t, err, port.ErrExecutionUninitialized)
	require.NoError(t, s.InitExecution(ctx, cfg))
	require.NoError(t, s.InitExecution(ctx, cfg))
	c, err := s.ExecutionStatus(ctx)
	require.NoError(t, err)
	require.Equal(t, cfg, c.ExecutionConfig)
	require.Equal(t, cfg.Anchor, c.Visible)
	require.Equal(t, cfg.AnchorRoot, c.Root)
	for _, w := range port.ExecutionWriters() {
		p, err := s.ExecutionCursor(ctx, w)
		require.NoError(t, err)
		require.Equal(t, cfg.Anchor, p.Ref)
	}
	wrong := cfg
	wrong.Chain.ID = "1"
	require.ErrorIs(t, s.InitExecution(ctx, wrong), port.ErrExecutionConflict)
	wrong = cfg
	wrong.PartitionBlocks++
	require.ErrorIs(t, s.InitExecution(ctx, wrong), port.ErrExecutionConflict)
	small, err := Open(ctx, Options{DSN: testDSN(t), Schema: newTestSchema(t), MaxConns: 2})
	require.NoError(t, err)
	defer func() { _ = small.Close() }()
	require.ErrorIs(t, small.InitExecution(ctx, cfg), port.ErrInvalidData)
	missing := newTestStore(t)
	require.ErrorIs(t, missing.InitExecution(ctx, cfg), port.ErrExecutionCoverage)
}

func TestExecutionPartDurabilityDoesNotPublish(t *testing.T) {
	s, cfg := executionFixture(t)
	ctx := t.Context()
	require.NoError(t, s.InitExecution(ctx, cfg))
	p := firstExecutionPart(port.ExecutionAccounts, cfg)
	calls := 0
	write := func(ctx context.Context) error {
		calls++
		return s.Put(ctx, []byte("unpublished-part"), []byte("durable"))
	}
	require.NoError(t, s.WriteExecutionPart(ctx, p, write))
	require.NoError(t, s.WriteExecutionPart(ctx, p, write))
	require.Equal(t, 1, calls)
	cursor, err := s.ExecutionCursor(ctx, p.Writer)
	require.NoError(t, err)
	require.Equal(t, p, cursor)
	parts, err := s.ExecutionParts(ctx, p.Ref.Height)
	require.NoError(t, err)
	require.Equal(t, []port.ExecutionPart{p}, parts)
	c, err := s.ExecutionStatus(ctx)
	require.NoError(t, err)
	require.Equal(t, cfg.Anchor, c.Visible, "one durable part cannot publish or ACK")
	value, err := s.Get(ctx, []byte("unpublished-part"))
	require.NoError(t, err)
	require.Equal(t, []byte("durable"), value)
	var logs int
	require.NoError(t, s.pool.QueryRow(ctx, "SELECT count(*) FROM undo_log").Scan(&logs))
	require.Zero(t, logs, "parts do not share the feature writer's undo log")
	conflict := p
	conflict.Digest[0]++
	require.ErrorIs(t, s.WriteExecutionPart(ctx, conflict, write), port.ErrExecutionConflict)
	require.Equal(t, 1, calls)
}

func TestExecutionPartSequenceRollbackAndCancellation(t *testing.T) {
	s, cfg := executionFixture(t)
	ctx := t.Context()
	require.NoError(t, s.InitExecution(ctx, cfg))
	p := firstExecutionPart(port.ExecutionStorage, cfg)
	failed := errors.New("part callback failed")
	require.ErrorIs(t, s.WriteExecutionPart(ctx, p, func(ctx context.Context) error {
		require.NoError(t, s.Put(ctx, []byte("must-rollback"), []byte("x")))
		return failed
	}), failed)
	_, err := s.Get(ctx, []byte("must-rollback"))
	require.ErrorIs(t, err, port.ErrNotFound)
	cursor, err := s.ExecutionCursor(ctx, p.Writer)
	require.NoError(t, err)
	require.Equal(t, cfg.Anchor, cursor.Ref)
	write := func(context.Context) error { return nil }
	gap := p
	gap.Ref.Height++
	require.ErrorIs(t, s.WriteExecutionPart(ctx, gap, write), port.ErrExecutionSequence)
	stale := p
	stale.Ref.Generation++
	require.ErrorIs(t, s.WriteExecutionPart(ctx, stale, write), port.ErrExecutionStale)
	badParent := p
	badParent.Parent[0]++
	require.ErrorIs(t, s.WriteExecutionPart(ctx, badParent, write), port.ErrExecutionSequence)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, s.WriteExecutionPart(cancelled, p, write), context.Canceled)
	require.NoError(t, s.WriteExecutionPart(ctx, p, write))
	p2 := p
	p2.Parent = p.Ref.Hash
	p2.Ref.Height++
	p2.Ref.Hash[0]++
	require.NoError(t, s.WriteExecutionPart(ctx, p2, write))
	require.NoError(t, s.WriteExecutionPart(ctx, p, write), "an exact retained earlier part is idempotent")
}

func TestExecutionPartRejectsInconsistentCursorProof(t *testing.T) {
	s, cfg := executionFixture(t)
	ctx := t.Context()
	require.NoError(t, s.InitExecution(ctx, cfg))
	p := firstExecutionPart(port.ExecutionAccounts, cfg)
	_, err := s.pool.Exec(ctx, "UPDATE exec_cursors SET generation=2 WHERE writer=$1", string(p.Writer))
	require.NoError(t, err)
	require.ErrorIs(t, s.WriteExecutionPart(ctx, p, func(context.Context) error { return nil }), port.ErrExecutionStale)
	_, err = s.pool.Exec(ctx, "UPDATE exec_cursors SET generation=1 WHERE writer=$1", string(p.Writer))
	require.NoError(t, err)
	require.NoError(t, s.WriteExecutionPart(ctx, p, func(context.Context) error { return nil }))
	_, err = s.pool.Exec(ctx, "UPDATE exec_cursors SET digest=$2 WHERE writer=$1", string(p.Writer), make([]byte, 32))
	require.NoError(t, err)
	require.ErrorIs(t, s.WriteExecutionPart(ctx, p, func(context.Context) error { return nil }), port.ErrExecutionConflict, "part receipt alone cannot hide an inconsistent latest cursor")
}
