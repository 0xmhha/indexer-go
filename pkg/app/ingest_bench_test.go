package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/testchain"
)

// BenchmarkIngest compares the legacy and atomic write paths on the same
// chain through the production wiring. Only FetchRange is timed; database
// creation and app start/stop are excluded. RPC goes through the in-process
// test chain, which costs the same for both paths.
//
//	go test ./pkg/app -run '^$' -bench BenchmarkIngest -benchtime 3x
//
// "regular": 200 blocks x 50 transactions (half ERC-20 transfers).
// "large_block": the same plus one block of 1200 transactions, which the
// legacy path processes with parallel workers and the atomic path does not.
func BenchmarkIngest(b *testing.B) {
	cases := []struct {
		name  string
		large int
	}{
		{"regular", 0},
		{"large_block", 1200},
	}
	for _, c := range cases {
		sc := testchain.BuildLoad(200, 50, c.large)
		srv := testchain.NewServer(sc.Chain)
		head := sc.Chain.Head()
		txs := sc.Chain.TxCount()
		for _, mode := range allModes {
			b.Run(c.name+"/"+mode.name, func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					dir := filepath.Join(b.TempDir(), "db")
					app := startAppMode(b, srv, dir, mode)
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
					b.StartTimer()

					if err := app.fetcher.FetchRange(ctx, 0, head); err != nil {
						b.Fatal(err)
					}

					b.StopTimer()
					cancel()
					app.Shutdown()
				}
				b.ReportMetric(float64(head+1), "blocks/op")
				b.ReportMetric(float64(txs), "txs/op")
			})
		}
		srv.Close()
	}
}
