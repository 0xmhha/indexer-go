package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestFastNotificationsArriveBeforeTheCommit: a fast setting's
// notification of a block's transaction reaches the webhook while that
// block's commit is held, so it does not wait for the block to be stored,
// and every transaction is notified once (the durable path skips fast
// settings). A durable setting is notified only after the commit.
func TestFastNotificationsArriveBeforeTheCommit(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	var mu sync.Mutex
	byHook := map[string]map[string]int{} // setting name -> tx hash -> deliveries
	arrived := make(chan string, 1000)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Data struct {
				BlockNumber uint64          `json:"block_number"`
				Data        json.RawMessage `json:"data"`
			} `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var tx struct {
			Hash string `json:"hash"`
		}
		_ = json.Unmarshal(body.Data.Data, &tx)
		name := r.URL.Path[1:]
		mu.Lock()
		if byHook[name] == nil {
			byHook[name] = map[string]int{}
		}
		byHook[name][tx.Hash]++
		mu.Unlock()
		arrived <- fmt.Sprintf("%s/%d", name, body.Data.BlockNumber)
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
	cfg.API.Enabled = false
	cfg.Notifications.Enabled = true
	cfg.Notifications.Webhook.Enabled = true
	cfg.Notifications.AllowPrivateDestinations = true // the hook is a local test server
	cfg.Notifications.Queue.FlushInterval = 20 * time.Millisecond
	enableTestChainFeatures(cfg)
	cfg.SetDefaults()
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	defer app.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	svc := app.notificationService.(*notifications.NotificationService)
	require.NoError(t, svc.Start(ctx))
	defer func() { _ = svc.Stop(context.Background()) }()
	for _, d := range []notifications.Delivery{notifications.DeliveryFast, notifications.DeliveryDurable} {
		_, err := svc.CreateSetting(ctx, &notifications.NotificationSetting{
			Name: string(d), Type: notifications.NotificationTypeWebhook, Enabled: true, Delivery: d,
			EventTypes:  []notifications.EventType{notifications.EventTypeTransaction},
			Destination: notifications.Destination{WebhookURL: hook.URL + "/" + string(d)},
		})
		require.NoError(t, err)
	}
	require.True(t, svc.Active(), "a fast setting turns the fast path on")
	app.fetcher.StartRelay()

	// Hold the commit of block 1 until the fast notification of its
	// transaction has arrived; no durable one may arrive meanwhile.
	var heldOK, durableEarly bool
	app.fetcher.SetBeforeCommitHook(func(height uint64) error {
		if height != 1 {
			return nil
		}
		timeout := time.After(10 * time.Second)
		for {
			select {
			case got := <-arrived:
				durableEarly = durableEarly || got == "durable/1"
				if got == "fast/1" {
					heldOK = true
					return nil
				}
			case <-timeout:
				return nil
			}
		}
	})
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))
	app.fetcher.SetBeforeCommitHook(nil)
	require.True(t, heldOK, "the fast notification of block 1 arrived while its commit was held")
	require.False(t, durableEarly, "a durable notification waits for the commit")

	txs := 0
	for n := uint64(0); n <= sc.Chain.Head(); n++ {
		b, err := app.storage.GetBlock(ctx, n)
		require.NoError(t, err)
		txs += len(b.Transactions)
	}
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(byHook["fast"]) == txs && len(byHook["durable"]) == txs
	}, time.Minute, 20*time.Millisecond, "both settings are notified of every transaction")
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for name, hashes := range byHook {
		for h, n := range hashes {
			assert.Equal(t, 1, n, "%s: transaction %s notified once", name, h)
		}
	}
	assert.Zero(t, svc.FastDropped())
}
