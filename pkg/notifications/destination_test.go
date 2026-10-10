package notifications

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestCheckDestination: a setting's destination must be http(s) without
// credentials, and, unless private destinations are allowed, not an
// internal address or local name.
func TestCheckDestination(t *testing.T) {
	for _, tc := range []struct {
		url      string
		public   bool // allowed with private destinations refused
		anyScope bool // allowed when private destinations are allowed
	}{
		{"https://hooks.example.com/services/T00", true, true},
		{"http://hooks.example.com/notify", true, true},
		{"https://93.184.216.34/notify", true, true},
		{"http://localhost:8080/webhook", false, true},
		{"http://api.localhost/webhook", false, true},
		{"http://127.0.0.1/webhook", false, true},
		{"http://10.0.0.5/webhook", false, true},
		{"http://192.168.1.10/webhook", false, true},
		{"http://172.16.0.1/webhook", false, true},
		{"http://169.254.169.254/latest/meta-data", false, true},
		{"http://100.64.0.1/webhook", false, true},
		{"http://0.0.0.0/webhook", false, true},
		{"http://[::1]/webhook", false, true},
		{"http://[fd00::1]/webhook", false, true},
		{"http://[::ffff:127.0.0.1]/webhook", false, true},
		{"http://metadata.google.internal/computeMetadata", false, true},
		{"http://intranet/webhook", false, true},
		{"ftp://hooks.example.com/notify", false, false},
		{"https://user:pass@hooks.example.com/notify", false, false},
		{"not a url", false, false},
		{"", false, false},
	} {
		err := checkDestination(tc.url, false)
		assert.Equal(t, tc.public, err == nil, "%q refused=%v", tc.url, err)
		err = checkDestination(tc.url, true)
		assert.Equal(t, tc.anyScope, err == nil, "%q with private allowed: %v", tc.url, err)
	}
}

// TestDeliveryClientRefusesInternalAddresses: the check runs on the
// address dialed after resolution, so a name resolving to loopback is
// refused like a loopback address, and nothing reaches the server.
func TestDeliveryClientRefusesInternalAddresses(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	byName := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)

	refusing := newDeliveryClient(time.Second, false)
	for _, u := range []string{srv.URL, byName} {
		_, err := refusing.Post(u, "application/json", strings.NewReader("{}"))
		require.Error(t, err, u)
		assert.ErrorIs(t, err, errBlockedDestination, u)
	}
	assert.Zero(t, hits.Load(), "no request reached the internal server")

	resp, err := newDeliveryClient(time.Second, true).Post(srv.URL, "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, int32(1), hits.Load(), "allowed for development")
}

// TestDeliveryClientDoesNotFollowRedirects: a destination cannot forward
// the delivery elsewhere (for example inward); the redirect is the result.
func TestDeliveryClientDoesNotFollowRedirects(t *testing.T) {
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirecting.Close()

	resp, err := newDeliveryClient(time.Second, true).Post(redirecting.URL, "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	assert.Zero(t, reached.Load())
}

// TestWebhookValidateRefusesInternalDestinations: registering a webhook to
// an internal address fails unless private destinations are allowed.
func TestWebhookValidateRefusesInternalDestinations(t *testing.T) {
	setting := &NotificationSetting{Type: NotificationTypeWebhook, Destination: Destination{WebhookURL: "http://169.254.169.254/latest/meta-data"}}
	cfg := DefaultConfig().Webhook
	require.Error(t, NewWebhookHandler(&cfg, zap.NewNop()).Validate(setting))
	cfg.AllowPrivateDestinations = true
	require.NoError(t, NewWebhookHandler(&cfg, zap.NewNop()).Validate(setting))

	slack := &NotificationSetting{Type: NotificationTypeSlack, Destination: Destination{SlackWebhookURL: "http://127.0.0.1:9/hook"}}
	require.Error(t, NewSlackHandler(nil, zap.NewNop()).Validate(slack))
}
