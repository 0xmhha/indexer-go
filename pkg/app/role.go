package app

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/stream"
)

// Roles (node.role, refactoring plan R4-1). One process indexes a database
// (ingest, or all, which also serves the API); any number of API processes
// (api) serve it. An API process opens the database read-only, indexes
// nothing and runs nothing that writes. Its event bus, which feeds GraphQL
// subscriptions, receives the change stream from the outbox the indexing
// process writes: a relay reads it as this node's group, keeping its
// position in memory (stream.OutboxBusConfig.Ephemeral), so it starts at
// the newest entry; subscribers that need earlier events resume from a
// sequence (fromSequence), which reads the outbox.

// initAPINode starts the parts of an API process: the database (read-only),
// the event bus fed from the change stream, and the node client, used by
// the RPC proxy, genesis balance lookups and token metadata lookups.
func (a *App) initAPINode(ctx context.Context) error {
	if a.config.Verifier.Enabled {
		a.logger.Warn("verifier.enabled is ignored by an API process: contract verification stores its results")
	}
	if err := a.initStorageOnly(ctx); err != nil {
		return err
	}
	a.initEventBus()
	// The notification API (settings, history): the service is not started
	// here, the indexing process delivers and reloads the settings.
	if err := a.initNotificationService(); err != nil {
		return fmt.Errorf("failed to initialize notification service: %w", err)
	}
	if err := a.initClient(); err != nil {
		return err
	}
	if err := a.testConnection(ctx); err != nil {
		return err
	}
	if g, ok := a.storage.(storage.GenesisBalanceConfigurer); ok {
		g.SetGenesisBalanceResolver(a.client)
	}

	ob, ok := a.storage.(port.Outbox)
	if !ok || !a.config.EventBus.Outbox || a.eventBus == nil {
		a.logger.Warn("No change stream: GraphQL subscriptions of this API process receive no events (eventbus.outbox)")
		return nil
	}
	bus := stream.NewOutboxBus(ob, stream.OutboxBusConfig{Ephemeral: true, Poll: a.streamPoll()}, a.logger)
	a.streamRelay = stream.NewRelay(bus, a.config.Node.ID, a.eventBus.Publish, a.logger)
	if _, err := bus.Join(ctx, a.streamRelay.Group(), stream.StartLatest); err != nil {
		return err
	}
	a.logger.Info("API process: serving the database another process indexes",
		storeLocation(&a.config.Database), zap.String("stream_group", a.streamRelay.Group()))
	return nil
}

// streamPoll is how often an API process looks for new outbox entries: the
// indexing process cannot wake it, so it polls at the live loop's interval.
func (a *App) streamPoll() time.Duration {
	if p := a.config.Indexer.PollInterval; p > 0 {
		return p
	}
	return stream.DefaultPoll
}

// runAPINode relays the change stream until ctx ends.
func (a *App) runAPINode(ctx context.Context) error {
	a.logger.Info("Serving the API", zap.String("role", config.RoleAPI))
	if a.streamRelay == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	for {
		err := a.streamRelay.Run(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a.logger.Error("Change stream relay stopped; restarting", zap.Error(err))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
