package observation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xmhha/indexer-go/internal/nativealerts"
	"github.com/0xmhha/indexer-go/internal/nativeobserve"
	"github.com/0xmhha/indexer-go/internal/observeconfig"
	"github.com/0xmhha/indexer-go/internal/opsguard"
	"go.uber.org/zap"
)

var ErrLifecycle = errors.New("observation: owner closed or already running")

type Checkpoint interface {
	nativealerts.Store
	Close() error
}

// Options inject owned test adapters only. App production assembly supplies no
// Sink: YAML v1 supports disabled only. An injected enabled sink additionally
// needs a separate exact operator binding, never chain notification settings.
type Options struct {
	Sink      nativealerts.Sink
	Operator  nativeobserve.Digest
	OpenStore func(context.Context, string) (Checkpoint, error)
	Now       func() time.Time
	Ticks     <-chan time.Time
	Changed   chan<- struct{}
	Logger    *zap.Logger
}
type AlertStatus struct {
	Key        nativealerts.Key
	Active     nativealerts.Level
	Quality    nativealerts.Quality
	Sequence   uint64
	ObservedAt time.Time
}
type Status struct {
	Running, Stopped, CheckpointUnknown, DeliveryUnknown, SourceMissing, Loss bool
	Pending                                                                   int
	Alerts                                                                    []AlertStatus
}
type registration struct {
	bindings []nativeobserve.Binding
	tap      *nativeobserve.Tap
	detach   func()
	once     sync.Once
}
type Runtime struct {
	cfg              observeconfig.Config
	sampler          *opsguard.StatsSampler
	ownSampler       bool
	options          Options
	inbox            *nativeobserve.Inbox
	engine           *nativealerts.Engine // alert worker only
	runID            nativeobserve.RunID
	mu               sync.Mutex
	regs             []*registration
	stopped, running bool
	cancel           context.CancelFunc
	done             chan struct{}
	stopOnce         sync.Once
	stopDone         chan struct{}
	current          atomic.Pointer[registration]
	selection        atomic.Uint64
	status           atomic.Pointer[Status]
}

func New(cfg observeconfig.Config, sampler *opsguard.StatsSampler, own bool, options Options) (*Runtime, error) {
	if !cfg.Enabled {
		return nil, nil
	} // no factory, RNG, file or worker when off
	cfg = cfg.WithDefaults()
	if cfg.Validate() != nil || sampler == nil {
		return nil, observeconfig.ErrConfig
	}
	if options.Sink != nil && options.Sink.Enabled() && (options.Operator == (nativeobserve.Digest{}) || options.Operator != cfg.OperatorDigest()) {
		return nil, observeconfig.ErrConfig
	}
	if options.Sink == nil {
		options.Sink = nativealerts.DisabledSink{}
	}
	if options.OpenStore == nil {
		options.OpenStore = func(ctx context.Context, path string) (Checkpoint, error) {
			if ctx.Err() != nil {
				return nil, nativealerts.ErrCheckpoint
			}
			return nativealerts.OpenFileStore(path)
		}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Logger == nil {
		options.Logger = zap.NewNop()
	}
	inbox, err := nativeobserve.NewInbox(cfg.InboxSize)
	if err != nil {
		return nil, err
	}
	r := &Runtime{cfg: cfg, sampler: sampler, ownSampler: own, options: options, inbox: inbox, stopDone: make(chan struct{})}
	var incarnation nativealerts.Incarnation
	if _, err = rand.Read(r.runID[:]); err != nil {
		return nil, err
	}
	if _, err = rand.Read(incarnation[:]); err != nil {
		return nil, err
	}
	r.engine, err = nativealerts.New(cfg.Rules(), incarnation)
	if err != nil {
		return nil, err
	}
	r.status.Store(&Status{SourceMissing: true})
	return r, nil
}

// AttachStats binds one actual Store lifetime, without reading or fingerprinting
// a DSN. Resource IDs are declarations, not physical-database authentication.
func (r *Runtime) AttachStats(provider opsguard.PostgresStats) (func(), error) {
	var token nativeobserve.SourceToken
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	bindings := r.cfg.Bindings(nativeobserve.Source{Token: token, Incarnation: 1, Run: r.runID})
	tap, err := r.inbox.Bind(bindings)
	if err != nil {
		return nil, err
	}
	reg := &registration{bindings: bindings, tap: tap}
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		tap.Retire()
		return nil, ErrLifecycle
	}
	detach, err := r.sampler.Attach(provider, tap)
	if err != nil {
		r.mu.Unlock()
		tap.Retire()
		return nil, err
	}
	reg.detach = func() {
		reg.once.Do(func() {
			r.mu.Lock()
			for n, v := range r.regs {
				if v == reg {
					r.regs = append(r.regs[:n], r.regs[n+1:]...)
					break
				}
			}
			var selected *registration
			if len(r.regs) > 0 {
				selected = r.regs[len(r.regs)-1]
			}
			r.current.Store(selected)
			r.selection.Add(1)
			tap.Retire()
			r.mu.Unlock()
			detach()
		})
	}
	r.regs = append(r.regs, reg)
	r.current.Store(reg)
	r.selection.Add(1)
	r.mu.Unlock()
	return reg.detach, nil
}
func (r *Runtime) Status() Status {
	if r == nil {
		return Status{Stopped: true}
	}
	v := *r.status.Load()
	v.Alerts = slices.Clone(v.Alerts)
	return v
}
func (r *Runtime) publish(s Status) {
	s.Loss = r.inbox.Lost()
	s.SourceMissing = r.current.Load() == nil
	s.Alerts = nil
	for _, rule := range r.cfg.Rules() {
		state, _ := r.engine.State(rule.Key)
		s.Alerts = append(s.Alerts, AlertStatus{rule.Key, state.Active, state.Quality, state.Sequence, state.ObservedAt})
	}
	snapshot := r.engine.Snapshot()
	s.Pending = len(snapshot.Deliveries)
	for _, record := range snapshot.Records {
		if record.Deferred != nil {
			s.Pending++
		}
	}
	r.status.Store(&s)
	if r.options.Changed != nil {
		select {
		case r.options.Changed <- struct{}{}:
		default:
		}
	}
}
func (r *Runtime) detachAll() {
	r.mu.Lock()
	r.stopped = true
	regs := slices.Clone(r.regs)
	r.mu.Unlock()
	for _, reg := range regs {
		reg.detach()
	}
}
func (r *Runtime) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() {
		r.mu.Lock()
		r.stopped = true
		cancel, done := r.cancel, r.done
		r.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		r.detachAll()
		if r.ownSampler {
			r.sampler.Close()
		}
		if done != nil {
			<-done
		}
		s := r.Status()
		s.Running, s.Stopped, s.SourceMissing = false, true, true
		r.status.Store(&s)
		close(r.stopDone)
	})
	<-r.stopDone
}

func cleanContext(parent context.Context) (context.Context, context.CancelFunc) {
	var ctx context.Context
	var cancel context.CancelFunc
	if deadline, ok := parent.Deadline(); ok {
		ctx, cancel = context.WithDeadline(context.Background(), deadline)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	stop := context.AfterFunc(parent, cancel)
	if parent.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}
func snapshotID(s nativealerts.Snapshot) [32]byte {
	data, _ := json.Marshal(s)
	return sha256.Sum256(data)
}

// Run owns the alert engine/store/sink serially. Only the bounded inbox is on
// producer paths. A cancellation-ignoring sink/OS call is honestly joined.
func (r *Runtime) Run(parent context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.stopped || r.running {
		r.mu.Unlock()
		return ErrLifecycle
	}
	ctx, cancel := cleanContext(parent)
	done := make(chan struct{})
	r.done, r.cancel, r.running = done, cancel, true
	r.mu.Unlock()
	blocked := false
	var samplerWG sync.WaitGroup
	if r.ownSampler {
		samplerWG.Go(func() { _ = r.sampler.Run(ctx, r.cfg.Interval) })
	}
	defer func() {
		cancel()
		r.detachAll()
		if r.ownSampler {
			r.sampler.Close()
		}
		samplerWG.Wait()
		r.publish(Status{Stopped: true, CheckpointUnknown: blocked || r.engine.PersistenceFailed()})
		r.mu.Lock()
		r.running = false
		close(done)
		r.mu.Unlock()
	}()
	store, openErr := r.options.OpenStore(ctx, r.cfg.CheckpointDir)
	if store != nil {
		defer func() { _ = store.Close() }()
	}
	blocked = openErr != nil || store == nil
	var saved [32]byte
	if !blocked {
		snapshot, err := store.Read(ctx)
		if err == nil {
			blocked = r.engine.Restore(snapshot) != nil
		} else if r.cfg.Initialize {
			blocked = r.engine.Persist(ctx, store) != nil
		} else {
			blocked = true
		}
		if !blocked {
			saved = snapshotID(r.engine.Snapshot())
		}
	}
	if blocked {
		_ = r.engine.Load(ctx, nil) // freeze unknown history; never manufacture recovery
		r.options.Logger.Warn("Observation checkpoint unavailable; indexing authority is unchanged")
	}
	var ticker *time.Ticker
	ticks := r.options.Ticks
	if ticks == nil {
		ticker = time.NewTicker(time.Second)
		ticks = ticker.C
		defer ticker.Stop()
	}
	last := map[nativeobserve.Metric]time.Time{}
	seenSelection := r.selection.Load()
	seenLoss := false
	var retryAt time.Time
	status := Status{Running: true, CheckpointUnknown: blocked}
	r.publish(status)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.inbox.Ready():
		case <-ticks:
		}
		if ctx.Err() != nil {
			return nil
		}
		at := r.options.Now()
		// Source selection and pure grading form one bounded fence. No IO or
		// producer Offer takes this lock; detach never waits on sink/persistence.
		r.mu.Lock()
		selected := r.current.Load()
		selection := r.selection.Load()
		changed := selection != seenSelection
		loss := r.inbox.Lost() && !seenLoss
		if !blocked && (changed || loss) {
			for _, rule := range r.cfg.Rules() {
				_ = r.engine.Apply(rule.Key, nativealerts.Signal{Key: rule.Key, Quality: nativealerts.Gap}, at)
			}
			if changed {
				last = map[nativeobserve.Metric]time.Time{}
			}
			seenSelection, seenLoss = selection, r.inbox.Lost()
		}
		for range r.cfg.InboxSize {
			o, ok := r.inbox.Poll()
			if !ok {
				break
			}
			if blocked || selected == nil || o.Binding.Source != selected.bindings[0].Source {
				continue
			}
			// Another component can finish after this drain started. Compare
			// against a fresh consumer clock, preserving its acquisition time.
			at = r.options.Now()
			for n, expected := range selected.bindings {
				if o.Binding.Metric != expected.Metric {
					continue
				}
				rule := r.cfg.Rules()[n]
				in, err := nativealerts.CapacitySignal(rule, expected, o, at, r.inbox.Lost())
				if err == nil {
					_ = r.engine.Apply(rule.Key, in, at)
					last[expected.Metric] = o.AcquiredAt
				}
			}
		}
		at = r.options.Now()
		for _, rule := range r.cfg.Rules() {
			if blocked {
				break
			}
			if selected == nil || last[rule.Key.Metric].IsZero() || at.Sub(last[rule.Key.Metric]) > r.cfg.MaxAge {
				_ = r.engine.Apply(rule.Key, nativealerts.Signal{Key: rule.Key, Quality: nativealerts.Missing}, at)
			}
		}
		r.mu.Unlock()
		if !blocked && !at.Before(retryAt) {
			if current := snapshotID(r.engine.Snapshot()); current != saved || status.CheckpointUnknown {
				if r.engine.Persist(ctx, store) != nil {
					status.CheckpointUnknown = true
					retryAt = at.Add(r.cfg.Interval)
				} else {
					saved = current
					status.CheckpointUnknown = false
				}
			}
			if !status.CheckpointUnknown && r.options.Sink.Enabled() {
				delivery, stop := context.WithTimeout(ctx, r.cfg.DeliveryTimeout)
				err := r.engine.Dispatch(delivery, at, r.options.Sink, store)
				stop()
				status.DeliveryUnknown = err != nil
				status.CheckpointUnknown = r.engine.PersistenceFailed()
				if status.CheckpointUnknown {
					retryAt = at.Add(r.cfg.Interval)
				} else {
					saved = snapshotID(r.engine.Snapshot())
				}
			}
		}
		r.publish(status)
	}
}
