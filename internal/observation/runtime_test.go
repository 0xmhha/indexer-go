package observation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/nativealerts"
	"github.com/0xmhha/indexer-go/internal/nativeobserve"
	"github.com/0xmhha/indexer-go/internal/observeconfig"
	"github.com/0xmhha/indexer-go/internal/opsguard"
	"github.com/stretchr/testify/require"
)

func policy() observeconfig.Config {
	return observeconfig.Config{Enabled: true, Initialize: true, CheckpointDir: "/private/observer", Sink: "disabled", OperatorID: strings.Repeat("55", 32), Database: observeconfig.Resource{ID: strings.Repeat("11", 32), Series: strings.Repeat("22", 32), BudgetBytes: 1000}, WAL: observeconfig.Resource{ID: strings.Repeat("33", 32), Series: strings.Repeat("44", 32), BudgetBytes: 1000}, Warn: 8000, Stop: 9000, Clear: 7000, Interval: time.Second, MaxAge: 3 * time.Second, WarnCount: 2, ClearCount: 2, WarnDwell: time.Second, ClearDwell: time.Second}
}

type memoryStore struct {
	snapshot nativealerts.Snapshot
	exists   bool
	fail     atomic.Bool
	closed   atomic.Bool
	writes   atomic.Int32
}

func (s *memoryStore) Read(context.Context) (nativealerts.Snapshot, error) {
	if !s.exists {
		return nativealerts.Snapshot{}, nativealerts.ErrCheckpoint
	}
	return s.snapshot, nil
}
func (s *memoryStore) Write(_ context.Context, v nativealerts.Snapshot) error {
	if s.fail.Load() {
		return nativealerts.ErrCheckpoint
	}
	s.snapshot = v
	s.exists = true
	s.writes.Add(1)
	return nil
}
func (s *memoryStore) Close() error { s.closed.Store(true); return nil }

type provider struct {
	bytes int64
	calls atomic.Int32
}

func (p *provider) DatabaseBytes(context.Context) (int64, error) { p.calls.Add(1); return p.bytes, nil }
func (p *provider) WALBytes(context.Context) (int64, error)      { return p.bytes, nil }
func waitStatus(t *testing.T, r *Runtime, changed <-chan struct{}, test func(Status) bool) Status {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		s := r.Status()
		if test(s) {
			return s
		}
		select {
		case <-changed:
		case <-timer.C:
			t.Fatalf("status not reached: %+v", s)
		}
	}
}
func launch(t *testing.T, r *Runtime) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- r.Run(t.Context()) }()
	t.Cleanup(r.Stop)
	return done
}
func TestObservationOffAndNeverStarted(t *testing.T) {
	r, e := New(observeconfig.Config{}, nil, false, Options{OpenStore: func(context.Context, string) (Checkpoint, error) { t.Fatal("off performed IO"); return nil, nil }})
	require.NoError(t, e)
	require.Nil(t, r)
	sampler := opsguard.NewObservationStatsSampler()
	r, e = New(policy(), sampler, true, Options{})
	require.NoError(t, e)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(r.Stop)
	}
	wg.Wait()
	require.True(t, r.Status().Stopped)
	require.ErrorIs(t, r.Run(t.Context()), ErrLifecycle)
}
func TestObservationMissingCheckpointIsStateLost(t *testing.T) {
	c := policy()
	c.Initialize = false
	changed := make(chan struct{}, 1)
	s := &memoryStore{}
	sampler := opsguard.NewObservationStatsSampler()
	r, e := New(c, sampler, true, Options{Changed: changed, OpenStore: func(context.Context, string) (Checkpoint, error) { return s, nil }})
	require.NoError(t, e)
	launch(t, r)
	st := waitStatus(t, r, changed, func(s Status) bool { return s.Running })
	require.True(t, st.CheckpointUnknown)
	for _, a := range st.Alerts {
		require.Equal(t, nativealerts.StateLost, a.Quality)
	}
	r.Stop()
	require.Zero(t, s.writes.Load())
	require.True(t, s.closed.Load())
}

// Check the complete worker boundary while a cancellation-ignoring fake sink
// holds its send. Producers must finish, Stop must join the real send, and the
// checkpoint must close only after that join. No network transport is used.
type blockingSink struct {
	entered, release chan struct{}
	once             sync.Once
	releaseOnce      sync.Once
	badContext       atomic.Bool
}
type ctxKey struct{}

func (s *blockingSink) Enabled() bool { return true }
func (s *blockingSink) Send(ctx context.Context, e nativealerts.Event) (nativealerts.Receipt, error) {
	if ctx.Value(ctxKey{}) != nil {
		s.badContext.Store(true)
	}
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return nativealerts.Receipt{ID: e.ID}, nil
}
func TestObservationSlowSinkDoesNotBlockProducerAndStopJoins(t *testing.T) {
	c := policy()
	s := &memoryStore{}
	sink := &blockingSink{entered: make(chan struct{}), release: make(chan struct{})}
	changed := make(chan struct{}, 1)
	sampler := opsguard.NewObservationStatsSampler()
	defer sampler.Close()
	r, e := New(c, sampler, false, Options{Sink: sink, Operator: c.OperatorDigest(), Changed: changed, OpenStore: func(ctx context.Context, _ string) (Checkpoint, error) {
		require.Nil(t, ctx.Value(ctxKey{}))
		return s, nil
	}})
	require.NoError(t, e)
	_, e = r.AttachStats(&provider{bytes: 950})
	require.NoError(t, e)
	done := make(chan error, 1)
	go func() { done <- r.Run(context.WithValue(t.Context(), ctxKey{}, true)) }()
	t.Cleanup(r.Stop)
	t.Cleanup(func() { sink.releaseOnce.Do(func() { close(sink.release) }) })
	waitStatus(t, r, changed, func(s Status) bool { return s.Running })
	require.True(t, sampler.Sample(t.Context()))
	select {
	case <-sink.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("critical never delivered")
	}
	for range 600 {
		require.True(t, sampler.Sample(t.Context()))
	}
	require.True(t, r.inbox.Lost())
	stopped := make(chan struct{})
	go func() { r.Stop(); close(stopped) }()
	require.False(t, s.closed.Load())
	select {
	case <-stopped:
		t.Fatal("Stop returned before actual send joined")
	default:
	}
	sink.releaseOnce.Do(func() { close(sink.release) })
	<-stopped
	require.NoError(t, <-done)
	require.True(t, s.closed.Load())
	require.False(t, sink.badContext.Load())
}

type harness struct {
	r       *Runtime
	sampler *opsguard.StatsSampler
	changed chan struct{}
	ticks   chan time.Time
	clock   atomic.Int64
}

func newHarness(t *testing.T, store *memoryStore, sink nativealerts.Sink) *harness {
	t.Helper()
	h := &harness{sampler: opsguard.NewObservationStatsSampler(), changed: make(chan struct{}, 1), ticks: make(chan time.Time, 1)}
	h.clock.Store(time.Now().UTC().UnixNano())
	c := policy()
	var e error
	h.r, e = New(c, h.sampler, false, Options{Sink: sink, Operator: c.OperatorDigest(), Now: h.now, Ticks: h.ticks, Changed: h.changed, OpenStore: func(context.Context, string) (Checkpoint, error) { return store, nil }})
	require.NoError(t, e)
	t.Cleanup(h.sampler.Close)
	_, e = h.r.AttachStats(&provider{})
	require.NoError(t, e)
	launch(t, h.r)
	waitStatus(t, h.r, h.changed, func(s Status) bool { return s.Running })
	return h
}
func (h *harness) now() time.Time { return time.Unix(0, h.clock.Load()).UTC() }
func (h *harness) advance()       { h.clock.Add(int64(time.Second)); h.ticks <- h.now() }
func (h *harness) offer(t *testing.T, value uint64, seq uint64) Status {
	t.Helper()
	h.clock.Add(int64(time.Second))
	at := h.now()
	tap := h.r.current.Load().tap
	require.True(t, tap.Offer(nativeobserve.DatabaseBytes, nativeobserve.Observation{Number: nativeobserve.Number{Present: true, Value: value}, Quality: nativeobserve.Known, Coverage: nativeobserve.Exact, StartedAt: at, AcquiredAt: at}))
	return waitStatus(t, h.r, h.changed, func(s Status) bool { return s.Alerts[0].Sequence == seq && s.Alerts[0].ObservedAt.Equal(at) })
}
func TestObservationDetachRebindFencesQueuedRecovery(t *testing.T) {
	h := newHarness(t, &memoryStore{}, nil)
	st := h.offer(t, 950, 1)
	require.Equal(t, nativealerts.Critical, st.Alerts[0].Active)
	old := h.r.current.Load()
	old.detach()
	require.False(t, old.tap.Offer(nativeobserve.DatabaseBytes, nativeobserve.Observation{}))
	h.advance()
	st = waitStatus(t, h.r, h.changed, func(s Status) bool { return s.SourceMissing && s.Alerts[0].Quality == nativealerts.Missing })
	require.Equal(t, nativealerts.Critical, st.Alerts[0].Active)
	_, e := h.r.AttachStats(&provider{})
	require.NoError(t, e)
	st = h.offer(t, 100, 1)
	require.Equal(t, nativealerts.Reset, st.Alerts[0].Quality)
	require.Equal(t, nativealerts.Critical, st.Alerts[0].Active)
	require.Equal(t, nativealerts.Critical, h.offer(t, 100, 2).Alerts[0].Active)
	require.Equal(t, nativealerts.Normal, h.offer(t, 100, 3).Alerts[0].Active)
}
func TestObservationPersistenceFailureRecoversWithoutBlockingProducer(t *testing.T) {
	store := &memoryStore{}
	h := newHarness(t, store, nil)
	store.fail.Store(true)
	h.offer(t, 950, 1)
	st := waitStatus(t, h.r, h.changed, func(s Status) bool { return s.CheckpointUnknown })
	require.Equal(t, nativealerts.Critical, st.Alerts[0].Active)
	require.Equal(t, 1, st.Pending)
	store.fail.Store(false)
	h.advance()
	waitStatus(t, h.r, h.changed, func(s Status) bool { return !s.CheckpointUnknown })
	h.r.Stop()
	require.Equal(t, nativealerts.Critical, store.snapshot.Records[0].Active)
	require.Len(t, store.snapshot.Deliveries, 1)
}

type failingSink struct{ events chan nativealerts.Event }

func (f failingSink) Enabled() bool { return true }
func (f failingSink) Send(_ context.Context, e nativealerts.Event) (nativealerts.Receipt, error) {
	f.events <- e
	return nativealerts.Receipt{}, nativealerts.ErrDelivery
}
func TestObservationFailingSinkKeepsStableDeadLetter(t *testing.T) {
	store := &memoryStore{}
	sink := failingSink{make(chan nativealerts.Event, 3)}
	h := newHarness(t, store, sink)
	h.offer(t, 950, 1)
	var id nativealerts.ID
	for n := range 3 {
		select {
		case e := <-sink.events:
			if n == 0 {
				id = e.ID
			} else {
				require.Equal(t, id, e.ID)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("missing bounded retry")
		}
		if n < 2 {
			h.clock.Add(int64(5 * time.Second))
			h.ticks <- h.now()
		}
	}
	h.r.Stop()
	require.Len(t, store.snapshot.Deliveries, 1)
	require.True(t, store.snapshot.Deliveries[0].Dead)
	require.Equal(t, 3, store.snapshot.Deliveries[0].Attempts)
}
func TestObservationPrivateCheckpointInitializationIsExplicit(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		initialize, corrupt, unknown bool
	}{{"absent_held", false, false, true}, {"explicit_absent", true, false, false}, {"corrupt_never_replaced", true, true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.Chmod(dir, 0700))
			path := filepath.Join(dir, "alerts.json")
			if tc.corrupt {
				require.NoError(t, os.WriteFile(path, []byte("broken"), 0600))
			}
			c := policy()
			c.CheckpointDir = dir
			c.Initialize = tc.initialize
			changed := make(chan struct{}, 1)
			sampler := opsguard.NewObservationStatsSampler()
			r, e := New(c, sampler, true, Options{Changed: changed})
			require.NoError(t, e)
			launch(t, r)
			st := waitStatus(t, r, changed, func(s Status) bool { return s.Running })
			require.Equal(t, tc.unknown, st.CheckpointUnknown)
			r.Stop()
			data, err := os.ReadFile(path)
			switch {
			case tc.corrupt:
				require.NoError(t, err)
				require.Equal(t, "broken", string(data))
			case tc.initialize:
				require.NoError(t, err)
				require.NotEmpty(t, data)
			default:
				require.ErrorIs(t, err, os.ErrNotExist)
			}
		})
	}
}
func TestObservationRejectsUnboundSink(t *testing.T) {
	sampler := opsguard.NewObservationStatsSampler()
	defer sampler.Close()
	_, e := New(policy(), sampler, false, Options{Sink: &blockingSink{}})
	require.ErrorIs(t, e, observeconfig.ErrConfig)
}

func TestObservationConcurrentArrivalKeepsAcquisitionTime(t *testing.T) {
	base := time.Now().UTC()
	var clock atomic.Int64
	clock.Store(base.UnixNano())
	entered, release := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	now := func() time.Time {
		if first.CompareAndSwap(false, true) {
			close(entered)
			<-release
			return base
		}
		return time.Unix(0, clock.Load()).UTC()
	}
	changed := make(chan struct{}, 1)
	store := &memoryStore{}
	sampler := opsguard.NewObservationStatsSampler()
	defer sampler.Close()
	r, e := New(policy(), sampler, false, Options{Now: now, Ticks: make(chan time.Time), Changed: changed, OpenStore: func(context.Context, string) (Checkpoint, error) { return store, nil }})
	require.NoError(t, e)
	_, e = r.AttachStats(&provider{})
	require.NoError(t, e)
	launch(t, r)
	waitStatus(t, r, changed, func(s Status) bool { return s.Running })
	offer := func(metric nativeobserve.Metric, at time.Time) {
		require.True(t, r.current.Load().tap.Offer(metric, nativeobserve.Observation{Number: nativeobserve.Number{Present: true, Value: 950}, Quality: nativeobserve.Known, Coverage: nativeobserve.Exact, StartedAt: at, AcquiredAt: at}))
	}
	offer(nativeobserve.DatabaseBytes, base)
	<-entered
	later := base.Add(time.Millisecond)
	clock.Store(later.UnixNano())
	offer(nativeobserve.WALBytes, later)
	close(release)
	st := waitStatus(t, r, changed, func(s Status) bool { return s.Alerts[1].Sequence == 1 })
	require.Equal(t, later, st.Alerts[1].ObservedAt)
	require.Equal(t, nativealerts.Critical, st.Alerts[1].Active)
}

func TestObservationInboxLossCannotClearRestoredCritical(t *testing.T) {
	cfg := policy().WithDefaults()
	base := time.Now().UTC()
	changed := make(chan struct{}, 1)
	store := &memoryStore{}
	sampler := opsguard.NewObservationStatsSampler()
	defer sampler.Close()
	r, e := New(cfg, sampler, false, Options{Now: func() time.Time { return base.Add(2 * time.Second) }, Ticks: make(chan time.Time), Changed: changed, OpenStore: func(context.Context, string) (Checkpoint, error) { return store, nil }})
	require.NoError(t, e)
	_, e = r.AttachStats(&provider{})
	require.NoError(t, e)
	reg := r.current.Load()
	engine, e := nativealerts.New(cfg.Rules(), nativealerts.Incarnation{1})
	require.NoError(t, e)
	o := nativeobserve.Observation{Binding: reg.bindings[0], Number: nativeobserve.Number{Present: true, Value: 950}, Sequence: 1, Quality: nativeobserve.Known, Coverage: nativeobserve.Exact, StartedAt: base, AcquiredAt: base}
	signal, e := nativealerts.CapacitySignal(cfg.Rules()[0], reg.bindings[0], o, base, false)
	require.NoError(t, e)
	require.NoError(t, engine.Apply(signal.Key, signal, base))
	require.NoError(t, store.Write(t.Context(), engine.Snapshot()))
	// The bounded queue is fresh enough and spans clear dwell. Known loss still
	// prevents it from resolving a restored incident.
	for n := range cfg.InboxSize + 1 {
		at := base.Add(2 * time.Second)
		if n < cfg.InboxSize-1 {
			at = base.Add(time.Second)
		}
		reg.tap.Offer(nativeobserve.DatabaseBytes, nativeobserve.Observation{Number: nativeobserve.Number{Present: true, Value: 100}, Quality: nativeobserve.Known, Coverage: nativeobserve.Exact, StartedAt: at, AcquiredAt: at})
	}
	launch(t, r)
	st := waitStatus(t, r, changed, func(s Status) bool { return len(s.Alerts) == 2 && s.Alerts[0].Sequence == uint64(cfg.InboxSize) })
	require.True(t, st.Loss)
	require.Equal(t, nativealerts.Critical, st.Alerts[0].Active)
	require.Equal(t, nativealerts.Gap, st.Alerts[0].Quality)
}

type lateProvider struct {
	entered, cancelled, release chan struct{}
	wal                         atomic.Int32
}

type slowCheckpoint struct {
	memoryStore
	entered, release chan struct{}
	calls            int
}

func (s *slowCheckpoint) Write(ctx context.Context, v nativealerts.Snapshot) error {
	s.calls++
	if s.calls == 2 {
		close(s.entered)
		<-s.release
	}
	return s.memoryStore.Write(ctx, v)
}
func TestObservationSlowCheckpointDoesNotBlockProducerOrDetach(t *testing.T) {
	store := &slowCheckpoint{entered: make(chan struct{}), release: make(chan struct{})}
	changed := make(chan struct{}, 1)
	sampler := opsguard.NewObservationStatsSampler()
	defer sampler.Close()
	r, e := New(policy(), sampler, false, Options{Changed: changed, OpenStore: func(context.Context, string) (Checkpoint, error) { return store, nil }})
	require.NoError(t, e)
	detach, e := r.AttachStats(&provider{bytes: 950})
	require.NoError(t, e)
	launch(t, r)
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(store.release) }) })
	waitStatus(t, r, changed, func(s Status) bool { return s.Running })
	require.True(t, sampler.Sample(t.Context()))
	<-store.entered
	// Both the producer and source detachment finish while alert disk IO is held.
	require.True(t, sampler.Sample(t.Context()))
	detach()
	stopped := make(chan struct{})
	go func() { r.Stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("Stop returned before checkpoint IO joined")
	default:
	}
	once.Do(func() { close(store.release) })
	<-stopped
	require.True(t, store.closed.Load())
}

func (p *lateProvider) DatabaseBytes(ctx context.Context) (int64, error) {
	close(p.entered)
	<-ctx.Done()
	close(p.cancelled)
	<-p.release
	return 950, nil
}
func (p *lateProvider) WALBytes(context.Context) (int64, error) { p.wal.Add(1); return 950, nil }
func TestObservationStopJoinsLateProviderAndFencesResult(t *testing.T) {
	p := &lateProvider{entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	store := &memoryStore{}
	changed := make(chan struct{}, 1)
	sampler := opsguard.NewObservationStatsSampler()
	r, e := New(policy(), sampler, true, Options{Changed: changed, OpenStore: func(context.Context, string) (Checkpoint, error) { return store, nil }})
	require.NoError(t, e)
	_, e = r.AttachStats(p)
	require.NoError(t, e)
	launch(t, r)
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(p.release) }) })
	<-p.entered
	waitStatus(t, r, changed, func(s Status) bool { return s.Running })
	stopped := make(chan struct{})
	go func() { r.Stop(); close(stopped) }()
	<-p.cancelled
	_, e = r.AttachStats(&provider{})
	require.ErrorIs(t, e, ErrLifecycle)
	select {
	case <-stopped:
		t.Fatal("Stop failed to join late provider")
	default:
	}
	once.Do(func() { close(p.release) })
	<-stopped
	require.Zero(t, p.wal.Load())
	for _, a := range r.Status().Alerts {
		require.Equal(t, nativealerts.Normal, a.Active)
		require.Zero(t, a.Sequence)
	}
}
