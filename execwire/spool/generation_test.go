package spool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
)

func TestGenerationRebindPreservesUnackedCanonicalResults(t *testing.T) {
	o, r := fixture(t)
	s := opened(t, o)
	ctx := context.Background()
	e, err := s.Append(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	r2 := nextResult(r)
	if _, err = s.Append(ctx, r2); err != nil {
		t.Fatal(err)
	}
	base := o.Anchor
	base.Generation = 2
	if err = s.Rebind(ctx, base); err != nil {
		t.Fatal("new generation must preserve and rebind pending results:", err)
	}
	st, err := s.Status(ctx)
	if err != nil || st.Committed != base || st.Pending != 0 || st.Quarantined != 2 {
		t.Fatalf("rebind inferred ACK or lost raw data: %+v %v", st, err)
	}
	if err = s.Acknowledge(ctx, ack(o, e)); err == nil {
		t.Fatal("old generation ACK accepted")
	}
	r.Ref.Generation = 2
	got, err := s.Retained(ctx, r.Ref)
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatalf("canonical retained replay: %v", err)
	}
	e, err = s.Append(ctx, got)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge(ctx, ack(o, e)); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s = opened(t, o)
	if err = s.Rebind(ctx, base); err != nil {
		t.Fatal("same base must not roll back later ACK:", err)
	}
	st, err = s.Status(ctx)
	if err != nil || st.Committed != r.Ref || st.Quarantined != 2 {
		t.Fatalf("reopen lost active checkpoint or old evidence: %+v %v", st, err)
	}
	r2.Ref.Generation = 2
	if _, err = s.Retained(ctx, r2.Ref); err != nil {
		t.Fatal(err)
	}
	wrong := r2.Ref
	wrong.Hash[0]++
	if _, err = s.Retained(ctx, wrong); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("noncanonical result replayed: %v", err)
	}
}

func TestGenerationRebindCannotPromoteHelloOrExceedRetainedQuota(t *testing.T) {
	o, r := fixture(t)
	o.MaxRecords = 2
	s := opened(t, o)
	ctx := context.Background()
	if _, err := s.Append(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, nextResult(r)); err != nil {
		t.Fatal(err)
	}
	forward := r.Ref
	forward.Generation = 2
	if err := s.Rebind(ctx, forward); !errors.Is(err, ErrSequence) {
		t.Fatalf("Hello promoted unacked height: %v", err)
	}
	base := o.Anchor
	base.Generation = 2
	if err := s.Rebind(ctx, base); err != nil {
		t.Fatal(err)
	}
	r.Ref.Generation = 2
	if _, err := s.Append(ctx, r); !errors.Is(err, ErrFull) {
		t.Fatalf("quarantine escaped quota: %v", err)
	}
	if _, err := s.Retained(ctx, r.Ref); err != nil {
		t.Fatal("quota failure lost raw evidence:", err)
	}
	if err := s.Rebind(ctx, o.Anchor); !errors.Is(err, ErrSequence) {
		t.Fatalf("stale generation accepted: %v", err)
	}
}

func TestGenerationRebindRecoversEveryDurableBoundary(t *testing.T) {
	for _, phase := range []string{"journal_sync", "journal_rename", "journal_dir", "quarantine", "state_sync", "state_rename", "cleanup"} {
		t.Run(phase, func(t *testing.T) {
			o, r := fixture(t)
			s := opened(t, o)
			ctx := context.Background()
			if _, err := s.Append(ctx, r); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Append(ctx, nextResult(r)); err != nil {
				t.Fatal(err)
			}
			base := o.Anchor
			base.Generation = 2
			ops := s.ops
			injected := errors.New("rebind boundary fault")
			fired := false
			syncs, dirs := 0, 0
			s.ops.syncFile = func(f *os.File) error {
				syncs++
				if !fired && (phase == "journal_sync" && syncs == 1 || phase == "state_sync" && syncs == 2) {
					fired = true
					return injected
				}
				return ops.syncFile(f)
			}
			s.ops.rename = func(a, b string) error {
				name := filepath.Base(b)
				if !fired && (phase == "journal_rename" && name == ".exec-spool-transition" || phase == "quarantine" && strings.HasPrefix(name, ".quarantine-") || phase == "state_rename" && name == stateName) {
					fired = true
					return injected
				}
				return ops.rename(a, b)
			}
			s.ops.syncDir = func(p string) error {
				dirs++
				if !fired && phase == "journal_dir" && dirs == 1 {
					fired = true
					return injected
				}
				return ops.syncDir(p)
			}
			s.ops.remove = func(p string) error {
				if !fired && phase == "cleanup" && filepath.Base(p) == ".exec-spool-transition" {
					fired = true
					return injected
				}
				return ops.remove(p)
			}
			if err := s.Rebind(ctx, base); !errors.Is(err, injected) || !fired {
				t.Fatalf("fault not exercised: %v", err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = opened(t, o)
			if err := s.Rebind(ctx, base); err != nil {
				t.Fatal("restart cannot complete rebind:", err)
			}
			for _, original := range []execwire.Result{r, nextResult(r)} {
				original.Ref.Generation = 2
				got, err := s.Retained(ctx, original.Ref)
				if err != nil || !reflect.DeepEqual(got, original) {
					t.Fatalf("durable transition lost unacked result: %v", err)
				}
			}
		})
	}
}
