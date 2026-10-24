package spool

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func retentionSpool(t *testing.T) (Options, *Store, retention.Permit) {
	t.Helper()
	o, r := fixture(t)
	o.MaxRecords = 64
	o.MaxBytes = 4 << 20
	s := opened(t, o)
	old := r
	for range 4 {
		if _, err := s.Append(t.Context(), old); err != nil {
			t.Fatal(err)
		}
		old = nextResult(old)
	}
	base := o.Anchor
	base.Generation = 2
	if err := s.Rebind(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	// A sealed recovered checkpoint fixture avoids replaying 130 fsync ACKs in
	// every fault/concurrency case. Exact ACK durability is covered separately.
	cp := execwire.Ref{Generation: 2, Height: 130, Hash: execwire.Hash{40}}
	s.state.Base = &cp
	s.state.Committed = cp
	s.state.Digest = execwire.Hash{}
	s.state.History = nil
	if err := s.saveState(t.Context()); err != nil {
		t.Fatal(err)
	}
	head := cp
	head.Generation = 0
	request, err := s.RetentionRequest(t.Context(), head, execwire.Hash{99}, 129)
	if err != nil {
		t.Fatal("acknowledged old quarantine must become collectable:", err)
	}
	return o, s, retention.Permit{Coverage: "full", BaselineDigest: execwire.Hash{102}, Version: 1, Token: execwire.Hash{77}, Request: request, Visible: cp, Root: execwire.Hash{99}}
}

func TestRetentionCollectPreservesWindowAndRequiresNodeCompletion(t *testing.T) {
	o, s, p := retentionSpool(t)
	failure := errors.New("node still leased")
	if _, err := s.CollectRetention(t.Context(), p, func(context.Context) error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	old := execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}
	path := filepath.Join(o.Path, ".quarantine-"+recordName(old))
	if _, err := os.Stat(path); err != nil {
		t.Fatal("failed node permit removed raw:", err)
	}
	receipt, err := s.CollectRetention(t.Context(), p, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Match(p) != nil || receipt.Files != 1 {
		t.Fatal("wrong completion", receipt)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expired quarantine remains", err)
	}
	protected := old
	protected.Height = 2
	protected.Hash[0]++
	if _, err = os.Stat(filepath.Join(o.Path, ".quarantine-"+recordName(protected))); err != nil {
		t.Fatal("window evidence removed", err)
	}
}

func releaseFor(t *testing.T, p retention.Permit, r retention.Receipt) retention.Release {
	t.Helper()
	pd, e := retention.Digest(p)
	if e != nil {
		t.Fatal(e)
	}
	rd, e := retention.Digest(r)
	if e != nil {
		t.Fatal(e)
	}
	return retention.Release{Version: 1, PermitDigest: pd, ReceiptDigest: rd}
}
func TestRetentionInterruptedDeletionRequiresSamePermitAndRelease(t *testing.T) {
	for _, phase := range []string{"intent_file", "intent_rename", "intent_dir", "unlink", "unlink_dir", "receipt_file", "receipt_rename", "receipt_dir"} {
		t.Run(phase, func(t *testing.T) {
			o, s, p := retentionSpool(t)
			ops := s.ops
			failure := errors.New("GC boundary fault")
			fired := false
			s.ops.syncFile = func(f *os.File) error {
				if !fired && (phase == "intent_file" || phase == "receipt_file" && strings.Contains(f.Name(), ".tmp-gc-")) {
					if phase != "receipt_file" || fileExists(filepath.Join(o.Path, gcIntentName)) {
						fired = true
						return failure
					}
				}
				return ops.syncFile(f)
			}
			s.ops.rename = func(a, b string) error {
				if !fired && (phase == "intent_rename" && filepath.Base(b) == gcIntentName || phase == "receipt_rename" && filepath.Base(b) == gcReceiptName) {
					fired = true
					return failure
				}
				return ops.rename(a, b)
			}
			dirs := 0
			s.ops.syncDir = func(path string) error {
				dirs++
				if !fired && (phase == "intent_dir" && dirs == 1 || phase == "unlink_dir" && dirs == 2 || phase == "receipt_dir" && dirs == 3) {
					fired = true
					return failure
				}
				return ops.syncDir(path)
			}
			s.ops.remove = func(path string) error {
				if !fired && phase == "unlink" && strings.HasPrefix(filepath.Base(path), ".quarantine-") {
					fired = true
					return failure
				}
				return ops.remove(path)
			}
			if _, e := s.CollectRetention(t.Context(), p, func(context.Context) error { return nil }); !errors.Is(e, failure) || !fired {
				t.Fatalf("boundary missing %v fired=%v", e, fired)
			}
			if e := s.Close(); e != nil {
				t.Fatal(e)
			}
			pending := fileExists(filepath.Join(o.Path, gcIntentName))
			if pending {
				if n, e := Open(t.Context(), o); !errors.Is(e, ErrRecovery) {
					if n != nil {
						_ = n.Close()
					}
					t.Fatal("normal Open finished GC", e)
				}
			}
			s, e := OpenMaintenance(t.Context(), o, p)
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = s.Close() }()
			receipt, e := s.CollectRetention(t.Context(), p, func(context.Context) error { return nil })
			if e != nil {
				t.Fatal(e)
			}
			wrong := releaseFor(t, p, receipt)
			wrong.PermitDigest[0]++
			if e = s.ReleaseRetention(t.Context(), p, wrong); e == nil {
				t.Fatal("wrong release accepted")
			}
			if e = s.ReleaseRetention(t.Context(), p, releaseFor(t, p, receipt)); e != nil {
				t.Fatal(e)
			}
			if e = s.Close(); e != nil {
				t.Fatal(e)
			}
			s, e = Open(t.Context(), o)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.Close(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func fileExists(path string) bool { _, e := os.Lstat(path); return e == nil }

func TestRetentionUnknownCorruptAndUnacknowledgedEvidence(t *testing.T) {
	o, s, p := retentionSpool(t)
	unknown := filepath.Join(o.Path, ".quarantine-.tmp-result-evidence")
	if e := os.WriteFile(unknown, []byte("unclassified"), 0600); e != nil {
		t.Fatal(e)
	}
	// Current pending work changes the request fingerprint, and remains readable.
	r := execwire.Result{Ref: execwire.Ref{Generation: 2, Height: 131, Hash: execwire.Hash{50}}, Parent: p.Request.ACK.Hash, Time: 1, StateRoot: execwire.Hash{3}, ReceiptsRoot: execwire.Hash{4}, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}, Steps: []execwire.Step{{Ordinal: 0, Cause: execwire.Finalize, TxIndex: -1}}}
	if _, e := s.Append(t.Context(), r); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CollectRetention(t.Context(), p, func(context.Context) error { return nil }); !errors.Is(e, ErrConflict) {
		t.Fatal("stale plan accepted", e)
	}
	head := r.Ref
	head.Generation = 0
	req, e := s.RetentionRequest(t.Context(), head, p.Request.HeadRoot, 129)
	if e != nil {
		t.Fatal(e)
	}
	p.Request = req
	path := filepath.Join(o.Path, ".quarantine-"+recordName(execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}))
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	data[len(data)-1] ^= 1
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CollectRetention(t.Context(), p, func(context.Context) error { return nil }); !errors.Is(e, ErrCorrupt) {
		t.Fatal("corrupt candidate collected", e)
	}
	if !fileExists(path) || !fileExists(unknown) {
		t.Fatal("evidence disappeared")
	}
	if _, e = s.Read(t.Context(), r.Ref); e != nil {
		t.Fatal("pending result lost", e)
	}
}

func TestRetentionConcurrentReadCloseAndGenerationJoin(t *testing.T) {
	_, s, p := retentionSpool(t)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, e := s.CollectRetention(t.Context(), p, func(context.Context) error { close(entered); <-release; return nil })
		done <- e
	}()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e := s.Retained(ctx, execwire.Ref{Generation: 2, Height: 1, Hash: execwire.Hash{2}}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	next := p.Request.ACK
	next.Generation++
	canceled := make(chan error, 1)
	go func() { canceled <- s.Rebind(ctx, next) }()
	if e := <-canceled; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case e := <-closed:
		t.Fatal("Close escaped active deletion lease", e)
	default:
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if e := <-closed; e != nil {
		t.Fatal(e)
	}
}

func TestRetentionResumeCapacityDoesNotDeleteOrRecover(t *testing.T) {
	o, s, p := retentionSpool(t)
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	if e := ResumeCapacity(t.Context(), o); e != nil {
		t.Fatal(e)
	}
	original := o
	o.MaxRecords = 1
	if e := ResumeCapacity(t.Context(), o); !errors.Is(e, ErrFull) {
		t.Fatal("quarantine escaped resume quota", e)
	}
	s, e := OpenMaintenance(t.Context(), original, p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CollectRetention(t.Context(), p, func(context.Context) error { return errors.New("retain fence") }); e == nil {
		t.Fatal("expected incomplete collection")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if e = ResumeCapacity(t.Context(), original); !errors.Is(e, ErrRecovery) {
		t.Fatal("resume observation released pending deletion", e)
	}
}
