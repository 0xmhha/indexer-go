package exporter_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/exporter"
	"github.com/0xmhha/indexer-go/execwire/spool"
)

func TestSharedArcSpoolRestartQuotaAndExactACK(t *testing.T) {
	ctx := t.Context()
	chain := execwire.Chain{ID: "5042002", Genesis: execwire.Hash{1}}
	anchor := execwire.Ref{Generation: 1, Hash: chain.Genesis}
	o := spool.Options{Path: filepath.Join(t.TempDir(), "spool"), Chain: chain, Anchor: anchor, MaxBytes: 1 << 20, MaxRecords: 1}
	r := execwire.Result{Ref: execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}, Parent: anchor.Hash, Time: 1, StateRoot: execwire.Hash{3}, ReceiptsRoot: execwire.Hash{4}, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}, Steps: []execwire.Step{{Cause: execwire.PreBlock, TxIndex: -1}, {Ordinal: 1, Cause: execwire.Finalize, TxIndex: -1}}}
	s, err := spool.Open(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Append(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = spool.Open(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	got, err := s.Read(ctx, r.Ref)
	if err != nil || len(got.Steps) != 2 || got.Steps[0].Cause != execwire.PreBlock {
		t.Fatalf("Arc result lost across restart: %v", err)
	}
	if _, err = (&exporter.Collector{Spool: s}).CatchUp(ctx); !errors.Is(err, spool.ErrConflict) {
		t.Fatalf("backend required: %v", err)
	}
	next := r
	next.Parent, next.Ref.Height, next.Ref.Hash = r.Ref.Hash, 2, execwire.Hash{5}
	if _, err = s.Append(ctx, next); !errors.Is(err, spool.ErrFull) {
		t.Fatalf("quota: %v", err)
	}
	e, ok, err := s.Next(ctx)
	if err != nil || !ok {
		t.Fatal(err)
	}
	ack := execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: chain, Ref: r.Ref, Digest: e.Digest}}
	ack.Header.Digest[0]++
	if err = s.Acknowledge(ctx, ack); err == nil {
		t.Fatal("wrong ACK accepted")
	}
	if _, err = s.Read(ctx, r.Ref); err != nil {
		t.Fatal("wrong ACK deleted source", err)
	}
	ack.Header.Digest = e.Digest
	if err = s.Acknowledge(ctx, ack); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Append(ctx, next); err != nil {
		t.Fatal("exact ACK did not release quota", err)
	}
}
