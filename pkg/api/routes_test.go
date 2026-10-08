package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

func init() {
	RegisterRoute(Route{Method: http.MethodGet, Pattern: "/test-routes/{name}", Handler: func(store port.QueryStore, _ *zap.Logger) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if store == nil {
				http.Error(w, "no store", http.StatusInternalServerError)
				return
			}
			if chi.URLParam(r, "name") == "missing" {
				http.Error(w, `{"error":"NOT_INDEXED"}`, http.StatusNotFound)
				return
			}
			_, _ = io.WriteString(w, "hello "+chi.URLParam(r, "name"))
		})
	}})
}

// TestRegisteredRoutes: a registered route is served over the server's
// storage, with its own status codes, also by a server of declared data
// only; registering it twice panics.
func TestRegisteredRoutes(t *testing.T) {
	for _, declared := range []bool{false, true} {
		s := securityServer(t, func(c *Config) { c.DeclaredOnly = declared; c.EnableRateLimit = false })
		serve := func(method, path, body string) (int, string) {
			rec := httptest.NewRecorder()
			s.Router().ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
			return rec.Code, rec.Body.String()
		}
		code, body := serve(http.MethodGet, "/test-routes/kiosk", "")
		assert.Equal(t, http.StatusOK, code, "declared only %v", declared)
		assert.Equal(t, "hello kiosk", body)
		code, _ = serve(http.MethodGet, "/test-routes/missing", "")
		assert.Equal(t, http.StatusNotFound, code)
		code, _ = serve(http.MethodPost, "/rpc", `{"jsonrpc":"2.0","id":1,"method":"eth_chainId"}`)
		if declared {
			assert.Equal(t, http.StatusNotFound, code, "no JSON-RPC with declared data only")
		} else {
			assert.NotEqual(t, http.StatusNotFound, code, "JSON-RPC is served")
		}
	}
	require.Panics(t, func() {
		RegisterRoute(Route{Method: http.MethodGet, Pattern: "/test-routes/{name}", Handler: func(port.QueryStore, *zap.Logger) http.Handler { return nil }})
	})
}
