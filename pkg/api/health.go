package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/pkg/eventbus"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// DetailedHealth provides comprehensive health information for the indexer service
type DetailedHealth struct {
	Status       string                   `json:"status"`
	NodeID       string                   `json:"node_id"`
	Role         string                   `json:"role"`
	Timestamp    string                   `json:"timestamp"`
	Uptime       string                   `json:"uptime"`
	Version      string                   `json:"version"`
	EventBus     *EventBusHealth          `json:"eventbus,omitempty"`
	Redis        *ComponentHealth         `json:"redis,omitempty"`
	Kafka        *ComponentHealth         `json:"kafka,omitempty"`
	Storage      *ComponentHealth         `json:"storage,omitempty"`
	Chains       map[string]ChainHealth   `json:"chains,omitempty"`
	Metrics      HealthMetrics            `json:"metrics"`
	Dependencies []DependencyHealth       `json:"dependencies,omitempty"`
}

// EventBusHealth contains EventBus health information
type EventBusHealth struct {
	Type            string  `json:"type"`
	Status          string  `json:"status"`
	Subscribers     int     `json:"subscribers"`
	TotalEvents     uint64  `json:"total_events"`
	TotalDeliveries uint64  `json:"total_deliveries"`
	DroppedEvents   uint64  `json:"dropped_events"`
	DropRate        float64 `json:"drop_rate_percent"`
	Connected       bool    `json:"connected,omitempty"`
}

// ComponentHealth represents the health of a component
type ComponentHealth struct {
	Status    string                 `json:"status"`
	Message   string                 `json:"message,omitempty"`
	Connected bool                   `json:"connected"`
	Latency   string                 `json:"latency,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// ChainHealth represents the health of a blockchain connection
type ChainHealth struct {
	Status          string `json:"status"`
	ChainID         uint64 `json:"chain_id"`
	LastBlockNumber uint64 `json:"last_block_number"`
	LastBlockTime   string `json:"last_block_time,omitempty"`
	SyncStatus      string `json:"sync_status"`
	Healthy         bool   `json:"healthy"`
}

// HealthMetrics contains operational metrics
type HealthMetrics struct {
	RequestsPerSecond  float64 `json:"requests_per_second,omitempty"`
	AverageLatencyMs   float64 `json:"average_latency_ms,omitempty"`
	ErrorRate          float64 `json:"error_rate_percent,omitempty"`
	MemoryUsageMB      float64 `json:"memory_usage_mb,omitempty"`
	GoroutineCount     int     `json:"goroutine_count,omitempty"`
	ActiveConnections  int     `json:"active_connections,omitempty"`
	EventsProcessed    uint64  `json:"events_processed,omitempty"`
}

// DependencyHealth represents health of an external dependency
type DependencyHealth struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Latency string `json:"latency,omitempty"`
	Message string `json:"message,omitempty"`
}

// HealthChecker manages health checks for various components
type HealthChecker struct {
	mu sync.RWMutex

	nodeID    string
	nodeRole  string
	version   string
	startTime time.Time

	// Components
	eventBus eventbus.EventBus
	storage  storage.Storage

	// Distributed components
	redisEventBus *eventbus.RedisEventBus
	kafkaProducer *eventbus.KafkaProducer

	// Health states
	lastCheck      time.Time //nolint:unused
	componentState map[string]string
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(nodeID, nodeRole, version string) *HealthChecker {
	return &HealthChecker{
		nodeID:         nodeID,
		nodeRole:       nodeRole,
		version:        version,
		startTime:      time.Now(),
		componentState: make(map[string]string),
	}
}

// SetEventBus sets the event bus for health checking
func (hc *HealthChecker) SetEventBus(eb eventbus.EventBus) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.eventBus = eb

	// Check if it's a Redis EventBus
	if reb, ok := eb.(*eventbus.RedisEventBus); ok {
		hc.redisEventBus = reb
	}
}

// SetStorage sets the storage for health checking
func (hc *HealthChecker) SetStorage(s storage.Storage) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.storage = s
}

// SetKafkaProducer sets the Kafka producer for health checking
func (hc *HealthChecker) SetKafkaProducer(kp *eventbus.KafkaProducer) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.kafkaProducer = kp
}

// GetDetailedHealth returns comprehensive health information
func (hc *HealthChecker) GetDetailedHealth(ctx context.Context) DetailedHealth {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	health := DetailedHealth{
		Status:    "healthy",
		NodeID:    hc.nodeID,
		Role:      hc.nodeRole,
		Timestamp: time.Now().Format(time.RFC3339),
		Uptime:    time.Since(hc.startTime).String(),
		Version:   hc.version,
		Chains:    make(map[string]ChainHealth),
		Metrics:   HealthMetrics{},
	}

	// Check EventBus health
	if hc.eventBus != nil {
		ebHealth := hc.checkEventBusHealth()
		health.EventBus = ebHealth
		if ebHealth.Status != "healthy" {
			health.Status = "degraded"
		}
	}

	// Check Redis health
	if hc.redisEventBus != nil {
		redisHealth := hc.checkRedisHealth(ctx)
		health.Redis = redisHealth
		if redisHealth.Status != "healthy" && redisHealth.Status != "" {
			health.Status = "degraded"
		}
	}

	// Check Kafka health
	if hc.kafkaProducer != nil {
		kafkaHealth := hc.checkKafkaHealth()
		health.Kafka = kafkaHealth
		if kafkaHealth.Status != "healthy" && kafkaHealth.Status != "" {
			health.Status = "degraded"
		}
	}

	// Check Storage health
	if hc.storage != nil {
		storageHealth := hc.checkStorageHealth(ctx)
		health.Storage = storageHealth
		if storageHealth.Status != "healthy" {
			health.Status = "unhealthy"
		}
	}

	return health
}

// checkEventBusHealth checks the EventBus health
func (hc *HealthChecker) checkEventBusHealth() *EventBusHealth {
	if hc.eventBus == nil {
		return nil
	}

	totalEvents, totalDeliveries, droppedEvents := hc.eventBus.Stats()
	dropRate := float64(0)
	if totalEvents > 0 {
		dropRate = float64(droppedEvents) / float64(totalEvents) * 100
	}

	status := "healthy"
	if !hc.eventBus.Healthy() {
		status = "unhealthy"
	} else if dropRate > 5.0 {
		status = "degraded"
	}

	ebHealth := &EventBusHealth{
		Type:            string(hc.eventBus.Type()),
		Status:          status,
		Subscribers:     hc.eventBus.SubscriberCount(),
		TotalEvents:     totalEvents,
		TotalDeliveries: totalDeliveries,
		DroppedEvents:   droppedEvents,
		DropRate:        dropRate,
	}

	// Add connection status for distributed event bus
	if deb, ok := hc.eventBus.(eventbus.DistributedEventBus); ok {
		ebHealth.Connected = deb.IsConnected()
	}

	return ebHealth
}

// checkRedisHealth checks Redis connection health
func (hc *HealthChecker) checkRedisHealth(ctx context.Context) *ComponentHealth {
	if hc.redisEventBus == nil {
		return nil
	}

	redisHealth := hc.redisEventBus.GetHealthStatus()

	return &ComponentHealth{
		Status:    redisHealth.Status,
		Message:   redisHealth.Message,
		Connected: hc.redisEventBus.IsConnected(),
		Details:   redisHealth.Details,
	}
}

// checkKafkaHealth checks Kafka connection health
func (hc *HealthChecker) checkKafkaHealth() *ComponentHealth {
	if hc.kafkaProducer == nil {
		return nil
	}

	kafkaHealth := hc.kafkaProducer.GetHealthStatus()

	return &ComponentHealth{
		Status:    kafkaHealth.Status,
		Message:   kafkaHealth.Message,
		Connected: hc.kafkaProducer.IsConnected(),
		Details:   kafkaHealth.Details,
	}
}

// checkStorageHealth checks storage health
func (hc *HealthChecker) checkStorageHealth(ctx context.Context) *ComponentHealth {
	if hc.storage == nil {
		return nil
	}

	start := time.Now()

	// Try to get the latest height to verify storage is working
	_, err := hc.storage.GetLatestHeight(ctx)
	latency := time.Since(start)

	status := "healthy"
	message := "Storage is operational"
	if err != nil {
		// Check if it's just an empty database (no blocks indexed yet)
		if err != storage.ErrNotFound {
			status = "unhealthy"
			message = err.Error()
		}
	}

	return &ComponentHealth{
		Status:    status,
		Message:   message,
		Connected: true,
		Latency:   latency.String(),
	}
}

// LivenessHandler returns a handler for Kubernetes liveness probe
// Returns 200 if the process is alive
func (hc *HealthChecker) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "alive",
		})
	}
}

// ReadinessHandler returns a handler for Kubernetes readiness probe
// Returns 200 if the service is ready to accept traffic
func (hc *HealthChecker) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Check if essential components are ready
		ready := true
		reasons := make([]string, 0)

		hc.mu.RLock()
		defer hc.mu.RUnlock()

		// Storage must be available
		if hc.storage == nil {
			ready = false
			reasons = append(reasons, "storage not initialized")
		}

		// EventBus should be healthy if configured
		if hc.eventBus != nil && !hc.eventBus.Healthy() {
			ready = false
			reasons = append(reasons, "eventbus not healthy")
		}

		if ready {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "ready",
			})
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":  "not_ready",
				"reasons": reasons,
			})
		}
	}
}

// StartupHandler returns a handler for Kubernetes startup probe
// Returns 200 once initial startup is complete
func (hc *HealthChecker) StartupHandler(startupComplete *bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if startupComplete != nil && *startupComplete {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "started",
			})
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "starting",
			})
		}
	}
}

// DetailedHealthHandler returns a handler for comprehensive health checks
func (hc *HealthChecker) DetailedHealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		health := hc.GetDetailedHealth(r.Context())

		status := http.StatusOK
		if health.Status == "unhealthy" {
			status = http.StatusServiceUnavailable
		} else if health.Status == "degraded" {
			status = http.StatusOK // Still serve traffic when degraded
		}

		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(health)
	}
}
