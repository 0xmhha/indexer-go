package notifications

import (
	"context"
	"errors"
	"fmt"
)

// Settings belong to the API key that created them: a caller sees and
// changes only its own settings and their notifications, and another
// key's setting looks absent (its existence is not revealed). Operator
// keys (Config.OperatorLabels) see every setting, including those created
// before settings had owners.

// ErrQuotaExceeded is returned when a key already has
// Config.MaxSettingsPerOwner settings.
var ErrQuotaExceeded = errors.New("notification settings quota exceeded")

// errLifecycle refuses starting or stopping the service through a
// caller's view.
var errLifecycle = errors.New("a caller cannot start or stop the notification service")

// ownerScope is implemented by the service: whether a label is an
// operator's, and the settings quota of other labels.
type ownerScope interface {
	IsOperator(label string) bool
	SettingsQuota() int
}

// ForCaller returns svc as the caller with API key label sees it: the
// service itself for an operator, otherwise a view limited to the
// caller's settings. A service without owner support is returned as is.
func ForCaller(svc Service, label string) Service {
	scope, ok := svc.(ownerScope)
	if !ok {
		return svc
	}
	if scope.IsOperator(label) {
		return operatorView{Service: svc, label: label}
	}
	return &ownedView{svc: svc, owner: label, quota: scope.SettingsQuota()}
}

// operatorView is the service as an operator sees it: everything, with
// the operator recorded as the owner of the settings it creates.
type operatorView struct {
	Service
	label string
}

func (v operatorView) Start(context.Context) error { return errLifecycle }
func (v operatorView) Stop(context.Context) error  { return errLifecycle }

func (v operatorView) CreateSetting(ctx context.Context, setting *NotificationSetting) (*NotificationSetting, error) {
	if setting.Owner == "" {
		setting.Owner = v.label
	}
	return v.Service.CreateSetting(ctx, setting)
}

// IsOperator reports whether label is an operator key's.
func (s *NotificationService) IsOperator(label string) bool {
	for _, l := range s.config.OperatorLabels {
		if l == label {
			return true
		}
	}
	return false
}

// SettingsQuota is the most settings a non-operator key may have, 0 for
// no limit.
func (s *NotificationService) SettingsQuota() int { return s.config.MaxSettingsPerOwner }

// ownedView is a Service limited to the settings of owner.
type ownedView struct {
	svc   Service
	owner string
	quota int
}

var _ Service = (*ownedView)(nil)

func (v *ownedView) Start(context.Context) error { return errLifecycle }
func (v *ownedView) Stop(context.Context) error  { return errLifecycle }

// owned returns the setting id when it is the owner's, nil otherwise.
func (v *ownedView) owned(ctx context.Context, id string) (*NotificationSetting, error) {
	st, err := v.svc.GetSetting(ctx, id)
	if err != nil || st == nil || st.Owner != v.owner {
		return nil, err
	}
	return st, nil
}

func notFound(what, id string) error { return fmt.Errorf("%s %s not found", what, id) }

// allSettings lists every setting of the underlying service, a page at a
// time.
func (v *ownedView) allSettings(ctx context.Context, filter *SettingsFilter) ([]*NotificationSetting, error) {
	const page = 1000
	var out []*NotificationSetting
	f := SettingsFilter{}
	if filter != nil {
		f = *filter
	}
	f.Limit = page
	for offset := 0; ; offset += page {
		f.Offset = offset
		got, err := v.svc.ListSettings(ctx, &f)
		if err != nil {
			return nil, err
		}
		for _, st := range got {
			if st.Owner == v.owner {
				out = append(out, st)
			}
		}
		if len(got) < page {
			return out, nil
		}
	}
}

// window applies a filter's offset and limit to a full list.
func window[T any](all []T, offset, limit int) []T {
	if offset >= len(all) {
		return nil
	}
	all = all[offset:]
	if limit > 0 && limit < len(all) {
		all = all[:limit]
	}
	return all
}

// CreateSetting records the caller as the owner, whatever the request
// says, within the caller's quota.
func (v *ownedView) CreateSetting(ctx context.Context, setting *NotificationSetting) (*NotificationSetting, error) {
	if v.quota > 0 {
		mine, err := v.allSettings(ctx, nil)
		if err != nil {
			return nil, err
		}
		if len(mine) >= v.quota {
			return nil, fmt.Errorf("%w: %d settings", ErrQuotaExceeded, v.quota)
		}
	}
	setting.Owner = v.owner
	return v.svc.CreateSetting(ctx, setting)
}

// UpdateSetting keeps the owner.
func (v *ownedView) UpdateSetting(ctx context.Context, setting *NotificationSetting) (*NotificationSetting, error) {
	st, err := v.owned(ctx, setting.ID)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, notFound("setting", setting.ID)
	}
	setting.Owner = v.owner
	return v.svc.UpdateSetting(ctx, setting)
}

func (v *ownedView) DeleteSetting(ctx context.Context, id string) error {
	st, err := v.owned(ctx, id)
	if err != nil {
		return err
	}
	if st == nil {
		return notFound("setting", id)
	}
	return v.svc.DeleteSetting(ctx, id)
}

func (v *ownedView) GetSetting(ctx context.Context, id string) (*NotificationSetting, error) {
	return v.owned(ctx, id)
}

func (v *ownedView) ListSettings(ctx context.Context, filter *SettingsFilter) ([]*NotificationSetting, error) {
	mine, err := v.allSettings(ctx, filter)
	if err != nil {
		return nil, err
	}
	offset, limit := 0, 100
	if filter != nil {
		offset = filter.Offset
		if filter.Limit > 0 {
			limit = filter.Limit
		}
	}
	return window(mine, offset, limit), nil
}

// ownedNotification returns the notification id when its setting is the
// owner's, nil otherwise.
func (v *ownedView) ownedNotification(ctx context.Context, id string) (*Notification, error) {
	n, err := v.svc.GetNotification(ctx, id)
	if err != nil || n == nil {
		return nil, err
	}
	st, err := v.owned(ctx, n.SettingID)
	if err != nil || st == nil {
		return nil, err
	}
	return n, nil
}

func (v *ownedView) GetNotification(ctx context.Context, id string) (*Notification, error) {
	return v.ownedNotification(ctx, id)
}

// ListNotifications lists the notifications of the caller's settings: of
// one setting when the filter names it, else of all of them.
func (v *ownedView) ListNotifications(ctx context.Context, filter *NotificationsFilter) ([]*Notification, error) {
	f := NotificationsFilter{}
	if filter != nil {
		f = *filter
	}
	var settings []string
	if f.SettingID != "" {
		st, err := v.owned(ctx, f.SettingID)
		if err != nil || st == nil {
			return nil, err
		}
		settings = []string{f.SettingID}
	} else {
		mine, err := v.allSettings(ctx, nil)
		if err != nil {
			return nil, err
		}
		for _, st := range mine {
			settings = append(settings, st.ID)
		}
	}
	offset, limit := f.Offset, f.Limit
	if limit <= 0 {
		limit = 100
	}
	var out []*Notification
	for _, id := range settings {
		page := f
		page.SettingID, page.Offset, page.Limit = id, 0, offset+limit
		got, err := v.svc.ListNotifications(ctx, &page)
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return window(out, offset, limit), nil
}

func (v *ownedView) RetryNotification(ctx context.Context, id string) error {
	n, err := v.ownedNotification(ctx, id)
	if err != nil {
		return err
	}
	if n == nil {
		return notFound("notification", id)
	}
	return v.svc.RetryNotification(ctx, id)
}

func (v *ownedView) CancelNotification(ctx context.Context, id string) error {
	n, err := v.ownedNotification(ctx, id)
	if err != nil {
		return err
	}
	if n == nil {
		return notFound("notification", id)
	}
	return v.svc.CancelNotification(ctx, id)
}

// GetStats gives the statistics of one of the caller's settings; the
// statistics of every setting are an operator's.
func (v *ownedView) GetStats(ctx context.Context, settingID string) (*NotificationStats, error) {
	if settingID == "" {
		return nil, errors.New("statistics of all settings need an operator key; name a setting")
	}
	st, err := v.owned(ctx, settingID)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, notFound("setting", settingID)
	}
	return v.svc.GetStats(ctx, settingID)
}

func (v *ownedView) GetDeliveryHistory(ctx context.Context, notificationID string) ([]*DeliveryHistory, error) {
	n, err := v.ownedNotification(ctx, notificationID)
	if err != nil || n == nil {
		return nil, err
	}
	return v.svc.GetDeliveryHistory(ctx, notificationID)
}

func (v *ownedView) TestSetting(ctx context.Context, id string) (*DeliveryResult, error) {
	st, err := v.owned(ctx, id)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, notFound("setting", id)
	}
	return v.svc.TestSetting(ctx, id)
}
