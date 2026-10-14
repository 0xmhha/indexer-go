package fetch

import (
	"sort"
	"sync"
	"time"
)

const (
	stageFetch = iota
	stagePrepare
	stageWrite
	pipelineSampleWindow = 256
)

// StageMetrics are cumulative counts and a bounded recent latency window.
// Durations include RPC limiter/transport waits. Write includes queue wait,
// canonical validation and the whole transaction, not just database commit.
type StageMetrics struct {
	Count   uint64  `json:"count"`
	Errors  uint64  `json:"errors"`
	TotalMS float64 `json:"total_ms"`
	P50MS   float64 `json:"p50_ms"`
	P95MS   float64 `json:"p95_ms"`
	P99MS   float64 `json:"p99_ms"`
}

type PipelineMetrics struct {
	Fetch          StageMetrics `json:"fetch_decode"`
	Prepare        StageMetrics `json:"state_prepare"`
	Write          StageMetrics `json:"ordered_write"`
	InFlight       int64        `json:"in_flight"`
	PeakInFlight   int64        `json:"peak_in_flight"`
	StateBytes     int64        `json:"prepared_state_bytes"`
	PeakStateBytes int64        `json:"peak_prepared_state_bytes"`
}

type stageSamples struct {
	count, errors uint64
	total         time.Duration
	samples       []time.Duration
	next          int
}
type pipelineMetrics struct {
	enabled                                            bool // immutable after construction
	mu                                                 sync.Mutex
	stages                                             [3]stageSamples
	inFlight, peakInFlight, stateBytes, peakStateBytes int64
}

func (m *pipelineMetrics) record(stage int, elapsed time.Duration, failed bool) {
	if !m.enabled {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &m.stages[stage]
	s.count++
	s.total += elapsed
	if failed {
		s.errors++
	}
	if len(s.samples) < pipelineSampleWindow {
		s.samples = append(s.samples, elapsed)
	} else {
		s.samples[s.next] = elapsed
		s.next = (s.next + 1) % pipelineSampleWindow
	}
}
func (m *pipelineMetrics) addInFlight(n int64) {
	if !m.enabled {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inFlight += n
	m.peakInFlight = max(m.peakInFlight, m.inFlight)
}
func (m *pipelineMetrics) addStateBytes(n int64) {
	if !m.enabled {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stateBytes += n
	m.peakStateBytes = max(m.peakStateBytes, m.stateBytes)
}

func (m *pipelineMetrics) start() time.Time {
	if !m.enabled {
		return time.Time{}
	}
	return time.Now()
}
func (m *pipelineMetrics) finish(stage int, started time.Time, failed bool) {
	if m.enabled {
		m.record(stage, time.Since(started), failed)
	}
}

// GetPipelineMetrics returns chain-local preparation/queue measurements.
// StateBytes is accounted payload, not a measurement of the entire Go heap.
func (f *Fetcher) GetPipelineMetrics() PipelineMetrics {
	m := &f.pipelineMetrics
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := func(s stageSamples) StageMetrics {
		out := StageMetrics{Count: s.count, Errors: s.errors, TotalMS: float64(s.total) / float64(time.Millisecond)}
		samples := append([]time.Duration(nil), s.samples...)
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		if len(samples) > 0 {
			percentile := func(p int) float64 {
				i := (len(samples)*p+99)/100 - 1
				return float64(samples[i]) / float64(time.Millisecond)
			}
			out.P50MS = percentile(50)
			out.P95MS = percentile(95)
			out.P99MS = percentile(99)
		}
		return out
	}
	return PipelineMetrics{Fetch: snapshot(m.stages[stageFetch]), Prepare: snapshot(m.stages[stagePrepare]), Write: snapshot(m.stages[stageWrite]), InFlight: m.inFlight, PeakInFlight: m.peakInFlight, StateBytes: m.stateBytes, PeakStateBytes: m.peakStateBytes}
}
