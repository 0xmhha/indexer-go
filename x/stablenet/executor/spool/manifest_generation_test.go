package spool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerationManifestRejectsAlteredAnchorAndACKGap(t *testing.T) {
	for _, kind := range []string{"anchor_hash", "ack_gap"} {
		t.Run(kind, func(t *testing.T) {
			o, r := fixture(t)
			s := opened(t, o)
			ctx := context.Background()
			if kind == "ack_gap" {
				e, err := s.Append(ctx, r)
				if err != nil {
					t.Fatal(err)
				}
				if err = s.Acknowledge(ctx, ack(o, e)); err != nil {
					t.Fatal(err)
				}
			}
			m := s.state
			if kind == "anchor_hash" {
				base := o.Anchor
				base.Generation = 2
				base.Hash[0]++
				m.Version, m.Base, m.Committed = 2, &base, base
			} else {
				m.History[0].Ref.Height++
				m.Committed = m.History[0].Ref
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			data, err := sealJSON(m)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(o.Path, stateName), data, 0600); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(ctx, o)
			if reopened != nil {
				_ = reopened.Close()
			}
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("semantically invalid sealed manifest accepted: %v", err)
			}
		})
	}
}
