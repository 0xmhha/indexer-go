package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/notifications"
)

// TestLiveNotificationLatency measures the real-time subscription path on
// a running node (subscriptions design 4.8, phase 6): the indexer follows
// the node (newHeads with INDEXER_LIVE_WS, else polling), a fast stream
// setting watches the transactions of the account whose key is
// INDEXER_LIVE_KEY (hex, funded on that node), and the test sends
// INDEXER_LIVE_NOTIFY_TXS (default 20) transfers to itself. Per
// transaction it reports the time from the node first showing its block
// (the test's own newHeads subscription, or polling every 2 ms) to the
// stream message, and checks p99 against INDEXER_NOTIFY_P99_MS (default
// 20). Fees: INDEXER_LIVE_TIP_GWEI (default 30000) and
// INDEXER_LIVE_FEE_GWEI (default 80000) suit StableNet.
//
// Runs only with INDEXER_LIVE_RPC, INDEXER_LIVE_KEY and
// INDEXER_LIVE_NOTIFY=1, on a node that produces blocks.
func TestLiveNotificationLatency(t *testing.T) {
	endpoint, keyHex := os.Getenv("INDEXER_LIVE_RPC"), os.Getenv("INDEXER_LIVE_KEY")
	if endpoint == "" || keyHex == "" || os.Getenv("INDEXER_LIVE_NOTIFY") == "" {
		t.Skip("INDEXER_LIVE_RPC, INDEXER_LIVE_KEY and INDEXER_LIVE_NOTIFY=1 not set")
	}
	wsEndpoint := os.Getenv("INDEXER_LIVE_WS")
	count := envInt(t, "INDEXER_LIVE_NOTIFY_TXS", 20)
	bound := time.Duration(envInt(t, "INDEXER_NOTIFY_P99_MS", int(notifyEndToEndP99/time.Millisecond))) * time.Millisecond
	tip := big.NewInt(int64(envInt(t, "INDEXER_LIVE_TIP_GWEI", 30000)) * 1e9)
	feeCap := big.NewInt(int64(envInt(t, "INDEXER_LIVE_FEE_GWEI", 80000)) * 1e9)

	key, err := crypto.HexToECDSA(strings.TrimPrefix(keyHex, "0x"))
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(count)*5*time.Second+time.Minute)
	defer cancel()
	ec, err := ethclient.DialContext(ctx, endpoint)
	require.NoError(t, err)
	defer ec.Close()
	chainID, err := ec.ChainID(ctx)
	require.NoError(t, err)
	head, err := ec.BlockNumber(ctx)
	require.NoError(t, err)

	// When the node first shows each block.
	var mu sync.Mutex
	seen := map[uint64]time.Time{}
	note := func(n uint64) {
		now := time.Now()
		mu.Lock()
		if _, ok := seen[n]; !ok {
			seen[n] = now
		}
		mu.Unlock()
	}
	if wsEndpoint != "" {
		wc, err := ethclient.DialContext(ctx, wsEndpoint)
		require.NoError(t, err)
		defer wc.Close()
		heads := make(chan *types.Header, 64)
		sub, err := wc.SubscribeNewHead(ctx, heads)
		require.NoError(t, err)
		defer sub.Unsubscribe()
		go func() {
			for h := range heads {
				note(h.Number.Uint64())
			}
		}()
	} else {
		go func() {
			for ctx.Err() == nil {
				if n, err := ec.BlockNumber(ctx); err == nil {
					note(n)
				}
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}

	apiKey := "live-key-0123456789abcdef0123"
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = endpoint
	cfg.RPC.WSEndpoint = wsEndpoint
	cfg.RPC.Timeout = 5 * time.Second
	cfg.Database.Path = filepath.Join(t.TempDir(), "db")
	cfg.Indexer.StartHeight = head
	cfg.API.Enabled = true
	cfg.API.Host = "127.0.0.1"
	cfg.API.Port = freeAPIPort(t)
	cfg.API.EnableGraphQL = true
	cfg.API.Keys = map[string]string{"live": apiKey}
	cfg.Notifications.Enabled = true
	cfg.SetDefaults()
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	defer app.Shutdown()
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.Run(runCtx) }()
	defer func() { stop(); <-done }()

	base := fmt.Sprintf("127.0.0.1:%d", cfg.API.Port)
	created := map[string]any{}
	require.Eventually(t, func() bool {
		data, _ := json.Marshal(map[string]any{
			"query": `mutation($in: CreateNotificationSettingInput!) { createNotificationSetting(input: $in) { id } }`,
			"variables": map[string]any{"in": map[string]any{"name": "live", "type": "STREAM", "delivery": "fast",
				"eventTypes": []string{"TRANSACTION"}, "destination": map[string]any{},
				"filter": map[string]any{"addresses": []string{from.Hex()}}}},
		})
		req, _ := http.NewRequest(http.MethodPost, "http://"+base+"/graphql", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", apiKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return json.NewDecoder(resp.Body).Decode(&created) == nil && created["errors"] == nil
	}, 30*time.Second, 50*time.Millisecond, "setting created: %v", created)
	conn, _, err := gws.DefaultDialer.Dial("ws://"+base+"/v1/subscriptions/stream", http.Header{"X-API-Key": {apiKey}})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	arrived := map[string]time.Time{}
	blockOf := map[string]uint64{}
	var readErr error
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		_ = conn.SetReadDeadline(time.Now().Add(time.Duration(count)*5*time.Second + 30*time.Second))
		for len(arrived) < count {
			var m notifications.StreamMessage
			if readErr = conn.ReadJSON(&m); readErr != nil {
				return
			}
			if m.Type != notifications.StreamNotification {
				continue
			}
			var tx struct {
				Hash string `json:"hash"`
			}
			if readErr = json.Unmarshal(m.Notification.Payload.Data, &tx); readErr != nil {
				return
			}
			h := strings.ToLower(tx.Hash)
			if _, ok := arrived[h]; !ok {
				arrived[h], blockOf[h] = time.Now(), m.Notification.Payload.BlockNumber
			}
		}
	}()

	nonce, err := ec.PendingNonceAt(ctx, from)
	require.NoError(t, err)
	signer := types.LatestSignerForChainID(chainID)
	for i := 0; i < count; i++ {
		tx, err := types.SignNewTx(key, signer, &types.DynamicFeeTx{ChainID: chainID, Nonce: nonce + uint64(i),
			GasTipCap: tip, GasFeeCap: feeCap, Gas: 21000, To: &from, Value: big.NewInt(1)})
		require.NoError(t, err)
		require.NoError(t, ec.SendTransaction(ctx, tx))
		// Spread the transactions over blocks.
		receipt, err := waitReceipt(ctx, ec, tx.Hash())
		require.NoError(t, err)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
	}
	<-readDone
	require.NoError(t, readErr)

	var lat []time.Duration
	mu.Lock()
	for h, at := range arrived {
		shown, ok := seen[blockOf[h]]
		require.True(t, ok, "block %d of %s seen", blockOf[h], h)
		lat = append(lat, at.Sub(shown))
	}
	mu.Unlock()
	s := statsOf(lat)
	t.Logf("%d transactions on chain %s, ws %q: node shows block -> stream message %v", len(lat), chainID, wsEndpoint, s)
	require.LessOrEqual(t, s.p99, bound, "fast stream p99 from the node showing the block")
}

// waitReceipt polls for a transaction's receipt.
func waitReceipt(ctx context.Context, ec *ethclient.Client, hash common.Hash) (*types.Receipt, error) {
	for {
		r, err := ec.TransactionReceipt(ctx, hash)
		if err == nil {
			return r, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}
