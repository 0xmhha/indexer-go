package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/notifications"
)

// TestNotificationsFollowTheChangeStream runs the real wiring with the
// notification service on (refactoring plan R3-5): every committed
// transaction of the indexed chain reaches the webhook once, also when the
// app restarts in the middle, because the service consumes the change
// stream as its own group and stores notifications before moving on.
func TestNotificationsFollowTheChangeStream(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	var mu sync.Mutex
	hooks := map[string]int{} // notification id -> deliveries
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hooks[r.Header.Get("X-Webhook-ID")]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	dir := filepath.Join(t.TempDir(), "db")
	start := func() *App {
		cfg := config.NewConfig()
		cfg.RPC.Endpoint = srv.URL()
		cfg.RPC.Timeout = 5 * time.Second
		setTestDatabase(t, cfg, dir)
		cfg.API.Enabled = false
		cfg.Indexer.PollInterval = 10 * time.Millisecond
		cfg.Notifications.Enabled = true
		cfg.Notifications.Webhook.Enabled = true
		cfg.Notifications.Queue.FlushInterval = 20 * time.Millisecond
		enableTestChainFeatures(cfg)
		cfg.SetDefaults()
		app, err := NewApp(cfg, zap.NewNop(), false, "")
		require.NoError(t, err)
		require.NoError(t, app.notificationService.Start(context.Background()))
		return app
	}
	stop := func(app *App) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, app.notificationService.Stop(ctx))
		app.Shutdown()
	}

	ctx := context.Background()
	app := start()
	_, err := app.notificationService.CreateSetting(ctx, &notifications.NotificationSetting{
		ID: "txs", Name: "txs", Type: notifications.NotificationTypeWebhook, Enabled: true,
		EventTypes:  []notifications.EventType{notifications.EventTypeTransaction},
		Destination: notifications.Destination{WebhookURL: hook.URL},
	})
	require.NoError(t, err)
	head := sc.Chain.Head()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head/2))
	stop(app)

	app = start() // the setting and the stream position are kept
	defer stop(app)
	app.fetcher.StartRelay()
	require.NoError(t, app.fetcher.FetchRange(ctx, head/2+1, head))

	entries, err := app.storage.(port.Outbox).ReadOutbox(ctx, 0, 0)
	require.NoError(t, err)
	want := 0
	for _, e := range entries {
		if events.EventType(e.Type) == events.EventTypeTransaction {
			want++
		}
	}
	require.NotZero(t, want)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(hooks) == want
	}, time.Minute, 20*time.Millisecond, "every transaction is notified")
	last := entries[len(entries)-1].Seq
	require.Eventually(t, func() bool {
		pos, ok, err := app.storage.(port.Outbox).OutboxCursor(ctx, notifications.DefaultStreamGroup)
		return err == nil && ok && pos == last
	}, 10*time.Second, 10*time.Millisecond, "the service consumes the change stream as its group")
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, hooks, want)
	for id, n := range hooks {
		require.Equal(t, 1, n, "notification %s delivered once", id)
	}
}
