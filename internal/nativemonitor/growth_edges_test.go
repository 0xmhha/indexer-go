package nativemonitor

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func growthValues(t *testing.T) (Policy, Snapshot, Snapshot, time.Time) {
	t.Helper()
	p, s, previous, args := growthFiles(t)
	var policy Policy
	var current, prior Snapshot
	for _, input := range []struct {
		path string
		out  any
	}{{p, &policy}, {s, &current}, {previous, &prior}} {
		if err := json.Unmarshal(growthInput(t, input.path), input.out); err != nil {
			t.Fatal(err)
		}
	}
	at, err := time.Parse(time.RFC3339Nano, args[5])
	if err != nil {
		t.Fatal(err)
	}
	return policy, current, prior, at
}

func TestIndexerGrowthExactHorizonAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		limit      uint64
		extra      time.Duration
		prior, now uint64
		code       string
	}{
		{"stop-inclusive", 1200, 0, 100, 200, "stop_horizon_reached"},
		{"stop-plus-nanoseconds", 1200, time.Nanosecond, 100, 200, "warning_horizon_reached"},
		{"warning-inclusive", 6200, 0, 100, 200, "warning_horizon_reached"},
		{"outside-warning", 6201, 0, 100, 200, "outside_warning_horizon"},
		{"uint64-outside-warning", math.MaxUint64, 0, math.MaxUint64 / 2, math.MaxUint64/2 + 1, "outside_warning_horizon"},
		{"uint64-stop", math.MaxUint64, 0, 0, math.MaxUint64 - 1, "stop_horizon_reached"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, s, before, at := growthValues(t)
			p.CapacityWarnBPS, p.CapacityStopBPS = u(9999), u(10000)
			s.Metrics.Storage[1].Limit, before.Metrics.Storage[1].Limit = u(tc.limit), u(tc.limit)
			s.Metrics.Storage[1].Used, before.Metrics.Storage[1].Used = u(tc.now), u(tc.prior)
			stamp := at.Add(-time.Minute - tc.extra)
			before.Metrics.ObservedAt = &stamp
			r := EvaluateWithPrevious(t.Context(), &p, &s, &before, &at)
			if !has(r, "growth.indexer.forecast", tc.code) {
				t.Fatalf("exact forecast boundary: %+v", r)
			}
		})
	}
}

func TestIndexerGrowthBindingTimeAndUnknownBoundaries(t *testing.T) {
	type mutation func(*Policy, *Snapshot, *Snapshot, *time.Time)
	for _, tc := range []struct {
		name   string
		change mutation
		field  string
		code   string
		status string
	}{
		{"no-growth", func(_ *Policy, s, b *Snapshot, _ *time.Time) { b.Metrics.Storage[1].Used = s.Metrics.Storage[1].Used }, "growth.indexer.forecast", "growth_not_observed", hold},
		{"shrink", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) { b.Metrics.Storage[1].Used = u(300) }, "growth.indexer.forecast", "growth_not_observed", hold},
		{"budget-change", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) { b.Metrics.Storage[1].Limit = u(2000) }, "growth.budget", "budget_changed", hold},
		{"series-change", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) {
			b.Metrics.Storage[1].SeriesID = strings.Repeat("a1", 32)
		}, "growth.series", "series_changed", hold},
		{"wrong-current-series", func(_ *Policy, s, _ *Snapshot, _ *time.Time) {
			s.Metrics.Storage[1].SeriesID = strings.Repeat("a1", 32)
		}, "growth.series", "series_mismatch", fail},
		{"missing-previous-series", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) { b.Metrics.Storage[1].SeriesID = "" }, "growth.previous", "not_observed", hold},
		{"invalid-previous-series", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) {
			b.Metrics.Storage[1].SeriesID = "private-value"
		}, "growth.previous", "invalid_indexer_sample", fail},
		{"different-db-chain", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) { b.Identity.Chain.ID = "2" }, "growth.identity", "identity_mismatch", fail},
		{"missing-expected-identity", func(p *Policy, _, _ *Snapshot, _ *time.Time) { p.Expected = nil }, "growth.identity", "identity_unavailable", hold},
		{"generation-change", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) { b.Metrics.Pipeline.Generation = u(1) }, "growth.generation", "generation_changed", hold},
		{"unknown-generation", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) { b.Metrics.Pipeline.Generation = nil }, "growth.generation", "not_observed", hold},
		{"invalid-generation", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) {
			b.Metrics.Pipeline.Generation = u(math.MaxUint64)
		}, "growth.generation", "invalid_generation", fail},
		{"same-time", func(_ *Policy, s, b *Snapshot, _ *time.Time) { b.Metrics.ObservedAt = s.Metrics.ObservedAt }, "growth.time", "invalid_time_order", fail},
		{"reverse-time", func(_ *Policy, s, b *Snapshot, at *time.Time) {
			later := at.Add(-time.Second)
			s.Metrics.ObservedAt = &later
			b.Metrics.ObservedAt = at
		}, "growth.time", "invalid_time_order", fail},
		{"future", func(_ *Policy, s, _ *Snapshot, at *time.Time) {
			future := at.Add(time.Second)
			s.Metrics.ObservedAt = &future
		}, "growth.time", "future_observation", fail},
		{"stale", func(_ *Policy, s, b *Snapshot, at *time.Time) {
			now, prior := at.Add(-61*time.Second), at.Add(-121*time.Second)
			s.Metrics.ObservedAt, b.Metrics.ObservedAt = &now, &prior
		}, "growth.time", "stale_observation", hold},
		{"interval-too-short", func(p *Policy, _, _ *Snapshot, _ *time.Time) { p.IndexerGrowth.MinIntervalSeconds = u(61) }, "growth.time", "interval_outside_policy", hold},
		{"interval-too-long", func(p *Policy, _, _ *Snapshot, _ *time.Time) {
			p.IndexerGrowth.MinIntervalSeconds = u(1)
			p.IndexerGrowth.MaxIntervalSeconds = u(59)
		}, "growth.time", "interval_outside_policy", hold},
		{"missing-time", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) { b.Metrics.ObservedAt = nil }, "growth.time", "not_observed", hold},
		{"missing-used", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) { b.Metrics.Storage[1].Used = nil }, "growth.previous", "not_observed", hold},
		{"duplicate-indexer", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) {
			b.Metrics.Storage = append(b.Metrics.Storage, b.Metrics.Storage[1])
		}, "growth.previous", "invalid_or_duplicate_kind", fail},
		{"zero-budget", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) { b.Metrics.Storage[1].Limit = u(0) }, "growth.previous", "invalid_indexer_sample", fail},
		{"missing-used-zero-budget", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) {
			b.Metrics.Storage[1].Used = nil
			b.Metrics.Storage[1].Limit = u(0)
		}, "growth.previous", "invalid_indexer_sample", fail},
		{"missing-series-invalid-budget", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) {
			b.Metrics.Storage[1].SeriesID = ""
			b.Metrics.Storage[1].Used = u(1001)
		}, "growth.previous", "invalid_indexer_sample", fail},
		{"prior-over-budget", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) { b.Metrics.Storage[1].Used = u(1001) }, "growth.previous", "invalid_indexer_sample", fail},
		{"unsupported-version", func(_ *Policy, _ *Snapshot, b *Snapshot, _ *time.Time) { b.Version = 2 }, "growth.samples", "unsupported_version", fail},
		{"missing-horizon", func(p *Policy, _, _ *Snapshot, _ *time.Time) { p.IndexerGrowth.StopWithinSeconds = nil }, "policy.indexer_growth.stop", "not_observed", hold},
		{"invalid-horizon-order", func(p *Policy, _, _ *Snapshot, _ *time.Time) { p.IndexerGrowth.StopWithinSeconds = u(3600) }, "policy.indexer_growth", "invalid_horizon_order", fail},
		{"invalid-interval-order", func(p *Policy, _, _ *Snapshot, _ *time.Time) { p.IndexerGrowth.MinIntervalSeconds = u(3601) }, "policy.indexer_growth", "invalid_interval_order", fail},
		{"invalid-interval-limit", func(p *Policy, _, _ *Snapshot, _ *time.Time) { p.IndexerGrowth.MaxIntervalSeconds = u(86401) }, "policy.indexer_growth.max_interval", "invalid_limit", fail},
		{"unknown-policy-series", func(p *Policy, _, _ *Snapshot, _ *time.Time) { p.IndexerGrowth.SeriesID = "HOLD" }, "policy.indexer_growth.series", "not_observed", hold},
		{"zero-policy-series", func(p *Policy, _, _ *Snapshot, _ *time.Time) { p.IndexerGrowth.SeriesID = strings.Repeat("0", 64) }, "policy.indexer_growth.series", "invalid_series_id", fail},
		{"invalid-age-limit", func(p *Policy, _, _ *Snapshot, _ *time.Time) { p.MaxAgeSeconds = nil }, "growth.time", "age_limit_unavailable", hold},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, s, before, at := growthValues(t)
			tc.change(&p, &s, &before, &at)
			r := EvaluateWithPrevious(t.Context(), &p, &s, &before, &at)
			if r.Status != tc.status || !has(r, tc.field, tc.code) || has(r, "growth.indexer.forecast", "stop_horizon_reached") {
				t.Fatalf("unqualified sample pair yielded forecast: %+v", r)
			}
		})
	}
}

func TestIndexerGrowthPreservesCurrentFailuresAuthorityAndBorrowedInputs(t *testing.T) {
	p, s, previous, at := growthValues(t)
	s.Metrics.Storage[0].Used = u(1000)
	previous.Metrics.Storage[1].Used = s.Metrics.Storage[1].Used
	before, err := json.Marshal([]any{p, s, previous, at})
	if err != nil {
		t.Fatal(err)
	}
	r := EvaluateWithPrevious(t.Context(), &p, &s, &previous, &at)
	if r.Status != fail || !has(r, "storage.raw.usage", "stop_threshold_reached") || !has(r, "growth.indexer.forecast", "growth_not_observed") {
		t.Fatal("unknown growth hid independent current stop", r)
	}
	after, err := json.Marshal([]any{p, s, previous, at})
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("evaluator mutated borrowed input")
	}
	if r.Actuation != "none" || r.Authenticity != "not_verified" || r.OperationalReadiness != "not_authorized" {
		t.Fatal("forecast granted authority")
	}
	if !has(Evaluate(t.Context(), &p, &s, &at), "growth.samples", "not_observed") {
		t.Fatal("missing history became zero growth")
	}
	p.IndexerGrowth = nil
	if !has(EvaluateWithPrevious(t.Context(), &p, &s, &previous, &at), "growth.policy", "not_configured") {
		t.Fatal("history without explicit policy accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if !has(EvaluateWithPrevious(ctx, &p, &s, &previous, &at), "input", "cancelled") {
		t.Fatal("cancellation ignored")
	}
}

func TestIndexerGrowthKnownHistoryConflictsSurvivePartialPolicyOrSamples(t *testing.T) {
	t.Run("unsupported-history-without-policy", func(t *testing.T) {
		p, s, previous, at := growthValues(t)
		p.IndexerGrowth = nil
		previous.Version = 2
		r := EvaluateWithPrevious(t.Context(), &p, &s, &previous, &at)
		if r.Status != fail || !has(r, "growth.samples", "unsupported_version") {
			t.Fatal("known unsupported history version hidden by absent policy", r)
		}
	})
	t.Run("current-series-with-partial-history", func(t *testing.T) {
		p, s, previous, at := growthValues(t)
		s.Metrics.Storage[1].SeriesID = strings.Repeat("a1", 32)
		previous.Metrics.Storage[1].SeriesID = ""
		r := EvaluateWithPrevious(t.Context(), &p, &s, &previous, &at)
		if r.Status != fail || !has(r, "growth.series", "series_mismatch") {
			t.Fatal("known current series conflict hidden by partial history", r)
		}
	})
}

func TestIndexerGrowthHistoryFilesStrictPrivacyAndNoMutation(t *testing.T) {
	p, s, previous, args := growthFiles(t)
	before := map[string][]byte{}
	info := map[string]os.FileInfo{}
	for _, path := range []string{p, s, previous} {
		before[path] = growthInput(t, path)
		var err error
		info[path], err = os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if Run(t.Context(), args, &out) != 1 {
		t.Fatal("real private three-file command did not evaluate history")
	}
	for _, path := range []string{p, s, previous} {
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(info[path], after) || !bytes.Equal(before[path], growthInput(t, path)) {
			t.Fatal("input bytes or inode changed")
		}
		if strings.Contains(out.String(), path) {
			t.Fatal("input path echoed")
		}
	}
	for _, secret := range []string{"series_id", "original_anchor", "observed_at", "fixture-native"} {
		if strings.Contains(out.String(), secret) {
			t.Fatal("identity or input echoed")
		}
	}
	for _, tc := range []string{"unknown", "duplicate", "scalar-null", "trailing", "oversize", "public", "symlink", "hardlink", "missing", "duplicate-flag", "cancelled", "failed-output"} {
		t.Run(tc, func(t *testing.T) {
			_, _, prev, a := growthFiles(t)
			data := growthInput(t, prev)
			ctx := t.Context()
			var output bytes.Buffer
			switch tc {
			case "unknown":
				data = append([]byte(`{"private-extra":true,`), data[1:]...)
			case "duplicate":
				data = append([]byte(`{"Version":1,`), data[1:]...)
			case "scalar-null":
				data = []byte(`{"version":null}`)
			case "trailing":
				data = append(data, []byte(` {}`)...)
			case "oversize":
				data = bytes.Repeat([]byte(" "), (1<<20)+1)
			case "public":
				if err := os.Chmod(prev, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(prev, prev+".link"); err != nil {
					t.Fatal(err)
				}
				a[7] = prev + ".link"
			case "hardlink":
				if err := os.Link(prev, prev+".link"); err != nil {
					t.Fatal(err)
				}
			case "missing":
				a[7] += ".missing"
			case "duplicate-flag":
				a = []string{"--previous-snapshot", prev, "-previous-snapshot", prev}
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "failed-output":
				if Run(ctx, a, shortWriter{}) != 1 {
					t.Fatal("failed output ignored")
				}
				return
			}
			if tc == "unknown" || tc == "duplicate" || tc == "scalar-null" || tc == "trailing" || tc == "oversize" {
				if err := os.WriteFile(prev, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if code := Run(ctx, a, &output); code != 1 || strings.Contains(output.String(), "private-extra") || strings.Contains(output.String(), prev) {
				t.Fatalf("unsafe history accepted or echoed: exit%d %s", code, &output)
			}
		})
	}
}
