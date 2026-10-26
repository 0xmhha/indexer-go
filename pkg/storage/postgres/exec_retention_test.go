package postgres

import (
	"bytes"
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/stretchr/testify/require"
)

// The sealed prefix under collection is one genuinely published seven-part
// block. Later public/ACK markers are synthetic: this tests GC admission, not
// execution throughput or EVM/consensus validity.
func retentionFixture(t *testing.T) (*Store, retention.Request) {
	t.Helper()

	s := newTestStore(t)
	m, payload := bootstrapEmpty(t, false)
	_, err := s.BootstrapExecution(t.Context(), bytes.NewReader(payload), m, evm.New("evm"), func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	control, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	c := control.ExecutionConfig
	part := firstExecutionPart(port.ExecutionBlocks, c)
	block := &model.Block{Number: 1, Hash: common.Hash(part.Ref.Hash), ParentHash: common.Hash(part.Parent), StateRoot: common.Hash(m.Root)}
	for _, w := range port.ExecutionWriters() {
		part.Writer = w
		require.NoError(t, s.WriteExecutionPart(t.Context(), part, func(ctx context.Context) error {
			if w == port.ExecutionBlocks {
				return s.ApplyExecutionCore(ctx, w, block, nil)
			}
			return nil
		}))
	}
	p := port.ExecutionPublication{Ref: part.Ref, Parent: part.Parent, Digest: part.Digest, Root: m.Root}
	_, err = s.CommitExecution(t.Context(), p, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	require.NoError(t, err)
	ack := execwire.Ref{Generation: p.Ref.Generation, Height: p.Ref.Height + 129, Hash: execwire.Hash{20}}
	require.NoError(t, s.SetBlock(t.Context(), &model.Block{Number: ack.Height, Hash: common.Hash(ack.Hash), ParentHash: common.Hash(p.Ref.Hash), StateRoot: common.Hash(p.Root)}))
	_, err = s.pool.Exec(t.Context(), `UPDATE exec_control SET visible_height=$1,visible_hash=$2 WHERE id;
`, int64(ack.Height), ack.Hash[:])
	require.NoError(t, err)
	_, err = s.pool.Exec(t.Context(), `UPDATE exec_cursors SET height=$1,hash=$2`, int64(ack.Height), ack.Hash[:])
	require.NoError(t, err)
	_, err = s.pool.Exec(t.Context(), `INSERT INTO exec_publications(generation,height,hash,parent,digest,root) VALUES($1,$2,$3,$4,$5,$6)`, int64(ack.Generation), int64(ack.Height), ack.Hash[:], p.Ref.Hash[:], p.Digest[:], p.Root[:])
	require.NoError(t, err)
	return s, retention.Request{Version: 1, Chain: c.Chain, Anchor: c.Anchor, ACK: ack, Tail: ack, Head: execwire.Ref{Height: ack.Height, Hash: ack.Hash}, HeadRoot: p.Root, SourceDigest: execwire.Hash{21}, KeepBlocks: 129, Through: p.Ref.Height}
}

func TestExecutionRetentionDurablePermitRequiresFinalFresh(t *testing.T) {
	s, r := retentionFixture(t)
	calls := 0
	p, err := s.BeginExecutionRetention(t.Context(), r, func(context.Context) error { calls++; return nil })
	require.NoError(t, err, "a complete acknowledged prefix must acquire a durable permit")
	require.NoError(t, p.Validate())
	require.Equal(t, 1, calls)
}

func TestExecutionRetentionPendingFenceAndExactCompletion(t *testing.T) {
	s, r := retentionFixture(t)
	p, err := s.BeginExecutionRetention(t.Context(), r, func(context.Context) error { return nil })
	require.NoError(t, err)
	control, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.True(t, control.Maintenance)
	cfg := control.ExecutionConfig
	require.ErrorIs(t, s.InitExecution(t.Context(), cfg), port.ErrExecutionMaintenance)
	require.ErrorIs(t, s.ReadSnapshot(t.Context(), func(context.Context) error { t.Fatal("read admitted during GC"); return nil }), port.ErrExecutionMaintenance)
	part := port.ExecutionPart{Writer: port.ExecutionBlocks, Ref: execwire.Ref{Generation: control.Generation, Height: control.Visible.Height + 1, Hash: execwire.Hash{50}}, Parent: control.Visible.Hash, Digest: execwire.Hash{51}}
	require.ErrorIs(t, s.WriteExecutionPart(t.Context(), part, func(context.Context) error { t.Fatal("part admitted"); return nil }), port.ErrExecutionMaintenance)
	_, err = s.CommitExecution(t.Context(), port.ExecutionPublication{Ref: part.Ref, Parent: part.Parent, Digest: part.Digest, Root: execwire.Hash{52}}, func(context.Context) error { t.Fatal("publish admitted"); return nil }, func(context.Context) error { return nil })
	require.ErrorIs(t, err, port.ErrExecutionMaintenance)
	_, err = s.BeginExecutionRewind(t.Context(), control.Visible, control.Root)
	require.ErrorIs(t, err, port.ErrExecutionMaintenance)
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.ErrorIs(t, err, port.ErrExecutionMaintenance)
	again, err := s.BeginExecutionRetention(t.Context(), r, func(context.Context) error { return nil })
	require.NoError(t, err)
	require.Equal(t, p, again)
	digest, err := retention.Digest(p)
	require.NoError(t, err)
	receipt := retention.Receipt{Version: 1, PermitDigest: digest, Node: true, Spool: true}
	bad := receipt
	bad.Node = false
	require.ErrorIs(t, s.CompleteExecutionRetention(t.Context(), p, bad, func(context.Context) error { return nil }), retention.ErrUnsafe)
	failure := errors.New("canonical changed")
	require.ErrorIs(t, s.CompleteExecutionRetention(t.Context(), p, receipt, func(context.Context) error { return failure }), failure)
	control, err = s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.True(t, control.Maintenance, "failed fresh must retain durable fence")
	require.NoError(t, s.CompleteExecutionRetention(t.Context(), p, receipt, func(context.Context) error { return nil }))
	require.NoError(t, s.CompleteExecutionRetention(t.Context(), p, receipt, func(context.Context) error { return nil }), "exact completion retry")
	control, err = s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.False(t, control.Maintenance)
	require.NoError(t, s.ReadSnapshot(t.Context(), func(context.Context) error { return nil }))
}

func TestExecutionRetentionRejectedEvidenceAndFreshRollback(t *testing.T) {
	for _, kind := range []string{"missing_part", "wrong_ack", "ahead_writer", "rewind", "fresh", "post_fresh_generation", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			s, r := retentionFixture(t)
			validate := func(context.Context) error { return nil }
			switch kind {
			case "missing_part":
				_, err := s.pool.Exec(t.Context(), "DELETE FROM exec_parts WHERE writer='logs' AND height=$1", int64(r.Through))
				require.NoError(t, err)
			case "wrong_ack":
				r.ACK.Hash[0]++
				r.Tail = r.ACK
				r.Head.Hash = r.ACK.Hash
			case "ahead_writer":
				_, err := s.pool.Exec(t.Context(), "UPDATE exec_cursors SET height=height+1 WHERE writer='logs'")
				require.NoError(t, err)
			case "rewind":
				_, err := s.pool.Exec(t.Context(), "UPDATE exec_control SET rewinding=true WHERE id")
				require.NoError(t, err)
			case "fresh":
				validate = func(context.Context) error { return errors.New("fresh failed") }
			case "post_fresh_generation":
				validate = func(ctx context.Context) error {
					_, err := s.q(ctx).Exec(ctx, "UPDATE exec_control SET generation=generation+1 WHERE id")
					return err
				}
			case "cancel":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				_, err := s.BeginExecutionRetention(ctx, r, validate)
				require.ErrorIs(t, err, context.Canceled)
				return
			}
			p, err := s.BeginExecutionRetention(t.Context(), r, validate)
			require.Error(t, err)
			require.Empty(t, p)
			_, _, _, err = s.ExecutionRetentionStatus(t.Context())
			require.ErrorIs(t, err, port.ErrNotFound, "rejection cannot leave permission behind")
		})
	}
}

func TestExecutionRetentionDrainsReadLeaseAndCancel(t *testing.T) {
	s, r := retentionFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	reader := make(chan error, 1)
	go func() {
		reader <- s.ReadSnapshot(t.Context(), func(context.Context) error { close(entered); <-release; return nil })
	}()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	attempted := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(attempted)
		_, err := s.BeginExecutionRetention(ctx, r, func(context.Context) error { return nil })
		result <- err
	}()
	<-attempted
	require.Eventually(t, func() bool {
		var waiting bool
		e := s.pool.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND query LIKE '%pg_advisory_xact_lock(%')").Scan(&waiting)
		return e == nil && waiting
	}, 5*time.Second, time.Millisecond, "retention must actually wait for the held read lease before cancellation")
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	_, _, _, err := s.ExecutionRetentionStatus(t.Context())
	require.ErrorIs(t, err, port.ErrNotFound)
	close(release)
	require.NoError(t, <-reader)
	p, err := s.BeginExecutionRetention(t.Context(), r, func(context.Context) error { return nil })
	require.NoError(t, err)
	// Reopening a distinct pool observes the persisted gate; a connection lock
	// on the original store is not the source of this refusal.
	reopened := openTestStore(t, s.schema, false)
	require.ErrorIs(t, reopened.ReadSnapshot(t.Context(), func(context.Context) error { t.Fatal("restored pool escaped GC fence"); return nil }), port.ErrExecutionMaintenance)
	got, pending, _, err := reopened.ExecutionRetentionStatus(t.Context())
	require.NoError(t, err)
	require.True(t, pending)
	require.Equal(t, p, got)
	leaseEntered, leaseRelease := make(chan struct{}), make(chan struct{})
	leaseDone := make(chan error, 1)
	go func() {
		leaseDone <- s.WithExecutionRetention(t.Context(), p, func(context.Context) error { close(leaseEntered); <-leaseRelease; return nil })
	}()
	<-leaseEntered
	pd, err := retention.Digest(p)
	require.NoError(t, err)
	completeCtx, completeCancel := context.WithCancel(t.Context())
	completeDone := make(chan error, 1)
	go func() {
		completeDone <- s.CompleteExecutionRetention(completeCtx, p, retention.Receipt{Version: 1, PermitDigest: pd, Node: true, Spool: true}, func(context.Context) error { t.Error("completion escaped live producer lease"); return nil })
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := s.pool.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND query LIKE '%pg_advisory_xact_lock(%')").Scan(&waiting)
		return e == nil && waiting
	}, 5*time.Second, time.Millisecond)
	completeCancel()
	require.ErrorIs(t, <-completeDone, context.Canceled)
	close(leaseRelease)
	require.NoError(t, <-leaseDone)
	_, pending, _, err = reopened.ExecutionRetentionStatus(t.Context())
	require.NoError(t, err)
	require.True(t, pending)
}

func TestExecutionRetentionRefusesPartialOrUnverifiedBaseline(t *testing.T) {
	for _, kind := range []string{"partial", "missing_digest"} {
		t.Run(kind, func(t *testing.T) {
			s, r := retentionFixture(t)
			query := "UPDATE exec_control SET coverage='partial' WHERE id"
			if kind == "missing_digest" {
				query = "UPDATE exec_control SET bootstrap_digest=NULL WHERE id"
			}
			_, err := s.pool.Exec(t.Context(), query)
			require.NoError(t, err)
			_, err = s.BeginExecutionRetention(t.Context(), r, func(context.Context) error { return nil })
			require.ErrorIs(t, err, port.ErrExecutionCoverage, "unproven full archive cannot authorize removal")
		})
	}
}

func TestExecutionRetentionSupervisorRequiresLivePendingPermit(t *testing.T) {
	s, r := retentionFixture(t)
	p, err := s.BeginExecutionRetention(t.Context(), r, func(context.Context) error { return nil })
	require.NoError(t, err)
	called := 0
	require.NoError(t, s.WithExecutionRetention(t.Context(), p, func(context.Context) error { called++; return nil }))
	require.Equal(t, 1, called)
	pd, err := retention.Digest(p)
	require.NoError(t, err)
	receipt := retention.Receipt{Version: 1, PermitDigest: pd, Node: true, Spool: true}
	require.NoError(t, s.CompleteExecutionRetention(t.Context(), p, receipt, func(context.Context) error { return nil }))
	require.Error(t, s.WithExecutionRetention(t.Context(), p, func(context.Context) error { called++; return nil }), "a completed artifact is never a live deletion lease")
	require.Equal(t, 1, called)
}

func TestExecutionMatchedBackupRequiresExactLivePermitPublicationAndFinalFresh(t *testing.T) {
	s, request := retentionFixture(t)
	p, err := s.BeginExecutionRetention(t.Context(), request, func(context.Context) error { return nil })
	require.NoError(t, err)
	pd, err := retention.Digest(p)
	require.NoError(t, err)
	r := retention.IndexerBackup{Version: 1, PermitDigest: pd, ArtifactDigest: execwire.Hash{3}, SnapshotIdentity: "external-authenticated-snapshot"}
	var raw []byte
	require.NoError(t, s.pool.QueryRow(t.Context(), `SELECT current_database() || '/' || current_schema(),pg_current_wal_lsn()::text,digest FROM exec_publications WHERE height=$1 AND hash=$2`, int64(p.Visible.Height), p.Visible.Hash[:]).Scan(&r.DatabaseIdentity, &r.WALBoundary, &raw))
	copy(r.PublicationDigest[:], raw)
	require.NoError(t, s.WithExecutionRetention(t.Context(), p, func(bound context.Context) error { return s.ValidateExecutionBackup(bound, p, r) }))
	for _, field := range []string{"database", "publication", "permit", "wal"} {
		t.Run(field, func(t *testing.T) {
			bad := r
			switch field {
			case "database":
				bad.DatabaseIdentity += "foreign"
			case "publication":
				bad.PublicationDigest[0]++
			case "permit":
				bad.PermitDigest[0]++
			case "wal":
				bad.WALBoundary = "FFFFFFFF/FFFFFFFF"
			}
			require.Error(t, s.WithExecutionRetention(t.Context(), p, func(bound context.Context) error { return s.ValidateExecutionBackup(bound, p, bad) }))
		})
	}
	calls := 0
	require.NoError(t, s.CheckExecutionRestore(t.Context(), p, func(context.Context) error { calls++; return nil }))
	require.Equal(t, 1, calls)
	failure := errors.New("fresh changed")
	require.ErrorIs(t, s.CheckExecutionRestore(t.Context(), p, func(context.Context) error { return failure }), failure)
	require.Error(t, s.CheckExecutionRestore(t.Context(), p, func(bound context.Context) error {
		_, e := s.q(bound).Exec(bound, "UPDATE exec_control SET generation=generation+1 WHERE id")
		return e
	}))
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, p.Visible.Generation, c.Generation)
	require.True(t, c.Maintenance)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, s.CheckExecutionRestore(ctx, p, func(context.Context) error { t.Fatal("cancelled restore read"); return nil }), context.Canceled)
	require.LessOrEqual(t, s.pool.Stat().TotalConns(), int32(4))
}
