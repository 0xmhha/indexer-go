package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type rawFilesGuard struct {
	s     *Store
	p     retention.Permit
	e     ingress.RawDeletion
	after error
	t     *testing.T
}

func TestExecutionRawDeletionCoversSplitSegmentsAndBoundsCompletion(t *testing.T) {
	s, p, e, raw, _ := rawFilesFixture(t)
	n, err := raw.Collect(t.Context(), rawFilesGuard{s: s, p: p, e: e, t: t})
	require.NoError(t, err)
	require.Equal(t, 3, n, "each raw dataset is in a different segment")
	var records int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM exec_raw_deletions").Scan(&records))
	require.Equal(t, 1, records, "keep one current completion, never an unbounded deletion history")
}
func (rawFilesGuard) WithDeletePermit(context.Context, []window.Reference, func() error) error {
	return window.ErrUnsafeGC
}
func (g rawFilesGuard) WithDeleteRecord(ctx context.Context, r window.DeletionRecord, refs []window.Reference, remove func() error) error {
	e := g.e
	e.Record = r
	e.References = refs
	return g.s.WithExecutionRawDeletion(ctx, g.p, e, func(bound context.Context) error {
		if err := g.s.CheckExecutionRawDeletion(bound, g.p, e, r); err != nil {
			return err
		}
		got, err := g.s.ExecutionRawDeletion(ctx, execwire.Hash(r.StoreID))
		require.NoError(g.t, err)
		require.False(g.t, got.Completed)
		if err := remove(); err != nil {
			return err
		}
		return g.after
	})
}

func rawFilesFixture(t *testing.T) (*Store, retention.Permit, ingress.RawDeletion, *window.Store, window.Options) {
	t.Helper()
	s, policy := ingressOwnerFixture(t)
	require.NoError(t, s.ConfigureExecutionIngress(t.Context(), &policy, false))
	clock := time.Now()
	root := t.TempDir()
	o := window.Options{Volume: window.VolumeSpec{Root: root, ID: "fixture", AllowDirectory: true}, Path: filepath.Join(root, "raw"), Chain: model.FeedChain{ID: policy.Chain.ID, GenesisHash: common.Hash(policy.Chain.Genesis)}, MaxBytes: 2 << 20, SegmentBytes: 128 << 10, MaxPayloadBytes: 2048, MaxSegmentRecords: 1, MaxSegments: 16, ReadBudgetBytes: 128 << 10, MaxReaders: 4, Now: func() time.Time { return clock }}
	require.NoError(t, window.InitializeVolume(o.Volume))
	raw, err := window.CreateOwned(o, "fixture.raw/v1", []byte(`{"Epoch":19}`))
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	rawOwner, err := raw.ReadOwner(t.Context())
	require.NoError(t, err)
	owner, err := s.ExecutionIngressOwner(t.Context())
	require.NoError(t, err)
	require.NoError(t, s.BindExecutionIngress(t.Context(), port.ExecutionIngressBinding{Version: 1, PolicyDigest: owner.PolicyDigest, NativeInstance: execwire.Hash{1}, BootstrapDigest: execwire.Hash{2}, RawStoreID: execwire.Hash(rawOwner.StoreID), SourcePolicyDigest: execwire.Hash{4}, NativeIdentity: []byte(`{"fixture":"raw-files"}`)}))
	owner, err = s.ExecutionIngressOwner(t.Context())
	require.NoError(t, err)
	part := port.ExecutionPart{Writer: port.ExecutionBlocks, Ref: execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}, Parent: policy.Anchor.Hash, Digest: execwire.Hash{3}}
	a := port.ExecutionIngressArchive{Version: 1, OwnerDigest: owner.BindingDigest, Ref: part.Ref, Parent: part.Parent, Digest: part.Digest, InputDigest: execwire.Hash{5}, NativeEpoch: 2, NativeReadEpoch: 9, NativeRequestID: 1, ObservedRawEpoch: 17, Provenance: []byte(`{"fixture":"immutable origin"}`)}
	var batch []chainfeed.Envelope
	for _, kind := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		batch = append(batch, chainfeed.Envelope{Block: model.FeedBlock{Chain: o.Chain, Number: 1, Hash: common.Hash(part.Ref.Hash)}, Kind: kind, Codec: "fixture.v1", Payload: []byte("original-" + string(kind))})
	}
	refs, err := raw.AppendBatch(t.Context(), batch)
	require.NoError(t, err)
	for i, r := range refs {
		reference, _ := json.Marshal(r)
		a.Datasets = append(a.Datasets, port.ExecutionIngressDataset{Kind: r.Kind, Codec: r.Codec, Reference: reference, Digest: execwire.Hash(sha256.Sum256(batch[i].Payload)), PayloadBytes: uint64(len(batch[i].Payload)), Payload: batch[i].Payload})
	}
	require.NoError(t, raw.Seal(t.Context()))
	p, e := publishRawDeletionFixture(t, s, policy, part, a, refs[:1])
	clock = clock.Add(25 * time.Hour)
	return s, p, e, raw, o
}

func TestExecutionRawDeletionRealFilesRecoverOnlyExactDurableReceipt(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "recover", true: "missing_receipt"}[missing], func(t *testing.T) {
			s, p, e, raw, o := rawFilesFixture(t)
			failure := errors.New("crash after durable file completion before SQL completion")
			n, err := raw.Collect(t.Context(), rawFilesGuard{s: s, p: p, e: e, after: failure, t: t})
			require.ErrorIs(t, err, failure)
			require.Equal(t, 1, n)
			ticket, err := s.ExecutionRawDeletion(t.Context(), execwire.Hash(e.Record.StoreID))
			require.NoError(t, err)
			require.False(t, ticket.Completed)
			require.NoError(t, raw.Close())
			if missing {
				require.NoError(t, os.Remove(filepath.Join(o.Path, "GC.completed.json")))
			}
			err = s.WithExecutionRawDeletion(t.Context(), p, e, func(bound context.Context) error {
				reopened, err := window.OpenOwnedAuthorized(o, "fixture.raw/v1", func(owner window.Owner, r window.DeletionRecord) error {
					if execwire.Hash(owner.StoreID) != execwire.Hash(e.Record.StoreID) {
						return port.ErrExecutionConflict
					}
					return s.CheckExecutionRawDeletion(bound, p, e, r)
				})
				if err != nil {
					return err
				}
				defer func() { _ = reopened.Close() }()
				return reopened.RecoverDeletion(bound, e.References, func() error { return s.CheckExecutionRawDeletion(bound, p, e, e.Record) }, func() error { return s.CheckExecutionRawDeletion(bound, p, e, e.Record) })
			})
			if missing {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			ticket, err = s.ExecutionRawDeletion(t.Context(), execwire.Hash(e.Record.StoreID))
			require.NoError(t, err)
			require.Equal(t, !missing, ticket.Completed)
		})
	}
}

func TestExecutionRawDeletionLiveLeaseBlocksReleaseAndJoinsCancellation(t *testing.T) {
	s, p, e := rawDeletionFixture(t)
	entered, release, done := make(chan int32, 1), make(chan struct{}), make(chan error, 1)
	releaseOnce := sync.OnceFunc(func() { close(release) })
	joined := false
	defer func() {
		releaseOnce()
		if !joined {
			<-done
		}
	}()
	go func() {
		done <- s.WithExecutionRawDeletion(t.Context(), p, e, func(bound context.Context) error {
			var pid int32
			if err := s.q(bound).QueryRow(bound, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
				return err
			}
			entered <- pid
			<-release
			return nil
		})
	}()
	var pid int32
	select {
	case pid = <-entered:
	case err := <-done:
		joined = true
		t.Fatalf("raw lease did not enter callback: %v", err)
	}
	pd, _ := retention.Digest(p)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	completed := make(chan error, 1)
	completeJoined := false
	defer func() {
		cancel()
		if !completeJoined {
			<-completed
		}
	}()
	go func() {
		completed <- s.CompleteExecutionRetention(ctx, p, retention.Receipt{Version: 1, PermitDigest: pd, Node: true, Spool: true}, func(context.Context) error { t.Error("release escaped live raw lease"); return nil })
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := s.pool.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::int=ANY(pg_blocking_pids(pid)))", pid).Scan(&blocked)
		return err == nil && blocked
	}, 3*time.Second, time.Millisecond)
	cancel()
	completeErr := <-completed
	completeJoined = true
	require.ErrorIs(t, completeErr, context.Canceled)
	releaseOnce()
	err := <-done
	joined = true
	require.NoError(t, err)
	got, err := s.ExecutionRawDeletion(t.Context(), execwire.Hash(e.Record.StoreID))
	require.NoError(t, err)
	require.True(t, got.Completed)
}
