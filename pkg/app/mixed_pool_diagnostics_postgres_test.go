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
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/api"
	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type dbDiagnosticRequest struct {
	trace *mixedAPITrace
	body  bytes.Buffer // read only after trace.done, after the last Write
	mu    sync.Mutex
	reads []postgres.ReadObservation
}

type dbDiagnosticWriter struct {
	http.ResponseWriter
	body *bytes.Buffer
}

func (w dbDiagnosticWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	_, _ = w.body.Write(p[:n])
	return n, err
}

type dbDiagnosticOutcome struct {
	row   mixedAPIObservation
	body  []byte
	reads []postgres.ReadObservation
}

func awaitDBDiagnostic[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		var zero T
		return zero
	}
}

// This is finite functional coverage, not an arrival/load/percentile run.
// The same production router, GraphQL pipeline and fixture diagnostics/verdict
// serve a small real-DB fixture; no node or public RPC is constructed.
func TestAPIDiagnosticsPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("INDEXER_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set INDEXER_TEST_POSTGRES")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	schema := "api_diag_" + testSchema(t.TempDir())
	store, err := postgres.Open(ctx, postgres.Options{DSN: dsn, Schema: schema, MaxConns: 4})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		conn, err := pgx.Connect(cleanup, dsn)
		require.NoError(t, err)
		defer func() { _ = conn.Close(cleanup) }()
		_, err = conn.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		require.NoError(t, err)
	})
	from, to := testchain.NewAccount(21), testchain.NewAccount(22)
	chain := testchain.NewChain(5042002, map[common.Address]*big.Int{from.Address: new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)})
	var transactions []testchain.TxSpec
	for i := 0; i < 2; i++ {
		transactions = append(transactions, testchain.TxSpec{From: from, Tx: &types.DynamicFeeTx{To: &to.Address, Gas: 50000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000)}, GasUsed: 21000})
	}
	chain.AddBlock(transactions...)
	block, err := gethconv.BlockFromGeth(chain.Block(1).Block)
	require.NoError(t, err)
	require.Len(t, block.Transactions, 2)
	require.NoError(t, store.SetBlock(ctx, block))
	require.NoError(t, store.SetLatestHeight(ctx, 1))
	cfg := api.DefaultConfig()
	cfg.ReserveQueryReader = true
	server, err := api.NewServer(cfg, zap.NewNop(), store)
	require.NoError(t, err)
	require.Equal(t, 1, server.ReservedQueryReaders())
	t.Cleanup(func() { require.NoError(t, server.Stop(context.Background())) })
	var traces, states sync.Map
	observed := mixedObservedHTTP(server.Router(), &traces)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if value, ok := states.Load(r.Header.Get("X-Indexer-Measure")); ok {
			w = dbDiagnosticWriter{ResponseWriter: w, body: &value.(*dbDiagnosticRequest).body}
		}
		observed.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)
	client := httpServer.Client()
	t.Cleanup(client.CloseIdleConnections)
	const single = `{ block(number:"1") { number hash transactionCount } }`
	const list = `{ blocks(pagination:{limit:8}) { nodes { number hash transactionCount } } }`
	run := func(requestCtx context.Context, id, query string, list bool, hook func(postgres.ReadObservation)) <-chan dbDiagnosticOutcome {
		state := &dbDiagnosticRequest{trace: &mixedAPITrace{done: make(chan struct{})}}
		state.trace.readHook = func(o postgres.ReadObservation) {
			state.mu.Lock()
			state.reads = append(state.reads, o)
			state.mu.Unlock()
			if hook != nil {
				hook(o)
			}
		}
		traces.Store(id, state.trace)
		states.Store(id, state)
		done := make(chan dbDiagnosticOutcome, 1)
		go func() {
			defer traces.Delete(id)
			defer states.Delete(id)
			start := time.Now()
			result, timing := mixedGraphQLTimed(context.WithValue(requestCtx, mixedTraceKey{}, id), client, httpServer.URL, query, chain, list)
			finished := time.Now()
			select {
			case <-state.trace.done:
			case <-ctx.Done():
			}
			row := mixedAPIObservation{Route: id, Phase: "functional", Result: result, MS: mixedMS(finished.Sub(start)), Started: start, Finished: finished, Diagnostics: state.trace.snapshot(), Client: timing}
			row.OutsideHandlerMS = mixedOutsideHandler(row)
			state.mu.Lock()
			reads := append([]postgres.ReadObservation(nil), state.reads...)
			state.mu.Unlock()
			// A missing handler completion is a functional failure below; do
			// not race a still-running response writer while recording it.
			var body []byte
			if row.Diagnostics.HandlerComplete {
				body = append([]byte(nil), state.body.Bytes()...)
			}
			done <- dbDiagnosticOutcome{row, body, reads}
		}()
		return done
	}
	compact := func(out dbDiagnosticOutcome) {
		t.Helper()
		var want bytes.Buffer
		require.NoError(t, json.Compact(&want, out.body))
		require.Equal(t, want.Bytes(), bytes.TrimSpace(out.body), "JSON values/errors remain compact through real DB/HTTP")
	}
	success := func(out dbDiagnosticOutcome, path string) {
		t.Helper()
		require.Equal(t, "ok", out.row.Result, "route %s: %s", out.row.Route, out.body)
		require.True(t, mixedAPIVerdict([]mixedAPIObservation{out.row}).Passed)
		require.True(t, out.row.Client.HTTPStarted)
		require.True(t, out.row.Client.ValidationStarted)
		require.Equal(t, 1, out.row.Diagnostics.Queries)
		require.Equal(t, map[string]int{path: 1}, out.row.Diagnostics.AdmissionPaths)
		for _, phase := range []string{"request_prepare", "parse", "limits", "validation", "execution", "response"} {
			require.Contains(t, out.row.Diagnostics.GraphQLMS, phase)
		}
		stages := map[string]int{}
		for _, o := range out.reads {
			stages[o.Stage]++
		}
		require.Equal(t, map[string]int{"admission": 1, "block_decode": 1}, stages)
		compact(out)
	}
	pinned := awaitDBDiagnostic(t, ctx, run(ctx, "pinned", single, false, nil))
	success(pinned, "pinned")
	window := awaitDBDiagnostic(t, ctx, run(ctx, "window", list, true, nil))
	success(window, "pinned")

	// Hold the reserved slot at its real admission callback, then demonstrate
	// a second HTTP request borrowing the SAME pool, with no artificial SQL.
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	requestCtx, cancelRequest := context.WithCancel(ctx)
	defer cancelRequest()
	held := run(requestCtx, "canceled", single, false, func(o postgres.ReadObservation) {
		if o.Stage == "admission" && o.Path == "pinned" {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	})
	awaitDBDiagnostic(t, ctx, entered)
	borrowed := awaitDBDiagnostic(t, ctx, run(ctx, "borrowed", single, false, nil))
	success(borrowed, "borrowed")
	require.GreaterOrEqual(t, borrowed.row.Diagnostics.BorrowProbes, 1)

	// Occupy the other three slots; recovery must use the returned pin.
	backgroundRelease := make(chan struct{})
	releaseBackground := sync.OnceFunc(func() { close(backgroundRelease) })
	backgroundEntered, backgroundDone := make(chan struct{}, 3), make(chan error, 3)
	ch := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	for i := 0; i < 3; i++ {
		go func() {
			backgroundDone <- store.CaptureView(ctx, ch, func(port.CaptureReader) error {
				backgroundEntered <- struct{}{}
				select {
				case <-backgroundRelease:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
	}
	defer func() {
		releaseBackground()
		for i := 0; i < 3; i++ {
			require.NoError(t, awaitDBDiagnostic(t, ctx, backgroundDone))
		}
	}()
	for i := 0; i < 3; i++ {
		awaitDBDiagnostic(t, ctx, backgroundEntered)
	}
	cancelRequest()
	canceled := awaitDBDiagnostic(t, ctx, held)
	require.Equal(t, "transport_error", canceled.row.Result, "explicit caller cancellation is not a timeout")
	require.False(t, mixedAPIVerdict([]mixedAPIObservation{canceled.row}).Passed)
	unblock()
	recovered := awaitDBDiagnostic(t, ctx, run(ctx, "recovered", single, false, nil))
	success(recovered, "pinned")

	missing := awaitDBDiagnostic(t, ctx, run(ctx, "missing", `{ block(number:"999") { number hash transactionCount } }`, false, nil))
	require.Equal(t, "graphql_error", missing.row.Result)
	require.Equal(t, 1, missing.row.Diagnostics.Queries)
	compact(missing)
	invalid := awaitDBDiagnostic(t, ctx, run(ctx, "invalid", `{ missingField }`, false, nil))
	require.Equal(t, "graphql_error", invalid.row.Result)
	require.Zero(t, invalid.row.Diagnostics.Queries)
	require.Empty(t, invalid.reads)
	compact(invalid)
	deep := `{ block(number:"1") {` + string(bytes.Repeat([]byte(`parent {`), 20)) + `number` + string(bytes.Repeat([]byte(`}`), 20)) + `} }`
	limited := awaitDBDiagnostic(t, ctx, run(ctx, "limited", deep, false, nil))
	require.Equal(t, "graphql_error", limited.row.Result)
	require.Zero(t, limited.row.Diagnostics.Queries)
	require.Empty(t, limited.reads)
	require.Contains(t, string(limited.body), "QUERY_TOO_DEEP")
	compact(limited)
	rows := []mixedAPIObservation{pinned.row, window.row, borrowed.row, canceled.row, recovered.row, missing.row, invalid.row, limited.row}
	verdict := mixedAPIVerdict(rows)
	require.False(t, verdict.Passed, "intentional API errors must never receive a passing verdict")
	require.Equal(t, map[string]int{"transport_error": 1, "graphql_error": 3}, verdict.Errors)
	require.Zero(t, verdict.IncompleteDiagnostics)
	// Export counts/verdict only, never latency summaries or a measurement marker.
	artifact := map[string]any{"maxConns": 4, "apiReservedReadSlots": 1, "api": map[string]any{"functional": map[string]any{"Errors": verdict.Errors, "IncompleteDiagnostics": verdict.IncompleteDiagnostics}}, "apiValidation": verdict}
	value, err := json.Marshal(artifact)
	require.NoError(t, err)
	dir := os.Getenv("INDEXER_FUNCTIONAL_ARTIFACT_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "db-api-verdict.json"), value, 0o600))
}
