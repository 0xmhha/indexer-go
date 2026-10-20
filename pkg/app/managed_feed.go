package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	_ "github.com/0xmhha/indexer-go/modules/feeds/arc" // registers the audited module
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmtrace"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/feed"
	"github.com/0xmhha/indexer-go/pkg/feedtelemetry"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/projection"
	"github.com/0xmhha/indexer-go/pkg/source/facts"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// Validate all enabled chain plans before any chain can open a DB/RPC/volume.
func preflightFeeds(cfg *config.Config, gap bool) error {
	used := map[string]bool{}
	check := func(c *config.Config) error {
		f := c.Source.Feed
		if f == nil {
			return nil
		}
		if err := f.Validate(); err != nil {
			return err
		}
		for _, name := range f.Projections {
			if !projection.Supported(name) {
				return fmt.Errorf("feed: unsupported independent projection %q", name)
			}
		}
		anchor, _ := f.Identity()
		if c.NodeRole() == config.RoleAPI || gap || c.DeclaredMode() || c.Source.EraDir != "" || c.Source.NodeFeedSocket != "" || c.RPC.RecordDir != "" {
			return fmt.Errorf("source.feed requires full ingest without era, IPC feed, RPC recording or gap recovery")
		}
		if c.RPC.RateLimit != 5 {
			return fmt.Errorf("source.feed requires the shared public RPC rate_limit: 5")
		}
		if c.Indexer.Finality != "" && c.Indexer.Finality != fetch.FinalityHead {
			return fmt.Errorf("source.feed projection finality must be head (captured DB head); feed finality/GC is selected separately")
		}
		if anchor.Number == math.MaxUint64 || c.Indexer.StartHeight != anchor.Number+1 {
			return fmt.Errorf("source.feed start_height must equal anchor_height + 1")
		}
		if _, err := c.WindowStorage.OptionsFor(f.Window, anchor.Chain); err != nil {
			return err
		}
		if used[f.Window] {
			return fmt.Errorf("source.feed window reservation is shared by multiple chains")
		}
		used[f.Window] = true
		enabled, err := feature.Enabled(feature.Defaults(), (&App{config: c}).featureOverrides())
		if err != nil {
			return err
		}
		for _, name := range enabled {
			if feature.ReadsState(name) || name == "dex.orderbook" {
				return fmt.Errorf("source.feed execution plan has no state coverage for feature %s; explicitly disable it", name)
			}
		}
		return nil
	}
	if cfg.MultiChainMode() {
		if cfg.Source.Feed != nil {
			return fmt.Errorf("multichain feed must be configured on its chain entry")
		}
		for _, cc := range cfg.MultiChain.Chains {
			if !cc.Enabled {
				continue
			}
			child := *cfg
			child.Source = config.SourceConfig{Feed: cc.Feed, NodeFeedSocket: cc.NodeFeedSocket}
			child.Indexer.StartHeight = cc.StartHeight
			child.Features = chainFeatures(cfg.Features, cc.Features)
			child.MultiChain = config.MultiChainConfig{}
			if err := check(&child); err != nil {
				return fmt.Errorf("chain %s: %w", cc.ID, err)
			}
		}
	} else if err := check(cfg); err != nil {
		return err
	}
	if cfg.WindowStorage.Configured() && len(used) == 0 {
		return fmt.Errorf("window_storage requires source.feed/capture.RunConfiguredSession with verified feed/assembler tasks")
	}
	return nil
}

type managedFeed struct {
	telemetry        *feedtelemetry.Recorder
	releaseTelemetry func()
	mu               sync.Mutex
	cancel           context.CancelFunc
	done             chan struct{}
	stopped          bool
	started          bool
	first            *fetch.Fetcher
	store            *capture.Store
	program          feed.Program
	session          capture.SessionConfig
	db               port.CaptureDatabase
	build            func(context.Context) (*fetch.Fetcher, uint64, error)
	firstEpoch       uint64
	projection       func(context.Context) error
	extraTasks       []func(context.Context) error
	stateSupplier    *evmstate.Supplier
	storage          any
	log              *zap.Logger
}

func (a *App) initManagedFeed(ctx context.Context) error {
	c := *a.config.Source.Feed
	p, err := feed.Open(ctx, c, a.profileSrc)
	if err != nil {
		return err
	}
	if a.profileErr != nil {
		return a.profileErr
	}
	actualID, e := a.client.GetChainID(ctx)
	if e != nil {
		return e
	}
	if actualID.String() != p.Chain.ID {
		return fmt.Errorf("feed chain identity differs from RPC chain id")
	}
	if a.profile.ID() != p.Profile.ID() {
		return fmt.Errorf("feed module and selected chain profile differ")
	}
	db, ok := a.storage.(port.CaptureDatabase)
	if !ok {
		return fmt.Errorf("feed requires capture database")
	}
	st, err := capture.New(db, p.Chain, a.profileSrc)
	if err != nil {
		return err
	}
	// Reject authority changes before Open can finish a previously durable GC.
	old, e := st.State(ctx)
	if e != nil && !errors.Is(e, port.ErrNotFound) {
		return e
	}
	policy := ""
	if p.Policy != nil {
		policy = p.Policy.ID()
	}
	if e == nil && ((old.FinalityPolicy != "" && old.FinalityPolicy != policy) || (old.FinalityPolicy == "" && old.Finalized != nil && policy != "")) {
		return capture.ErrConflict
	}
	if err := st.Initialize(ctx, p.Plan, p.Anchor, nil); err != nil {
		return err
	}
	// Bind and check the tracked-state deletion contract BEFORE raw Open can
	// resume a durable deletion. The GC session uses the same pure verifier.
	st = st.WithSupplementVerifier(evmstate.Verifier(p.Profile))
	var stateDescriptor, required *model.SupplementStreamDescriptor
	if c.StateProofs != nil {
		d, e := c.StateProofs.Descriptor(p.Anchor)
		if e != nil {
			return e
		}
		stateDescriptor = &d
		if d.Required {
			required = &d
		}
	}
	if err := st.BindRequiredSupplement(ctx, required); err != nil {
		return err
	}
	if stateDescriptor != nil {
		if _, err := st.InitializeSupplementStream(ctx, *stateDescriptor); err != nil {
			return err
		}
	}
	opts, err := a.config.WindowStorage.OptionsFor(c.Window, p.Chain)
	if err != nil {
		return err
	}

	binding, _ := json.Marshal(struct{ Volume, Root, Path, Reservation string }{opts.Volume.ID, opts.Volume.Root, opts.Path, opts.Reservation})
	ready := false
	key := []byte("app/feed-window/v1")
	err = db.CaptureView(ctx, p.Chain, func(r port.CaptureReader) error {
		old, e := r.Get(key)
		if errors.Is(e, port.ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		if !bytes.Equal(old, binding) {
			return capture.ErrConflict
		}
		ready = true
		return nil
	})
	if err != nil {
		return err
	}
	if c.InitializeVolume && !ready {
		if err := window.InitializeVolume(opts.Volume); err != nil {
			return err
		}
	}
	var raw *window.Store
	if c.CreateWindow && !ready {
		state, e := st.State(ctx)
		if e != nil {
			return e
		}
		if state.Sequence != 0 || state.PendingDelete != (common.Hash{}) {
			return capture.ErrConflict
		}
		raw, err = window.Create(opts)
	} else {
		raw, err = st.OpenWindow(ctx, opts)
	}
	if err != nil {
		return err
	}
	if err = raw.Close(); err != nil {
		return err
	}
	// This durable binding makes repeated first-start flags safe for manager/
	// process restarts. Once recorded, missing storage is ALWAYS an Open error.
	err = db.CaptureUpdate(ctx, p.Chain, func(w port.CaptureWriter) error {
		old, e := w.Get(key)
		if e == nil {
			if !bytes.Equal(old, binding) {
				return capture.ErrConflict
			}
			return nil
		}
		if !errors.Is(e, port.ErrNotFound) {
			return e
		}
		return w.Put(key, binding)
	})
	if err != nil {
		return err
	}

	m := &managedFeed{store: st, program: p, db: db, storage: a.storage, log: a.logger, session: capture.SessionConfig{Storage: a.config.WindowStorage, Instance: c.Window, Plan: p.Plan, Anchor: p.Anchor, Logger: a.logger, OnUsage: a.observeRaw}}
	m.session.RequiredSupplement, m.session.SupplementVerifier = required, evmstate.Verifier(p.Profile)
	m.build = func(ctx context.Context) (*fetch.Fetcher, uint64, error) {
		return a.buildFactsFetcher(ctx, st, p, c.Consumer)
	}
	m.first, m.firstEpoch, err = m.build(ctx)
	if err != nil {
		return err
	}
	if c.Telemetry != nil {
		m.telemetry, err = feedtelemetry.New(p.Anchor, p.Plan, c.Telemetry.ReceiveIntervalTarget)
		if err != nil {
			m.first.Close()
			return err
		}
		m.releaseTelemetry, err = a.client.Pool().AttachTelemetry(m.telemetry)
		if err != nil {
			m.telemetry.Close()
			m.first.Close()
			return err
		}
		defer func() {
			if a.managedFeed != m {
				m.releaseTelemetry()
				m.telemetry.Close()
			}
		}()
	}
	descriptor := model.ProjectionDescriptor{Version: 1, Anchor: p.Anchor, Plan: p.Plan, Workers: append([]string{}, c.Projections...)}
	blobs := p.BlobCapability
	descriptor.BlobCapability = &blobs
	for _, name := range c.Projections {
		if name == model.SourceSearchWorker {
			descriptor.SourceSearch = &model.SourceSearchDescriptor{Version: 1, Profile: p.Profile.ID()}
		}
		if name == model.SourceLookupWorker {
			descriptor.SourceLookup = &model.SourceLookupDescriptor{Version: 1, Profile: p.Profile.ID()}
		}
	}
	descriptor.Telemetry = m.telemetry.Descriptor()
	if stateDescriptor != nil {
		d := *stateDescriptor
		supplier, e := evmstate.New(st, p.Profile, a.client.RPCClient())
		if e != nil {
			m.first.Close()
			return e
		}
		m.stateSupplier = supplier
		descriptor.StateProofs = &d
		m.extraTasks = append(m.extraTasks, func(ctx context.Context) error { return supplier.Run(ctx, d, c.StateProofs.PollInterval) })
		if c.StateProofs.ProjectChanges {
			changes := evmstate.ProjectionDescriptor(d)
			if _, e := st.InitializeStateProjection(ctx, changes); e != nil {
				m.first.Close()
				return e
			}
			descriptor.StateChanges = &changes
			m.extraTasks = append(m.extraTasks, func(ctx context.Context) error {
				return evmstate.RunProjection(ctx, st, p.Profile, changes, c.StateProofs.PollInterval)
			})
		}
	}
	if c.CallTraces != nil {
		d, e := c.CallTraces.Descriptor(p.Anchor, p.Profile.ID())
		if e != nil {
			m.first.Close()
			return e
		}
		if _, e := st.InitializeSupplementStream(ctx, d); e != nil {
			m.first.Close()
			return e
		}
		supplier, e := evmtrace.New(st, p.Profile, a.client.RPCClient())
		if e != nil {
			m.first.Close()
			return e
		}
		descriptor.CallTraces = &d
		m.extraTasks = append(m.extraTasks, func(ctx context.Context) error { return supplier.Run(ctx, d, c.CallTraces.PollInterval) })
	}
	if e = m.first.ConfigureProjection(ctx, descriptor); e != nil {
		m.first.Close()
		return e
	}

	for _, name := range c.Projections {
		m.extraTasks = append(m.extraTasks, func(ctx context.Context) error { return projection.Run(ctx, name, st, p.Profile, a.profileSrc) })
	}
	m.projection = m.project
	a.managedFeed, a.fetcher = m, m.first
	return nil
}

func (a *App) buildFactsFetcher(ctx context.Context, st *capture.Store, p feed.Program, consumer string) (_ *fetch.Fetcher, epoch uint64, err error) {
	src, err := facts.New(ctx, facts.Config{Store: st, Profile: p.Profile, Canonical: a.profileSrc, Consumer: consumer})
	if err != nil {
		return nil, 0, err
	}
	cfg := a.config.Indexer
	f := fetch.NewFetcher(src, a.storage, &fetch.Config{StatePrefetch: cfg.StatePrefetch, PipelineMetrics: cfg.PipelineMetrics, StateMaxBytes: cfg.StateMaxBytes, AcquisitionQueue: cfg.AcquisitionQueue, QueueBlocks: cfg.QueueBlocks, QueueMaxBytes: cfg.QueueMaxBytes, StartHeight: cfg.StartHeight, BatchSize: cfg.ChunkSize, MaxRetries: 3, RetryDelay: 200 * time.Millisecond, NumWorkers: cfg.Workers, RPCTimeout: a.config.RPC.Timeout, PollInterval: cfg.PollInterval, Finality: fetch.FinalityHead, NoOutbox: !a.config.EventBus.Outbox, OutboxRetain: a.config.EventBus.OutboxRetention, StreamGroup: a.config.Node.ID}, a.logger, a.eventBus)
	defer func() {
		if err != nil {
			f.Close()
		}
	}()
	f.SetSource(src)
	f.SetStateSource(src)
	enabled, err := feature.Enabled(a.defaultFeatures(p.Profile), a.featureOverrides())
	if err != nil {
		return nil, 0, err
	}
	for _, name := range enabled {
		if feature.ReadsState(name) || name == "dex.orderbook" {
			return nil, 0, fmt.Errorf("facts: unsupported state feature %s", name)
		}
	}
	deps := src.BindFeatures(feature.Deps{Storage: a.storage, Logger: a.logger, Profile: p.Profile, Publish: f.Publish, Settings: a.config.FeatureSettings, PrepareConcurrency: orDefault(cfg.StateWorkers, 4), StateBatchSize: orDefault(cfg.StateBatchSize, 1)})
	pipeline, err := feature.Build(enabled, deps)
	if err != nil {
		return nil, 0, err
	}
	f.SetFeatures(pipeline)
	deps.Publish = func(events.Event) bool { return true }
	backfill, err := feature.Build(enabled, deps)
	if err != nil {
		return nil, 0, err
	}
	latest, e := a.storage.GetLatestHeight(ctx)
	if e == nil {
		if latest < p.Anchor.Number {
			return nil, 0, fmt.Errorf("facts: projection cursor is outside captured history; use a separate projection database")
		}
		// A committed capture reset may lower its prefix below the old
		// projection head. That head can also disappear from the node entirely.
		// Only an established post-reset epoch permits this reconciliation;
		// initial/legacy databases retain their strict history checks. Starting
		// at the retained capture prefix still uses HandleReorg's fresh hashes,
		// bootstrap fence, undo window and writer/outbox transaction.
		capturedHead, e := src.Head(ctx)
		if e != nil {
			return nil, 0, e
		}
		if src.Epoch() > 1 && latest > capturedHead {
			latest, e = f.HandleReorg(ctx, capturedHead)
			if e != nil {
				return nil, 0, e
			}
		}
		if latest == p.Anchor.Number {
			has, e := a.storage.HasBlock(ctx, latest)
			if e != nil {
				return nil, 0, e
			}
			if has {
				return nil, 0, fmt.Errorf("facts: bootstrap block belongs to uncaptured projection history; use a separate projection database")
			}
		}
		// Recover's legacy RPC path may skip a transient missing node. For this
		// mode the canonical check is mandatory before undo/reconciliation.
		if _, err := src.HashAt(ctx, latest); err != nil {
			return nil, 0, err
		}
		if fs, ok := a.storage.(port.FeatureStateStore); ok {
			states, e := fs.FeatureStates(ctx)
			if e != nil {
				return nil, 0, e
			}
			_, jobs, e := feature.ReconcileUnits(pipeline.Units(), states, latest, true)
			if e != nil {
				return nil, 0, e
			}
			for _, job := range jobs {
				if job.From <= p.Anchor.Number {
					return nil, 0, fmt.Errorf("%w: feature %s backfill begins before captured history", facts.ErrCoverage, job.Feature)
				}
			}
		}
	} else if !errors.Is(e, port.ErrNotFound) {
		return nil, 0, e
	}
	if err := f.Recover(ctx, pipeline.Units(), backfill); err != nil {
		return nil, 0, err
	}
	latest, e = a.storage.GetLatestHeight(ctx)
	if e == nil && latest > p.Anchor.Number {
		if err := f.FetchBlock(ctx, latest); err != nil {
			return nil, 0, err
		} // repair a post-commit receipt lost before a crash
	} else if e != nil && !errors.Is(e, port.ErrNotFound) {
		return nil, 0, e
	}
	if tap, ok := a.notificationService.(*notifications.NotificationService); ok {
		f.SetBlockTap(tap)
	}
	return f, src.Epoch(), nil
}

func (m *managedFeed) Stop() {
	m.mu.Lock()
	m.stopped = true
	cancel, done := m.cancel, m.done
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	if m.releaseTelemetry != nil {
		m.releaseTelemetry()
	}
	m.telemetry.Close()
}
func (m *managedFeed) Run(ctx context.Context) error {
	m.mu.Lock()
	if m.stopped || m.started {
		m.mu.Unlock()
		return fmt.Errorf("feed: application run is single-use")
	}
	run, cancel := context.WithCancel(feedtelemetry.WithRecorder(ctx, m.telemetry))
	m.cancel = cancel
	m.done = make(chan struct{})
	m.started = true
	done := m.done
	m.mu.Unlock()
	defer close(done)
	defer m.telemetry.Stop()
	defer cancel()
	results := make(chan error, 2+len(m.extraTasks))
	go func() { results <- m.program.Run(run, m.db, m.session) }()
	go func() { results <- m.projection(run) }()
	for _, task := range m.extraTasks {
		go func() { results <- task(run) }()
	}
	errs := []error{<-results}
	cancel()
	for i := 1; i < 2+len(m.extraTasks); i++ {
		errs = append(errs, <-results)
	}
	return errors.Join(errs...)
}
func (m *managedFeed) project(ctx context.Context) error {
	f, epoch := m.first, m.firstEpoch
	for restarts := 0; ; restarts++ {
		if restarts > 64 {
			f.Close()
			return capture.ErrRebuildRequired
		}
		fetch.AttachProgress(m.storage, f)
		f.StartRelay()
		run, cancel := context.WithCancel(ctx)
		result := make(chan error, 1)
		go func() { result <- f.Run(run) }()
		ticker := time.NewTicker(50 * time.Millisecond)
		changed := false
		var err error
	wait:
		for {
			select {
			case err = <-result:
				break wait
			case <-ctx.Done():
				cancel()
				err = <-result
				break wait
			case <-ticker.C:
				state, e := m.store.State(ctx)
				if e != nil || state.Generation != epoch {
					changed = e == nil
					cancel()
					err = errors.Join(e, <-result)
					break wait
				}
			}
		}
		ticker.Stop()
		cancel()
		f.Close() // joins online backfill, writer and relay before any replacement
		if !changed || ctx.Err() != nil || (err != nil && !errors.Is(err, context.Canceled)) {
			return err
		}
		m.log.Info("Restarting DB facts projection after capture reset", zap.Uint64("previous_epoch", epoch))
		var e error
		f, epoch, e = m.build(ctx)
		if e != nil {
			return e
		}
	}
}
