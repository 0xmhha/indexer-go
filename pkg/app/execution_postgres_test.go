package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	checkpointformat "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/internal/executionops"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/internal/client"
	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type executionFrozenState struct {
	Address execwire.Address
	Account execwire.Account
	Code    []byte
}
type executionFrozen struct {
	Version      int
	Profile      string
	Chain        execwire.Chain
	ParentHeader []byte
	Baseline     port.ExecutionBaseline
	Payload      []byte
	Accounts     []executionFrozenState
	Storage      []execwire.StorageChange
}

func loadExecutionFrozen(t *testing.T, name string) executionFrozen {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "execution", name+".json"))
	require.NoError(t, err)
	var f executionFrozen
	require.NoError(t, json.Unmarshal(b, &f))
	require.Equal(t, 1, f.Version)
	return f
}
func preparedExecutionDB(t *testing.T, f executionFrozen) (*postgres.Store, *config.Config) {
	t.Helper()
	cfg := executionAppConfig()
	cfg.Database.Postgres.DSN = postgresDSN(t)
	cfg.Database.Postgres.Schema = testSchema(t.TempDir())
	cfg.API.Enabled = false
	cfg.Source.Execution.AnchorHeight = f.Baseline.Ref.Height
	cfg.Source.Execution.AnchorHash = common.Hash(f.Baseline.Ref.Hash).Hex()
	cfg.Source.Execution.AnchorRoot = common.Hash(f.Baseline.Root).Hex()
	s, err := openPostgres(t.Context(), &cfg.Database)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(); dropTestSchemas(cfg.Database.Postgres.DSN, cfg.Database.Postgres.Schema) })
	p, ok := chains.Lookup(f.Profile)
	require.True(t, ok)
	parent, err := p.(chains.HeaderCodec).DecodeHeaderRLP(f.ParentHeader)
	require.NoError(t, err)
	require.NoError(t, s.SetBlock(t.Context(), parent))
	require.NoError(t, s.InitExecution(t.Context(), port.ExecutionConfig{Chain: f.Chain, Anchor: f.Baseline.Ref, AnchorRoot: f.Baseline.Root, PartitionBlocks: 100}))
	require.NoError(t, s.SeedExecution(t.Context(), f.Baseline))
	return s, cfg
}

type executionRPCMock struct {
	mu        sync.Mutex
	chain     execwire.Chain
	canonical map[uint64]execwire.Hash
	methods   []string
	fresh     int
}

func (m *executionRPCMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     json.RawMessage
		Method string
		Params []json.RawMessage
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.methods = append(m.methods, req.Method)
	var result any
	var failure any
	switch req.Method {
	case "web3_clientVersion":
		result = "Gstable/E1-offline-fixture"
	case "eth_chainId":
		id, _ := new(big.Int).SetString(m.chain.ID, 10)
		result = hexutil.EncodeBig(id)
	case "eth_blockNumber":
		result = "0x1"
	case "eth_getBlockByNumber":
		var number string
		_ = json.Unmarshal(req.Params[0], &number)
		height, err := hexutil.DecodeUint64(number)
		if err == nil {
			result = map[string]any{"hash": common.Hash(m.canonical[height]).Hex()}
			if height > 0 {
				m.fresh++
			}
		}
	default:
		failure = map[string]any{"code": -32601, "message": "fixture forbids this RPC method"}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result, "error": failure})
}
func fixtureRPC(t *testing.T, f executionFrozen) (*executionRPCMock, *httptest.Server) {
	t.Helper()
	result, err := execwire.UnmarshalResult(f.Payload)
	require.NoError(t, err)
	m := &executionRPCMock{chain: f.Chain, canonical: map[uint64]execwire.Hash{0: f.Chain.Genesis, 1: result.Ref.Hash}}
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	return m, srv
}
func assertExecutionFrozen(t *testing.T, ctx context.Context, s *postgres.Store, f executionFrozen, ref execwire.Ref) {
	t.Helper()
	c, err := s.ExecutionStatus(ctx)
	require.NoError(t, err)
	require.Equal(t, ref, c.Visible)
	for _, expected := range f.Accounts {
		a, err := s.VisibleExecutionAccount(ctx, expected.Address)
		require.NoError(t, err)
		require.Equal(t, expected.Account, a.Account)
		code, err := s.VisibleExecutionCode(ctx, expected.Address)
		require.NoError(t, err)
		require.Equal(t, hexutil.Encode(expected.Code), hexutil.Encode(code.Code))
		balance, err := s.GetAddressBalance(ctx, common.Address(expected.Address), 0)
		require.NoError(t, err)
		require.Zero(t, balance.Cmp(new(big.Int).SetBytes(expected.Account.Balance[:])))
	}
	for _, expected := range f.Storage {
		v, err := s.VisibleExecutionStorage(ctx, expected.Address, expected.Slot)
		require.NoError(t, err)
		require.Equal(t, expected.After, v.Value)
	}
	lease, err := s.ReserveQueryReader(ctx)
	require.NoError(t, err)
	require.NotNil(t, lease)
	defer lease.Close()
	h, err := graphql.NewHandler(s, zap.NewNop())
	require.NoError(t, err)
	addr := common.Address(f.Accounts[0].Address).Hex()
	response := h.Execute(lease.Context(ctx), fmt.Sprintf(`{ latestHeight executionAccount(address:%q){height generation balance nonce extra exists} executionCode(address:%q){code} }`, addr, addr), nil)
	require.Empty(t, response.Errors)
	data := response.Data.(map[string]any)
	require.Equal(t, fmt.Sprint(ref.Height), data["latestHeight"])
	a := data["executionAccount"].(map[string]any)
	require.Equal(t, new(big.Int).SetBytes(f.Accounts[0].Account.Balance[:]).String(), a["balance"])
}

func TestExecutionFrozenEVMPostgresPublicationAndAPIParity(t *testing.T) {
	postgresDSN(t)
	for _, name := range []string{"evm-storage", "evm-revert", "evm-creations", "evm-calls", "wbft-native", "arc-storage", "arc-create-gone", "arc-calls", "arc-call-from-oog", "arc-call-from-direct"} {
		t.Run(name, func(t *testing.T) {
			f := loadExecutionFrozen(t, name)
			s, cfg := preparedExecutionDB(t, f)
			_, srv := fixtureRPC(t, f)
			cl, err := client.NewClient(&client.Config{Endpoint: srv.URL, Timeout: time.Second, RateLimit: 5, Logger: zap.NewNop()})
			require.NoError(t, err)
			defer cl.Close()
			p, _ := chains.Lookup(f.Profile)
			fe := fetch.NewFetcher(cl, s, &fetch.Config{StatePrefetch: true, RPCTimeout: time.Second, NoOutbox: true}, zap.NewNop(), nil)
			defer fe.Close()
			pipeline, err := feature.Build([]string{"address.index", "balance.native", "token.transfers"}, feature.Deps{Storage: s, Logger: zap.NewNop(), Profile: p, NativeBalances: feature.NativeBalancesFromContext, BlockAt: s.GetBlock})
			require.NoError(t, err)
			fe.SetFeatures(pipeline)
			fe.SetExecutionCanonical(sourcerpc.New(cl.RPCClient(), p))
			prepared, err := execution.Prepare(p.(chains.BinaryProfile), f.Chain, f.Payload)
			require.NoError(t, err)
			backend := &execution.StoreBackend{Store: s, Publisher: fe}
			scheduler, err := execution.New(t.Context(), execution.Config{Anchor: f.Baseline.Ref, MaxBlocks: 4, MaxBytes: 64 << 20}, backend)
			require.NoError(t, err)
			defer func() { require.NoError(t, scheduler.Close()) }()
			future, err := scheduler.Submit(t.Context(), prepared)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			require.NoError(t, future.Wait(ctx))
			observer, err := pgx.Connect(ctx, cfg.Database.Postgres.DSN)
			require.NoError(t, err)
			defer func() { require.NoError(t, observer.Close(context.Background())) }()
			var duplicateBalances, duplicateHistory int
			require.NoError(t, observer.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{cfg.Database.Postgres.Schema, "balances"}.Sanitize()).Scan(&duplicateBalances))
			require.NoError(t, observer.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{cfg.Database.Postgres.Schema, "balance_history"}.Sanitize()).Scan(&duplicateHistory))
			require.Zero(t, duplicateBalances, "native account writer is the only balance authority")
			require.Zero(t, duplicateHistory, "feature writer must not duplicate account history")
			result, err := execwire.UnmarshalResult(f.Payload)
			require.NoError(t, err)
			assertExecutionFrozen(t, ctx, s, f, result.Ref)
			if result.CallTraces != nil {
				page, e := s.ExecutionCalls(ctx, port.ExecutionCallQuery{Height: result.Ref.Height, Limit: 256})
				require.NoError(t, e)
				require.EqualValues(t, len(result.CallTraces.Frames), page.Total)
				if name == "arc-call-from-oog" {
					require.False(t, page.Frames[0].Reverted)
					require.True(t, page.Frames[1].Reverted)
					require.True(t, page.Frames[1].Internal)
				}
			}
			history, _, err := s.GetBalanceHistory(ctx, common.Address(f.Accounts[0].Address), 0, 1, port.Page{Limit: 100})
			require.NoError(t, err)
			require.NoError(t, backend.Publish(ctx, prepared))
			again, _, err := s.GetBalanceHistory(ctx, common.Address(f.Accounts[0].Address), 0, 1, port.Page{Limit: 100})
			require.NoError(t, err)
			require.Equal(t, history, again)
			if name == "evm-creations" {
				require.Len(t, result.Steps, 4)
				require.Equal(t, uint32(3), result.TxCount)
				creations, _, err := s.ListContracts(ctx, port.Page{Limit: 100})
				require.NoError(t, err)
				require.Len(t, creations, 2, "successful empty/gone creations survive; reverted creation is excluded")
			}
			if name == "arc-create-gone" {
				creations, _, err := s.ListContracts(ctx, port.Page{Limit: 100})
				require.NoError(t, err)
				require.Len(t, creations, 1, "Arc successful constructor-time destruction retains creation evidence")
			}
		})
	}
}

func dialExecutionFixture(t *testing.T, ctx context.Context, socket string, chain execwire.Chain, anchor execwire.Ref) *execwire.Conn {
	t.Helper()
	var raw net.Conn
	var err error
	// bind creates the pathname before listen is ready. Wait for the actual
	// transport admission, as the production exporter does on reconnect.
	require.Eventually(t, func() bool {
		raw, err = (&net.Dialer{Timeout: 50 * time.Millisecond}).DialContext(ctx, "unix", socket)
		return err == nil
	}, time.Second, time.Millisecond, "execution listener not ready")
	c := execwire.NewConn(raw)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: chain, Ref: anchor}}))
	hello, err := c.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, execwire.Hello, hello.Kind)
	return c
}
func sendExecutionFixture(t *testing.T, ctx context.Context, c *execwire.Conn, f executionFrozen, generation uint64) execwire.Ref {
	t.Helper()
	result, err := execwire.UnmarshalResult(f.Payload)
	require.NoError(t, err)
	result.Ref.Generation = generation
	data, err := execwire.MarshalResult(result)
	require.NoError(t, err)
	frames, err := execwire.Split(f.Chain, result.Ref, data, execwire.MaxHeaderBytes+512)
	require.NoError(t, err)
	for _, frame := range frames {
		require.NoError(t, c.Write(ctx, frame))
	}
	ack, err := c.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, execwire.Committed, ack.Kind)
	require.Equal(t, result.Ref, ack.Header.Ref)
	require.Equal(t, execwire.Hash(sha256.Sum256(data)), ack.Header.Digest)
	return result.Ref
}
func TestExecutionAppFrozenWBFTIPCReorgRestartAndLostACK(t *testing.T) {
	f := loadExecutionFrozen(t, "wbft-native")
	s, cfg := preparedExecutionDB(t, f)
	require.NoError(t, s.Close())
	m, srv := fixtureRPC(t, f)
	cfg.RPC.Endpoint = srv.URL
	cfg.RPC.CoalesceReads = false
	ipc, err := os.MkdirTemp("", "e1-ipc-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(ipc)) })
	cfg.Source.Execution.Socket = filepath.Join(ipc, "feed.sock")
	// Metadata's current contract calls are outside this fixture's canonical
	// RPC server; keep all remaining normal StableNet features enabled.
	no := false
	cfg.Features = map[string]config.FeatureConfig{"token.metadata": {Enabled: &no}}
	a, err := NewApp(cfg, zap.NewNop(), false, "stablenet")
	require.NoError(t, err)
	defer a.Shutdown()
	require.NotNil(t, a.executionFeed)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.run(ctx) }()
	finish := sync.OnceFunc(func() {
		cancel()
		a.executionFeed.Stop()
		err := <-done
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("execution runtime: %v", err)
		}
	})
	defer finish()
	c := dialExecutionFixture(t, ctx, cfg.Source.Execution.Socket, f.Chain, f.Baseline.Ref)
	ref := sendExecutionFixture(t, ctx, c, f, 1)
	store := a.storage.(*postgres.Store)
	assertExecutionFrozen(t, ctx, store, f, ref)
	seq, err := store.LastOutboxSeq(ctx)
	require.NoError(t, err)
	// Discard the ACK locally and reconnect from the old source checkpoint.
	require.NoError(t, c.Close())
	c = dialExecutionFixture(t, ctx, cfg.Source.Execution.Socket, f.Chain, f.Baseline.Ref)
	require.Equal(t, ref, sendExecutionFixture(t, ctx, c, f, 1))
	afterSeq, err := store.LastOutboxSeq(ctx)
	require.NoError(t, err)
	require.Equal(t, seq, afterSeq, "lost ACK does not repeat feature/outbox writes")
	fork := loadExecutionFrozen(t, "wbft-native-fork")
	fr, err := execwire.UnmarshalResult(fork.Payload)
	require.NoError(t, err)
	m.mu.Lock()
	m.canonical[1] = fr.Ref.Hash
	m.mu.Unlock()
	require.NoError(t, c.Write(ctx, execwire.Frame{Kind: execwire.Rewind, Header: execwire.Header{Version: execwire.Version, Chain: f.Chain, Ref: fr.Ref}}))
	_, err = c.Read(ctx)
	require.Error(t, err)
	require.Eventually(t, func() bool {
		status, err := store.ExecutionStatus(ctx)
		return err == nil && !status.Rewinding && status.Generation == 2 && status.Visible.Height == 0
	}, 3*time.Second, time.Millisecond)
	require.NoError(t, c.Close())
	c = dialExecutionFixture(t, ctx, cfg.Source.Execution.Socket, f.Chain, f.Baseline.Ref)
	ref = sendExecutionFixture(t, ctx, c, fork, 2)
	assertExecutionFrozen(t, ctx, store, fork, ref)
	require.NoError(t, c.Close())
	finish()
	a.Shutdown()
	reopened, err := NewApp(cfg, zap.NewNop(), false, "stablenet")
	require.NoError(t, err)
	defer reopened.Shutdown()
	assertExecutionFrozen(t, t.Context(), reopened.storage.(*postgres.Store), fork, ref)
	m.mu.Lock()
	defer m.mu.Unlock()
	require.GreaterOrEqual(t, m.fresh, 5)
	for _, method := range m.methods {
		require.False(t, strings.Contains(method, "getBalance"), method)
	}
}

// Offline operator path: actual WBFT genesis snapshot → private env → shared
// physical RPC budget → atomic PG bootstrap → existing seven-writer publication.
func TestCheckpointBootstrapToolThenExecutionPublication(t *testing.T) {
	f := loadExecutionFrozen(t, "wbft-native")
	rpcMock, srv := fixtureRPC(t, f)
	schema := testSchema(t.TempDir())
	dsn := postgresDSN(t)
	t.Cleanup(func() { dropTestSchemas(dsn, schema) })
	t.Setenv("INDEXER_DB_POSTGRES_DSN", dsn)
	t.Setenv("INDEXER_CHECKPOINT_RPC", srv.URL)
	path := "../../testdata/execution/checkpoints/wbft-genesis.ndjson"
	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	first, err := checkpointformat.NewDecoder(bytes.NewReader(payload)).Next()
	require.NoError(t, err)
	expected, err := json.Marshal(first.Manifest)
	require.NoError(t, err)
	expectedPath := filepath.Join(t.TempDir(), "expected.json")
	require.NoError(t, os.WriteFile(expectedPath, expected, 0600))
	p, _ := chains.Lookup(f.Profile)
	var output bytes.Buffer
	require.NoError(t, executionops.Run(t.Context(), executionops.Options{Action: "import", Input: path, Expected: expectedPath, Schema: schema}, p.(chains.HeaderCodec), &output))
	rpcMock.mu.Lock()
	canonicalReads := 0
	for _, method := range rpcMock.methods {
		if method == "eth_getBlockByNumber" {
			canonicalReads++
		}
	}
	rpcMock.mu.Unlock()
	require.Equal(t, 2, canonicalReads, "genesis identity and final fresh must be separate physical reads even at height0")
	s, err := postgres.Open(t.Context(), postgres.Options{DSN: dsn, Schema: schema, MaxConns: 4})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.True(t, c.BaselineReady)
	cl, err := client.NewClient(&client.Config{Endpoint: srv.URL, Timeout: time.Second, RateLimit: 5})
	require.NoError(t, err)
	defer cl.Close()
	fe := fetch.NewFetcher(cl, s, &fetch.Config{StatePrefetch: true, RPCTimeout: time.Second, NoOutbox: true}, zap.NewNop(), nil)
	defer fe.Close()
	pipeline, err := feature.Build([]string{"address.index", "balance.native", "token.transfers"}, feature.Deps{Storage: s, Logger: zap.NewNop(), Profile: p, NativeBalances: feature.NativeBalancesFromContext, BlockAt: s.GetBlock})
	require.NoError(t, err)
	fe.SetFeatures(pipeline)
	fe.SetExecutionCanonical(sourcerpc.New(cl.RPCClient(), p))
	prepared, err := execution.Prepare(p.(chains.BinaryProfile), f.Chain, f.Payload)
	require.NoError(t, err)
	backend := &execution.StoreBackend{Store: s, Publisher: fe}
	scheduler, err := execution.New(t.Context(), execution.Config{Anchor: c.Anchor, MaxBlocks: 4, MaxBytes: 64 << 20}, backend)
	require.NoError(t, err)
	defer func() { require.NoError(t, scheduler.Close()) }()
	future, err := scheduler.Submit(t.Context(), prepared)
	require.NoError(t, err)
	require.NoError(t, future.Wait(t.Context()))
	result, err := execwire.UnmarshalResult(f.Payload)
	require.NoError(t, err)
	assertExecutionFrozen(t, t.Context(), s, f, result.Ref)
	output.Reset()
	require.NoError(t, executionops.Run(t.Context(), executionops.Options{Action: "inspect", Schema: schema}, p.(chains.HeaderCodec), &output))
	require.Contains(t, output.String(), `"Coverage":"full"`)
}
