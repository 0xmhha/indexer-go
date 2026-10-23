package spool

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
)

func fixture(t *testing.T) (Options, execwire.Result) {
	t.Helper()
	chain := execwire.Chain{ID: "8283", Genesis: execwire.Hash{1}}
	anchor := execwire.Ref{Generation: 1, Height: 0, Hash: chain.Genesis}
	return Options{Path: filepath.Join(t.TempDir(), "spool"), Chain: chain, Anchor: anchor, MaxBytes: 1 << 20, MaxRecords: 8}, execwire.Result{Ref: execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}, Parent: anchor.Hash, Time: 1, StateRoot: execwire.Hash{3}, ReceiptsRoot: execwire.Hash{4}, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}, Steps: []execwire.Step{{Ordinal: 0, Cause: execwire.Finalize, TxIndex: -1}}}
}
func opened(t *testing.T, o Options) *Store {
	t.Helper()
	s, err := Open(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func ack(o Options, e Entry) execwire.Frame {
	return execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: o.Chain, Ref: e.Ref, Digest: e.Digest}}
}
func nextResult(r execwire.Result) execwire.Result {
	r.Parent = r.Ref.Hash
	r.Ref.Height++
	r.Ref.Hash[0]++
	return r
}

func TestAppendDurabilityAndExactACK(t *testing.T) {
	o, r := fixture(t)
	s := opened(t, o)
	ctx := context.Background()
	events := []string{}
	fileSync, dirSync, rename := s.ops.syncFile, s.ops.syncDir, s.ops.rename
	s.ops.syncFile = func(f *os.File) error { events = append(events, "file-sync"); return fileSync(f) }
	s.ops.rename = func(a, b string) error { events = append(events, "rename"); return rename(a, b) }
	s.ops.syncDir = func(p string) error { events = append(events, "dir-sync"); return dirSync(p) }
	e, err := s.Append(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"file-sync", "rename", "dir-sync"}) {
		t.Fatalf("published before durable seal: %v", events)
	}
	got, err := s.Read(ctx, e.Ref)
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatalf("stored result differs: %v", err)
	}
	n, ok, err := s.Next(ctx)
	if err != nil || !ok || n != e {
		t.Fatalf("next: %v", err)
	}
	for _, mutate := range []func(*execwire.Frame){func(f *execwire.Frame) { f.Header.Ref.Hash[0]++ }, func(f *execwire.Frame) { f.Header.Ref.Generation++ }, func(f *execwire.Frame) { f.Header.Digest[0]++ }, func(f *execwire.Frame) { f.Header.Chain.ID = "8282" }} {
		f := ack(o, e)
		mutate(&f)
		if err := s.Acknowledge(ctx, f); err == nil {
			t.Fatal("wrong ACK accepted")
		}
		if _, err := s.Read(ctx, e.Ref); err != nil {
			t.Fatal("wrong ACK removed uncommitted result")
		}
	}
	if err := s.Acknowledge(ctx, ack(o, e)); err != nil {
		t.Fatal(err)
	}
	if err := s.Acknowledge(ctx, ack(o, e)); err != nil {
		t.Fatal("duplicate ACK was not idempotent")
	}
	st, err := s.Status(ctx)
	if err != nil || st.Pending != 0 || st.Committed != e.Ref || st.Digest != e.Digest {
		t.Fatalf("checkpoint: %+v %v", st, err)
	}
}

func TestLostACKReopenDuplicateAndSequence(t *testing.T) {
	o, r := fixture(t)
	ctx := context.Background()
	s := opened(t, o)
	e, err := s.Append(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.Status(ctx)
	duplicate, err := s.Append(ctx, r)
	if err != nil || duplicate != e {
		t.Fatalf("duplicate append: %v", err)
	}
	after, _ := s.Status(ctx)
	if before != after {
		t.Fatal("duplicate consumed more capacity")
	}
	conflict := r
	conflict.Time++
	if _, err := s.Append(ctx, conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed digest accepted: %v", err)
	}
	gap := nextResult(nextResult(r))
	if _, err := s.Append(ctx, gap); !errors.Is(err, ErrSequence) {
		t.Fatalf("gap accepted: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = opened(t, o)
	if got, err := s.Read(ctx, e.Ref); err != nil || !reflect.DeepEqual(got, r) {
		t.Fatalf("lost-ACK recovery: %v", err)
	}
	if err := s.Acknowledge(ctx, ack(o, e)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = opened(t, o)
	st, err := s.Status(ctx)
	if err != nil || st.Committed != e.Ref || st.Pending != 0 {
		t.Fatalf("durable ACK lost: %+v %v", st, err)
	}
}

func TestCapacityDoesNotDeleteUnacknowledgedResults(t *testing.T) {
	o, r := fixture(t)
	o.MaxBytes = 256 << 10
	r.BlockRLP = bytes.Repeat([]byte{0xc0}, 100<<10)
	s := opened(t, o)
	ctx := context.Background()
	e, err := s.Append(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, nextResult(r)); !errors.Is(err, ErrFull) {
		t.Fatalf("byte capacity ignored: %v", err)
	}
	if _, err := s.Read(ctx, e.Ref); err != nil {
		t.Fatal("capacity pressure deleted input")
	}
	o2, r2 := fixture(t)
	o2.MaxRecords = 1
	s2 := opened(t, o2)
	if _, err := s2.Append(ctx, r2); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Append(ctx, nextResult(r2)); !errors.Is(err, ErrFull) {
		t.Fatalf("record capacity ignored: %v", err)
	}
}

func TestOwnershipIdentitySymlinkAndCancellation(t *testing.T) {
	o, r := fixture(t)
	s := opened(t, o)
	ctx := context.Background()
	if second, err := Open(ctx, o); !errors.Is(err, ErrBusy) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("second owner: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Append(cancelled, r); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled append: %v", err)
	}
	st, _ := s.Status(ctx)
	if st.Pending != 0 {
		t.Fatal("cancelled append created input")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	wrong := o
	wrong.Chain.ID = "1"
	if store, err := Open(ctx, wrong); err == nil {
		if store != nil {
			_ = store.Close()
		}
		t.Fatal("foreign chain took spool")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(o.Path, link); err != nil {
		t.Fatal(err)
	}
	o.Path = link
	if store, err := Open(ctx, o); err == nil {
		if store != nil {
			_ = store.Close()
		}
		t.Fatal("symlink spool accepted")
	}
}

func TestFsyncFailureDoesNotPublishOrDelete(t *testing.T) {
	o, r := fixture(t)
	s := opened(t, o)
	ctx := context.Background()
	injected := errors.New("injected fsync failure")
	sync := s.ops.syncFile
	s.ops.syncFile = func(*os.File) error { return injected }
	if _, err := s.Append(ctx, r); !errors.Is(err, injected) {
		t.Fatalf("fsync failure: %v", err)
	}
	if _, ok, err := s.Next(ctx); err != nil || ok {
		t.Fatalf("unsealed result became visible: %v", err)
	}
	s.ops.syncFile = sync
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = opened(t, o)
	st, err := s.Status(ctx)
	if err != nil || st.Quarantined == 0 || st.Bytes == 0 {
		t.Fatalf("failed temporary data not retained/accounted: %+v %v", st, err)
	}
	if _, err := s.Append(ctx, r); err != nil {
		t.Fatal(err)
	}
}

func TestDurableACKReceiptRecoversInterruptedDeletion(t *testing.T) {
	o, r := fixture(t)
	s := opened(t, o)
	ctx := context.Background()
	e, err := s.Append(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	e2, err := s.Append(ctx, nextResult(r))
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected remove failure")
	s.ops.remove = func(string) error { return injected }
	if err := s.Acknowledge(ctx, ack(o, e)); !errors.Is(err, injected) {
		t.Fatalf("ACK cleanup failure: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = opened(t, o)
	st, err := s.Status(ctx)
	if err != nil || st.Committed != e.Ref || st.Pending != 1 {
		t.Fatalf("ACK receipt recovery: %+v %v", st, err)
	}
	if _, err := s.Read(ctx, e2.Ref); err != nil {
		t.Fatal("recovery removed a later unacknowledged block")
	}
}
