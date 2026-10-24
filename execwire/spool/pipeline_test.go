package spool

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"testing"
)

func TestSpoolPipelineWalkDoesNotAcknowledgeOrSkip(t *testing.T) {
	o, r := fixture(t)
	s := opened(t, o)
	ctx := context.Background()
	e1, err := s.Append(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	e2, err := s.Append(ctx, nextResult(r))
	if err != nil {
		t.Fatal(err)
	}
	tail, err := s.Tail(ctx)
	if err != nil || tail != e2.Ref {
		t.Fatalf("durable tail: %+v %v", tail, err)
	}
	first, ok, err := s.After(ctx, o.Anchor)
	if err != nil || !ok || first != e1 {
		t.Fatalf("first pipeline result: %v", err)
	}
	second, ok, err := s.After(ctx, first.Ref)
	if err != nil || !ok || second != e2 {
		t.Fatalf("second must be available before first ACK: %v", err)
	}
	if _, ok, err := s.After(ctx, second.Ref); err != nil || ok {
		t.Fatalf("tail: %v %v", ok, err)
	}
	wrong := first.Ref
	wrong.Hash[0]++
	if _, _, err := s.After(ctx, wrong); !errors.Is(err, ErrConflict) {
		t.Fatalf("bad sender cursor: %v", err)
	}
	st, err := s.Status(ctx)
	if err != nil || st.Committed != o.Anchor || st.Pending != 2 {
		t.Fatal("pipeline inferred ACK")
	}
}

func TestSpoolReferenceCoversOnlyDurablePrefixAndPending(t *testing.T) {
	o, r := fixture(t)
	s := opened(t, o)
	ctx := context.Background()
	e, err := s.Append(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge(ctx, ack(o, e)); err != nil {
		t.Fatal(err)
	}
	e2, err := s.Append(ctx, nextResult(r))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []execwire.Ref{o.Anchor, e.Ref, e2.Ref} {
		got, ok, err := s.Reference(ctx, want.Height)
		if err != nil || !ok || got != want {
			t.Fatalf("known durable reference missing: %+v %v %v", got, ok, err)
		}
	}
	if _, ok, err := s.Reference(ctx, 3); err != nil || ok {
		t.Fatalf("unknown height fabricated: %v %v", ok, err)
	}
}
