package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	apigraphql "github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/stretchr/testify/require"
)

type mixedRoundTripFunc func(*http.Request) (*http.Response, error)

func (f mixedRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type mixedTimeoutBody struct{}

func (mixedTimeoutBody) Read([]byte) (int, error) { return 0, context.DeadlineExceeded }
func (mixedTimeoutBody) Close() error             { return nil }

func TestMixedClientTimingAndFailures(t *testing.T) {
	// One local fixture block and an in-memory transport: no live RPC/DB/HTTP
	// server, arrival loop, sleep or percentile/performance assertions.
	c := testchain.NewChain(5042002, nil)
	c.AddBlock()
	value := fmt.Sprintf(`{"data":{"block":{"number":"1","hash":%q,"transactionCount":2}}}`, c.Block(1).Block.Hash().Hex())
	for _, tc := range []struct {
		name, body, want string
		status           int
		err              error
		timeoutBody      bool
		validate         bool
	}{
		{"ok", value, "ok", 200, nil, false, true},
		{"invalid_json", `{`, "invalid_data", 200, nil, false, true},
		{"graphql_error", `{"errors":[{"message":"failure"}]}`, "graphql_error", 200, nil, false, true},
		{"http_error", `{}`, "http_429", 429, nil, false, true},
		{"header_timeout", "", "timeout", 0, context.DeadlineExceeded, false, false},
		{"body_timeout", "", "timeout", 200, nil, true, false},
		{"transport_error", "", "transport_error", 0, errors.New("unavailable"), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: mixedRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, "trace-id", r.Header.Get("X-Indexer-Measure"))
				if tc.err != nil {
					return nil, tc.err
				}
				body := io.NopCloser(strings.NewReader(tc.body))
				if tc.timeoutBody {
					body = mixedTimeoutBody{}
				}
				return &http.Response{StatusCode: tc.status, Body: body, Header: make(http.Header), Request: r}, nil
			})}
			ctx := context.WithValue(t.Context(), mixedTraceKey{}, "trace-id")
			result, timing := mixedGraphQLTimed(ctx, client, "http://fixture", `{ block(number:"1"){number hash transactionCount} }`, c, false)
			require.Equal(t, tc.want, result)
			require.True(t, timing.HTTPStarted)
			require.Equal(t, tc.validate, timing.ValidationStarted)
			require.GreaterOrEqual(t, timing.PrepareMS, float64(0))
			require.GreaterOrEqual(t, timing.HTTPMS, float64(0))
			if !tc.validate {
				require.Zero(t, timing.ValidationMS)
			}
		})
	}
}

func TestMixedVerdictAndUnavailableTiming(t *testing.T) {
	base := time.Unix(100, 0)
	ok := mixedAPIObservation{Route: "block", Phase: "drain", Result: "ok", MS: 10, Started: base, Finished: base.Add(10 * time.Millisecond), Diagnostics: mixedAPIDiagnostics{HandlerComplete: true, ServerStarted: base.Add(time.Millisecond), ServerFinished: base.Add(5 * time.Millisecond), ServerMS: 4, AcquireMS: 1, QueryMS: 2}}
	require.True(t, mixedAPIVerdict([]mixedAPIObservation{ok}).Passed)
	require.Equal(t, float64(6), *mixedOutsideHandler(ok))
	for _, result := range []string{"timeout", "http_500", "graphql_error", "invalid_data", "body_error", "transport_error"} {
		failed := ok
		failed.Result = result
		v := mixedAPIVerdict([]mixedAPIObservation{ok, failed})
		require.False(t, v.Passed)
		require.Equal(t, 1, v.Errors[result])
	}
	incomplete := ok
	incomplete.Diagnostics.HandlerComplete = false
	require.Nil(t, mixedOutsideHandler(incomplete))
	require.False(t, mixedAPIVerdict([]mixedAPIObservation{incomplete}).Passed)
	require.False(t, mixedAPIVerdict(nil).Passed)
	overlap := ok
	overlap.Diagnostics.ServerFinished = ok.Finished.Add(time.Millisecond)
	require.Nil(t, mixedOutsideHandler(overlap))
	summary := mixedAPISummaries([]mixedAPIObservation{incomplete})["drain/block"]
	require.Equal(t, 1, summary.IncompleteDiagnostics)
	require.Nil(t, summary.ServerMS, "missing diagnostics must not become a zero-ms sample")
	require.Nil(t, summary.OtherServerMS)
	negative := ok
	negative.Diagnostics.QueryMS = 20
	summary = mixedAPISummaries([]mixedAPIObservation{negative})["drain/block"]
	require.Equal(t, 1, summary.InvalidServerResiduals)
	require.Nil(t, summary.OtherServerMS)
	_, err := json.Marshal(mixedAPIVerdict([]mixedAPIObservation{incomplete}))
	require.NoError(t, err, "failure verdict can be emitted before the assertion")
}

func TestMixedSnapshotSurvivesLateCallbacks(t *testing.T) {
	tr := &mixedAPITrace{}
	tr.observeGraphQL(apigraphql.RequestObservation{Stage: "parse", Elapsed: time.Millisecond})
	tr.observeRead(postgres.ReadObservation{Stage: "admission", Path: "pinned"})
	saved := tr.snapshot()
	// A canceled resolver may still emit a callback after the handler returns.
	// Emitting/marshaling that earlier snapshot must never share mutable maps.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 8; i++ {
			tr.observeGraphQL(apigraphql.RequestObservation{Stage: "parse", Elapsed: time.Millisecond})
			tr.observeRead(postgres.ReadObservation{Stage: "admission", Path: "borrowed", BorrowProbes: 1})
		}
	}()
	for i := 0; i < 8; i++ {
		_, err := json.Marshal(saved)
		require.NoError(t, err)
	}
	<-done
	require.Equal(t, map[string]float64{"parse": 1}, saved.GraphQLMS)
	require.Equal(t, map[string]int{"pinned": 1}, saved.AdmissionPaths)
	require.Equal(t, 8, tr.snapshot().BorrowProbes)
}
