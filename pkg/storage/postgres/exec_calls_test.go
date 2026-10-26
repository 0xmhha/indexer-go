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

func TestExecutionCallsDurabilityVisibilityPagesAndRewind(t *testing.T) {
	s, cfg, b, rs := executionCoreFixture(t)
	w, ok := any(s).(port.ExecutionCallWriter)
	require.True(t, ok, "transactions writer must durably consume same-replay calls")
	reader, ok := any(s).(port.ExecutionCallReader)
	require.True(t, ok, "visible calls must be queryable without RPC")
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
	query := port.ExecutionCallQuery{Height: 1, Limit: 1}
	_, e = reader.ExecutionCalls(t.Context(), query)
	require.ErrorIs(t, e, port.ErrNotFound, "durable parts are not public")
	pub := port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Digest: p.Digest, Root: r.StateRoot, CallTraces: true}
	failure := errors.New("fresh failed")
	_, e = s.CommitExecution(t.Context(), pub, func(context.Context) error { return nil }, func(context.Context) error { return failure })
	require.ErrorIs(t, e, failure)
	_, e = reader.ExecutionCalls(t.Context(), query)
	require.ErrorIs(t, e, port.ErrNotFound)
	fresh := 0
	_, e = s.CommitExecution(t.Context(), pub, func(context.Context) error { return nil }, func(ctx context.Context) error {
		fresh++
		_, e := reader.ExecutionCalls(ctx, query)
		require.NoError(t, e)
		_, e = reader.ExecutionCalls(t.Context(), query)
		require.ErrorIs(t, e, port.ErrNotFound)
		return nil
	})
	require.NoError(t, e)
	require.Equal(t, 1, fresh)
	page, e := reader.ExecutionCalls(t.Context(), query)
	require.NoError(t, e)
	require.EqualValues(t, 2, page.Total)
	require.Len(t, page.Frames, 1)
	require.EqualValues(t, 1, *page.Next)
	require.False(t, page.Frames[0].Internal)
	query.After = *page.Next
	_, e = reader.ExecutionCalls(t.Context(), query)
	require.ErrorIs(t, e, port.ErrInvalidData, "continuation must pin fork")
	query.Epoch = page.Epoch
	query.Hash = page.Ref.Hash
	page, e = reader.ExecutionCalls(t.Context(), query)
	require.NoError(t, e)
	require.Nil(t, page.Next)
	require.True(t, page.Frames[0].Internal)
	require.True(t, page.Frames[0].Reverted)
	require.False(t, page.Frames[0].AncestorReverted)
	require.Equal(t, []uint32{0}, page.Frames[0].Path)
	require.Equal(t, child.Error, page.Frames[0].Error)
	require.Equal(t, child.Input, page.Frames[0].Input)
	query = port.ExecutionCallQuery{Transaction: &root.TransactionHash, Limit: 10, InternalOnly: true}
	page, e = reader.ExecutionCalls(t.Context(), query)
	require.NoError(t, e)
	require.Len(t, page.Frames, 1)
	require.EqualValues(t, 1, page.Total)
	query.Epoch = page.Epoch + 1
	query.Hash = page.Ref.Hash
	_, e = reader.ExecutionCalls(t.Context(), query)
	require.ErrorIs(t, e, port.ErrExecutionStale)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, e = reader.ExecutionCalls(ctx, port.ExecutionCallQuery{Height: 1, Limit: 1})
	require.ErrorIs(t, e, context.Canceled)
	confirmed, e := s.ConfirmExecution(t.Context(), raw, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, e)
	require.True(t, confirmed, "exact lost-ACK payload must reuse durable trace rows")
	// A rewind to this very block preserves its original payload identity,
	// even though subsequent queries/ACKs use the new public generation.
	_, e = s.BeginExecutionRewind(t.Context(), r.Ref, r.StateRoot)
	require.NoError(t, e)
	_, e = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, e)
	r.Ref.Generation++
	raw, e = execwire.MarshalResult(r)
	require.NoError(t, e)
	confirmed, e = s.ConfirmExecution(t.Context(), raw, func(_ context.Context, ref execwire.Ref) error {
		require.Equal(t, r.Ref, ref, "fresh checks the incoming current generation")
		return nil
	})
	require.NoError(t, e)
	require.True(t, confirmed)
	page, e = reader.ExecutionCalls(t.Context(), port.ExecutionCallQuery{Height: 1, Limit: 10})
	require.NoError(t, e)
	require.Equal(t, r.Ref.Generation, page.Epoch)
	require.EqualValues(t, 1, page.Ref.Generation, "retained prefix keeps its original source generation")
	require.Equal(t, p.Digest, page.Digest, "new generation must not relabel the original payload")
	cfg.Anchor.Generation = r.Ref.Generation
	var saved, checksum []byte
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT data,row_digest FROM exec_call_frames WHERE height=$1 AND ordinal=$2", int64(1), int32(1)).Scan(&saved, &checksum))
	_, e = s.pool.Exec(t.Context(), "DELETE FROM exec_call_frames WHERE height=$1 AND ordinal=$2", int64(1), int32(1))
	require.NoError(t, e)
	_, e = reader.ExecutionCalls(t.Context(), port.ExecutionCallQuery{Height: 1, Limit: 1})
	require.Error(t, e, "a collected marker with missing frames is corruption, not an empty page")
	freshAfterCorruption := 0
	_, e = s.ConfirmExecution(t.Context(), raw, func(context.Context, execwire.Ref) error { freshAfterCorruption++; return nil })
	require.Error(t, e, "lost ACK must not authorize a missing trace set")
	require.Zero(t, freshAfterCorruption)
	_, e = s.pool.Exec(t.Context(), "INSERT INTO exec_call_frames(height,ordinal,tx_hash,internal,data,row_digest) VALUES($1,$2,$3,$4,$5,$6)", int64(1), int32(1), child.TransactionHash[:], true, saved, checksum)
	require.NoError(t, e)
	// The row count alone cannot certify durable trace data after bit damage.
	_, e = s.pool.Exec(t.Context(), "UPDATE exec_call_frames SET data=$1 WHERE height=$2 AND ordinal=$3", []byte("corrupt"), int64(1), int32(1))
	require.NoError(t, e)
	_, e = s.ConfirmExecution(t.Context(), raw, func(context.Context, execwire.Ref) error { return nil })
	require.Error(t, e, "ACK recovery must authenticate stored trace bytes, not only their count")
	_, e = s.pool.Exec(t.Context(), "UPDATE exec_call_frames SET data=$1 WHERE height=$2 AND ordinal=$3", saved, int64(1), int32(1))
	require.NoError(t, e)
	_, e = s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.NoError(t, e)
	_, e = reader.ExecutionCalls(t.Context(), port.ExecutionCallQuery{Height: 1, Limit: 1})
	require.ErrorIs(t, e, port.ErrExecutionRewinding)
	_, e = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, e)
	_, e = reader.ExecutionCalls(t.Context(), port.ExecutionCallQuery{Height: 1, Limit: 1, After: 1, Epoch: 1, Hash: r.Ref.Hash})
	require.ErrorIs(t, e, port.ErrExecutionStale, "old fork pin remains stale even when the new public tip is lower")
	_, e = reader.ExecutionCalls(t.Context(), port.ExecutionCallQuery{Height: 1, Limit: 1})
	require.ErrorIs(t, e, port.ErrNotFound)
	var count int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM exec_call_frames").Scan(&count))
	require.Zero(t, count, "rewind must remove unpublished and published abandoned calls")
}

func TestExecutionCallsEmptySetDiffersFromLegacyAbsence(t *testing.T) {
	for _, collected := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "collected empty"}[collected], func(t *testing.T) {
			s, cfg := executionFixture(t)
			require.NoError(t, s.InitExecution(t.Context(), cfg))
			require.NoError(t, s.SeedExecution(t.Context(), port.ExecutionBaseline{Ref: cfg.Anchor, Root: cfg.AnchorRoot}))
			p := firstExecutionPart(port.ExecutionBlocks, cfg)
			b := &model.Block{Number: p.Ref.Height, Hash: common.Hash(p.Ref.Hash), ParentHash: common.Hash(p.Parent), StateRoot: common.Hash{8}, Time: 1}
			r := execwire.Result{Ref: p.Ref, Parent: p.Parent, Time: 1, StateRoot: execwire.Hash(b.StateRoot), ReceiptsRoot: execwire.Hash{4}, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}, Steps: []execwire.Step{{Cause: execwire.Finalize, TxIndex: -1}}}
			if collected {
				r.CallTraces = &execwire.CallTraces{Version: 1, Profile: "evm", Frames: []execwire.CallFrame{}}
			}
			raw, e := execwire.MarshalResult(r)
			require.NoError(t, e)
			p.Digest = execwire.Hash(sha256.Sum256(raw))
			for _, w := range port.ExecutionWriters() {
				p.Writer = w
				require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error {
					switch w {
					case port.ExecutionBlocks, port.ExecutionReceipts, port.ExecutionLogs:
						return s.ApplyExecutionCore(ctx, w, b, nil)
					case port.ExecutionTransactions:
						if e := s.ApplyExecutionCore(ctx, w, b, nil); e != nil {
							return e
						}
						return s.ApplyExecutionCalls(ctx, r, b, nil)
					}
					return nil
				}))
			}
			_, e = s.CommitExecution(t.Context(), port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Root: r.StateRoot, Digest: p.Digest, CallTraces: collected}, func(context.Context) error { return nil }, func(context.Context) error { return nil })
			require.NoError(t, e)
			page, e := s.ExecutionCalls(t.Context(), port.ExecutionCallQuery{Height: 1, Limit: 10})
			rangePage, rangeErr := s.ExecutionCallRange(t.Context(), port.ExecutionCallRangeQuery{FromBlock: 1, ToBlock: 1, Limit: 10, Selection: "canonical"})
			if collected {
				require.NoError(t, rangeErr)
				require.True(t, rangePage.Complete)
				require.Empty(t, rangePage.Missing)
				require.Empty(t, rangePage.Calls)
			} else {
				require.ErrorIs(t, rangeErr, port.ErrExecutionCoverage)
			}

			if !collected {
				require.ErrorIs(t, e, port.ErrNotFound)
				return
			}
			require.NoError(t, e)
			require.Zero(t, page.Total)
			require.NotNil(t, page.Frames)
			require.Empty(t, page.Frames)
			require.Nil(t, page.Next)
		})
	}
}
