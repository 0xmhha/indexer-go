package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestParseTrustedProxies(t *testing.T) {
	got, err := ParseTrustedProxies([]string{"10.0.0.0/8", " 192.0.2.1 ", "", "::1", "10.1.2.3/8"})
	require.NoError(t, err)
	var s []string
	for _, p := range got {
		s = append(s, p.String())
	}
	assert.Equal(t, []string{"10.0.0.0/8", "192.0.2.1/32", "::1/128", "10.0.0.0/8"}, s)

	_, err = ParseTrustedProxies([]string{"proxy.local"})
	assert.ErrorContains(t, err, "proxy.local")
}

// TestClientIP: forwarding headers count only from trusted proxies, and
// only the hops those proxies added.
func TestClientIP(t *testing.T) {
	trusted, err := ParseTrustedProxies([]string{"10.0.0.0/8", "::1"})
	require.NoError(t, err)

	cases := []struct {
		name    string
		trusted bool
		peer    string
		headers map[string][]string
		want    string
	}{
		{name: "no proxies configured", peer: "192.0.2.7:4000",
			headers: map[string][]string{"X-Forwarded-For": {"203.0.113.9"}}, want: "192.0.2.7"},
		{name: "untrusted peer", trusted: true, peer: "192.0.2.7:4000",
			headers: map[string][]string{"X-Forwarded-For": {"203.0.113.9"}, "X-Real-IP": {"203.0.113.8"}}, want: "192.0.2.7"},
		{name: "trusted proxy", trusted: true, peer: "10.0.0.5:4000",
			headers: map[string][]string{"X-Forwarded-For": {"203.0.113.9"}}, want: "203.0.113.9"},
		{name: "client-written hops are skipped", trusted: true, peer: "10.0.0.5:4000",
			headers: map[string][]string{"X-Forwarded-For": {"1.1.1.1, 203.0.113.9, 10.0.0.6"}}, want: "203.0.113.9"},
		{name: "several header lines", trusted: true, peer: "10.0.0.5:4000",
			headers: map[string][]string{"X-Forwarded-For": {"1.1.1.1", "203.0.113.9"}}, want: "203.0.113.9"},
		{name: "unreadable hop stops the walk", trusted: true, peer: "10.0.0.5:4000",
			headers: map[string][]string{"X-Forwarded-For": {"203.0.113.9, garbage, 10.0.0.6"}}, want: "10.0.0.6"},
		{name: "only proxies", trusted: true, peer: "10.0.0.5:4000",
			headers: map[string][]string{"X-Forwarded-For": {"10.0.0.7"}}, want: "10.0.0.7"},
		{name: "X-Real-IP from a trusted proxy", trusted: true, peer: "[::1]:4000",
			headers: map[string][]string{"X-Real-IP": {"203.0.113.9"}}, want: "203.0.113.9"},
		{name: "IPv4-mapped peer", trusted: true, peer: "[::ffff:10.0.0.5]:4000",
			headers: map[string][]string{"X-Forwarded-For": {"203.0.113.9"}}, want: "203.0.113.9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var proxies = trusted
			if !tc.trusted {
				proxies = nil
			}
			var got string
			h := ClientIP(proxies, zap.NewNop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.RemoteAddr
			}))
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.peer
			for k, vs := range tc.headers {
				for _, v := range vs {
					req.Header.Add(k, v)
				}
			}
			h.ServeHTTP(httptest.NewRecorder(), req)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestRateLimitBehindTrustedProxy: clients behind a trusted proxy have an
// allowance each; a client cannot borrow another's by writing headers.
func TestRateLimitBehindTrustedProxy(t *testing.T) {
	trusted, err := ParseTrustedProxies([]string{"10.0.0.5"})
	require.NoError(t, err)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := ClientIP(trusted, zap.NewNop())(RateLimit(1, 1, zap.NewNop())(ok))

	send := func(xff string) int {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.5:4000"
		req.Header.Set("X-Forwarded-For", xff)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	assert.Equal(t, http.StatusOK, send("203.0.113.1"))
	assert.Equal(t, http.StatusOK, send("203.0.113.2"), "another client has its own allowance")
	assert.Equal(t, http.StatusTooManyRequests, send("198.51.100.1, 203.0.113.1"), "a written hop does not change the client")
}
