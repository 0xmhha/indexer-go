package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// One genuinely seven-part published archived block plus a synthetic complete
// ACK130 proves admission only. It makes no execution/consensus/throughput claim.
func rawDeletionFixture(t *testing.T) (*Store, retention.Permit, ingress.RawDeletion) {
	t.Helper()
	s, policy, part, a := ingressArchiveFixture(t)
	var refs []window.Reference
	for i := range a.Datasets {
		d := &a.Datasets[i]
		r := window.Reference{StoreID: common.Hash{3}, Segment: 1, Offset: int64(i) * 1000, FrameBytes: 1000, PayloadBytes: int64(len(d.Payload)), Digest: common.Hash(d.Digest), FirstReceived: 1, Block: model.FeedBlock{Chain: model.FeedChain{ID: policy.Chain.ID, GenesisHash: common.Hash(policy.Chain.Genesis)}, Number: part.Ref.Height, Hash: common.Hash(part.Ref.Hash)}, Kind: d.Kind, Codec: d.Codec}
		refs = append(refs, r)
		d.Reference, _ = json.Marshal(r)
	}
	p, e := publishRawDeletionFixture(t, s, policy, part, a, refs)
	return s, p, e
}

func publishRawDeletionFixture(t *testing.T, s *Store, policy port.ExecutionIngressPolicy, part port.ExecutionPart, a port.ExecutionIngressArchive, refs []window.Reference) (retention.Permit, ingress.RawDeletion) {
	t.Helper()
	b := &model.Block{Number: 1, Hash: common.Hash(part.Ref.Hash), ParentHash: common.Hash(part.Parent), StateRoot: common.Hash(policy.AnchorRoot)}
	for _, w := range port.ExecutionWriters() {
		p := part
		p.Writer = w
		require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error {
			if w != port.ExecutionBlocks {
				return nil
			}
			if err := s.ApplyExecutionCore(ctx, w, b, nil); err != nil {
				return err
			}
			return s.ApplyExecutionIngress(ctx, a)
		}))
	}
	_, err := s.CommitExecution(t.Context(), port.ExecutionPublication{Ref: part.Ref, Parent: part.Parent, Digest: part.Digest, Root: policy.AnchorRoot}, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	require.NoError(t, err)
	ack := execwire.Ref{Generation: 7, Height: 130, Hash: execwire.Hash{20}}
	digest, parent := execwire.Hash{23}, execwire.Hash{25}
	require.NoError(t, s.SetBlock(t.Context(), &model.Block{Number: ack.Height, Hash: common.Hash(ack.Hash), ParentHash: common.Hash(parent), StateRoot: common.Hash(policy.AnchorRoot)}))
	_, err = s.pool.Exec(t.Context(), "UPDATE exec_control SET generation=7,visible_height=$1,visible_hash=$2 WHERE id", int64(ack.Height), ack.Hash[:])
	require.NoError(t, err)
	_, err = s.pool.Exec(t.Context(), "UPDATE exec_cursors SET generation=7,height=$1,hash=$2,parent=$3,digest=$4", int64(ack.Height), ack.Hash[:], parent[:], digest[:])
	require.NoError(t, err)
	for _, w := range port.ExecutionWriters() {
		_, err = s.pool.Exec(t.Context(), "INSERT INTO exec_parts(writer,generation,height,hash,parent,digest) VALUES($1,7,$2,$3,$4,$5)", string(w), int64(ack.Height), ack.Hash[:], parent[:], digest[:])
		require.NoError(t, err)
	}
	_, err = s.pool.Exec(t.Context(), "INSERT INTO exec_publications(generation,height,hash,parent,digest,root) VALUES(7,$1,$2,$3,$4,$5)", int64(ack.Height), ack.Hash[:], parent[:], digest[:], policy.AnchorRoot[:])
	require.NoError(t, err)
	r := retention.Request{Version: 1, Chain: policy.Chain, Anchor: policy.Anchor, ACK: ack, Tail: ack, Head: execwire.Ref{Height: ack.Height, Hash: ack.Hash}, HeadRoot: policy.AnchorRoot, SourceDigest: execwire.Hash{21}, KeepBlocks: 129, Through: 1}
	p, err := s.BeginExecutionRetention(t.Context(), r, func(context.Context) error { return nil })
	require.NoError(t, err)
	record, err := window.DescribeDeletion(refs)
	require.NoError(t, err)
	e := ingress.RawDeletion{Version: 1, OwnerDigest: a.OwnerDigest, SourceDigest: r.SourceDigest, ACKDigest: digest, VolumeID: "fixture", NativeEpoch: 3, NativeReadEpoch: 12, RawEpoch: 19, Record: record, References: refs}
	require.NoError(t, e.Validate(p))
	return p, e
}

func TestExecutionRawDeletionCommitsAuthorizationBeforeCallbackAndRequiresExactCompletion(t *testing.T) {
	s, p, e := rawDeletionFixture(t)
	failure := errors.New("interrupted file deletion")
	var escaped context.Context
	err := s.WithExecutionRawDeletion(t.Context(), p, e, func(bound context.Context) error {
		escaped = bound
		require.NoError(t, s.CheckExecutionRawDeletion(bound, p, e, e.Record))
		got, err := s.ExecutionRawDeletion(t.Context(), execwire.Hash(e.Record.StoreID))
		require.NoError(t, err)
		require.Equal(t, e, got.Evidence)
		require.False(t, got.Completed, "authorization is visible on another pool connection before unlink")
		return failure
	})
	require.ErrorIs(t, err, failure)
	require.ErrorIs(t, s.CheckExecutionRawDeletion(escaped, p, e, e.Record), port.ErrClosed)
	pd, _ := retention.Digest(p)
	require.ErrorIs(t, s.CompleteExecutionRetention(t.Context(), p, retention.Receipt{Version: 1, PermitDigest: pd, Node: true, Spool: true}, func(context.Context) error { t.Fatal("pending raw deletion reached final fresh"); return nil }), port.ErrExecutionMaintenance)
	require.NoError(t, s.WithExecutionRawDeletion(t.Context(), p, e, func(bound context.Context) error { return s.CheckExecutionRawDeletion(bound, p, e, e.Record) }))
	got, err := s.ExecutionRawDeletion(t.Context(), execwire.Hash(e.Record.StoreID))
	require.NoError(t, err)
	require.True(t, got.Completed)
	require.NoError(t, s.CompleteExecutionRetention(t.Context(), p, retention.Receipt{Version: 1, PermitDigest: pd, Node: true, Spool: true}, func(context.Context) error { return nil }))
	require.Error(t, s.WithExecutionRawDeletion(t.Context(), p, e, func(context.Context) error { t.Fatal("completed permit reused"); return nil }))
	require.EqualValues(t, 4, s.pool.Config().MaxConns)
}

func TestExecutionRawDeletionPendingBlocksOtherMaintenance(t *testing.T) {
	s, p, e := rawDeletionFixture(t)
	failure := errors.New("raw files incomplete")
	require.ErrorIs(t, s.WithExecutionRawDeletion(t.Context(), p, e, func(context.Context) error { return failure }), failure)
	require.ErrorIs(t, s.WithExecutionRetention(t.Context(), p, func(context.Context) error {
		t.Fatal("other maintenance changed source before raw recovery")
		return nil
	}), port.ErrExecutionMaintenance)
}

func TestExecutionRawDeletionRefusesUnpublishedForkCorruptionAndWrongACK(t *testing.T) {
	for _, mode := range []string{"fork", "missing_part", "corrupt_payload", "wrong_ack", "zero_ack", "future_epoch", "changed_reference", "wrong_cursor_parent"} {
		t.Run(mode, func(t *testing.T) {
			s, p, e := rawDeletionFixture(t)
			var err error
			switch mode {
			case "fork":
				e.References[0].Block.Hash[0]++
				e.Record, err = window.DescribeDeletion(e.References)
			case "missing_part":
				_, err = s.pool.Exec(t.Context(), "DELETE FROM exec_parts WHERE height=1 AND writer=$1", string(port.ExecutionStorage))
			case "corrupt_payload":
				_, err = s.pool.Exec(t.Context(), "UPDATE exec_ingress_payloads SET body=$1", []byte("bad"))
			case "wrong_ack":
				e.ACKDigest[0]++
			case "zero_ack":
				e.ACKDigest = execwire.Hash{}
			case "future_epoch":
				e.NativeEpoch = 1
			case "wrong_cursor_parent":
				_, err = s.pool.Exec(t.Context(), "UPDATE exec_cursors SET parent=$1", []byte(string(make([]byte, 32))))
			case "changed_reference":
				e.References[0].FirstReceived++
				e.Record, err = window.DescribeDeletion(e.References)
			}
			require.NoError(t, err)
			err = s.WithExecutionRawDeletion(t.Context(), p, e, func(context.Context) error { t.Fatal("unproved bytes would be deleted"); return nil })
			require.Error(t, err)
		})
	}
}
