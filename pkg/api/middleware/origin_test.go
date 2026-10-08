package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func corsResponse(o Origins, method, origin string) *httptest.ResponseRecorder {
	h := CORS(o)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	req := httptest.NewRequest(method, "/graphql", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestCORS: no response allows credentials, "*" answers "*" instead of
// echoing the origin, and only listed origins are named.
func TestCORS(t *testing.T) {
	t.Run("any origin", func(t *testing.T) {
		o := NewOrigins([]string{"*"})
		rec := corsResponse(o, "POST", "https://evil.example")
		assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"))
		assert.Equal(t, http.StatusTeapot, rec.Code)

		rec = corsResponse(o, "POST", "")
		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"), "not a cross-origin request")
		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"))
	})
	t.Run("listed origins", func(t *testing.T) {
		o := NewOrigins([]string{"https://explorer.example/", "http://localhost:3000"})
		rec := corsResponse(o, "POST", "https://Explorer.example")
		assert.Equal(t, "https://Explorer.example", rec.Header().Get("Access-Control-Allow-Origin"))
		assert.Contains(t, rec.Header().Values("Vary"), "Origin")
		assert.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), "X-API-Key")
		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"))

		rec = corsResponse(o, "POST", "https://evil.example")
		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
		assert.Contains(t, rec.Header().Values("Vary"), "Origin")

		rec = corsResponse(o, "OPTIONS", "http://localhost:3000")
		assert.Equal(t, http.StatusOK, rec.Code, "preflight is answered here")
		assert.Equal(t, "http://localhost:3000", rec.Header().Get("Access-Control-Allow-Origin"))
	})
}

func TestCheckWebSocketOrigin(t *testing.T) {
	listed := NewOrigins([]string{"https://explorer.example"})
	cases := []struct {
		name   string
		o      Origins
		origin string
		host   string
		want   bool
	}{
		{"no Origin (not a browser)", listed, "", "api.example", true},
		{"listed", listed, "https://explorer.example", "api.example", true},
		{"same host", listed, "https://api.example", "api.example", true},
		{"same host and port", listed, "http://localhost:8080", "localhost:8080", true},
		{"other site", listed, "https://evil.example", "api.example", false},
		{"other port", listed, "http://localhost:3001", "localhost:8080", false},
		{"any origin", NewOrigins([]string{"*"}), "https://evil.example", "api.example", true},
		{"nothing allowed", NewOrigins(nil), "https://evil.example", "api.example", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/graphql/ws", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			assert.Equal(t, tc.want, tc.o.CheckWebSocketOrigin(req))
		})
	}
}
