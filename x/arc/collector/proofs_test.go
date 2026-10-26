package collector

import (
	"bytes"
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/spool"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"github.com/ethereum/go-ethereum/common"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestArcProofPolicyRejectsLegacyReplay(t *testing.T) {
	b, r := helperBackend(t, "normal")
	b.requiredStateProofs = &stateproof.Selection{Targets: []stateproof.Target{{Address: common.Address{1}}}}
	if _, e := b.Replay(t.Context(), r.Ref.Height, r.Ref.Generation); e == nil {
		t.Fatal("required native proof omitted but replay accepted")
	}
	select {
	case <-b.done:
	default:
		t.Fatal("invalid proof child not joined")
	}
}

func TestArcProofPolicyNormalizedCopiedAndPreflight(t *testing.T) {
	original := stateproof.Selection{Targets: []stateproof.Target{{Address: common.Address{2}, Code: true, Slots: []common.Hash{{2}, {1}}}, {Address: common.Address{1}}}}
	normalized, digest, e := proofPolicy(&original)
	if e != nil {
		t.Fatal(e)
	}
	original.Targets[0].Slots[0] = common.Hash{3}
	original.Targets[1].Code = true
	_, again, e := proofPolicy(normalized)
	if e != nil || *again != *digest {
		t.Fatal("scope aliased caller", e)
	}
	if _, changed, e := proofPolicy(&original); e != nil || *changed == *digest {
		t.Fatal("changed scope not bound", e)
	}
	empty := stateproof.Selection{}
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if e := Run(t.Context(), Config{Native: Options{StateProofs: &empty, Chain: execwire.Chain{ID: "5042002"}}, SpoolDir: source}); e == nil {
		t.Fatal("empty selection accepted")
	}
	if _, e := os.Lstat(source); !os.IsNotExist(e) {
		t.Fatal("invalid policy created source", e)
	}
	if b, e := Start(t.Context(), Options{StateProofs: &empty}); e == nil {
		_ = b.Close()
		t.Fatal("invalid selection started child")
	}
}

func TestArcProofNativeReplyAndImmutableSpoolRecovery(t *testing.T) {
	b, r := helperBackend(t, "proof-valid")
	got, e := b.Replay(t.Context(), r.Ref.Height, r.Ref.Generation)
	if e != nil {
		t.Fatal(e)
	}
	exact, _ := execwire.MarshalResult(r)
	actual, _ := execwire.MarshalResult(got)
	if !bytes.Equal(exact, actual) {
		t.Fatal("native proof bytes changed")
	}
	_, binding, e := proofPolicy(b.requiredStateProofs)
	if e != nil {
		t.Fatal(e)
	}
	o := spool.Options{Path: filepath.Join(t.TempDir(), "source"), Chain: b.chain, Anchor: execwire.Ref{Generation: r.Ref.Generation, Height: r.Ref.Height - 1, Hash: r.Parent}, Admission: binding, MaxBytes: 1 << 20, MaxRecords: 1}
	s, e := spool.Open(t.Context(), o)
	if e != nil {
		t.Fatal(e)
	}
	entry, e := s.Append(t.Context(), got)
	if e != nil {
		t.Fatal(e)
	}
	next := got
	next.Parent = got.Ref.Hash
	next.Ref.Height++
	next.Ref.Hash[0]++
	if _, e = s.Append(t.Context(), next); !errors.Is(e, spool.ErrFull) {
		t.Fatal("proof input bypassed capacity", e)
	}
	_ = s.Close()
	s, e = spool.Open(t.Context(), o)
	if e != nil {
		t.Fatal(e)
	}
	read, e := s.Read(t.Context(), r.Ref)
	if e != nil {
		t.Fatal(e)
	}
	p, _ := execwire.MarshalResult(read)
	if !bytes.Equal(exact, p) {
		t.Fatal("restart lost native evidence")
	}
	_ = s.Close()
	o.MaxRecords = 4
	s, e = spool.Open(t.Context(), o)
	if e != nil {
		t.Fatal(e)
	}
	base := o.Anchor
	base.Generation++
	if e = s.Rebind(t.Context(), base); e != nil {
		t.Fatal(e)
	}
	ref := r.Ref
	ref.Generation++
	reused, e := s.Retained(t.Context(), ref)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(reused.StateProofBundle.Response, r.StateProofBundle.Response) {
		t.Fatal("reorg changed proof")
	}
	entry, e = s.Append(t.Context(), reused)
	if e != nil {
		t.Fatal(e)
	}
	ack := execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: o.Chain, Ref: entry.Ref, Digest: entry.Digest}}
	if e = s.Acknowledge(t.Context(), ack); e != nil {
		t.Fatal(e)
	}
	if e = s.Acknowledge(t.Context(), ack); e != nil {
		t.Fatal("lost ACK", e)
	}
	_ = s.Close()
	bad := o
	bad.Admission = nil
	if opened, e := spool.Open(t.Context(), bad); e == nil {
		_ = opened.Close()
		t.Fatal("drained spool lost binding")
	}
	for _, mode := range []string{"proof-legacy", "proof-provider", "proof-corrupt"} {
		t.Run(mode, func(t *testing.T) {
			b, r := helperBackend(t, mode)
			if _, e := b.Replay(t.Context(), r.Ref.Height, r.Ref.Generation); e == nil {
				t.Fatal("invalid native proof accepted")
			}
			select {
			case <-b.done:
			default:
				t.Fatal("invalid child not joined")
			}
		})
	}
}
func TestArcProofCancellationJoinsAndReleasesNativeLease(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "entered")
	t.Setenv("INDEXER_ARC_HELPER_READY", marker)
	b, r := helperBackend(t, "proof-block")
	native := t.TempDir()
	lease, e := nativestore.AcquireRun(native)
	if e != nil {
		t.Fatal(e)
	}
	b.nativeLease = lease
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, e := b.Replay(ctx, r.Ref.Height, r.Ref.Generation); done <- e }()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
waiting:
	for {
		if _, e := os.Stat(marker); e == nil {
			break
		}
		select {
		case <-tick.C:
		case <-timer.C:
			t.Fatal("child did not enter")
			break waiting
		}
	}
	cancel()
	if e := <-done; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	select {
	case <-b.done:
	default:
		t.Fatal("canceled child not joined")
	}
	lease, e = nativestore.AcquireRun(native)
	if e != nil {
		t.Fatal("native owner lease leaked", e)
	}
	_ = lease.Close()
}

func TestArcProofHandshakeFailureJoinsOwnedNativeResources(t *testing.T) {
	f, _ := fixture(t)
	native := t.TempDir()
	lease, e := nativestore.AcquireRun(native)
	if e != nil {
		t.Fatal(e)
	}
	work := filepath.Join(t.TempDir(), "work")
	if e = os.Mkdir(work, 0700); e != nil {
		t.Fatal(e)
	}
	info, e := os.Lstat(work)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestArcBackendHelper$")
	cmd.Env = append(os.Environ(), "INDEXER_ARC_BACKEND_HELPER=identity", "GORACE=atexit_sleep_ms=0")
	if b, e := startOwnedBackend(ctx, cancel, cmd, f.Chain, time.Second, &backendOwnership{work: work, info: info, lease: lease}); e == nil {
		_ = b.Close()
		t.Fatal("invalid handshake accepted")
	}
	if cmd.ProcessState == nil {
		t.Fatal("handshake child not joined")
	}
	if _, e := os.Lstat(work); !os.IsNotExist(e) {
		t.Fatal("handshake scratch leaked", e)
	}
	lease, e = nativestore.AcquireRun(native)
	if e != nil {
		t.Fatal("handshake lease leaked", e)
	}
	_ = lease.Close()
}
