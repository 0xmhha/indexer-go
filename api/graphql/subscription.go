package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/events"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	// WebSocket configuration
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
)

// SubscriptionServer handles GraphQL subscriptions over WebSocket
type SubscriptionServer struct {
	eventBus        *events.EventBus
	logger          *zap.Logger
	upgrader        websocket.Upgrader
	enableKeepAlive bool
}

// NewSubscriptionServer creates a new subscription server
func NewSubscriptionServer(eventBus *events.EventBus, logger *zap.Logger, enableKeepAlive bool) *SubscriptionServer {
	return &SubscriptionServer{
		eventBus:        eventBus,
		logger:          logger,
		enableKeepAlive: enableKeepAlive,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			Subprotocols:    []string{"graphql-transport-ws", "graphql-ws"}, // Support both protocols
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for development
			},
		},
	}
}

// ServeHTTP handles WebSocket connections for GraphQL subscriptions
func (s *SubscriptionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.logger.Info("WebSocket connection request received",
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("origin", r.Header.Get("Origin")),
		zap.String("protocol", r.Header.Get("Sec-WebSocket-Protocol")),
	)

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("failed to upgrade connection",
			zap.Error(err),
			zap.String("remote_addr", r.RemoteAddr),
		)
		return
	}

	s.logger.Info("WebSocket connection established",
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("subprotocol", conn.Subprotocol()),
	)

	ctx, cancel := context.WithCancel(context.Background())
	client := &subscriptionClient{
		server:          s,
		conn:            conn,
		send:            make(chan []byte, 256),
		subscriptions:   make(map[string]*clientSubscription),
		logger:          s.logger,
		ctx:             ctx,
		cancel:          cancel,
		enableKeepAlive: s.enableKeepAlive,
	}

	go client.writePump()
	go client.readPump()
}

// subscriptionClient represents a WebSocket client for subscriptions
type subscriptionClient struct {
	server          *SubscriptionServer
	conn            *websocket.Conn
	send            chan []byte
	subscriptions   map[string]*clientSubscription // id -> subscription
	mu              sync.RWMutex
	logger          *zap.Logger
	ctx             context.Context
	cancel          context.CancelFunc
	enableKeepAlive bool
}

// clientSubscription holds subscription state
type clientSubscription struct {
	id         string
	subType    string
	eventSub   *events.Subscription
	cancelFunc context.CancelFunc
}

// GraphQL over WebSocket protocol messages
type wsMessage struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type subscribePayload struct {
	Query         string                 `json:"query"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
	OperationName string                 `json:"operationName,omitempty"`
}

// readPump reads messages from the WebSocket connection
func (c *subscriptionClient) readPump() {
	defer func() {
		c.logger.Info("WebSocket connection closing")
		c.cleanup()
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.logger.Debug("received pong message")
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Error("websocket read error", zap.Error(err))
			} else {
				c.logger.Info("websocket connection closed", zap.Error(err))
			}
			break
		}

		c.logger.Debug("received message", zap.String("message", string(message)))
		c.handleMessage(message)
	}
}

// writePump writes messages to the WebSocket connection
func (c *subscriptionClient) writePump() {
	var ticker *time.Ticker
	if c.enableKeepAlive {
		ticker = time.NewTicker(pingPeriod)
		c.logger.Debug("WebSocket keep-alive enabled",
			zap.Duration("ping_period", pingPeriod),
			zap.Duration("pong_wait", pongWait))
	}

	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-func() <-chan time.Time {
			if c.enableKeepAlive && ticker != nil {
				return ticker.C
			}
			// Return a channel that never sends if keep-alive is disabled
			return make(<-chan time.Time)
		}():
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.logger.Debug("ping failed", zap.Error(err))
				return
			}
			c.logger.Debug("sent ping message")
		}
	}
}

// handleMessage processes incoming WebSocket messages
func (c *subscriptionClient) handleMessage(data []byte) {
	var msg wsMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		c.logger.Error("failed to unmarshal message",
			zap.Error(err),
			zap.String("raw_message", string(data)),
		)
		return
	}

	c.logger.Info("handling WebSocket message",
		zap.String("type", msg.Type),
		zap.String("id", msg.ID),
	)

	switch msg.Type {
	case "connection_init":
		c.logger.Info("received connection_init, sending connection_ack")
		c.sendMessage(wsMessage{Type: "connection_ack"})

	case "subscribe":
		c.logger.Info("received subscribe request", zap.String("id", msg.ID))
		c.handleSubscribe(msg.ID, msg.Payload)

	case "complete":
		c.logger.Info("received complete request", zap.String("id", msg.ID))
		c.handleComplete(msg.ID)

	case "ping":
		c.logger.Debug("received ping, sending pong")
		c.sendMessage(wsMessage{Type: "pong"})

	default:
		c.logger.Warn("unknown message type",
			zap.String("type", msg.Type),
			zap.String("raw_message", string(data)),
		)
	}
}

// handleSubscribe handles subscription requests
func (c *subscriptionClient) handleSubscribe(id string, payload json.RawMessage) {
	c.logger.Info("processing subscribe request",
		zap.String("id", id),
		zap.String("payload", string(payload)),
	)

	var sub subscribePayload
	if err := json.Unmarshal(payload, &sub); err != nil {
		c.logger.Error("failed to parse subscription payload",
			zap.String("id", id),
			zap.Error(err),
		)
		c.sendError(id, "invalid payload")
		return
	}

	// Parse the subscription query to determine type
	subType := c.parseSubscriptionType(sub.Query)
	c.logger.Info("parsed subscription type",
		zap.String("id", id),
		zap.String("type", subType),
		zap.String("query", sub.Query),
	)

	if subType == "" {
		c.logger.Warn("unknown subscription type",
			zap.String("id", id),
			zap.String("query", sub.Query),
		)
		c.sendError(id, "invalid subscription query")
		return
	}

	// Subscribe to EventBus
	if c.server.eventBus == nil {
		c.logger.Error("EventBus not available",
			zap.String("id", id),
			zap.String("type", subType),
		)
		c.sendError(id, "event bus not available")
		return
	}

	var (
		eventType events.EventType
		filter    *events.Filter
		err       error
	)
	switch subType {
	case "newBlock":
		eventType = events.EventTypeBlock
	case "newTransaction":
		eventType = events.EventTypeTransaction
		filter, err = buildTransactionFilter(sub.Variables["filter"])
		if err != nil {
			c.sendError(id, err.Error())
			return
		}
	case "newPendingTransactions":
		eventType = events.EventTypeTransaction
	case "logs":
		eventType = events.EventTypeLog
		filter, err = buildLogFilter(sub.Variables["filter"])
		if err != nil {
			c.sendError(id, err.Error())
			return
		}
	case "chainConfig":
		eventType = events.EventTypeChainConfig
	case "validatorSet":
		eventType = events.EventTypeValidatorSet
	case "consensusBlock":
		eventType = events.EventTypeConsensusBlock
	case "consensusFork":
		eventType = events.EventTypeConsensusFork
	case "consensusValidatorChange":
		eventType = events.EventTypeConsensusValidatorChange
	case "consensusError":
		eventType = events.EventTypeConsensusError
	case "systemContractEvents":
		eventType = events.EventTypeSystemContract
		filter, err = buildSystemContractFilter(sub.Variables["filter"])
		if err != nil {
			c.sendError(id, err.Error())
			return
		}
	default:
		c.sendError(id, "unknown subscription type")
		return
	}

	// Create subscription ID
	subID := events.SubscriptionID(id)
	eventSub := c.server.eventBus.Subscribe(subID, []events.EventType{eventType}, filter, 100)
	if eventSub == nil {
		c.sendError(id, "failed to create subscription")
		return
	}

	// Create context for this subscription
	subCtx, subCancel := context.WithCancel(c.ctx)

	// Store subscription
	clientSub := &clientSubscription{
		id:         id,
		subType:    subType,
		eventSub:   eventSub,
		cancelFunc: subCancel,
	}

	c.mu.Lock()
	c.subscriptions[id] = clientSub
	c.mu.Unlock()

	// Start goroutine to handle events
	go c.eventLoop(subCtx, clientSub)

	c.logger.Info("subscription started",
		zap.String("id", id),
		zap.String("type", subType),
	)
}

// eventLoop handles events for a subscription
func (c *subscriptionClient) eventLoop(ctx context.Context, sub *clientSubscription) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub.eventSub.Channel:
			if !ok {
				return
			}
			c.handleEvent(sub.id, sub.subType, event)
		}
	}
}

// handleComplete handles subscription completion
func (c *subscriptionClient) handleComplete(id string) {
	c.mu.Lock()
	if sub, ok := c.subscriptions[id]; ok {
		// Cancel the event loop
		sub.cancelFunc()
		// Unsubscribe from EventBus
		if c.server.eventBus != nil {
			c.server.eventBus.Unsubscribe(events.SubscriptionID(id))
		}
		delete(c.subscriptions, id)
	}
	c.mu.Unlock()

	c.logger.Info("subscription completed", zap.String("id", id))
}

// handleEvent handles events from EventBus
func (c *subscriptionClient) handleEvent(id string, subType string, event interface{}) {
	c.logger.Debug("handling event",
		zap.String("id", id),
		zap.String("type", subType),
	)

	var payload interface{}

	switch subType {
	case "newBlock":
		if blockEvent, ok := event.(*events.BlockEvent); ok {
			blockData := map[string]interface{}{
				"number":           blockEvent.Number,
				"hash":             blockEvent.Hash.Hex(),
				"timestamp":        blockEvent.CreatedAt.Unix(),
				"transactionCount": blockEvent.TxCount,
			}
			// Add parentHash and miner if block is available
			if blockEvent.Block != nil {
				blockData["parentHash"] = blockEvent.Block.ParentHash().Hex()
				blockData["miner"] = blockEvent.Block.Coinbase().Hex()
			}
			payload = map[string]interface{}{
				"data": map[string]interface{}{
					"newBlock": blockData,
				},
			}
		}

	case "newTransaction":
		if txEvent, ok := event.(*events.TransactionEvent); ok {
			txData := map[string]interface{}{
				"hash":        txEvent.Hash.Hex(),
				"from":        txEvent.From.Hex(),
				"value":       txEvent.Value,
				"blockNumber": txEvent.BlockNumber,
			}
			// Add to address if available
			if txEvent.To != nil {
				txData["to"] = txEvent.To.Hex()
			}
			payload = map[string]interface{}{
				"data": map[string]interface{}{
					"newTransaction": txData,
				},
			}
		}

	case "newPendingTransactions":
		if txEvent, ok := event.(*events.TransactionEvent); ok {
			pendingData := map[string]interface{}{
				"hash":  txEvent.Hash.Hex(),
				"from":  txEvent.From.Hex(),
				"value": txEvent.Value,
			}
			if txEvent.To != nil {
				pendingData["to"] = txEvent.To.Hex()
			}
			if txEvent.Tx != nil {
				pendingData["nonce"] = txEvent.Tx.Nonce()
				pendingData["gas"] = txEvent.Tx.Gas()
				pendingData["type"] = fmt.Sprintf("0x%x", txEvent.Tx.Type())
				if gasPrice := txEvent.Tx.GasPrice(); gasPrice != nil {
					pendingData["gasPrice"] = gasPrice.String()
				}
				if maxFee := txEvent.Tx.GasFeeCap(); maxFee != nil {
					pendingData["maxFeePerGas"] = maxFee.String()
				}
				if maxPriority := txEvent.Tx.GasTipCap(); maxPriority != nil {
					pendingData["maxPriorityFeePerGas"] = maxPriority.String()
				}
			} else {
				pendingData["type"] = "0x0"
			}
			payload = map[string]interface{}{
				"data": map[string]interface{}{
					"newPendingTransactions": pendingData,
				},
			}
		}

	case "logs":
		if logEvent, ok := event.(*events.LogEvent); ok && logEvent.Log != nil {
			topicStrings := make([]string, len(logEvent.Log.Topics))
			for i, topic := range logEvent.Log.Topics {
				topicStrings[i] = topic.Hex()
			}
			logData := map[string]interface{}{
				"address":          logEvent.Log.Address.Hex(),
				"topics":           topicStrings,
				"data":             hexutil.Encode(logEvent.Log.Data),
				"blockNumber":      logEvent.Log.BlockNumber,
				"transactionHash":  logEvent.Log.TxHash.Hex(),
				"transactionIndex": logEvent.Log.TxIndex,
				"logIndex":         logEvent.Log.Index,
				"removed":          logEvent.Log.Removed,
			}
			if (logEvent.Log.BlockHash != common.Hash{}) {
				logData["blockHash"] = logEvent.Log.BlockHash.Hex()
			}
			payload = map[string]interface{}{
				"data": map[string]interface{}{
					"logs": logData,
				},
			}
		}

	case "chainConfig":
		if configEvent, ok := event.(*events.ChainConfigEvent); ok {
			configData := map[string]interface{}{
				"blockNumber": configEvent.BlockNumber,
				"blockHash":   configEvent.BlockHash.Hex(),
				"parameter":   configEvent.Parameter,
				"oldValue":    configEvent.OldValue,
				"newValue":    configEvent.NewValue,
			}
			payload = map[string]interface{}{
				"data": map[string]interface{}{
					"chainConfig": configData,
				},
			}
		}

	case "validatorSet":
		if validatorEvent, ok := event.(*events.ValidatorSetEvent); ok {
			validatorData := map[string]interface{}{
				"blockNumber":      validatorEvent.BlockNumber,
				"blockHash":        validatorEvent.BlockHash.Hex(),
				"changeType":       validatorEvent.ChangeType,
				"validator":        validatorEvent.Validator.Hex(),
				"validatorSetSize": validatorEvent.ValidatorSetSize,
			}
			if validatorEvent.ValidatorInfo != "" {
				validatorData["validatorInfo"] = validatorEvent.ValidatorInfo
			}
			payload = map[string]interface{}{
				"data": map[string]interface{}{
					"validatorSet": validatorData,
				},
			}
		}

	case "consensusBlock":
		if consensusEvent, ok := event.(*events.ConsensusBlockEvent); ok {
			consensusData := map[string]interface{}{
				"blockNumber":         consensusEvent.BlockNumber,
				"blockHash":           consensusEvent.BlockHash.Hex(),
				"timestamp":           consensusEvent.BlockTimestamp,
				"round":               consensusEvent.Round,
				"prevRound":           consensusEvent.PrevRound,
				"roundChanged":        consensusEvent.RoundChanged,
				"proposer":            consensusEvent.Proposer.Hex(),
				"validatorCount":      consensusEvent.ValidatorCount,
				"prepareCount":        consensusEvent.PrepareCount,
				"commitCount":         consensusEvent.CommitCount,
				"participationRate":   consensusEvent.ParticipationRate,
				"missedValidatorRate": consensusEvent.MissedValidatorRate,
				"isEpochBoundary":     consensusEvent.IsEpochBoundary,
			}
			if consensusEvent.EpochNumber != nil {
				consensusData["epochNumber"] = *consensusEvent.EpochNumber
			}
			if consensusEvent.EpochValidators != nil {
				validators := make([]string, len(consensusEvent.EpochValidators))
				for i, v := range consensusEvent.EpochValidators {
					validators[i] = v.Hex()
				}
				consensusData["epochValidators"] = validators
			}
			payload = map[string]interface{}{
				"data": map[string]interface{}{
					"consensusBlock": consensusData,
				},
			}
		}

	case "consensusFork":
		if forkEvent, ok := event.(*events.ConsensusForkEvent); ok {
			forkData := map[string]interface{}{
				"forkBlockNumber": forkEvent.ForkBlockNumber,
				"forkBlockHash":   forkEvent.ForkBlockHash.Hex(),
				"chain1Hash":      forkEvent.Chain1Hash.Hex(),
				"chain1Height":    forkEvent.Chain1Height,
				"chain1Weight":    forkEvent.Chain1Weight,
				"chain2Hash":      forkEvent.Chain2Hash.Hex(),
				"chain2Height":    forkEvent.Chain2Height,
				"chain2Weight":    forkEvent.Chain2Weight,
				"resolved":        forkEvent.Resolved,
				"winningChain":    forkEvent.WinningChain,
				"detectedAt":      forkEvent.DetectedAt.Unix(),
				"detectionLag":    forkEvent.DetectionLag,
			}
			payload = map[string]interface{}{
				"data": map[string]interface{}{
					"consensusFork": forkData,
				},
			}
		}

	case "consensusValidatorChange":
		if changeEvent, ok := event.(*events.ConsensusValidatorChangeEvent); ok {
			changeData := map[string]interface{}{
				"blockNumber":            changeEvent.BlockNumber,
				"blockHash":              changeEvent.BlockHash.Hex(),
				"timestamp":              changeEvent.BlockTimestamp,
				"epochNumber":            changeEvent.EpochNumber,
				"isEpochBoundary":        changeEvent.IsEpochBoundary,
				"changeType":             changeEvent.ChangeType,
				"previousValidatorCount": changeEvent.PreviousValidatorCount,
				"newValidatorCount":      changeEvent.NewValidatorCount,
			}
			if len(changeEvent.AddedValidators) > 0 {
				added := make([]string, len(changeEvent.AddedValidators))
				for i, v := range changeEvent.AddedValidators {
					added[i] = v.Hex()
				}
				changeData["addedValidators"] = added
			}
			if len(changeEvent.RemovedValidators) > 0 {
				removed := make([]string, len(changeEvent.RemovedValidators))
				for i, v := range changeEvent.RemovedValidators {
					removed[i] = v.Hex()
				}
				changeData["removedValidators"] = removed
			}
			if len(changeEvent.ValidatorSet) > 0 {
				validators := make([]string, len(changeEvent.ValidatorSet))
				for i, v := range changeEvent.ValidatorSet {
					validators[i] = v.Hex()
				}
				changeData["validatorSet"] = validators
			}
			if changeEvent.AdditionalInfo != "" {
				changeData["additionalInfo"] = changeEvent.AdditionalInfo
			}
			payload = map[string]interface{}{
				"data": map[string]interface{}{
					"consensusValidatorChange": changeData,
				},
			}
		}

	case "consensusError":
		if errorEvent, ok := event.(*events.ConsensusErrorEvent); ok {
			errorData := map[string]interface{}{
				"blockNumber":        errorEvent.BlockNumber,
				"blockHash":          errorEvent.BlockHash.Hex(),
				"timestamp":          errorEvent.BlockTimestamp,
				"errorType":          errorEvent.ErrorType,
				"severity":           errorEvent.Severity,
				"errorMessage":       errorEvent.ErrorMessage,
				"round":              errorEvent.Round,
				"expectedValidators": errorEvent.ExpectedValidators,
				"actualSigners":      errorEvent.ActualSigners,
				"participationRate":  errorEvent.ParticipationRate,
				"consensusImpacted":  errorEvent.ConsensusImpacted,
				"recoveryTime":       errorEvent.RecoveryTime,
			}
			if len(errorEvent.MissedValidators) > 0 {
				missed := make([]string, len(errorEvent.MissedValidators))
				for i, v := range errorEvent.MissedValidators {
					missed[i] = v.Hex()
				}
				errorData["missedValidators"] = missed
			}
			if errorEvent.ErrorDetails != "" {
				errorData["errorDetails"] = errorEvent.ErrorDetails
			}
			payload = map[string]interface{}{
				"data": map[string]interface{}{
					"consensusError": errorData,
				},
			}
		}

	case "systemContractEvents":
		if scEvent, ok := event.(*events.SystemContractEvent); ok {
			// Serialize data to JSON string
			dataJSON, _ := json.Marshal(scEvent.Data)
			eventData := map[string]interface{}{
				"contract":        scEvent.Contract.Hex(),
				"eventName":       string(scEvent.EventName),
				"blockNumber":     fmt.Sprintf("%d", scEvent.BlockNumber),
				"transactionHash": scEvent.TxHash.Hex(),
				"logIndex":        scEvent.LogIndex,
				"data":            string(dataJSON),
				"timestamp":       fmt.Sprintf("%d", scEvent.CreatedAt.Unix()),
			}
			payload = map[string]interface{}{
				"data": map[string]interface{}{
					"systemContractEvents": eventData,
				},
			}
		}
	}

	if payload != nil {
		c.sendNext(id, payload)
	}
}

// parseSubscriptionType extracts subscription type from query
func (c *subscriptionClient) parseSubscriptionType(query string) string {
	// Simple parsing - check for subscription keywords (order matters: more specific first)
	if contains(query, "newPendingTransactions") {
		return "newPendingTransactions"
	}
	if contains(query, "systemContractEvents") {
		return "systemContractEvents"
	}
	if contains(query, "consensusValidatorChange") {
		return "consensusValidatorChange"
	}
	if contains(query, "consensusBlock") {
		return "consensusBlock"
	}
	if contains(query, "consensusFork") {
		return "consensusFork"
	}
	if contains(query, "consensusError") {
		return "consensusError"
	}
	if contains(query, "validatorSet") {
		return "validatorSet"
	}
	if contains(query, "chainConfig") {
		return "chainConfig"
	}
	if contains(query, "newBlock") {
		return "newBlock"
	}
	if contains(query, "newTransaction") {
		return "newTransaction"
	}
	if contains(query, "logs") {
		return "logs"
	}
	return ""
}

// contains checks if s contains substr (simple implementation)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func buildTransactionFilter(raw interface{}) (*events.Filter, error) {
	if raw == nil {
		return nil, nil
	}
	filterMap, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid transaction filter format")
	}

	filter := events.NewFilter()

	// Parse from addresses
	if fromVal, ok := filterMap["from"]; ok {
		addresses, err := parseAddressList(fromVal)
		if err != nil {
			return nil, fmt.Errorf("invalid from address: %w", err)
		}
		filter.FromAddresses = addresses
	}

	// Parse to addresses
	if toVal, ok := filterMap["to"]; ok {
		addresses, err := parseAddressList(toVal)
		if err != nil {
			return nil, fmt.Errorf("invalid to address: %w", err)
		}
		filter.ToAddresses = addresses
	}

	// Parse block range
	if fromBlockVal, ok := filterMap["fromBlock"]; ok {
		blockNum, err := parseUint64Value(fromBlockVal)
		if err != nil {
			return nil, fmt.Errorf("invalid fromBlock: %w", err)
		}
		filter.FromBlock = blockNum
	}
	if toBlockVal, ok := filterMap["toBlock"]; ok {
		blockNum, err := parseUint64Value(toBlockVal)
		if err != nil {
			return nil, fmt.Errorf("invalid toBlock: %w", err)
		}
		filter.ToBlock = blockNum
	}

	if filter.IsEmpty() {
		return nil, nil
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	return filter, nil
}

func buildLogFilter(raw interface{}) (*events.Filter, error) {
	if raw == nil {
		return nil, nil
	}
	filterMap, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid log filter format")
	}

	filter := events.NewFilter()

	if addrVal, ok := filterMap["address"]; ok {
		addrStr, ok := addrVal.(string)
		if !ok {
			return nil, fmt.Errorf("address must be a string")
		}
		address, err := parseAddress(addrStr)
		if err != nil {
			return nil, err
		}
		filter.Addresses = append(filter.Addresses, address)
	}

	if addrsVal, ok := filterMap["addresses"]; ok {
		addresses, err := parseAddressList(addrsVal)
		if err != nil {
			return nil, err
		}
		filter.Addresses = append(filter.Addresses, addresses...)
	}

	if topicsVal, ok := filterMap["topics"]; ok {
		topicsSlice, ok := topicsVal.([]interface{})
		if !ok {
			return nil, fmt.Errorf("topics must be an array")
		}
		for _, entry := range topicsSlice {
			topicSet, err := parseTopicEntry(entry)
			if err != nil {
				return nil, err
			}
			filter.Topics = append(filter.Topics, topicSet)
		}
	}

	if fromVal, ok := filterMap["fromBlock"]; ok {
		blockNum, err := parseUint64Value(fromVal)
		if err != nil {
			return nil, fmt.Errorf("invalid fromBlock: %w", err)
		}
		filter.FromBlock = blockNum
	}
	if toVal, ok := filterMap["toBlock"]; ok {
		blockNum, err := parseUint64Value(toVal)
		if err != nil {
			return nil, fmt.Errorf("invalid toBlock: %w", err)
		}
		filter.ToBlock = blockNum
	}

	if filter.IsEmpty() {
		return nil, nil
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	return filter, nil
}

func buildSystemContractFilter(raw interface{}) (*events.Filter, error) {
	if raw == nil {
		return nil, nil
	}
	filterMap, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid system contract filter format")
	}

	filter := events.NewFilter()

	// Parse contract address filter
	if contractVal, ok := filterMap["contract"]; ok {
		contractStr, ok := contractVal.(string)
		if !ok {
			return nil, fmt.Errorf("contract must be a string")
		}
		address, err := parseAddress(contractStr)
		if err != nil {
			return nil, err
		}
		filter.Addresses = append(filter.Addresses, address)
	}

	// Parse event types filter (stored in custom data)
	if eventTypesVal, ok := filterMap["eventTypes"]; ok {
		eventTypesSlice, ok := eventTypesVal.([]interface{})
		if ok && len(eventTypesSlice) > 0 {
			eventTypes := make([]string, 0, len(eventTypesSlice))
			for _, et := range eventTypesSlice {
				if etStr, ok := et.(string); ok {
					eventTypes = append(eventTypes, etStr)
				}
			}
			if len(eventTypes) > 0 {
				filter.CustomData = map[string]interface{}{
					"eventTypes": eventTypes,
				}
			}
		}
	}

	if filter.IsEmpty() {
		return nil, nil
	}

	return filter, nil
}

func parseAddressList(value interface{}) ([]common.Address, error) {
	switch v := value.(type) {
	case []interface{}:
		addresses := make([]common.Address, 0, len(v))
		for _, item := range v {
			addrStr, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("address must be a string")
			}
			addr, err := parseAddress(addrStr)
			if err != nil {
				return nil, err
			}
			addresses = append(addresses, addr)
		}
		return addresses, nil
	case string:
		addr, err := parseAddress(v)
		if err != nil {
			return nil, err
		}
		return []common.Address{addr}, nil
	default:
		return nil, fmt.Errorf("addresses must be an array or string")
	}
}

func parseAddress(value string) (common.Address, error) {
	if !common.IsHexAddress(value) {
		return common.Address{}, fmt.Errorf("invalid address: %s", value)
	}
	return common.HexToAddress(value), nil
}

func parseTopicEntry(entry interface{}) ([]common.Hash, error) {
	switch v := entry.(type) {
	case nil:
		return nil, nil
	case string:
		if v == "" {
			return nil, nil
		}
		hash, err := parseHashString(v)
		if err != nil {
			return nil, err
		}
		return []common.Hash{hash}, nil
	case []interface{}:
		if len(v) == 0 {
			return nil, nil
		}
		hashes := make([]common.Hash, 0, len(v))
		for _, item := range v {
			if item == nil {
				return nil, nil
			}
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("topic entry must be a string")
			}
			hash, err := parseHashString(str)
			if err != nil {
				return nil, err
			}
			hashes = append(hashes, hash)
		}
		return hashes, nil
	default:
		return nil, fmt.Errorf("invalid topic entry type")
	}
}

func parseHashString(value string) (common.Hash, error) {
	if !strings.HasPrefix(value, "0x") {
		return common.Hash{}, fmt.Errorf("hash must be hex string")
	}
	if len(value) != 66 {
		return common.Hash{}, fmt.Errorf("hash must be 32 bytes: %s", value)
	}
	return common.HexToHash(value), nil
}

func parseUint64Value(value interface{}) (uint64, error) {
	switch v := value.(type) {
	case float64:
		return uint64(v), nil
	case int:
		return uint64(v), nil
	case int64:
		return uint64(v), nil
	case string:
		if strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X") {
			parsed, err := strconv.ParseUint(v[2:], 16, 64)
			if err != nil {
				return 0, err
			}
			return parsed, nil
		}
		parsed, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported number type %T", value)
	}
}

// sendMessage sends a message to the client
func (c *subscriptionClient) sendMessage(msg wsMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		c.logger.Error("failed to marshal message", zap.Error(err))
		return
	}

	c.logger.Debug("sending WebSocket message",
		zap.String("type", msg.Type),
		zap.String("id", msg.ID),
		zap.String("message", string(data)),
	)

	select {
	case c.send <- data:
	default:
		c.logger.Warn("send buffer full, dropping message",
			zap.String("type", msg.Type),
		)
	}
}

// sendNext sends subscription data
func (c *subscriptionClient) sendNext(id string, payload interface{}) {
	data, _ := json.Marshal(payload)
	c.logger.Debug("sending subscription data",
		zap.String("id", id),
		zap.Int("payload_size", len(data)),
	)
	c.sendMessage(wsMessage{
		ID:      id,
		Type:    "next",
		Payload: data,
	})
}

// sendError sends an error message
func (c *subscriptionClient) sendError(id string, errMsg string) {
	c.logger.Error("sending error to client",
		zap.String("id", id),
		zap.String("error", errMsg),
	)
	payload, _ := json.Marshal([]map[string]string{
		{"message": errMsg},
	})
	c.sendMessage(wsMessage{
		ID:      id,
		Type:    "error",
		Payload: payload,
	})
}

// cleanup unsubscribes from all EventBus subscriptions
func (c *subscriptionClient) cleanup() {
	c.logger.Info("cleaning up WebSocket client", zap.Int("subscriptions", len(c.subscriptions)))

	// Cancel main context to stop all event loops
	c.cancel()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Unsubscribe all subscriptions from EventBus
	if c.server.eventBus != nil {
		for id, sub := range c.subscriptions {
			c.logger.Debug("unsubscribing",
				zap.String("id", id),
				zap.String("type", sub.subType),
			)
			sub.cancelFunc()
			c.server.eventBus.Unsubscribe(events.SubscriptionID(id))
		}
	}

	c.subscriptions = make(map[string]*clientSubscription)
	close(c.send)
	c.logger.Info("WebSocket client cleanup completed")
}

// SetEventBus sets the EventBus (for dependency injection)
func (s *SubscriptionServer) SetEventBus(bus *events.EventBus) {
	s.eventBus = bus
}

// SubscriptionHandler returns a handler that checks for EventBus availability
func (s *SubscriptionServer) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.eventBus == nil {
			s.logger.Error("EventBus not available for WebSocket subscriptions",
				zap.String("remote_addr", r.RemoteAddr),
			)
			http.Error(w, "subscriptions not available", http.StatusServiceUnavailable)
			return
		}
		s.logger.Debug("EventBus available, proceeding with WebSocket upgrade")
		s.ServeHTTP(w, r)
	}
}

// SubscriptionContext holds context for subscription operations
type SubscriptionContext struct {
	context.Context
	EventBus *events.EventBus
}
