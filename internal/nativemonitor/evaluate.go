package nativemonitor

import (
	"context"
	"io"
	"math"
	"math/bits"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
)

const (
	pass = "PASS"
	hold = "HOLD"
	fail = "FAIL"
	warn = "WARN"
)

func newReport() Report {
	r := Report{Version: 1, Status: pass, AlertLevel: "none", Scope: "submitted_unverified_separate_samples_only", OperationalReadiness: "not_authorized", LiveFreshness: "not_performed", Authenticity: "not_verified", Actuation: "none", Findings: []Finding{}}
	r.add("authority", hold, "live_probes_not_performed")
	return r
}

func (r *Report) add(field, status, code string) {
	if len(r.Findings) >= 128 {
		r.Findings[127] = Finding{"report", fail, "finding_limit_exceeded"}
		r.Status = fail
		r.AlertLevel = "critical"
		return
	}
	r.Findings = append(r.Findings, Finding{field, status, code})
	if status == fail {
		r.Status = fail
		r.AlertLevel = "critical"
		return
	}
	if r.Status != fail && status == hold {
		r.Status = hold
	}
	if status == warn && r.AlertLevel != "critical" {
		r.AlertLevel = "warning"
	}
	if status == hold && r.AlertLevel == "none" {
		r.AlertLevel = "unknown"
	}
}

func validRef(c execwire.Chain, r execwire.Ref) bool {
	return r.Height <= math.MaxInt64 && r.Generation <= math.MaxInt64 && execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: c, Ref: r}}) == nil
}
func (r *Report) identity(field string, id *Identity) {
	if id == nil {
		r.add(field, hold, "not_observed")
		return
	}
	if id.Profile == "" || len(id.Profile) > 64 || !validRef(id.Chain, id.Anchor) || id.Anchor.Generation != 1 || id.Anchor.Height == 0 && id.Anchor.Hash != id.Chain.Genesis {
		r.add(field, fail, "invalid_identity")
	}
}
func (r *Report) number(field string, v *uint64, limit uint64) {
	if v == nil {
		r.add(field, hold, "not_observed")
	} else if *v == 0 || *v > limit {
		r.add(field, fail, "invalid_limit")
	}
}
func (r *Report) pair(field string, w, s *uint64, max uint64) {
	r.number(field+".warn", w, max)
	r.number(field+".stop", s, max)
	if w != nil && s != nil && *w >= *s {
		r.add(field, fail, "invalid_threshold_order")
	}
}
func storageKind(k string) bool {
	switch k {
	case "raw", "native", "indexer", "wal", "filesystem", "docker_vm":
		return true
	}
	return false
}
func (r *Report) policy(p *Policy) {
	if p == nil {
		r.add("policy", hold, "not_observed")
		return
	}
	if p.Version != 1 {
		r.add("policy", fail, "unsupported_version")
	}
	r.identity("policy.identity", p.Expected)
	r.number("policy.max_age", p.MaxAgeSeconds, 86400)
	r.pair("policy.capacity", p.CapacityWarnBPS, p.CapacityStopBPS, 10000)
	r.number("policy.min_free", p.MinFreeBytes, math.MaxUint64)
	r.pair("policy.backlog", p.BacklogWarnBlocks, p.BacklogStopBlocks, math.MaxInt64)
	r.pair("policy.lag", p.LagWarnSeconds, p.LagStopSeconds, 604800)
	if len(p.RequiredStorage) == 0 {
		r.add("policy.storage", hold, "not_observed")
	}
	if len(p.RequiredStorage) > 6 {
		r.add("policy.storage", fail, "storage_limit_exceeded")
		return
	}
	seen := map[string]bool{}
	for _, k := range p.RequiredStorage {
		if !storageKind(k) || seen[k] {
			r.add("policy.storage", fail, "invalid_or_duplicate_kind")
		}
		seen[k] = true
	}
}

func (r *Report) fresh(field string, observed, at *time.Time, p *Policy) {
	if observed == nil || observed.IsZero() {
		r.add(field, hold, "not_observed")
		return
	}
	if at == nil || at.IsZero() {
		r.add(field, hold, "evaluation_time_missing")
		return
	}
	if observed.After(*at) {
		r.add(field, fail, "future_observation")
		return
	}
	if p == nil || p.MaxAgeSeconds == nil || *p.MaxAgeSeconds == 0 || *p.MaxAgeSeconds > 86400 {
		r.add(field, hold, "age_limit_unavailable")
		return
	}
	if at.Sub(*observed) > time.Duration(*p.MaxAgeSeconds)*time.Second {
		r.add(field, hold, "stale_observation")
	} else {
		r.add(field, pass, "within_declared_age")
	}
}

func (r *Report) threshold(field string, value uint64, w, s *uint64) {
	switch {
	case s != nil && *s > 0 && value >= *s:
		r.add(field, fail, "stop_threshold_reached")
	case w != nil && *w > 0 && value >= *w:
		r.add(field, warn, "warning_threshold_reached")
	case w == nil || s == nil || *w == 0 || *s == 0 || *w >= *s:
		r.add(field, hold, "threshold_unavailable")
	default:
		r.add(field, pass, "below_warning_threshold")
	}
}
func productGE(a, b, c, d uint64) bool {
	ah, al := bits.Mul64(a, b)
	bh, bl := bits.Mul64(c, d)
	return ah > bh || ah == bh && al >= bl
}

func (r *Report) storage(p *Policy, m *Metrics) {
	seen := map[string]bool{}
	if len(m.Storage) > 6 {
		r.add("storage", fail, "storage_limit_exceeded")
		return
	}
	for _, v := range m.Storage {
		if !storageKind(v.Kind) || seen[v.Kind] {
			r.add("storage", fail, "invalid_or_duplicate_kind")
			continue
		}
		seen[v.Kind] = true
		field := "storage." + v.Kind
		if v.Kind == "filesystem" || v.Kind == "docker_vm" {
			if v.Free == nil {
				r.add(field+".free", hold, "not_observed")
			} else if p == nil || p.MinFreeBytes == nil || *p.MinFreeBytes == 0 {
				r.add(field+".free", hold, "threshold_unavailable")
			} else if *v.Free <= *p.MinFreeBytes {
				r.add(field+".free", fail, "free_floor_reached")
			} else {
				r.add(field+".free", pass, "above_free_floor")
			}
			if v.Used != nil || v.Limit != nil {
				r.add(field, fail, "unexpected_budget_fields")
			}
			continue
		}
		if v.Free != nil {
			r.add(field, fail, "unexpected_free_field")
		}
		if v.Used == nil || v.Limit == nil {
			r.add(field+".usage", hold, "not_observed")
			continue
		}
		if *v.Limit == 0 {
			r.add(field+".usage", fail, "invalid_limit")
			continue
		}
		if *v.Used > *v.Limit {
			r.add(field+".usage", fail, "budget_exceeded")
			continue
		}
		if p == nil {
			r.add(field+".usage", hold, "threshold_unavailable")
			continue
		}
		switch {
		case p.CapacityStopBPS != nil && *p.CapacityStopBPS > 0 && productGE(*v.Used, 10000, *v.Limit, *p.CapacityStopBPS):
			r.add(field+".usage", fail, "stop_threshold_reached")
		case p.CapacityWarnBPS != nil && *p.CapacityWarnBPS > 0 && productGE(*v.Used, 10000, *v.Limit, *p.CapacityWarnBPS):
			r.add(field+".usage", warn, "warning_threshold_reached")
		case p.CapacityWarnBPS == nil || p.CapacityStopBPS == nil:
			r.add(field+".usage", hold, "threshold_unavailable")
		default:
			r.add(field+".usage", pass, "below_warning_threshold")
		}
	}
	if p != nil {
		for _, k := range p.RequiredStorage {
			if storageKind(k) && !seen[k] {
				r.add("storage."+k, hold, "not_observed")
			}
		}
	}
}

func (r *Report) pipeline(p *Policy, q *Pipeline) {
	if q == nil {
		r.add("pipeline", hold, "not_observed")
		return
	}
	fields := []struct {
		name string
		v    *uint64
	}{{"head", q.Head}, {"received", q.Received}, {"processed", q.Processed}, {"ack", q.ACK}}
	for _, f := range fields {
		if f.v == nil {
			r.add("pipeline."+f.name, hold, "not_observed")
		} else if *f.v > math.MaxInt64 {
			r.add("pipeline."+f.name, fail, "invalid_height")
		}
	}
	if q.Generation == nil {
		r.add("pipeline.generation", hold, "not_observed")
	} else if *q.Generation == 0 || *q.Generation > math.MaxInt64 {
		r.add("pipeline.generation", fail, "invalid_generation")
	}
	knownGeneration := q.Generation != nil && *q.Generation > 0 && *q.Generation <= math.MaxInt64
	for i, a := range fields {
		for _, b := range fields[i+1:] {
			if knownGeneration && a.v != nil && b.v != nil && *a.v < *b.v {
				r.add("pipeline.order", fail, "height_order_conflict")
			}
		}
	}
	var bw, bs, lw, ls *uint64
	if p != nil {
		bw, bs, lw, ls = p.BacklogWarnBlocks, p.BacklogStopBlocks, p.LagWarnSeconds, p.LagStopSeconds
	}
	for _, g := range []struct {
		field  string
		hi, lo *uint64
	}{{"receive_gap", q.Head, q.Received}, {"backlog", q.Received, q.Processed}, {"ack_gap", q.Processed, q.ACK}} {
		if knownGeneration && g.hi != nil && g.lo != nil && *g.hi >= *g.lo && *g.hi <= math.MaxInt64 {
			r.threshold("pipeline."+g.field, *g.hi-*g.lo, bw, bs)
		}
	}
	if !knownGeneration {
		r.add("pipeline.gaps", hold, "generation_unavailable")
	}
	for _, v := range []struct {
		field string
		value *uint64
	}{{"oldest_pending", q.OldestPendingSeconds}, {"ack_lag", q.ACKLagSeconds}, {"worker_lag", q.WorkerLagSeconds}} {
		if v.value == nil {
			r.add("pipeline."+v.field, hold, "not_observed")
		} else {
			r.threshold("pipeline."+v.field, *v.value, lw, ls)
		}
	}
}

func Evaluate(ctx context.Context, p *Policy, s *Snapshot, at *time.Time) Report {
	r := newReport()
	if ctx.Err() != nil {
		r.add("input", fail, "cancelled")
		return r
	}
	r.policy(p)
	if at == nil || at.IsZero() {
		r.add("evaluation.time", hold, "not_observed")
	}
	if s == nil {
		r.add("snapshot", hold, "not_observed")
		return r
	}
	if s.Version != 1 {
		r.add("snapshot", fail, "unsupported_version")
	}
	r.identity("snapshot.identity", s.Identity)
	if p != nil && p.Expected != nil && s.Identity != nil && *p.Expected != *s.Identity {
		r.add("snapshot.identity", fail, "identity_mismatch")
	}
	r.diagnostics(p, s, at)
	if s.Metrics == nil {
		r.add("metrics", hold, "not_observed")
	} else {
		r.fresh("metrics.time", s.Metrics.ObservedAt, at, p)
		r.pipeline(p, s.Metrics.Pipeline)
		if q := s.Metrics.Pipeline; q != nil && s.Identity != nil {
			for _, height := range []*uint64{q.Head, q.Received, q.Processed, q.ACK} {
				if height != nil && *height < s.Identity.Anchor.Height {
					r.add("pipeline.anchor", fail, "height_below_original_anchor")
				}
			}
			if q.Generation != nil && *q.Generation > 0 && *q.Generation < s.Identity.Anchor.Generation {
				r.add("pipeline.anchor", fail, "generation_before_original_anchor")
			}
		}
		r.storage(p, s.Metrics)
	}
	if ctx.Err() != nil {
		r.add("input", fail, "cancelled")
	}
	return r
}
