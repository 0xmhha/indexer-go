package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"

	fdmeta "github.com/0xmhha/indexer-go/pkg/chains/stablenet/feedelegation"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestLiveStableNet indexes a running StableNet node twice, once straight
// through and once with crashes injected before commit at several heights,
// and requires identical storage. It also checks StableNet-specific paths the
// test chain cannot exercise (adapter detection, WBFT data, fee delegation).
//
// Skipped unless INDEXER_LIVE_RPC is set, e.g.
//
//	INDEXER_LIVE_RPC=http://127.0.0.1:8600 \
//	INDEXER_LIVE_FD_TXS=0xabc...,0xdef... \
//	go test ./pkg/app -run TestLiveStableNet -v
func TestLiveStableNet(t *testing.T) {
	rpc := os.Getenv("INDEXER_LIVE_RPC")
	if rpc == "" {
		t.Skip("INDEXER_LIVE_RPC not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	ec, err := ethclient.DialContext(ctx, rpc)
	require.NoError(t, err)
	head, err := ec.BlockNumber(ctx)
	require.NoError(t, err)
	ec.Close()
	head = liveHead(t, head)
	t.Logf("indexing blocks 0..%d from %s", head, rpc)

	// Run A: straight through.
	dirA := filepath.Join(t.TempDir(), "a")
	app := startAppAt(t, rpc, dirA, atomicMode)
	require.NotNil(t, app.profile)
	require.Equal(t, "stablenet", app.profile.ID(), "chain profile detection")
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))

	// Fee delegation metadata must be stored for the given transactions.
	if list := os.Getenv("INDEXER_LIVE_FD_TXS"); list != "" {
		fd, err := fdmeta.OpenMetaStore(app.storage)
		require.NoError(t, err)
		for _, h := range strings.Split(list, ",") {
			meta, err := fd.TxMeta(ctx, common.HexToHash(h))
			require.NoError(t, err)
			require.NotNil(t, meta, "fee delegation meta for %s", h)
			require.Equal(t, uint8(0x16), meta.OriginalType)
			t.Logf("fee delegation %s payer %s", h, meta.FeePayer.Hex())
		}
	}
	app.Shutdown()
	a := dumpDir(t, dirA)
	require.NotEmpty(t, prefixCount(a, "/data/wbft/"), "WBFT data must be indexed")
	t.Logf("run A: %d keys, %d WBFT keys, %d fee delegation keys", len(a), prefixCount(a, "/data/wbft/"), prefixCount(a, "/data/feedelegation/"))

	// Run B: crash before commit at several heights, restart each time.
	dirB := filepath.Join(t.TempDir(), "b")
	errCrash := errors.New("injected crash")
	for _, crashAt := range []uint64{head / 4, head / 2, head * 3 / 4} {
		app := startAppAt(t, rpc, dirB, atomicMode)
		app.fetcher.SetBeforeCommitHook(func(h uint64) error {
			if h == crashAt {
				return errCrash
			}
			return nil
		})
		err := app.fetcher.FetchRange(ctx, app.fetcher.GetNextHeight(ctx), head)
		require.ErrorIs(t, err, errCrash, "crash at %d", crashAt)
		app.Shutdown()
	}
	app = startAppAt(t, rpc, dirB, atomicMode)
	require.NoError(t, app.fetcher.FetchRange(ctx, app.fetcher.GetNextHeight(ctx), head))
	app.Shutdown()

	diff := testchain.DiffKeyspace(a, dumpDir(t, dirB), 0)
	require.Empty(t, diff, fmt.Sprint(testchain.SummarizeDiff(diff)))
}

func prefixCount(es []testchain.Entry, prefix string) int {
	n := 0
	for _, e := range es {
		if strings.HasPrefix(string(e.Key), prefix) {
			n++
		}
	}
	return n
}

// liveHead caps the indexed range of the live tests. A local network keeps
// producing blocks, so indexing up to its head makes the tests slower every
// run. INDEXER_LIVE_MAX_HEIGHT sets the cap (default 3000; 0 means no cap).
func liveHead(t *testing.T, head uint64) uint64 {
	t.Helper()
	limit := uint64(3000)
	if v := os.Getenv("INDEXER_LIVE_MAX_HEIGHT"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		require.NoError(t, err, "INDEXER_LIVE_MAX_HEIGHT")
		limit = n
	}
	if limit > 0 && head > limit {
		return limit
	}
	return head
}
