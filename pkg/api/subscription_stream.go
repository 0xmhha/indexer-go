package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	gws "github.com/gorilla/websocket"
	"go.uber.org/zap"

	apimiddleware "github.com/0xmhha/indexer-go/pkg/api/middleware"
	"github.com/0xmhha/indexer-go/pkg/notifications"
)

// SubscriptionStreamPath is where a key's stream connection is served:
// the notifications of its stream settings, and lagging messages for its
// fast settings, one JSON text message each (notifications.StreamMessage).
const SubscriptionStreamPath = "/v1/subscriptions/stream"

const (
	streamWriteWait = 10 * time.Second
	streamPongWait  = 60 * time.Second
	streamPing      = 30 * time.Second
	// streamSlowClose is the close reason of a connection that fell more
	// than its buffer behind (close code 1008, as for GraphQL
	// subscriptions).
	streamSlowClose = "SLOW_SUBSCRIBER"
)

// subscriptionStreams serves stream connections and closes them when the
// server stops.
type subscriptionStreams struct {
	streams  *notifications.Streams
	upgrader gws.Upgrader
	logger   *zap.Logger

	mu     sync.Mutex
	conns  map[*gws.Conn]struct{}
	closed bool
	wg     sync.WaitGroup
}

func newSubscriptionStreams(streams *notifications.Streams, checkOrigin func(*http.Request) bool, logger *zap.Logger) *subscriptionStreams {
	return &subscriptionStreams{
		streams:  streams,
		upgrader: gws.Upgrader{CheckOrigin: checkOrigin},
		logger:   logger.Named("subscription-stream"),
		conns:    map[*gws.Conn]struct{}{},
	}
}

func writeStreamError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "error": message})
}

// ServeHTTP upgrades a request with an API key to a stream connection of
// the key's label. Without a key it answers 401, over the key's
// connection limit 429.
func (h *subscriptionStreams) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	label, _ := apimiddleware.APIKeyFromContext(r.Context())
	if label == "" {
		writeStreamError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "an API key (api.keys) is required")
		return
	}
	st, err := h.streams.Open(label)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, notifications.ErrTooManyStreams) {
			status = http.StatusTooManyRequests
		}
		writeStreamError(w, status, "STREAM_REFUSED", err.Error())
		return
	}
	defer h.streams.Close(st)
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // the upgrader answered the request
	}
	if !h.track(conn) {
		_ = conn.Close()
		return
	}
	defer h.untrack(conn)
	h.serve(conn, st)
}

// track records conn so close can end it; false once the server stopped.
func (h *subscriptionStreams) track(conn *gws.Conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.conns[conn] = struct{}{}
	h.wg.Add(1)
	return true
}

func (h *subscriptionStreams) untrack(conn *gws.Conn) {
	h.mu.Lock()
	delete(h.conns, conn)
	h.mu.Unlock()
	_ = conn.Close()
	h.wg.Done()
}

// serve writes the stream's messages until the client goes away, the
// stream ends or the server closes the connection. Messages from the
// client are read and ignored (they keep the pong deadline).
func (h *subscriptionStreams) serve(conn *gws.Conn, st *notifications.Stream) {
	gone := make(chan struct{})
	_ = conn.SetReadDeadline(time.Now().Add(streamPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(streamPongWait))
	})
	go func() {
		defer close(gone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	defer func() { _ = conn.Close(); <-gone }()

	ping := time.NewTicker(streamPing)
	defer ping.Stop()
	for {
		select {
		case msg := <-st.Messages():
			_ = conn.SetWriteDeadline(time.Now().Add(streamWriteWait))
			if err := conn.WriteMessage(gws.TextMessage, msg); err != nil {
				return
			}
		case <-ping.C:
			if err := conn.WriteControl(gws.PingMessage, nil, time.Now().Add(streamWriteWait)); err != nil {
				return
			}
		case <-st.Done():
			if st.Overflowed() {
				h.logger.Warn("stream connection fell behind and was closed", zap.String("owner", st.Owner()))
				_ = conn.WriteControl(gws.CloseMessage, gws.FormatCloseMessage(gws.ClosePolicyViolation, streamSlowClose), time.Now().Add(streamWriteWait))
			}
			return
		case <-gone:
			return
		}
	}
}

// close ends every connection and waits for their handlers.
func (h *subscriptionStreams) close() {
	h.mu.Lock()
	h.closed = true
	for conn := range h.conns {
		_ = conn.WriteControl(gws.CloseMessage, gws.FormatCloseMessage(gws.CloseGoingAway, "server stopping"), time.Now().Add(time.Second))
		_ = conn.Close()
	}
	h.mu.Unlock()
	h.wg.Wait()
}
