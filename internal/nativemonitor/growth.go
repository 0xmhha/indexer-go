package nativemonitor

import (
	"context"
	"math"
	"time"

	"github.com/0xmhha/indexer-go/internal/monitorpredicate"
)

// Evaluate preserves the one-sample API. An enabled growth policy without its
// previous sample stays unresolved, rather than inventing a zero growth rate.
func Evaluate(ctx context.Context, p *Policy, s *Snapshot, at *time.Time) Report {
	return EvaluateWithPrevious(ctx, p, s, nil, at)
}

// EvaluateWithPrevious adds an opt-in two-point estimate to current findings.
// Inputs are borrowed read-only. Previous diagnostics are not current evidence.
func EvaluateWithPrevious(ctx context.Context, p *Policy, s, previous *Snapshot, at *time.Time) Report {
	r := evaluateCurrent(ctx, p, s, at)
	if ctx.Err() == nil {
		r.indexerGrowth(p, s, previous, at)
	}
	if ctx.Err() != nil && !hasFinding(r, "input", "cancelled") {
		r.add("input", fail, "cancelled")
	}
	return r
}

func hasFinding(r Report, field, code string) bool {
	for _, f := range r.Findings {
		if f.Field == field && f.Code == code {
			return true
		}
	}
	return false
}

func validSeriesID(s string) bool {
	if len(s) != 64 {
		return false
	}
	nonzero := false
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
		nonzero = nonzero || c != '0'
	}
	return nonzero
}

func growthIdentity(id *Identity) bool {
	return id != nil && id.Profile != "" && len(id.Profile) <= 64 && validRef(id.Chain, id.Anchor) && id.Anchor.Generation == 1 && (id.Anchor.Height != 0 || id.Anchor.Hash == id.Chain.Genesis)
}

func (r *Report) growthPolicy(p *GrowthPolicy) bool {
	known := true
	for _, v := range []struct {
		field string
		value *uint64
		max   uint64
	}{{"warn", p.WarnWithinSeconds, 604800}, {"stop", p.StopWithinSeconds, 604800}, {"min_interval", p.MinIntervalSeconds, 86400}, {"max_interval", p.MaxIntervalSeconds, 86400}} {
		r.number("policy.indexer_growth."+v.field, v.value, v.max)
		known = known && v.value != nil && *v.value > 0 && *v.value <= v.max
	}
	if p.SeriesID == "" || p.SeriesID == "HOLD" {
		r.add("policy.indexer_growth.series", hold, "not_observed")
		known = false
	} else if !validSeriesID(p.SeriesID) {
		r.add("policy.indexer_growth.series", fail, "invalid_series_id")
		known = false
	}
	if p.WarnWithinSeconds != nil && p.StopWithinSeconds != nil && *p.StopWithinSeconds >= *p.WarnWithinSeconds {
		r.add("policy.indexer_growth", fail, "invalid_horizon_order")
		known = false
	}
	if p.MinIntervalSeconds != nil && p.MaxIntervalSeconds != nil && *p.MinIntervalSeconds > *p.MaxIntervalSeconds {
		r.add("policy.indexer_growth", fail, "invalid_interval_order")
		known = false
	}
	return known
}

func (r *Report) growthUsage(label string, m *Metrics) *Storage {
	field := "growth." + label
	if m == nil {
		r.add(field, hold, "not_observed")
		return nil
	}
	if len(m.Storage) > 6 {
		r.add(field, fail, "storage_limit_exceeded")
		return nil
	}
	var found *Storage
	seen := map[string]bool{}
	for i := range m.Storage {
		v := &m.Storage[i]
		if !storageKind(v.Kind) || seen[v.Kind] {
			r.add(field, fail, "invalid_or_duplicate_kind")
			return nil
		}
		seen[v.Kind] = true
		if v.Kind == "indexer" {
			found = v
		}
	}
	if found == nil {
		r.add(field, hold, "not_observed")
		return nil
	}
	if found.Limit != nil && *found.Limit == 0 ||
		found.Used != nil && found.Limit != nil && *found.Used > *found.Limit ||
		found.Free != nil || found.SeriesID != "" && found.SeriesID != "HOLD" && !validSeriesID(found.SeriesID) {
		r.add(field, fail, "invalid_indexer_sample")
		return nil
	}
	if found.Used == nil || found.Limit == nil || found.SeriesID == "" || found.SeriesID == "HOLD" {
		r.add(field, hold, "not_observed")
		return nil
	}
	return found
}

func (r *Report) indexerGrowth(p *Policy, s, previous *Snapshot, at *time.Time) {
	if previous != nil && previous.Version != 1 {
		r.add("growth.samples", fail, "unsupported_version")
		return
	}
	if p == nil || p.IndexerGrowth == nil {
		if previous != nil {
			r.add("growth.policy", hold, "not_configured")
		}
		return
	}
	g := p.IndexerGrowth
	if !r.growthPolicy(g) {
		return
	}
	if s == nil || previous == nil {
		r.add("growth.samples", hold, "not_observed")
		return
	}
	if s.Version != 1 || previous.Version != 1 {
		r.add("growth.samples", fail, "unsupported_version")
		return
	}
	r.identity("growth.previous.identity", previous.Identity)
	if !growthIdentity(p.Expected) || !growthIdentity(s.Identity) || !growthIdentity(previous.Identity) {
		r.add("growth.identity", hold, "identity_unavailable")
		return
	}
	if *p.Expected != *s.Identity || *p.Expected != *previous.Identity {
		r.add("growth.identity", fail, "identity_mismatch")
		return
	}
	current, prior := r.growthUsage("current", s.Metrics), r.growthUsage("previous", previous.Metrics)
	if current == nil {
		return
	}
	if current.SeriesID != g.SeriesID {
		r.add("growth.series", fail, "series_mismatch")
		return
	}
	if prior == nil {
		return
	}
	if prior.SeriesID != current.SeriesID {
		r.add("growth.series", hold, "series_changed")
		return
	}
	if *current.Limit != *prior.Limit {
		r.add("growth.budget", hold, "budget_changed")
		return
	}
	for _, m := range []*Metrics{s.Metrics, previous.Metrics} {
		if m.Pipeline == nil || m.Pipeline.Generation == nil || *m.Pipeline.Generation == 0 {
			r.add("growth.generation", hold, "not_observed")
			return
		}
		if *m.Pipeline.Generation > math.MaxInt64 {
			r.add("growth.generation", fail, "invalid_generation")
			return
		}
	}
	if *s.Metrics.Pipeline.Generation != *previous.Metrics.Pipeline.Generation {
		r.add("growth.generation", hold, "generation_changed")
		return
	}
	now, before := s.Metrics.ObservedAt, previous.Metrics.ObservedAt
	if at == nil || at.IsZero() || now == nil || now.IsZero() || before == nil || before.IsZero() {
		r.add("growth.time", hold, "not_observed")
		return
	}
	if now.After(*at) || before.After(*at) {
		r.add("growth.time", fail, "future_observation")
		return
	}
	if !now.After(*before) {
		r.add("growth.time", fail, "invalid_time_order")
		return
	}
	if p.MaxAgeSeconds == nil || *p.MaxAgeSeconds == 0 || *p.MaxAgeSeconds > 86400 {
		r.add("growth.time", hold, "age_limit_unavailable")
		return
	}
	if at.Sub(*now) > time.Duration(*p.MaxAgeSeconds)*time.Second {
		r.add("growth.time", hold, "stale_observation")
		return
	}
	span := now.Sub(*before)
	if span < time.Duration(*g.MinIntervalSeconds)*time.Second || span > time.Duration(*g.MaxIntervalSeconds)*time.Second {
		r.add("growth.time", hold, "interval_outside_policy")
		return
	}
	if *current.Used <= *prior.Used {
		r.add("growth.indexer.forecast", hold, "growth_not_observed")
		return
	}
	delta, remaining := *current.Used-*prior.Used, *current.Limit-*current.Used
	// remaining*span/delta <= horizon, without division, rounding or overflow.
	within := func(seconds uint64) bool {
		return monitorpredicate.ProductGE(delta, seconds*uint64(time.Second), remaining, uint64(span))
	}
	switch {
	case within(*g.StopWithinSeconds):
		r.add("growth.indexer.forecast", fail, "stop_horizon_reached")
	case within(*g.WarnWithinSeconds):
		r.add("growth.indexer.forecast", warn, "warning_horizon_reached")
	default:
		r.add("growth.indexer.forecast", pass, "outside_warning_horizon")
	}
}
