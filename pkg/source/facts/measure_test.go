package facts_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/rpcpool"
	"github.com/0xmhha/indexer-go/pkg/source/facts"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type factsReadCounts struct{ Views, Gets, Batches int64 }
type factsCountDB struct {
	port.CaptureDatabase
	views, gets, batches atomic.Int64
}
type factsCountReader struct {
	port.CaptureReader
	db *factsCountDB
}

func (r factsCountReader) Get(key []byte) ([]byte, error) {
	r.db.gets.Add(1)
	return r.CaptureReader.Get(key)
}

type factsCountBatch struct {
	factsCountReader
	batch port.CaptureBatchReader
}

func (r factsCountBatch) GetMany(keys [][]byte) (map[string][]byte, error) {
	r.db.batches.Add(1)
	return r.batch.GetMany(keys)
}
func (db *factsCountDB) CaptureView(c context.Context, chain model.FeedChain, fn func(port.CaptureReader) error) error {
	db.views.Add(1)
	return db.CaptureDatabase.CaptureView(c, chain, func(r port.CaptureReader) error {
		counted := factsCountReader{r, db}
		if batch, ok := r.(port.CaptureBatchReader); ok {
			return fn(factsCountBatch{counted, batch})
		}
		return fn(counted)
	})
}
func (db *factsCountDB) counts() factsReadCounts {
	return factsReadCounts{db.views.Load(), db.gets.Load(), db.batches.Load()}
}

type factsLatency struct{ P50, P95, P99 float64 }

type factsMeasureTransport struct {
	mu       sync.Mutex
	attempts []time.Time
}

func (r *factsMeasureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.attempts = append(r.attempts, time.Now())
	r.mu.Unlock()
	return http.DefaultTransport.RoundTrip(req)
}
func (r *factsMeasureTransport) times() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.attempts...)
}

func factsPercentiles(values []float64) factsLatency {
	sort.Float64s(values)
	at := func(p float64) float64 { return values[int(math.Ceil(p*float64(len(values))))-1] }
	return factsLatency{at(.5), at(.95), at(.99)}
}

type factsTimedSource struct {
	*facts.Source // preserve optional projection/validation/ACK contracts
	mu            sync.Mutex
	reads         []float64
	acks          []time.Time
}

func (s *factsTimedSource) BlockWithReceipts(c context.Context, n uint64) (*model.Block, []*model.Receipt, error) {
	start := time.Now()
	b, rs, e := s.Source.BlockWithReceipts(c, n)
	s.mu.Lock()
	s.reads = append(s.reads, float64(time.Since(start))/float64(time.Millisecond))
	s.mu.Unlock()
	return b, rs, e
}
func (s *factsTimedSource) Acknowledge(c context.Context, b *model.Block) error {
	if e := s.Source.Acknowledge(c, b); e != nil {
		return e
	}
	s.mu.Lock()
	s.acks = append(s.acks, time.Now())
	s.mu.Unlock()
	return nil
}

// Opt-in fixed Arc input comparison. It measures DB-only source reads and the
// REAL ordered feature writer, durable commit and source ACK on the same DB.
// Fresh canonical requests still use the normal shared 5 HTTP/s, burst-1 pool.
// This is a local replay measurement, not live P2P acquisition or an Arc SLO.
func TestMeasureFactsWriter(t *testing.T) {
	if os.Getenv("INDEXER_MEASURE_FACTS_WRITER") != "1" {
		t.Skip("set INDEXER_MEASURE_FACTS_WRITER=1 for the fixed-input writer comparison")
	}
	backend := "pebble"
	maxConns := int64(1)
	if os.Getenv("INDEXER_TEST_POSTGRES") != "" {
		backend = "postgres"
		if configured := os.Getenv("INDEXER_MEASURE_FACTS_MAX_CONNS"); configured != "" {
			var e error
			maxConns, e = strconv.ParseInt(configured, 10, 32)
			require.NoError(t, e)
			require.Contains(t, []int64{1, 2, 4}, maxConns, "fixed comparison budgets only")
		}
	}
	for _, layout := range []string{"inline", "objects"} {
		t.Run(layout, func(t *testing.T) {
			const heights = 32
			db := dbFactoryWithConns(t, backend, int32(maxConns))()
			defer func() { require.NoError(t, db.Close()) }()
			counted := &factsCountDB{CaptureDatabase: db}
			from, to := testchain.NewAccount(21), testchain.NewAccount(22)
			allocation := new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)
			chain := testchain.NewChain(5042002, map[common.Address]*big.Int{from.Address: allocation})
			for n := 1; n <= heights; n++ {
				specs := []testchain.TxSpec{}
				for tx := 0; tx < 2; tx++ {
					logs := []*types.Log{}
					for i := 0; i < 8; i++ {
						logs = append(logs, &types.Log{Address: common.Address{byte(n % 16)}, Topics: []common.Hash{{7}, {byte(i % 4)}}, Data: []byte("fixed-facts-writer-fixture")})
					}
					specs = append(specs, testchain.TxSpec{From: from, Tx: &types.DynamicFeeTx{To: &to.Address, Gas: 50000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000)}, GasUsed: 21000, Logs: logs})
				}
				chain.AddBlock(specs...)
			}
			chain.SetHead(heights)
			feed := model.FeedChain{ID: "5042002", GenesisHash: chain.Block(0).Block.Hash()}
			fresh := &oracle{chain: chain}
			var objects *window.Objects
			if layout == "objects" {
				v := window.VolumeSpec{Root: t.TempDir(), ID: "facts-writer-measure", AllowDirectory: true}
				require.NoError(t, window.InitializeVolume(v))
				var e error
				objects, e = window.CreateObjects(window.ObjectOptions{Volume: v, Path: filepath.Join(v.Root, "objects"), Chain: feed, MaxBytes: 32 << 20})
				require.NoError(t, e)
				defer func() { require.NoError(t, objects.Close()) }()
			}
			store, e := capture.New(counted, feed, fresh)
			if objects != nil {
				store, e = capture.NewWithObjects(counted, feed, fresh, objects)
			}
			require.NoError(t, e)
			require.NoError(t, store.Initialize(ctx, evmdata.BasePlan(), model.FeedBlock{Chain: feed, Hash: feed.GenesisHash}, nil))
			fixture := &fixture{chain: chain, profile: arc.New(), fresh: fresh, store: store, feed: feed}
			for n := uint64(1); n <= heights; n++ {
				in := assembleFact(t, fixture, n)
				if objects != nil {
					for _, p := range in.Payloads {
						ref, e := objects.Put(ctx, sha256.Sum256(p.Bytes), int64(len(p.Bytes)), bytes.NewReader(p.Bytes))
						require.NoError(t, e)
						in.Objects = append(in.Objects, capture.ObjectPayload{Kind: p.Kind, Ref: ref})
					}
					in.Payloads = nil
				}
				require.NoError(t, store.Commit(ctx, in))
				fixture.inputs = append(fixture.inputs, in)
			}
			server := testchain.NewServer(chain)
			defer server.Close()
			transport := &factsMeasureTransport{}
			pool, e := rpcpool.New(rpcpool.Config{Endpoints: []string{server.URL()}, RateLimit: 5, Burst: 1, CoalesceReads: true, Transport: transport})
			require.NoError(t, e)
			rpc, e := gethrpc.DialOptions(ctx, pool.Primary(), gethrpc.WithHTTPClient(&http.Client{Transport: pool}))
			require.NoError(t, e)
			defer rpc.Close()
			src, e := facts.New(ctx, facts.Config{Store: store, Profile: fixture.profile, Canonical: sourcerpc.New(rpc, fixture.profile), Consumer: "measure-writer"})
			require.NoError(t, e)
			// Warm all inputs, then time 64 rotating block reads with no RPC.
			for n := uint64(1); n <= heights; n++ {
				_, _, e = src.BlockWithReceipts(ctx, n)
				require.NoError(t, e)
			}
			beforeRPC := server.Calls()
			countsBefore := counted.counts()
			latencies := []float64{}
			digest := sha256.New()
			for i := 0; i < 64; i++ {
				start := time.Now()
				b, rs, e := src.BlockWithReceipts(ctx, uint64(i%heights+1))
				latencies = append(latencies, float64(time.Since(start))/float64(time.Millisecond))
				require.NoError(t, e)
				value, e := model.EncodeBlock(b)
				require.NoError(t, e)
				_, e = digest.Write(value)
				require.NoError(t, e)
				for _, r := range rs {
					value, e := model.EncodeReceipt(r)
					require.NoError(t, e)
					_, e = digest.Write(value)
					require.NoError(t, e)
				}
			}
			countsAfter := counted.counts()
			require.Equal(t, beforeRPC, server.Calls(), "DB-only materialization must make no RPC")
			readCounts := factsReadCounts{countsAfter.Views - countsBefore.Views, countsAfter.Gets - countsBefore.Gets, countsAfter.Batches - countsBefore.Batches}
			// balance.native is disabled because base facts carry no state coverage.
			// address.index and token.transfers run through the normal feature writer.
			timed := &factsTimedSource{Source: src}
			writer := makeFetcher(t, db, timed, src, src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop()}))
			pipeline, e := feature.Build([]string{"address.index", "token.transfers"}, src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop(), Publish: writer.Publish}))
			require.NoError(t, e)
			writer.SetFeatures(pipeline)
			countsBefore = counted.counts()
			attemptsBefore := len(transport.times())
			started := time.Now()
			require.NoError(t, writer.FetchRange(ctx, 1, heights))
			elapsed := time.Since(started)
			countsAfter = counted.counts()
			stats := writer.GetPipelineMetrics()
			writer.Close()
			require.EqualValues(t, heights, stats.SourceValidate.Count)
			require.Zero(t, stats.SourceValidate.Errors)
			require.EqualValues(t, heights, stats.SourceAck.Count)
			require.Zero(t, stats.SourceAck.Errors)
			calls := server.Calls()
			require.Equal(t, heights, calls["eth_getBlockByNumber"]-beforeRPC["eth_getBlockByNumber"])
			require.EqualValues(t, calls["eth_getBlockByNumber"], pool.Status()[0].Requests)
			require.Zero(t, calls["eth_getBlockReceipts"])
			require.Zero(t, calls["eth_getBalance"])
			require.GreaterOrEqual(t, elapsed, (heights-1)*200*time.Millisecond)
			attempts := transport.times()[attemptsBefore:]
			require.Len(t, attempts, heights)
			minInterval := time.Hour
			for i := 1; i < len(attempts); i++ {
				minInterval = min(minInterval, attempts[i].Sub(attempts[i-1]))
				// Allow scheduler jitter while checking the entire burst-1 envelope.
				require.GreaterOrEqual(t, attempts[i].Sub(attempts[0]), time.Duration(i)*200*time.Millisecond-20*time.Millisecond)
			}
			output := sha256.New()
			for n := uint64(1); n <= heights; n++ {
				b, e := db.GetBlock(ctx, n)
				require.NoError(t, e)
				want, rs, e := src.BlockWithReceipts(ctx, n)
				require.NoError(t, e)
				a, e := model.EncodeBlock(b)
				require.NoError(t, e)
				v, e := model.EncodeBlock(want)
				require.NoError(t, e)
				require.Equal(t, v, a)
				_, e = output.Write(a)
				require.NoError(t, e)
				for _, r := range rs {
					got, e := db.GetReceipt(ctx, r.TxHash)
					require.NoError(t, e)
					a, e := model.EncodeReceipt(got)
					require.NoError(t, e)
					v, e := model.EncodeReceipt(r)
					require.NoError(t, e)
					require.Equal(t, v, a)
					_, e = output.Write(a)
					require.NoError(t, e)
				}
				ack, e := store.ProjectionAck(ctx, "measure-writer", src.Epoch(), fixture.inputs[n-1].Manifest.Identity)
				require.NoError(t, e)
				require.Equal(t, fixture.inputs[n-1].Manifest.Identity, ack.Identity)
			}
			outbox, e := db.(port.Outbox).ReadOutbox(ctx, 0, 1000)
			require.NoError(t, e)
			for _, event := range outbox {
				var data map[string]any
				require.NoError(t, json.Unmarshal(event.Data, &data))
				delete(data, "created_at") // wall-clock event creation is not consensus data
				value, e := json.Marshal([]any{event.Seq, event.Type, data})
				require.NoError(t, e)
				_, e = output.Write(value)
				require.NoError(t, e)
			}
			intervals := []float64{}
			for i := 1; i < len(timed.acks); i++ {
				intervals = append(intervals, float64(timed.acks[i].Sub(timed.acks[i-1]))/float64(time.Millisecond))
			}
			result := map[string]any{"backend": backend, "layout": layout, "blocks": heights, "transactions": heights * 2, "logs": heights * 16, "readSamples": len(latencies), "readMS": factsPercentiles(latencies), "readCounts": readCounts, "readDigest": hex.EncodeToString(digest.Sum(nil)), "writerSeconds": elapsed.Seconds(), "writerBlocksPerSecond": heights / elapsed.Seconds(), "writerReadMS": factsPercentiles(timed.reads), "ackIntervalMS": factsPercentiles(intervals), "writerReadCounts": factsReadCounts{countsAfter.Views - countsBefore.Views, countsAfter.Gets - countsBefore.Gets, countsAfter.Batches - countsBefore.Batches}, "freshRequests": heights, "physicalRequests": len(attempts), "physicalMinIntervalMS": float64(minInterval) / float64(time.Millisecond), "outputDigest": hex.EncodeToString(output.Sum(nil)), "outboxEvents": len(outbox), "pipeline": stats}
			result["maxConns"] = maxConns
			value, e := json.Marshal(result)
			require.NoError(t, e)
			fmt.Printf("FACTS_WRITER_MEASURE %s\n", value)
		})
	}
}
