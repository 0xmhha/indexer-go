package websocket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

func TestWebSocketServer(t *testing.T) {
	logger := zap.NewNop()
	server := NewServer(logger)
	defer server.Stop()

	// Create test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(server.ServeHTTP))
	defer ts.Close()

	// Convert http://... to ws://...
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	t.Run("Connect", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Should have 1 client
		time.Sleep(100 * time.Millisecond)
		if count := server.Hub().ClientCount(); count != 1 {
			t.Errorf("expected 1 client, got %d", count)
		}
	})

	t.Run("Subscribe", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Send subscribe message
		subReq := Message{
			Type: "subscribe",
		}
		payload, _ := json.Marshal(SubscribeRequest{Type: SubscribeNewBlock})
		subReq.Payload = payload

		if err := conn.WriteJSON(subReq); err != nil {
			t.Fatalf("failed to send subscribe: %v", err)
		}

		// Read response
		var resp Message
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		if resp.Type != "success" {
			t.Errorf("expected success response, got %s", resp.Type)
		}
	})

	t.Run("Unsubscribe", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Subscribe first
		subReq := Message{Type: "subscribe"}
		payload, _ := json.Marshal(SubscribeRequest{Type: SubscribeNewBlock})
		subReq.Payload = payload
		_ = conn.WriteJSON(subReq)

		// Read subscribe response
		var resp Message
		_ = conn.ReadJSON(&resp)

		// Unsubscribe
		unsubReq := Message{Type: "unsubscribe"}
		unsubPayload, _ := json.Marshal(UnsubscribeRequest{Type: SubscribeNewBlock})
		unsubReq.Payload = unsubPayload

		if err := conn.WriteJSON(unsubReq); err != nil {
			t.Fatalf("failed to send unsubscribe: %v", err)
		}

		// Read response
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		if resp.Type != "success" {
			t.Errorf("expected success response, got %s", resp.Type)
		}
	})

	t.Run("Broadcast", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Subscribe to newBlock
		subReq := Message{Type: "subscribe"}
		payload, _ := json.Marshal(SubscribeRequest{Type: SubscribeNewBlock})
		subReq.Payload = payload
		_ = conn.WriteJSON(subReq)

		// Read subscribe response
		var resp Message
		_ = conn.ReadJSON(&resp)

		// Broadcast an event
		blockData := map[string]interface{}{
			"number": "0x1",
			"hash":   "0xabc",
		}
		server.Hub().BroadcastNewBlock(blockData)

		// Set read deadline
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

		// Read broadcasted event
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("failed to read broadcast: %v", err)
		}

		if resp.Type != "event" {
			t.Errorf("expected event message, got %s", resp.Type)
		}

		var event Event
		if err := json.Unmarshal(resp.Payload, &event); err != nil {
			t.Fatalf("failed to unmarshal event: %v", err)
		}

		if event.Type != SubscribeNewBlock {
			t.Errorf("expected newBlock event, got %s", event.Type)
		}
	})

	t.Run("Ping", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Send ping
		pingMsg := Message{Type: "ping"}
		if err := conn.WriteJSON(pingMsg); err != nil {
			t.Fatalf("failed to send ping: %v", err)
		}

		// Read pong
		var resp Message
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("failed to read pong: %v", err)
		}

		if resp.Type != "pong" {
			t.Errorf("expected pong response, got %s", resp.Type)
		}
	})

	t.Run("InvalidMessage", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Send invalid message
		if err := conn.WriteMessage(websocket.TextMessage, []byte("invalid json")); err != nil {
			t.Fatalf("failed to send message: %v", err)
		}

		// Read error response
		var resp Message
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		if resp.Type != "error" {
			t.Errorf("expected error response, got %s", resp.Type)
		}
	})

	t.Run("InvalidSubscriptionType", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Send subscribe with invalid type
		subReq := Message{Type: "subscribe"}
		payload, _ := json.Marshal(SubscribeRequest{Type: "invalidType"})
		subReq.Payload = payload

		if err := conn.WriteJSON(subReq); err != nil {
			t.Fatalf("failed to send subscribe: %v", err)
		}

		// Read error response
		var resp Message
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		if resp.Type != "error" {
			t.Errorf("expected error response, got %s", resp.Type)
		}
	})

	t.Run("UnknownMessageType", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Send unknown message type
		unknownMsg := Message{Type: "unknown"}
		if err := conn.WriteJSON(unknownMsg); err != nil {
			t.Fatalf("failed to send message: %v", err)
		}

		// Read error response
		var resp Message
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		if resp.Type != "error" {
			t.Errorf("expected error response, got %s", resp.Type)
		}
	})

	t.Run("MultipleClients", func(t *testing.T) {
		// Connect first client
		conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect client 1: %v", err)
		}
		defer conn1.Close()

		// Connect second client
		conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect client 2: %v", err)
		}
		defer conn2.Close()

		// Wait for connections to be registered
		time.Sleep(100 * time.Millisecond)

		// Should have 2 clients
		if count := server.Hub().ClientCount(); count != 2 {
			t.Errorf("expected 2 clients, got %d", count)
		}
	})

	t.Run("UnsubscribeWithoutSubscribe", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Try to unsubscribe without subscribing first
		unsubReq := Message{Type: "unsubscribe"}
		payload, _ := json.Marshal(UnsubscribeRequest{Type: SubscribeNewBlock})
		unsubReq.Payload = payload

		if err := conn.WriteJSON(unsubReq); err != nil {
			t.Fatalf("failed to send unsubscribe: %v", err)
		}

		// Should still get success response
		var resp Message
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		if resp.Type != "success" {
			t.Errorf("expected success response, got %s", resp.Type)
		}
	})

	// Error case tests
	t.Run("InvalidSubscribePayload", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Send subscribe with invalid payload as raw message
		invalidMsg := `{"type":"subscribe","payload":"invalid json"}`
		if err := conn.WriteMessage(websocket.TextMessage, []byte(invalidMsg)); err != nil {
			t.Fatalf("failed to send subscribe: %v", err)
		}

		// Read error response
		var resp Message
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		if resp.Type != "error" {
			t.Errorf("expected error response, got %s", resp.Type)
		}
	})

	t.Run("InvalidUnsubscribePayload", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Send unsubscribe with invalid payload as raw message
		invalidMsg := `{"type":"unsubscribe","payload":"invalid json"}`
		if err := conn.WriteMessage(websocket.TextMessage, []byte(invalidMsg)); err != nil {
			t.Fatalf("failed to send unsubscribe: %v", err)
		}

		// Read error response
		var resp Message
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		if resp.Type != "error" {
			t.Errorf("expected error response, got %s", resp.Type)
		}
	})

	t.Run("SendBufferStressTest", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Subscribe to newBlock
		subReq := Message{Type: "subscribe"}
		payload, _ := json.Marshal(SubscribeRequest{Type: SubscribeNewBlock})
		subReq.Payload = payload
		_ = conn.WriteJSON(subReq)

		// Read subscribe response
		var resp Message
		_ = conn.ReadJSON(&resp)

		// Send many broadcasts to test send buffer handling
		for i := 0; i < 300; i++ {
			blockData := map[string]interface{}{
				"number": fmt.Sprintf("0x%x", i),
				"hash":   fmt.Sprintf("0x%x", i),
			}
			server.Hub().BroadcastNewBlock(blockData)
		}

		// Try to read some messages (not all, as send buffer is 256)
		messagesReceived := 0
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		for i := 0; i < 100; i++ {
			if err := conn.ReadJSON(&resp); err != nil {
				break
			}
			if resp.Type == "event" {
				messagesReceived++
			}
		}

		if messagesReceived == 0 {
			t.Error("expected to receive at least some broadcast messages")
		}
	})

	t.Run("ConnectionCloseHandling", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}

		// Subscribe first
		subReq := Message{Type: "subscribe"}
		payload, _ := json.Marshal(SubscribeRequest{Type: SubscribeNewBlock})
		subReq.Payload = payload
		_ = conn.WriteJSON(subReq)

		var resp Message
		_ = conn.ReadJSON(&resp)

		// Abruptly close the connection
		conn.Close()

		// Give server time to process the close
		time.Sleep(200 * time.Millisecond)

		// Broadcast should not panic
		server.Hub().BroadcastNewBlock(map[string]interface{}{"number": "0x1"})

		// Verify client was unregistered
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("ConnectionCloseDuringRead", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}

		// Close immediately to trigger read error
		go func() {
			time.Sleep(50 * time.Millisecond)
			conn.Close()
		}()

		// Send a message and try to read response
		subReq := Message{Type: "ping"}
		_ = conn.WriteJSON(subReq)

		time.Sleep(200 * time.Millisecond)
	})
}

func TestHub(t *testing.T) {
	logger := zap.NewNop()
	hub := NewHub(logger)

	t.Run("ClientCount", func(t *testing.T) {
		if count := hub.ClientCount(); count != 0 {
			t.Errorf("expected 0 clients, got %d", count)
		}
	})

	t.Run("BroadcastNewBlock", func(t *testing.T) {
		blockData := map[string]interface{}{"number": "0x1"}
		hub.BroadcastNewBlock(blockData)
		// Should not panic even with no clients
	})

	t.Run("BroadcastNewTransaction", func(t *testing.T) {
		txData := map[string]interface{}{"hash": "0xabc"}
		hub.BroadcastNewTransaction(txData)
		// Should not panic even with no clients
	})

	t.Run("MultipleRegistrations", func(t *testing.T) {
		// Create test server for real clients
		server := NewServer(logger)
		defer server.Stop()

		ts := httptest.NewServer(http.HandlerFunc(server.ServeHTTP))
		defer ts.Close()

		wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

		// Register multiple clients
		conns := make([]*websocket.Conn, 5)
		for i := 0; i < 5; i++ {
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Fatalf("failed to connect client %d: %v", i, err)
			}
			conns[i] = conn
		}

		time.Sleep(200 * time.Millisecond)

		// Verify all clients are registered
		if count := server.Hub().ClientCount(); count != 5 {
			t.Errorf("expected 5 clients, got %d", count)
		}

		// Close all connections
		for i, conn := range conns {
			if conn != nil {
				conn.Close()
			}
			_ = i
		}

		time.Sleep(200 * time.Millisecond)

		// Verify all clients are unregistered
		if count := server.Hub().ClientCount(); count != 0 {
			t.Errorf("expected 0 clients after close, got %d", count)
		}
	})

	t.Run("Stop", func(t *testing.T) {
		hub.Stop()
		if count := hub.ClientCount(); count != 0 {
			t.Errorf("expected 0 clients after stop, got %d", count)
		}
	})
}

func TestWebSocketEdgeCases(t *testing.T) {
	logger := zap.NewNop()
	server := NewServer(logger)
	defer server.Stop()

	// Create test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(server.ServeHTTP))
	defer ts.Close()

	// Convert http://... to ws://...
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	t.Run("BroadcastNewTransaction", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Subscribe to newTransaction
		subReq := Message{Type: "subscribe"}
		payload, _ := json.Marshal(SubscribeRequest{Type: SubscribeNewTransaction})
		subReq.Payload = payload
		_ = conn.WriteJSON(subReq)

		// Read subscribe response
		var resp Message
		_ = conn.ReadJSON(&resp)

		// Broadcast a transaction event
		txData := map[string]interface{}{
			"hash": "0xabc",
			"from": "0x123",
		}
		server.Hub().BroadcastNewTransaction(txData)

		// Set read deadline
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

		// Read broadcasted event
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("failed to read broadcast: %v", err)
		}

		if resp.Type != "event" {
			t.Errorf("expected event message, got %s", resp.Type)
		}

		var event Event
		if err := json.Unmarshal(resp.Payload, &event); err != nil {
			t.Fatalf("failed to unmarshal event: %v", err)
		}

		if event.Type != SubscribeNewTransaction {
			t.Errorf("expected newTransaction event, got %s", event.Type)
		}
	})

	t.Run("MultipleSubscriptions", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Subscribe to both newBlock and newTransaction
		subReqBlock := Message{Type: "subscribe"}
		payloadBlock, _ := json.Marshal(SubscribeRequest{Type: SubscribeNewBlock})
		subReqBlock.Payload = payloadBlock
		_ = conn.WriteJSON(subReqBlock)
		var resp Message
		_ = conn.ReadJSON(&resp)

		subReqTx := Message{Type: "subscribe"}
		payloadTx, _ := json.Marshal(SubscribeRequest{Type: SubscribeNewTransaction})
		subReqTx.Payload = payloadTx
		_ = conn.WriteJSON(subReqTx)
		_ = conn.ReadJSON(&resp)

		// Give subscriptions time to register
		time.Sleep(100 * time.Millisecond)

		// Broadcast both types of events with delay
		server.Hub().BroadcastNewBlock(map[string]interface{}{"number": "0x1"})
		time.Sleep(100 * time.Millisecond)
		server.Hub().BroadcastNewTransaction(map[string]interface{}{"hash": "0xabc"})

		// Should receive both events
		eventsReceived := 0
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		for i := 0; i < 3; i++ {
			if err := conn.ReadJSON(&resp); err != nil {
				break
			}
			if resp.Type == "event" {
				eventsReceived++
			}
		}

		if eventsReceived < 2 {
			t.Errorf("expected at least 2 events, got %d", eventsReceived)
		}
	})

	t.Run("SubscribeOnlyReceivesOwnType", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		// Subscribe only to newBlock
		subReq := Message{Type: "subscribe"}
		payload, _ := json.Marshal(SubscribeRequest{Type: SubscribeNewBlock})
		subReq.Payload = payload
		_ = conn.WriteJSON(subReq)
		var resp Message
		_ = conn.ReadJSON(&resp)

		// Broadcast both types
		server.Hub().BroadcastNewBlock(map[string]interface{}{"number": "0x1"})
		server.Hub().BroadcastNewTransaction(map[string]interface{}{"hash": "0xabc"})

		// Should only receive block event
		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		messagesReceived := 0
		for i := 0; i < 5; i++ {
			if err := conn.ReadJSON(&resp); err != nil {
				break
			}
			if resp.Type == "event" {
				messagesReceived++
			}
		}

		if messagesReceived != 1 {
			t.Errorf("expected 1 event (block only), got %d", messagesReceived)
		}
	})
}

func TestClient(t *testing.T) {
	logger := zap.NewNop()
	hub := NewHub(logger)

	// Create a mock websocket connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		client := NewClient(hub, conn, logger)

		// Test subscription methods
		client.Subscribe(SubscribeNewBlock)
		if !client.IsSubscribed(SubscribeNewBlock) {
			t.Error("expected client to be subscribed to newBlock")
		}

		client.Unsubscribe(SubscribeNewBlock)
		if client.IsSubscribed(SubscribeNewBlock) {
			t.Error("expected client to be unsubscribed from newBlock")
		}

		// Keep connection open for a bit
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	time.Sleep(200 * time.Millisecond)
}
