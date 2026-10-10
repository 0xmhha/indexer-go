package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// roleConfig is the configuration of a process of role on the PostgreSQL
// schema of dir, reading the test chain at endpoint.
func roleConfig(t *testing.T, endpoint, dir, role, id string) *config.Config {
	t.Helper()
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = endpoint
	cfg.RPC.Timeout = 5 * time.Second
	dsn := postgresDSN(t)
	schema := testSchema(dir)
	cfg.Database.Path = dir
	cfg.Database.Driver = config.DriverPostgres
	cfg.Database.Postgres = config.PostgresConfig{DSN: dsn, Schema: schema, MaxConns: 4}
	t.Cleanup(func() { dropTestSchemas(dsn, schema) })
	cfg.Node.Role = role
	cfg.Node.ID = id
	cfg.API.Enabled = false
	cfg.Indexer.PollInterval = 10 * time.Millisecond
	enableTestChainFeatures(cfg)
	require.NoError(t, cfg.Validate())
	require.NoError(t, validateConfig(cfg))
	return cfg
}

// TestAPIProcessesServeTheIndex: one ingest process indexes a PostgreSQL
// database and several API processes serve it at the same time (refactoring
// plan R4-1). The API processes answer the same as the index, refuse
// writes, and their subscribers receive the events of blocks indexed after
// they started, numbered as the outbox numbers them.
func TestAPIProcessesServeTheIndex(t *testing.T) {
	postgresDSN(t)
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// An API process cannot start before the schema exists: the indexing
	// process creates it.
	_, err := NewApp(roleConfig(t, srv.URL(), dir, config.RoleAPI, "early"), zap.NewNop(), false, "")
	require.ErrorContains(t, err, "start an ingest process to migrate")

	ingest, err := NewApp(roleConfig(t, srv.URL(), dir, config.RoleIngest, "ingest"), zap.NewNop(), false, "")
	require.NoError(t, err)
	defer ingest.Shutdown()
	half := sc.Chain.Head() / 2
	require.NoError(t, ingest.fetcher.FetchRange(ctx, 0, half))

	const apis = 3
	nodes := make([]*App, apis)
	subs := make([]*events.Subscription, apis)
	var wg sync.WaitGroup
	runCtx, stop := context.WithCancel(ctx)
	for i := range nodes {
		app, err := NewApp(roleConfig(t, srv.URL(), dir, config.RoleAPI, "api-"+string(rune('a'+i))), zap.NewNop(), false, "")
		require.NoError(t, err)
		defer app.Shutdown()
		require.Nil(t, app.fetcher, "an API process indexes nothing")
		nodes[i] = app
		subs[i] = app.eventBus.Subscribe("all", allEventTypes(), nil, 1<<16)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := app.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("api process: %v", err)
			}
		}()
	}

	// The rest of the chain is indexed while the API processes run.
	first, err := ingest.storage.(port.Outbox).LastOutboxSeq(ctx)
	require.NoError(t, err)
	require.NoError(t, ingest.fetcher.FetchRange(ctx, half+1, sc.Chain.Head()))
	last, err := ingest.storage.(port.Outbox).LastOutboxSeq(ctx)
	require.NoError(t, err)
	require.Greater(t, last, first)

	want := queryAll(t, ingest)
	for i, app := range nodes {
		assert.Equal(t, want, queryAll(t, app), "api process %d answers as the index", i)
		assert.ErrorIs(t, app.storage.SetLatestHeight(ctx, 1), port.ErrReadOnly, "api process %d refuses writes", i)

		evs := drain(t, subs[i])
		require.NotEmpty(t, evs, "api process %d receives the new blocks' events", i)
		requireSequence(t, evs, first+1)
		assert.Equal(t, last, events.SequenceOf(evs[len(evs)-1]), "api process %d receives every event", i)
	}
	stop()
	wg.Wait()
}

// queryAll answers the reference GraphQL queries through app's store.
func queryAll(t *testing.T, app *App) string {
	t.Helper()
	h, err := graphql.NewHandler(app.storage, zap.NewNop())
	require.NoError(t, err)
	queries := append([]struct{ name, query string }{
		{"latestHeight", `{ latestHeight }`},
		{"transactionCount", `{ transactionCount }`},
		{"blocks", `{ blocks(pagination: {limit: 100}) { totalCount nodes { number hash transactionCount } } }`},
	}, graphqlGoldenQueries...)
	results := map[string]any{}
	for _, q := range queries {
		res := h.ExecuteQuery(q.query, nil)
		require.Empty(t, res.Errors, "%s: %v", q.name, res.Errors)
		results[q.name] = res.Data
	}
	out, err := json.Marshal(results)
	require.NoError(t, err)
	return string(out)
}

// TestAPIProcessManagesNotifications: with roles split, the API process
// serves the notification API and stores the settings it creates (its
// database is read-only but for the notification keys); the ingest process
// reloads them and delivers. Before, neither process could create a
// setting.
func TestAPIProcessManagesNotifications(t *testing.T) {
	postgresDSN(t)
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	var hooks atomic.Int64
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hooks.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()
	notify := func(cfg *config.Config) *config.Config {
		cfg.Notifications.Enabled = true
		cfg.Notifications.AllowPrivateDestinations = true // the hook is a local test server
		cfg.Notifications.Webhook.Enabled = true
		cfg.Notifications.Queue.FlushInterval = 20 * time.Millisecond
		cfg.SetDefaults()
		return cfg
	}
	dir := filepath.Join(t.TempDir(), "db")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ingest, err := NewApp(notify(roleConfig(t, srv.URL(), dir, config.RoleIngest, "ingest")), zap.NewNop(), false, "")
	require.NoError(t, err)
	defer ingest.Shutdown()
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- ingest.Run(runCtx) }()
	defer func() { stop(); <-done }()

	api, err := NewApp(notify(roleConfig(t, srv.URL(), dir, config.RoleAPI, "api")), zap.NewNop(), false, "")
	require.NoError(t, err)
	defer api.Shutdown()
	require.NotNil(t, api.notificationService, "the API process serves the notification API")
	_, err = api.notificationService.CreateSetting(ctx, &notifications.NotificationSetting{
		ID: "txs", Name: "txs", Type: notifications.NotificationTypeWebhook, Enabled: true,
		EventTypes:  []notifications.EventType{notifications.EventTypeTransaction},
		Destination: notifications.Destination{WebhookURL: hook.URL},
	})
	require.NoError(t, err, "the API process stores the setting")
	assert.ErrorIs(t, api.storage.SetLatestHeight(ctx, 1), port.ErrReadOnly, "other writes stay refused")

	// New blocks until the ingest process has reloaded the setting.
	deadline := time.Now().Add(30 * time.Second)
	for hooks.Load() == 0 && time.Now().Before(deadline) {
		extendChain(sc, 1)
		time.Sleep(300 * time.Millisecond)
	}
	require.Positive(t, hooks.Load(), "the ingest process delivers for a setting the API process created")
}
