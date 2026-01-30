package notifications

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
	"go.uber.org/zap"
)

// mockStorage implements Storage interface for testing
type mockStorage struct {
	settings      map[string]*NotificationSetting
	notifications map[string]*Notification
	history       map[string][]*DeliveryHistory
	stats         map[string]*NotificationStats

	// Error injection
	saveSettingErr              error
	getSettingErr               error
	deleteSettingErr            error
	listSettingsErr             error
	saveNotifErr                error
	getNotificationErr          error
	updateNotificationStatusErr error
	listNotificationsErr        error
	getPendingErr               error
	saveHistoryErr              error
	getDeliveryHistoryErr       error
	getStatsErr                 error
	incrementStatsErr           error
	cleanupHistoryErr           error
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		settings:      make(map[string]*NotificationSetting),
		notifications: make(map[string]*Notification),
		history:       make(map[string][]*DeliveryHistory),
		stats:         make(map[string]*NotificationStats),
	}
}

func (m *mockStorage) SaveSetting(ctx context.Context, setting *NotificationSetting) error {
	if m.saveSettingErr != nil {
		return m.saveSettingErr
	}
	m.settings[setting.ID] = setting
	return nil
}

func (m *mockStorage) GetSetting(ctx context.Context, id string) (*NotificationSetting, error) {
	if m.getSettingErr != nil {
		return nil, m.getSettingErr
	}
	setting, ok := m.settings[id]
	if !ok {
		return nil, nil
	}
	return setting, nil
}

func (m *mockStorage) DeleteSetting(ctx context.Context, id string) error {
	if m.deleteSettingErr != nil {
		return m.deleteSettingErr
	}
	delete(m.settings, id)
	return nil
}

func (m *mockStorage) ListSettings(ctx context.Context, filter *SettingsFilter) ([]*NotificationSetting, error) {
	if m.listSettingsErr != nil {
		return nil, m.listSettingsErr
	}
	result := make([]*NotificationSetting, 0, len(m.settings))
	for _, s := range m.settings {
		if filter != nil {
			if len(filter.Types) > 0 {
				found := false
				for _, t := range filter.Types {
					if s.Type == t {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			if filter.Enabled != nil && s.Enabled != *filter.Enabled {
				continue
			}
		}
		result = append(result, s)
	}
	return result, nil
}

func (m *mockStorage) SaveNotification(ctx context.Context, notification *Notification) error {
	if m.saveNotifErr != nil {
		return m.saveNotifErr
	}
	m.notifications[notification.ID] = notification
	return nil
}

func (m *mockStorage) GetNotification(ctx context.Context, id string) (*Notification, error) {
	if m.getNotificationErr != nil {
		return nil, m.getNotificationErr
	}
	notification, ok := m.notifications[id]
	if !ok {
		return nil, nil
	}
	return notification, nil
}

func (m *mockStorage) UpdateNotificationStatus(ctx context.Context, id string, status DeliveryStatus, errMsg string) error {
	if m.updateNotificationStatusErr != nil {
		return m.updateNotificationStatusErr
	}
	if notification, ok := m.notifications[id]; ok {
		notification.Status = status
		notification.Error = errMsg
	}
	return nil
}

func (m *mockStorage) ListNotifications(ctx context.Context, filter *NotificationsFilter) ([]*Notification, error) {
	if m.listNotificationsErr != nil {
		return nil, m.listNotificationsErr
	}
	result := make([]*Notification, 0, len(m.notifications))
	for _, n := range m.notifications {
		if filter != nil {
			if len(filter.Status) > 0 {
				found := false
				for _, s := range filter.Status {
					if n.Status == s {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			if filter.SettingID != "" && n.SettingID != filter.SettingID {
				continue
			}
		}
		result = append(result, n)
	}
	return result, nil
}

func (m *mockStorage) GetPendingNotifications(ctx context.Context, limit int) ([]*Notification, error) {
	if m.getPendingErr != nil {
		return nil, m.getPendingErr
	}
	var result []*Notification
	for _, n := range m.notifications {
		if n.Status == DeliveryStatusPending {
			result = append(result, n)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *mockStorage) SaveDeliveryHistory(ctx context.Context, history *DeliveryHistory) error {
	if m.saveHistoryErr != nil {
		return m.saveHistoryErr
	}
	m.history[history.NotificationID] = append(m.history[history.NotificationID], history)
	return nil
}

func (m *mockStorage) GetDeliveryHistory(ctx context.Context, notificationID string) ([]*DeliveryHistory, error) {
	if m.getDeliveryHistoryErr != nil {
		return nil, m.getDeliveryHistoryErr
	}
	return m.history[notificationID], nil
}

func (m *mockStorage) GetStats(ctx context.Context, settingID string) (*NotificationStats, error) {
	if m.getStatsErr != nil {
		return nil, m.getStatsErr
	}
	stats, ok := m.stats[settingID]
	if !ok {
		return &NotificationStats{SettingID: settingID}, nil
	}
	return stats, nil
}

func (m *mockStorage) IncrementStats(ctx context.Context, settingID string, success bool, deliveryMs int64) error {
	if m.incrementStatsErr != nil {
		return m.incrementStatsErr
	}
	stats, ok := m.stats[settingID]
	if !ok {
		stats = &NotificationStats{SettingID: settingID}
		m.stats[settingID] = stats
	}
	if success {
		stats.TotalSent++
	} else {
		stats.TotalFailed++
	}
	return nil
}

func (m *mockStorage) CleanupOldHistory(ctx context.Context, before time.Time) (int64, error) {
	if m.cleanupHistoryErr != nil {
		return 0, m.cleanupHistoryErr
	}
	var count int64
	for id, historyList := range m.history {
		var kept []*DeliveryHistory
		for _, h := range historyList {
			if h.Timestamp.Before(before) {
				count++
			} else {
				kept = append(kept, h)
			}
		}
		m.history[id] = kept
	}
	return count, nil
}

func TestNewService(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(config, storage, eventBus, logger)
	if service == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestNewService_NilConfig(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(nil, storage, eventBus, logger)
	if service == nil {
		t.Fatal("expected non-nil service")
	}
	// Should use default config
	if service.config == nil {
		t.Error("expected default config to be used")
	}
}

func TestNotificationService_RegisterHandler(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(config, storage, eventBus, logger)

	// Register webhook handler
	webhookHandler := NewWebhookHandler(nil, logger)
	service.RegisterHandler(webhookHandler)

	// Handler should be registered
	if len(service.handlers) != 1 {
		t.Errorf("expected 1 handler, got %d", len(service.handlers))
	}
}

func TestNotificationService_CreateSetting(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	config.Enabled = true
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(config, storage, eventBus, logger)
	service.RegisterHandler(NewWebhookHandler(nil, logger))

	ctx := context.Background()

	t.Run("create valid setting", func(t *testing.T) {
		setting := &NotificationSetting{
			Name:    "Test Webhook",
			Type:    NotificationTypeWebhook,
			Enabled: true,
			Destination: Destination{
				WebhookURL: "https://example.com/webhook",
			},
		}

		created, err := service.CreateSetting(ctx, setting)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if created == nil {
			t.Fatal("expected non-nil setting")
		}
		if created.ID == "" {
			t.Error("expected ID to be generated")
		}
		if created.CreatedAt.IsZero() {
			t.Error("expected CreatedAt to be set")
		}
	})

	t.Run("create setting with empty name", func(t *testing.T) {
		setting := &NotificationSetting{
			Name: "",
			Type: NotificationTypeWebhook,
		}

		_, err := service.CreateSetting(ctx, setting)
		if err == nil {
			t.Error("expected error for empty name")
		}
	})

	t.Run("create setting with invalid type", func(t *testing.T) {
		setting := &NotificationSetting{
			Name: "Test",
			Type: NotificationType("invalid"),
		}

		_, err := service.CreateSetting(ctx, setting)
		if err == nil {
			t.Error("expected error for invalid type")
		}
	})

	t.Run("create setting with no handler registered", func(t *testing.T) {
		// Create service without handlers
		service2 := NewService(config, storage, eventBus, logger)

		setting := &NotificationSetting{
			Name: "Test Email",
			Type: NotificationTypeEmail,
			Destination: Destination{
				EmailTo: []string{"test@example.com"},
			},
		}

		_, err := service2.CreateSetting(ctx, setting)
		if err == nil {
			t.Error("expected error when handler not registered")
		}
	})
}

func TestNotificationService_UpdateSetting(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	config.Enabled = true
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(config, storage, eventBus, logger)
	service.RegisterHandler(NewWebhookHandler(nil, logger))

	ctx := context.Background()

	// First create a setting
	setting := &NotificationSetting{
		Name:    "Test Webhook",
		Type:    NotificationTypeWebhook,
		Enabled: true,
		Destination: Destination{
			WebhookURL: "https://example.com/webhook",
		},
	}
	created, _ := service.CreateSetting(ctx, setting)

	t.Run("update existing setting", func(t *testing.T) {
		created.Name = "Updated Webhook"
		updated, err := service.UpdateSetting(ctx, created)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if updated.Name != "Updated Webhook" {
			t.Errorf("expected name 'Updated Webhook', got %s", updated.Name)
		}
		if updated.UpdatedAt.IsZero() {
			t.Error("expected UpdatedAt to be set")
		}
	})

	t.Run("update non-existent setting", func(t *testing.T) {
		nonExistent := &NotificationSetting{
			ID:   "non-existent",
			Name: "Test",
			Type: NotificationTypeWebhook,
		}
		_, err := service.UpdateSetting(ctx, nonExistent)
		if err == nil {
			t.Error("expected error for non-existent setting")
		}
	})
}

func TestNotificationService_DeleteSetting(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	config.Enabled = true
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(config, storage, eventBus, logger)
	service.RegisterHandler(NewWebhookHandler(nil, logger))

	ctx := context.Background()

	// Create a setting
	setting := &NotificationSetting{
		Name:    "Test Webhook",
		Type:    NotificationTypeWebhook,
		Enabled: true,
		Destination: Destination{
			WebhookURL: "https://example.com/webhook",
		},
	}
	created, _ := service.CreateSetting(ctx, setting)

	t.Run("delete existing setting", func(t *testing.T) {
		err := service.DeleteSetting(ctx, created.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify it's deleted
		retrieved, _ := service.GetSetting(ctx, created.ID)
		if retrieved != nil {
			t.Error("expected setting to be deleted")
		}
	})

	t.Run("delete non-existent setting", func(t *testing.T) {
		err := service.DeleteSetting(ctx, "non-existent")
		// Should not error for non-existent
		if err != nil {
			t.Logf("delete non-existent returned: %v", err)
		}
	})
}

func TestNotificationService_GetSetting(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(config, storage, eventBus, logger)
	ctx := context.Background()

	// Pre-populate storage
	setting := &NotificationSetting{
		ID:   "test-setting-001",
		Name: "Test",
		Type: NotificationTypeWebhook,
	}
	storage.settings[setting.ID] = setting

	t.Run("get existing setting", func(t *testing.T) {
		retrieved, err := service.GetSetting(ctx, "test-setting-001")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if retrieved == nil {
			t.Fatal("expected non-nil setting")
		}
		if retrieved.ID != "test-setting-001" {
			t.Errorf("expected ID 'test-setting-001', got %s", retrieved.ID)
		}
	})

	t.Run("get non-existent setting", func(t *testing.T) {
		retrieved, err := service.GetSetting(ctx, "non-existent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if retrieved != nil {
			t.Error("expected nil for non-existent setting")
		}
	})
}

func TestNotificationService_ListSettings(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(config, storage, eventBus, logger)
	ctx := context.Background()

	// Pre-populate storage
	storage.settings["setting-1"] = &NotificationSetting{
		ID:      "setting-1",
		Type:    NotificationTypeWebhook,
		Enabled: true,
	}
	storage.settings["setting-2"] = &NotificationSetting{
		ID:      "setting-2",
		Type:    NotificationTypeEmail,
		Enabled: false,
	}

	t.Run("list all settings", func(t *testing.T) {
		settings, err := service.ListSettings(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(settings) != 2 {
			t.Errorf("expected 2 settings, got %d", len(settings))
		}
	})

	t.Run("list with filter", func(t *testing.T) {
		filter := &SettingsFilter{
			Types: []NotificationType{NotificationTypeWebhook},
		}
		settings, err := service.ListSettings(ctx, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(settings) != 1 {
			t.Errorf("expected 1 setting, got %d", len(settings))
		}
	})
}

func TestNotificationService_GetNotification(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(config, storage, eventBus, logger)
	ctx := context.Background()

	// Pre-populate storage
	storage.notifications["notif-001"] = &Notification{
		ID:     "notif-001",
		Status: DeliveryStatusPending,
	}

	t.Run("get existing notification", func(t *testing.T) {
		notif, err := service.GetNotification(ctx, "notif-001")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if notif == nil {
			t.Fatal("expected non-nil notification")
		}
	})

	t.Run("get non-existent notification", func(t *testing.T) {
		notif, err := service.GetNotification(ctx, "non-existent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if notif != nil {
			t.Error("expected nil for non-existent notification")
		}
	})
}

func TestNotificationService_ListNotifications(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(config, storage, eventBus, logger)
	ctx := context.Background()

	// Pre-populate storage
	storage.notifications["notif-001"] = &Notification{
		ID:        "notif-001",
		SettingID: "setting-001",
		Status:    DeliveryStatusPending,
	}
	storage.notifications["notif-002"] = &Notification{
		ID:        "notif-002",
		SettingID: "setting-001",
		Status:    DeliveryStatusSent,
	}

	t.Run("list all notifications", func(t *testing.T) {
		notifications, err := service.ListNotifications(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(notifications) != 2 {
			t.Errorf("expected 2 notifications, got %d", len(notifications))
		}
	})

	t.Run("list with status filter", func(t *testing.T) {
		filter := &NotificationsFilter{
			Status: []DeliveryStatus{DeliveryStatusPending},
		}
		notifications, err := service.ListNotifications(ctx, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(notifications) != 1 {
			t.Errorf("expected 1 notification, got %d", len(notifications))
		}
	})
}

func TestNotificationService_GetStats(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(config, storage, eventBus, logger)
	ctx := context.Background()

	// Pre-populate stats
	storage.stats["setting-001"] = &NotificationStats{
		SettingID:   "setting-001",
		TotalSent:   100,
		TotalFailed: 5,
		SuccessRate: 95.0,
	}

	t.Run("get existing stats", func(t *testing.T) {
		stats, err := service.GetStats(ctx, "setting-001")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if stats.TotalSent != 100 {
			t.Errorf("expected TotalSent 100, got %d", stats.TotalSent)
		}
	})

	t.Run("get non-existent stats returns empty", func(t *testing.T) {
		stats, err := service.GetStats(ctx, "non-existent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if stats == nil {
			t.Fatal("expected non-nil stats")
		}
		if stats.TotalSent != 0 {
			t.Errorf("expected TotalSent 0, got %d", stats.TotalSent)
		}
	})
}

func TestNotificationService_GetDeliveryHistory(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(config, storage, eventBus, logger)
	ctx := context.Background()

	// Pre-populate history
	storage.history["notif-001"] = []*DeliveryHistory{
		{NotificationID: "notif-001", Attempt: 1, Result: &DeliveryResult{Success: true}},
		{NotificationID: "notif-001", Attempt: 2, Result: &DeliveryResult{Success: false}},
	}

	t.Run("get existing history", func(t *testing.T) {
		history, err := service.GetDeliveryHistory(ctx, "notif-001")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(history) != 2 {
			t.Errorf("expected 2 history entries, got %d", len(history))
		}
	})

	t.Run("get non-existent history returns empty", func(t *testing.T) {
		history, err := service.GetDeliveryHistory(ctx, "non-existent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(history) != 0 {
			t.Errorf("expected 0 history entries, got %d", len(history))
		}
	})
}

func TestNotificationService_CancelNotification(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(config, storage, eventBus, logger)
	ctx := context.Background()

	// Pre-populate notification
	storage.notifications["notif-001"] = &Notification{
		ID:     "notif-001",
		Status: DeliveryStatusPending,
	}

	t.Run("cancel pending notification", func(t *testing.T) {
		err := service.CancelNotification(ctx, "notif-001")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify status changed
		notif := storage.notifications["notif-001"]
		if notif.Status != DeliveryStatusCancelled {
			t.Errorf("expected status Cancelled, got %s", notif.Status)
		}
	})
}

func TestNotificationService_RetryNotification(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	config.Enabled = true
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(config, storage, eventBus, logger)
	ctx := context.Background()

	// Pre-populate notification and setting
	storage.settings["setting-001"] = &NotificationSetting{
		ID:      "setting-001",
		Type:    NotificationTypeWebhook,
		Enabled: true,
	}
	storage.notifications["notif-001"] = &Notification{
		ID:        "notif-001",
		SettingID: "setting-001",
		Status:    DeliveryStatusFailed,
	}

	t.Run("retry failed notification", func(t *testing.T) {
		err := service.RetryNotification(ctx, "notif-001")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Status should be reset to pending
		notif := storage.notifications["notif-001"]
		if notif.Status != DeliveryStatusPending {
			t.Errorf("expected status Pending, got %s", notif.Status)
		}
	})

	t.Run("retry non-existent notification", func(t *testing.T) {
		err := service.RetryNotification(ctx, "non-existent")
		if err == nil {
			t.Error("expected error for non-existent notification")
		}
	})
}

func TestNotificationService_StorageErrors(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	config.Enabled = true
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)

	service := NewService(config, storage, eventBus, logger)
	service.RegisterHandler(NewWebhookHandler(nil, logger))
	ctx := context.Background()

	t.Run("create setting with storage error", func(t *testing.T) {
		storage.saveSettingErr = errors.New("storage error")
		defer func() { storage.saveSettingErr = nil }()

		setting := &NotificationSetting{
			Name: "Test",
			Type: NotificationTypeWebhook,
			Destination: Destination{
				WebhookURL: "https://example.com/webhook",
			},
		}
		_, err := service.CreateSetting(ctx, setting)
		if err == nil {
			t.Error("expected storage error")
		}
	})

	t.Run("list settings with storage error", func(t *testing.T) {
		storage.listSettingsErr = errors.New("storage error")
		defer func() { storage.listSettingsErr = nil }()

		_, err := service.ListSettings(ctx, nil)
		if err == nil {
			t.Error("expected storage error")
		}
	})
}

func TestConvertEventType(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	service := NewService(config, newMockStorage(), events.NewEventBus(100, 100), logger)

	tests := []struct {
		input    events.EventType
		expected EventType
	}{
		{events.EventTypeBlock, EventTypeBlock},
		{events.EventTypeTransaction, EventTypeTransaction},
		{events.EventTypeLog, EventTypeLog},
		{events.EventType("unknown"), EventType("unknown")},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			result := service.convertEventType(tt.input)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestCalculateRetryDelay(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	config.Retry.InitialDelay = 1 * time.Second
	config.Retry.MaxDelay = 1 * time.Minute
	config.Retry.Multiplier = 2.0

	service := NewService(config, newMockStorage(), events.NewEventBus(100, 100), logger)

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{10, 1 * time.Minute}, // Should cap at MaxDelay
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			delay := service.calculateRetryDelay(tt.attempt)
			if delay != tt.expected {
				t.Errorf("attempt %d: expected %v, got %v", tt.attempt, tt.expected, delay)
			}
		})
	}
}

func TestNotificationService_TestSetting(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)
	service := NewService(config, storage, eventBus, logger)

	// Create a mock handler for testing
	mockHandler := &mockNotificationHandler{
		handlerType: NotificationTypeWebhook,
		validateErr: nil,
		deliverResult: &DeliveryResult{
			Success:      true,
			StatusCode:   200,
			ResponseBody: "test response",
		},
	}
	service.RegisterHandler(mockHandler)

	// Create a test setting
	setting := &NotificationSetting{
		ID:      "test-setting-id",
		Name:    "Test Setting",
		Type:    NotificationTypeWebhook,
		Enabled: true,
		Destination: Destination{
			WebhookURL: "https://example.com/webhook",
		},
	}
	storage.settings[setting.ID] = setting

	ctx := context.Background()

	t.Run("test setting successfully", func(t *testing.T) {
		result, err := service.TestSetting(ctx, setting.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if !result.Success {
			t.Error("expected success")
		}
	})

	t.Run("test non-existent setting", func(t *testing.T) {
		_, err := service.TestSetting(ctx, "non-existent")
		if err == nil {
			t.Error("expected error for non-existent setting")
		}
	})

	t.Run("test setting with unsupported type", func(t *testing.T) {
		unsupportedSetting := &NotificationSetting{
			ID:   "unsupported-setting",
			Name: "Unsupported",
			Type: NotificationType("unsupported"),
		}
		storage.settings[unsupportedSetting.ID] = unsupportedSetting

		_, err := service.TestSetting(ctx, unsupportedSetting.ID)
		if err == nil {
			t.Error("expected error for unsupported type")
		}
	})

	t.Run("test setting with storage error", func(t *testing.T) {
		storage.getSettingErr = errors.New("storage error")
		defer func() { storage.getSettingErr = nil }()

		_, err := service.TestSetting(ctx, setting.ID)
		if err == nil {
			t.Error("expected storage error")
		}
	})

	t.Run("test setting with delivery failure", func(t *testing.T) {
		mockHandler.deliverResult = &DeliveryResult{
			Success:    false,
			StatusCode: 500,
			Error:      "delivery failed",
		}
		mockHandler.deliverErr = errors.New("delivery error")
		defer func() {
			mockHandler.deliverResult = &DeliveryResult{Success: true, StatusCode: 200}
			mockHandler.deliverErr = nil
		}()

		result, err := service.TestSetting(ctx, setting.ID)
		if err == nil {
			t.Error("expected delivery error")
		}
		if result != nil && result.Success {
			t.Error("expected failure result")
		}
	})
}

// mockNotificationHandler implements NotificationHandler for testing
type mockNotificationHandler struct {
	handlerType   NotificationType
	validateErr   error
	deliverResult *DeliveryResult
	deliverErr    error
}

func (m *mockNotificationHandler) Type() NotificationType {
	return m.handlerType
}

func (m *mockNotificationHandler) Validate(setting *NotificationSetting) error {
	return m.validateErr
}

func (m *mockNotificationHandler) Deliver(ctx context.Context, notification *Notification, setting *NotificationSetting) (*DeliveryResult, error) {
	return m.deliverResult, m.deliverErr
}

func TestNotificationService_UpdateSetting_EdgeCases(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)
	service := NewService(config, storage, eventBus, logger)

	mockHandler := &mockNotificationHandler{
		handlerType: NotificationTypeWebhook,
	}
	service.RegisterHandler(mockHandler)

	ctx := context.Background()

	t.Run("update with storage error on get", func(t *testing.T) {
		storage.getSettingErr = errors.New("get error")
		defer func() { storage.getSettingErr = nil }()

		update := &NotificationSetting{ID: "test-id", Name: "Updated"}
		_, err := service.UpdateSetting(ctx, update)
		if err == nil {
			t.Error("expected get error")
		}
	})

	t.Run("update with storage error on save", func(t *testing.T) {
		setting := &NotificationSetting{
			ID:      "update-test",
			Name:    "Original",
			Type:    NotificationTypeWebhook,
			Enabled: true,
		}
		storage.settings[setting.ID] = setting

		storage.saveSettingErr = errors.New("save error")
		defer func() { storage.saveSettingErr = nil }()

		update := &NotificationSetting{ID: setting.ID, Name: "Updated"}
		_, err := service.UpdateSetting(ctx, update)
		if err == nil {
			t.Error("expected save error")
		}
	})
}

func TestNotificationService_DeleteSetting_EdgeCases(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)
	service := NewService(config, storage, eventBus, logger)

	ctx := context.Background()

	t.Run("delete with storage error", func(t *testing.T) {
		storage.deleteSettingErr = errors.New("delete error")
		defer func() { storage.deleteSettingErr = nil }()

		err := service.DeleteSetting(ctx, "test-id")
		if err == nil {
			t.Error("expected delete error")
		}
	})
}

func TestNotificationService_RetryNotification_EdgeCases(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)
	service := NewService(config, storage, eventBus, logger)

	ctx := context.Background()

	t.Run("retry with storage error on get", func(t *testing.T) {
		storage.getNotificationErr = errors.New("get error")
		defer func() { storage.getNotificationErr = nil }()

		err := service.RetryNotification(ctx, "test-id")
		if err == nil {
			t.Error("expected get error")
		}
	})

	t.Run("retry pending notification succeeds", func(t *testing.T) {
		notification := &Notification{
			ID:     "pending-notif",
			Status: DeliveryStatusPending,
		}
		storage.notifications[notification.ID] = notification

		err := service.RetryNotification(ctx, notification.ID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("retry sent notification succeeds", func(t *testing.T) {
		notification := &Notification{
			ID:     "sent-notif",
			Status: DeliveryStatusSent,
		}
		storage.notifications[notification.ID] = notification

		err := service.RetryNotification(ctx, notification.ID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestNotificationService_CancelNotification_EdgeCases(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)
	service := NewService(config, storage, eventBus, logger)

	ctx := context.Background()

	t.Run("cancel non-existent notification succeeds", func(t *testing.T) {
		// CancelNotification directly updates status in storage
		// It doesn't check if notification exists first
		err := service.CancelNotification(ctx, "non-existent")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("cancel sent notification succeeds", func(t *testing.T) {
		// CancelNotification doesn't check current status
		// It just updates the status to cancelled
		notification := &Notification{
			ID:     "sent-notif-cancel",
			Status: DeliveryStatusSent,
		}
		storage.notifications[notification.ID] = notification

		err := service.CancelNotification(ctx, notification.ID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		// Verify status was updated
		if storage.notifications[notification.ID].Status != DeliveryStatusCancelled {
			t.Error("expected status to be cancelled")
		}
	})

	t.Run("cancel with update error", func(t *testing.T) {
		notification := &Notification{
			ID:     "cancel-test",
			Status: DeliveryStatusPending,
		}
		storage.notifications[notification.ID] = notification
		storage.updateNotificationStatusErr = errors.New("update error")
		defer func() { storage.updateNotificationStatusErr = nil }()

		err := service.CancelNotification(ctx, notification.ID)
		if err == nil {
			t.Error("expected update error")
		}
	})
}

func TestNotificationService_GetStats_EdgeCases(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)
	service := NewService(config, storage, eventBus, logger)

	ctx := context.Background()

	t.Run("get stats with storage error", func(t *testing.T) {
		storage.getStatsErr = errors.New("stats error")
		defer func() { storage.getStatsErr = nil }()

		_, err := service.GetStats(ctx, "test-setting")
		if err == nil {
			t.Error("expected stats error")
		}
	})
}

func TestNotificationService_GetDeliveryHistory_EdgeCases(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)
	service := NewService(config, storage, eventBus, logger)

	ctx := context.Background()

	t.Run("get history with storage error", func(t *testing.T) {
		storage.getDeliveryHistoryErr = errors.New("history error")
		defer func() { storage.getDeliveryHistoryErr = nil }()

		_, err := service.GetDeliveryHistory(ctx, "test-notif")
		if err == nil {
			t.Error("expected history error")
		}
	})
}

func TestNotificationService_GetNotification_EdgeCases(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)
	service := NewService(config, storage, eventBus, logger)

	ctx := context.Background()

	t.Run("get notification with storage error", func(t *testing.T) {
		storage.getNotificationErr = errors.New("notification error")
		defer func() { storage.getNotificationErr = nil }()

		_, err := service.GetNotification(ctx, "test-id")
		if err == nil {
			t.Error("expected notification error")
		}
	})
}

func TestNotificationService_ListNotifications_EdgeCases(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)
	service := NewService(config, storage, eventBus, logger)

	ctx := context.Background()

	t.Run("list notifications with storage error", func(t *testing.T) {
		storage.listNotificationsErr = errors.New("list error")
		defer func() { storage.listNotificationsErr = nil }()

		_, err := service.ListNotifications(ctx, nil)
		if err == nil {
			t.Error("expected list error")
		}
	})
}

func TestNotificationService_StartStop(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	config.Queue.Workers = 1 // Use fewer workers for testing
	storage := newMockStorage()
	eventBus := events.NewEventBus(100, 100)
	service := NewService(config, storage, eventBus, logger)

	ctx := context.Background()

	t.Run("start and stop service", func(t *testing.T) {
		err := service.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start service: %v", err)
		}

		// Give workers time to start
		time.Sleep(50 * time.Millisecond)

		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		err = service.Stop(stopCtx)
		if err != nil {
			t.Errorf("failed to stop service: %v", err)
		}
	})

	t.Run("start already running service", func(t *testing.T) {
		service2 := NewService(config, storage, eventBus, logger)

		err := service2.Start(ctx)
		if err != nil {
			t.Fatalf("first start failed: %v", err)
		}

		// Try to start again
		err = service2.Start(ctx)
		if err == nil {
			t.Error("expected error when starting already running service")
		}

		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		service2.Stop(stopCtx)
	})

	t.Run("stop not running service", func(t *testing.T) {
		service3 := NewService(config, storage, eventBus, logger)

		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		err := service3.Stop(stopCtx)
		if err != nil {
			t.Errorf("unexpected error stopping not-running service: %v", err)
		}
	})

	t.Run("start with invalid config", func(t *testing.T) {
		invalidConfig := &Config{
			Enabled: true,
			Email: EmailConfig{
				Enabled:  true,
				SMTPHost: "", // Invalid - missing SMTP host
			},
		}
		service4 := NewService(invalidConfig, storage, eventBus, logger)

		err := service4.Start(ctx)
		if err == nil {
			t.Error("expected error with invalid config")
		}
	})
}
