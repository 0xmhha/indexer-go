package graphql

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/events"
	"github.com/ethereum/go-ethereum/common"
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
		{"subscription { newPendingTransactions { hash } }", "newPendingTransactions"},
		{"subscription { logs { address } }", "logs"},
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

func TestBuildLogFilter(t *testing.T) {
	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	raw := map[string]interface{}{
		"address":   addr1.Hex(),
		"addresses": []interface{}{addr2.Hex()},
		"topics": []interface{}{
			"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			[]interface{}{"0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			nil,
		},
		"fromBlock": float64(10),
		"toBlock":   "0x20",
	}

	filter, err := buildLogFilter(raw)
	if err != nil {
		t.Fatalf("buildLogFilter returned error: %v", err)
	}
	if len(filter.Addresses) != 2 {
		t.Fatalf("expected 2 addresses, got %d", len(filter.Addresses))
	}
	if filter.Addresses[0] != addr1 || filter.Addresses[1] != addr2 {
		t.Errorf("addresses mismatch: %+v", filter.Addresses)
	}
	if len(filter.Topics) != 3 {
		t.Fatalf("expected 3 topic entries, got %d", len(filter.Topics))
	}
	if filter.FromBlock != 10 || filter.ToBlock != 32 {
		t.Errorf("unexpected block range %d-%d", filter.FromBlock, filter.ToBlock)
	}
}

func TestBuildLogFilter_Invalid(t *testing.T) {
	_, err := buildLogFilter(map[string]interface{}{"address": 123})
	if err == nil {
		t.Fatal("expected error for invalid address type")
	}

	_, err = buildLogFilter(map[string]interface{}{"topics": "not-array"})
	if err == nil {
		t.Fatal("expected error for invalid topics")
	}

	filter, err := buildLogFilter(nil)
	if err != nil {
		t.Fatalf("unexpected error for nil filter: %v", err)
	}
	if filter != nil {
		t.Fatalf("expected nil filter, got %+v", filter)
	}
}
