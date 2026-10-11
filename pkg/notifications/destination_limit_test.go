package notifications

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// TestDestinationKeys: webhooks share a limit per host (port included,
// case ignored), Slack messages per incoming webhook URL; other channels
// are not limited.
func TestDestinationKeys(t *testing.T) {
	hook := func(u string) string { return destinationKey(NotificationTypeWebhook, Destination{WebhookURL: u}) }
	assert.Equal(t, hook("https://hooks.example.com/a"), hook("https://HOOKS.example.com/b?x=1"))
	assert.NotEqual(t, hook("https://hooks.example.com/a"), hook("https://hooks.example.com:8443/a"))
	slack := func(u string) string { return destinationKey(NotificationTypeSlack, Destination{SlackWebhookURL: u}) }
	assert.NotEqual(t, slack("https://hooks.slack.com/services/A"), slack("https://hooks.slack.com/services/B"))
	assert.Empty(t, destinationKey(NotificationTypeStream, Destination{}))
	assert.Empty(t, hook("::not a url"))
}

// TestDestinationLimitWaits: a destination's burst goes at once, the next
// delivery waits for the rate, other destinations do not wait, and no cap
// means no wait.
func TestDestinationLimitWaits(t *testing.T) {
	now := time.Now()
	l := newDestinationLimits(2, 3)
	for i := 0; i < 3; i++ {
		require.Zero(t, l.wait("a", now), "burst %d", i)
	}
	wait := l.wait("a", now)
	assert.InDelta(t, 500*time.Millisecond, wait, float64(10*time.Millisecond), "one token every 500ms")
	assert.Equal(t, wait, l.wait("a", now), "a refused delivery takes no token")
	assert.Zero(t, l.wait("b", now), "another destination")
	assert.Zero(t, l.wait("a", now.Add(wait)), "due once the rate allows")
	assert.Zero(t, newDestinationLimits(0, 0).wait("a", now), "0: no cap")
}

// TestDeliveryOverTheLimitIsDeferred: a delivery over its destination's
// limit is not attempted; it is stored for later without counting as an
// attempt, while the same setting's first delivery goes through.
func TestDeliveryOverTheLimitIsDeferred(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.DestinationRateLimit, cfg.DestinationBurst = 1, 1
	storage := newMockStorage()
	svc := NewService(cfg, storage, events.NewEventBus(100, 100), zap.NewNop())
	h := &countingHandler{got: map[uint64]int{}}
	svc.RegisterHandler(h)
	svc.workCtx = ctx
	st, err := svc.CreateSetting(ctx, hookSetting("a"))
	require.NoError(t, err)

	var ns []*Notification
	for i := uint64(1); i <= 2; i++ {
		n := &Notification{ID: string(rune('0' + i)), SettingID: st.ID, Type: NotificationTypeWebhook, Status: DeliveryStatusPending,
			Payload: &EventPayload{BlockNumber: i}}
		require.NoError(t, storage.SaveNotification(ctx, n))
		ns = append(ns, n)
	}
	before := time.Now()
	svc.processNotification(ns[0])
	svc.processNotification(ns[1])
	distinct, _ := h.delivered()
	assert.Equal(t, 1, distinct, "only the first is attempted")

	second, err := storage.GetNotification(ctx, ns[1].ID)
	require.NoError(t, err)
	assert.Equal(t, DeliveryStatusRetrying, second.Status)
	assert.Zero(t, second.RetryCount, "not an attempt")
	require.NotNil(t, second.NextRetry)
	assert.True(t, second.NextRetry.After(before.Add(500*time.Millisecond)), "due when the limit allows, about a second later")
}
