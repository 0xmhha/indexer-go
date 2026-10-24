package app

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	checkpointformat "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/internal/executionops"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/0xmhha/indexer-go/x/stablenet"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

func TestRetentionOfflineToolsConsumeFrozenProducerPlan(t *testing.T) {
	dsn := postgresDSN(t)
	schema := testSchema(t.TempDir())
	t.Cleanup(func() { dropTestSchemas(dsn, schema) })
	data, err := os.ReadFile("../../testdata/execution/retention/stablenet-storage.json")
	require.NoError(t, err)
	var f struct {
		Version        int
		Scope, Profile string
		Request        retention.Request
		Genesis        []byte
		Payloads       [][]byte
	}
	require.NoError(t, json.Unmarshal(data, &f))
	require.NoError(t, f.Request.Validate())
	first, err := checkpointformat.NewDecoder(bytes.NewReader(f.Genesis)).Next()
	require.NoError(t, err)
	s, err := postgres.Open(t.Context(), postgres.Options{DSN: dsn, Schema: schema, MaxConns: 4})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	profile := stablenet.New()
	_, err = s.BootstrapExecution(t.Context(), bytes.NewReader(f.Genesis), *first.Manifest, profile, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	// Match the producer's legitimate generation2 base without inventing an ACK:
	// fence and resume a zero-depth public rewind, then publish every part.
	_, err = s.BeginExecutionRewind(t.Context(), first.Manifest.Ref, first.Manifest.Root)
	require.NoError(t, err)
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	headers := map[uint64]*types.Header{}
	var genesis types.Header
	require.NoError(t, rlp.DecodeBytes(first.Manifest.Header, &genesis))
	headers[0] = &genesis
	for _, payload := range f.Payloads {
		prepared, err := execution.Prepare(profile, f.Request.Chain, payload)
		require.NoError(t, err)
		result, err := execwire.UnmarshalResult(payload)
		require.NoError(t, err)
		backend := &execution.StoreBackend{Store: s}
		for _, writer := range port.ExecutionWriters() {
			require.NoError(t, backend.Part(t.Context(), writer, prepared))
		}
		_, err = s.CommitExecution(t.Context(), port.ExecutionPublication{Ref: result.Ref, Parent: result.Parent, Digest: prepared.Digest(), Root: result.StateRoot}, func(context.Context) error { return nil }, func(context.Context) error { return nil })
		require.NoError(t, err)
		var block types.Block
		require.NoError(t, rlp.DecodeBytes(result.BlockRLP, &block))
		headers[result.Ref.Height] = block.Header()
	}
	var mu sync.Mutex
	reads := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			ID     json.RawMessage
			Method string
			Params []json.RawMessage
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&q))
		mu.Lock()
		reads[q.Method]++
		mu.Unlock()
		var result any
		switch q.Method {
		case "eth_chainId":
			id, ok := new(big.Int).SetString(f.Request.Chain.ID, 10)
			require.True(t, ok)
			result = "0x" + id.Text(16)
		case "eth_getBlockByNumber":
			var n string
			require.NoError(t, json.Unmarshal(q.Params[0], &n))
			height, e := strconv.ParseUint(strings.TrimPrefix(n, "0x"), 16, 64)
			require.NoError(t, e)
			result = headers[height]
		default:
			t.Errorf("unexpected retention RPC %s", q.Method)
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result}))
	}))
	defer srv.Close()
	t.Setenv("INDEXER_DB_POSTGRES_DSN", dsn)
	t.Setenv("INDEXER_CHECKPOINT_RPC", srv.URL)
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	requestPath := filepath.Join(dir, "request.json")
	require.NoError(t, retention.WriteArtifact(requestPath, f.Request))
	permitPath := filepath.Join(dir, "permit.json")
	var out bytes.Buffer
	require.NoError(t, executionops.RunRetention(t.Context(), executionops.RetentionOptions{Action: "authorize", Schema: schema, Request: requestPath, Output: permitPath}, profile, &out), "valid frozen producer plan must acquire an actual offline DB permit")
	var p retention.Permit
	require.NoError(t, retention.ReadArtifact(permitPath, &p))
	require.NoError(t, p.Validate())
	require.Equal(t, f.Request, p.Request)
	control, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.True(t, control.Maintenance)
	require.ErrorIs(t, s.ReadSnapshot(t.Context(), func(context.Context) error { return nil }), port.ErrExecutionMaintenance)
	require.NoError(t, executionops.RunRetention(t.Context(), executionops.RetentionOptions{Action: "inspect", Schema: schema}, profile, &out))
	require.Contains(t, out.String(), `"Pending":true`)

	helper := filepath.Join(dir, "producer")
	quoted := "'" + strings.ReplaceAll(os.Args[0], "'", "'\\''") + "'"
	script := "#!/bin/sh\nexec " + quoted + " -test.run='^TestRetentionProducerHelper$' -- -retention-helper \"$@\"\n"
	require.NoError(t, os.WriteFile(helper, []byte(script), 0700))
	producerConfig := filepath.Join(dir, "producer.json")
	require.NoError(t, os.WriteFile(producerConfig, []byte("{}"), 0600))
	releasePath := filepath.Join(dir, "release.json")
	require.NoError(t, executionops.RunRetention(t.Context(), executionops.RetentionOptions{Action: "collect", Schema: schema, Permit: permitPath, Producer: helper, ProducerConfig: producerConfig, Output: releasePath}, profile, &out))
	_, pending, receipt, err := s.ExecutionRetentionStatus(t.Context())
	require.NoError(t, err)
	require.False(t, pending)
	require.NotNil(t, receipt)
	var release retention.Release
	require.NoError(t, retention.ReadArtifact(releasePath, &release))
	require.NoError(t, release.Match(p, *receipt))
	control, err = s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.False(t, control.Maintenance)
	require.NoError(t, s.ReadSnapshot(t.Context(), func(ctx context.Context) error {
		block, e := s.GetBlock(ctx, 1)
		require.NoError(t, e)
		require.EqualValues(t, 1, block.Number)
		return nil
	}))
	// Replaying a completed artifact must not launch a child at all. This is
	// especially important when an old permit is found after DB restoration.
	require.NoError(t, executionops.RunRetention(t.Context(), executionops.RetentionOptions{Action: "collect", Schema: schema, Permit: permitPath, Producer: "/missing/producer-must-not-run", ProducerConfig: producerConfig, Output: filepath.Join(dir, "release-retry.json")}, profile, &out))
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 6, reads["eth_chainId"], "constructor ping and explicit identity each use the shared budget")
	require.Equal(t, 6, reads["eth_getBlockByNumber"], "each authorization/completion requires identity AND a separate final uncached fresh")
}

// Owned subprocess stub exercises the actual supervised child protocol. The
// separate executor test consumes/regenerates the same frozen producer vector
// and performs native node/spool collection; this helper starts no services.
func TestRetentionProducerHelper(t *testing.T) {
	helper := false
	permitPath := ""
	for i, arg := range os.Args {
		if arg == "-retention-helper" {
			helper = true
		}
		if arg == "-permit" && i+1 < len(os.Args) {
			permitPath = os.Args[i+1]
		}
	}
	if !helper {
		t.Skip("owned subprocess only")
	}
	var p retention.Permit
	require.NoError(t, retention.ReadArtifact(permitPath, &p))
	ctx, hello, stop, err := retention.Supervise(t.Context(), os.NewFile(3, "retention-supervisor"), p)
	require.NoError(t, err)
	require.NoError(t, ctx.Err())
	pd, err := retention.Digest(p)
	require.NoError(t, err)
	reply := retention.SupervisedReply{Version: 1, Challenge: hello.Challenge, Receipt: retention.Receipt{Version: 1, PermitDigest: pd, Node: true, Spool: true, Files: 3}}
	require.NoError(t, json.NewEncoder(os.Stdout).Encode(reply))
	stop()
	os.Exit(0)
}
