package spool

import (
	"context"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
)

func exactRetentionSource(t *testing.T) (Options, *Store, execwire.Ref, execwire.Hash) {
	t.Helper()
	o, r := fixture(t)
	admission := execwire.Hash{18}
	o.Admission = &admission
	s := opened(t, o)
	base := execwire.Ref{Generation: 2, Height: 129, Hash: execwire.Hash{40}}
	// Sealed checkpoint fixture, not a forward ACK inferred by Rebind.
	s.state.Version, s.state.Base, s.state.Committed = 2, &base, base
	if err := s.saveState(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.Ref = execwire.Ref{Generation: 2, Height: 130, Hash: execwire.Hash{41}}
	r.Parent = base.Hash
	e, err := s.Append(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge(t.Context(), ack(o, e)); err != nil {
		t.Fatal(err)
	}
	head := e.Ref
	head.Generation = 0
	return o, s, head, e.Digest
}

func TestRetentionEvidenceOwnsExactSourceUntilCallbackAndRefusesAdmissionChange(t *testing.T) {
	o, s, head, digest := exactRetentionSource(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	called := false
	err := WithRetentionEvidence(t.Context(), o, head, execwire.Hash{99}, 129, func(r retention.Request, got execwire.Hash) error {
		called = true
		if got != digest || r.ACK.Height != head.Height {
			t.Error("lost exact ACK")
		}
		done := make(chan error, 1)
		go func() {
			other, err := Open(t.Context(), o)
			if other != nil {
				_ = other.Close()
			}
			done <- err
		}()
		if !errors.Is(<-done, ErrBusy) {
			t.Error("source ownership escaped callback")
		}
		return nil
	})
	if err != nil || !called {
		t.Fatalf("exact source not admitted: %v, callback=%v", err, called)
	}
	changed := execwire.Hash{19}
	o.Admission = &changed
	if err = WithRetentionEvidence(t.Context(), o, head, execwire.Hash{99}, 129, func(retention.Request, execwire.Hash) error { t.Error("changed source admitted"); return nil }); err == nil {
		t.Fatal("admission change accepted")
	}
}

func TestRetentionEvidenceRefusesHelloWithoutCommittedDigest(t *testing.T) {
	o, s, p := retentionSpool(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := WithRetentionEvidence(context.Background(), o, p.Request.Head, p.Root, p.Request.KeepBlocks, func(retention.Request, execwire.Hash) error { t.Error("Hello became ACK"); return nil }); err == nil {
		t.Fatal("zero digest admitted")
	}
}
