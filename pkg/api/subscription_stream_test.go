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
