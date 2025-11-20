package graphql

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/events"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

func TestNewSubscriptionServer(t *testing.T) {
	logger := zap.NewNop()
	eventBus := events.NewEventBus(100, 10)
	go eventBus.Run()
	defer eventBus.Stop()

	server := NewSubscriptionServer(eventBus, logger)
	if server == nil {
		t.Fatal("expected non-nil server")
	}

	if server.eventBus != eventBus {
		t.Error("expected eventBus to be set")
	}
}

func TestSubscriptionServer_SetEventBus(t *testing.T) {
	logger := zap.NewNop()
	server := NewSubscriptionServer(nil, logger)

	if server.eventBus != nil {
		t.Error("expected nil eventBus initially")
	}

	eventBus := events.NewEventBus(100, 10)
	go eventBus.Run()
	defer eventBus.Stop()

	server.SetEventBus(eventBus)

	if server.eventBus != eventBus {
		t.Error("expected eventBus to be set after SetEventBus")
	}
}

func TestSubscriptionServer_HandlerWithoutEventBus(t *testing.T) {
	logger := zap.NewNop()
	server := NewSubscriptionServer(nil, logger)

	req := httptest.NewRequest("GET", "/graphql/ws", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestSubscriptionServer_WebSocketConnection(t *testing.T) {
	logger := zap.NewNop()
	eventBus := events.NewEventBus(100, 10)
	go eventBus.Run()
	defer eventBus.Stop()

	server := NewSubscriptionServer(eventBus, logger)
	ts := httptest.NewServer(http.HandlerFunc(server.ServeHTTP))
	defer ts.Close()

	// Connect via WebSocket
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Send connection_init
	initMsg := wsMessage{Type: "connection_init"}
	if err := conn.WriteJSON(initMsg); err != nil {
		t.Fatalf("failed to send init: %v", err)
	}

	// Receive connection_ack
	var ackMsg wsMessage
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if err := conn.ReadJSON(&ackMsg); err != nil {
		t.Fatalf("failed to read ack: %v", err)
	}

	if ackMsg.Type != "connection_ack" {
		t.Errorf("expected connection_ack, got %s", ackMsg.Type)
	}
}

func TestSubscriptionServer_Subscribe(t *testing.T) {
	logger := zap.NewNop()
	eventBus := events.NewEventBus(100, 10)
	go eventBus.Run()
	defer eventBus.Stop()

	server := NewSubscriptionServer(eventBus, logger)
	ts := httptest.NewServer(http.HandlerFunc(server.ServeHTTP))
	defer ts.Close()

	// Connect via WebSocket
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Send connection_init
	initMsg := wsMessage{Type: "connection_init"}
	conn.WriteJSON(initMsg)

	// Read connection_ack
	var ackMsg wsMessage
	conn.SetReadDeadline(time.Now().Add(time.Second))
	conn.ReadJSON(&ackMsg)

	// Subscribe to newBlock
	payload, _ := json.Marshal(subscribePayload{
		Query: "subscription { newBlock { number hash } }",
	})
	subMsg := wsMessage{
		ID:      "1",
		Type:    "subscribe",
		Payload: payload,
	}
	if err := conn.WriteJSON(subMsg); err != nil {
		t.Fatalf("failed to send subscribe: %v", err)
	}

	// Small delay for subscription to be processed
	time.Sleep(50 * time.Millisecond)

	// Check that subscription was created
	if eventBus.SubscriberCount() != 1 {
		t.Errorf("expected 1 subscriber, got %d", eventBus.SubscriberCount())
	}
}

func TestSubscriptionServer_PingPong(t *testing.T) {
	logger := zap.NewNop()
	eventBus := events.NewEventBus(100, 10)
	go eventBus.Run()
	defer eventBus.Stop()

	server := NewSubscriptionServer(eventBus, logger)
	ts := httptest.NewServer(http.HandlerFunc(server.ServeHTTP))
	defer ts.Close()

	// Connect via WebSocket
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Send ping
	pingMsg := wsMessage{Type: "ping"}
	if err := conn.WriteJSON(pingMsg); err != nil {
		t.Fatalf("failed to send ping: %v", err)
	}

	// Receive pong
	var pongMsg wsMessage
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if err := conn.ReadJSON(&pongMsg); err != nil {
		t.Fatalf("failed to read pong: %v", err)
	}

	if pongMsg.Type != "pong" {
		t.Errorf("expected pong, got %s", pongMsg.Type)
	}
}

func TestParseSubscriptionType(t *testing.T) {
	logger := zap.NewNop()
	client := &subscriptionClient{logger: logger}

	tests := []struct {
		query    string
		expected string
	}{
		{"subscription { newBlock { number } }", "newBlock"},
		{"subscription { newTransaction { hash } }", "newTransaction"},
		{"subscription { unknown { field } }", ""},
		{"query { block { number } }", ""},
	}

	for _, tt := range tests {
		result := client.parseSubscriptionType(tt.query)
		if result != tt.expected {
			t.Errorf("parseSubscriptionType(%q) = %q, want %q", tt.query, result, tt.expected)
		}
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"hello world", "world", true},
		{"hello world", "foo", false},
		{"newBlock", "newBlock", true},
		{"subscription { newBlock }", "newBlock", true},
		{"", "test", false},
		{"test", "", true},
	}

	for _, tt := range tests {
		result := contains(tt.s, tt.substr)
		if result != tt.expected {
			t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, result, tt.expected)
		}
	}
}
