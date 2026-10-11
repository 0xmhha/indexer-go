package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// verifyingService is a service whose webhooks must answer a challenge
// when verify is set (local test servers allowed).
func verifyingService(t *testing.T, verify bool) *NotificationService {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	svc := NewService(cfg, newMockStorage(), events.NewEventBus(100, 100), zap.NewNop())
	svc.RegisterHandler(NewWebhookHandler(&WebhookConfig{Enabled: true, Timeout: 5 * time.Second, AllowPrivateDestinations: true,
		SignatureHeader: "X-Signature-256", VerifyDestinations: verify}, zap.NewNop()))
	return svc
}

// challengeServer answers challenges as answer says: "echo" (plain), "json",
// "wrong" or "status".
func challengeServer(t *testing.T, answer string, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var c challengePayload
		require.NoError(t, json.NewDecoder(r.Body).Decode(&c))
		assert.Equal(t, "url_verification", c.Type)
		assert.Equal(t, "url_verification", r.Header.Get("X-Event-Type"))
		switch answer {
		case "echo":
			_, _ = w.Write([]byte(c.Challenge))
		case "json":
			_ = json.NewEncoder(w).Encode(map[string]string{"challenge": c.Challenge})
		case "wrong":
			_, _ = w.Write([]byte("ok"))
		case "status":
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestWebhookDestinationsAnswerAChallenge: with verification on, a webhook
// URL is accepted only when it echoes the challenge (plain or as JSON);
// an update that keeps the URL sends no challenge, one that changes it
// does; with verification off no challenge is sent.
func TestWebhookDestinationsAnswerAChallenge(t *testing.T) {
	ctx := context.Background()
	for _, answer := range []string{"echo", "json"} {
		var calls atomic.Int32
		srv := challengeServer(t, answer, &calls)
		_, err := verifyingService(t, true).CreateSetting(ctx, &NotificationSetting{Name: answer, Type: NotificationTypeWebhook, Enabled: true,
			EventTypes: []EventType{EventTypeBlock}, Destination: Destination{WebhookURL: srv.URL, WebhookSecret: "s"}})
		require.NoError(t, err, answer)
		assert.Equal(t, int32(1), calls.Load())
	}
	for _, answer := range []string{"wrong", "status"} {
		var calls atomic.Int32
		srv := challengeServer(t, answer, &calls)
		_, err := verifyingService(t, true).CreateSetting(ctx, &NotificationSetting{Name: answer, Type: NotificationTypeWebhook, Enabled: true,
			EventTypes: []EventType{EventTypeBlock}, Destination: Destination{WebhookURL: srv.URL}})
		require.Error(t, err, answer)
		assert.Contains(t, err.Error(), "challenge")
	}

	var calls, otherCalls atomic.Int32
	srv, other := challengeServer(t, "echo", &calls), challengeServer(t, "echo", &otherCalls)
	svc := verifyingService(t, true)
	st, err := svc.CreateSetting(ctx, &NotificationSetting{Name: "u", Type: NotificationTypeWebhook, Enabled: true,
		EventTypes: []EventType{EventTypeBlock}, Destination: Destination{WebhookURL: srv.URL}})
	require.NoError(t, err)
	renamed := *st
	renamed.Name = "renamed"
	_, err = svc.UpdateSetting(ctx, &renamed)
	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load(), "the same URL is not challenged again")
	moved := renamed
	moved.Destination.WebhookURL = other.URL
	_, err = svc.UpdateSetting(ctx, &moved)
	require.NoError(t, err)
	assert.Equal(t, int32(1), otherCalls.Load(), "a new URL is challenged")

	var offCalls atomic.Int32
	off := challengeServer(t, "wrong", &offCalls)
	_, err = verifyingService(t, false).CreateSetting(ctx, &NotificationSetting{Name: "off", Type: NotificationTypeWebhook, Enabled: true,
		EventTypes: []EventType{EventTypeBlock}, Destination: Destination{WebhookURL: off.URL}})
	require.NoError(t, err)
	assert.Zero(t, offCalls.Load(), "verification off: no challenge")
}
