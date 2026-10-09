package notifications

import (
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/storage"
)

// Key prefixes of notification data.
const (
	// Notification data prefixes
	prefixNotificationSetting = "/data/notification/setting/"
	prefixNotification        = "/data/notification/notif/"
	prefixNotificationHistory = "/data/notification/history/"
	prefixNotificationStats   = "/data/notification/stats/"

	// Notification index prefixes
	prefixIdxNotificationStatus  = "/index/notification/status/"
	prefixIdxNotificationSetting = "/index/notification/setting/"
	prefixIdxNotificationPending = "/index/notification/pending/"
)

// KeyPrefixes are the key-value prefixes of all notification data: an API
// process (node.role api) opens its read-only database with them writable
// so it can manage notification settings.
func KeyPrefixes() []string { return []string{"/data/notification/", "/index/notification/"} }

func init() {
	storage.RegisterKeyspace("notifications", storage.ChainData, KeyPrefixes()...)
}

// NotificationSettingKey returns the key for storing a notification setting
// Format: /data/notification/setting/{id}
func NotificationSettingKey(id string) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixNotificationSetting, id))
}

// NotificationSettingKeyPrefix returns the prefix for all notification settings
func NotificationSettingKeyPrefix() []byte {
	return []byte(prefixNotificationSetting)
}

// NotificationKey returns the key for storing a notification
// Format: /data/notification/notif/{id}
func NotificationKey(id string) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixNotification, id))
}

// NotificationKeyPrefix returns the prefix for all notifications
func NotificationKeyPrefix() []byte {
	return []byte(prefixNotification)
}

// NotificationHistoryKey returns the key for storing delivery history
// Format: /data/notification/history/{notificationId}/{attempt}
func NotificationHistoryKey(notificationID string, attempt int) []byte {
	return []byte(fmt.Sprintf("%s%s/%06d", prefixNotificationHistory, notificationID, attempt))
}

// NotificationHistoryKeyPrefix returns the prefix for notification history
func NotificationHistoryKeyPrefix(notificationID string) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixNotificationHistory, notificationID))
}

// NotificationStatsKey returns the key for storing notification stats
// Format: /data/notification/stats/{settingId}
func NotificationStatsKey(settingID string) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixNotificationStats, settingID))
}

// NotificationStatusIndexKey returns the index key for notifications by status
// Format: /index/notification/status/{status}/{createdAt}/{id}
func NotificationStatusIndexKey(status string, createdAt int64, id string) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%s", prefixIdxNotificationStatus, status, createdAt, id))
}

// NotificationStatusIndexKeyPrefix returns the prefix for notifications by status
func NotificationStatusIndexKeyPrefix(status string) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxNotificationStatus, status))
}

// NotificationSettingIndexKey returns the index key for notifications by setting
// Format: /index/notification/setting/{settingId}/{createdAt}/{id}
func NotificationSettingIndexKey(settingID string, createdAt int64, id string) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%s", prefixIdxNotificationSetting, settingID, createdAt, id))
}

// NotificationSettingIndexKeyPrefix returns the prefix for notifications by setting
func NotificationSettingIndexKeyPrefix(settingID string) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxNotificationSetting, settingID))
}

// NotificationPendingIndexKey returns the index key for pending notifications
// Format: /index/notification/pending/{nextRetry}/{id}
func NotificationPendingIndexKey(nextRetry int64, id string) []byte {
	return []byte(fmt.Sprintf("%s%020d/%s", prefixIdxNotificationPending, nextRetry, id))
}

// NotificationPendingIndexKeyPrefix returns the prefix for all pending notifications
func NotificationPendingIndexKeyPrefix() []byte {
	return []byte(prefixIdxNotificationPending)
}
