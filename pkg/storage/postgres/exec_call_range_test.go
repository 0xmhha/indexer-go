package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func callRangeFixture(t *testing.T) (*Store, port.ExecutionConfig, execwire.Result) {
	s, cfg, b, rs := executionCoreFixture(t)
	w := port.ExecutionCallWriter(s)
	require.NoError(t, s.SeedExecution(t.Context(), port.ExecutionBaseline{Ref: cfg.Anchor, Root: cfg.AnchorRoot}))
	target := common.Address{2}
	b.Transactions[0].To = &target
	b.Transactions[0].Gas = 60000
	rs[0].GasUsed = 21000
	b.StateRoot = common.Hash{8}
	p := firstExecutionPart(port.ExecutionTransactions, cfg)
	to, value := execwire.Address(target), execwire.Word{31: 1}
	r := execwire.Result{Ref: p.Ref, Parent: p.Parent, Time: b.Time, StateRoot: execwire.Hash(b.StateRoot), ReceiptsRoot: execwire.Hash{4}, TxCount: 1, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}, Steps: []execwire.Step{{Cause: execwire.Transaction, TxHash: execwire.Hash(b.Transactions[0].Hash)}, {Ordinal: 1, Cause: execwire.Finalize, TxIndex: -1}}}
	root := execwire.CallFrame{TransactionHash: r.Steps[0].TxHash, Parent: -1, Type: "CALL", From: execwire.Address(b.Transactions[0].From), To: &to, Value: &value, Gas: 60000, GasUsed: 21000, Input: []byte{}, Output: []byte{}}
	child := root
	child.Parent = 0
	child.From = to
	child.Error = "reverted'); DROP TABLE blocks; --"
	child.Input = []byte("untrusted'); DROP TABLE blocks; --")
	r.CallTraces = &execwire.CallTraces{Version: 1, Profile: "evm", Frames: []execwire.CallFrame{root, child}}
	raw, e := execwire.MarshalResult(r)
	require.NoError(t, e)
	p.Digest = execwire.Hash(sha256.Sum256(raw))
	require.ErrorIs(t, w.ApplyExecutionCalls(t.Context(), r, b, rs), port.ErrClosed)
	for _, writer := range port.ExecutionWriters() {
		p.Writer = writer
		require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error {
			switch writer {
			case port.ExecutionBlocks, port.ExecutionReceipts, port.ExecutionLogs:
				return s.ApplyExecutionCore(ctx, writer, b, rs)
			case port.ExecutionTransactions:
				if e := s.ApplyExecutionCore(ctx, writer, b, rs); e != nil {
					return e
				}
				return w.ApplyExecutionCalls(ctx, r, b, rs)
			}
			return nil
		}))
	}

	_, e = s.CommitExecution(t.Context(), port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Digest: p.Digest, Root: r.StateRoot, CallTraces: true}, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	require.NoError(t, e)
	return s, cfg, r
}

func TestExecutionCallRangeCoverageOrphanRestart(t *testing.T) {
	s, cfg, r := callRangeFixture(t)
	reader, ok := any(s).(port.ExecutionCallRangeReader)
	require.True(t, ok, "bounded address query must be implemented")
	q := port.ExecutionCallRangeQuery{Address: r.CallTraces.Frames[0].From, FromBlock: 1, ToBlock: 2, Limit: 1, Selection: "canonical"}
	_, err := reader.ExecutionCallRange(t.Context(), q)
	require.ErrorIs(t, err, port.ErrExecutionCoverage, "strict coverage cannot pretend missing block 2 is empty")
	q.Partial = true
	page, err := reader.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.False(t, page.Complete)
	require.Equal(t, []port.ExecutionCallMissingRange{{FromBlock: 2, ToBlock: 2}}, page.Missing)
	require.Len(t, page.Calls, 1)
	require.Equal(t, r.Ref, page.Calls[0].Coverage.Ref)
	q.ToBlock = 1
	q.Partial = false
	page, err = reader.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.True(t, page.Complete)
	q.Address = execwire.Address{99}
	page, err = reader.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.True(t, page.Complete)
	require.Empty(t, page.Calls)
	q.Address = execwire.Address(*r.CallTraces.Frames[0].To)
	page, err = reader.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.Len(t, page.Calls, 1)
	require.NotEmpty(t, page.Next)
	q.Cursor = page.Next
	second, err := reader.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.Len(t, second.Calls, 1)
	require.EqualValues(t, 1, second.Calls[0].Call.Ordinal)
	require.Empty(t, second.Next)
	q.Address = execwire.Address{99}
	_, err = reader.ExecutionCallRange(t.Context(), q)
	require.ErrorIs(t, err, port.ErrExecutionStale)
	q.Address = execwire.Address(*r.CallTraces.Frames[0].To)
	_, err = s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.NoError(t, err)
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	_, err = reader.ExecutionCallRange(t.Context(), q)
	require.ErrorIs(t, err, port.ErrExecutionStale)
	q.Cursor = ""
	q.Selection = "orphan"
	q.OrphanEpoch = 1
	archived, err := reader.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.True(t, archived.Complete)
	require.Equal(t, r.Ref, archived.Calls[0].Coverage.Ref)
	require.EqualValues(t, 1, archived.Calls[0].Coverage.Epoch)
	require.NotEqual(t, execwire.Hash{}, archived.Calls[0].Coverage.Digest)
	require.NoError(t, s.Close())
	reopened := openTestStore(t, s.schema, false)
	recovered, err := any(reopened).(port.ExecutionCallRangeReader).ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.Equal(t, archived, recovered)
	q.Selection = "canonical"
	q.OrphanEpoch = 0
	_, err = any(reopened).(port.ExecutionCallRangeReader).ExecutionCallRange(t.Context(), q)
	require.ErrorIs(t, err, port.ErrExecutionCoverage)
	q.Selection = "orphan"
	q.OrphanEpoch = 1
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = any(reopened).(port.ExecutionCallRangeReader).ExecutionCallRange(ctx, q)
	require.ErrorIs(t, err, context.Canceled)
	// Archive data is authenticated and incomplete sets fail, rather than
	// returning an empty successful result after damage or restart.
	_, err = reopened.pool.Exec(t.Context(), "DELETE FROM exec_call_orphan_frames WHERE epoch=$1 AND height=$2 AND ordinal=$3", int64(1), int64(1), int32(1))
	require.NoError(t, err)
	_, err = any(reopened).(port.ExecutionCallRangeReader).ExecutionCallRange(t.Context(), q)
	require.Error(t, err)
	var fsync string
	require.NoError(t, reopened.pool.QueryRow(t.Context(), "SHOW fsync").Scan(&fsync))
	require.Equal(t, "on", fsync)
}

func TestExecutionCallRangeArchiveAtomicFailureAndEpoch(t *testing.T) {
	s, cfg, r := callRangeFixture(t)
	// Surviving prefix enters epoch 2 but retains generation 1 and its digest.
	_, err := s.BeginExecutionRewind(t.Context(), r.Ref, r.StateRoot)
	require.NoError(t, err)
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	q := port.ExecutionCallRangeQuery{Address: r.CallTraces.Frames[0].From, FromBlock: 1, ToBlock: 1, Limit: 10, Selection: "canonical"}
	original, err := s.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.EqualValues(t, 2, original.Calls[0].Coverage.Epoch)
	require.Equal(t, r.Ref, original.Calls[0].Coverage.Ref)
	cfg.Anchor.Generation = 2
	_, err = s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.NoError(t, err)
	failure := errors.New("after archive interruption")
	_, err = s.resumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil }, func(phase string) error {
		if phase == "call_archive" {
			return failure
		}
		return nil
	})
	require.ErrorIs(t, err, failure)
	var count int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM exec_call_orphan_blocks").Scan(&count))
	require.Zero(t, count, "failed transaction cannot publish archive")
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM exec_call_frames").Scan(&count))
	require.Equal(t, 2, count, "failed archive must preserve canonical bytes")
	require.NoError(t, s.Close())
	s = openTestStore(t, s.schema, false)
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	q.Selection = "orphan"
	q.OrphanEpoch = 2
	archived, err := s.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.Equal(t, original.Calls, archived.Calls)
	q.OrphanEpoch = 1
	_, err = s.ExecutionCallRange(t.Context(), q)
	require.ErrorIs(t, err, port.ErrExecutionCoverage, "original generation is not the public orphan epoch")
}

func TestExecutionCallRangeUncollectedArchiveAndBounds(t *testing.T) {
	s, cfg := emptyExecutionChain(t, 3)
	q := port.ExecutionCallRangeQuery{FromBlock: 0, ToBlock: 4, Limit: 2, Selection: "canonical", Partial: true}
	p, err := s.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.Empty(t, p.Calls)
	require.False(t, p.Complete)
	require.Equal(t, []port.ExecutionCallMissingRange{{FromBlock: 0, ToBlock: 4}}, p.Missing)
	require.NotEqual(t, execwire.Hash{}, p.Coverage[1].Digest, "known uncollected publications retain provenance")
	for _, bad := range []port.ExecutionCallRangeQuery{
		{FromBlock: 2, ToBlock: 1, Limit: 1, Selection: "canonical"}, {ToBlock: 128, Limit: 1, Selection: "canonical"}, {Limit: 0, Selection: "canonical"}, {Limit: 257, Selection: "canonical"}, {Limit: 1, Selection: "orphan"}, {Limit: 1, Selection: "canonical", OrphanEpoch: 1}, {Limit: 1, Selection: "both"}, {Limit: 1, Selection: "canonical", Cursor: "!"},
	} {
		_, err = s.ExecutionCallRange(t.Context(), bad)
		require.ErrorIs(t, err, port.ErrInvalidData)
	}
	_, err = s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.NoError(t, err)
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	q.Selection = "orphan"
	q.OrphanEpoch = 1
	p, err = s.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.False(t, p.Complete)
	require.Empty(t, p.Calls)
	require.EqualValues(t, 1, p.Coverage[1].Ref.Generation)
	q.Partial = false
	_, err = s.ExecutionCallRange(t.Context(), q)
	require.ErrorIs(t, err, port.ErrExecutionCoverage)
}

func TestExecutionCallRangeAcrossBlocksAndVisibilityFence(t *testing.T) {
	s, _, r := callRangeFixture(t)
	q := port.ExecutionCallRangeQuery{Address: *r.CallTraces.Frames[0].To, FromBlock: 1, ToBlock: 2, Limit: 1, Selection: "canonical", Partial: true}
	first, err := s.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.NotEmpty(t, first.Next)
	tx, _, err := s.GetTransaction(t.Context(), common.Hash(r.Steps[0].TxHash))
	require.NoError(t, err)
	tx.Hash = common.Hash{77}
	tx.BlockNumber = 2
	tx.BlockHash = common.Hash{3}
	b := &model.Block{Number: 2, Hash: tx.BlockHash, ParentHash: common.Hash(r.Ref.Hash), StateRoot: common.Hash(r.StateRoot), Time: 2, Transactions: []*model.Transaction{tx}}
	rs := []*model.Receipt{{BlockNumber: 2, BlockHash: b.Hash, TxHash: tx.Hash, TxIndex: 0, Status: 1, GasUsed: 21000}}
	next := r
	next.Ref = execwire.Ref{Generation: 1, Height: 2, Hash: execwire.Hash(b.Hash)}
	next.Parent = r.Ref.Hash
	next.Time = 2
	next.Steps = append([]execwire.Step(nil), r.Steps...)
	next.Steps[0].TxHash = execwire.Hash(tx.Hash)
	calls := *r.CallTraces
	calls.Frames = append([]execwire.CallFrame(nil), r.CallTraces.Frames...)
	for i := range calls.Frames {
		calls.Frames[i].TransactionHash = execwire.Hash(tx.Hash)
	}
	next.CallTraces = &calls
	raw, err := execwire.MarshalResult(next)
	require.NoError(t, err)
	digest := execwire.Hash(sha256.Sum256(raw))
	for _, w := range port.ExecutionWriters() {
		p := port.ExecutionPart{Writer: w, Ref: next.Ref, Parent: next.Parent, Digest: digest}
		require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error {
			switch w {
			case port.ExecutionBlocks, port.ExecutionReceipts, port.ExecutionLogs:
				return s.ApplyExecutionCore(ctx, w, b, rs)
			case port.ExecutionTransactions:
				if e := s.ApplyExecutionCore(ctx, w, b, rs); e != nil {
					return e
				}
				return s.ApplyExecutionCalls(ctx, next, b, rs)
			}
			return nil
		}))
	}
	_, err = s.CommitExecution(t.Context(), port.ExecutionPublication{Ref: next.Ref, Parent: next.Parent, Digest: digest, Root: next.StateRoot, CallTraces: true}, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	require.NoError(t, err)
	q.Cursor = first.Next
	_, err = s.ExecutionCallRange(t.Context(), q)
	require.ErrorIs(t, err, port.ErrExecutionStale, "newly visible coverage fences a partial cursor without a reorg")
	q.Cursor = ""
	q.Partial = false
	q.Limit = 2
	page, err := s.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.True(t, page.Complete)
	require.Len(t, page.Calls, 2)
	require.EqualValues(t, 1, page.Calls[1].Coverage.Height)
	require.NotEmpty(t, page.Next)
	q.Cursor = page.Next
	page, err = s.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.Len(t, page.Calls, 2)
	require.EqualValues(t, 2, page.Calls[0].Coverage.Height)
	require.EqualValues(t, 0, page.Calls[0].Call.Ordinal)
	require.Empty(t, page.Next)
}

func TestExecutionCallRangeCanonicalReplacementDoesNotRelabelOrphans(t *testing.T) {
	s, cfg, r := callRangeFixture(t)
	_, err := s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.NoError(t, err)
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	q := port.ExecutionCallRangeQuery{Address: r.CallTraces.Frames[0].From, FromBlock: 1, ToBlock: 1, Limit: 10, Selection: "orphan", OrphanEpoch: 1}
	before, err := s.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	ref := execwire.Ref{Generation: 2, Height: 1, Hash: execwire.Hash{33}}
	block := &model.Block{Number: 1, Hash: common.Hash(ref.Hash), ParentHash: common.Hash(cfg.Anchor.Hash), StateRoot: common.Hash{8}, Time: 2}
	replacement := execwire.Result{Ref: ref, Parent: cfg.Anchor.Hash, Time: 2, StateRoot: execwire.Hash(block.StateRoot), ReceiptsRoot: execwire.Hash{4}, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}, Steps: []execwire.Step{{Cause: execwire.Finalize, TxIndex: -1}}, CallTraces: &execwire.CallTraces{Version: 1, Profile: "evm", Frames: []execwire.CallFrame{}}}
	raw, err := execwire.MarshalResult(replacement)
	require.NoError(t, err)
	digest := execwire.Hash(sha256.Sum256(raw))
	for _, w := range port.ExecutionWriters() {
		part := port.ExecutionPart{Writer: w, Ref: ref, Parent: replacement.Parent, Digest: digest}
		require.NoError(t, s.WriteExecutionPart(t.Context(), part, func(ctx context.Context) error {
			switch w {
			case port.ExecutionBlocks, port.ExecutionReceipts, port.ExecutionLogs:
				return s.ApplyExecutionCore(ctx, w, block, nil)
			case port.ExecutionTransactions:
				if e := s.ApplyExecutionCore(ctx, w, block, nil); e != nil {
					return e
				}
				return s.ApplyExecutionCalls(ctx, replacement, block, nil)
			}
			return nil
		}))
	}
	_, err = s.CommitExecution(t.Context(), port.ExecutionPublication{Ref: ref, Parent: replacement.Parent, Digest: digest, Root: replacement.StateRoot, CallTraces: true}, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	require.NoError(t, err)
	after, err := s.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.Equal(t, before, after)
	q.Selection = "canonical"
	q.OrphanEpoch = 0
	canonical, err := s.ExecutionCallRange(t.Context(), q)
	require.NoError(t, err)
	require.True(t, canonical.Complete)
	require.Empty(t, canonical.Calls)
	require.Equal(t, ref, canonical.Coverage[0].Ref)
}
