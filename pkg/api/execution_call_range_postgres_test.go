package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/big"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestExecutionCallRangePostgresAPI(t *testing.T) {
	dsn := os.Getenv("INDEXER_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set INDEXER_TEST_POSTGRES for fsync-on API integration")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	schema := fmt.Sprintf("call_api_%d", time.Now().UnixNano())
	st, err := postgres.Open(ctx, postgres.Options{DSN: dsn, Schema: schema, MaxConns: 4})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, st.Close())
		conn, e := pgx.Connect(context.Background(), dsn)
		require.NoError(t, e)
		defer func() { _ = conn.Close(context.Background()) }()
		_, e = conn.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		require.NoError(t, e)
	})
	anchor := execwire.Ref{Generation: 1, Hash: execwire.Hash{1}}
	root := execwire.Hash{9}
	require.NoError(t, st.SetBlock(ctx, &model.Block{Hash: common.Hash(anchor.Hash), StateRoot: common.Hash(root)}))
	require.NoError(t, st.InitExecution(ctx, port.ExecutionConfig{Chain: execwire.Chain{ID: "8283", Genesis: anchor.Hash}, Anchor: anchor, AnchorRoot: root, PartitionBlocks: 2}))
	require.NoError(t, st.SeedExecution(ctx, port.ExecutionBaseline{Ref: anchor, Root: root}))
	to := common.Address{2}
	from := common.Address{1}
	ref := execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}
	txHash := common.Hash{7}
	b := &model.Block{Number: 1, Hash: common.Hash(ref.Hash), ParentHash: common.Hash(anchor.Hash), StateRoot: common.Hash{8}, Time: 1, Transactions: []*model.Transaction{{Hash: txHash, From: from, To: &to, Value: big.NewInt(1), Gas: 60000, BlockNumber: 1, BlockHash: common.Hash(ref.Hash)}}}
	rs := []*model.Receipt{{BlockNumber: 1, BlockHash: b.Hash, TxHash: txHash, Status: 1, GasUsed: 21000}}
	target, value := execwire.Address(to), execwire.Word{31: 1}
	r := execwire.Result{Ref: ref, Parent: anchor.Hash, Time: 1, StateRoot: execwire.Hash(b.StateRoot), ReceiptsRoot: execwire.Hash{4}, TxCount: 1, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}, Steps: []execwire.Step{{Cause: execwire.Transaction, TxHash: execwire.Hash(txHash)}, {Ordinal: 1, Cause: execwire.Finalize, TxIndex: -1}}, CallTraces: &execwire.CallTraces{Version: 1, Profile: "evm", Frames: []execwire.CallFrame{{TransactionHash: execwire.Hash(txHash), Parent: -1, Type: "CALL", From: execwire.Address(from), To: &target, Value: &value, Gas: 60000, GasUsed: 21000, Input: []byte{}, Output: []byte{}}}}}
	raw, err := execwire.MarshalResult(r)
	require.NoError(t, err)
	digest := execwire.Hash(sha256.Sum256(raw))
	for _, writer := range port.ExecutionWriters() {
		p := port.ExecutionPart{Writer: writer, Ref: ref, Parent: r.Parent, Digest: digest}
		require.NoError(t, st.WriteExecutionPart(ctx, p, func(ctx context.Context) error {
			switch writer {
			case port.ExecutionBlocks, port.ExecutionReceipts, port.ExecutionLogs:
				return st.ApplyExecutionCore(ctx, writer, b, rs)
			case port.ExecutionTransactions:
				if e := st.ApplyExecutionCore(ctx, writer, b, rs); e != nil {
					return e
				}
				return st.ApplyExecutionCalls(ctx, r, b, rs)
			}
			return nil
		}))
	}
	_, err = st.CommitExecution(ctx, port.ExecutionPublication{Ref: ref, Parent: r.Parent, Digest: digest, Root: r.StateRoot, CallTraces: true}, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	require.NoError(t, err)
	cfg := DefaultConfig()
	cfg.ReserveQueryReader = true
	cfg.EnableRateLimit = false
	server, err := NewServer(cfg, zap.NewNop(), st)
	require.NoError(t, err)
	require.Equal(t, 1, server.ReservedQueryReaders())
	defer func() { require.NoError(t, server.Stop(context.Background())) }()
	post := func(path, body string) string {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router().ServeHTTP(w, req.WithContext(ctx))
		return w.Body.String()
	}
	gql := fmt.Sprintf(`{"query":"{executionCalls(address:\"%s\",fromBlock:\"1\",toBlock:\"1\",limit:1){complete calls{ordinal coverage{generation epoch blockHash digest}}}}"}`, from.Hex())
	// Fill the three non-reserved slots. The API must use slot four from
	// the same pool, without calling an upstream service or adding a pool.
	release := make(chan struct{})
	entered := make(chan struct{}, 3)
	results := make(chan error, 3)
	for range 3 {
		go func() {
			results <- st.CaptureView(ctx, model.FeedChain{ID: "8283", GenesisHash: common.Hash(anchor.Hash)}, func(port.CaptureReader) error {
				entered <- struct{}{}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
	}
	for range 3 {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	result := post(cfg.GraphQLPath, gql)
	require.NotContains(t, result, `"errors"`)
	require.Contains(t, result, `"complete":true`)
	require.Contains(t, result, common.Hash(digest).Hex())
	cancelled, stop := context.WithCancel(ctx)
	stop()
	req := httptest.NewRequest("POST", cfg.GraphQLPath, strings.NewReader(gql))
	req.Header.Set("Content-Type", "application/json")
	server.Router().ServeHTTP(httptest.NewRecorder(), req.WithContext(cancelled))
	require.Contains(t, post(cfg.GraphQLPath, gql), `"complete":true`, "cancel returns the reservation")

	missingQuery := strings.Replace(gql, `toBlock:\"1\"`, `toBlock:\"2\"`, 1)
	require.Contains(t, post(cfg.GraphQLPath, missingQuery), "execution_calls_not_collected")
	partialQuery := strings.Replace(missingQuery, "limit:1)", "limit:1,partial:true)", 1)
	require.Contains(t, post(cfg.GraphQLPath, partialQuery), `"complete":false`)
	close(release)
	for range 3 {
		require.NoError(t, <-results)
	}
	_, err = st.BeginExecutionRewind(ctx, anchor, root)
	require.NoError(t, err)
	_, err = st.ResumeExecutionRewind(ctx, nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	require.Contains(t, post(cfg.GraphQLPath, gql), "execution_calls_not_collected")
	rpc := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"getExecutionCalls","params":{"address":"%s","fromBlock":"1","toBlock":"1","selection":"orphan","orphanEpoch":"1"}}`, from.Hex())
	require.Contains(t, post(cfg.JSONRPCPath, rpc), common.Hash(digest).Hex())
	require.NoError(t, server.Stop(context.Background()))
	require.NoError(t, st.Close())
	st, err = postgres.Open(ctx, postgres.Options{DSN: dsn, Schema: schema, MaxConns: 4, ReadOnly: true})
	require.NoError(t, err)
	server, err = NewServer(cfg, zap.NewNop(), st)
	require.NoError(t, err)
	require.Contains(t, post(cfg.JSONRPCPath, rpc), common.Hash(digest).Hex(), "orphan provenance survives API/store restart")
}
