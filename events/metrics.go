package events

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the EventBus
type Metrics struct {
	// Gauges (current values)
	SubscribersTotal     prometheus.Gauge
	SubscribersByType    *prometheus.GaugeVec
	PublishChannelSize   prometheus.Gauge
	SubscribeChannelSize prometheus.Gauge

	// Counters (cumulative values)
	EventsPublishedTotal  *prometheus.CounterVec
	EventsDeliveredTotal  *prometheus.CounterVec
	EventsDroppedTotal    *prometheus.CounterVec
	EventsFilteredTotal   *prometheus.CounterVec
	SubscriptionsTotal    prometheus.Counter
	UnsubscriptionsTotal  prometheus.Counter

	// Histograms (distributions)
	EventDeliveryDuration *prometheus.HistogramVec
	FilterMatchingDuration *prometheus.HistogramVec
	BroadcastDuration     prometheus.Histogram
}

// NewMetrics creates and registers all EventBus metrics
func NewMetrics(namespace, subsystem string) *Metrics {
	if namespace == "" {
		namespace = "indexer"
	}
	if subsystem == "" {
		subsystem = "eventbus"
	}

	return &Metrics{
		// Gauges
		SubscribersTotal: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "subscribers_total",
			Help:      "Current number of active subscribers",
		}),
		SubscribersByType: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "subscribers_by_type",
			Help:      "Current number of subscribers by event type",
		}, []string{"event_type"}),
		PublishChannelSize: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "publish_channel_size",
			Help:      "Current size of the publish channel buffer",
		}),
		SubscribeChannelSize: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "subscribe_channel_size",
			Help:      "Current size of the subscribe channel buffer",
		}),

		// Counters
		EventsPublishedTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "events_published_total",
			Help:      "Total number of events published",
		}, []string{"event_type"}),
		EventsDeliveredTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "events_delivered_total",
			Help:      "Total number of events delivered to subscribers",
		}, []string{"event_type"}),
		EventsDroppedTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "events_dropped_total",
			Help:      "Total number of events dropped due to full channels",
		}, []string{"event_type"}),
		EventsFilteredTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "events_filtered_total",
			Help:      "Total number of events filtered out by subscriber filters",
		}, []string{"event_type", "filter_type"}),
		SubscriptionsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "subscriptions_total",
			Help:      "Total number of subscription requests",
		}),
		UnsubscriptionsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "unsubscriptions_total",
			Help:      "Total number of unsubscription requests",
		}),

		// Histograms
		EventDeliveryDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "event_delivery_duration_seconds",
			Help:      "Event delivery duration in seconds",
			Buckets:   []float64{0.000001, 0.000005, 0.00001, 0.00005, 0.0001, 0.0005, 0.001, 0.005, 0.01}, // 1μs to 10ms
		}, []string{"event_type"}),
		FilterMatchingDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "filter_matching_duration_seconds",
			Help:      "Filter matching duration in seconds",
			Buckets:   []float64{0.000001, 0.000005, 0.00001, 0.00005, 0.0001, 0.0005, 0.001}, // 1μs to 1ms
		}, []string{"event_type", "has_filter"}),
		BroadcastDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "broadcast_duration_seconds",
			Help:      "Event broadcast duration in seconds",
			Buckets:   []float64{0.00001, 0.00005, 0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1}, // 10μs to 100ms
		}),
	}
}

// ObserveEventDelivery records the time taken to deliver an event
func (m *Metrics) ObserveEventDelivery(eventType EventType, duration time.Duration) {
	m.EventDeliveryDuration.WithLabelValues(string(eventType)).Observe(duration.Seconds())
}

// ObserveFilterMatching records the time taken for filter matching
func (m *Metrics) ObserveFilterMatching(eventType EventType, hasFilter bool, duration time.Duration) {
	hasFilterStr := "false"
	if hasFilter {
		hasFilterStr = "true"
	}
	m.FilterMatchingDuration.WithLabelValues(string(eventType), hasFilterStr).Observe(duration.Seconds())
}

// ObserveBroadcast records the time taken to broadcast an event to all subscribers
func (m *Metrics) ObserveBroadcast(duration time.Duration) {
	m.BroadcastDuration.Observe(duration.Seconds())
}

// RecordEventPublished increments the published events counter
func (m *Metrics) RecordEventPublished(eventType EventType) {
	m.EventsPublishedTotal.WithLabelValues(string(eventType)).Inc()
}

// RecordEventDelivered increments the delivered events counter
func (m *Metrics) RecordEventDelivered(eventType EventType) {
	m.EventsDeliveredTotal.WithLabelValues(string(eventType)).Inc()
}

// RecordEventDropped increments the dropped events counter
func (m *Metrics) RecordEventDropped(eventType EventType) {
	m.EventsDroppedTotal.WithLabelValues(string(eventType)).Inc()
}

// RecordEventFiltered increments the filtered events counter
func (m *Metrics) RecordEventFiltered(eventType EventType, filterType string) {
	m.EventsFilteredTotal.WithLabelValues(string(eventType), filterType).Inc()
}

// UpdateSubscriberCount updates the total subscribers gauge
func (m *Metrics) UpdateSubscriberCount(count int) {
	m.SubscribersTotal.Set(float64(count))
}

// UpdateSubscribersByType updates the subscribers by type gauge
func (m *Metrics) UpdateSubscribersByType(eventType EventType, count int) {
	m.SubscribersByType.WithLabelValues(string(eventType)).Set(float64(count))
}

// UpdatePublishChannelSize updates the publish channel size gauge
func (m *Metrics) UpdatePublishChannelSize(size int) {
	m.PublishChannelSize.Set(float64(size))
}

// UpdateSubscribeChannelSize updates the subscribe channel size gauge
func (m *Metrics) UpdateSubscribeChannelSize(size int) {
	m.SubscribeChannelSize.Set(float64(size))
}

// RecordSubscription increments the subscription counter
func (m *Metrics) RecordSubscription() {
	m.SubscriptionsTotal.Inc()
}

// RecordUnsubscription increments the unsubscription counter
func (m *Metrics) RecordUnsubscription() {
	m.UnsubscriptionsTotal.Inc()
}
