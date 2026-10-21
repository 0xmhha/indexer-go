package notifications

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
)

// A PostgreSQL Put can observe cancellation between any two index writes.
// Keep this failure deterministic instead of depending on a shutdown race.
type interruptedNotificationKV struct {
	KeyValueStore
	writes int
	failAt int
}

func (s *interruptedNotificationKV) Put(ctx context.Context, key, value []byte) error {
	s.writes++
	if s.writes == s.failAt {
		return context.Canceled
	}
	return s.KeyValueStore.Put(ctx, key, value)
}

func TestNotificationPublicationRetriesInterruptedWrites(t *testing.T) {
	for _, failAt := range []int{1, 2, 3, 4} {
		t.Run("write_"+strconv.Itoa(failAt), func(t *testing.T) {
			kv := &interruptedNotificationKV{KeyValueStore: newMockKVStore(), failAt: failAt}
			store := NewPebbleStorage(kv)
			ctx := context.Background()
			n := &Notification{ID: "one", SettingID: "setting", Type: NotificationTypeWebhook,
				Status: DeliveryStatusPending, CreatedAt: time.Now().Add(-time.Hour)}
			require.ErrorIs(t, store.SaveNotification(ctx, n), context.Canceled)
			published, err := store.GetNotification(ctx, n.ID)
			require.NoError(t, err)
			require.Nil(t, published, "a failed creation must not make replay deduplicate incomplete indexes")

			kv.failAt = 0
			// Replay recreates the same ID at a new wall-clock time. Partial
			// indexes from the previous attempt must not duplicate the result.
			n.CreatedAt = n.CreatedAt.Add(time.Minute)
			require.NoError(t, store.SaveNotification(ctx, n))
			pending, err := store.GetPendingNotifications(ctx, 10)
			require.NoError(t, err)
			require.Len(t, pending, 1)
			for _, filter := range []*NotificationsFilter{
				{SettingID: n.SettingID}, {Status: []DeliveryStatus{DeliveryStatusPending}},
			} {
				listed, err := store.ListNotifications(ctx, filter)
				require.NoError(t, err)
				require.Len(t, listed, 1)
				require.True(t, n.CreatedAt.Equal(listed[0].CreatedAt))
			}
		})
	}
}

func TestNotificationStreamReplaysInterruptedPublication(t *testing.T) {
	data, err := events.MarshalEvent(events.NewLogEvent(&types.Log{BlockNumber: 1}))
	require.NoError(t, err)
	batch := []port.OutboxEntry{{Seq: 1, Type: string(events.EventTypeLog), Data: data}}
	for _, failAt := range []int{1, 2, 3, 4} {
		t.Run("write_"+strconv.Itoa(failAt), func(t *testing.T) {
			kv := &interruptedNotificationKV{KeyValueStore: newMockKVStore(), failAt: failAt}
			store := NewPebbleStorage(kv)
			start := func() *NotificationService {
				svc := NewService(DefaultConfig(), store, nil, zap.NewNop())
				svc.settings["setting"] = &NotificationSetting{ID: "setting", Enabled: true,
					Type: NotificationTypeWebhook, EventTypes: []EventType{EventTypeLog}}
				return svc
			}
			require.ErrorIs(t, start().handleBatch(context.Background(), batch), context.Canceled)
			kv.failAt = 0
			restarted := start()
			require.NoError(t, restarted.handleBatch(context.Background(), batch))
			require.Len(t, restarted.queue, 1, "replay must queue the interrupted notification before accepting the batch")
			pending, err := store.GetPendingNotifications(context.Background(), 10)
			require.NoError(t, err)
			require.Len(t, pending, 1)
		})
	}
}

func TestNotificationIndexReadersIgnoreUnpublishedAndStale(t *testing.T) {
	kv := newMockKVStore()
	store := NewPebbleStorage(kv)
	ctx := context.Background()
	created := time.Now().Add(-time.Hour)
	for _, key := range [][]byte{
		NotificationStatusIndexKey(string(DeliveryStatusPending), created.UnixNano(), "one"),
		NotificationSettingIndexKey("setting", created.UnixNano(), "one"),
		NotificationPendingIndexKey(created.UnixNano(), "one"),
	} {
		require.NoError(t, kv.Put(ctx, key, []byte("one")))
	}
	for _, status := range []DeliveryStatus{"", DeliveryStatusSent} {
		if status != "" {
			require.NoError(t, store.SaveNotification(ctx, &Notification{ID: "one", SettingID: "setting",
				Status: status, CreatedAt: created.Add(time.Minute)}))
		}
		since := created.Add(-time.Hour)
		listed, err := store.ListNotifications(ctx, &NotificationsFilter{
			Status: []DeliveryStatus{DeliveryStatusPending}, FromTime: &since})
		require.NoError(t, err)
		require.Empty(t, listed)
		pending, err := store.GetPendingNotifications(ctx, 10)
		require.NoError(t, err)
		require.Empty(t, pending)
	}
	listed, err := store.ListNotifications(ctx, &NotificationsFilter{SettingID: "setting"})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	listed, err = store.ListNotifications(ctx, &NotificationsFilter{SettingID: "setting", Offset: 1})
	require.NoError(t, err)
	require.Empty(t, listed, "unpublished indexes do not consume pagination offsets")
}
