package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/indexingstatus"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type sourceSearchLatency struct{ P50, P95, P99 float64 }

type sourceSearchReadCounts struct {
	Gets, NodeGets, Batches, BatchKeys, BatchBytes int
	Views, NodeBatches, MetadataBatches            int
}
type sourceSearchCountStore struct {
	port.IndexingReader
	port.CaptureDatabase
	counts *sourceSearchReadCounts
	single bool
}
type sourceSearchCountReader struct {
	port.CaptureReader
	counts *sourceSearchReadCounts
}

func (r sourceSearchCountReader) Get(key []byte) ([]byte, error) {
	r.counts.Gets++
	if bytes.HasPrefix(key, []byte("search/v1/nodes/")) {
		r.counts.NodeGets++
	}
	return r.CaptureReader.Get(key)
}

type sourceSearchCountBatch struct {
	sourceSearchCountReader
	batch port.CaptureBatchReader
}

func (r sourceSearchCountBatch) GetMany(keys [][]byte) (map[string][]byte, error) {
	r.counts.Batches++
	nodes := len(keys) != 0
	for _, key := range keys {
		nodes = nodes && bytes.HasPrefix(key, []byte("search/v1/nodes/"))
	}
	if nodes {
		r.counts.NodeBatches++
	} else {
		r.counts.MetadataBatches++
	}
	r.counts.BatchKeys += len(keys)
	values, e := r.batch.GetMany(keys)
	for key, value := range values {
		r.counts.BatchBytes += len(key) + len(value)
	}
	return values, e
}
func (s sourceSearchCountStore) CaptureView(ctx context.Context, chain model.FeedChain, fn func(port.CaptureReader) error) error {
	s.counts.Views++
	return s.CaptureDatabase.CaptureView(ctx, chain, func(r port.CaptureReader) error {
		counted := sourceSearchCountReader{r, s.counts}
		if batch, ok := r.(port.CaptureBatchReader); ok && !s.single {
			return fn(sourceSearchCountBatch{counted, batch})
		}
		return fn(counted)
	})
}

func sourceSearchPercentiles(samples []float64) sourceSearchLatency {
	sort.Float64s(samples)
	at := func(p float64) float64 { return samples[int(math.Ceil(p*float64(len(samples))))-1] }
	return sourceSearchLatency{at(.5), at(.95), at(.99)}
}

// TestMeasureSourceSearch is an opt-in, fixed-input comparison. SQL is a
// session-local candidate-index prototype, without the durable worker/reorg
// contract. Its latency is compared only to trie candidate discovery.
func TestMeasureSourceSearch(t *testing.T) {
	if os.Getenv("INDEXER_MEASURE_SOURCE_SEARCH") != "1" {
		t.Skip("set INDEXER_MEASURE_SOURCE_SEARCH=1 for the fixed-input comparison")
	}
	const heights = 32
	const repetitions = 20
	from, to := testchain.NewAccount(21), testchain.NewAccount(22)
	c := testchain.NewChain(5042002, map[common.Address]*big.Int{from.Address: new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)})
	for n := 1; n <= heights; n++ {
		specs := []testchain.TxSpec{}
		for j := 0; j < 2; j++ {
			logs := []*types.Log{}
			for i := 0; i < 8; i++ {
				logs = append(logs, &types.Log{Address: common.Address{byte(n % 16)}, Topics: []common.Hash{{7}, {byte(i % 4)}}, Data: []byte("fixed-source-query-fixture")})
			}
			specs = append(specs, testchain.TxSpec{From: from, Tx: &types.DynamicFeeTx{To: &to.Address, Gas: 50000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000)}, GasUsed: 21000, Logs: logs})
		}
		c.AddBlock(specs...)
	}
	cfg, _, rpc := feedAppConfig(t, c)
	cfg.Source.Feed.Projections = []string{model.SourceSearchWorker}
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	require.Eventually(t, func() bool {
		s, e := indexingstatus.Read(t.Context(), a.storage, "", 100)
		return e == nil && len(s.Workers) == 1 && s.Workers[0].Complete != nil && s.Workers[0].Complete.Height == heights
	}, 150*time.Second, 20*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	beforeRPC := rpc.Calls()
	start, end := uint64(1700000016), uint64(1700000064)
	q := capture.SearchFilter{Kind: capture.RowLog, From: 1, To: heights, Addresses: []common.Address{{1}, {2}}, TopicAny: [][]common.Hash{nil, {{1}, {3}}, nil, nil}, FromTime: &start, ToTime: &end}
	queryStore := any(a.storage)
	read := func(filter capture.SearchFilter, baseline bool) ([]string, int, int) {
		rows := []string{}
		cursor := ""
		decoded, scanned := 0, 0
		for page := 0; page < 10; page++ {
			p, e := indexingstatus.ReadSearch(t.Context(), queryStore, filter, cursor, 256)
			require.NoError(t, e)
			decoded += p.DecodedBlocks
			scanned += p.ScannedCandidates
			for _, row := range p.Rows {
				if baseline {
					var log model.Log
					require.NoError(t, json.Unmarshal(row.Row.Value, &log))
					if log.Address != (common.Address{1}) && log.Address != (common.Address{2}) || len(log.Topics) < 2 || log.Topics[1] != (common.Hash{1}) && log.Topics[1] != (common.Hash{3}) {
						continue
					}
				}
				rows = append(rows, fmt.Sprintf("%d/%d/%x", row.Identity.Block.Number, row.Row.Index, row.Row.Value))
			}
			if p.Done {
				return rows, decoded, scanned
			}
			cursor = p.Next
		}
		t.Fatal("measurement did not finish within page budget")
		return nil, 0, 0
	}
	baseline := capture.SearchFilter{Kind: q.Kind, From: q.From, To: q.To, FromTime: q.FromTime, ToTime: q.ToTime}
	expected, indexDecoded, indexScanned := read(q, false)
	full, scanDecoded, scanScanned := read(baseline, true)
	require.Equal(t, full, expected)
	require.Len(t, expected, 16)
	status, e := indexingstatus.Read(t.Context(), a.storage, "", 100)
	require.NoError(t, e)
	reader, e := capture.NewReader(a.storage.(port.CaptureDatabase), status.Source.Chain, nil)
	require.NoError(t, e)
	d := *status.Source.SourceSearch
	var candidateRows int
	var candidatePositions []capture.SearchPosition
	candidates := func() {
		p, e := reader.ScanSearch(t.Context(), d, status.Source.Generation, q, nil, 256)
		require.NoError(t, e)
		require.True(t, p.Done)
		candidateRows = len(p.Candidates)
		candidatePositions = nil
		for _, hit := range p.Candidates {
			candidatePositions = append(candidatePositions, hit.Position)
		}
	}
	candidates()
	require.Equal(t, 64, candidateRows)
	var conn *pgx.Conn
	var sqlBytes int64
	var trieNodes, trieBytes, sourceBytes int64
	if cfg.Database.Driver == "postgres" {
		cc, e := pgx.ParseConfig(cfg.Database.Postgres.DSN)
		require.NoError(t, e)
		cc.DefaultQueryExecMode = pgx.QueryExecModeExec
		conn, e = pgx.ConnectConfig(t.Context(), cc)
		require.NoError(t, e)
		defer func() { require.NoError(t, conn.Close(context.Background())) }()
		_, e = conn.Exec(t.Context(), "CREATE TEMP TABLE source_search_measure (height bigint, ordinal integer, hash bytea, address bytea, topic0 bytea, topic1 bytea, time bigint, PRIMARY KEY(height, ordinal)) ON COMMIT PRESERVE ROWS")
		require.NoError(t, e)
		batch := &pgx.Batch{}
		for n := 1; n <= heights; n++ {
			block := c.Block(uint64(n))
			ordinal := 0
			for _, receipt := range block.Receipts {
				for _, log := range receipt.Logs {
					batch.Queue("INSERT INTO source_search_measure VALUES ($1,$2,$3,$4,$5,$6,$7)", n, ordinal, block.Block.Hash().Bytes(), log.Address.Bytes(), log.Topics[0].Bytes(), log.Topics[1].Bytes(), int64(block.Block.Time()))
					ordinal++
				}
			}
		}
		require.NoError(t, conn.SendBatch(t.Context(), batch).Close())
		_, e = conn.Exec(t.Context(), "CREATE INDEX source_search_measure_address ON source_search_measure(address,height,ordinal)")
		require.NoError(t, e)
		_, e = conn.Exec(t.Context(), "ANALYZE source_search_measure")
		require.NoError(t, e)
		e = conn.QueryRow(t.Context(), "SELECT pg_total_relation_size('pg_temp.source_search_measure')").Scan(&sqlBytes)
		require.NoError(t, e)
		table := pgx.Identifier{cfg.Database.Postgres.Schema, "capture_records"}.Sanitize()
		e = conn.QueryRow(t.Context(), "SELECT count(*), COALESCE(sum(octet_length(key)+octet_length(value)),0) FROM "+table+" WHERE key >= $1 AND key < $2", []byte("search/v1/nodes/"), []byte("search/v1/nodes0")).Scan(&trieNodes, &trieBytes)
		require.NoError(t, e)
		e = conn.QueryRow(t.Context(), "SELECT COALESCE(sum(octet_length(key)+octet_length(value)),0) FROM "+table).Scan(&sourceBytes)
		require.NoError(t, e)
	} else {
		prefix := "/source/capture/" + status.Source.Chain.ID + "/" + status.Source.Chain.GenesisHash.Hex() + "/"
		require.NoError(t, a.storage.(port.KV).Iterate(t.Context(), []byte(prefix), func(key, value []byte) bool {
			suffix := strings.TrimPrefix(string(key), prefix)
			bytes := int64(len(suffix) + len(value))
			sourceBytes += bytes
			if strings.HasPrefix(suffix, "search/v1/nodes/") {
				trieNodes++
				trieBytes += bytes
			}
			return true
		}))
	}
	sqlCandidates := func() {
		rows, e := conn.Query(t.Context(), "SELECT height,ordinal,hash FROM source_search_measure WHERE address=ANY($1::bytea[]) AND height >= $2 AND height <= $3 ORDER BY height,ordinal LIMIT $4", [][]byte{common.Address{1}.Bytes(), common.Address{2}.Bytes()}, int64(1), int64(heights), 256)
		require.NoError(t, e)
		positions := []capture.SearchPosition{}
		for rows.Next() {
			var n int64
			var i int
			var hash []byte
			require.NoError(t, rows.Scan(&n, &i, &hash))
			require.Equal(t, c.Block(uint64(n)).Block.Hash().Bytes(), hash)
			positions = append(positions, capture.SearchPosition{Height: uint64(n), Index: uint32(i)})
		}
		require.NoError(t, rows.Err())
		require.Equal(t, candidatePositions, positions)
	}
	if conn != nil {
		sqlCandidates()
	}
	samples := map[string][]float64{}
	measure := func(key string, fn func()) {
		begin := time.Now()
		fn()
		samples[key] = append(samples[key], float64(time.Since(begin))/float64(time.Millisecond))
	}
	indexed := func() { got, _, _ := read(q, false); require.Equal(t, expected, got) }
	scanned := func() { got, _, _ := read(baseline, true); require.Equal(t, expected, got) }
	for i := 0; i < repetitions; i++ {
		if i%2 == 0 {
			measure("verifiedIndexMs", indexed)
			measure("verifiedScanMs", scanned)
		} else {
			measure("verifiedScanMs", scanned)
			measure("verifiedIndexMs", indexed)
		}
		measure("trieCandidatesMs", candidates)
		if conn != nil {
			measure("sqlPrototypeCandidatesMs", sqlCandidates)
		}
	}
	latencies := map[string]sourceSearchLatency{}
	for key, values := range samples {
		latencies[key] = sourceSearchPercentiles(values)
	}
	// Count outside latency samples. The Get-only control keeps the same
	// snapshot cache and hides just the optional batch capability.
	readCounts := map[string]*sourceSearchReadCounts{}
	for _, single := range []bool{false, true} {
		counts := &sourceSearchReadCounts{}
		name := "optimized"
		if single {
			name = "getOnly"
		}
		queryStore = sourceSearchCountStore{IndexingReader: a.storage.(port.IndexingReader), CaptureDatabase: a.storage.(port.CaptureDatabase), counts: counts, single: single}
		got, _, _ := read(q, false)
		require.Equal(t, expected, got)
		readCounts[name] = counts
	}
	if cfg.Database.Driver == "postgres" {
		require.Greater(t, readCounts["optimized"].Batches, 0)
		require.Less(t, readCounts["optimized"].NodeGets+readCounts["optimized"].NodeBatches, readCounts["getOnly"].NodeGets)
	}
	require.Equal(t, beforeRPC, rpc.Calls(), "all measured reads are DB-only")
	require.Greater(t, trieNodes, int64(0))
	require.Greater(t, trieBytes, int64(0))
	backend := cfg.Database.Driver
	if backend == "" {
		backend = "pebble"
	}
	result := map[string]any{"backend": backend, "blocks": heights, "transactions": heights * 2, "logs": heights * 16, "repetitionsWarm": repetitions, "resultRows": len(expected), "indexDecoded": indexDecoded, "scanDecoded": scanDecoded, "indexScanned": indexScanned, "scanScanned": scanScanned, "candidateRows": candidateRows, "historicalTrieNodes": trieNodes, "historicalTrieNodeKeyValueBytes": trieBytes, "allCaptureKeyValueBytes": sourceBytes, "sqlPrototypeTableAndIndexesBytes": sqlBytes, "queryRPC": 0, "latencies": latencies, "captureReads": readCounts}
	b, e := json.Marshal(result)
	require.NoError(t, e)
	t.Logf("SOURCE_SEARCH_MEASURE %s", b)
}
