package rpcpool

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/feedtelemetry"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func rpcRecorder(t *testing.T) *feedtelemetry.Recorder {
	t.Helper()
	r, e := feedtelemetry.New(model.FeedBlock{Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, Hash: common.Hash{1}}, model.CapturePlan{Version: 1, Required: []model.DatasetRequirement{{Kind: model.DatasetHeaders, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}}}}, 0)
	require.NoError(t, e)
	t.Cleanup(r.Close)
	return r
}
func TestTelemetryCoalescedPhysicalAttemptsAndFailover(t *testing.T) {
	var attempts int
	p, start := stagedReadPool(t, readTransport(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{StatusCode: 503, Body: http.NoBody}, nil
		}
		var parts []rpcObject
		require.NoError(t, json.NewDecoder(req.Body).Decode(&parts))
		require.Len(t, parts, 2)
		for i := range parts {
			parts[i] = rpcObject{"jsonrpc": json.RawMessage(`"2.0"`), "id": parts[i]["id"], "result": json.RawMessage(`"ok"`)}
		}
		b, e := json.Marshal(parts)
		require.NoError(t, e)
		return readResponse(b), nil
	}))
	// The existing dispatcher reserves exactly one slot; failover must reserve
	// the second slot through the same five-per-second limiter.
	fallback, e := http.NewRequest("POST", "http://fallback", nil)
	require.NoError(t, e)
	p.endpoints = append(p.endpoints, &endpoint{url: fallback.URL})
	p.limiter = rate.NewLimiter(5, 1)
	r := rpcRecorder(t)
	release, e := p.AttachTelemetry(r)
	require.NoError(t, e)
	t.Cleanup(release)
	_, e = p.AttachTelemetry(r)
	require.Error(t, e)
	a := submitRead(p, context.Background(), `{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x1",false]}`)
	b := submitRead(p, context.Background(), `{"jsonrpc":"2.0","id":2,"method":"eth_getBlockByNumber","params":["0x2",false]}`)
	start(2)
	_ = replyJSON(t, a)
	_ = replyJSON(t, b)
	// A valid recorder snapshot requires the pinned scope; read it from the descriptor.
	anchor := model.FeedBlock{Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, Hash: common.Hash{1}}
	plan := model.CapturePlan{Version: 1, Required: []model.DatasetRequirement{{Kind: model.DatasetHeaders, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}}}}
	digest, _ := plan.Digest()
	v := feedtelemetry.Read(r.Descriptor(), anchor, digest)
	require.EqualValues(t, 2, v.Metrics["rpcHTTP"].Count)
	require.EqualValues(t, 1, v.Metrics["rpcHTTP"].Failures)
	require.EqualValues(t, 2, v.Metrics["rpcLimitWait"].Count)
	require.GreaterOrEqual(t, v.Metrics["rpcLimitWait"].TotalMS, float64(150))
	require.EqualValues(t, 1, p.Status()[0].Requests)
	require.EqualValues(t, 1, p.Status()[1].Requests)
	release()
	require.Nil(t, p.telemetry.Load())
	// Cancelled token waits do not become physical attempts.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", p.Primary(), bytes.NewBufferString(`{}`))
	_, e = p.RoundTrip(req)
	require.Error(t, e)
	require.Eventually(t, func() bool { return p.telemetry.Load() == nil }, time.Second, time.Millisecond)
}
