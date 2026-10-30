package nativeops

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
)

func managedFixture(t *testing.T) (Native, Config) {
	t.Helper()
	c, p := nativeFixture(t)
	boot := []byte("owned Go protocol fixture, no native SDK")
	if err := os.WriteFile(filepath.Join(c.NativeDataDir, nativeinput.BootstrapName), boot, 0600); err != nil {
		t.Fatal(err)
	}
	i := nativeinput.Identity{Version: 1, Instance: execwire.Hash{17}, ChainID: 5042002, Genesis: c.Chain.Genesis, ArcRevision: nativeinput.ArcRevision, RethRevision: nativeinput.RethRevision, Layout: "reth-native-v2", BootstrapDigest: execwire.Hash(sha256.Sum256(boot)), Anchor: nativeinput.Ref{Hash: c.Anchor.Hash, Root: c.AnchorRoot}}
	h := nativeinput.Head{Version: 1, Instance: i.Instance, ImportEpoch: 1, ReadEpoch: 133, RequestID: 132, Tip: nativeinput.Ref{Height: 132, Hash: p.Request.Head.Hash, Root: p.Root}}
	r := nativeinput.Receipt{Version: 1, Identity: i, ImportEpoch: 1, ReadEpoch: 133, RequestID: 132, Height: 132, Hash: h.Tip.Hash, Root: h.Tip.Root, Parent: execwire.Hash{132}, ParentRoot: execwire.Hash{3}, ReceiptsRoot: execwire.Hash{4}, InputDigest: execwire.Hash{5}}
	if err := os.Mkdir(filepath.Join(c.NativeDataDir, ".arc-native-import-receipts"), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, v any) {
		t.Helper()
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(struct {
			Data   json.RawMessage
			Digest execwire.Hash
		}{data, execwire.Hash(sha256.Sum256(data))})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(c.NativeDataDir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(nativeinput.IdentityName, i)
	write(nativeinput.HeadName, h)
	write(".arc-native-import-receipts/1-132.json", r)
	proof := nativeinput.Proof{Version: 1, Identity: i, Head: h, Ref: h.Tip, Receipt: &r}
	if err := nativeinput.VerifyCurrent(c.NativeDataDir, proof, i); err != nil {
		t.Fatal(err)
	}
	return Native{Executable: c.NativeExecutable, Chain: c.Chain, AnchorRoot: c.AnchorRoot, Managed: &ManagedContext{Version: 1, OriginalDataDir: c.NativeDataDir, Proof: proof}}, c
}
func TestManagedNativeBindsOwnedDescriptorAndFreezesContext(t *testing.T) {
	n, c := managedFixture(t)
	lease, err := nativestore.AcquireExclusiveRun(c.NativeDataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lease.Close() }()
	bound, err := n.WithRunLease(lease)
	if err != nil {
		t.Fatal("managed adapter cannot borrow the current EX lease:", err)
	}
	n.Managed.Proof.Identity.Instance[0] ^= 1
	if bound.Managed.Proof.Identity == n.Managed.Proof.Identity {
		t.Fatal("binding retained mutable caller context")
	}
	n.Managed.Proof.Receipt.InputDigest[1] ^= 1
	if *bound.Managed.Proof.Receipt == *n.Managed.Proof.Receipt {
		t.Fatal("binding retained mutable receipt")
	}
	if _, err = lease.Stat(); err != nil {
		t.Fatal("binding closed caller's descriptor")
	}
	if err = lease.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = bound.WithRunLease(lease); err == nil {
		t.Fatal("closed descriptor accepted")
	}
}

func TestManagedNativeRejectsPublicPrefixAheadOfFrozenACK(t *testing.T) {
	n, c := managedFixture(t)
	_, p := nativeFixture(t)
	p.Visible.Height++
	p.Visible.Hash[0]++
	if err := p.Validate(); err != nil {
		t.Fatal("legacy permit must allow an ahead visible prefix for this requirement test:", err)
	}
	lease, err := nativestore.AcquireExclusiveRun(c.NativeDataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lease.Close() }()
	if _, err = n.BindRunLease(lease, p); err == nil {
		t.Fatal("managed bridge accepted a public prefix ahead of frozen ACK/native N")
	}
}

func TestManagedNativeChildKeepsParentExclusiveLeaseThroughJoin(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "cancel"}[cancelled], func(t *testing.T) {
			n, c := managedFixture(t)
			_, p := nativeFixture(t)
			p.Request.Chain = c.Chain
			p.Request.Anchor = c.Anchor
			p.Request.Head.Hash = n.Managed.Proof.Head.Tip.Hash
			p.Request.HeadRoot = n.Managed.Proof.Head.Tip.Root
			p.Root = p.Request.HeadRoot
			lease, err := nativestore.AcquireExclusiveRun(c.NativeDataDir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = lease.Close() }()
			adapter, err := n.BindRunLease(lease, p)
			if err != nil {
				t.Fatal(err)
			}
			bound := adapter.(Native)
			if _, err = n.Inspect(t.Context(), c.NativeDataDir, retention.Request{Version: 1, Chain: c.Chain, Anchor: c.Anchor}); err == nil {
				t.Fatal("unbound managed context launched a child")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			hold, err := bound.Lock(ctx, c.NativeDataDir, p.Request)
			if err != nil {
				t.Fatal(err)
			}
			if cancelled {
				cancel()
			}
			err = hold.Close()
			if cancelled && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			if !cancelled && err != nil {
				t.Fatal(err)
			}
			if other, err := nativestore.AcquireRun(c.NativeDataDir); err == nil {
				_ = other.Close()
				t.Fatal("child exit unlocked parent's EX lease")
			}
			if err = lease.Close(); err != nil {
				t.Fatal(err)
			}
			other, err := nativestore.AcquireRun(c.NativeDataDir)
			if err != nil {
				t.Fatal("joined child retained EX descriptor:", err)
			}
			_ = other.Close()
			work, err := filepath.Glob(filepath.Join(filepath.Dir(c.NativeDataDir), ".arc-maintenance-*"))
			if err != nil || len(work) != 0 {
				t.Fatalf("owned scratch leaked: %v %v", work, err)
			}
		})
	}
}
