package notifications

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// mixedSetting is a fast log setting that streams to alice and posts to a
// webhook.
func mixedSetting() *NotificationSetting {
	return &NotificationSetting{Name: "m", Type: NotificationTypeStream, Channels: []NotificationType{NotificationTypeWebhook},
		Enabled: true, Owner: "alice", Delivery: DeliveryFast, EventTypes: []EventType{EventTypeLog},
		Destination: Destination{WebhookURL: "https://hooks.example.com/m"}}
}

// TestChannelsAreValidated: every channel must be known, named once and
// accept the setting.
func TestChannelsAreValidated(t *testing.T) {
	ctx := context.Background()
	svc, _ := scopedService(t, 0)
	for name, change := range map[string]func(*NotificationSetting){
		"named twice":          func(s *NotificationSetting) { s.Channels = []NotificationType{NotificationTypeStream} },
		"unknown":              func(s *NotificationSetting) { s.Channels = []NotificationType{"pager"} },
		"webhook without url":  func(s *NotificationSetting) { s.Destination = Destination{} },
		"stream without owner": func(s *NotificationSetting) { s.Owner = "" },
	} {
		t.Run(name, func(t *testing.T) {
			st := mixedSetting()
			change(st)
			_, err := svc.CreateSetting(ctx, st)
			require.Error(t, err)
		})
	}
	_, err := svc.CreateSetting(ctx, mixedSetting())
	require.NoError(t, err)
}

// TestOneSettingManyChannels: one setting's notification goes to each
// channel once: the stream gets it with the setting's id, the webhook's
// stored copy has an id of its own, and evaluating the event again stores
// nothing more. The storage lists the setting under a further channel.
func TestOneSettingManyChannels(t *testing.T) {
	ctx := context.Background()
	svc, storage := scopedService(t, 0)
	st, err := svc.CreateSetting(ctx, mixedSetting())
	require.NoError(t, err)
	c, err := svc.Streams().Open("alice")
	require.NoError(t, err)

	block := []events.Event{events.NewLogEvent(&types.Log{Address: common.HexToAddress("0xaa"), BlockNumber: 7,
		BlockHash: common.HexToHash("0x07"), Index: 3, Topics: []common.Hash{common.HexToHash("0x01")}})}
	require.NoError(t, svc.notifyFast(ctx, block))
	require.NoError(t, svc.notifyFast(ctx, block))

	streamed := decodeStream(t, c)
	require.Equal(t, NotificationTypeStream, streamed.Notification.Type)
	stored, err := storage.ListNotifications(ctx, nil)
	require.NoError(t, err)
	require.Len(t, stored, 1, "one webhook copy, however often the event is evaluated")
	assert.Equal(t, NotificationTypeWebhook, stored[0].Type)
	assert.Equal(t, st.ID, stored[0].SettingID)
	assert.Equal(t, channelID(streamed.Notification.ID, NotificationTypeWebhook), stored[0].ID)

	ps := NewPebbleStorage(newMockKVStore())
	require.NoError(t, ps.SaveSetting(ctx, st))
	found, err := ps.ListSettings(ctx, &SettingsFilter{Types: []NotificationType{NotificationTypeWebhook}})
	require.NoError(t, err)
	assert.Len(t, found, 1, "listed by a further channel")
}
