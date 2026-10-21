package opsguard

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/window"
)

// Level is the state of one monitored value.
type Level string

const (
	LevelNormal  Level = "normal"
	LevelWarning Level = "warning"
	LevelStop    Level = "stop"
	LevelUnknown Level = "unknown"
)

// Thresholds that are fractions of a budget.
const (
	warnRatio    = 0.80
	prepareRatio = 0.90
	rawStopRatio = 0.95
	// rawStopForecast: a raw window filling within this halts. Raw space is
	// only reclaimed by collection, so gross appends are the growth rate.
	rawStopForecast = time.Hour
	// warnSamples consecutive valid samples raise a warning; stops need one.
	warnSamples = 2
)

// Record is one journal line.
type Record struct {
	At         time.Time `json:"at"`
	Kind       string    `json:"kind"`
	Level      Level     `json:"level"`
	Bytes      *int64    `json:"bytes,omitempty"`
	LimitBytes int64     `json:"limitBytes,omitempty"`
	ForecastS  *float64  `json:"forecastSeconds,omitempty"`
	Name       string    `json:"name,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// Option changes a Monitor's clock or probes, for tests.
type Option func(*Monitor)

// WithClock sets the clock.
func WithClock(now func() time.Time) Option { return func(m *Monitor) { m.now = now } }

// WithProbes replaces the filesystem and directory size probes.
func WithProbes(free func(string) (uint64, error), tree func(context.Context, string, int) (int64, error)) Option {
	return func(m *Monitor) { m.free, m.tree = free, tree }
}

// WithLogger sets the logger.
func WithLogger(l *zap.Logger) Option { return func(m *Monitor) { m.logger = l } }

// Monitor samples capacity and halts once. It is safe for concurrent use.
type Monitor struct {
	cfg     CapacityConfig
	latch   string
	journal *Journal
	logger  *zap.Logger
	now     func() time.Time
	free    func(string) (uint64, error)
	tree    func(context.Context, string, int) (int64, error)
	onHalt  func(*HaltError)

	mu      sync.Mutex
	started time.Time
	fs, pg  series
	raw     map[string]*series // gross appended bytes per reservation
	stats   []*statsSource     // attached PostgreSQL statistics, latest last
	fsSeen  time.Time
	pgSeen  time.Time
	counts  map[string]int
	levels  map[string]Level
	halted  *HaltError
	done    chan struct{}
}

// NewMonitor returns a monitor of a validated configuration with defaults.
// onHalt runs once, after the latch is written; it must return promptly.
func NewMonitor(cfg Config, onHalt func(*HaltError), opts ...Option) (*Monitor, error) {
	if cfg.Capacity == nil || cfg.HaltFile == "" {
		return nil, errors.New("opsguard: capacity monitoring needs operations.capacity and halt_file")
	}
	m := &Monitor{cfg: *cfg.Capacity, latch: cfg.HaltFile, logger: zap.NewNop(), now: time.Now, free: FreeBytes, tree: TreeBytes, onHalt: onHalt, counts: map[string]int{}, levels: map[string]Level{}, raw: map[string]*series{}, done: make(chan struct{})}
	for _, o := range opts {
		o(m)
	}
	m.started = m.now()
	if cfg.JournalFile != "" {
		j, err := OpenJournal(cfg.JournalFile, cfg.JournalMaxBytes)
		if err != nil {
			return nil, err
		}
		m.journal = j
	}
	return m, nil
}

// Close closes the journal. It does not remove a latch.
func (m *Monitor) Close() error { return m.journal.Close() }

// Halted returns the halt, or nil.
func (m *Monitor) Halted() *HaltError {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.halted
}

// Done is closed when the monitor halts.
func (m *Monitor) Done() <-chan struct{} { return m.done }

// Run samples until ctx ends or the monitor halts, and returns the halt.
// A probe that blocks, such as statfs on a vanished mount, is not waited for:
// the missing observation halts once it exceeds its limit. A statistics
// query is cancelled and waited for before Run returns.
func (m *Monitor) Run(ctx context.Context) error {
	fsResult := make(chan fsProbe, 1)
	pgResult := make(chan pgProbe, 1)
	fsBusy, pgBusy := false, false
	var statsWG sync.WaitGroup
	defer statsWG.Wait()
	statsCtx, cancelStats := context.WithCancel(ctx)
	defer cancelStats()
	statsDone := make(chan struct{}, 1)
	statsBusy := false
	startStats := func() {
		if statsBusy {
			return
		}
		statsBusy = true
		statsWG.Add(1)
		go func() {
			defer statsWG.Done()
			m.sampleStats(statsCtx)
			statsDone <- struct{}{}
		}()
	}
	startFS := func() {
		if fsBusy {
			return
		}
		fsBusy = true
		go func() { v, err := m.free(m.cfg.FilesystemPath); fsResult <- fsProbe{at: m.now(), free: v, err: err} }()
	}
	startPG := func() {
		if pgBusy || m.cfg.PostgresDataDir == "" {
			return
		}
		pgBusy = true
		go func() {
			v, err := m.tree(ctx, m.cfg.PostgresDataDir, m.cfg.PostgresMaxEntries)
			pgResult <- pgProbe{at: m.now(), bytes: v, err: err}
		}()
	}
	fsTick := time.NewTicker(m.cfg.FilesystemInterval)
	defer fsTick.Stop()
	pgTick := time.NewTicker(m.cfg.PostgresInterval)
	defer pgTick.Stop()
	statsTick := time.NewTicker(m.cfg.PostgresStatsInterval)
	defer statsTick.Stop()
	startFS()
	startPG()
	startStats()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-m.done:
			return m.Halted()
		case p := <-fsResult:
			fsBusy = false
			m.ObserveFilesystem(p.at, p.free, p.err)
		case p := <-pgResult:
			pgBusy = false
			m.ObservePostgres(p.at, p.bytes, p.err)
		case <-statsDone:
			statsBusy = false
		case <-fsTick.C:
			m.checkMissing()
			startFS()
		case <-pgTick.C:
			m.checkMissing()
			startPG()
		case <-statsTick.C:
			startStats()
		}
	}
}

type fsProbe struct {
	at   time.Time
	free uint64
	err  error
}

type pgProbe struct {
	at    time.Time
	bytes int64
	err   error
}

// ObserveFilesystem applies one filesystem sample.
func (m *Monitor) ObserveFilesystem(at time.Time, free uint64, err error) {
	if err != nil {
		m.record(Record{At: at, Kind: string(SourceFilesystem), Level: LevelUnknown, Error: err.Error()})
		m.checkMissing()
		return
	}
	v := int64(min(free, uint64(1<<62)))
	m.mu.Lock()
	m.fs.add(at, v)
	m.fsSeen = at
	eta, known := m.fs.forecast(v-m.cfg.FloorFreeBytes, -1)
	m.mu.Unlock()
	rec := Record{At: at, Kind: string(SourceFilesystem), Bytes: &v, LimitBytes: m.cfg.StopFreeBytes, ForecastS: seconds(eta, known)}
	switch {
	case v <= m.cfg.StopFreeBytes:
		rec.Level = LevelStop
		m.record(rec)
		m.halt(&HaltError{Reason: ReasonCapacity, Source: SourceFilesystem, Detail: fmt.Sprintf("free %d bytes at or below stop_free_bytes %d", v, m.cfg.StopFreeBytes)})
	case known && eta <= m.cfg.StopForecast:
		rec.Level = LevelStop
		m.record(rec)
		m.halt(&HaltError{Reason: ReasonCapacity, Source: SourceFilesystem, Detail: fmt.Sprintf("free space reaches floor_free_bytes %d in %s", m.cfg.FloorFreeBytes, eta.Round(time.Second))})
	default:
		rec.Level = m.warnLevel(string(SourceFilesystem), v <= m.cfg.WarnFreeBytes)
		m.record(rec)
	}
}

// ObservePostgres applies one data directory size sample.
func (m *Monitor) ObservePostgres(at time.Time, used int64, err error) {
	if err != nil {
		m.record(Record{At: at, Kind: string(SourcePostgres), Level: LevelUnknown, Error: err.Error()})
		m.checkMissing()
		return
	}
	budget := m.cfg.PostgresBudgetBytes
	m.mu.Lock()
	m.pg.add(at, used)
	m.pgSeen = at
	eta, known := m.pg.forecast(budget-used, 1)
	m.mu.Unlock()
	rec := Record{At: at, Kind: string(SourcePostgres), Bytes: &used, LimitBytes: budget, ForecastS: seconds(eta, known)}
	ratio := float64(used) / float64(budget)
	switch {
	case used >= budget:
		rec.Level = LevelStop
		m.record(rec)
		m.halt(&HaltError{Reason: ReasonCapacity, Source: SourcePostgres, Detail: fmt.Sprintf("data directory %d bytes at or above postgres_budget_bytes %d", used, budget)})
	case known && eta <= m.cfg.StopForecast:
		rec.Level = LevelStop
		m.record(rec)
		m.halt(&HaltError{Reason: ReasonCapacity, Source: SourcePostgres, Detail: fmt.Sprintf("data directory reaches postgres_budget_bytes in %s", eta.Round(time.Second))})
	default:
		rec.Level = m.warnLevel(string(SourcePostgres), ratio >= warnRatio)
		if ratio >= prepareRatio && m.transition("postgres-prepare", LevelWarning) {
			m.logger.Warn("PostgreSQL data directory above 90% of its budget; prepare a stop or more capacity", zap.Int64("bytes", used), zap.Int64("budget", budget))
		}
		m.record(rec)
	}
}

// ObserveRaw applies one raw window usage, as the capture session reports it.
func (m *Monitor) ObserveRaw(u window.Usage) {
	if u.LimitBytes <= 0 {
		return
	}
	at := m.now()
	used := u.FileBytes + u.IndexReserveBytes
	ratio := float64(used) / float64(u.LimitBytes)
	m.mu.Lock()
	s := m.raw[u.Reservation]
	if s == nil {
		s = &series{}
		m.raw[u.Reservation] = s
	}
	if n := len(s.s); n > 0 && u.AppendedBytes < s.s[n-1].v {
		s.s = s.s[:0] // a reopened window restarts its counter
	}
	s.add(at, u.AppendedBytes)
	eta, known := s.forecast(int64(rawStopRatio*float64(u.LimitBytes))-used, 1)
	m.mu.Unlock()
	rec := Record{At: at, Kind: string(SourceRaw), Name: u.Reservation, Bytes: &used, LimitBytes: u.LimitBytes, ForecastS: seconds(eta, known)}
	switch {
	case ratio >= rawStopRatio:
		rec.Level = LevelStop
		m.record(rec)
		m.halt(&HaltError{Reason: ReasonCapacity, Source: SourceRaw, Detail: fmt.Sprintf("raw window %s uses %d of %d bytes", u.Reservation, used, u.LimitBytes)})
		return
	case known && eta <= rawStopForecast:
		rec.Level = LevelStop
		m.record(rec)
		m.halt(&HaltError{Reason: ReasonCapacity, Source: SourceRaw, Detail: fmt.Sprintf("raw window %s reaches 95%% in %s", u.Reservation, eta.Round(time.Second))})
		return
	}
	rec.Level = m.warnLevel("raw/"+u.Reservation, ratio >= warnRatio)
	m.record(rec)
}

// checkMissing halts when a required value has not been observed in time.
func (m *Monitor) checkMissing() {
	now := m.now()
	m.mu.Lock()
	fsSeen, pgSeen := m.fsSeen, m.pgSeen
	if fsSeen.IsZero() {
		fsSeen = m.started
	}
	if pgSeen.IsZero() {
		pgSeen = m.started
	}
	m.mu.Unlock()
	if gap := now.Sub(fsSeen); gap > m.cfg.FilesystemMissingStop {
		m.halt(&HaltError{Reason: ReasonCapacity, Source: SourceFilesystem, Detail: fmt.Sprintf("free space unobserved for %s", gap.Round(time.Second))})
		return
	}
	if m.cfg.PostgresDataDir == "" {
		return
	}
	if gap := now.Sub(pgSeen); gap > m.cfg.PostgresMissingStop {
		m.halt(&HaltError{Reason: ReasonCapacity, Source: SourcePostgres, Detail: fmt.Sprintf("data directory size unobserved for %s", gap.Round(time.Second))})
	}
}

// warnLevel requires warnSamples consecutive high samples and logs changes.
func (m *Monitor) warnLevel(name string, high bool) Level {
	m.mu.Lock()
	if high {
		m.counts[name]++
	} else {
		m.counts[name] = 0
	}
	level := LevelNormal
	if m.counts[name] >= warnSamples {
		level = LevelWarning
	}
	m.mu.Unlock()
	if m.transition(name, level) {
		if level == LevelWarning {
			m.logger.Warn("capacity warning", zap.String("value", name))
		} else {
			m.logger.Info("capacity warning cleared", zap.String("value", name))
		}
	}
	return level
}

// transition records a level and reports whether it changed.
func (m *Monitor) transition(name string, level Level) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.levels[name]
	m.levels[name] = level
	if !ok {
		return level != LevelNormal
	}
	return old != level
}

func (m *Monitor) halt(h *HaltError) {
	m.mu.Lock()
	if m.halted != nil {
		m.mu.Unlock()
		return
	}
	m.halted = h
	m.mu.Unlock()
	at := m.now()
	if err := WriteLatch(m.latch, Latch{Reason: h.Reason, Source: h.Source, Detail: h.Detail, At: at}); err != nil {
		// The process still stops; the operator sees why the latch is missing.
		m.logger.Error("failed to write halt latch", zap.String("path", m.latch), zap.Error(err))
	}
	m.record(Record{At: at, Kind: "halt", Level: LevelStop, Name: string(h.Source), Error: h.Detail})
	m.logger.Error("capacity halt", zap.String("source", string(h.Source)), zap.String("detail", h.Detail))
	close(m.done)
	if m.onHalt != nil {
		m.onHalt(h)
	}
}

func (m *Monitor) record(r Record) {
	if err := m.journal.Write(r); err != nil {
		m.logger.Warn("capacity journal write failed", zap.Error(err))
	}
}

func seconds(d time.Duration, known bool) *float64 {
	if !known {
		return nil
	}
	v := d.Seconds()
	return &v
}
