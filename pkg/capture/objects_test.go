package capture_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func objectArea(t *testing.T) (*window.Objects, window.ObjectOptions) {
	t.Helper()
	root := t.TempDir()
	o := window.ObjectOptions{Volume: window.VolumeSpec{Root: root, ID: "source-objects", AllowDirectory: true}, Path: filepath.Join(root, "permanent"), Chain: chain, MaxBytes: 32 << 20, MaxObjectBytes: 16 << 20}
	require.NoError(t, window.InitializeVolume(o.Volume))
	s, e := window.CreateObjects(o)
	require.NoError(t, e)
	return s, o
}
func normalized(in capture.Input) capture.Input {
	for _, k := range []capture.RowKind{capture.RowHeader, capture.RowBody, capture.RowTransaction, capture.RowReceipt, capture.RowLog} {
		for i := uint32(0); i < map[capture.RowKind]uint32{capture.RowHeader: 1, capture.RowBody: 1, capture.RowTransaction: 2, capture.RowReceipt: 2, capture.RowLog: 3}[k]; i++ {
			value, _ := json.Marshal(map[string]any{"height": in.Manifest.Identity.Block.Number, "hash": in.Manifest.Identity.Block.Hash, "index": i, "data": "'; DROP TABLE capture_records; --"})
			in.Rows = append(in.Rows, capture.Row{Kind: k, Index: i, Value: value})
		}
	}
	in.Normalized, _ = capture.DescribeRows(in.Rows)
	return in
}
func external(t *testing.T, s *window.Objects, in capture.Input) capture.Input {
	t.Helper()
	p := in.Payloads[0]
	r, e := s.Put(ctx, sha256.Sum256(p.Bytes), int64(len(p.Bytes)), bytes.NewReader(p.Bytes))
	require.NoError(t, e)
	in.Objects = []capture.ObjectPayload{{Kind: p.Kind, Ref: r}}
	in.Payloads = in.Payloads[1:]
	return in
}
func TestPermanentSourceStores(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			for name, test := range map[string]func(*testing.T, opener){"object-restart-gc": objectRestartGC, "retirement-retry": objectRetirement, "normalization": normalizedQueries, "object-corruption": objectCorruption, "object-atomicity": objectAtomicity} {
				t.Run(name, func(t *testing.T) { test(t, openFactory(t, backend)) })
			}
		})
	}
}
func objectRestartGC(t *testing.T, open opener) {
	db := open()
	o := newOracle()
	area, opts := objectArea(t)
	s, e := capture.NewWithObjects(db, chain, o, area)
	require.NoError(t, e)
	initialize(t, s)
	in := external(t, area, normalized(input(1, 1, common.Hash{2}, chain.GenesisHash)))
	require.NoError(t, s.Commit(ctx, in))
	require.NoError(t, s.Commit(ctx, in))
	require.NoError(t, area.Close())
	require.NoError(t, db.Close())
	db = open()
	defer func() { require.NoError(t, db.Close()) }()
	area, e = window.OpenObjects(opts)
	require.NoError(t, e)
	defer func() { require.NoError(t, area.Close()) }()
	s, e = capture.NewWithObjects(db, chain, o, area)
	require.NoError(t, e)
	got, e := s.Read(ctx, in.Manifest.Identity)
	require.NoError(t, e)
	require.Equal(t, in.Objects, got.Objects)
	var out bytes.Buffer
	require.NoError(t, s.CopyPayload(ctx, in.Manifest.Identity, in.Objects[0].Kind, &out))
	require.Equal(t, input(1, 1, common.Hash{2}, chain.GenesisHash).Payloads[0].Bytes, out.Bytes())
	_, e = service(t, db, o).Read(ctx, in.Manifest.Identity)
	require.ErrorIs(t, e, capture.ErrObjectsUnavailable)
	require.NoError(t, s.Finalize(ctx, 1, in.Manifest.Identity.Block))
	r := window.Reference{StoreID: common.Hash{8}, Segment: 1, Block: in.Manifest.Identity.Block, Kind: in.Objects[0].Kind, Codec: "fixture.v1", Digest: in.Objects[0].Ref.Digest, PayloadBytes: in.Objects[0].Ref.Size}
	require.NoError(t, s.WithDeletePermit(ctx, []window.Reference{r}, func() error { return nil }))
	_, n, e := s.CollectObjects(ctx, common.Hash{}, 256)
	require.NoError(t, e)
	require.Zero(t, n)
	// A reset never treats retained immutable fork objects as orphans.
	require.NoError(t, area.Verify(ctx, in.Objects[0].Ref))
	page, e := s.Rows(ctx, in.Manifest.Identity, capture.RowLog, 0, 2)
	require.NoError(t, e)
	require.Len(t, page.Rows, 2)
	require.False(t, page.Done)
	page, e = s.Rows(ctx, in.Manifest.Identity, capture.RowLog, page.Next, 2)
	require.NoError(t, e)
	require.Len(t, page.Rows, 1)
	require.True(t, page.Done)
	events, e := s.Events(ctx, 0, 256)
	require.NoError(t, e)
	require.Len(t, events, 1)
}
func objectRetirement(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	area, _ := objectArea(t)
	defer func() { require.NoError(t, area.Close()) }()
	s, e := capture.NewWithObjects(db, chain, newOracle(), area)
	require.NoError(t, e)
	initialize(t, s)
	in := external(t, area, input(1, 1, common.Hash{2}, chain.GenesisHash))
	r := in.Objects[0].Ref
	crash := errors.New("simulated death before unlink")
	require.ErrorIs(t, s.WithObjectDeletePermit(ctx, r, func() error { return crash }), crash)
	require.NoError(t, area.Verify(ctx, r))
	require.ErrorIs(t, s.Commit(ctx, in), capture.ErrObjectRetired)
	// Durable tombstone resumes cleanup even after a failed deletion callback.
	_, n, e := s.CollectObjects(ctx, common.Hash{}, 256)
	require.NoError(t, e)
	require.Equal(t, 1, n)
	require.Error(t, area.Verify(ctx, r))
	require.NoError(t, s.WithObjectDeletePermit(ctx, r, func() error { return nil }))
	// Same content can be uploaded with a new identity and committed.
	in = external(t, area, input(1, 1, common.Hash{2}, chain.GenesisHash))
	require.NotEqual(t, r.ID, in.Objects[0].Ref.ID)
	require.NoError(t, s.Commit(ctx, in))
	called := false
	require.ErrorIs(t, s.WithObjectDeletePermit(ctx, in.Objects[0].Ref, func() error { called = true; return nil }), window.ErrRetain)
	require.False(t, called)
}
func normalizedQueries(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	s := service(t, db, newOracle())
	initialize(t, s)
	in := normalized(input(1, 1, common.Hash{2}, chain.GenesisHash))
	bad := in
	bad.Rows = append([]capture.Row(nil), in.Rows...)
	bad.Rows[2].Value = json.RawMessage(`{"changed":true}`)
	require.ErrorIs(t, s.Commit(ctx, bad), capture.ErrConflict)
	st, e := s.State(ctx)
	require.NoError(t, e)
	require.Zero(t, st.Through.Number)
	require.NoError(t, s.Commit(ctx, in))
	require.NoError(t, s.Commit(ctx, in))
	for _, k := range []capture.RowKind{capture.RowHeader, capture.RowBody, capture.RowTransaction, capture.RowReceipt, capture.RowLog} {
		page, e := s.Rows(ctx, in.Manifest.Identity, k, 0, 256)
		require.NoError(t, e)
		require.True(t, page.Done)
		for _, row := range page.Rows {
			require.NotEmpty(t, row.Value)
		}
	}
	_, e = s.Rows(ctx, in.Manifest.Identity, capture.RowKind("log'; DROP TABLE capture_records"), 0, 1)
	require.ErrorIs(t, e, port.ErrInvalidData)
	_, e = s.Rows(ctx, in.Manifest.Identity, capture.RowLog, 4, 1)
	require.ErrorIs(t, e, port.ErrInvalidData)
	// No normalized rows are inferred for older encoded-only captures.
	in2 := input(2, 1, common.Hash{3}, common.Hash{2})
	require.NoError(t, s.Commit(ctx, in2))
	_, e = s.Rows(ctx, in2.Manifest.Identity, capture.RowLog, 0, 1)
	require.ErrorIs(t, e, model.ErrCaptureIncomplete)
	in3 := normalized(input(3, 1, common.Hash{4}, common.Hash{3}))
	require.NoError(t, s.Commit(ctx, in3))
	// Damage to a normalized row prevents duplicate acceptance and raw deletion.
	id := in.Manifest.Identity
	key := fmt.Sprintf("facts/%s/%s/%020d/rows/log/%010d", id.Block.Hash.Hex(), id.PlanDigest.Hex(), id.Generation, 0)
	require.NoError(t, db.CaptureUpdate(ctx, chain, func(tx port.CaptureWriter) error {
		other := in3.Manifest.Identity
		otherKey := fmt.Sprintf("facts/%s/%s/%020d/rows/log/%010d", other.Block.Hash.Hex(), other.PlanDigest.Hex(), other.Generation, 0)
		value, err := tx.Get([]byte(otherKey))
		if err != nil {
			return err
		}
		return tx.Put([]byte(key), value) // intact row/digest from the wrong block
	}))
	_, e = s.Rows(ctx, id, capture.RowLog, 0, 1)
	require.ErrorIs(t, e, capture.ErrConflict)
	require.ErrorIs(t, s.Commit(ctx, in), capture.ErrConflict)
	require.NoError(t, s.Finalize(ctx, 1, id.Block))
	r := window.Reference{StoreID: common.Hash{8}, Segment: 1, Block: id.Block, Kind: in.Payloads[0].Kind, Codec: "fixture.v1", Digest: sha256.Sum256(in.Payloads[0].Bytes), PayloadBytes: int64(len(in.Payloads[0].Bytes))}
	require.ErrorIs(t, s.WithDeletePermit(ctx, []window.Reference{r}, func() error { t.Fatal("deleted corrupt normalized facts"); return nil }), capture.ErrConflict)
}
func objectCorruption(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	area, opts := objectArea(t)
	defer func() { require.NoError(t, area.Close()) }()
	s, e := capture.NewWithObjects(db, chain, newOracle(), area)
	require.NoError(t, e)
	initialize(t, s)
	in := external(t, area, input(1, 1, common.Hash{2}, chain.GenesisHash))
	require.NoError(t, s.Commit(ctx, in))
	require.NoError(t, s.Finalize(ctx, 1, in.Manifest.Identity.Block))
	r := in.Objects[0].Ref
	name := filepath.Join(opts.Path, r.ID.Hex()[2:]+".object")
	f, e := os.OpenFile(name, os.O_RDWR, 0)
	require.NoError(t, e)
	_, e = f.WriteAt([]byte{0}, 4096)
	require.NoError(t, e)
	require.NoError(t, f.Close())
	_, e = s.Read(ctx, in.Manifest.Identity)
	require.ErrorIs(t, e, window.ErrCorrupt)
	ref := window.Reference{StoreID: common.Hash{8}, Segment: 1, Block: in.Manifest.Identity.Block, Kind: in.Objects[0].Kind, Codec: "fixture.v1", Digest: r.Digest, PayloadBytes: r.Size}
	require.ErrorIs(t, s.WithDeletePermit(ctx, []window.Reference{ref}, func() error { t.Fatal("deleted missing permanent bytes"); return nil }), window.ErrCorrupt)
	st, e := s.State(ctx)
	require.NoError(t, e)
	require.Equal(t, common.Hash{}, st.PendingDelete)
}

// Both transactions contend on the chain DB lock. Whichever wins, it is
// impossible to commit a live reference and successfully delete its object.
func TestObjectCommitCollectRace(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	area, _ := objectArea(t)
	defer func() { require.NoError(t, area.Close()) }()
	s, e := capture.NewWithObjects(db, chain, newOracle(), area)
	require.NoError(t, e)
	initialize(t, s)
	in := external(t, area, input(1, 1, common.Hash{2}, chain.GenesisHash))
	start := make(chan struct{})
	var commitErr, collectErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-start; commitErr = s.Commit(ctx, in) }()
	go func() { defer wg.Done(); <-start; collectErr = area.Collect(ctx, in.Objects[0].Ref, s) }()
	close(start)
	wg.Wait()
	if commitErr == nil {
		require.ErrorIs(t, collectErr, window.ErrRetain)
		require.NoError(t, area.Verify(ctx, in.Objects[0].Ref))
	} else {
		require.ErrorIs(t, commitErr, capture.ErrObjectRetired)
		require.NoError(t, collectErr)
		st, e := s.State(ctx)
		require.NoError(t, e)
		require.Zero(t, st.Through.Number)
	}
}

func objectAtomicity(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	area, _ := objectArea(t)
	defer func() { require.NoError(t, area.Close()) }()
	o := newOracle()
	s, e := capture.NewWithObjects(db, chain, o, area)
	require.NoError(t, e)
	initialize(t, s)
	in := external(t, area, normalized(input(1, 1, common.Hash{2}, chain.GenesisHash)))
	// External binding + inline value + 9 rows + manifest + canonical + event +
	// cursor: each write failure must roll back the entire publication.
	for at := 1; at <= 15; at++ {
		broken, e := capture.NewWithObjects(failDB{db, at}, chain, o, area)
		require.NoError(t, e)
		require.ErrorIs(t, broken.Commit(ctx, in), errInjected)
		st, e := s.State(ctx)
		require.NoError(t, e)
		require.Zero(t, st.Sequence)
		require.Zero(t, st.Through.Number)
		_, e = s.Rows(ctx, in.Manifest.Identity, capture.RowLog, 0, 1)
		require.ErrorIs(t, e, port.ErrNotFound)
	}
	// Losing a committed transaction's response must keep the object linked.
	uncertain, e := capture.NewWithObjects(uncertainDB{db}, chain, o, area)
	require.NoError(t, e)
	require.ErrorIs(t, uncertain.Commit(ctx, in), errInjected)
	require.NoError(t, s.Commit(ctx, in))
	_, n, e := s.CollectObjects(ctx, common.Hash{}, 256)
	require.NoError(t, e)
	require.Zero(t, n)
	// An uncertain retirement commit never invokes unlink. A normal handle can
	// observe its durable tombstone and resume later without a timeout bypass.
	orphan := external(t, area, input(2, 1, common.Hash{3}, common.Hash{2})).Objects[0].Ref
	called := false
	require.ErrorIs(t, uncertain.WithObjectDeletePermit(ctx, orphan, func() error { called = true; return nil }), errInjected)
	require.False(t, called)
	require.NoError(t, area.Verify(ctx, orphan))
	_, n, e = s.CollectObjects(ctx, common.Hash{}, 256)
	require.NoError(t, e)
	require.Equal(t, 1, n)
	// Replaced fork objects remain referenced even after a generation reset.
	in2 := external(t, area, input(2, 1, common.Hash{3}, common.Hash{2}))
	require.NoError(t, s.Commit(ctx, in2))
	require.NoError(t, s.Reset(ctx, 1, in.Manifest.Identity.Block))
	o.fork(2, common.Hash{99})
	replacement := external(t, area, input(2, 2, common.Hash{99}, common.Hash{2}))
	require.NoError(t, s.Commit(ctx, replacement))
	_, n, e = s.CollectObjects(ctx, common.Hash{}, 256)
	require.NoError(t, e)
	require.Zero(t, n)
	require.NoError(t, area.Verify(ctx, in2.Objects[0].Ref))
}

func TestObjectVolumeFaultStopsSession(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	area, o := objectArea(t)
	defer func() { require.NoError(t, area.Close()) }()
	raw := rawOptions(t.TempDir())
	require.NoError(t, window.InitializeVolume(raw.Volume))
	started := make(chan struct{}, 2)
	var joined atomic.Int32
	task := func(c context.Context, _ *window.Store, _ *capture.Store) error {
		started <- struct{}{}
		<-c.Done()
		joined.Add(1)
		return c.Err()
	}
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- capture.RunSession(runCtx, db, newOracle(), capture.SessionOptions{Window: raw, Objects: area, Plan: plan, Anchor: model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}, Create: true, UsageInterval: time.Millisecond}, task, task)
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case e := <-done:
			t.Fatalf("early session exit: %v", e)
		case <-runCtx.Done():
			t.Fatal(runCtx.Err())
		}
	}
	require.NoError(t, os.Rename(o.Path, o.Path+"-offline"))
	require.NoError(t, os.Mkdir(o.Path, 0700))
	select {
	case e := <-done:
		require.ErrorIs(t, e, window.ErrUnavailable)
	case <-runCtx.Done():
		t.Fatal(runCtx.Err())
	}
	require.EqualValues(t, 2, joined.Load(), "both tasks join before session returns")
	// Raw lock is released; permanent area's caller still owns its handle.
	w, e := window.Open(raw)
	require.NoError(t, e)
	require.NoError(t, w.Close())
}
