package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func executionRewindSnapshot(t *testing.T, s *Store) map[string]string {
	t.Helper()
	out := snapshot(t, s)
	for _, table := range []string{"exec_call_blocks", "exec_call_frames", "exec_call_orphan_blocks", "exec_call_orphan_frames", "exec_control", "exec_rewinds", "exec_cursors", "exec_parts", "exec_publications", "state_accounts", "state_account_changes", "state_storage", "state_storage_roots", "state_storage_changes", "state_storage_clears", "state_contracts", "state_contract_changes", "state_code", "outbox", "outbox_sequence", "undo_log", "undo_blocks", "reorgs", "orphaned_blocks", "orphaned_transactions"} {
		var data string
		require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM "+pgx.Identifier{table}.Sanitize()+" t").Scan(&data))
		out[table] = data
	}
	return out
}
func TestExecutionRewindEveryMutationPhaseIsAtomicAndRestartable(t *testing.T) {
	for _, stage := range []string{"call_archive", "feature_undo", "state_restore", "core_purge", "cursor_reset", "fresh"} {
		t.Run(stage, func(t *testing.T) {
			s, cfg, _, _, _ := rewindStateFixture(t)
			_, err := s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
			require.NoError(t, err)
			before := executionRewindSnapshot(t, s)
			failure := errors.New("phase interrupted")
			seen := false
			_, err = s.resumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error {
				if stage == "fresh" {
					seen = true
					return failure
				}
				return nil
			}, func(current string) error {
				if current == stage {
					seen = true
					return failure
				}
				return nil
			})
			require.ErrorIs(t, err, failure)
			require.True(t, seen)
			require.Equal(t, before, executionRewindSnapshot(t, s))
			require.NoError(t, s.Close())
			reopened := openTestStore(t, s.schema, false)
			_, err = reopened.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
			require.NoError(t, err)
			c, err := reopened.ExecutionStatus(t.Context())
			require.NoError(t, err)
			require.False(t, c.Rewinding)
			require.Equal(t, cfg.Anchor.Height, c.Visible.Height)
		})
	}
}
func emptyExecutionChain(t *testing.T, count uint64) (*Store, port.ExecutionConfig) {
	t.Helper()
	s, cfg := executionFixture(t)
	require.NoError(t, s.InitExecution(t.Context(), cfg))
	require.NoError(t, s.SeedExecution(t.Context(), port.ExecutionBaseline{Ref: cfg.Anchor, Root: cfg.AnchorRoot}))
	for h := uint64(1); h <= count; h++ {
		ref := execwire.Ref{Generation: 1, Height: h, Hash: execwire.Hash{byte(h + 1)}}
		root := execwire.Hash{byte(h + 10)}
		parent := execwire.Hash{byte(h)}
		digest := execwire.Hash{byte(h + 2)}
		b := &model.Block{Number: h, Hash: common.Hash(ref.Hash), ParentHash: common.Hash(parent), StateRoot: common.Hash(root)}
		for _, w := range port.ExecutionWriters() {
			p := port.ExecutionPart{Writer: w, Ref: ref, Parent: parent, Digest: digest}
			require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error {
				if w == port.ExecutionBlocks {
					return s.ApplyExecutionCore(ctx, w, b, nil)
				}
				return nil
			}))
		}
		_, err := s.CommitExecution(t.Context(), port.ExecutionPublication{Ref: ref, Parent: parent, Digest: digest, Root: root}, func(context.Context) error { return nil }, func(context.Context) error { return nil })
		require.NoError(t, err)
	}
	return s, cfg
}
func TestExecutionRewindExactly128AndReject129(t *testing.T) {
	s, cfg := emptyExecutionChain(t, 129)
	_, err := s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.ErrorIs(t, err, port.ErrNoUndo)
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.False(t, c.Rewinding)
	target := execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}
	marker, err := s.BeginExecutionRewind(t.Context(), target, execwire.Hash{11})
	require.NoError(t, err)
	reorg, err := s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	require.Len(t, reorg.Removed, 128)
	c, err = s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, marker.Target, c.Visible)
}
func TestExecutionRewindRetargetsOnlyDeeperAndFencesOldRequests(t *testing.T) {
	s, _ := emptyExecutionChain(t, 3)
	target := execwire.Ref{Generation: 1, Height: 2, Hash: execwire.Hash{3}}
	first, err := s.BeginExecutionRewind(t.Context(), target, execwire.Hash{12})
	require.NoError(t, err)
	lower := execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}
	_, err = s.BeginExecutionRewind(t.Context(), lower, execwire.Hash{11})
	require.ErrorIs(t, err, port.ErrExecutionStale)
	lower.Generation = first.Target.Generation
	next, err := s.BeginExecutionRewind(t.Context(), lower, execwire.Hash{11})
	require.NoError(t, err)
	require.Equal(t, uint64(3), next.Target.Generation)
	require.Equal(t, first.From, next.From)
	duplicate, err := s.BeginExecutionRewind(t.Context(), lower, execwire.Hash{11})
	require.NoError(t, err)
	require.Equal(t, next, duplicate)
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
}
