package capture

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/window"
)

// SessionConfig selects one reservation from a validated shared layout. The
// anchor's chain/genesis is verified, never inferred from a path. The entry
// does not select an adapter, initialize a volume or activate legacy ingest.
type SessionConfig struct {
	RequiredSupplement *model.SupplementStreamDescriptor
	SupplementVerifier SupplementVerifier
	Storage            window.Layout
	Objects            *window.Objects // explicit library composition, separate permanent quota
	Instance           string
	Plan               model.CapturePlan
	Anchor             model.FeedBlock
	RawConsumers       []string
	Create             bool
	UsageInterval      time.Duration
	Logger             *zap.Logger
	// OnUsage optionally receives every raw window usage sample after it is
	// logged, for capacity monitoring; it must return promptly.
	OnUsage func(window.Usage)
}

func RunConfiguredSession(ctx context.Context, db port.CaptureDatabase, fresh source.CanonicalReader, cfg SessionConfig, receive, process Task) error {
	opts, err := configuredOptions(cfg)
	if err != nil {
		return err
	}
	return RunSession(ctx, db, fresh, opts, receive, process)
}

func RunConfiguredFeed(ctx context.Context, db port.CaptureDatabase, fresh source.CanonicalReader, cfg SessionConfig, factory FeedFactory, runtime RuntimeOptions) error {
	opts, err := configuredOptions(cfg)
	if err != nil {
		return err
	}
	return RunFeedSession(ctx, db, fresh, opts, factory, runtime)
}

func configuredOptions(cfg SessionConfig) (SessionOptions, error) {
	opts, err := cfg.Storage.OptionsFor(cfg.Instance, cfg.Anchor.Chain)
	if err != nil {
		return SessionOptions{}, err
	}
	log := cfg.Logger
	if log == nil {
		log = zap.NewNop()
	}
	last := ""
	onUsage := func(u window.Usage) {
		if u.Severity == last {
			return
		}
		last = u.Severity
		fields := []zap.Field{zap.String("reservation", u.Reservation), zap.String("pressure", u.Severity), zap.Int64("file_bytes", u.FileBytes), zap.Int64("max_bytes", u.LimitBytes), zap.Int64("volume_allocated_bytes", u.VolumeAllocatedBytes), zap.Int64("volume_max_bytes", u.VolumeLimitBytes), zap.Uint64("free_bytes", u.FreeBytes), zap.Int64("min_free_bytes", u.MinFreeBytes)}
		if u.Severity == "warning" || u.Severity == "critical" {
			log.Warn("window storage pressure", fields...)
		} else {
			log.Info("window storage pressure", fields...)
		}
	}
	if observe := cfg.OnUsage; observe != nil {
		logUsage := onUsage
		onUsage = func(u window.Usage) {
			logUsage(u)
			observe(u)
		}
	}
	return SessionOptions{Window: opts, Objects: cfg.Objects, Plan: cfg.Plan, Anchor: cfg.Anchor, RawConsumers: cfg.RawConsumers, Create: cfg.Create, UsageInterval: cfg.UsageInterval, OnUsage: onUsage, RequiredSupplement: cfg.RequiredSupplement, SupplementVerifier: cfg.SupplementVerifier}, nil
}
