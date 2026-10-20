package opsinspect

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type memoryDB struct {
	values       map[string][]byte
	page         port.IndexingPage
	views, pages int
	onView       func(int)
	onPage       func(int)
	fail         error
}
type values map[string][]byte

func (v values) Get(k []byte) ([]byte, error) {
	b, ok := v[string(k)]
	if !ok {
		return nil, port.ErrNotFound
	}
	return bytes.Clone(b), nil
}
func (d *memoryDB) CaptureView(ctx context.Context, _ model.FeedChain, fn func(port.CaptureReader) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.fail != nil {
		return d.fail
	}
	d.views++
	if d.onView != nil {
		d.onView(d.views)
	}
	return fn(values(d.values))
}
func (d *memoryDB) CaptureUpdate(context.Context, model.FeedChain, func(port.CaptureWriter) error) error {
	panic("inspection attempted a write")
}
func (d *memoryDB) ReadIndexingPage(ctx context.Context, _ string, _ int) (port.IndexingPage, error) {
	if err := ctx.Err(); err != nil {
		return port.IndexingPage{}, err
	}
	d.pages++
	if d.onPage != nil {
		d.onPage(d.pages)
	}
	b, _ := json.Marshal(d.page)
	var copy port.IndexingPage
	err := json.Unmarshal(b, &copy)
	return copy, err
}
func put(t *testing.T, d *memoryDB, key string, v any) {
	t.Helper()
	b, e := json.Marshal(v)
	require.NoError(t, e)
	d.values[key] = b
}

func inspectionFixture(t *testing.T) (*memoryDB, window.Options, window.Reference, capture.State) {
	t.Helper()
	chain := model.FeedChain{ID: "5042002", GenesisHash: common.Hash{1}}
	root := t.TempDir()
	opts := window.Options{Volume: window.VolumeSpec{Root: root, ID: "fixture", AllowDirectory: true, MaxBytes: 8 << 20}, Path: filepath.Join(root, "raw"), Reservation: "arc", Chain: chain, MaxBytes: 2 << 20, SegmentBytes: 32 << 10, MaxPayloadBytes: 2048, MaxSegmentRecords: 8}
	require.NoError(t, window.InitializeVolume(opts.Volume))
	raw, e := window.Create(opts)
	require.NoError(t, e)
	block := model.FeedBlock{Chain: chain, Number: 1, Hash: common.Hash{2}}
	ref, e := raw.Append(context.Background(), chainfeed.Envelope{Block: block, Kind: model.DatasetBodies, Codec: "fixture.v1", Payload: []byte("fixture")})
	require.NoError(t, e)
	require.NoError(t, raw.Close())
	plan := model.CapturePlan{Version: 1, Required: []model.DatasetRequirement{{Kind: model.DatasetBodies, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}}}}
	anchor := model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}
	st := capture.State{Generation: 1, Anchor: anchor, Through: block, Plan: plan, RawConsumers: []string{"raw-a", "raw-b"}, Finalized: &block, FinalityPolicy: "fixture-authority"}
	d := &memoryDB{values: map[string][]byte{}, page: port.IndexingPage{Descriptor: &model.ProjectionDescriptor{Version: 1, Anchor: anchor, Plan: plan}, Units: []port.IndexingUnit{{Name: "fixture", State: port.FeatureState{Active: true, Gap: &port.BlockRange{From: 1, To: 1}}}}}}
	digest, e := plan.Digest()
	require.NoError(t, e)
	id := model.CaptureIdentity{Block: block, Generation: 1, PlanVersion: 1, PlanDigest: digest}
	put(t, d, "state", st)
	put(t, d, "canonical/00000000000000000001", id)
	put(t, d, "app/feed-window/v1", capture.WindowBinding{Volume: opts.Volume.ID, Root: root, Path: opts.Path, Reservation: opts.Reservation})
	d.values[fmt.Sprintf("ack/%s/%020d/%x", block.Hash.Hex(), 1, sha256.Sum256([]byte("raw-a")))] = []byte{1}
	return d, opts, ref, st
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[path] = fmt.Sprintf("%x/%o/%d", sha256.Sum256(b), info.Mode(), info.ModTime().UnixNano())
		}
		return nil
	}))
	return out
}

func TestInspectionCheckpointAndACKScopeReadOnly(t *testing.T) {
	d, opts, _, st := inspectionFixture(t)
	before := snapshotTree(t, opts.Volume.Root)
	r, err := Read(context.Background(), d, opts.Chain, opts, "", 100)
	require.NoError(t, err)
	require.Equal(t, st.Through, r.Checkpoint.State.Through)
	require.Equal(t, "through_only", r.Checkpoint.ACKScope)
	require.Equal(t, "acknowledged", r.Checkpoint.RawACKs[0].Status)
	require.Equal(t, "missing", r.Checkpoint.RawACKs[1].Status)
	require.Equal(t, "unknown", r.Raw.LeaseStatus)
	require.Equal(t, "not_checked", r.Raw.PayloadIntegrity)
	require.Equal(t, "not_authorized", r.GCDecision)
	require.True(t, r.UnitsPageComplete)
	require.Contains(t, r.Findings, "raw_ack_missing_in_observed_scope")
	require.Equal(t, &port.BlockRange{From: 1, To: 1}, r.Projection.Units[0].Gap)
	require.Nil(t, r.Projection.Units[0].Complete)
	require.Equal(t, before, snapshotTree(t, opts.Volume.Root))
}

func TestInspectionPendingDeletionDoesNotRecoverOrClearFence(t *testing.T) {
	for _, mode := range []string{"intent", "partial", "completed", "missing"} {
		t.Run(mode, func(t *testing.T) {
			d, opts, ref, st := inspectionFixture(t)
			st.PendingDelete = common.Hash{9}
			put(t, d, "state", st)
			put(t, d, "deletion_intent", capture.DeleteIntent{Token: st.PendingDelete, Generation: 1, References: []window.Reference{ref}})
			record, e := window.DescribeDeletion([]window.Reference{ref})
			require.NoError(t, e)
			b, e := json.Marshal(record)
			require.NoError(t, e)
			if mode == "intent" || mode == "partial" {
				require.NoError(t, os.WriteFile(filepath.Join(opts.Path, fmt.Sprintf("%020d.gc", ref.Segment)), b, 0600))
			}
			if mode == "partial" || mode == "completed" || mode == "missing" {
				require.NoError(t, os.Remove(filepath.Join(opts.Path, fmt.Sprintf("%020d.raw", ref.Segment))))
			}
			if mode == "completed" {
				require.NoError(t, os.WriteFile(filepath.Join(opts.Path, "GC.completed.json"), b, 0600))
			}
			before := snapshotTree(t, opts.Volume.Root)
			dbBefore := bytes.Clone(d.values["state"])
			r, e := Read(context.Background(), d, opts.Chain, opts, "", 100)
			require.NoError(t, e)
			require.Contains(t, r.Findings, "pending_deletion_requires_coordinated_recovery")
			require.Equal(t, "not_authorized", r.GCDecision)
			expect := map[string]string{"intent": "intent_matches", "partial": "intent_matches", "completed": "completion_matches", "missing": "no_matching_receipt"}[mode]
			require.Equal(t, expect, r.Raw.PendingMatch)
			require.Equal(t, before, snapshotTree(t, opts.Volume.Root))
			require.Equal(t, dbBefore, d.values["state"])
		})
	}
}

func TestInspectionRejectsConflictChangeCancellationAndIO(t *testing.T) {
	for _, kind := range []string{"bad_ack", "invalid_state", "missing_intent", "wrong_token", "wrong_binding", "raw_digest", "epoch_change", "page_change", "io", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			d, opts, ref, st := inspectionFixture(t)
			ctx := context.Background()
			switch kind {
			case "invalid_state":
				st.Generation = 0
				put(t, d, "state", st)
			case "bad_ack":
				d.values[fmt.Sprintf("ack/%s/%020d/%x", st.Through.Hash.Hex(), 1, sha256.Sum256([]byte("raw-a")))] = []byte{2}
			case "missing_intent":
				st.PendingDelete = common.Hash{9}
				put(t, d, "state", st)
			case "wrong_token":
				st.PendingDelete = common.Hash{9}
				put(t, d, "state", st)
				put(t, d, "deletion_intent", capture.DeleteIntent{Token: common.Hash{8}, Generation: 1, References: []window.Reference{ref}})
			case "wrong_binding":
				put(t, d, "app/feed-window/v1", capture.WindowBinding{Volume: "other", Root: opts.Volume.Root, Path: opts.Path, Reservation: opts.Reservation})
			case "raw_digest":
				st.PendingDelete = common.Hash{9}
				put(t, d, "state", st)
				put(t, d, "deletion_intent", capture.DeleteIntent{Token: st.PendingDelete, Generation: 1, References: []window.Reference{ref}})
				b, _ := json.Marshal(window.DeletionRecord{StoreID: ref.StoreID, Segment: ref.Segment, ReferencesHash: common.Hash{7}})
				require.NoError(t, os.WriteFile(filepath.Join(opts.Path, fmt.Sprintf("%020d.gc", ref.Segment)), b, 0600))
			case "epoch_change":
				d.onView = func(n int) {
					if n == 2 {
						st.Generation++
						put(t, d, "state", st)
					}
				}
			case "page_change":
				d.onPage = func(n int) {
					if n == 2 {
						d.page.Units[0].State.Active = false
					}
				}
			case "io":
				d.fail = errors.New("fixture I/O")
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			before := snapshotTree(t, opts.Volume.Root)
			r, e := Read(ctx, d, opts.Chain, opts, "", 100)
			require.Error(t, e)
			require.Zero(t, r.Version)
			require.Equal(t, before, snapshotTree(t, opts.Volume.Root))
		})
	}
}

func TestInspectionRawACKBoundAndMissingBinding(t *testing.T) {
	d, opts, ref, st := inspectionFixture(t)
	var refs []window.Reference
	digest, err := st.Plan.Digest()
	require.NoError(t, err)
	for n := uint64(1); n <= 18; n++ {
		block := model.FeedBlock{Chain: opts.Chain, Number: n, Hash: common.Hash{byte(n + 1)}}
		id := model.CaptureIdentity{Block: block, Generation: 1, PlanVersion: 1, PlanDigest: digest}
		put(t, d, fmt.Sprintf("canonical/%020d", n), id)
		r := ref
		r.Block, r.Offset = block, int64(n-1)*ref.FrameBytes
		refs = append(refs, r)
		st.Through = block
	}
	st.Finalized = &st.Through
	st.PendingDelete = common.Hash{9}
	put(t, d, "state", st)
	put(t, d, "deletion_intent", capture.DeleteIntent{Token: st.PendingDelete, Generation: 1, References: refs})
	delete(d.values, "app/feed-window/v1")
	snapshot, err := capture.ReadCheckpoint(context.Background(), d, opts.Chain)
	require.NoError(t, err)
	require.Nil(t, snapshot.Binding)
	require.True(t, snapshot.ACKTruncated)
	require.Len(t, snapshot.RawACKs, 32)
	require.Equal(t, 18, snapshot.PendingCount)
	require.Equal(t, "pending_deletion_blocks", snapshot.ACKScope)
}

func TestInspectionOrphanedIntentAndHistoricalCompletionAreNotApproval(t *testing.T) {
	d, opts, ref, _ := inspectionFixture(t)
	record, err := window.DescribeDeletion([]window.Reference{ref})
	require.NoError(t, err)
	b, err := json.Marshal(record)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(opts.Path, fmt.Sprintf("%020d.gc", ref.Segment)), b, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(opts.Path, "GC.completed.json"), b, 0600))
	before := snapshotTree(t, opts.Volume.Root)
	report, err := Read(context.Background(), d, opts.Chain, opts, "", 100)
	require.NoError(t, err)
	require.Contains(t, report.Findings, "raw_intent_without_db_fence")
	require.Equal(t, "not_pending", report.Raw.PendingMatch)
	require.Equal(t, "not_authorized", report.GCDecision)
	require.Equal(t, before, snapshotTree(t, opts.Volume.Root))
}

func TestInspectionPaginationAndUnavailableAreExplicit(t *testing.T) {
	d, opts, _, _ := inspectionFixture(t)
	d.page.Next = "fixture"
	r, e := Read(context.Background(), d, opts.Chain, opts, "", 1)
	require.NoError(t, e)
	require.False(t, r.UnitsPageComplete)
	require.Equal(t, "fixture", r.Projection.Next)
	require.Contains(t, r.Findings, "feature_page_incomplete")
	d.page.Descriptor = nil
	d.page.Next = ""
	r, e = Read(context.Background(), d, opts.Chain, opts, "fixture", 100)
	require.NoError(t, e)
	require.False(t, r.UnitsPageComplete)
	require.Contains(t, r.Findings, "projection_scope_unreported")
	require.Nil(t, r.Projection.Source)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, e = Read(ctx, d, opts.Chain, opts, "", 100)
	require.ErrorIs(t, e, context.DeadlineExceeded)
}
