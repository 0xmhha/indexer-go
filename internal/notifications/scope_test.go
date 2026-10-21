package notifications

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
)

func scopedService(t *testing.T, quota int) (*NotificationService, *mockStorage) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.OperatorLabels = []string{"ops"}
	cfg.MaxSettingsPerOwner = quota
	storage := newMockStorage()
	svc := NewService(cfg, storage, events.NewEventBus(100, 100), zap.NewNop())
	svc.RegisterHandler(NewWebhookHandler(nil, zap.NewNop()))
	return svc, storage
}

func hookSetting(name string) *NotificationSetting {
	return &NotificationSetting{Name: name, Type: NotificationTypeWebhook, Enabled: true,
		EventTypes: []EventType{EventTypeBlock}, Destination: Destination{WebhookURL: "https://hooks.example.com/" + name}}
}

// TestSettingsBelongToTheirKey: a key sees and changes only the settings
// it created and their notifications; another key's setting looks absent;
// the owner cannot be chosen by the request; operator keys see everything,
// including settings without an owner, and an update keeps the owner.
func TestSettingsBelongToTheirKey(t *testing.T) {
	ctx := context.Background()
	svc, storage := scopedService(t, 0)
	alice, bob, ops := ForCaller(svc, "alice"), ForCaller(svc, "bob"), ForCaller(svc, "ops")

	claimed := hookSetting("a")
	claimed.Owner = "bob" // a request cannot choose the owner
	a, err := alice.CreateSetting(ctx, claimed)
	require.NoError(t, err)
	assert.Equal(t, "alice", a.Owner)
	b, err := bob.CreateSetting(ctx, hookSetting("b"))
	require.NoError(t, err)
	legacy := hookSetting("legacy")
	legacy.ID = "legacy"
	require.NoError(t, storage.SaveSetting(ctx, legacy)) // created before owners existed

	got, err := bob.GetSetting(ctx, a.ID)
	require.NoError(t, err)
	assert.Nil(t, got, "another key's setting looks absent")
	list, err := bob.ListSettings(ctx, nil)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, b.ID, list[0].ID)

	steal := hookSetting("stolen")
	steal.ID = a.ID
	_, err = bob.UpdateSetting(ctx, steal)
	assert.ErrorContains(t, err, "not found")
	assert.ErrorContains(t, bob.DeleteSetting(ctx, a.ID), "not found")
	_, err = bob.TestSetting(ctx, a.ID)
	assert.ErrorContains(t, err, "not found")
	_, err = bob.GetStats(ctx, a.ID)
	assert.ErrorContains(t, err, "not found")
	_, err = bob.GetStats(ctx, "")
	assert.ErrorContains(t, err, "operator", "statistics of all settings")
	for _, id := range []string{a.ID, "legacy"} {
		st, err := svc.GetSetting(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, st, "%s is untouched", id)
	}

	require.NoError(t, storage.SaveNotification(ctx, &Notification{ID: "na", SettingID: a.ID, Status: DeliveryStatusFailed}))
	require.NoError(t, storage.SaveNotification(ctx, &Notification{ID: "nb", SettingID: b.ID, Status: DeliveryStatusFailed}))
	n, err := bob.GetNotification(ctx, "na")
	require.NoError(t, err)
	assert.Nil(t, n)
	assert.ErrorContains(t, bob.RetryNotification(ctx, "na"), "not found")
	assert.ErrorContains(t, bob.CancelNotification(ctx, "na"), "not found")
	ns, err := bob.ListNotifications(ctx, nil)
	require.NoError(t, err)
	require.Len(t, ns, 1)
	assert.Equal(t, "nb", ns[0].ID)
	ns, err = bob.ListNotifications(ctx, &NotificationsFilter{SettingID: a.ID})
	require.NoError(t, err)
	assert.Empty(t, ns)

	all, err := ops.ListSettings(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, all, 3, "an operator sees every setting, the legacy one too")
	o, err := ops.CreateSetting(ctx, hookSetting("o"))
	require.NoError(t, err)
	assert.Equal(t, "ops", o.Owner)
	upd := hookSetting("a2")
	upd.ID = a.ID
	_, err = ops.UpdateSetting(ctx, upd)
	require.NoError(t, err)
	st, err := svc.GetSetting(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, "alice", st.Owner, "an update keeps the owner")
	assert.Equal(t, "a2", st.Name)

	assert.ErrorIs(t, bob.Start(ctx), errLifecycle)
	assert.ErrorIs(t, ops.Stop(ctx), errLifecycle)
}

// TestSettingsQuota: a key cannot hold more settings than the quota;
// another key and operators are not limited by it.
func TestSettingsQuota(t *testing.T) {
	ctx := context.Background()
	svc, _ := scopedService(t, 2)
	alice := ForCaller(svc, "alice")
	for i := range 2 {
		_, err := alice.CreateSetting(ctx, hookSetting(string(rune('a'+i))))
		require.NoError(t, err)
	}
	_, err := alice.CreateSetting(ctx, hookSetting("c"))
	assert.ErrorIs(t, err, ErrQuotaExceeded)
	_, err = ForCaller(svc, "bob").CreateSetting(ctx, hookSetting("b"))
	assert.NoError(t, err)
	for i := range 3 {
		_, err = ForCaller(svc, "ops").CreateSetting(ctx, hookSetting(string(rune('x'+i))))
		require.NoError(t, err, "operators are not limited")
	}
}
