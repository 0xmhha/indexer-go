package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// IndexForCheck indexes the chain cfg names until block head is indexed
// and every online backfill has finished, and returns the keys under
// prefixes with their values. It is one indexing run of the determinism
// check (pkg/sdk/sdktest.RequireDeterministic); the API is not served.
func IndexForCheck(ctx context.Context, cfg *config.Config, head uint64, prefixes []string) (map[string][]byte, error) {
	cfg.API.Enabled = false
	a, err := NewApp(cfg, zap.NewNop(), false, "")
	if err != nil {
		return nil, err
	}
	defer a.Shutdown()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.fetcher.Run(runCtx) }()
	for {
		if h, err := a.storage.GetLatestHeight(ctx); err == nil && h >= head {
			break
		}
		select {
		case err := <-done:
			return nil, fmt.Errorf("indexing stopped before block %d: %w", head, err)
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		return nil, err
	}
	a.fetcher.WaitBackground()

	kv, ok := a.storage.(port.KV)
	if !ok {
		return nil, errors.New("the storage has no key-value store")
	}
	out := map[string][]byte{}
	for _, p := range prefixes {
		err := kv.Scan(ctx, []byte(p), storage.PrefixEnd([]byte(p)), false, func(k, v []byte) bool {
			out[string(k)] = v
			return true
		})
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
	}
	return out, nil
}
