// Package feedtelemetry keeps bounded, process-local diagnostics. It never
// owns source cursors, database transactions, payloads or RPC requests.
package feedtelemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
)

type Stage uint8

const (
	ReceiveNext Stage = iota
	RawAppend
	RawQueue
	Assemble
	CaptureCommit
	CaptureInterval
	ReceiveInterval
	ProofQueue
	ProofAcquire
	ProofBatch
	ProofVerify
	ProofCommit
	StateQueue
	StateApply
	RPCLimitWait
	RPCHTTP
	TraceAcquire
	TraceBatch
	TraceVerify
	TraceCommit
	BlobAcquire
	BlobSupply
	BlobVerify
	BlobCommit
	LookupVerify
	LookupCommit
	SearchVerify
	SearchCommit
	stages
)

var names = [stages]string{"receiveNext", "rawAppend", "rawQueue", "assemble", "captureCommit", "captureInterval", "receiveBlockInterval", "proofQueue", "proofAcquire", "proofBatch", "proofVerify", "proofCommit", "stateQueue", "stateApply", "rpcLimitWait", "rpcHTTP", "traceAcquire", "traceBatch", "traceVerify", "traceCommit", "blobAcquire", "blobSupply", "blobVerify", "blobCommit", "lookupVerify", "lookupCommit", "searchVerify", "searchCommit"}

const Samples = 256
const Points = 1024

type Phase uint8

const (
	Captured Phase = iota
	Proved
	Projected
	phases
)

var phaseNames = [phases]string{"captured", "proved", "projected"}

type metric struct {
	count, failures    uint64
	total              time.Duration
	recent             [Samples]time.Duration
	used, next, active int
	busy               time.Time
}
type point struct {
	id model.CaptureIdentity
	at time.Time
}
type progress struct {
	last       *point
	entries    map[model.CaptureIdentity]time.Time
	ring       [Points]model.CaptureIdentity
	used, next int
}
type receivedPoint struct {
	block model.FeedBlock
	at    time.Time
}
type Observed struct {
	Block model.FeedBlock `json:"block"`
	At    time.Time       `json:"at"`
	AgeMS float64         `json:"ageMs"`
}
type Recorder struct {
	mu       sync.Mutex
	anchor   model.FeedBlock
	plan     common.Hash
	id       string
	target   time.Duration
	received *receivedPoint
	started  time.Time
	stopped  *time.Time
	metrics  [stages]metric
	progress [phases]progress
}
type Metric struct {
	Count         uint64   `json:"count"`
	Failures      uint64   `json:"failures"`
	TotalMS       float64  `json:"totalMs"`
	RecentSamples int      `json:"recentSamples"`
	P50MS         *float64 `json:"p50Ms"`
	P95MS         *float64 `json:"p95Ms"`
	P99MS         *float64 `json:"p99Ms"`
	InFlight      int      `json:"inFlight"`
	BusyForMS     *float64 `json:"busyForMs"`
}
type Progress struct {
	Input       model.CaptureIdentity `json:"input"`
	CompletedAt time.Time             `json:"completedAt"`
	AgeMS       float64               `json:"ageMs"`
}
type Snapshot struct {
	Availability            string              `json:"availability"`
	Observed                *Observed           `json:"observedHeader,omitempty"`
	ReceiveIntervalTargetMS float64             `json:"receiveIntervalTargetMs"`
	ReceiveAssessment       string              `json:"receiveAssessment"`
	RunID                   string              `json:"runId"`
	StartedAt               *time.Time          `json:"startedAt,omitempty"`
	StoppedAt               *time.Time          `json:"stoppedAt,omitempty"`
	Metrics                 map[string]Metric   `json:"metrics"`
	Progress                map[string]Progress `json:"progress"`
}

var registry = struct {
	sync.Mutex
	entries map[string]*Recorder
}{entries: map[string]*Recorder{}}

func New(anchor model.FeedBlock, plan model.CapturePlan, target time.Duration) (*Recorder, error) {
	if target == 0 {
		target = 450 * time.Millisecond
	}
	if target < 0 || target > time.Minute {
		return nil, errors.New("telemetry: invalid target")
	}
	digest, e := plan.Digest()
	if e != nil || anchor.Validate() != nil {
		return nil, errors.New("telemetry: invalid scope")
	}
	var id [16]byte
	if _, e = rand.Read(id[:]); e != nil {
		return nil, e
	}
	r := &Recorder{anchor: anchor, plan: digest, id: hex.EncodeToString(id[:]), started: time.Now(), target: target}
	registry.Lock()
	defer registry.Unlock()
	if len(registry.entries) >= 64 {
		return nil, errors.New("telemetry: process runtime limit reached")
	}
	if registry.entries[r.id] != nil {
		return nil, errors.New("telemetry: duplicate runtime")
	}
	registry.entries[r.id] = r
	return r, nil
}
func (r *Recorder) Descriptor() *model.FeedTelemetryDescriptor {
	if r == nil {
		return nil
	}
	return &model.FeedTelemetryDescriptor{RunID: r.id, ReceiveIntervalTargetMS: ms(r.target)}
}

// Stop preserves a stopped snapshot until Close; it does not claim all work completed.
func (r *Recorder) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped == nil {
		now := time.Now()
		r.stopped = &now
	}
}
func (r *Recorder) Close() {
	if r == nil {
		return
	}
	r.Stop()
	registry.Lock()
	defer registry.Unlock()
	if registry.entries[r.id] == r {
		delete(registry.entries, r.id)
	}
}

type contextKey struct{}

func WithRecorder(ctx context.Context, r *Recorder) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, r)
}
func From(ctx context.Context) *Recorder { r, _ := ctx.Value(contextKey{}).(*Recorder); return r }

type Span struct {
	r     *Recorder
	stage Stage
	start time.Time
}

func (r *Recorder) Start(s Stage) Span {
	if r == nil || s >= stages {
		return Span{}
	}
	now := time.Now()
	r.mu.Lock()
	m := &r.metrics[s]
	if m.active == 0 {
		m.busy = now
	}
	m.active++
	r.mu.Unlock()
	return Span{r: r, stage: s, start: now}
}
func (s Span) Discard() {
	if s.r == nil {
		return
	}
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	s.r.metrics[s.stage].active--
}
func (s Span) End(err error) {
	if s.r == nil {
		return
	}
	r := s.r
	r.mu.Lock()
	defer r.mu.Unlock()
	m := &r.metrics[s.stage]
	m.active--
	m.record(time.Since(s.start), err)
}
func (m *metric) record(d time.Duration, err error) {
	m.count++
	if err != nil {
		m.failures++
	}
	m.total += d
	m.recent[m.next] = d
	m.next = (m.next + 1) % Samples
	if m.used < Samples {
		m.used++
	}
}
func (r *Recorder) Record(s Stage, d time.Duration) {
	if r == nil || s >= stages || d < 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics[s].record(d, nil)
}

// QueueAge records only a known, exact input from this run. Eviction/restart is
// missing information, never zero. Call on successful downstream completion,
// using its operation start so work time is excluded from queue time.
func (r *Recorder) QueueAge(from Phase, stage Stage, id model.CaptureIdentity, at time.Time) {
	if r == nil || from >= phases || stage >= stages {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.progress[from].entries[id]; ok && !at.Before(t) {
		r.metrics[stage].record(at.Sub(t), nil)
	}
}
func (r *Recorder) Complete(p Phase, id model.CaptureIdentity) {
	if r == nil || p >= phases || id.Block.Chain != r.anchor.Chain || id.PlanDigest != r.plan {
		return
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	g := &r.progress[p]
	if g.entries == nil {
		g.entries = map[model.CaptureIdentity]time.Time{}
	}
	if _, ok := g.entries[id]; ok {
		return
	}
	if g.used == Points {
		delete(g.entries, g.ring[g.next])
	} else {
		g.used++
	}
	g.entries[id] = now
	g.ring[g.next] = id
	g.next = (g.next + 1) % Points
	if g.last != nil && g.last.id.Generation == id.Generation && g.last.id.Block.Number >= id.Block.Number {
		return
	}
	if p == Captured && g.last != nil && g.last.id.Generation == id.Generation && id.Block.Number == g.last.id.Block.Number+1 {
		r.metrics[CaptureInterval].record(now.Sub(g.last.at), nil)
	}
	g.last = &point{id: id, at: now}
}
func (r *Recorder) Observe(b model.FeedBlock) {
	if r == nil || b.Chain != r.anchor.Chain {
		return
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.received != nil {
		old := r.received
		if old.block == b {
			return
		}
		if b.Number > 0 && old.block.Number == b.Number-1 {
			r.metrics[ReceiveInterval].record(now.Sub(old.at), nil)
		}
	}
	r.received = &receivedPoint{block: b, at: now}
}
func Read(d *model.FeedTelemetryDescriptor, anchor model.FeedBlock, plan common.Hash) Snapshot {
	out := Snapshot{Availability: "disabled", ReceiveAssessment: "unmeasured", Metrics: map[string]Metric{}, Progress: map[string]Progress{}}
	if d == nil {
		return out
	}
	out.RunID = d.RunID
	out.ReceiveIntervalTargetMS = d.ReceiveIntervalTargetMS
	out.ReceiveAssessment = "unmeasured"
	out.Availability = "runtime_unavailable"
	registry.Lock()
	r := registry.entries[d.RunID]
	registry.Unlock()
	if r == nil || r.anchor != anchor || r.plan != plan {
		return out
	}
	return r.snapshot()
}
func (r *Recorder) snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	started := r.started
	var stopped *time.Time
	if r.stopped != nil {
		v := *r.stopped
		stopped = &v
	}
	out := Snapshot{Availability: "available", RunID: r.id, StartedAt: &started, StoppedAt: stopped, Metrics: map[string]Metric{}, Progress: map[string]Progress{}}
	out.ReceiveIntervalTargetMS = ms(r.target)
	out.ReceiveAssessment = "unmeasured"
	if r.received != nil {
		out.Observed = &Observed{Block: r.received.block, At: r.received.at, AgeMS: ms(now.Sub(r.received.at))}
	}
	if r.stopped != nil {
		out.Availability = "stopped"
	}
	for s := Stage(0); s < stages; s++ {
		m := r.metrics[s]
		v := Metric{Count: m.count, Failures: m.failures, TotalMS: ms(m.total), RecentSamples: m.used, InFlight: m.active}
		if m.active > 0 {
			d := ms(now.Sub(m.busy))
			v.BusyForMS = &d
		}
		if m.used > 0 {
			a := append([]time.Duration{}, m.recent[:m.used]...)
			sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
			quantile := func(p int) *float64 { i := (p*len(a)+99)/100 - 1; v := ms(a[i]); return &v }
			v.P50MS = quantile(50)
			v.P95MS = quantile(95)
			v.P99MS = quantile(99)
		}
		out.Metrics[names[s]] = v
	}
	if v := out.Metrics["receiveBlockInterval"]; v.RecentSamples >= 2 && v.P95MS != nil {
		out.ReceiveAssessment = "within_target"
		if *v.P95MS > ms(r.target) {
			out.ReceiveAssessment = "above_target"
		}
	}
	for p := Phase(0); p < phases; p++ {
		if v := r.progress[p].last; v != nil {
			out.Progress[phaseNames[p]] = Progress{Input: v.id, CompletedAt: v.at, AgeMS: ms(now.Sub(v.at))}
		}
	}
	return out
}
func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func (r *Recorder) RecordResult(s Stage, d time.Duration, err error) {
	if r == nil || s >= stages || d < 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics[s].record(d, err)
}

// PendingAge is unknown when an exact current-epoch point was not observed or
// has left the bounded ledger. It never estimates remote state retention.
func PendingAge(d *model.FeedTelemetryDescriptor, anchor model.FeedBlock, plan common.Hash, phase Phase, generation, height uint64) *float64 {
	if d == nil || phase >= phases {
		return nil
	}
	registry.Lock()
	r := registry.entries[d.RunID]
	registry.Unlock()
	if r == nil || r.anchor != anchor || r.plan != plan {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, at := range r.progress[phase].entries {
		if id.Generation == generation && id.Block.Number == height {
			v := ms(time.Since(at))
			return &v
		}
	}
	return nil
}
