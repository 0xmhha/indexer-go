package nativestore

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/0xmhha/indexer-go/execwire/retention"
)

type leaseAdapter struct {
	fixtureAdapter
	bound    bool
	observed **os.File
	dataDir  string
}

func (a leaseAdapter) BindRunLease(f *os.File, p retention.Permit) (Adapter, error) {
	if p.Validate() != nil {
		return nil, ErrUnsafe
	}
	*a.observed = f
	if other, err := takeRunLease(a.dataDir); err == nil {
		_ = other.Close()
		return nil, ErrUnsafe
	}
	a.bound = true
	return a, nil
}
func (a leaseAdapter) Lock(ctx context.Context, path string, r retention.Request) (io.Closer, error) {
	if !a.bound {
		return nil, ErrUnsafe
	}
	return a.fixtureAdapter.Lock(ctx, path, r)
}
func (a leaseAdapter) Prune(ctx context.Context, path string, p retention.Permit) error {
	if !a.bound {
		return ErrUnsafe
	}
	return a.fixtureAdapter.Prune(ctx, path, p)
}
func (a leaseAdapter) Verify(ctx context.Context, path string, p retention.Permit) error {
	if !a.bound {
		return ErrUnsafe
	}
	return a.fixtureAdapter.Verify(ctx, path, p)
}
func TestMaintenanceAdapterBorrowsCurrentExclusiveRunLease(t *testing.T) {
	o, p := fixture(t)
	var observed *os.File
	a := leaseAdapter{observed: &observed, dataDir: o.DataDir}
	o.Adapter = a
	if err := Compact(t.Context(), o, p); err != nil {
		t.Fatal("adapter never received the owned EX descriptor:", err)
	}
	if a.bound {
		t.Fatal("binding mutated caller's adapter")
	}
	if observed == nil {
		t.Fatal("missing inherited descriptor")
	}
	if _, err := observed.Stat(); err == nil {
		t.Fatal("owned EX lease escaped Compact")
	}
	if err := WithRetentionSnapshot(t.Context(), o, p, func(string) error { return nil }); err != nil {
		t.Fatal("snapshot did not bind its own EX lease:", err)
	}
	d, err := retention.Digest(p)
	if err != nil {
		t.Fatal(err)
	}
	r := retention.Receipt{Version: 1, PermitDigest: d, Node: true, Spool: true}
	rd, _ := retention.Digest(r)
	if err = Release(t.Context(), o, p, r, retention.Release{Version: 1, PermitDigest: d, ReceiptDigest: rd}); err != nil {
		t.Fatal("release adapter did not receive its own EX lease:", err)
	}
}
