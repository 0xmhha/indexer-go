package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestReservedQueryReaderHTTPWhileBackgroundFull(t *testing.T) {
	dsn := os.Getenv("INDEXER_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set INDEXER_TEST_POSTGRES")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	schema := fmt.Sprintf("api_reader_%d", time.Now().UnixNano())
	store, err := postgres.Open(ctx, postgres.Options{DSN: dsn, Schema: schema, MaxConns: 4})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
		conn, err := pgx.Connect(context.Background(), dsn)
		if err == nil {
			defer func() { _ = conn.Close(context.Background()) }()
			_, err = conn.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
			require.NoError(t, err)
		}
	})
	require.NoError(t, store.SetBlock(ctx, &model.Block{Number: 1, Hash: common.Hash{1}}))
	cfg := DefaultConfig()
	cfg.ReserveQueryReader = true
	server, err := NewServer(cfg, zap.NewNop(), store)
	require.NoError(t, err)
	require.Equal(t, 1, server.ReservedQueryReaders())
	defer func() { require.NoError(t, server.Stop(context.Background())) }()
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()
	bound, tx, err := store.BeginBlock(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, store.SetBlock(bound, &model.Block{Number: 2, Hash: common.Hash{2}}))
	ch := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- store.CaptureView(ctx, ch, func(port.CaptureReader) error {
				entered <- struct{}{}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
	}
	defer func() {
		close(release)
		wg.Wait()
		for i := 0; i < 2; i++ {
			require.NoError(t, <-results)
		}
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	waiting, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stop()
	queued := make(chan error, 1)
	var unexpected atomic.Bool
	go func() {
		queued <- store.CaptureView(waiting, ch, func(port.CaptureReader) error { unexpected.Store(true); return nil })
	}()
	client := httpServer.Client()
	client.Timeout = 2 * time.Second
	query := func(number int) string {
		response, err := client.Post(httpServer.URL+"/graphql", "application/json", bytes.NewBufferString(fmt.Sprintf(`{"query":"{block(number:\"%d\"){number hash}}"}`, number)))
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		return string(body)
	}
	require.Contains(t, query(1), common.Hash{1}.Hex(), "HTTP reads use the pinned fourth slot")
	require.Contains(t, query(2), `"errors"`, "staged writer data is invisible to the API")
	require.ErrorIs(t, <-queued, context.DeadlineExceeded)
	require.False(t, unexpected.Load())
}
