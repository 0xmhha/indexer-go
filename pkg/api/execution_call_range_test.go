package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type rangeAPIProbe struct {
	*nativeCallAPIProbe
	rangeQuery port.ExecutionCallRangeQuery
	rangeErr   error
	calls      int
}

func (p *rangeAPIProbe) ExecutionCallRange(ctx context.Context, q port.ExecutionCallRangeQuery) (port.ExecutionCallRangePage, error) {
	p.rangeQuery = q
	p.calls++
	p.bound = ctx.Value(nativeCallSnapshotKey{}) == true
	if !p.bound {
		return port.ExecutionCallRangePage{}, errors.New("missing reserved snapshot")
	}
	c := port.ExecutionCallCoverage{Height: 1, Collected: true, Epoch: 1, Ref: execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}, Digest: execwire.Hash{3}, Profile: "evm"}
	return port.ExecutionCallRangePage{Selection: q.Selection, Epoch: 1, Complete: true, Calls: []port.ExecutionCallRangeItem{{Coverage: c, Call: port.ExecutionCall{CallFrame: execwire.CallFrame{Type: "CALL", From: q.Address, Parent: -1}}}}, Coverage: []port.ExecutionCallCoverage{c}}, p.rangeErr
}

func TestSeedPriority2CallQueriesAndOrphanTraces(t *testing.T) {
	p := &rangeAPIProbe{nativeCallAPIProbe: &nativeCallAPIProbe{mockStorage: &mockStorage{}}}
	cfg := DefaultConfig()
	cfg.EnableRateLimit = false
	s, err := NewServer(cfg, zap.NewNop(), p)
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Stop(context.Background())) }()
	post := func(path, body string) string {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		s.Router().ServeHTTP(w, r)
		return w.Body.String()
	}
	gql := `{"query":"{executionCalls(address:\"0x0000000000000000000000000000000000000001\",fromBlock:\"1\",toBlock:\"2\",limit:1){selection epoch complete missing{fromBlock toBlock} coverage{collected generation blockHash digest} calls{ordinal from coverage{generation digest}} next}}"}`
	result := post(cfg.GraphQLPath, gql)
	require.NotContains(t, result, "Cannot query field")
	require.NotContains(t, result, `"errors"`)
	require.Contains(t, result, `"complete":true`)
	require.Contains(t, result, `"ordinal":0`)
	require.True(t, p.bound)
	require.EqualValues(t, 1, p.rangeQuery.Limit)
	require.Equal(t, "canonical", p.rangeQuery.Selection)
	rpc := `{"jsonrpc":"2.0","id":1,"method":"getExecutionCalls","params":{"address":"0x0000000000000000000000000000000000000001","fromBlock":"1","toBlock":"2","limit":1,"selection":"orphan","orphanEpoch":"1"}}`
	result = post(cfg.JSONRPCPath, rpc)
	require.Contains(t, result, `"complete":true`)
	require.True(t, p.bound)
	require.Equal(t, "orphan", p.rangeQuery.Selection)
	require.EqualValues(t, 1, p.rangeQuery.OrphanEpoch)
	for _, test := range []struct {
		err  error
		code string
	}{
		{&port.ExecutionCallCoverageError{Missing: []port.ExecutionCallMissingRange{{FromBlock: 2, ToBlock: 3}}}, "execution_calls_not_collected"},
		{port.ErrExecutionStale, "execution_call_cursor_stale"}, {port.ErrExecutionRewinding, "execution_calls_unavailable"}, {context.Canceled, "execution_call_cancelled"}, {errors.New("secret database DSN"), "execution_calls_unavailable"},
	} {
		p.rangeErr = test.err
		for _, response := range []string{post(cfg.GraphQLPath, gql), post(cfg.JSONRPCPath, rpc)} {
			require.Contains(t, response, test.code)
			require.NotContains(t, response, "secret database")
		}
	}
	p.rangeErr = nil
	partialRPC := strings.Replace(rpc, `"orphanEpoch":"1"`, `"orphanEpoch":"1","partial":true`, 1)
	require.Contains(t, post(cfg.JSONRPCPath, partialRPC), `"complete":true`)
	require.True(t, p.rangeQuery.Partial)
	p.rangeErr = nil
	for _, params := range []string{
		`{"address":"0x0000000000000000000000000000000000000001","fromBlock":"1","toBlock":"2","partial":false,"partial":true}`,
		`{"address":"0x0000000000000000000000000000000000000001","fromBlock":"1","toBlock":"2","limit":null}`,
		`{"address":"0x01","fromBlock":"1","toBlock":"2"}`,
		`{"address":"0x0000000000000000000000000000000000000001","fromBlock":"2","toBlock":"1"}`,
		`{"address":"0x0000000000000000000000000000000000000001","fromBlock":"0","toBlock":"128"}`,
		`{"address":"0x0000000000000000000000000000000000000001","fromBlock":"1","toBlock":"2","limit":257}`,
		`{"address":"0x0000000000000000000000000000000000000001","fromBlock":"1","toBlock":"2","limit":0}`,
		`{"address":"0x0000000000000000000000000000000000000001","fromBlock":"1","toBlock":"2","selection":"orphan"}`,
		`{"address":"0x0000000000000000000000000000000000000001","fromBlock":"1","toBlock":"2","orphanEpoch":"1"}`,
		`{"address":"0x0000000000000000000000000000000000000001","fromBlock":"1","toBlock":"2","unknown":1}`,
	} {
		before := p.calls
		response := post(cfg.JSONRPCPath, `{"jsonrpc":"2.0","id":1,"method":"getExecutionCalls","params":`+params+`}`)
		require.Contains(t, response, `"code":-32602`)
		require.Equal(t, before, p.calls, "reject before range storage work")
	}
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(post(cfg.GraphQLPath, gql)), &decoded))
}
