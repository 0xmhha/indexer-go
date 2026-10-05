package fetch

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/testchain"
	_ "github.com/0xmhha/indexer-go/pkg/chains/evm" // generic profile for the test chain
	"github.com/0xmhha/indexer-go/pkg/client"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/source"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// chainHarness indexes the deterministic test chain the way the indexer
// does: blocks read through the chain profile source, each indexed in one
// storage transaction of a Pebble database.
type chainHarness struct {
	chain *testchain.Chain
	srv   *testchain.Server
	db    *storagepkg.PebbleStorage
	src   *flakySource
	f     *Fetcher
}

func newChainHarness(t *testing.T, cfg *Config, bus *events.EventBus) *chainHarness {
	t.Helper()
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)

	ctx := context.Background()
	rc, err := gethrpc.DialContext(ctx, srv.URL())
	require.NoError(t, err)
	t.Cleanup(rc.Close)
	src, err := sourcerpc.Detect(ctx, rc)
	require.NoError(t, err)

	c, err := client.NewClient(&client.Config{Endpoint: srv.URL(), Timeout: 5 * time.Second, Logger: zap.NewNop()})
	require.NoError(t, err)
	t.Cleanup(c.Close)

	db, err := storagepkg.NewPebbleStorage(storagepkg.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = time.Millisecond
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 10
	}
	h := &chainHarness{chain: sc.Chain, srv: srv, db: db, src: &flakySource{Source: src}}
	h.f = NewFetcher(c, db, cfg, zap.NewNop(), bus)
	h.f.SetSource(h.src)
	t.Cleanup(h.f.Close)
	return h
}

// requireIndexed requires blocks from..to to be stored with the chain's
// hashes and the cursor at to.
func (h *chainHarness) requireIndexed(t *testing.T, from, to uint64) {
	t.Helper()
	ctx := context.Background()
	for n := from; n <= to; n++ {
		b, err := h.db.GetModelBlock(ctx, n)
		require.NoError(t, err, "block %d", n)
		require.Equal(t, h.chain.Block(n).Block.Hash(), b.Hash, "block %d", n)
	}
	latest, err := h.db.GetLatestHeight(ctx)
	require.NoError(t, err)
	require.Equal(t, to, latest)
}

// flakySource fails the next failures block reads with errFlaky.
type flakySource struct {
	source.Source
	mu       sync.Mutex
	failures int
	reads    int
}

var errFlaky = errors.New("flaky source")

func (s *flakySource) failNext(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures = n
}

func (s *flakySource) BlockWithReceipts(ctx context.Context, n uint64) (*model.Block, []*model.Receipt, error) {
	s.mu.Lock()
	s.reads++
	fail := s.failures > 0
	if fail {
		s.failures--
	}
	s.mu.Unlock()
	if fail {
		return nil, nil, errFlaky
	}
	return s.Source.BlockWithReceipts(ctx, n)
}

func (s *flakySource) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	return s.Source.HashAt(ctx, n)
}
