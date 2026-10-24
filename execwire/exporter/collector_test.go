package exporter

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/spool"
)

type fakeBackend struct {
	blocks    map[uint64]execwire.Result
	canonical map[uint64]execwire.Hash
	head      uint64
	calls     []uint64
	failure   error
	after     func(uint64)
}

func TestCollectorDistinguishesShortReorgFromNodeCatchup(t *testing.T) {
	for _, fork := range []bool{true, false} {
		t.Run(map[bool]string{true: "fork", false: "catchup"}[fork], func(t *testing.T) {
			s, b, _ := collectorFixture(t, 16)
			c := &Collector{Backend: b, Spool: s}
			if _, err := c.CatchUp(t.Context()); err != nil {
				t.Fatal(err)
			}
			b.head = 2
			if fork {
				b.canonical[2] = execwire.Hash{99}
			}
			n, err := c.CatchUp(t.Context())
			if n != 0 {
				t.Fatal("lower head advanced result")
			}
			if fork {
				var change *CanonicalChange
				if !errors.As(err, &change) || change.Ref.Height != 2 || change.Ref.Hash != b.canonical[2] {
					t.Fatalf("short reorg hidden: %v", err)
				}
			} else if err != nil {
				t.Fatal("same canonical prefix needs node catchup:", err)
			}
		})
	}
}

func (b *fakeBackend) Head(context.Context) (execwire.Ref, error) {
	return execwire.Ref{Generation: 1, Height: b.head, Hash: b.canonical[b.head]}, nil
}
func (b *fakeBackend) Canonical(_ context.Context, n uint64) (execwire.Hash, error) {
	h, ok := b.canonical[n]
	if !ok {
		return h, errors.New("missing header")
	}
	return h, nil
}
func (b *fakeBackend) Replay(_ context.Context, n, g uint64) (execwire.Result, error) {
	b.calls = append(b.calls, n)
	if b.failure != nil {
		return execwire.Result{}, b.failure
	}
	r := b.blocks[n]
	r.Ref.Generation = g
	if b.after != nil {
		b.after(n)
	}
	return r, nil
}
func collectorFixture(t *testing.T, records int) (*spool.Store, *fakeBackend, spool.Options) {
	t.Helper()
	chain := execwire.Chain{ID: "8283", Genesis: execwire.Hash{1}}
	anchor := execwire.Ref{Generation: 1, Hash: chain.Genesis}
	o := spool.Options{Path: filepath.Join(t.TempDir(), "spool"), Chain: chain, Anchor: anchor, MaxBytes: 1 << 20, MaxRecords: records}
	s, err := spool.Open(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	b := &fakeBackend{head: 3, blocks: make(map[uint64]execwire.Result), canonical: map[uint64]execwire.Hash{0: anchor.Hash}}
	for n := uint64(1); n <= 3; n++ {
		h := execwire.Hash{byte(n + 1)}
		b.canonical[n] = h
		b.blocks[n] = execwire.Result{Ref: execwire.Ref{Generation: 1, Height: n, Hash: h}, Parent: b.canonical[n-1], Time: n, StateRoot: execwire.Hash{10}, ReceiptsRoot: execwire.Hash{11}, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}, Steps: []execwire.Step{{Cause: execwire.Finalize, TxIndex: -1}}}
	}
	return s, b, o
}
func TestCollectorWalksEveryCanonicalHeightAndSealsBeforeReturn(t *testing.T) {
	s, b, o := collectorFixture(t, 16)
	c := &Collector{Backend: b, Spool: s, Limit: 2}
	n, err := c.CatchUp(t.Context())
	if err != nil || n != 2 {
		t.Fatalf("coalesced head lost range: %d %v", n, err)
	}
	n, err = c.CatchUp(t.Context())
	if err != nil || n != 1 {
		t.Fatalf("range continuation: %d %v", n, err)
	}
	if !reflect.DeepEqual(b.calls, []uint64{1, 2, 3}) {
		t.Fatalf("replay heights: %v", b.calls)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = spool.Open(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	tail, err := s.Tail(t.Context())
	if err != nil || tail.Height != 3 {
		t.Fatal("returned before durable spool")
	}
}
func TestCollectorRejectsChangedCanonicalAndMissingParent(t *testing.T) {
	for _, kind := range []string{"changed", "parent"} {
		t.Run(kind, func(t *testing.T) {
			s, b, _ := collectorFixture(t, 16)
			c := &Collector{Backend: b, Spool: s}
			failure := errors.New("retained parent unavailable")
			if kind == "changed" {
				b.after = func(n uint64) { b.canonical[n] = execwire.Hash{99} }
			} else {
				b.failure = failure
			}
			n, err := c.CatchUp(t.Context())
			if kind == "changed" {
				if !errors.Is(err, ErrCanonicalChanged) {
					t.Fatalf("orphan sealed: %v", err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatalf("parent error hidden: %v", err)
			}
			st, _ := s.Status(t.Context())
			if n != 0 || st.Pending != 0 {
				t.Fatal("failed replay became durable result")
			}
		})
	}
}
func TestCollectorReusesRetainedCanonicalResultsAndStopsAtQuota(t *testing.T) {
	t.Run("retained", func(t *testing.T) {
		s, b, o := collectorFixture(t, 16)
		for n := uint64(1); n <= 3; n++ {
			e, err := s.Append(t.Context(), b.blocks[n])
			if err != nil {
				t.Fatal(err)
			}
			if n == 1 {
				if err = s.Acknowledge(t.Context(), execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: o.Chain, Ref: e.Ref, Digest: e.Digest}}); err != nil {
					t.Fatal(err)
				}
			}
		}
		base := b.blocks[1].Ref
		base.Generation = 2
		if err := s.Rebind(t.Context(), base); err != nil {
			t.Fatal(err)
		}
		b.failure = errors.New("must not need old parent")
		c := &Collector{Backend: b, Spool: s}
		n, err := c.CatchUp(t.Context())
		if err != nil || n != 2 || len(b.calls) != 0 {
			t.Fatalf("retained canonical replay: %d %v %v", n, err, b.calls)
		}
	})
	t.Run("quota", func(t *testing.T) {
		s, b, _ := collectorFixture(t, 1)
		c := &Collector{Backend: b, Spool: s}
		n, err := c.CatchUp(t.Context())
		if !errors.Is(err, spool.ErrFull) || n != 1 {
			t.Fatalf("quota did not stop source: %d %v", n, err)
		}
		tail, err := s.Tail(t.Context())
		if err != nil || tail.Height != 1 {
			t.Fatal("quota failure lost sealed prefix")
		}
	})
}
