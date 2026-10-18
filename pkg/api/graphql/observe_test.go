package graphql

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRequestDiagnosticsPreserveResponses(t *testing.T) {
	for _, tc := range []struct {
		name, query  string
		limits       Limits
		want, absent []string
	}{
		{"success", `{ latestHeight }`, Limits{MaxDepth: 4}, []string{"limits", "request_prepare", "parse", "validation", "execution", "response"}, nil},
		{"syntax", `{`, Limits{MaxDepth: 4}, []string{"request_prepare", "parse", "response"}, []string{"limits", "validation", "execution"}},
		{"schema_error", `{ missingField }`, Limits{}, []string{"request_prepare", "parse", "validation", "response"}, []string{"limits", "execution"}},
		{"limit_rejected", `{ block(number: "1") { number } }`, Limits{MaxDepth: 1}, []string{"request_prepare", "parse", "limits", "response"}, []string{"validation", "execution"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(t)
			h.limits = tc.limits
			plain, observed := httptest.NewRecorder(), httptest.NewRecorder()
			h.ServeHTTP(plain, jsonRequest(t, tc.query, nil))
			seen := map[string]int{}
			r := jsonRequest(t, tc.query, nil)
			r = r.WithContext(ObserveRequest(r.Context(), func(o RequestObservation) {
				require.GreaterOrEqual(t, o.Elapsed, time.Duration(0))
				seen[o.Stage]++
			}))
			h.ServeHTTP(observed, r)
			require.Equal(t, plain.Code, observed.Code)
			require.JSONEq(t, plain.Body.String(), observed.Body.String())
			var body map[string]interface{}
			require.NoError(t, json.Unmarshal(observed.Body.Bytes(), &body))
			require.NotContains(t, body, "extensions", "diagnostics stay outside the API response")
			for _, stage := range tc.want {
				require.Equal(t, 1, seen[stage], stage)
			}
			for _, stage := range tc.absent {
				require.Zero(t, seen[stage], stage)
			}
		})
	}
}
