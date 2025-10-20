package events

import (
	"context"
	"sync"
	"sync/atomic"
)

// SubscriptionID is a unique identifier for a subscription
type SubscriptionID string

// Subscription represents a client subscription to events
type Subscription struct {
	// ID is the unique identifier for this subscription
	ID SubscriptionID

	// EventTypes is the set of event types this subscription is interested in
	EventTypes map[EventType]bool

	// Channel is where events are delivered to the subscriber
	Channel chan Event

	// CancelFunc allows canceling this subscription
	CancelFunc context.CancelFunc
}

// EventBus is the central message broker for blockchain events
type EventBus struct {
	// subscribers is the registry of active subscriptions
	subscribers map[SubscriptionID]*Subscription
	mu          sync.RWMutex

	// publishCh is the channel for publishing events
	publishCh chan Event

	// subscribeCh is the channel for new subscription requests
	subscribeCh chan *Subscription

	// unsubscribeCh is the channel for unsubscribe requests
	unsubscribeCh chan SubscriptionID

	// done signals when the event bus should stop
	done chan struct{}

	// ctx is the context for the event bus
	ctx context.Context

	// cancel is the cancel function for the event bus
	cancel context.CancelFunc

	// stats tracks event bus statistics
	stats struct {
		totalEvents     atomic.Uint64
		totalDeliveries atomic.Uint64
		droppedEvents   atomic.Uint64
	}
}

// NewEventBus creates a new EventBus with the given buffer sizes
func NewEventBus(publishBufferSize, subscribeBufferSize int) *EventBus {
	ctx, cancel := context.WithCancel(context.Background())

	return &EventBus{
		subscribers:   make(map[SubscriptionID]*Subscription),
		publishCh:     make(chan Event, publishBufferSize),
		subscribeCh:   make(chan *Subscription, subscribeBufferSize),
		unsubscribeCh: make(chan SubscriptionID, subscribeBufferSize),
		done:          make(chan struct{}),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Run starts the event bus main loop
// This should be called in a goroutine
func (eb *EventBus) Run() {
	defer close(eb.done)

	for {
		select {
		case <-eb.ctx.Done():
			// Shutdown: close all subscriptions
			eb.closeAllSubscriptions()
			return

		case sub := <-eb.subscribeCh:
			// Handle new subscription
			eb.mu.Lock()
			eb.subscribers[sub.ID] = sub
			eb.mu.Unlock()

		case subID := <-eb.unsubscribeCh:
			// Handle unsubscribe
			eb.mu.Lock()
			if sub, exists := eb.subscribers[subID]; exists {
				close(sub.Channel)
				delete(eb.subscribers, subID)
			}
			eb.mu.Unlock()

		case event := <-eb.publishCh:
			// Handle event publishing
			eb.stats.totalEvents.Add(1)
			eb.broadcastEvent(event)
		}
	}
}

// broadcastEvent sends an event to all interested subscribers
func (eb *EventBus) broadcastEvent(event Event) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	eventType := event.Type()

	for _, sub := range eb.subscribers {
		// Check if subscriber is interested in this event type
		if !sub.EventTypes[eventType] {
			continue
		}

		// Try to send event non-blocking
		select {
		case sub.Channel <- event:
			eb.stats.totalDeliveries.Add(1)
		default:
			// Channel is full, drop the event
			eb.stats.droppedEvents.Add(1)
		}
	}
}

// closeAllSubscriptions closes all active subscriptions
func (eb *EventBus) closeAllSubscriptions() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	for _, sub := range eb.subscribers {
		close(sub.Channel)
		if sub.CancelFunc != nil {
			sub.CancelFunc()
		}
	}

	eb.subscribers = make(map[SubscriptionID]*Subscription)
}

// Stop gracefully stops the event bus
func (eb *EventBus) Stop() {
	eb.cancel()
	<-eb.done
}

// SubscriberCount returns the current number of active subscribers
func (eb *EventBus) SubscriberCount() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.subscribers)
}

// Stats returns the current statistics
func (eb *EventBus) Stats() (totalEvents, totalDeliveries, droppedEvents uint64) {
	return eb.stats.totalEvents.Load(),
		eb.stats.totalDeliveries.Load(),
		eb.stats.droppedEvents.Load()
}

// Publish publishes an event to all interested subscribers
// This is a non-blocking operation - if the publish channel is full, it returns false
func (eb *EventBus) Publish(event Event) bool {
	// Check if bus is stopped first
	select {
	case <-eb.ctx.Done():
		return false
	default:
	}

	// Try to publish
	select {
	case eb.publishCh <- event:
		return true
	default:
		// Channel is full
		return false
	}
}

// Subscribe creates a new subscription for the given event types
// Returns a Subscription that can be used to receive events
func (eb *EventBus) Subscribe(id SubscriptionID, eventTypes []EventType, channelSize int) *Subscription {
	// Create event type map for fast lookup
	eventTypeMap := make(map[EventType]bool)
	for _, et := range eventTypes {
		eventTypeMap[et] = true
	}

	// Create subscription context
	ctx, cancel := context.WithCancel(eb.ctx)

	sub := &Subscription{
		ID:         id,
		EventTypes: eventTypeMap,
		Channel:    make(chan Event, channelSize),
		CancelFunc: cancel,
	}

	// Send subscribe request
	select {
	case eb.subscribeCh <- sub:
		return sub
	case <-ctx.Done():
		close(sub.Channel)
		return nil
	}
}

// Unsubscribe removes a subscription
func (eb *EventBus) Unsubscribe(id SubscriptionID) {
	select {
	case eb.unsubscribeCh <- id:
	case <-eb.ctx.Done():
	}
}
