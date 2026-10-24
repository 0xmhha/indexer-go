package nativestore

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type fixtureAdapter struct{}
type fixtureLock struct{}

func (fixtureLock) Close() error { return nil }
func (f fixtureAdapter) Lock(ctx context.Context, path string, r retention.Request) (io.Closer, error) {
	if e := f.Check(ctx, path, r); e != nil {
		return nil, e
	}
	return fixtureLock{}, nil
}
func (fixtureAdapter) Check(_ context.Context, path string, _ retention.Request) error {
	b, e := os.ReadFile(filepath.Join(path, "CURRENT"))
	if e != nil {
		return e
	}
	if string(b) != "state and code" {
		return ErrUnsafe
	}
	return nil
}

func TestCopyRetentionResumesEachDurableBoundary(t *testing.T) {
	for _, phase := range []string{"intent", "copy", "candidate", "swap_intent", "old_rename", "new_rename", "cleanup_intent", "cleanup", "done"} {
		t.Run(phase, func(t *testing.T) {
			o, p := fixture(t)
			interrupted := errors.New("fixture power loss")
			e := compact(context.Background(), o, p, hooks{phase: func(at string) error {
				if at == phase {
					return interrupted
				}
				return nil
			}})
			if !errors.Is(e, interrupted) {
				t.Fatal("required interruption:", e)
			}
			changed := p
			changed.Token[0]++
			if e := Compact(context.Background(), o, changed); e == nil {
				t.Fatal("different permit accepted")
			}
			if _, e := AcquireRun(o.DataDir); !errors.Is(e, ErrPending) {
				t.Fatal("pending owner admitted:", e)
			}
			if e := Compact(context.Background(), o, p); e != nil {
				t.Fatal("same permit recovery:", e)
			}
			if e := (fixtureAdapter{}).Verify(context.Background(), o.DataDir, p); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestCopyRetentionRunOwnersCapacityAndUnknownFiles(t *testing.T) {
	o, p := fixture(t)
	first, e := AcquireRun(o.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	second, e := AcquireRun(o.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	if e := Compact(context.Background(), o, p); !errors.Is(e, ErrPending) {
		t.Fatal("active reader admitted:", e)
	}
	_ = first.Close()
	_ = second.Close()
	if e := compact(context.Background(), o, p, hooks{free: func(string) (uint64, error) { return 0, nil }}); !errors.Is(e, ErrCapacity) {
		t.Fatal("capacity:", e)
	}
	if b, e := os.ReadFile(filepath.Join(o.DataDir, "EXPIRED")); e != nil || string(b) != "old body" {
		t.Fatal("source changed:", e)
	}
	_, _, stage, _, e := paths(o)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(stage, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(stage, "UNKNOWN"), []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = Compact(context.Background(), o, p); e == nil {
		t.Fatal("unknown candidate removed")
	}
	if _, e = os.Stat(filepath.Join(stage, "UNKNOWN")); e != nil {
		t.Fatal(e)
	}
}
func (fixtureAdapter) Prune(ctx context.Context, path string, p retention.Permit) error {
	if e := (fixtureAdapter{}).Check(ctx, path, p.Request); e != nil {
		return e
	}
	return os.Remove(filepath.Join(path, "EXPIRED"))
}
func (fixtureAdapter) Verify(ctx context.Context, path string, p retention.Permit) error {
	if e := (fixtureAdapter{}).Check(ctx, path, p.Request); e != nil {
		return e
	}
	if _, e := os.Lstat(filepath.Join(path, "EXPIRED")); !os.IsNotExist(e) {
		return ErrUnsafe
	}
	return nil
}
func fixture(t *testing.T) (Options, retention.Permit) {
	t.Helper()
	parent := t.TempDir()
	if e := os.Chmod(parent, 0700); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(parent, "native")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	for name, data := range map[string]string{"CURRENT": "state and code", "EXPIRED": "old body"} {
		if e := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	chain := execwire.Chain{ID: "5042002", Genesis: execwire.Hash{1}}
	ack := execwire.Ref{Generation: 1, Height: 132, Hash: execwire.Hash{2}}
	r := retention.Request{Version: 1, Chain: chain, Anchor: execwire.Ref{Generation: 1, Hash: chain.Genesis}, ACK: ack, Tail: ack, Head: execwire.Ref{Height: 132, Hash: ack.Hash}, HeadRoot: execwire.Hash{3}, SourceDigest: execwire.Hash{4}, KeepBlocks: 129, Through: 3}
	p := retention.Permit{Version: 1, Coverage: "full", BaselineDigest: execwire.Hash{5}, Token: execwire.Hash{6}, Request: r, Visible: ack, Root: r.HeadRoot}
	if e := p.Validate(); e != nil {
		t.Fatal(e)
	}
	return Options{DataDir: dir, ReserveBytes: 1, Adapter: fixtureAdapter{}}, p
}
func TestCopyRetentionKeepsCurrentStateAndNeedsExactRelease(t *testing.T) {
	o, p := fixture(t)
	ctx := context.Background()
	if e := Compact(ctx, o, p); e != nil {
		t.Fatal("complete permitted native copy must compact:", e)
	}
	if e := (fixtureAdapter{}).Verify(ctx, o.DataDir, p); e != nil {
		t.Fatal(e)
	}
	pd, _ := retention.Digest(p)
	r := retention.Receipt{Version: 1, PermitDigest: pd, Node: true, Spool: true}
	rd, _ := retention.Digest(r)
	release := retention.Release{Version: 1, PermitDigest: pd, ReceiptDigest: rd}
	if e := Release(ctx, o, p, r, release); e != nil {
		t.Fatal(e)
	}
	if e := Release(ctx, o, p, r, release); e != nil {
		t.Fatal("release retry:", e)
	}
}
