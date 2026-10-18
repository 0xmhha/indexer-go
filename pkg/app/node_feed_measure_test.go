package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	"github.com/0xmhha/indexer-go/pkg/nodefeed"
	"github.com/0xmhha/indexer-go/pkg/source/replay"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rlp"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type feedMeasurementInput struct {
	ReplayArchive string                                    `json:"replay_archive"`
	ChainID       uint64                                    `json:"chain_id"`
	From          uint64                                    `json:"from"`
	To            uint64                                    `json:"to"`
	Blocks        map[uint64]json.RawMessage                `json:"blocks"`
	States        map[common.Hash]map[common.Address]string `json:"strict_state"`
}

type feedMeasurementResult struct {
	Mode       string                `json:"mode"`
	Round      int                   `json:"round"`
	Seconds    float64               `json:"seconds"`
	Blocks     uint64                `json:"blocks"`
	BlockRate  float64               `json:"blocks_per_second"`
	HTTP       int                   `json:"http_attempts"`
	Logical    map[string]int        `json:"logical_methods"`
	Unrecorded map[string]int        `json:"unused_state_read_errors"`
	ArrivalsMS []float64             `json:"http_arrivals_ms"`
	Pipeline   fetch.PipelineMetrics `json:"pipeline"`
}

// Opt-in, fixed-input transport comparison. The Rust fixture serves recorded
// state, not a running Arc node. The HTTP source is an immutable replay with
// the production 5 HTTP/s limiter; HashAt still sends fresh HTTP requests.
// Setup, fixture conversion and correctness comparisons are outside the timer.
func TestNodeFeedRecordedMeasurement(t *testing.T) {
	out := os.Getenv("INDEXER_NODE_FEED_MEASURE_DIR")
	binary := os.Getenv("INDEXER_NODE_FEED_FIXTURE_BINARY")
	if out == "" || binary == "" {
		t.Skip("set INDEXER_NODE_FEED_MEASURE_DIR and INDEXER_NODE_FEED_FIXTURE_BINARY")
	}
	pebbleOnly(t)
	raw, err := os.ReadFile(filepath.Join(out, "measurement-input.json"))
	require.NoError(t, err)
	var input feedMeasurementInput
	require.NoError(t, json.Unmarshal(raw, &input))
	require.GreaterOrEqual(t, input.To, input.From)
	require.LessOrEqual(t, input.To-input.From, uint64(99))
	archive, err := replay.Open(input.ReplayArchive)
	require.NoError(t, err)
	oracle := replay.NewServer(archive)
	conversion := httptest.NewServer(oracle)
	defer conversion.Close()
	client, err := gethrpc.DialContext(t.Context(), conversion.URL)
	require.NoError(t, err)
	defer client.Close()
	profile, err := chains.Detect(chains.NodeInfo{ChainID: input.ChainID})
	require.NoError(t, err)
	require.Equal(t, "arc", profile.ID())
	var bundles []nodefeed.Bundle
	var missing []map[string]string
	heights := make([]uint64, 0, len(input.Blocks))
	for height := range input.Blocks {
		heights = append(heights, height)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	for _, height := range heights {
		var header struct {
			Hash       common.Hash `json:"hash"`
			ParentHash common.Hash `json:"parentHash"`
		}
		require.NoError(t, json.Unmarshal(input.Blocks[height], &header))
		bundle := nodefeed.Bundle{Height: height, Hash: header.Hash, ParentHash: header.ParentHash, Header: []byte{}, Body: []byte{}, Receipts: []byte{}}
		if height >= input.From && height <= input.To {
			block, err := profile.DecodeBlock(input.Blocks[height])
			require.NoError(t, err)
			gethBlock, err := gethconv.BlockToGeth(block)
			require.NoError(t, err)
			require.Equal(t, block.Hash, gethBlock.Hash(), "fixture must preserve every header commitment")
			bundle.Header, err = rlp.EncodeToBytes(gethBlock.Header())
			require.NoError(t, err)
			bundle.Body, err = rlp.EncodeToBytes(gethBlock.Body())
			require.NoError(t, err)
			var receipts json.RawMessage
			require.NoError(t, client.CallContext(t.Context(), &receipts, "eth_getBlockReceipts", fmt.Sprintf("0x%x", height)))
			decoded, err := profile.DecodeReceipts(receipts)
			require.NoError(t, err)
			deltas, err := chains.AccountingOf(profile).NativeDeltas(t.Context(), block, decoded, nil)
			require.NoError(t, err)
			merged := make(map[common.Address]*big.Int)
			for _, delta := range deltas {
				if merged[delta.Address] == nil {
					merged[delta.Address] = new(big.Int)
				}
				merged[delta.Address].Add(merged[delta.Address], delta.Delta)
			}
			for address, delta := range merged {
				if _, exists := input.States[block.ParentHash][address]; !exists && delta.Sign() != 0 {
					missing = append(missing, map[string]string{"address": address.Hex(), "hash": block.ParentHash.Hex()})
				}
			}
			bundle.Receipts, err = rlp.EncodeToBytes(gethconv.ReceiptsToGeth(decoded))
			require.NoError(t, err)
		}
		bundles = append(bundles, bundle)
	}
	missingRaw, err := json.MarshalIndent(missing, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(out, "missing-native-pairs.json"), missingRaw, 0o600))
	require.Empty(t, missing, "freeze each potential prepared native state before timing; do not synthesize missing values")
	fixture, err := json.Marshal(map[string]any{"chain_id": input.ChainID, "blocks": bundles, "states": input.States, "strict_state": true})
	require.NoError(t, err)
	fixturePath := filepath.Join(out, "fixture.json")
	require.NoError(t, os.WriteFile(fixturePath, fixture, 0o600))
	require.Empty(t, oracle.Unrecorded())
	var results []feedMeasurementResult
	var want []testchain.Entry
	// Alternate order; keep all three planned pairs, including slow runs.
	for round := 0; round < 3; round++ {
		modes := []string{"rpc", "rust_ipc"}
		if round%2 == 1 {
			modes[0], modes[1] = modes[1], modes[0]
		}
		for _, mode := range modes {
			t.Run(fmt.Sprintf("round%d/%s", round+1, mode), func(t *testing.T) {
				var mu sync.Mutex
				var arrivals []time.Time
				var requests []json.RawMessage
				methods := make(map[string]int)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					r.Body = io.NopCloser(bytes.NewReader(body))
					var batch []struct {
						Method string `json:"method"`
					}
					if len(body) > 0 && body[0] == '[' {
						err = json.Unmarshal(body, &batch)
					} else {
						batch = append(batch, struct {
							Method string `json:"method"`
						}{})
						err = json.Unmarshal(body, &batch[0])
					}
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					mu.Lock()
					arrivals = append(arrivals, time.Now())
					requests = append(requests, append(json.RawMessage(nil), body...))
					for _, q := range batch {
						methods[q.Method]++
					}
					mu.Unlock()
					oracle.ServeHTTP(w, r)
				}))
				defer server.Close()
				socket := ""
				if mode == "rust_ipc" {
					socket = startFeedMeasurementProducer(t, binary, fixturePath)
				}
				dir := filepath.Join(t.TempDir(), "db")
				cfg := config.NewConfig()
				cfg.API.Enabled = false
				cfg.RPC.Endpoint, cfg.RPC.RateLimit, cfg.RPC.CoalesceReads = server.URL, 5, true
				cfg.Database.Path = dir
				cfg.Indexer.StartHeight, cfg.Indexer.Workers = input.From, 2
				cfg.Indexer.StatePrefetch, cfg.Indexer.AcquisitionQueue = true, true
				cfg.Indexer.StateWorkers, cfg.Indexer.StateBatchSize = 4, 16
				cfg.Indexer.QueueBlocks, cfg.Indexer.QueueMaxBytes = 256, 256<<20
				cfg.Indexer.PipelineMetrics = true
				cfg.Source.NodeFeedSocket = socket
				app, err := NewApp(cfg, zap.NewNop(), false, "")
				require.NoError(t, err)
				defer app.Shutdown()
				mu.Lock()
				arrivals = nil
				requests = nil
				clear(methods)
				mu.Unlock()
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
				defer cancel()
				started := time.Now()
				beforeMissing := oracle.Unrecorded()
				require.NoError(t, app.fetcher.FetchRange(ctx, input.From, input.To))
				elapsed := time.Since(started)
				stats := app.fetcher.GetPipelineMetrics()
				result := feedMeasurementResult{Mode: mode, Round: round + 1, Seconds: elapsed.Seconds(), Blocks: input.To - input.From + 1, BlockRate: float64(input.To-input.From+1) / elapsed.Seconds(), Pipeline: stats}
				mu.Lock()
				result.HTTP = len(arrivals)
				result.Logical = methods
				for _, at := range arrivals {
					result.ArrivalsMS = append(result.ArrivalsMS, float64(at.Sub(started))/float64(time.Millisecond))
				}
				mu.Unlock()
				requestDump, dumpErr := json.Marshal(requests)
				require.NoError(t, dumpErr)
				require.NoError(t, os.WriteFile(filepath.Join(out, fmt.Sprintf("requests-round%d-%s.json", round+1, mode)), requestDump, 0o600))
				result.Unrecorded = make(map[string]int)
				for method, count := range oracle.Unrecorded() {
					if count -= beforeMissing[method]; count != 0 {
						result.Unrecorded[method] = count
					}
				}
				require.Empty(t, result.Unrecorded, "complete exact-hash input required for the comparison")
				require.Equal(t, input.To, *stats.CommittedHeight)
				require.Zero(t, stats.InFlight)
				require.Zero(t, stats.QueueBlocks)
				if mode == "rust_ipc" {
					require.Zero(t, result.Logical["eth_getBalance"])
				}
				var finalHeader struct {
					Hash common.Hash `json:"hash"`
				}
				require.NoError(t, json.Unmarshal(input.Blocks[input.To], &finalHeader))
				for address, encoded := range input.States[finalHeader.Hash] {
					balance, tracked := indexedBalanceAt(t, app.storage.(port.HistoricalReader), address, input.To)
					if tracked {
						expected, err := hexutil.DecodeBig(encoded)
						require.NoError(t, err)
						require.Zero(t, balance.Cmp(expected), "%s", address)
					}
				}
				app.Shutdown()
				got := dumpDir(t, dir)
				if want == nil {
					want = got
				} else {
					diff := testchain.DiffKeyspace(want, got, 0)
					require.Empty(t, diff, testchain.SummarizeDiff(diff))
				}
				results = append(results, result)
				raw, err := json.MarshalIndent(results, "", "  ")
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(out, "results.json"), raw, 0o600))
				t.Logf("%s round%d: %.3fs %.3f blk/s HTTP=%d acquired interval p50/p95=%.1f/%.1fms writer p95=%.1fms", mode, round+1, result.Seconds, result.BlockRate, result.HTTP, stats.AcquireInterval.P50MS, stats.AcquireInterval.P95MS, stats.Write.P95MS)
			})
			if t.Failed() {
				return
			}
		}
	}
}

func startFeedMeasurementProducer(t *testing.T, binary, fixture string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "infm-")
	require.NoError(t, err)
	socket := filepath.Join(dir, "feed.sock")
	cmd := exec.Command(binary, socket, fixture)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case err := <-done:
			require.NoError(t, err, output.String())
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Error("fixture failed to stop")
		}
		_ = os.RemoveAll(dir)
	})
	require.Eventually(t, func() bool { _, err := os.Stat(socket); return err == nil }, 5*time.Second, time.Millisecond)
	return socket
}
