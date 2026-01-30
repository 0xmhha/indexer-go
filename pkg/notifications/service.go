package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// Service defines the notification service interface.
type Service interface {
	// Start starts the notification service.
	Start(ctx context.Context) error

	// Stop gracefully stops the notification service.
	Stop(ctx context.Context) error

	// Settings management
	CreateSetting(ctx context.Context, setting *NotificationSetting) (*NotificationSetting, error)
	UpdateSetting(ctx context.Context, setting *NotificationSetting) (*NotificationSetting, error)
	DeleteSetting(ctx context.Context, id string) error
	GetSetting(ctx context.Context, id string) (*NotificationSetting, error)
	ListSettings(ctx context.Context, filter *SettingsFilter) ([]*NotificationSetting, error)

	// Notification operations
	GetNotification(ctx context.Context, id string) (*Notification, error)
	ListNotifications(ctx context.Context, filter *NotificationsFilter) ([]*Notification, error)
	RetryNotification(ctx context.Context, id string) error
	CancelNotification(ctx context.Context, id string) error

	// Statistics
	GetStats(ctx context.Context, settingID string) (*NotificationStats, error)
	GetDeliveryHistory(ctx context.Context, notificationID string) ([]*DeliveryHistory, error)

	// Testing
	TestSetting(ctx context.Context, id string) (*DeliveryResult, error)
}

// SettingsFilter for listing notification settings.
type SettingsFilter struct {
	Types      []NotificationType
	EventTypes []EventType
	Enabled    *bool
	Limit      int
	Offset     int
}

// NotificationsFilter for listing notifications.
type NotificationsFilter struct {
	SettingID  string
	Status     []DeliveryStatus
	EventTypes []EventType
	FromTime   *time.Time
	ToTime     *time.Time
	Limit      int
	Offset     int
}

// Storage defines the notification storage interface.
type Storage interface {
	// Settings
	SaveSetting(ctx context.Context, setting *NotificationSetting) error
	GetSetting(ctx context.Context, id string) (*NotificationSetting, error)
	DeleteSetting(ctx context.Context, id string) error
	ListSettings(ctx context.Context, filter *SettingsFilter) ([]*NotificationSetting, error)

	// Notifications
	SaveNotification(ctx context.Context, notification *Notification) error
	GetNotification(ctx context.Context, id string) (*Notification, error)
	UpdateNotificationStatus(ctx context.Context, id string, status DeliveryStatus, err string) error
	ListNotifications(ctx context.Context, filter *NotificationsFilter) ([]*Notification, error)
	GetPendingNotifications(ctx context.Context, limit int) ([]*Notification, error)

	// History
	SaveDeliveryHistory(ctx context.Context, history *DeliveryHistory) error
	GetDeliveryHistory(ctx context.Context, notificationID string) ([]*DeliveryHistory, error)

	// Stats
	GetStats(ctx context.Context, settingID string) (*NotificationStats, error)
	IncrementStats(ctx context.Context, settingID string, success bool, deliveryMs int64) error

	// Cleanup
	CleanupOldHistory(ctx context.Context, before time.Time) (int64, error)
}

// Handler defines the interface for notification delivery handlers.
type Handler interface {
	Type() NotificationType
	Deliver(ctx context.Context, notification *Notification, setting *NotificationSetting) (*DeliveryResult, error)
	Validate(setting *NotificationSetting) error
}

// NotificationService implements the Service interface.
type NotificationService struct {
	config   *Config
	storage  Storage
	eventBus *events.EventBus
	logger   *zap.Logger

	handlers map[NotificationType]Handler
	queue    chan *Notification

	mu        sync.RWMutex
	settings  map[string]*NotificationSetting
	running   bool
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	eventSub *events.Subscription
}

// NewService creates a new notification service.
func NewService(
	config *Config,
	storage Storage,
	eventBus *events.EventBus,
	logger *zap.Logger,
) *NotificationService {
	if config == nil {
		config = DefaultConfig()
	}

	return &NotificationService{
		config:   config,
		storage:  storage,
		eventBus: eventBus,
		logger:   logger.Named("notifications"),
		handlers: make(map[NotificationType]Handler),
		queue:    make(chan *Notification, config.Queue.BufferSize),
		settings: make(map[string]*NotificationSetting),
	}
}

// RegisterHandler registers a notification handler.
func (s *NotificationService) RegisterHandler(handler Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[handler.Type()] = handler
	s.logger.Info("registered notification handler", zap.String("type", string(handler.Type())))
}

// Start starts the notification service.
func (s *NotificationService) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("notification service already running")
	}
	s.running = true
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Unlock()

	s.logger.Info("starting notification service")

	// Validate configuration
	if err := s.config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Load existing settings
	if err := s.loadSettings(s.ctx); err != nil {
		s.logger.Warn("failed to load notification settings", zap.Error(err))
	}

	// Start workers
	for i := 0; i < s.config.Queue.Workers; i++ {
		s.wg.Add(1)
		go s.worker(i)
	}

	// Subscribe to events
	if s.eventBus != nil {
		if err := s.subscribeToEvents(); err != nil {
			s.logger.Error("failed to subscribe to events", zap.Error(err))
		}
	}

	// Start retry processor
	s.wg.Add(1)
	go s.retryProcessor()

	// Start cleanup processor
	s.wg.Add(1)
	go s.cleanupProcessor()

	s.logger.Info("notification service started",
		zap.Int("workers", s.config.Queue.Workers),
		zap.Int("queue_size", s.config.Queue.BufferSize),
	)

	return nil
}

// Stop gracefully stops the notification service.
func (s *NotificationService) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	s.logger.Info("stopping notification service")

	// Cancel context
	s.cancel()

	// Unsubscribe from events
	if s.eventSub != nil && s.eventBus != nil {
		s.eventBus.Unsubscribe(s.eventSub.ID)
	}

	// Wait for workers with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("notification service stopped gracefully")
	case <-ctx.Done():
		s.logger.Warn("notification service stop timed out")
	}

	return nil
}

// loadSettings loads all notification settings from storage.
func (s *NotificationService) loadSettings(ctx context.Context) error {
	settings, err := s.storage.ListSettings(ctx, &SettingsFilter{Limit: 10000})
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, setting := range settings {
		s.settings[setting.ID] = setting
	}

	s.logger.Info("loaded notification settings", zap.Int("count", len(settings)))
	return nil
}

// subscribeToEvents subscribes to blockchain events.
func (s *NotificationService) subscribeToEvents() error {
	eventTypes := []events.EventType{
		events.EventTypeBlock,
		events.EventTypeTransaction,
		events.EventTypeLog,
	}

	subID := events.SubscriptionID("notifications-" + uuid.New().String())
	sub := s.eventBus.Subscribe(subID, eventTypes, nil, s.config.Queue.BufferSize)
	if sub == nil {
		return fmt.Errorf("failed to subscribe to events")
	}

	s.eventSub = sub

	// Process events
	s.wg.Add(1)
	go s.processEvents()

	s.logger.Info("subscribed to blockchain events")
	return nil
}

// processEvents processes blockchain events and creates notifications.
func (s *NotificationService) processEvents() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case event, ok := <-s.eventSub.Channel:
			if !ok {
				return
			}
			s.handleEvent(event)
		}
	}
}

// handleEvent processes a single blockchain event.
func (s *NotificationService) handleEvent(event events.Event) {
	s.mu.RLock()
	settings := make([]*NotificationSetting, 0, len(s.settings))
	for _, setting := range s.settings {
		if setting.Enabled {
			settings = append(settings, setting)
		}
	}
	s.mu.RUnlock()

	for _, setting := range settings {
		if s.shouldNotify(setting, event) {
			notification := s.createNotification(setting, event)
			if notification != nil {
				s.enqueueNotification(notification)
			}
		}
	}
}

// shouldNotify checks if a setting should be notified for an event.
func (s *NotificationService) shouldNotify(setting *NotificationSetting, event events.Event) bool {
	eventType := s.convertEventType(event.Type())

	// Check if event type is in the setting's event types
	found := false
	for _, et := range setting.EventTypes {
		if et == eventType {
			found = true
			break
		}
	}
	if !found {
		return false
	}

	// Apply filters if present
	if setting.Filter != nil {
		return s.matchesFilter(setting.Filter, event)
	}

	return true
}

// convertEventType converts internal event type to notification event type.
func (s *NotificationService) convertEventType(eventType events.EventType) EventType {
	switch eventType {
	case events.EventTypeBlock:
		return EventTypeBlock
	case events.EventTypeTransaction:
		return EventTypeTransaction
	case events.EventTypeLog:
		return EventTypeLog
	default:
		return EventType(eventType)
	}
}

// matchesFilter checks if an event matches the notification filter.
func (s *NotificationService) matchesFilter(filter *NotifyFilter, event events.Event) bool {
	// TODO: Implement detailed filter matching
	// For now, accept all events
	return true
}

// createNotification creates a notification from an event.
func (s *NotificationService) createNotification(setting *NotificationSetting, event events.Event) *Notification {
	payload, err := s.createPayload(event)
	if err != nil {
		s.logger.Error("failed to create notification payload", zap.Error(err))
		return nil
	}

	return &Notification{
		ID:         uuid.New().String(),
		SettingID:  setting.ID,
		Type:       setting.Type,
		EventType:  s.convertEventType(event.Type()),
		Payload:    payload,
		Status:     DeliveryStatusPending,
		RetryCount: 0,
		CreatedAt:  time.Now(),
	}
}

// createPayload creates an event payload.
func (s *NotificationService) createPayload(event events.Event) (*EventPayload, error) {
	var blockNumber uint64
	var blockHash common.Hash
	var chainID uint64 = 1 // Default chain ID

	// Extract block info based on event type
	switch e := event.(type) {
	case *events.BlockEvent:
		blockNumber = e.Number
		blockHash = e.Hash
	case *events.TransactionEvent:
		blockNumber = e.BlockNumber
		blockHash = e.BlockHash
	case *events.LogEvent:
		if e.Log != nil {
			blockNumber = e.Log.BlockNumber
			blockHash = e.Log.BlockHash
		}
	}

	// Serialize the event data
	data, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	return &EventPayload{
		ChainID:     chainID,
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		Timestamp:   event.Timestamp(),
		EventType:   s.convertEventType(event.Type()),
		Data:        data,
	}, nil
}

// enqueueNotification adds a notification to the queue.
func (s *NotificationService) enqueueNotification(notification *Notification) {
	select {
	case s.queue <- notification:
		// Save to storage
		if err := s.storage.SaveNotification(s.ctx, notification); err != nil {
			s.logger.Error("failed to save notification", zap.Error(err))
		}
	default:
		s.logger.Warn("notification queue full, dropping notification",
			zap.String("notification_id", notification.ID))
	}
}

// worker processes notifications from the queue.
func (s *NotificationService) worker(id int) {
	defer s.wg.Done()
	s.logger.Debug("notification worker started", zap.Int("worker_id", id))

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Debug("notification worker stopping", zap.Int("worker_id", id))
			return
		case notification, ok := <-s.queue:
			if !ok {
				return
			}
			s.processNotification(notification)
		}
	}
}

// processNotification delivers a single notification.
func (s *NotificationService) processNotification(notification *Notification) {
	s.mu.RLock()
	setting := s.settings[notification.SettingID]
	s.mu.RUnlock()

	if setting == nil {
		s.logger.Warn("notification setting not found",
			zap.String("notification_id", notification.ID),
			zap.String("setting_id", notification.SettingID))
		return
	}

	handler, ok := s.handlers[notification.Type]
	if !ok {
		s.logger.Error("no handler for notification type",
			zap.String("type", string(notification.Type)))
		return
	}

	// Update status to sending
	notification.Status = DeliveryStatusRetrying
	_ = s.storage.UpdateNotificationStatus(s.ctx, notification.ID, DeliveryStatusRetrying, "")

	// Deliver notification
	start := time.Now()
	result, err := handler.Deliver(s.ctx, notification, setting)
	duration := time.Since(start).Milliseconds()

	// Record history
	history := &DeliveryHistory{
		NotificationID: notification.ID,
		SettingID:      setting.ID,
		Attempt:        notification.RetryCount + 1,
		Result:         result,
		Timestamp:      time.Now(),
	}
	_ = s.storage.SaveDeliveryHistory(s.ctx, history)

	if err != nil || (result != nil && !result.Success) {
		s.handleDeliveryFailure(notification, result, err)
	} else {
		s.handleDeliverySuccess(notification, result, duration)
	}
}

// handleDeliverySuccess handles successful delivery.
func (s *NotificationService) handleDeliverySuccess(notification *Notification, result *DeliveryResult, durationMs int64) {
	now := time.Now()
	notification.Status = DeliveryStatusSent
	notification.SentAt = &now

	_ = s.storage.UpdateNotificationStatus(s.ctx, notification.ID, DeliveryStatusSent, "")
	_ = s.storage.IncrementStats(s.ctx, notification.SettingID, true, durationMs)

	s.logger.Debug("notification delivered",
		zap.String("notification_id", notification.ID),
		zap.Int64("duration_ms", durationMs))
}

// handleDeliveryFailure handles failed delivery.
func (s *NotificationService) handleDeliveryFailure(notification *Notification, result *DeliveryResult, err error) {
	notification.RetryCount++

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	} else if result != nil {
		errMsg = result.Error
	}
	notification.Error = errMsg

	if notification.RetryCount >= s.config.Retry.MaxAttempts {
		notification.Status = DeliveryStatusFailed
		_ = s.storage.UpdateNotificationStatus(s.ctx, notification.ID, DeliveryStatusFailed, errMsg)
		_ = s.storage.IncrementStats(s.ctx, notification.SettingID, false, 0)
		s.logger.Warn("notification delivery failed permanently",
			zap.String("notification_id", notification.ID),
			zap.Int("attempts", notification.RetryCount),
			zap.String("error", errMsg))
	} else {
		// Schedule retry
		delay := s.calculateRetryDelay(notification.RetryCount)
		nextRetry := time.Now().Add(delay)
		notification.NextRetry = &nextRetry
		notification.Status = DeliveryStatusRetrying
		_ = s.storage.UpdateNotificationStatus(s.ctx, notification.ID, DeliveryStatusRetrying, errMsg)
		s.logger.Debug("scheduling notification retry",
			zap.String("notification_id", notification.ID),
			zap.Int("attempt", notification.RetryCount),
			zap.Duration("delay", delay))
	}
}

// calculateRetryDelay calculates the delay for a retry attempt.
func (s *NotificationService) calculateRetryDelay(attempt int) time.Duration {
	delay := s.config.Retry.InitialDelay
	for i := 1; i < attempt; i++ {
		delay = time.Duration(float64(delay) * s.config.Retry.Multiplier)
	}
	if delay > s.config.Retry.MaxDelay {
		delay = s.config.Retry.MaxDelay
	}
	return delay
}

// retryProcessor periodically checks for notifications to retry.
func (s *NotificationService) retryProcessor() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.Queue.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.processRetries()
		}
	}
}

// processRetries processes pending retries.
func (s *NotificationService) processRetries() {
	notifications, err := s.storage.GetPendingNotifications(s.ctx, s.config.Queue.BatchSize)
	if err != nil {
		s.logger.Error("failed to get pending notifications", zap.Error(err))
		return
	}

	now := time.Now()
	for _, notification := range notifications {
		if notification.NextRetry != nil && notification.NextRetry.Before(now) {
			s.enqueueNotification(notification)
		}
	}
}

// cleanupProcessor periodically cleans up old history.
func (s *NotificationService) cleanupProcessor() {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			before := time.Now().Add(-s.config.Storage.HistoryRetention)
			count, err := s.storage.CleanupOldHistory(s.ctx, before)
			if err != nil {
				s.logger.Error("failed to cleanup old history", zap.Error(err))
			} else if count > 0 {
				s.logger.Info("cleaned up old notification history", zap.Int64("count", count))
			}
		}
	}
}

// CreateSetting creates a new notification setting.
func (s *NotificationService) CreateSetting(ctx context.Context, setting *NotificationSetting) (*NotificationSetting, error) {
	if setting.ID == "" {
		setting.ID = uuid.New().String()
	}
	setting.CreatedAt = time.Now()
	setting.UpdatedAt = time.Now()

	// Validate with handler
	handler, ok := s.handlers[setting.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported notification type: %s", setting.Type)
	}
	if err := handler.Validate(setting); err != nil {
		return nil, fmt.Errorf("invalid setting: %w", err)
	}

	if err := s.storage.SaveSetting(ctx, setting); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.settings[setting.ID] = setting
	s.mu.Unlock()

	s.logger.Info("created notification setting",
		zap.String("id", setting.ID),
		zap.String("type", string(setting.Type)))

	return setting, nil
}

// UpdateSetting updates an existing notification setting.
func (s *NotificationService) UpdateSetting(ctx context.Context, setting *NotificationSetting) (*NotificationSetting, error) {
	existing, err := s.storage.GetSetting(ctx, setting.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("setting not found: %s", setting.ID)
	}

	setting.CreatedAt = existing.CreatedAt
	setting.UpdatedAt = time.Now()

	// Validate with handler
	handler, ok := s.handlers[setting.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported notification type: %s", setting.Type)
	}
	if err := handler.Validate(setting); err != nil {
		return nil, fmt.Errorf("invalid setting: %w", err)
	}

	if err := s.storage.SaveSetting(ctx, setting); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.settings[setting.ID] = setting
	s.mu.Unlock()

	s.logger.Info("updated notification setting",
		zap.String("id", setting.ID))

	return setting, nil
}

// DeleteSetting deletes a notification setting.
func (s *NotificationService) DeleteSetting(ctx context.Context, id string) error {
	if err := s.storage.DeleteSetting(ctx, id); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.settings, id)
	s.mu.Unlock()

	s.logger.Info("deleted notification setting", zap.String("id", id))
	return nil
}

// GetSetting returns a notification setting by ID.
func (s *NotificationService) GetSetting(ctx context.Context, id string) (*NotificationSetting, error) {
	return s.storage.GetSetting(ctx, id)
}

// ListSettings returns notification settings matching the filter.
func (s *NotificationService) ListSettings(ctx context.Context, filter *SettingsFilter) ([]*NotificationSetting, error) {
	return s.storage.ListSettings(ctx, filter)
}

// GetNotification returns a notification by ID.
func (s *NotificationService) GetNotification(ctx context.Context, id string) (*Notification, error) {
	return s.storage.GetNotification(ctx, id)
}

// ListNotifications returns notifications matching the filter.
func (s *NotificationService) ListNotifications(ctx context.Context, filter *NotificationsFilter) ([]*Notification, error) {
	return s.storage.ListNotifications(ctx, filter)
}

// RetryNotification retries a failed notification.
func (s *NotificationService) RetryNotification(ctx context.Context, id string) error {
	notification, err := s.storage.GetNotification(ctx, id)
	if err != nil {
		return err
	}
	if notification == nil {
		return fmt.Errorf("notification not found: %s", id)
	}

	notification.RetryCount = 0
	notification.Status = DeliveryStatusPending
	notification.NextRetry = nil
	notification.Error = ""

	s.enqueueNotification(notification)
	return nil
}

// CancelNotification cancels a pending notification.
func (s *NotificationService) CancelNotification(ctx context.Context, id string) error {
	return s.storage.UpdateNotificationStatus(ctx, id, DeliveryStatusCancelled, "cancelled by user")
}

// GetStats returns statistics for a notification setting.
func (s *NotificationService) GetStats(ctx context.Context, settingID string) (*NotificationStats, error) {
	return s.storage.GetStats(ctx, settingID)
}

// GetDeliveryHistory returns delivery history for a notification.
func (s *NotificationService) GetDeliveryHistory(ctx context.Context, notificationID string) ([]*DeliveryHistory, error) {
	return s.storage.GetDeliveryHistory(ctx, notificationID)
}

// TestSetting tests a notification setting with a sample event.
func (s *NotificationService) TestSetting(ctx context.Context, id string) (*DeliveryResult, error) {
	setting, err := s.storage.GetSetting(ctx, id)
	if err != nil {
		return nil, err
	}
	if setting == nil {
		return nil, fmt.Errorf("setting not found: %s", id)
	}

	handler, ok := s.handlers[setting.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported notification type: %s", setting.Type)
	}

	// Create a test notification
	testNotification := &Notification{
		ID:        uuid.New().String(),
		SettingID: setting.ID,
		Type:      setting.Type,
		EventType: EventTypeBlock,
		Payload: &EventPayload{
			ChainID:     1,
			BlockNumber: 12345678,
			BlockHash:   [32]byte{0x01, 0x02, 0x03},
			Timestamp:   time.Now(),
			EventType:   EventTypeBlock,
			Data:        json.RawMessage(`{"test": true, "message": "This is a test notification"}`),
		},
		Status:    DeliveryStatusPending,
		CreatedAt: time.Now(),
	}

	return handler.Deliver(ctx, testNotification, setting)
}
