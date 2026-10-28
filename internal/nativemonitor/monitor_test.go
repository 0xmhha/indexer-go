package nativemonitor

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
)

func u(v uint64) *uint64 { return &v }
func b(v bool) *bool     { return &v }
func fixture() (Policy, Snapshot, time.Time) {
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	id := Identity{Profile: "fixture-native", Chain: execwire.Chain{ID: "1", Genesis: execwire.Hash{1}}, Anchor: execwire.Ref{Generation: 1, Hash: execwire.Hash{1}}}
	p := Policy{Version: 1, Expected: &id, MaxAgeSeconds: u(60), CapacityWarnBPS: u(8000), CapacityStopBPS: u(9500), MinFreeBytes: u(100), BacklogWarnBlocks: u(10), BacklogStopBlocks: u(20), LagWarnSeconds: u(5), LagStopSeconds: u(10), RequiredStorage: []string{"raw", "indexer", "filesystem"}}
	s := Snapshot{Version: 1, Identity: &id, Diagnostics: &DiagnosticSample{ObservedAt: &at, Report: &DiagnosticReport{Version: 1, Profile: id.Profile, Status: "HOLD", Scope: "explicit_metadata_and_exported_artifacts_only", Authority: "not_authorized", LiveFreshness: "not_performed", FullState: "not_performed", Consistency: "separate_samples_not_atomic_not_a_lease", Findings: []Finding{{"indexer.live", "HOLD", "operating_db_uninspected"}}, Observations: []Observation{}}}, Metrics: &Metrics{ObservedAt: &at, Pipeline: &Pipeline{Generation: u(2), Head: u(100), Received: u(100), Processed: u(100), ACK: u(100), OldestPendingSeconds: u(0), ACKLagSeconds: u(0), WorkerLagSeconds: u(0)}, Storage: []Storage{{Kind: "raw", Used: u(0), Limit: u(1000)}, {Kind: "indexer", Used: u(100), Limit: u(1000)}, {Kind: "filesystem", Free: u(1000)}}}}
	return p, s, at
}

func has(r Report, field, code string) bool {
	for _, f := range r.Findings {
		if f.Field == field && f.Code == code {
			return true
		}
	}
	return false
}

func TestKnownCriticalSurvivesMissingStaleAndUnknownInputs(t *testing.T) {
	p, s, at := fixture()
	s.Diagnostics = nil
	s.Metrics.Storage[0].Used = u(950)
	stale := at.Add(-time.Hour)
	s.Metrics.ObservedAt = &stale
	r := Evaluate(t.Context(), &p, &s, &at)
	if r.Status != "FAIL" || r.AlertLevel != "critical" || !has(r, "storage.raw.usage", "stop_threshold_reached") {
		t.Fatalf("known capacity critical was masked: %+v", r)
	}
}

func TestNormalMetricsAreNotOperationalReadiness(t *testing.T) {
	p, s, at := fixture()
	r := Evaluate(t.Context(), &p, &s, &at)
	if r.Status != "HOLD" || r.AlertLevel != "unknown" || r.OperationalReadiness != "not_authorized" || r.LiveFreshness != "not_performed" || r.Actuation != "none" || r.Authenticity != "not_verified" {
		t.Fatalf("offline sample became operational readiness: %+v", r)
	}
	if !has(r, "storage.raw.usage", "below_warning_threshold") {
		t.Fatal("explicit zero treated as missing")
	}
}

func TestCapacityBacklogLagAndFloorBoundaries(t *testing.T) {
	cases := []struct {
		name               string
		mutate             func(*Snapshot)
		field, code, alert string
	}{
		{"capacity-warning", func(s *Snapshot) { s.Metrics.Storage[0].Used = u(800) }, "storage.raw.usage", "warning_threshold_reached", "warning"},
		{"capacity-stop", func(s *Snapshot) { s.Metrics.Storage[0].Used = u(950) }, "storage.raw.usage", "stop_threshold_reached", "critical"},
		{"free-floor", func(s *Snapshot) { s.Metrics.Storage[2].Free = u(100) }, "storage.filesystem.free", "free_floor_reached", "critical"},
		{"receive-gap", func(s *Snapshot) { s.Metrics.Pipeline.Head = u(110) }, "pipeline.receive_gap", "warning_threshold_reached", "warning"},
		{"worker-backlog", func(s *Snapshot) { s.Metrics.Pipeline.Head = u(120); s.Metrics.Pipeline.Received = u(120) }, "pipeline.backlog", "stop_threshold_reached", "critical"},
		{"ack-backlog", func(s *Snapshot) { s.Metrics.Pipeline.ACK = u(90) }, "pipeline.ack_gap", "warning_threshold_reached", "warning"},
		{"oldest-pending", func(s *Snapshot) { s.Metrics.Pipeline.OldestPendingSeconds = u(10) }, "pipeline.oldest_pending", "stop_threshold_reached", "critical"},
		{"ack-delay", func(s *Snapshot) { s.Metrics.Pipeline.ACKLagSeconds = u(5) }, "pipeline.ack_lag", "warning_threshold_reached", "warning"},
		{"worker-delay", func(s *Snapshot) { s.Metrics.Pipeline.WorkerLagSeconds = u(10) }, "pipeline.worker_lag", "stop_threshold_reached", "critical"},
		{"full-uint64-budget", func(s *Snapshot) {
			s.Metrics.Storage[0].Limit = u(math.MaxUint64)
			s.Metrics.Storage[0].Used = u(math.MaxUint64 - 1)
		}, "storage.raw.usage", "stop_threshold_reached", "critical"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, s, at := fixture()
			c.mutate(&s)
			r := Evaluate(t.Context(), &p, &s, &at)
			if r.AlertLevel != c.alert || !has(r, c.field, c.code) {
				t.Fatalf("wrong boundary result: %+v", r)
			}
			if c.alert == "critical" && r.Status != "FAIL" {
				t.Fatal("critical masked")
			}
		})
	}
}

func TestUnknownFreshnessAndIndependentFailures(t *testing.T) {
	cases := []struct {
		name        string
		mutate      func(*Policy, *Snapshot, *time.Time)
		field, code string
	}{
		{"stale-metrics", func(_ *Policy, s *Snapshot, at *time.Time) {
			v := at.Add(-time.Minute - time.Nanosecond)
			s.Metrics.ObservedAt = &v
		}, "metrics.time", "stale_observation"},
		{"stale-diagnostics", func(_ *Policy, s *Snapshot, at *time.Time) { v := at.Add(-time.Hour); s.Diagnostics.ObservedAt = &v }, "diagnostics.time", "stale_observation"},
		{"unknown-limit", func(p *Policy, _ *Snapshot, _ *time.Time) { p.CapacityStopBPS = nil }, "policy.capacity.stop", "not_observed"},
		{"missing-pipeline", func(_ *Policy, s *Snapshot, _ *time.Time) { s.Metrics.Pipeline = nil }, "pipeline", "not_observed"},
		{"missing-free", func(_ *Policy, s *Snapshot, _ *time.Time) { s.Metrics.Storage[2].Free = nil }, "storage.filesystem.free", "not_observed"},
		{"missing-head", func(_ *Policy, s *Snapshot, _ *time.Time) { s.Metrics.Pipeline.Head = nil }, "pipeline.head", "not_observed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, s, at := fixture()
			c.mutate(&p, &s, &at)
			r := Evaluate(t.Context(), &p, &s, &at)
			if r.Status != "HOLD" || !has(r, c.field, c.code) {
				t.Fatalf("unknown treated as healthy: %+v", r)
			}
		})
	}
	p, s, at := fixture()
	boundary := at.Add(-time.Minute)
	s.Metrics.ObservedAt = &boundary
	if has(Evaluate(t.Context(), &p, &s, &at), "metrics.time", "stale_observation") {
		t.Fatal("inclusive age boundary rejected")
	}
	s.Diagnostics.Report.Status = "FAIL"
	s.Diagnostics.Report.Findings = []Finding{{"private/input", "FAIL", "private-error"}}
	r := Evaluate(t.Context(), nil, &s, nil)
	if r.Status != "FAIL" || !has(r, "diagnostics", "reported_failure") {
		t.Fatal("known diagnostic failure masked by missing policy/time")
	}
}

func TestContradictionsInvalidPolicyAndCancellationFailClosed(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Policy, *Snapshot, *time.Time)
	}{
		{"policy-version", func(p *Policy, _ *Snapshot, _ *time.Time) { p.Version = 2 }},
		{"threshold-order", func(p *Policy, _ *Snapshot, _ *time.Time) { p.BacklogWarnBlocks = u(20) }},
		{"invalid-percent", func(p *Policy, _ *Snapshot, _ *time.Time) { p.CapacityStopBPS = u(10001) }},
		{"zero-limit", func(_ *Policy, s *Snapshot, _ *time.Time) { s.Metrics.Storage[0].Limit = u(0) }},
		{"duplicate-storage", func(_ *Policy, s *Snapshot, _ *time.Time) {
			s.Metrics.Storage = append(s.Metrics.Storage, s.Metrics.Storage[0])
		}},
		{"opaque-kind", func(_ *Policy, s *Snapshot, _ *time.Time) { s.Metrics.Storage[0].Kind = "postgres://private" }},
		{"ack-ahead", func(_ *Policy, s *Snapshot, _ *time.Time) { s.Metrics.Pipeline.ACK = u(101) }},
		{"ack-ahead-missing-middle", func(_ *Policy, s *Snapshot, _ *time.Time) {
			s.Metrics.Pipeline.Processed = nil
			s.Metrics.Pipeline.ACK = u(101)
		}},
		{"invalid-height", func(_ *Policy, s *Snapshot, _ *time.Time) { s.Metrics.Pipeline.Head = u(math.MaxUint64) }},
		{"zero-generation", func(_ *Policy, s *Snapshot, _ *time.Time) { s.Metrics.Pipeline.Generation = u(0) }},
		{"future-metric", func(_ *Policy, s *Snapshot, at *time.Time) { v := at.Add(time.Nanosecond); s.Metrics.ObservedAt = &v }},
		{"identity-mismatch", func(_ *Policy, s *Snapshot, _ *time.Time) { v := *s.Identity; v.Chain.ID = "2"; s.Identity = &v }},
		{"false-authority", func(_ *Policy, s *Snapshot, _ *time.Time) { s.Diagnostics.Report.Authority = "authorized" }},
		{"unsupported-diagnostics", func(_ *Policy, s *Snapshot, _ *time.Time) { s.Diagnostics.Report.Version = 2 }},
		{"too-many-observations", func(_ *Policy, s *Snapshot, _ *time.Time) {
			s.Diagnostics.Report.Observations = make([]Observation, 13)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, s, at := fixture()
			c.mutate(&p, &s, &at)
			if r := Evaluate(t.Context(), &p, &s, &at); r.Status != "FAIL" {
				t.Fatalf("unsafe snapshot accepted: %+v", r)
			}
		})
	}
	p, s, at := fixture()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if Evaluate(ctx, &p, &s, &at).Status != "FAIL" {
		t.Fatal("cancellation ignored")
	}
}

func TestPendingFenceAndGenerationRemainUnresolved(t *testing.T) {
	p, s, at := fixture()
	s.Diagnostics.Report.Observations = []Observation{{Kind: "source.store", Basis: "separate_metadata_samples", Chain: s.Identity.Chain, Anchor: s.Identity.Anchor, ACK: &execwire.Ref{Generation: 3, Height: 100, Hash: execwire.Hash{2}}, MaintenancePending: b(true)}}
	r := Evaluate(t.Context(), &p, &s, &at)
	if r.Status != "HOLD" || r.AlertLevel != "warning" || !has(r, "diagnostics.generation", "generation_rebind_unverified") || !has(r, "diagnostics.pending", "maintenance_pending") {
		t.Fatalf("fence or generation cleared: %+v", r)
	}
}

func TestUnknownGenerationCannotBecomeMeasuredBacklog(t *testing.T) {
	p, s, at := fixture()
	s.Metrics.Pipeline.Generation = nil
	s.Metrics.Pipeline.Head = u(120)
	s.Metrics.Pipeline.Received = u(120)
	r := Evaluate(t.Context(), &p, &s, &at)
	if r.Status != "HOLD" || r.AlertLevel != "unknown" || has(r, "pipeline.backlog", "stop_threshold_reached") {
		t.Fatalf("unknown generation became measured backlog: %+v", r)
	}
}

func TestPipelineCannotPrecedeItsDeclaredAnchor(t *testing.T) {
	p, s, at := fixture()
	id := *s.Identity
	id.Anchor.Height = 10
	id.Anchor.Hash = execwire.Hash{3}
	p.Expected = &id
	s.Identity = &id
	p.BacklogWarnBlocks = u(500)
	p.BacklogStopBlocks = u(1000)
	s.Metrics.Pipeline.ACK = u(9)
	r := Evaluate(t.Context(), &p, &s, &at)
	if r.Status != "FAIL" || !has(r, "pipeline.anchor", "height_below_original_anchor") {
		t.Fatalf("below-anchor pipeline accepted: %+v", r)
	}
}

func TestKnownWarningSurvivesUnresolvedThresholdAndDoneFence(t *testing.T) {
	p, s, at := fixture()
	p.CapacityStopBPS = nil
	s.Metrics.Storage[0].Used = u(800)
	r := Evaluate(t.Context(), &p, &s, &at)
	if r.Status != "HOLD" || r.AlertLevel != "warning" {
		t.Fatalf("known warning masked by unresolved stop threshold: %+v", r)
	}
	p, s, at = fixture()
	s.Diagnostics.Report.Observations = []Observation{{Kind: "native.fence.permit", Basis: "recorded_permit_metadata_not_current_native_head", Chain: s.Identity.Chain, Anchor: s.Identity.Anchor, Phase: "done"}}
	r = Evaluate(t.Context(), &p, &s, &at)
	if r.Status != "HOLD" || r.AlertLevel != "warning" || !has(r, "diagnostics.pending", "maintenance_pending") {
		t.Fatal("unreleased done fence treated as released")
	}
}

func TestConflictingDiagnosticStatusAndBoundedFindings(t *testing.T) {
	p, s, at := fixture()
	s.Diagnostics.Report.Status = "PASS"
	if r := Evaluate(t.Context(), &p, &s, &at); r.Status != "FAIL" || !has(r, "diagnostics", "status_findings_conflict") {
		t.Fatal("diagnostic status hid unresolved findings")
	}
	p, s, at = fixture()
	for range 12 {
		s.Diagnostics.Report.Observations = append(s.Diagnostics.Report.Observations, Observation{Kind: "invalid", Basis: "invalid", Chain: execwire.Chain{}, Anchor: execwire.Ref{}, ACK: &execwire.Ref{}, Base: &execwire.Ref{}, Visible: &execwire.Ref{}, Parent: &execwire.Ref{}, Root: &execwire.Hash{}, Generation: u(0), Floor: u(math.MaxUint64), MaintenancePending: b(true), TransitionPending: b(true), HeaderAvailable: b(false), StateAvailable: b(false)})
	}
	r := Evaluate(t.Context(), &p, &s, &at)
	if r.Status != "FAIL" || len(r.Findings) != 128 || r.Findings[127].Code != "finding_limit_exceeded" {
		t.Fatal("unbounded or incomplete findings", len(r.Findings))
	}
}
