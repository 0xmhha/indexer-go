package events

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Default configuration values
const (
	// DefaultEventHistorySize is the default number of events to keep in history
	DefaultEventHistorySize = 100
)

// SubscriptionID is a unique identifier for a subscription
type SubscriptionID string

// SubscriptionStats tracks statistics for a subscription
type SubscriptionStats struct {
	// EventsReceived is the total number of events received by this subscription
	EventsReceived atomic.Uint64

	// EventsDropped is the number of events dropped due to full channel
	EventsDropped atomic.Uint64

	// LastEventTime is the timestamp of the last event received
	LastEventTime atomic.Int64 // Unix timestamp in nanoseconds

	// CreatedAt is when the subscription was created
	CreatedAt time.Time
}

// SubscribeOptions configures subscription behavior
type SubscribeOptions struct {
	// ReplayLast replays the last N events matching the subscription criteria
	// Set to 0 to disable replay (default)
	ReplayLast int

	// ChannelSize is the buffer size for the subscription channel
	// Default is 100 if not specified
	ChannelSize int
}

// DefaultSubscribeOptions returns default subscription options
func DefaultSubscribeOptions() SubscribeOptions {
	return SubscribeOptions{
		ReplayLast:  0,
		ChannelSize: 100,
	}
}

// Subscription represents a client subscription to events
type Subscription struct {
	// ID is the unique identifier for this subscription
	ID SubscriptionID

	// EventTypes is the set of event types this subscription is interested in
	EventTypes map[EventType]bool

	// Filter contains the filtering conditions for this subscription
	// If nil, no filtering is applied (receives all events of matching types)
	Filter *Filter

	// Channel is where events are delivered to the subscriber
	Channel chan Event

	// CancelFunc allows canceling this subscription
	CancelFunc context.CancelFunc

	// Stats tracks statistics for this subscription
	Stats SubscriptionStats
}

// eventHistoryEntry stores an event with metadata for replay
type eventHistoryEntry struct {
	event     Event
	timestamp time.Time
}

// EventBus is the central message broker for blockchain events
type EventBus struct {
	// subscribers is the registry of active subscriptions
	subscribers map[SubscriptionID]*Subscription
	mu          sync.RWMutex

	// publishCh is the channel for publishing events
	publishCh chan Event

	// done signals when the event bus should stop
	done chan struct{}

	// ctx is the context for the event bus
	ctx context.Context

	// cancel is the cancel function for the event bus
	cancel context.CancelFunc

	// eventHistory is a ring buffer storing recent events for replay
	eventHistory     []eventHistoryEntry
	eventHistorySize int
	eventHistoryIdx  int
	eventHistoryMu   sync.RWMutex

	// stats tracks event bus statistics
	stats struct {
		totalEvents     atomic.Uint64
		totalDeliveries atomic.Uint64
		droppedEvents   atomic.Uint64
	}

	// metrics holds Prometheus metrics (optional)
	metrics *Metrics
}

// NewEventBus creates a new EventBus with the given buffer sizes
// subscribeBufferSize is kept for backward compatibility but no longer used
func NewEventBus(publishBufferSize, subscribeBufferSize int) *EventBus {
	return NewEventBusWithHistory(publishBufferSize, DefaultEventHistorySize)
}

// NewEventBusWithHistory creates a new EventBus with configurable history size
func NewEventBusWithHistory(publishBufferSize, historySize int) *EventBus {
	ctx, cancel := context.WithCancel(context.Background())

	if historySize <= 0 {
		historySize = DefaultEventHistorySize
	}

	return &EventBus{
		subscribers:      make(map[SubscriptionID]*Subscription),
		publishCh:        make(chan Event, publishBufferSize),
		done:             make(chan struct{}),
		ctx:              ctx,
		cancel:           cancel,
		eventHistory:     make([]eventHistoryEntry, historySize),
		eventHistorySize: historySize,
		eventHistoryIdx:  0,
	}
}

// SetMetrics enables Prometheus metrics for the EventBus
// This is optional - if not called, metrics will not be collected
func (eb *EventBus) SetMetrics(metrics *Metrics) {
	eb.metrics = metrics
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

		case event := <-eb.publishCh:
			// Handle event publishing
			eb.stats.totalEvents.Add(1)

			// Store in history for replay
			eb.storeEventInHistory(event)

			// Record metrics
			if eb.metrics != nil {
				eb.metrics.RecordEventPublished(event.Type())
			}

			eb.broadcastEvent(event)
		}
	}
}

// storeEventInHistory stores an event in the ring buffer for replay
func (eb *EventBus) storeEventInHistory(event Event) {
	eb.eventHistoryMu.Lock()
	defer eb.eventHistoryMu.Unlock()

	eb.eventHistory[eb.eventHistoryIdx] = eventHistoryEntry{
		event:     event,
		timestamp: time.Now(),
	}
	eb.eventHistoryIdx = (eb.eventHistoryIdx + 1) % eb.eventHistorySize
}

// getRecentEvents returns the last N events matching the given criteria
// Events are returned in chronological order (oldest first)
func (eb *EventBus) getRecentEvents(n int, eventTypes map[EventType]bool, filter *Filter) []Event {
	eb.eventHistoryMu.RLock()
	defer eb.eventHistoryMu.RUnlock()

	if n <= 0 || n > eb.eventHistorySize {
		n = eb.eventHistorySize
	}

	// Collect matching events
	var matched []Event

	// Start from the oldest entry and work forward
	for i := 0; i < eb.eventHistorySize; i++ {
		idx := (eb.eventHistoryIdx + i) % eb.eventHistorySize
		entry := eb.eventHistory[idx]

		// Skip empty entries
		if entry.event == nil {
			continue
		}

		// Check event type
		if !eventTypes[entry.event.Type()] {
			continue
		}

		// Apply filter if present
		if filter != nil && !filter.Match(entry.event) {
			continue
		}

		matched = append(matched, entry.event)
	}

	// Return only the last N events
	if len(matched) > n {
		matched = matched[len(matched)-n:]
	}

	return matched
}

// broadcastEvent sends an event to all interested subscribers
func (eb *EventBus) broadcastEvent(event Event) {
	startTime := time.Now()
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	eventType := event.Type()

	for _, sub := range eb.subscribers {
		// Check if subscriber is interested in this event type
		if !sub.EventTypes[eventType] {
			continue
		}

		// Apply filter if present
		hasFilter := sub.Filter != nil
		filterStart := time.Now()
		if hasFilter && !sub.Filter.Match(event) {
			// Record filter matching time
			if eb.metrics != nil {
				eb.metrics.ObserveFilterMatching(eventType, true, time.Since(filterStart))
				eb.metrics.RecordEventFiltered(eventType, "filtered")
			}
			continue
		}
		if eb.metrics != nil && hasFilter {
			eb.metrics.ObserveFilterMatching(eventType, true, time.Since(filterStart))
		} else if eb.metrics != nil {
			eb.metrics.ObserveFilterMatching(eventType, false, time.Since(filterStart))
		}

		// Try to send event non-blocking
		deliveryStart := time.Now()
		select {
		case sub.Channel <- event:
			eb.stats.totalDeliveries.Add(1)
			// Update subscription stats
			sub.Stats.EventsReceived.Add(1)
			sub.Stats.LastEventTime.Store(time.Now().UnixNano())
			// Record delivery metrics
			if eb.metrics != nil {
				eb.metrics.RecordEventDelivered(eventType)
				eb.metrics.ObserveEventDelivery(eventType, time.Since(deliveryStart))
			}
		default:
			// Channel is full, drop the event
			eb.stats.droppedEvents.Add(1)
			sub.Stats.EventsDropped.Add(1)
			if eb.metrics != nil {
				eb.metrics.RecordEventDropped(eventType)
			}
		}
	}

	// Record broadcast duration
	if eb.metrics != nil {
		eb.metrics.ObserveBroadcast(time.Since(startTime))
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
// filter can be nil for no filtering
// This method is synchronous - the subscription is active immediately upon return
func (eb *EventBus) Subscribe(id SubscriptionID, eventTypes []EventType, filter *Filter, channelSize int) *Subscription {
	return eb.SubscribeWithOptions(id, eventTypes, filter, SubscribeOptions{
		ChannelSize: channelSize,
		ReplayLast:  0,
	})
}

// SubscribeWithOptions creates a new subscription with configurable options
// This method is synchronous - the subscription is active immediately upon return
// If ReplayLast > 0, matching historical events will be delivered first
func (eb *EventBus) SubscribeWithOptions(id SubscriptionID, eventTypes []EventType, filter *Filter, opts SubscribeOptions) *Subscription {
	// Check if bus is stopped
	select {
	case <-eb.ctx.Done():
		return nil
	default:
	}

	// Validate filter if provided
	if filter != nil {
		if err := filter.Validate(); err != nil {
			// Invalid filter, return nil
			return nil
		}
		// Clone filter to prevent external modification
		filter = filter.Clone()
	}

	// Create event type map for fast lookup
	eventTypeMap := make(map[EventType]bool)
	for _, et := range eventTypes {
		eventTypeMap[et] = true
	}

	// Apply default channel size
	channelSize := opts.ChannelSize
	if channelSize <= 0 {
		channelSize = 100
	}

	// Create subscription context for cancellation
	_, cancel := context.WithCancel(eb.ctx)

	sub := &Subscription{
		ID:         id,
		EventTypes: eventTypeMap,
		Filter:     filter,
		Channel:    make(chan Event, channelSize),
		CancelFunc: cancel,
		Stats: SubscriptionStats{
			CreatedAt: time.Now(),
		},
	}

	// Synchronously register the subscription
	// This ensures the subscription is active before returning
	eb.mu.Lock()
	eb.subscribers[id] = sub
	eb.mu.Unlock()

	// Update metrics
	if eb.metrics != nil {
		eb.metrics.RecordSubscription()
		eb.updateSubscriberMetrics()
	}

	// Replay historical events if requested
	if opts.ReplayLast > 0 {
		eb.replayEventsToSubscriber(sub, opts.ReplayLast)
	}

	return sub
}

// replayEventsToSubscriber sends historical events to a subscriber
func (eb *EventBus) replayEventsToSubscriber(sub *Subscription, count int) {
	events := eb.getRecentEvents(count, sub.EventTypes, sub.Filter)

	for _, event := range events {
		select {
		case sub.Channel <- event:
			sub.Stats.EventsReceived.Add(1)
			sub.Stats.LastEventTime.Store(time.Now().UnixNano())
		default:
			// Channel full, skip replay event
			sub.Stats.EventsDropped.Add(1)
		}
	}
}

// Unsubscribe removes a subscription
// This method is synchronous - the subscription is removed immediately
func (eb *EventBus) Unsubscribe(id SubscriptionID) {
	eb.mu.Lock()
	if sub, exists := eb.subscribers[id]; exists {
		close(sub.Channel)
		if sub.CancelFunc != nil {
			sub.CancelFunc()
		}
		delete(eb.subscribers, id)
	}
	eb.mu.Unlock()

	// Update metrics
	if eb.metrics != nil {
		eb.metrics.RecordUnsubscription()
		eb.updateSubscriberMetrics()
	}
}

// updateSubscriberMetrics updates subscriber count metrics
func (eb *EventBus) updateSubscriberMetrics() {
	if eb.metrics == nil {
		return
	}

	// Count total subscribers
	eb.mu.RLock()
	totalCount := len(eb.subscribers)

	// Count subscribers by event type
	typeCount := make(map[EventType]int)
	for _, sub := range eb.subscribers {
		for eventType := range sub.EventTypes {
			typeCount[eventType]++
		}
	}
	eb.mu.RUnlock()

	// Update metrics
	eb.metrics.UpdateSubscriberCount(totalCount)
	for eventType, count := range typeCount {
		eb.metrics.UpdateSubscribersByType(eventType, count)
	}

	// Update channel sizes
	eb.metrics.UpdatePublishChannelSize(len(eb.publishCh))
}

// SubscriberInfo contains information about a subscriber
type SubscriberInfo struct {
	ID             SubscriptionID
	EventTypes     []EventType
	HasFilter      bool
	EventsReceived uint64
	EventsDropped  uint64
	LastEventTime  time.Time
	CreatedAt      time.Time
	Uptime         time.Duration
}

// GetSubscriberInfo returns information about a specific subscriber
func (eb *EventBus) GetSubscriberInfo(id SubscriptionID) *SubscriberInfo {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	sub, exists := eb.subscribers[id]
	if !exists {
		return nil
	}

	// Collect event types
	eventTypes := make([]EventType, 0, len(sub.EventTypes))
	for et := range sub.EventTypes {
		eventTypes = append(eventTypes, et)
	}

	// Get last event time
	lastEventNano := sub.Stats.LastEventTime.Load()
	var lastEventTime time.Time
	if lastEventNano > 0 {
		lastEventTime = time.Unix(0, lastEventNano)
	}

	return &SubscriberInfo{
		ID:             sub.ID,
		EventTypes:     eventTypes,
		HasFilter:      sub.Filter != nil,
		EventsReceived: sub.Stats.EventsReceived.Load(),
		EventsDropped:  sub.Stats.EventsDropped.Load(),
		LastEventTime:  lastEventTime,
		CreatedAt:      sub.Stats.CreatedAt,
		Uptime:         time.Since(sub.Stats.CreatedAt),
	}
}

// GetAllSubscriberInfo returns information about all subscribers
func (eb *EventBus) GetAllSubscriberInfo() []SubscriberInfo {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	infos := make([]SubscriberInfo, 0, len(eb.subscribers))
	for id := range eb.subscribers {
		if info := eb.GetSubscriberInfo(id); info != nil {
			infos = append(infos, *info)
		}
	}

	return infos
}
