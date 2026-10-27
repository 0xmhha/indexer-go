package preflight

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/nativepreflight"
)

func TestAppNumericPlaceholders(t *testing.T) {
	for _, tc := range []struct {
		name, want    string
		anchor, start string
		change        func(*testing.T, nativepreflight.Inputs)
	}{
		{name: "anchor_hold", want: "HOLD", anchor: "anchor_height: HOLD", start: "start_height: 101"},
		{name: "quoted_anchor_hold", want: "HOLD", anchor: `anchor_height: "HOLD"`, start: "start_height: 101"},
		{name: "anchor_missing", want: "HOLD", start: "start_height: 101"},
		{name: "anchor_empty_string", want: "HOLD", anchor: `anchor_height: ""`, start: "start_height: 101"},
		{name: "start_hold", want: "HOLD", anchor: "anchor_height: 100", start: "start_height: HOLD"},
		{name: "start_missing", want: "HOLD", anchor: "anchor_height: 100"},
		{name: "both_hold", want: "HOLD", anchor: "anchor_height: HOLD", start: "start_height: HOLD"},
		{name: "all_anchor_heights_unknown", want: "HOLD", anchor: "anchor_height: HOLD", start: "start_height: 101", change: func(t *testing.T, in nativepreflight.Inputs) {
			for _, path := range []string{in.Native, in.Retention, in.Checkpoint, in.Declaration} {
				replace(t, path, `"Height": 100`, `"Height": "HOLD"`)
			}
			replace(t, in.Declaration, `"retained_floor": 100`, `"retained_floor": "HOLD"`)
		}},
		{name: "known_root_conflict", want: "FAIL", anchor: "anchor_height: HOLD", start: "start_height: 101", change: func(t *testing.T, in nativepreflight.Inputs) {
			replace(t, in.Checkpoint, "0x"+strings.Repeat("22", 32), "0x"+strings.Repeat("33", 32))
		}},
		{name: "known_height_conflict", want: "FAIL", anchor: "anchor_height: HOLD", start: "start_height: 101", change: func(t *testing.T, in nativepreflight.Inputs) {
			replace(t, in.Checkpoint, `"Height": 100`, `"Height": 99`)
		}},
		{name: "known_start_with_declared_start_hold", want: "FAIL", anchor: "anchor_height: HOLD", start: "start_height: 102", change: func(t *testing.T, in nativepreflight.Inputs) {
			replace(t, in.Declaration, `"start_height": 101`, `"start_height": "HOLD"`)
		}},
		{name: "known_start_floor_with_all_heights_unknown", want: "FAIL", anchor: "anchor_height: HOLD", start: "start_height: 102", change: func(t *testing.T, in nativepreflight.Inputs) {
			for _, path := range []string{in.Native, in.Retention, in.Checkpoint, in.Declaration} {
				replace(t, path, `"Height": 100`, `"Height": "HOLD"`)
			}
			replace(t, in.Declaration, `"start_height": 101`, `"start_height": 102`)
		}},
		{name: "db_pool_conflict", want: "FAIL", anchor: "anchor_height: HOLD", start: "start_height: 101", change: func(t *testing.T, in nativepreflight.Inputs) {
			replace(t, in.App, "max_conns: 4", "max_conns: 7")
		}},
		{name: "rpc_budget_conflict", want: "FAIL", anchor: "anchor_height: HOLD", start: "start_height: 101", change: func(t *testing.T, in nativepreflight.Inputs) {
			replace(t, in.App, "rate_limit: 5", "rate_limit: 6")
		}},
		{name: "explicit_zero_is_known", want: "FAIL", anchor: "anchor_height: 0", start: "start_height: 101"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := fixture(t)
			replace(t, in.App, "anchor_height: 100", tc.anchor)
			replace(t, in.App, "start_height: 101", tc.start)
			if tc.change != nil {
				tc.change(t, in)
			}
			before, err := os.ReadFile(in.App)
			if err != nil {
				t.Fatal(err)
			}
			r := nativepreflight.Check(context.Background(), in, Product())
			if r.Status != tc.want {
				t.Fatalf("want %s, got %s: %+v", tc.want, r.Status, r.Findings)
			}
			for _, f := range r.Findings {
				if f.Field == "app" && f.Code == "invalid_schema" {
					t.Fatal("a valid operator placeholder became a schema error")
				}
			}
			after, err := os.ReadFile(in.App)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("preflight modified its App input")
			}
		})
	}
}

func TestAppPlaceholderKeepsStrictSchema(t *testing.T) {
	for _, value := range []string{"???", "null", "[]", "{height: HOLD}", "HOLD\n    anchor_height: 100", "HOLD\n    unexpected: true", "*missing", "HOLD\n---\n{}"} {
		t.Run(value, func(t *testing.T) {
			in := fixture(t)
			replace(t, in.App, "anchor_height: 100", "anchor_height: "+value)
			if r := nativepreflight.Check(context.Background(), in, Product()); r.Status != "FAIL" {
				t.Fatalf("invalid App schema passed: %+v", r)
			}
		})
	}
}

func TestRuntimeAppRejectsNumericPlaceholder(t *testing.T) {
	in := fixture(t)
	replace(t, in.App, "anchor_height: 100", "anchor_height: HOLD")
	b, err := os.ReadFile(in.App)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.DecodeStrict(b, config.NewConfig()); err == nil {
		t.Fatal("runtime App decoder accepted an unresolved numeric height")
	}
}
