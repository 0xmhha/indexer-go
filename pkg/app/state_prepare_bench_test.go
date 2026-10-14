package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

// A controlled remote-node workload: 24 blocks, each touching eight new
// recipients. Measure zero/5ms RPC round trips and a saturated request cap.
// Node/database construction is excluded. Both modes use identical input.
func BenchmarkStatePreparation(b *testing.B) {
	sender := testchain.NewAccount(700)
	chain := testchain.NewChain(testchain.DefaultChainID, map[common.Address]*big.Int{sender.Address: new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil)})
	for n := 0; n < 24; n++ {
		var specs []testchain.TxSpec
		for i := 0; i < 8; i++ {
			addr := common.BigToAddress(big.NewInt(int64(10000 + n*8 + i)))
			specs = append(specs, testchain.TxSpec{From: sender, Tx: &types.LegacyTx{To: &addr, Value: big.NewInt(1), Gas: 21000, GasPrice: big.NewInt(1_000_000_000)}})
		}
		chain.AddBlock(specs...)
	}
	srv := testchain.NewServer(chain)
	defer srv.Close()
	upstream, err := url.Parse(srv.URL())
	require.NoError(b, err)
	for _, tc := range []struct {
		name  string
		delay time.Duration
		rate  float64
	}{
		{"local", 0, 0}, {"rpc_5ms", 5 * time.Millisecond, 0}, {"limited_50rps", 5 * time.Millisecond, 50},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var requests atomic.Int64
			proxy := httputil.NewSingleHostReverseProxy(upstream)
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				_ = r.Body.Close()
				r.Body = io.NopCloser(bytes.NewReader(raw))
				count := int64(1)
				if len(raw) > 0 && raw[0] == '[' {
					var batch []json.RawMessage
					_ = json.Unmarshal(raw, &batch)
					count = int64(len(batch))
				}
				requests.Add(count)
				if tc.delay > 0 {
					timer := time.NewTimer(tc.delay)
					defer timer.Stop()
					select {
					case <-timer.C:
					case <-r.Context().Done():
						return
					}
				}
				proxy.ServeHTTP(w, r)
			}))
			defer gateway.Close()
			for _, on := range []bool{false, true} {
				name := "ordered"
				if on {
					name = "prepared"
				}
				b.Run(name, func(b *testing.B) {
					b.ReportAllocs()
					var calls int64
					var peak int64
					var writeP95 float64
					for i := 0; i < b.N; i++ {
						b.StopTimer()
						app := newPreparedApp(b, gateway.URL, filepath.Join(b.TempDir(), "db"), on, 0, func(cfg *config.Config) { cfg.RPC.RateLimit = tc.rate; cfg.Indexer.PipelineMetrics = true })
						before := requests.Load()
						b.StartTimer()
						err := app.fetcher.FetchRange(context.Background(), 0, chain.Head())
						b.StopTimer()
						require.NoError(b, err)
						calls += requests.Load() - before
						stats := app.fetcher.GetPipelineMetrics()
						peak = max(peak, stats.PeakStateBytes)
						writeP95 += stats.Write.P95MS
						app.Shutdown()
					}
					b.ReportMetric(float64(chain.Head()+1), "blocks/op")
					b.ReportMetric(float64(calls)/float64(b.N), "RPCs/op")
					b.ReportMetric(float64(peak), "peak_state_bytes")
					b.ReportMetric(writeP95/float64(b.N), "writer_p95_ms")
				})
			}
		})
	}
}
