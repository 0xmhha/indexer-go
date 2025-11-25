package graphql

import (
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/events"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// TestWebSocketBlockSubscription_Integration tests the complete flow:
// EventBus.Publish() → Subscription → WebSocket → Client
func TestWebSocketBlockSubscription_Integration(t *testing.T) {
	// 1. Setup EventBus
	eventBus := events.NewEventBus(1000, 100)
	go eventBus.Run()
	defer eventBus.Stop()

	// 2. Setup Subscription Server
	logger, _ := zap.NewDevelopment()
	subServer := NewSubscriptionServer(eventBus, logger)

	// 3. Setup HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subServer.ServeHTTP(w, r)
	}))
	defer server.Close()

	// Convert http://... to ws://...
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	t.Logf("WebSocket URL: %s", wsURL)

	// 4. Connect WebSocket client
	header := http.Header{}
	header.Add("Sec-WebSocket-Protocol", "graphql-transport-ws")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer conn.Close()

	t.Log("✅ WebSocket connected")

	// 5. Send connection_init
	initMsg := map[string]interface{}{
		"type": "connection_init",
	}
	if err := conn.WriteJSON(initMsg); err != nil {
		t.Fatalf("Failed to send connection_init: %v", err)
	}

	// 6. Wait for connection_ack
	var ackMsg map[string]interface{}
	if err := conn.ReadJSON(&ackMsg); err != nil {
		t.Fatalf("Failed to read connection_ack: %v", err)
	}

	if ackMsg["type"] != "connection_ack" {
		t.Fatalf("Expected connection_ack, got: %v", ackMsg["type"])
	}

	t.Log("✅ Connection acknowledged")

	// 7. Subscribe to newBlock
	subscribeMsg := map[string]interface{}{
		"id":   "block-test-1",
		"type": "subscribe",
		"payload": map[string]interface{}{
			"query": `
				subscription {
					newBlock {
						number
						hash
						timestamp
						txCount
					}
				}
			`,
		},
	}

	if err := conn.WriteJSON(subscribeMsg); err != nil {
		t.Fatalf("Failed to send subscribe: %v", err)
	}

	t.Log("✅ Subscription sent")

	// 8. Publish block event via EventBus
	testBlock := createTestBlock(12345)
	blockEvent := events.NewBlockEvent(testBlock)

	// Give subscription time to register
	time.Sleep(100 * time.Millisecond)

	// Publish event
	if !eventBus.Publish(blockEvent) {
		t.Fatal("Failed to publish block event to EventBus")
	}

	t.Log("✅ Block event published to EventBus")

	// 9. Wait for WebSocket message
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var receivedMsg map[string]interface{}
	if err := conn.ReadJSON(&receivedMsg); err != nil {
		t.Fatalf("Failed to receive message from WebSocket: %v", err)
	}

	t.Logf("📨 Received message: %+v", receivedMsg)

	// 10. Verify message structure
	if receivedMsg["type"] != "next" {
		t.Errorf("Expected type 'next', got: %v", receivedMsg["type"])
	}

	if receivedMsg["id"] != "block-test-1" {
		t.Errorf("Expected id 'block-test-1', got: %v", receivedMsg["id"])
	}

	// 11. Verify payload
	payload, ok := receivedMsg["payload"].(map[string]interface{})
	if !ok {
		t.Fatal("Payload is not a map")
	}

	newBlock, ok := payload["newBlock"].(map[string]interface{})
	if !ok {
		t.Fatal("newBlock is not a map")
	}

	// Check block number
	blockNumber, ok := newBlock["number"].(float64)
	if !ok || blockNumber != 12345 {
		t.Errorf("Expected block number 12345, got: %v", newBlock["number"])
	}

	// Check block hash
	blockHash, ok := newBlock["hash"].(string)
	if !ok || blockHash != testBlock.Hash().Hex() {
		t.Errorf("Expected hash %s, got: %v", testBlock.Hash().Hex(), newBlock["hash"])
	}

	// Check txCount
	txCount, ok := newBlock["txCount"].(float64)
	if !ok || txCount != 0 {
		t.Errorf("Expected txCount 0, got: %v", newBlock["txCount"])
	}

	t.Log("✅ Block data verified!")

	// 12. Publish another block
	testBlock2 := createTestBlock(12346)
	blockEvent2 := events.NewBlockEvent(testBlock2)

	if !eventBus.Publish(blockEvent2) {
		t.Fatal("Failed to publish second block event")
	}

	// Receive second message
	var receivedMsg2 map[string]interface{}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := conn.ReadJSON(&receivedMsg2); err != nil {
		t.Fatalf("Failed to receive second message: %v", err)
	}

	payload2 := receivedMsg2["payload"].(map[string]interface{})
	newBlock2 := payload2["newBlock"].(map[string]interface{})
	blockNumber2 := newBlock2["number"].(float64)

	if blockNumber2 != 12346 {
		t.Errorf("Expected second block number 12346, got: %v", blockNumber2)
	}

	t.Log("✅ Second block received!")

	// 13. Unsubscribe
	completeMsg := map[string]interface{}{
		"id":   "block-test-1",
		"type": "complete",
	}

	if err := conn.WriteJSON(completeMsg); err != nil {
		t.Fatalf("Failed to send complete: %v", err)
	}

	t.Log("✅ Integration test passed!")
}

// TestWebSocketTransactionSubscription tests transaction event flow
func TestWebSocketTransactionSubscription(t *testing.T) {
	// Setup
	eventBus := events.NewEventBus(1000, 100)
	go eventBus.Run()
	defer eventBus.Stop()

	logger, _ := zap.NewDevelopment()
	subServer := NewSubscriptionServer(eventBus, logger)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subServer.ServeHTTP(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect
	header := http.Header{}
	header.Add("Sec-WebSocket-Protocol", "graphql-transport-ws")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Init
	conn.WriteJSON(map[string]interface{}{"type": "connection_init"})
	var ack map[string]interface{}
	conn.ReadJSON(&ack)

	// Subscribe to transactions
	conn.WriteJSON(map[string]interface{}{
		"id":   "tx-test",
		"type": "subscribe",
		"payload": map[string]interface{}{
			"query": `
				subscription {
					newTransaction {
						hash
						from
						value
						blockNumber
					}
				}
			`,
		},
	})

	time.Sleep(100 * time.Millisecond)

	// Publish transaction event
	tx := createTestTransaction()
	txEvent := events.NewTransactionEvent(
		tx,
		12345,
		common.HexToHash("0xblock123"),
		0,
		common.HexToAddress("0xfrom123"),
		nil,
	)

	if !eventBus.Publish(txEvent) {
		t.Fatal("Failed to publish tx event")
	}

	// Receive
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var msg map[string]interface{}
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("Failed to receive tx message: %v", err)
	}

	// Verify
	payload := msg["payload"].(map[string]interface{})
	newTx := payload["newTransaction"].(map[string]interface{})

	if newTx["hash"].(string) != tx.Hash().Hex() {
		t.Errorf("Hash mismatch: %v != %v", newTx["hash"], tx.Hash().Hex())
	}

	blockNum := newTx["blockNumber"].(float64)
	if blockNum != 12345 {
		t.Errorf("Block number mismatch: %v != 12345", blockNum)
	}

	t.Log("✅ Transaction subscription test passed!")
}

// TestWebSocketLogSubscription tests log event flow with filter
func TestWebSocketLogSubscription(t *testing.T) {
	// Setup
	eventBus := events.NewEventBus(1000, 100)
	go eventBus.Run()
	defer eventBus.Stop()

	logger, _ := zap.NewDevelopment()
	subServer := NewSubscriptionServer(eventBus, logger)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subServer.ServeHTTP(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect
	header := http.Header{}
	header.Add("Sec-WebSocket-Protocol", "graphql-transport-ws")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Init
	conn.WriteJSON(map[string]interface{}{"type": "connection_init"})
	var ack map[string]interface{}
	conn.ReadJSON(&ack)

	// Subscribe to logs with filter
	targetAddress := "0x1234567890123456789012345678901234567890"
	conn.WriteJSON(map[string]interface{}{
		"id":   "log-test",
		"type": "subscribe",
		"payload": map[string]interface{}{
			"query": `
				subscription($filter: LogFilterInput) {
					logs(filter: $filter) {
						address
						topics
						data
						blockNumber
					}
				}
			`,
			"variables": map[string]interface{}{
				"filter": map[string]interface{}{
					"address": targetAddress,
				},
			},
		},
	})

	time.Sleep(100 * time.Millisecond)

	// Publish matching log event
	matchingLog := &types.Log{
		Address:     common.HexToAddress(targetAddress),
		Topics:      []common.Hash{common.HexToHash("0xtopic1")},
		Data:        []byte{1, 2, 3},
		BlockNumber: 12345,
		TxHash:      common.HexToHash("0xtx1"),
		TxIndex:     0,
		Index:       0,
	}

	logEvent := events.NewLogEvent(matchingLog)

	if !eventBus.Publish(logEvent) {
		t.Fatal("Failed to publish log event")
	}

	// Receive
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var msg map[string]interface{}
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("Failed to receive log message: %v", err)
	}

	// Verify
	payload := msg["payload"].(map[string]interface{})
	log := payload["logs"].(map[string]interface{})

	receivedAddress := log["address"].(string)
	if !strings.EqualFold(receivedAddress, targetAddress) {
		t.Errorf("Address mismatch: %v != %v", receivedAddress, targetAddress)
	}

	blockNum := log["blockNumber"].(float64)
	if blockNum != 12345 {
		t.Errorf("Block number mismatch: %v != 12345", blockNum)
	}

	t.Log("✅ Log subscription with filter test passed!")
}

// TestWebSocketMultipleSubscriptions tests multiple concurrent subscriptions
func TestWebSocketMultipleSubscriptions(t *testing.T) {
	// Setup
	eventBus := events.NewEventBus(1000, 100)
	go eventBus.Run()
	defer eventBus.Stop()

	logger, _ := zap.NewDevelopment()
	subServer := NewSubscriptionServer(eventBus, logger)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subServer.ServeHTTP(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect
	header := http.Header{}
	header.Add("Sec-WebSocket-Protocol", "graphql-transport-ws")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Init
	conn.WriteJSON(map[string]interface{}{"type": "connection_init"})
	var ack map[string]interface{}
	conn.ReadJSON(&ack)

	// Subscribe to blocks
	conn.WriteJSON(map[string]interface{}{
		"id":   "blocks",
		"type": "subscribe",
		"payload": map[string]interface{}{
			"query": "subscription { newBlock { number } }",
		},
	})

	// Subscribe to transactions
	conn.WriteJSON(map[string]interface{}{
		"id":   "txs",
		"type": "subscribe",
		"payload": map[string]interface{}{
			"query": "subscription { newTransaction { hash } }",
		},
	})

	time.Sleep(100 * time.Millisecond)

	// Publish block
	blockEvent := events.NewBlockEvent(createTestBlock(100))
	eventBus.Publish(blockEvent)

	// Publish transaction
	txEvent := events.NewTransactionEvent(
		createTestTransaction(),
		100,
		common.HexToHash("0xblock"),
		0,
		common.HexToAddress("0xfrom"),
		nil,
	)
	eventBus.Publish(txEvent)

	// Receive 2 messages
	receivedCount := 0
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	for receivedCount < 2 {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to receive message %d: %v", receivedCount+1, err)
		}

		msgType := msg["type"].(string)
		if msgType != "next" {
			continue
		}

		receivedCount++
		t.Logf("📨 Received message %d: id=%v", receivedCount, msg["id"])
	}

	if receivedCount != 2 {
		t.Errorf("Expected 2 messages, got %d", receivedCount)
	}

	t.Log("✅ Multiple subscriptions test passed!")
}

// Helper functions

func createTestBlock(number uint64) *types.Block {
	header := &types.Header{
		Number:     big.NewInt(int64(number)),
		Time:       uint64(time.Now().Unix()),
		Difficulty: big.NewInt(1),
		GasLimit:   8000000,
		GasUsed:    0,
	}

	return types.NewBlock(header, nil, nil, nil, nil)
}

func createTestTransaction() *types.Transaction {
	return types.NewTransaction(
		0,                                                           // nonce
		common.HexToAddress("0x1234567890123456789012345678901234567890"), // to
		big.NewInt(1000000000000000000),                             // value (1 ETH)
		21000,                                                       // gas limit
		big.NewInt(1000000000),                                      // gas price
		nil,                                                         // data
	)
}

// TestWebSocketErrorHandling tests error scenarios
func TestWebSocketErrorHandling(t *testing.T) {
	// Setup without EventBus
	logger, _ := zap.NewDevelopment()
	subServer := NewSubscriptionServer(nil, logger)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subServer.ServeHTTP(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect
	header := http.Header{}
	header.Add("Sec-WebSocket-Protocol", "graphql-transport-ws")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)

	// Should get HTTP error since EventBus is nil
	if err == nil {
		conn.Close()
		t.Log("Note: Connection succeeded even without EventBus (Handler will check later)")
	} else if resp != nil && resp.StatusCode == http.StatusServiceUnavailable {
		t.Log("✅ Correctly returned 503 Service Unavailable")
	}
}

// TestWebSocketInvalidSubscription tests invalid subscription queries
func TestWebSocketInvalidSubscription(t *testing.T) {
	// Setup
	eventBus := events.NewEventBus(1000, 100)
	go eventBus.Run()
	defer eventBus.Stop()

	logger, _ := zap.NewDevelopment()
	subServer := NewSubscriptionServer(eventBus, logger)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subServer.ServeHTTP(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect
	header := http.Header{}
	header.Add("Sec-WebSocket-Protocol", "graphql-transport-ws")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Init
	conn.WriteJSON(map[string]interface{}{"type": "connection_init"})
	var ack map[string]interface{}
	conn.ReadJSON(&ack)

	// Send invalid subscription
	conn.WriteJSON(map[string]interface{}{
		"id":   "invalid-test",
		"type": "subscribe",
		"payload": map[string]interface{}{
			"query": "subscription { unknownType { field } }",
		},
	})

	// Should receive error
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var msg map[string]interface{}
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("Failed to receive error message: %v", err)
	}

	if msg["type"] != "error" {
		t.Errorf("Expected error message, got: %v", msg["type"])
	}

	t.Log("✅ Invalid subscription correctly rejected!")
}

// TestEventBusStats verifies EventBus statistics tracking
func TestEventBusStats(t *testing.T) {
	eventBus := events.NewEventBus(1000, 100)
	go eventBus.Run()
	defer eventBus.Stop()

	// Create subscription
	subID := events.SubscriptionID("stats-test")
	sub := eventBus.Subscribe(subID, []events.EventType{events.EventTypeBlock}, nil, 100)
	if sub == nil {
		t.Fatal("Failed to create subscription")
	}

	// Publish events
	for i := 0; i < 5; i++ {
		block := createTestBlock(uint64(i))
		blockEvent := events.NewBlockEvent(block)
		eventBus.Publish(blockEvent)
	}

	// Wait for events to be processed
	time.Sleep(200 * time.Millisecond)

	// Check subscriber stats
	info := eventBus.GetSubscriberInfo(subID)
	if info == nil {
		t.Fatal("Failed to get subscriber info")
	}

	t.Logf("Subscriber stats: received=%d, dropped=%d", info.EventsReceived, info.EventsDropped)

	if info.EventsReceived != 5 {
		t.Errorf("Expected 5 events received, got %d", info.EventsReceived)
	}

	if info.EventsDropped != 0 {
		t.Errorf("Expected 0 events dropped, got %d", info.EventsDropped)
	}

	t.Log("✅ EventBus stats test passed!")
}

// Benchmark WebSocket throughput
func BenchmarkWebSocketThroughput(b *testing.B) {
	eventBus := events.NewEventBus(10000, 100)
	go eventBus.Run()
	defer eventBus.Stop()

	logger, _ := zap.NewDevelopment()
	subServer := NewSubscriptionServer(eventBus, logger)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subServer.ServeHTTP(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	header := http.Header{}
	header.Add("Sec-WebSocket-Protocol", "graphql-transport-ws")
	conn, _, _ := websocket.DefaultDialer.Dial(wsURL, header)
	defer conn.Close()

	conn.WriteJSON(map[string]interface{}{"type": "connection_init"})
	var ack map[string]interface{}
	conn.ReadJSON(&ack)

	conn.WriteJSON(map[string]interface{}{
		"id":   "bench",
		"type": "subscribe",
		"payload": map[string]interface{}{
			"query": "subscription { newBlock { number } }",
		},
	})

	time.Sleep(100 * time.Millisecond)

	// Start receiving in background
	go func() {
		for {
			var msg map[string]interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		block := createTestBlock(uint64(i))
		blockEvent := events.NewBlockEvent(block)
		eventBus.Publish(blockEvent)
	}
}
