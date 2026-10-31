package spool

import (
	"bytes"
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectMetadataRetainsSourceAdmissionAndACKDigest(t *testing.T) {
	o, result := fixture(t)
	admission := execwire.Hash{91}
	o.Admission = &admission
	s, err := Open(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	entry, err := s.Append(t.Context(), result)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge(t.Context(), ack(o, entry)); err != nil {
		t.Fatal(err)
	}
	observed, err := InspectMetadata(t.Context(), o.Path)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Admission == nil || *observed.Admission != admission || observed.Digest != entry.Digest {
		t.Fatal("source metadata binding lost")
	}

}

func TestInspectMetadataDoesNotRecoverCreateOrACK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent")
	if _, err := InspectMetadata(t.Context(), path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("created path")
	}
	o, _ := fixture(t)
	s, err := Open(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(o.Path, stateName))
	if err != nil {
		t.Fatal(err)
	}
	out, err := InspectMetadata(t.Context(), o.Path)
	if err != nil || out.Committed != o.Anchor {
		t.Fatal(out, err)
	}
	after, err := os.ReadFile(filepath.Join(o.Path, stateName))
	if err != nil || string(before) != string(after) {
		t.Fatal("inspection modified manifest", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = InspectMetadata(ctx, o.Path); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestInspectionValidatesPendingJournalsWithoutRecovering(t *testing.T) {
	for _, kind := range []string{"transition", "gc", "completion"} {
		t.Run(kind, func(t *testing.T) {
			o, s, p := retentionSpool(t)
			if e := s.Close(); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(o.Path, transitionName)
			var v any
			switch kind {
			case "transition":
				path = filepath.Join(o.Path, transitionName)
				to := p.Request.ACK
				to.Generation++
				v = transition{Version: 1, Chain: o.Chain, Anchor: o.Anchor, From: p.Request.ACK, To: to}
			case "gc":
				path = filepath.Join(o.Path, gcIntentName)
				v = gcIntent{Version: 1, Permit: p}
			case "completion":
				path = filepath.Join(o.Path, gcReceiptName)
				pd, e := retention.Digest(p)
				if e != nil {
					t.Fatal(e)
				}
				v = gcCompletion{Permit: p, Receipt: retention.Receipt{Version: 1, PermitDigest: pd, Node: true, Spool: true}}
			}
			b, e := sealJSON(v)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path, b, 0600); e != nil {
				t.Fatal(e)
			}
			i, e := os.Stat(path)
			if e != nil {
				t.Fatal(e)
			}
			out, e := InspectMetadata(t.Context(), o.Path)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "transition" && !out.TransitionPending || kind == "gc" && !out.MaintenancePending || kind == "completion" && out.Completion == nil {
				t.Fatalf("pending observation lost: %+v", out)
			}
			after, e := os.ReadFile(path)
			if e != nil || !bytes.Equal(b, after) {
				t.Fatal("journal modified")
			}
			j, e := os.Stat(path)
			if e != nil || !os.SameFile(i, j) {
				t.Fatal("journal replaced")
			}
			if e = os.WriteFile(path, []byte(`{"Data":{},"Digest":"bad"}`), 0600); e != nil {
				t.Fatal(e)
			}
			if _, e = InspectMetadata(t.Context(), o.Path); !errors.Is(e, ErrCorrupt) {
				t.Fatalf("corrupt journal accepted: %v", e)
			}
		})
	}
}
func TestInspectionRefusesUnsafeAncestorsMarkersAndManifest(t *testing.T) {
	o, _ := fixture(t)
	s, e := Open(t.Context(), o)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	parent := filepath.Join(t.TempDir(), "linked")
	if e = os.Symlink(o.Path, parent); e != nil {
		t.Fatal(e)
	}
	if _, e = InspectMetadata(t.Context(), parent); e == nil {
		t.Fatal("directory link accepted")
	}
	marker := filepath.Join(o.Path, transitionName)
	if e = os.Symlink(filepath.Join(o.Path, stateName), marker); e != nil {
		t.Fatal(e)
	}
	if _, e = InspectMetadata(t.Context(), o.Path); e == nil {
		t.Fatal("marker link accepted")
	}
	if e = os.Remove(marker); e != nil {
		t.Fatal(e)
	}
	for _, b := range []string{`{`, `{"Data":{},"Data":{}}`, strings.Repeat("x", (1<<20)+1)} {
		if e = os.WriteFile(filepath.Join(o.Path, stateName), []byte(b), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e = InspectMetadata(t.Context(), o.Path); e == nil {
			t.Fatal("corrupt manifest accepted")
		}
	}
}

func TestInspectionRejectsCommittedTransitionWithoutExactBase(t *testing.T) {
	o, _ := fixture(t)
	s, e := Open(t.Context(), o)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	base := o.Anchor
	base.Generation = 2
	committed := execwire.Ref{Generation: 2, Height: 1, Hash: execwire.Hash{2}}
	digest := execwire.Hash{4}
	m := manifest{Version: 2, Chain: o.Chain, Anchor: o.Anchor, Base: &base, Committed: committed, Digest: digest, History: []Entry{{Ref: committed, Parent: o.Anchor.Hash, Digest: digest, Size: 1}}}
	state, e := sealJSON(m)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(o.Path, stateName), state, 0600); e != nil {
		t.Fatal(e)
	}
	j := transition{Version: 1, Chain: o.Chain, Anchor: o.Anchor, From: execwire.Ref{Generation: 1, Height: 2, Hash: execwire.Hash{3}}, To: committed}
	journal, e := sealJSON(j)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(o.Path, transitionName), journal, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = InspectMetadata(t.Context(), o.Path); !errors.Is(e, ErrCorrupt) {
		t.Fatalf("known committed-To/base conflict must be corrupt, got %v", e)
	}
}
