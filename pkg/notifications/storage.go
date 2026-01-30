package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/0xmhha/indexer-go/pkg/storage"
)

// PebbleStorage implements the Storage interface using PebbleDB.
type PebbleStorage struct {
	store storage.KVStore
}

// NewPebbleStorage creates a new PebbleStorage.
func NewPebbleStorage(store storage.KVStore) *PebbleStorage {
	return &PebbleStorage{store: store}
}

// SaveSetting saves a notification setting.
func (s *PebbleStorage) SaveSetting(ctx context.Context, setting *NotificationSetting) error {
	data, err := json.Marshal(setting)
	if err != nil {
		return fmt.Errorf("failed to marshal setting: %w", err)
	}

	key := storage.NotificationSettingKey(setting.ID)
	return s.store.Put(ctx, key, data)
}

// GetSetting returns a notification setting by ID.
func (s *PebbleStorage) GetSetting(ctx context.Context, id string) (*NotificationSetting, error) {
	key := storage.NotificationSettingKey(id)
	data, err := s.store.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get setting: %w", err)
	}
	if data == nil {
		return nil, nil
	}

	var setting NotificationSetting
	if err := json.Unmarshal(data, &setting); err != nil {
		return nil, fmt.Errorf("failed to unmarshal setting: %w", err)
	}

	return &setting, nil
}

// DeleteSetting deletes a notification setting.
func (s *PebbleStorage) DeleteSetting(ctx context.Context, id string) error {
	key := storage.NotificationSettingKey(id)
	return s.store.Delete(ctx, key)
}

// ListSettings returns notification settings matching the filter.
func (s *PebbleStorage) ListSettings(ctx context.Context, filter *SettingsFilter) ([]*NotificationSetting, error) {
	prefix := storage.NotificationSettingKeyPrefix()

	var settings []*NotificationSetting
	count := 0
	offset := 0
	if filter != nil {
		offset = filter.Offset
	}
	limit := 100
	if filter != nil && filter.Limit > 0 {
		limit = filter.Limit
	}

	err := s.store.Iterate(ctx, prefix, func(key, value []byte) bool {
		// Skip items before offset
		if count < offset {
			count++
			return true
		}

		// Check limit
		if len(settings) >= limit {
			return false
		}

		var setting NotificationSetting
		if err := json.Unmarshal(value, &setting); err != nil {
			return true // Skip invalid entries
		}

		// Apply filters
		if filter != nil {
			if filter.Enabled != nil && setting.Enabled != *filter.Enabled {
				return true
			}
			if len(filter.Types) > 0 {
				found := false
				for _, t := range filter.Types {
					if t == setting.Type {
						found = true
						break
					}
				}
				if !found {
					return true
				}
			}
		}

		settings = append(settings, &setting)
		count++
		return true
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list settings: %w", err)
	}

	return settings, nil
}

// SaveNotification saves a notification.
func (s *PebbleStorage) SaveNotification(ctx context.Context, notification *Notification) error {
	data, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	key := storage.NotificationKey(notification.ID)
	if err := s.store.Put(ctx, key, data); err != nil {
		return err
	}

	// Create status index
	statusKey := storage.NotificationStatusIndexKey(
		string(notification.Status),
		notification.CreatedAt.UnixNano(),
		notification.ID,
	)
	if err := s.store.Put(ctx, statusKey, []byte(notification.ID)); err != nil {
		return err
	}

	// Create setting index
	settingKey := storage.NotificationSettingIndexKey(
		notification.SettingID,
		notification.CreatedAt.UnixNano(),
		notification.ID,
	)
	if err := s.store.Put(ctx, settingKey, []byte(notification.ID)); err != nil {
		return err
	}

	// Create pending index if applicable
	if notification.Status == DeliveryStatusPending || notification.Status == DeliveryStatusRetrying {
		nextRetry := notification.CreatedAt.UnixNano()
		if notification.NextRetry != nil {
			nextRetry = notification.NextRetry.UnixNano()
		}
		pendingKey := storage.NotificationPendingIndexKey(nextRetry, notification.ID)
		if err := s.store.Put(ctx, pendingKey, []byte(notification.ID)); err != nil {
			return err
		}
	}

	return nil
}

// GetNotification returns a notification by ID.
func (s *PebbleStorage) GetNotification(ctx context.Context, id string) (*Notification, error) {
	key := storage.NotificationKey(id)
	data, err := s.store.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get notification: %w", err)
	}
	if data == nil {
		return nil, nil
	}

	var notification Notification
	if err := json.Unmarshal(data, &notification); err != nil {
		return nil, fmt.Errorf("failed to unmarshal notification: %w", err)
	}

	return &notification, nil
}

// UpdateNotificationStatus updates a notification's status.
func (s *PebbleStorage) UpdateNotificationStatus(ctx context.Context, id string, status DeliveryStatus, errMsg string) error {
	notification, err := s.GetNotification(ctx, id)
	if err != nil {
		return err
	}
	if notification == nil {
		return fmt.Errorf("notification not found: %s", id)
	}

	oldStatus := notification.Status
	notification.Status = status
	notification.Error = errMsg

	if status == DeliveryStatusSent {
		now := time.Now()
		notification.SentAt = &now
	}

	// Save updated notification
	data, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}
	key := storage.NotificationKey(id)
	if err := s.store.Put(ctx, key, data); err != nil {
		return err
	}

	// Update status index (remove old, add new)
	oldStatusKey := storage.NotificationStatusIndexKey(
		string(oldStatus),
		notification.CreatedAt.UnixNano(),
		notification.ID,
	)
	_ = s.store.Delete(ctx, oldStatusKey)

	newStatusKey := storage.NotificationStatusIndexKey(
		string(status),
		notification.CreatedAt.UnixNano(),
		notification.ID,
	)
	if err := s.store.Put(ctx, newStatusKey, []byte(notification.ID)); err != nil {
		return err
	}

	// Remove from pending index if not pending/retrying
	if status != DeliveryStatusPending && status != DeliveryStatusRetrying {
		nextRetry := notification.CreatedAt.UnixNano()
		if notification.NextRetry != nil {
			nextRetry = notification.NextRetry.UnixNano()
		}
		pendingKey := storage.NotificationPendingIndexKey(nextRetry, notification.ID)
		_ = s.store.Delete(ctx, pendingKey)
	}

	return nil
}

// ListNotifications returns notifications matching the filter.
func (s *PebbleStorage) ListNotifications(ctx context.Context, filter *NotificationsFilter) ([]*Notification, error) {
	var prefix []byte

	// Determine which index to use
	if filter != nil && filter.SettingID != "" {
		prefix = storage.NotificationSettingIndexKeyPrefix(filter.SettingID)
	} else if filter != nil && len(filter.Status) > 0 {
		prefix = storage.NotificationStatusIndexKeyPrefix(string(filter.Status[0]))
	} else {
		prefix = storage.NotificationKeyPrefix()
	}

	var notifications []*Notification
	count := 0
	offset := 0
	if filter != nil {
		offset = filter.Offset
	}
	limit := 100
	if filter != nil && filter.Limit > 0 {
		limit = filter.Limit
	}

	err := s.store.Iterate(ctx, prefix, func(key, value []byte) bool {
		// Skip items before offset
		if count < offset {
			count++
			return true
		}

		// Check limit
		if len(notifications) >= limit {
			return false
		}

		var notification *Notification
		var loadErr error

		// If using index, value is the notification ID
		if filter != nil && (filter.SettingID != "" || len(filter.Status) > 0) {
			notification, loadErr = s.GetNotification(ctx, string(value))
		} else {
			notification = &Notification{}
			loadErr = json.Unmarshal(value, notification)
		}

		if loadErr != nil {
			return true // Skip invalid entries
		}

		// Apply time filters
		if filter != nil {
			if filter.FromTime != nil && notification.CreatedAt.Before(*filter.FromTime) {
				return true
			}
			if filter.ToTime != nil && notification.CreatedAt.After(*filter.ToTime) {
				return true
			}
		}

		notifications = append(notifications, notification)
		count++
		return true
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list notifications: %w", err)
	}

	return notifications, nil
}

// GetPendingNotifications returns pending notifications ready for retry.
func (s *PebbleStorage) GetPendingNotifications(ctx context.Context, limit int) ([]*Notification, error) {
	prefix := storage.NotificationPendingIndexKeyPrefix()
	now := time.Now().UnixNano()

	var notifications []*Notification

	err := s.store.Iterate(ctx, prefix, func(key, value []byte) bool {
		if len(notifications) >= limit {
			return false
		}

		notification, err := s.GetNotification(ctx, string(value))
		if err != nil || notification == nil {
			return true
		}

		// Check if retry time has passed
		retryTime := notification.CreatedAt.UnixNano()
		if notification.NextRetry != nil {
			retryTime = notification.NextRetry.UnixNano()
		}

		if retryTime <= now {
			notifications = append(notifications, notification)
		}

		return true
	})

	if err != nil {
		return nil, fmt.Errorf("failed to get pending notifications: %w", err)
	}

	return notifications, nil
}

// SaveDeliveryHistory saves delivery history.
func (s *PebbleStorage) SaveDeliveryHistory(ctx context.Context, history *DeliveryHistory) error {
	data, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("failed to marshal history: %w", err)
	}

	key := storage.NotificationHistoryKey(history.NotificationID, history.Attempt)
	return s.store.Put(ctx, key, data)
}

// GetDeliveryHistory returns delivery history for a notification.
func (s *PebbleStorage) GetDeliveryHistory(ctx context.Context, notificationID string) ([]*DeliveryHistory, error) {
	prefix := storage.NotificationHistoryKeyPrefix(notificationID)

	var history []*DeliveryHistory

	err := s.store.Iterate(ctx, prefix, func(key, value []byte) bool {
		var h DeliveryHistory
		if err := json.Unmarshal(value, &h); err != nil {
			return true // Skip invalid entries
		}
		history = append(history, &h)
		return true
	})

	if err != nil {
		return nil, fmt.Errorf("failed to get delivery history: %w", err)
	}

	return history, nil
}

// GetStats returns notification statistics for a setting.
func (s *PebbleStorage) GetStats(ctx context.Context, settingID string) (*NotificationStats, error) {
	key := storage.NotificationStatsKey(settingID)
	data, err := s.store.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}
	if data == nil {
		return &NotificationStats{SettingID: settingID}, nil
	}

	var stats NotificationStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stats: %w", err)
	}

	return &stats, nil
}

// IncrementStats increments notification statistics.
func (s *PebbleStorage) IncrementStats(ctx context.Context, settingID string, success bool, deliveryMs int64) error {
	stats, err := s.GetStats(ctx, settingID)
	if err != nil {
		return err
	}

	now := time.Now()
	if success {
		stats.TotalSent++
		stats.LastSentAt = &now

		// Update average delivery time
		total := float64(stats.TotalSent + stats.TotalFailed)
		if total > 0 {
			stats.AvgDeliveryMs = ((stats.AvgDeliveryMs * (total - 1)) + float64(deliveryMs)) / total
		}
	} else {
		stats.TotalFailed++
		stats.LastFailedAt = &now
	}

	// Update success rate
	total := stats.TotalSent + stats.TotalFailed
	if total > 0 {
		stats.SuccessRate = float64(stats.TotalSent) / float64(total) * 100
	}

	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal stats: %w", err)
	}

	key := storage.NotificationStatsKey(settingID)
	return s.store.Put(ctx, key, data)
}

// CleanupOldHistory removes delivery history older than the given time.
func (s *PebbleStorage) CleanupOldHistory(ctx context.Context, before time.Time) (int64, error) {
	prefix := storage.NotificationHistoryKeyPrefix("")
	var count int64
	var keysToDelete [][]byte

	err := s.store.Iterate(ctx, prefix, func(key, value []byte) bool {
		var history DeliveryHistory
		if err := json.Unmarshal(value, &history); err != nil {
			return true
		}

		if history.Timestamp.Before(before) {
			keysToDelete = append(keysToDelete, key)
		}
		return true
	})

	if err != nil {
		return 0, fmt.Errorf("failed to iterate history: %w", err)
	}

	for _, key := range keysToDelete {
		if err := s.store.Delete(ctx, key); err == nil {
			count++
		}
	}

	return count, nil
}
