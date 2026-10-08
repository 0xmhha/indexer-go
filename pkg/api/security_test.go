package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// The public API's protections (refactoring plan R4-3, S1): the rate limit
// is on by default and keys on an address clients cannot forge, CORS never
// allows credentials, WebSocket upgrades check their Origin, and GraphQL
// requests over the depth or complexity bound are refused.

func securityServer(t *testing.T, mutate func(*Config)) *Server {
	t.Helper()
	cfg := DefaultConfig()
	if mutate != nil {
		mutate(cfg)
	}
	s, err := NewServer(cfg, zap.NewNop(), &mockStorage{})
	require.NoError(t, err)
	return s
}

func TestDefaultRateLimitIgnoresForgedHeaders(t *testing.T) {
	cfg := DefaultConfig()
	require.True(t, cfg.EnableRateLimit, "the rate limit is on by default")
	s := securityServer(t, func(c *Config) { c.RateLimitPerSecond, c.RateLimitBurst = 1, 3 })

	codes := make([]int, 0, 5)
	for i := range 5 {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "192.0.2.7:4000"
		req.Header.Set("X-Forwarded-For", "203.0.113."+string(rune('1'+i)))
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, req)
		codes = append(codes, w.Code)
	}
	assert.Equal(t, []int{200, 200, 200, 429, 429}, codes, "a new X-Forwarded-For per request gets no new allowance")
}

func TestRateLimitBehindTrustedProxy(t *testing.T) {
	s := securityServer(t, func(c *Config) {
		c.RateLimitPerSecond, c.RateLimitBurst = 1, 1
		c.TrustedProxies = []string{"10.0.0.0/8"}
	})
	send := func(client string) int {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "10.1.2.3:4000"
		req.Header.Set("X-Forwarded-For", client)
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, req)
		return w.Code
	}
	assert.Equal(t, http.StatusOK, send("203.0.113.1"))
	assert.Equal(t, http.StatusOK, send("203.0.113.2"), "every client behind the proxy has its own allowance")
	assert.Equal(t, http.StatusTooManyRequests, send("203.0.113.1"))
}

func TestInvalidTrustedProxy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TrustedProxies = []string{"proxy.local"}
	_, err := NewServer(cfg, zap.NewNop(), &mockStorage{})
	assert.ErrorContains(t, err, "proxy.local")
}

func TestCORSNeverAllowsCredentials(t *testing.T) {
	for _, origins := range [][]string{{"*"}, {"https://explorer.example"}} {
		s := securityServer(t, func(c *Config) { c.AllowedOrigins = origins })
		for _, origin := range []string{"", "https://explorer.example", "https://evil.example"} {
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			if origin != "" {
				req.Header.Set("Origin", origin)
			}
			w := httptest.NewRecorder()
			s.Router().ServeHTTP(w, req)
			assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"), "%v %q", origins, origin)
			allow := w.Header().Get("Access-Control-Allow-Origin")
			switch {
			case origin == "":
				assert.Empty(t, allow, "%v: no Origin, no CORS", origins)
			case origins[0] == "*":
				assert.Equal(t, "*", allow, "%q", origin)
			case origin == origins[0]:
				assert.Equal(t, origin, allow)
			default:
				assert.Empty(t, allow, "%q is not allowed", origin)
			}
		}
	}
}

func TestWebSocketOriginCheck(t *testing.T) {
	s := securityServer(t, func(c *Config) {
		c.AllowedOrigins = []string{"https://explorer.example"}
		c.EnableRateLimit = false
	})
	bus := events.NewEventBus(100, 100)
	go bus.Run()
	defer bus.Stop()
	s.SetEventBus(bus)
	srv := httptest.NewServer(s.Router())
	defer srv.Close()
	base := "ws" + strings.TrimPrefix(srv.URL, "http")

	for _, path := range []string{"/graphql/ws", "/ws"} {
		for origin, ok := range map[string]bool{
			"":                         true,
			"https://explorer.example": true,
			srv.URL:                    true, // the server's own pages
			"https://evil.example":     false,
		} {
			h := http.Header{}
			if origin != "" {
				h.Set("Origin", origin)
			}
			conn, resp, err := websocket.DefaultDialer.Dial(base+path, h)
			if ok {
				if assert.NoError(t, err, "%s from %q", path, origin) {
					_ = conn.Close()
				}
				continue
			}
			assert.Error(t, err, "%s from %q", path, origin)
			if assert.NotNil(t, resp) {
				assert.Equal(t, http.StatusForbidden, resp.StatusCode)
			}
		}
	}
}

func TestGraphQLBoundsAreOnByDefault(t *testing.T) {
	s := securityServer(t, nil)
	deep := `{"query":"{ a { b { c { d { e { f { g { h { i { j { k { l { m { n { o { p } } } } } } } } } } } } } } } }"}`
	wide := `{"query":"{ blocks(pagination: {limit: 1000}) { nodes { number hash parentHash timestamp miner gasUsed } } }"}`
	for name, body := range map[string]string{"QUERY_TOO_DEEP": deep, "QUERY_TOO_COMPLEX": wide} {
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), name)
	}
}
