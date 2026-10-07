package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// KeyValueStore is the storage port of the notification service: plain
// key-value access to the indexer's database.
type KeyValueStore interface {
	Put(ctx context.Context, key, value []byte) error
	Get(ctx context.Context, key []byte) ([]byte, error)
	Delete(ctx context.Context, key []byte) error
	Iterate(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error
}

var _ Storage = (*PebbleStorage)(nil)

// get reads a key, nil if it is absent: the indexer's store reports an
// absent key as port.ErrNotFound.
func (s *PebbleStorage) get(ctx context.Context, key []byte) ([]byte, error) {
	data, err := s.store.Get(ctx, key)
	if errors.Is(err, port.ErrNotFound) {
		return nil, nil
	}
	return data, err
}

// PebbleStorage implements the Storage interface over a KeyValueStore (the
// indexer's Pebble database).
type PebbleStorage struct {
	store KeyValueStore
}

// NewPebbleStorage creates a new PebbleStorage.
func NewPebbleStorage(store KeyValueStore) *PebbleStorage {
	return &PebbleStorage{store: store}
}

// SaveSetting saves a notification setting.
func (s *PebbleStorage) SaveSetting(ctx context.Context, setting *NotificationSetting) error {
	data, err := json.Marshal(setting)
	if err != nil {
		return fmt.Errorf("failed to marshal setting: %w", err)
	}

	key := NotificationSettingKey(setting.ID)
	return s.store.Put(ctx, key, data)
}

// GetSetting returns a notification setting by ID.
func (s *PebbleStorage) GetSetting(ctx context.Context, id string) (*NotificationSetting, error) {
	key := NotificationSettingKey(id)
	data, err := s.get(ctx, key)
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
	key := NotificationSettingKey(id)
	return s.store.Delete(ctx, key)
}

// ListSettings returns notification settings matching the filter.
func (s *PebbleStorage) ListSettings(ctx context.Context, filter *SettingsFilter) ([]*NotificationSetting, error) {
	prefix := NotificationSettingKeyPrefix()

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

	key := NotificationKey(notification.ID)
	if err := s.store.Put(ctx, key, data); err != nil {
		return err
	}

	// Create status index
	statusKey := NotificationStatusIndexKey(
		string(notification.Status),
		notification.CreatedAt.UnixNano(),
		notification.ID,
	)
	if err := s.store.Put(ctx, statusKey, []byte(notification.ID)); err != nil {
		return err
	}

	// Create setting index
	settingKey := NotificationSettingIndexKey(
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
		pendingKey := NotificationPendingIndexKey(nextRetry, notification.ID)
		if err := s.store.Put(ctx, pendingKey, []byte(notification.ID)); err != nil {
			return err
		}
	}

	return nil
}

// GetNotification returns a notification by ID.
func (s *PebbleStorage) GetNotification(ctx context.Context, id string) (*Notification, error) {
	key := NotificationKey(id)
	data, err := s.get(ctx, key)
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
	key := NotificationKey(id)
	if err := s.store.Put(ctx, key, data); err != nil {
		return err
	}

	// Update status index (remove old, add new)
	oldStatusKey := NotificationStatusIndexKey(
		string(oldStatus),
		notification.CreatedAt.UnixNano(),
		notification.ID,
	)
	_ = s.store.Delete(ctx, oldStatusKey)

	newStatusKey := NotificationStatusIndexKey(
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
		pendingKey := NotificationPendingIndexKey(nextRetry, notification.ID)
		_ = s.store.Delete(ctx, pendingKey)
	}

	return nil
}

// UpdateNotification implements Storage: it stores the notification's
// delivery state and moves its status and pending index entries.
func (s *PebbleStorage) UpdateNotification(ctx context.Context, notification *Notification) error {
	old, err := s.GetNotification(ctx, notification.ID)
	if err != nil {
		return err
	}
	if old == nil {
		return fmt.Errorf("notification not found: %s", notification.ID)
	}
	data, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}
	if err := s.store.Put(ctx, NotificationKey(notification.ID), data); err != nil {
		return err
	}
	_ = s.store.Delete(ctx, NotificationStatusIndexKey(string(old.Status), old.CreatedAt.UnixNano(), old.ID))
	_ = s.store.Delete(ctx, NotificationPendingIndexKey(pendingTime(old), old.ID))
	if err := s.store.Put(ctx, NotificationStatusIndexKey(string(notification.Status), notification.CreatedAt.UnixNano(), notification.ID), []byte(notification.ID)); err != nil {
		return err
	}
	if notification.Status == DeliveryStatusPending || notification.Status == DeliveryStatusRetrying {
		return s.store.Put(ctx, NotificationPendingIndexKey(pendingTime(notification), notification.ID), []byte(notification.ID))
	}
	return nil
}

// pendingTime is the time a pending notification is due: its next retry,
// or its creation when it was never attempted.
func pendingTime(n *Notification) int64 {
	if n.NextRetry != nil {
		return n.NextRetry.UnixNano()
	}
	return n.CreatedAt.UnixNano()
}

// ListNotifications returns notifications matching the filter.
func (s *PebbleStorage) ListNotifications(ctx context.Context, filter *NotificationsFilter) ([]*Notification, error) {
	var prefix []byte

	// Determine which index to use
	if filter != nil && filter.SettingID != "" {
		prefix = NotificationSettingIndexKeyPrefix(filter.SettingID)
	} else if filter != nil && len(filter.Status) > 0 {
		prefix = NotificationStatusIndexKeyPrefix(string(filter.Status[0]))
	} else {
		prefix = NotificationKeyPrefix()
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
	prefix := NotificationPendingIndexKeyPrefix()
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

	key := NotificationHistoryKey(history.NotificationID, history.Attempt)
	return s.store.Put(ctx, key, data)
}

// GetDeliveryHistory returns delivery history for a notification.
func (s *PebbleStorage) GetDeliveryHistory(ctx context.Context, notificationID string) ([]*DeliveryHistory, error) {
	prefix := NotificationHistoryKeyPrefix(notificationID)

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
	key := NotificationStatsKey(settingID)
	data, err := s.get(ctx, key)
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

	key := NotificationStatsKey(settingID)
	return s.store.Put(ctx, key, data)
}

// CleanupOldHistory removes delivery history older than the given time.
func (s *PebbleStorage) CleanupOldHistory(ctx context.Context, before time.Time) (int64, error) {
	prefix := NotificationHistoryKeyPrefix("")
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
