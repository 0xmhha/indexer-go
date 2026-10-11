package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"go.uber.org/zap"

	apimiddleware "github.com/0xmhha/indexer-go/pkg/api/middleware"
	"github.com/0xmhha/indexer-go/pkg/notifications"
)

// TestSubscriptionStreamStopsWithTheServer: a stream connection counts
// for its key while open, and stopping the server closes it (going away)
// and leaves no goroutine behind.
func TestSubscriptionStreamStopsWithTheServer(t *testing.T) {
	baseline := goleak.IgnoreCurrent()
	streams := notifications.NewStreams(1)
	cfg := DefaultConfig()
	cfg.EnableGraphQL = true
	cfg.APIKeys = map[string]string{"alice-key-0123456789abcdef01": "alice"}
	s, err := NewServerWithOptions(cfg, zap.NewNop(), &mockStorage{}, &ServerOptions{NotificationStreams: streams})
	require.NoError(t, err)
	web := httptest.NewServer(s.Router())

	url := "ws" + strings.TrimPrefix(web.URL, "http") + SubscriptionStreamPath
	conn, _, err := gws.DefaultDialer.Dial(url, http.Header{"X-API-Key": {"alice-key-0123456789abcdef01"}})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return streams.Connected("alice") == 1 }, 5*time.Second, 5*time.Millisecond)

	require.NoError(t, s.Stop(context.Background()))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, _, err = conn.ReadMessage()
	require.True(t, gws.IsCloseError(err, gws.CloseGoingAway), "closed as going away: %v", err)
	require.Zero(t, streams.Connected("alice"))
	_ = conn.Close()
	web.Close()
	goleak.VerifyNone(t, baseline)
}

// TestSubscriptionStreamRefusedWhileStopping: a connection asked for once
// the server is stopping is refused (503) and holds no stream.
func TestSubscriptionStreamRefusedWhileStopping(t *testing.T) {
	baseline := goleak.IgnoreCurrent()
	streams := notifications.NewStreams(1)
	h := newSubscriptionStreams(streams, func(*http.Request) bool { return true }, zap.NewNop())
	h.close()
	web := httptest.NewServer(apiKeyLabel("alice", h))
	url := "ws" + strings.TrimPrefix(web.URL, "http")
	_, resp, err := gws.DefaultDialer.Dial(url, nil)
	require.Error(t, err)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Zero(t, streams.Connected("alice"))
	web.Close()
	goleak.VerifyNone(t, baseline)
}

// TestSubscriptionStreamStopWaitsForHandlers: Stop waits for a handler
// that has begun but not yet registered its connection (the window a CI
// run hit: the stream was released after Stop returned), and that
// handler's stream is released by the time Stop returns.
func TestSubscriptionStreamStopWaitsForHandlers(t *testing.T) {
	streams := notifications.NewStreams(1)
	h := newSubscriptionStreams(streams, func(*http.Request) bool { return true }, zap.NewNop())
	require.True(t, h.begin(), "a handler has begun")
	st, err := streams.Open("alice")
	require.NoError(t, err)
	stopped := make(chan struct{})
	go func() { h.close(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("close returned while a handler was still running")
	case <-time.After(50 * time.Millisecond):
	}
	// The handler ends: it releases its stream, then stops counting.
	streams.Close(st)
	h.wg.Done()
	<-stopped
	require.Zero(t, streams.Connected("alice"))
}

// apiKeyLabel serves h with requests carrying an API key of label.
func apiKeyLabel(label string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(apimiddleware.WithAPIKeyLabel(r.Context(), label)))
	})
}
