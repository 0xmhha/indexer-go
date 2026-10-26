package spool

import (
	"bytes"
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"os"
	"path/filepath"
	"testing"
)

func TestAdmissionPinnedBeforeRecovery(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "pending"}[pending], func(t *testing.T) {
			o, r := fixture(t)
			h := execwire.Hash{42}
			o.Admission = &h
			s := opened(t, o)
			if pending {
				if _, e := s.Append(t.Context(), r); e != nil {
					t.Fatal(e)
				}
			}
			if e := s.Close(); e != nil {
				t.Fatal(e)
			}
			before, e := os.ReadFile(filepath.Join(o.Path, stateName))
			if e != nil {
				t.Fatal(e)
			}
			for _, changed := range []*execwire.Hash{nil, {}, {43}} {
				bad := o
				bad.Admission = changed
				s, e := Open(t.Context(), bad)
				if s != nil {
					_ = s.Close()
				}
				if !errors.Is(e, ErrConflict) {
					t.Fatalf("policy change accepted: %v", e)
				}
				after, _ := os.ReadFile(filepath.Join(o.Path, stateName))
				if !bytes.Equal(before, after) {
					t.Fatal("mismatched open changed manifest")
				}
			}
			same := execwire.Hash{42}
			o.Admission = &same
			s = opened(t, o)
			_ = s.Close()
		})
	}
	o, _ := fixture(t)
	s := opened(t, o)
	_ = s.Close()
	h := execwire.Hash{42}
	o.Admission = &h
	if s, e := Open(t.Context(), o); e == nil {
		_ = s.Close()
		t.Fatal("enabled proofs on legacy spool")
	}
}

func TestAdmissionMaintenancePreservesBindingAndRejectsActiveWrites(t *testing.T) {
	o, s, p := retentionSpool(t)
	h := execwire.Hash{42}
	s.state.Admission = &h
	if e := s.saveState(t.Context()); e != nil {
		t.Fatal(e)
	}
	var e error
	p.Request, e = s.RetentionRequest(t.Context(), p.Request.Head, p.Request.HeadRoot, p.Request.KeepBlocks)
	if e != nil {
		t.Fatal(e)
	}
	remove := s.ops.remove
	failure := errors.New("stop after sealed GC intent")
	s.ops.remove = func(string) error { return failure }
	if _, e = s.CollectRetention(t.Context(), p, func(context.Context) error { return nil }); !errors.Is(e, failure) {
		t.Fatal(e)
	}
	s.ops.remove = remove
	_ = s.Close()
	wrong := p
	wrong.Token[0]++
	if m, e := OpenMaintenance(t.Context(), o, wrong); e == nil {
		_ = m.Close()
		t.Fatal("wrong permit bypassed binding")
	}
	m, e := OpenMaintenance(t.Context(), o, p)
	if e != nil {
		t.Fatal(e)
	}
	if m.state.Admission == nil || *m.state.Admission != h {
		t.Fatal("binding lost")
	}
	_, r := fixture(t)
	if _, e = m.Append(t.Context(), r); !errors.Is(e, ErrRecovery) {
		t.Fatal("maintenance admitted active input", e)
	}
	receipt, e := m.CollectRetention(t.Context(), p, func(context.Context) error { return nil })
	if e != nil || receipt.Match(p) != nil {
		t.Fatal(e)
	}
	if e = m.ReleaseRetention(t.Context(), p, releaseFor(t, p, receipt)); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Append(t.Context(), r); !errors.Is(e, ErrRecovery) {
		t.Fatal("released maintenance handle admitted active input", e)
	}
	_ = m.Close()
	o.Admission = &h
	m = opened(t, o)
	_ = m.Close()
}
func TestAdmissionCopiesCallerAndPrecedesTransitionRecovery(t *testing.T) {
	o, _ := fixture(t)
	h := execwire.Hash{42}
	o.Admission = &h
	s := opened(t, o)
	h[0] = 43
	if *s.state.Admission != (execwire.Hash{42}) {
		t.Fatal("caller mutated policy")
	}
	_ = s.Close()
	marker := filepath.Join(o.Path, transitionName)
	if e := os.WriteFile(marker, []byte("invalid transition"), 0600); e != nil {
		t.Fatal(e)
	}
	if s, e := Open(t.Context(), o); !errors.Is(e, ErrConflict) {
		if s != nil {
			_ = s.Close()
		}
		t.Fatal("recovery ran before policy check", e)
	}
	if data, e := os.ReadFile(marker); e != nil || string(data) != "invalid transition" {
		t.Fatal("transition mutated", e)
	}
}
