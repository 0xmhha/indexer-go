package notifications

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Delivery metrics (subscriptions design phase 6). Expression metrics are
// in expression_errors.go.
var (
	metricDeliveries = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "indexer_notification_deliveries_total",
		Help: "Delivery attempts of stored notifications by channel and result: sent, retry (failed, retried later) or failed (given up)",
	}, []string{"type", "result"})
	metricDeliverySeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "indexer_notification_delivery_seconds",
		Help:    "Time of one delivery attempt by channel",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10},
	}, []string{"type"})
	metricDeferred = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "indexer_notification_deferred_total",
		Help: "Deliveries put back because their destination was over its rate limit, by channel",
	}, []string{"type"})
	metricStreamMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "indexer_notification_stream_messages_total",
		Help: "Stream messages by type (notification, lagging, error) and result: sent (to at least one connection) or unsent (no connection)",
	}, []string{"type", "result"})
	metricStreamOverflows = promauto.NewCounter(prometheus.CounterOpts{
		Name: "indexer_notification_stream_overflows_total",
		Help: "Stream connections closed because they fell more than their buffer behind",
	})
	metricFastDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "indexer_notification_fast_dropped_blocks_total",
		Help: "Blocks the fast path dropped because its queue was full",
	})
)
