package graphql

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/stream"
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

// SubscriptionServer handles GraphQL subscriptions over WebSocket.
//
// Events reach the connections through a subscription engine
// (stream.Engine, refactoring plan R3-3) that holds the server's only
// event bus subscription: it encodes each event once per subscription
// kind, queues it for the connections whose subscriptions match, and
// disconnects a connection whose queue is full with the sequence to
// resubscribe from. SetDirect(true) restores the former delivery, a bus
// subscription and an encoding per client subscription, events dropped when
// a buffer is full.
type SubscriptionServer struct {
	eventBus        *events.EventBus
	logger          *zap.Logger
	upgrader        websocket.Upgrader
	enableKeepAlive bool
	direct          bool

	engineMu  sync.Mutex
	engine    *stream.Engine
	engineBus *events.EventBus
	engineSub events.SubscriptionID
	outbox    stream.Outbox
}

// SetOutbox sets the outbox of the chain whose events the server delivers,
// so subscriptions can resume from a sequence (the fromSequence variable,
// refactoring plan R3-4). Without one, fromSequence is refused.
func (s *SubscriptionServer) SetOutbox(ob stream.Outbox) {
	s.engineMu.Lock()
	defer s.engineMu.Unlock()
	s.outbox = ob
	if s.engine != nil {
		s.engine.SetOutbox(ob)
	}
}

// SetCheckOrigin sets the check of the Origin header of upgrade requests
// (api.allowed_origins); every origin is allowed until it is set. Call it
// before serving.
func (s *SubscriptionServer) SetCheckOrigin(check func(r *http.Request) bool) {
	s.upgrader.CheckOrigin = check
}

// SetDirect selects the former delivery (true): a bus subscription per
// client subscription. It is the switch back from the subscription engine
// (api.subscription_engine: false). Connections opened before keep the
// delivery they started with.
func (s *SubscriptionServer) SetDirect(direct bool) {
	s.engineMu.Lock()
	s.direct = direct
	s.engineMu.Unlock()
	if direct {
		s.stopEngine()
	} else {
		s.subscriptionEngine()
	}
}

// builtinSubscriptions are the subscription kinds of the schema and the
// event types they listen to.
var builtinSubscriptions = map[string]events.EventType{
	"newBlock":               events.EventTypeBlock,
	"newTransaction":         events.EventTypeTransaction,
	"newPendingTransactions": events.EventTypeTransaction,
	"logs":                   events.EventTypeLog,
	"chainConfig":            events.EventTypeChainConfig,
	"validatorSet":           events.EventTypeValidatorSet,
	"reorg":                  events.EventTypeReorg,
}

// engineBusBuffer is the size of the engine's event bus subscription. The
// engine never waits for connections, so it only buffers bursts.
const engineBusBuffer = 1 << 16

// subscriptionEngine returns the engine fed by the server's event bus,
// starting it when needed: the server starts it as soon as it has a bus, so
// replays (replayLast) cover the events before the first connection. It
// returns nil without a bus or in direct mode.
func (s *SubscriptionServer) subscriptionEngine() *stream.Engine {
	s.engineMu.Lock()
	defer s.engineMu.Unlock()
	bus := s.eventBus
	if bus == nil || s.direct {
		return nil
	}
	if s.engine != nil && s.engineBus == bus && !s.engine.Closed() {
		return s.engine
	}
	s.stopEngineLocked()
	e := stream.NewEngine(stream.EngineConfig{Buffer: bus.SubscriberBufferSize()})
	e.SetOutbox(s.outbox)
	for name, eventType := range builtinSubscriptions {
		e.AddTopic(name, eventType, subscriptionEncoder(name))
	}
	for _, name := range registeredSubscriptionNames() {
		spec, _ := registeredSubscription(name)
		e.AddTopic(name, spec.EventType, subscriptionEncoder(name))
	}
	id := events.SubscriptionID("graphql-engine/" + newConnID())
	sub := bus.Subscribe(id, e.EventTypes(), nil, engineBusBuffer)
	if sub == nil {
		return nil // the bus is stopped
	}
	go func() {
		for ev := range sub.Channel {
			e.Publish(ev)
		}
		e.Close() // unsubscribed, or the bus stopped
	}()
	s.engine, s.engineBus, s.engineSub = e, bus, id
	return e
}

// stopEngine stops the engine: its connections close and its bus
// subscription ends.
func (s *SubscriptionServer) stopEngine() {
	s.engineMu.Lock()
	defer s.engineMu.Unlock()
	s.stopEngineLocked()
}

func (s *SubscriptionServer) stopEngineLocked() {
	if s.engine == nil {
		return
	}
	s.engineBus.Unsubscribe(s.engineSub)
	s.engine.Close()
	s.engine, s.engineBus, s.engineSub = nil, nil, ""
}

// subscriptionEncoder encodes an event as the payload of a "next" message
// of subscription kind subType: the GraphQL result, with the event's change
// stream sequence in extensions.sequence when it has one.
func subscriptionEncoder(subType string) stream.Encoder {
	return func(ev events.Event) ([]byte, bool) {
		value, ok := subscriptionValue(subType, ev)
		if !ok {
			return nil, false
		}
		result := map[string]interface{}{"data": map[string]interface{}{subType: value}}
		if seq := events.SequenceOf(ev); seq != 0 {
			result["extensions"] = map[string]interface{}{"sequence": seq}
		}
		data, err := json.Marshal(result)
		return data, err == nil
	}
}

// NewSubscriptionServer creates a new subscription server
func NewSubscriptionServer(eventBus *events.EventBus, logger *zap.Logger, enableKeepAlive bool) *SubscriptionServer {
	s := &SubscriptionServer{
		eventBus:        eventBus,
		logger:          logger,
		enableKeepAlive: enableKeepAlive,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			Subprotocols:    []string{"graphql-transport-ws", "graphql-ws"}, // Support both protocols
			CheckOrigin: func(r *http.Request) bool {
				return true // until SetCheckOrigin
			},
		},
	}
	s.subscriptionEngine()
	return s
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

	// Note: context.Background() is correct here because WebSocket connections
	// outlive the HTTP request - r.Context() would cancel when the handler returns.
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
		connID:          newConnID(),
	}
	if e := s.subscriptionEngine(); e != nil {
		client.stream = e.Connect()
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
	// connID scopes client-chosen subscription ids on the shared event bus,
	// so two connections using the same id ("1") do not collide.
	connID string
	// stream is the connection's place in the subscription engine (nil for
	// direct delivery); writePump writes its frames.
	stream *stream.Conn
}

// busID returns the event bus id for a client subscription id.
func (c *subscriptionClient) busID(id string) events.SubscriptionID {
	return events.SubscriptionID(c.connID + "/" + id)
}

func newConnID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
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
	// A read deadline is only meaningful when the server pings: pongs extend
	// it. Without keep-alive an idle subscriber would be dropped after
	// pongWait even though it is healthy.
	if c.enableKeepAlive {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		c.conn.SetPongHandler(func(string) error {
			c.logger.Debug("received pong message")
			return c.conn.SetReadDeadline(time.Now().Add(pongWait))
		})
	}

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

	var ready, failed <-chan struct{}
	if c.stream != nil {
		ready, failed = c.stream.Ready(), c.stream.Done()
		// A write blocked on a client that stopped reading would hold the
		// disconnected connection open until writeWait; bound it.
		go func() {
			select {
			case <-failed:
				_ = c.conn.UnderlyingConn().SetWriteDeadline(time.Now().Add(disconnectWait))
			case <-c.ctx.Done():
			}
		}()
	}
	w := &frameWriter{}

	for {
		select {
		case <-ready:
			if !c.writeFrames(w) {
				return
			}

		case <-failed:
			c.writeFrames(w) // writes the reason and closes
			return

		case <-c.ctx.Done():
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return

		case message := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
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
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.logger.Debug("ping failed", zap.Error(err))
				return
			}
			c.logger.Debug("sent ping message")
		}
	}
}

// frameWriter builds the "next" messages of engine frames, reusing one
// buffer; the frame's payload is copied in as encoded, once per event.
type frameWriter struct {
	buf []byte
	ids map[string][]byte // subscription id -> its JSON string
}

func (w *frameWriter) next(f stream.Frame) []byte {
	id, ok := w.ids[f.Sub]
	if !ok {
		id, _ = json.Marshal(f.Sub)
		if w.ids == nil {
			w.ids = make(map[string][]byte)
		}
		w.ids[f.Sub] = id
	}
	w.buf = append(w.buf[:0], `{"id":`...)
	w.buf = append(w.buf, id...)
	w.buf = append(w.buf, `,"type":"next","payload":`...)
	w.buf = append(w.buf, f.Payload...)
	w.buf = append(w.buf, '}')
	return w.buf
}

// writeFrames writes the frames the engine queued for the connection. It
// returns false when the connection must close: a write failed, or the
// engine disconnected it (the client is told why).
func (c *subscriptionClient) writeFrames(w *frameWriter) bool {
	frames, err := c.stream.Take()
	if err != nil {
		c.writeDisconnect(err)
		return false
	}
	for _, f := range frames {
		_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := c.conn.WriteMessage(websocket.TextMessage, w.next(f)); err != nil {
			return false
		}
	}
	return true
}

// disconnectWait bounds the writes that tell a disconnected client why.
const disconnectWait = time.Second

// writeDisconnect tells the client why the engine disconnected it. A slow
// connection gets an error per subscription with the change stream
// sequence to resubscribe from (extensions.resumeFrom), then a close frame.
func (c *subscriptionClient) writeDisconnect(err error) {
	var slow *stream.SlowError
	if !errors.As(err, &slow) {
		if c.ctx.Err() == nil { // the engine stopped with its event bus
			_ = c.conn.SetWriteDeadline(time.Now().Add(disconnectWait))
			_ = c.conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, "event stream stopped"))
		}
		return
	}
	c.logger.Warn("Disconnecting slow subscriber", zap.String("conn", c.connID), zap.Uint64("resume_from", slow.ResumeFrom))
	message := "subscriber too slow"
	ext := map[string]interface{}{"code": "SLOW_SUBSCRIBER"}
	if slow.ResumeFrom != 0 {
		message = fmt.Sprintf("subscriber too slow; resubscribe from sequence %d", slow.ResumeFrom)
		ext["resumeFrom"] = slow.ResumeFrom
	}
	payload, _ := json.Marshal([]map[string]interface{}{{"message": message, "extensions": ext}})
	c.mu.RLock()
	ids := make([]string, 0, len(c.subscriptions))
	for id := range c.subscriptions {
		ids = append(ids, id)
	}
	c.mu.RUnlock()
	for _, id := range ids {
		data, _ := json.Marshal(wsMessage{ID: id, Type: "error", Payload: payload})
		_ = c.conn.SetWriteDeadline(time.Now().Add(disconnectWait))
		if c.conn.WriteMessage(websocket.TextMessage, data) != nil {
			return
		}
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(disconnectWait))
	_ = c.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "subscriber too slow"))
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
	case "reorg":
		eventType = events.EventTypeReorg
	default:
		spec, ok := registeredSubscription(subType)
		if !ok {
			c.sendError(id, "unknown subscription type")
			return
		}
		eventType = spec.EventType
		if spec.Filter != nil {
			filter, err = spec.Filter(sub.Variables)
			if err != nil {
				c.sendError(id, err.Error())
				return
			}
		}
	}

	// Parse replayLast parameter
	replayLast := parseReplayLast(sub.Variables["replayLast"])

	// fromSequence resumes from a change stream sequence (R3-4): the
	// events from there that are kept, then live ones.
	var from uint64
	resume := sub.Variables["fromSequence"] != nil
	if resume {
		if from, err = parseUint64Value(sub.Variables["fromSequence"]); err != nil {
			c.sendError(id, "invalid fromSequence")
			return
		}
		if c.stream == nil {
			c.sendErrorCode(id, "fromSequence needs the subscription engine (api.subscription_engine)", "RESUME_UNSUPPORTED", nil)
			return
		}
	}

	if c.stream != nil {
		var match func(events.Event) bool
		if filter != nil {
			match = filter.Match
		}
		if resume {
			err = c.stream.SubscribeFrom(c.ctx, id, subType, match, from)
		} else {
			err = c.stream.Subscribe(id, subType, match, replayLast)
		}
		if err != nil {
			var tooOld *stream.TooOldError
			switch {
			case errors.Is(err, stream.ErrDuplicateSub):
				c.sendError(id, fmt.Sprintf("subscriber for %s already exists", id))
			case errors.As(err, &tooOld):
				ext := map[string]interface{}{}
				if tooOld.Oldest != 0 {
					ext["oldest"] = tooOld.Oldest
				}
				c.sendErrorCode(id, fmt.Sprintf("events from sequence %d are no longer kept; start again from a snapshot (streamSequence)", from),
					"SEQUENCE_TOO_OLD", ext)
			case errors.Is(err, stream.ErrNoOutbox):
				c.sendErrorCode(id, "resuming from a sequence is not supported: events are not kept (eventbus.outbox)", "RESUME_UNSUPPORTED", nil)
			default:
				c.sendError(id, "failed to create subscription")
			}
			return
		}
		c.mu.Lock()
		c.subscriptions[id] = &clientSubscription{id: id, subType: subType, cancelFunc: func() {}}
		c.mu.Unlock()
		c.logger.Info("subscription started", zap.String("id", id), zap.String("type", subType))
		return
	}

	// Create subscription ID
	subID := c.busID(id)
	opts := events.SubscribeOptions{
		ReplayLast: replayLast, // channel size: the bus default (eventbus.subscriber_buffer_size)
	}
	eventSub := c.server.eventBus.SubscribeWithOptions(subID, []events.EventType{eventType}, filter, opts)
	if eventSub == nil {
		c.sendError(id, "failed to create subscription")
		return
	}

	if replayLast > 0 {
		c.logger.Info("subscription with replay",
			zap.String("id", id),
			zap.String("type", subType),
			zap.Int("replayLast", replayLast),
		)
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
		// Unsubscribe from the engine or the EventBus
		if c.stream != nil {
			c.stream.Unsubscribe(id)
		} else if c.server.eventBus != nil {
			c.server.eventBus.Unsubscribe(c.busID(id))
		}
		delete(c.subscriptions, id)
	}
	c.mu.Unlock()

	c.logger.Info("subscription completed", zap.String("id", id))
}

// handleEvent sends an event of a direct (bus) subscription.
func (c *subscriptionClient) handleEvent(id string, subType string, event interface{}) {
	c.logger.Debug("handling event",
		zap.String("id", id),
		zap.String("type", subType),
	)
	ev, ok := event.(events.Event)
	if !ok {
		return
	}
	if data, ok := subscriptionEncoder(subType)(ev); ok {
		c.sendMessage(wsMessage{ID: id, Type: "next", Payload: data})
	}
}

// subscriptionValue returns the value of subscription field subType for an
// event; false skips the event.
func subscriptionValue(subType string, event events.Event) (interface{}, bool) {
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
			payload = blockData
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
			payload = txData
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
			payload = pendingData
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
			payload = logData
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
			payload = configData
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
			payload = validatorData
		}

	case "reorg":
		if reorgEvent, ok := event.(*events.ReorgEvent); ok {
			payload = reorgEventToMap(reorgEvent)
		}

	}

	if payload == nil {
		if spec, ok := registeredSubscription(subType); ok && spec.Payload != nil {
			return spec.Payload(event)
		}
	}
	return payload, payload != nil
}

// parseSubscriptionType extracts subscription type from query
func (c *subscriptionClient) parseSubscriptionType(query string) string {
	// Simple parsing - check for subscription keywords (order matters: more specific first)
	if name := registeredSubscriptionIn(query); name != "" {
		return name
	}
	if contains(query, "newPendingTransactions") {
		return "newPendingTransactions"
	}
	if contains(query, "reorg") {
		return "reorg"
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

// parseReplayLast parses the replayLast parameter from subscription variables
// Returns 0 if not specified or invalid, capped at 100 maximum
func parseReplayLast(value interface{}) int {
	if value == nil {
		return 0
	}

	var replayLast int

	switch v := value.(type) {
	case float64:
		// JSON numbers are decoded as float64
		replayLast = int(v)
	case int:
		replayLast = v
	case int64:
		replayLast = int(v)
	default:
		return 0
	}

	// Validate range: must be positive and capped at 100
	if replayLast < 0 {
		return 0
	}
	if replayLast > 100 {
		return 100
	}

	return replayLast
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
	case <-c.ctx.Done():
		return // connection closing; the send channel is never closed
	case c.send <- data:
	default:
		c.logger.Warn("send buffer full, dropping message",
			zap.String("type", msg.Type),
		)
	}
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

// sendErrorCode sends an error message with extensions.code (and further
// extensions).
func (c *subscriptionClient) sendErrorCode(id, errMsg, code string, ext map[string]interface{}) {
	if ext == nil {
		ext = map[string]interface{}{}
	}
	ext["code"] = code
	payload, _ := json.Marshal([]map[string]interface{}{{"message": errMsg, "extensions": ext}})
	c.sendMessage(wsMessage{ID: id, Type: "error", Payload: payload})
}

// cleanup unsubscribes from all EventBus subscriptions
func (c *subscriptionClient) cleanup() {
	c.logger.Info("cleaning up WebSocket client", zap.Int("subscriptions", len(c.subscriptions)))

	// Cancel main context to stop all event loops
	c.cancel()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stream != nil {
		c.stream.Close()
	}

	// Unsubscribe all subscriptions from EventBus
	if c.stream == nil && c.server.eventBus != nil {
		for id, sub := range c.subscriptions {
			c.logger.Debug("unsubscribing",
				zap.String("id", id),
				zap.String("type", sub.subType),
			)
			sub.cancelFunc()
			c.server.eventBus.Unsubscribe(c.busID(id))
		}
	}

	// c.send is not closed: event loops may still be sending. writePump
	// stops on c.ctx, which was cancelled above.
	c.subscriptions = make(map[string]*clientSubscription)
	c.logger.Info("WebSocket client cleanup completed")
}

// SetEventBus sets the EventBus (for dependency injection)
func (s *SubscriptionServer) SetEventBus(bus *events.EventBus) {
	s.engineMu.Lock()
	s.eventBus = bus
	s.engineMu.Unlock()
	s.subscriptionEngine()
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
