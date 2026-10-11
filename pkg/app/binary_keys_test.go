package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet"
	"github.com/0xmhha/indexer-go/pkg/source/era"
)

// TestBinaryKeysOnRealData repeats the K2 measurement of the refactoring
// plan (section 11) on a real chain: it indexes the blocks of era1 archives
// with the default features of their chain, then stores every key twice, as
// the string the key functions build and as a binary key holding the same
// fields, with the same values, and compares size and read time.
//
// Runs only with INDEXER_K2_ERA_DIR (StableNet era1 files from genesis, as
// `gstable export-history` writes them). INDEXER_K2_DB keeps the indexed
// database there and reuses it. Results: refactoring-plan.md section 11.
func TestBinaryKeysOnRealData(t *testing.T) {
	eraDir := os.Getenv("INDEXER_K2_ERA_DIR")
	if eraDir == "" {
		t.Skip("set INDEXER_K2_ERA_DIR")
	}
	pebbleOnly(t)
	dbDir := os.Getenv("INDEXER_K2_DB")
	if dbDir == "" {
		dbDir = filepath.Join(t.TempDir(), "db")
	}
	if _, err := os.Stat(dbDir); err != nil {
		indexEraOnly(t, eraDir, dbDir)
	}

	src, err := pebble.Open(dbDir, &pebble.Options{ReadOnly: true})
	require.NoError(t, err)
	defer func() { _ = src.Close() }() // read-only or measurement copy
	work := t.TempDir()
	str, err := pebble.Open(filepath.Join(work, "string"), &pebble.Options{Cache: pebble.NewCache(128 << 20)})
	require.NoError(t, err)
	defer func() { _ = str.Close() }() // read-only or measurement copy
	bin, err := pebble.Open(filepath.Join(work, "binary"), &pebble.Options{Cache: pebble.NewCache(128 << 20)})
	require.NoError(t, err)
	defer func() { _ = bin.Close() }() // read-only or measurement copy

	// Copy every key in both forms. Binary keys keep the order of their
	// string keys within a group (fixed-width numbers, same segments), so
	// both stores hold the same records in the same groups.
	type group struct{ keys, strBytes, binBytes, valBytes int }
	groups := map[string]*group{}
	var sample [][]byte
	var keys, strBytes, binBytes, valBytes int
	rng := rand.New(rand.NewSource(1))
	it, err := src.NewIter(nil)
	require.NoError(t, err)
	sb, bb := str.NewBatch(), bin.NewBatch()
	flush := func() {
		require.NoError(t, sb.Commit(pebble.NoSync))
		require.NoError(t, bb.Commit(pebble.NoSync))
		sb, bb = str.NewBatch(), bin.NewBatch()
	}
	for it.First(); it.Valid(); it.Next() {
		k, v := it.Key(), it.Value()
		b := binaryKey(k)
		require.NoError(t, sb.Set(k, v, nil))
		require.NoError(t, bb.Set(b, v, nil))
		g := groups[keyGroup(k)]
		if g == nil {
			g = &group{}
			groups[keyGroup(k)] = g
		}
		g.keys++
		g.strBytes += len(k)
		g.binBytes += len(b)
		g.valBytes += len(v)
		keys++
		strBytes += len(k)
		binBytes += len(b)
		valBytes += len(v)
		if rng.Intn(100) == 0 {
			sample = append(sample, slices.Clone(k))
		}
		if sb.Len() > 64<<20 {
			flush()
		}
	}
	require.NoError(t, it.Close())
	flush()
	for _, db := range []*pebble.DB{str, bin} {
		require.NoError(t, db.Flush())
		require.NoError(t, db.Compact([]byte{0}, []byte{0xff, 0xff, 0xff}, true))
	}

	t.Logf("keys %d, average key %.1f B as string, %.1f B as binary (%.0f%% less), average value %.1f B",
		keys, float64(strBytes)/float64(keys), float64(binBytes)/float64(keys),
		100*(1-float64(binBytes)/float64(strBytes)), float64(valBytes)/float64(keys))
	t.Logf("keys are %.0f%% of the string store's raw bytes", 100*float64(strBytes)/float64(strBytes+valBytes))
	names := make([]string, 0, len(groups))
	for n := range groups {
		names = append(names, n)
	}
	slices.SortFunc(names, func(a, b string) int {
		return groups[b].strBytes + groups[b].valBytes - groups[a].strBytes - groups[a].valBytes
	})
	for _, n := range names[:min(15, len(names))] {
		g := groups[n]
		t.Logf("  %-28s %9d keys, key %5.1f -> %4.1f B, value %7.1f B", n, g.keys,
			float64(g.strBytes)/float64(g.keys), float64(g.binBytes)/float64(g.keys), float64(g.valBytes)/float64(g.keys))
	}
	sizeStr, sizeBin := str.Metrics().DiskSpaceUsage(), bin.Metrics().DiskSpaceUsage()
	t.Logf("disk after compaction: string %.1f MB, binary %.1f MB (%.1f%% less)",
		float64(sizeStr)/1e6, float64(sizeBin)/1e6, 100*(1-float64(sizeBin)/float64(sizeStr)))

	rng.Shuffle(len(sample), func(i, j int) { sample[i], sample[j] = sample[j], sample[i] })
	sample = sample[:min(len(sample), 50_000)]
	binSample := make([][]byte, len(sample))
	for i, k := range sample {
		binSample[i] = binaryKey(k)
	}
	get := func(db *pebble.DB, ks [][]byte) time.Duration {
		start := time.Now()
		for _, k := range ks {
			_, closer, err := db.Get(k)
			require.NoError(t, err)
			require.NoError(t, closer.Close())
		}
		return time.Since(start) / time.Duration(len(ks))
	}
	scan := func(db *pebble.DB, ks [][]byte) time.Duration {
		start := time.Now()
		for _, k := range ks {
			it, err := db.NewIter(nil)
			require.NoError(t, err)
			n := 0
			for it.SeekGE(k); it.Valid() && n < 20; it.Next() {
				_ = it.Value()
				n++
			}
			require.NoError(t, it.Close())
		}
		return time.Since(start) / time.Duration(len(ks))
	}
	for round := range 3 { // the first round warms the caches
		gs, gb := get(str, sample), get(bin, binSample)
		ss, sbn := scan(str, sample), scan(bin, binSample)
		t.Logf("round %d over %d keys: get string %v binary %v, 20-key scan string %v binary %v",
			round+1, len(sample), gs, gb, ss, sbn)
	}
}

// indexEraOnly indexes every block of the era1 files in eraDir into dbDir.
// The node is a stub answering from the archive, so no block comes from
// elsewhere; calls for state (balances, code) answer empty.
func indexEraOnly(t *testing.T, eraDir, dbDir string) {
	t.Helper()
	es, err := era.OpenDir(eraDir, stablenet.New())
	require.NoError(t, err)
	defer func() { _ = es.Close() }() // read-only or measurement copy
	_, last := es.Range()
	node := eraNode(t, es)

	cfg := config.NewConfig()
	cfg.RPC.Endpoint = node.URL
	cfg.RPC.Timeout = 5 * time.Second
	cfg.Source.EraDir = eraDir
	cfg.Database.Path = dbDir
	cfg.API.Enabled = false
	app, err := NewApp(cfg, zap.NewNop(), false, "stableone")
	require.NoError(t, err)
	defer app.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cancel()
	start := time.Now()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, last))
	t.Logf("indexed blocks 0..%d from era1 in %v", last, time.Since(start).Round(time.Second))
}

// eraNode serves the JSON-RPC calls the indexer makes besides reading
// blocks, answering heads and hashes from the archive.
func eraNode(t *testing.T, es *era.Source) *httptest.Server {
	_, last := es.Range()
	var mu sync.Mutex
	unknown := map[string]int{}
	type call struct {
		ID     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	answer := func(c call) any {
		switch c.Method {
		case "eth_chainId":
			return "0x205b"
		case "net_version":
			return "8283"
		case "web3_clientVersion":
			return "Gstable/v1.1.0-stable"
		case "eth_blockNumber":
			return hexutil.EncodeUint64(last)
		case "eth_getBlockByNumber":
			var tag string
			_ = json.Unmarshal(c.Params[0], &tag)
			n := last
			if v, err := hexutil.DecodeUint64(tag); err == nil {
				n = v
			}
			if n > last {
				return nil
			}
			b, _, err := es.BlockWithReceipts(context.Background(), n)
			if err != nil {
				return nil
			}
			return map[string]any{"number": hexutil.EncodeUint64(n), "hash": b.Hash, "parentHash": b.ParentHash,
				"timestamp": hexutil.EncodeUint64(b.Time), "transactions": []any{}}
		case "eth_getBalance", "eth_getTransactionCount":
			return "0x0"
		case "eth_getCode", "eth_call":
			return "0x"
		}
		mu.Lock()
		unknown[c.Method]++
		mu.Unlock()
		return nil
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reply := func(c call) map[string]any {
			return map[string]any{"jsonrpc": "2.0", "id": c.ID, "result": answer(c)}
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
			var calls []call
			_ = json.Unmarshal(raw, &calls)
			out := make([]any, len(calls))
			for i, c := range calls {
				out[i] = reply(c)
			}
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		var c call
		_ = json.Unmarshal(raw, &c)
		_ = json.NewEncoder(w).Encode(reply(c))
	}))
	t.Cleanup(func() {
		srv.Close()
		if len(unknown) > 0 {
			t.Logf("node calls answered null: %v", unknown)
		}
	})
	return srv
}

// keyGroup is the leading segments of a key that name its record kind.
func keyGroup(k []byte) string {
	segs := strings.Split(string(k), "/")
	var out []string
	for _, s := range segs {
		if s == "" {
			continue
		}
		if _, ok := segmentBytes(s); ok {
			break
		}
		out = append(out, s)
		if len(out) == 3 {
			break
		}
	}
	return "/" + strings.Join(out, "/")
}

// binaryKey encodes the fields of a string key in binary: hex values as
// their bytes, fixed-width decimal numbers as big-endian integers of the
// width their digits need, and each name segment as a two-byte id. A key
// that is not slash separated text is kept as it is.
func binaryKey(k []byte) []byte {
	if len(k) == 0 || k[0] != '/' {
		return k
	}
	out := make([]byte, 0, len(k))
	for _, s := range strings.Split(string(k[1:]), "/") {
		if b, ok := segmentBytes(s); ok {
			out = append(out, b...)
			continue
		}
		id := segmentID(s)
		out = append(out, byte(id>>8), byte(id))
	}
	return out
}

func segmentBytes(s string) ([]byte, bool) {
	if strings.HasPrefix(s, "0x") && len(s) > 2 && len(s)%2 == 0 {
		if b, err := hex.DecodeString(s[2:]); err == nil {
			return b, true
		}
	}
	if len(s) >= 3 && strings.Trim(s, "0123456789") == "" {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return nil, false
		}
		width := 8
		switch {
		case len(s) <= 3:
			width = 2
		case len(s) <= 9:
			width = 4
		}
		b := make([]byte, width)
		for i := width - 1; i >= 0; i-- {
			b[i] = byte(v)
			v >>= 8
		}
		return b, true
	}
	return nil, false
}

var segmentIDs = struct {
	sync.Mutex
	m map[string]int
}{m: map[string]int{}}

func segmentID(s string) int {
	segmentIDs.Lock()
	defer segmentIDs.Unlock()
	id, ok := segmentIDs.m[s]
	if !ok {
		id = len(segmentIDs.m) + 1
		if id > 0xffff {
			panic(fmt.Sprintf("more than 65535 key segment names (%q)", s))
		}
		segmentIDs.m[s] = id
	}
	return id
}
