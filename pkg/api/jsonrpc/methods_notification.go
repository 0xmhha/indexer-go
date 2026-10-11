package jsonrpc

import (
	"context"
	"encoding/json"

	"github.com/0xmhha/indexer-go/pkg/api/middleware"
	"github.com/0xmhha/indexer-go/pkg/notifications"
)

// SetNotificationService sets the notification service of this handler
// (each chain's handler has its own in multichain mode).
func (h *Handler) SetNotificationService(service notifications.Service) {
	h.notifications = service
}

// Notification methods

// getNotificationSettings returns notification settings
func (h *Handler) getNotificationSettings(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if h.notifications == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		Types   []string `json:"types,omitempty"`
		Enabled *bool    `json:"enabled,omitempty"`
		Limit   int      `json:"limit,omitempty"`
		Offset  int      `json:"offset,omitempty"`
	}

	if len(params) > 0 {
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, NewError(InvalidParams, "invalid params", err.Error())
		}
	}

	filter := &notifications.SettingsFilter{
		Limit:   input.Limit,
		Offset:  input.Offset,
		Enabled: input.Enabled,
	}

	if filter.Limit <= 0 {
		filter.Limit = 100
	}

	for _, t := range input.Types {
		filter.Types = append(filter.Types, notifications.NotificationType(t))
	}

	settings, err := h.notificationsFor(ctx).ListSettings(ctx, filter)
	if err != nil {
		return nil, NewError(InternalError, "failed to list settings", err.Error())
	}

	return settings, nil
}

// getNotificationSetting returns a single notification setting
func (h *Handler) getNotificationSetting(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if h.notifications == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if input.ID == "" {
		return nil, NewError(InvalidParams, "id is required", nil)
	}

	setting, err := h.notificationsFor(ctx).GetSetting(ctx, input.ID)
	if err != nil {
		return nil, NewError(InternalError, "failed to get setting", err.Error())
	}

	return setting, nil
}

// createNotificationSetting creates a new notification setting
func (h *Handler) createNotificationSetting(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if h.notifications == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		Name        string                   `json:"name"`
		Type        string                   `json:"type"`
		Enabled     *bool                    `json:"enabled,omitempty"`
		Delivery    string                   `json:"delivery,omitempty"`
		Condition   string                   `json:"condition,omitempty"`
		Payload     string                   `json:"payload,omitempty"`
		EventTypes  []string                 `json:"eventTypes"`
		Filter      *notificationFilterInput `json:"filter,omitempty"`
		Destination notificationDestInput    `json:"destination"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if input.Name == "" {
		return nil, NewError(InvalidParams, "name is required", nil)
	}

	if input.Type == "" {
		return nil, NewError(InvalidParams, "type is required", nil)
	}

	if len(input.EventTypes) == 0 {
		return nil, NewError(InvalidParams, "eventTypes is required", nil)
	}

	setting := &notifications.NotificationSetting{
		Name:    input.Name,
		Type:    notifications.NotificationType(input.Type),
		Enabled: true,
	}

	if input.Enabled != nil {
		setting.Enabled = *input.Enabled
	}
	setting.Delivery = notifications.Delivery(input.Delivery)
	setting.Condition, setting.Payload = input.Condition, input.Payload

	for _, et := range input.EventTypes {
		setting.EventTypes = append(setting.EventTypes, notifications.EventType(et))
	}

	if input.Filter != nil {
		filter, err := parseJSONRPCNotifyFilter(input.Filter)
		if err != nil {
			return nil, NewError(InvalidParams, err.Error(), nil)
		}
		setting.Filter = filter
	}

	setting.Destination = parseJSONRPCDestination(input.Destination)

	created, err := h.notificationsFor(ctx).CreateSetting(ctx, setting)
	if err != nil {
		return nil, NewError(InternalError, "failed to create setting", err.Error())
	}

	return created, nil
}

// updateNotificationSetting updates a notification setting
func (h *Handler) updateNotificationSetting(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if h.notifications == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		ID          string                   `json:"id"`
		Name        *string                  `json:"name,omitempty"`
		Enabled     *bool                    `json:"enabled,omitempty"`
		Delivery    *string                  `json:"delivery,omitempty"`
		Condition   *string                  `json:"condition,omitempty"`
		Payload     *string                  `json:"payload,omitempty"`
		EventTypes  []string                 `json:"eventTypes,omitempty"`
		Filter      *notificationFilterInput `json:"filter,omitempty"`
		Destination *notificationDestInput   `json:"destination,omitempty"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if input.ID == "" {
		return nil, NewError(InvalidParams, "id is required", nil)
	}

	existing, err := h.notificationsFor(ctx).GetSetting(ctx, input.ID)
	if err != nil {
		return nil, NewError(InternalError, "failed to get setting", err.Error())
	}
	if existing == nil {
		return nil, NewError(InvalidParams, "setting not found", nil)
	}

	if input.Name != nil {
		existing.Name = *input.Name
	}

	if input.Enabled != nil {
		existing.Enabled = *input.Enabled
	}
	if input.Delivery != nil {
		existing.Delivery = notifications.Delivery(*input.Delivery)
	}
	if input.Condition != nil {
		existing.Condition = *input.Condition
	}
	if input.Payload != nil {
		existing.Payload = *input.Payload
	}

	if len(input.EventTypes) > 0 {
		existing.EventTypes = nil
		for _, et := range input.EventTypes {
			existing.EventTypes = append(existing.EventTypes, notifications.EventType(et))
		}
	}

	if input.Filter != nil {
		filter, err := parseJSONRPCNotifyFilter(input.Filter)
		if err != nil {
			return nil, NewError(InvalidParams, err.Error(), nil)
		}
		existing.Filter = filter
	}

	if input.Destination != nil {
		existing.Destination = parseJSONRPCDestination(*input.Destination)
	}

	updated, err := h.notificationsFor(ctx).UpdateSetting(ctx, existing)
	if err != nil {
		return nil, NewError(InternalError, "failed to update setting", err.Error())
	}

	return updated, nil
}

// deleteNotificationSetting deletes a notification setting
func (h *Handler) deleteNotificationSetting(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if h.notifications == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if input.ID == "" {
		return nil, NewError(InvalidParams, "id is required", nil)
	}

	if err := h.notificationsFor(ctx).DeleteSetting(ctx, input.ID); err != nil {
		return nil, NewError(InternalError, "failed to delete setting", err.Error())
	}

	return map[string]bool{"success": true}, nil
}

// getNotifications returns notifications
func (h *Handler) getNotifications(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if h.notifications == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		SettingID  string   `json:"settingId,omitempty"`
		Status     []string `json:"status,omitempty"`
		EventTypes []string `json:"eventTypes,omitempty"`
		Limit      int      `json:"limit,omitempty"`
		Offset     int      `json:"offset,omitempty"`
	}

	if len(params) > 0 {
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, NewError(InvalidParams, "invalid params", err.Error())
		}
	}

	filter := &notifications.NotificationsFilter{
		SettingID: input.SettingID,
		Limit:     input.Limit,
		Offset:    input.Offset,
	}

	if filter.Limit <= 0 {
		filter.Limit = 100
	}

	for _, s := range input.Status {
		filter.Status = append(filter.Status, notifications.DeliveryStatus(s))
	}

	for _, et := range input.EventTypes {
		filter.EventTypes = append(filter.EventTypes, notifications.EventType(et))
	}

	notifs, err := h.notificationsFor(ctx).ListNotifications(ctx, filter)
	if err != nil {
		return nil, NewError(InternalError, "failed to list notifications", err.Error())
	}

	return notifs, nil
}

// getNotification returns a single notification
func (h *Handler) getNotification(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if h.notifications == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if input.ID == "" {
		return nil, NewError(InvalidParams, "id is required", nil)
	}

	notif, err := h.notificationsFor(ctx).GetNotification(ctx, input.ID)
	if err != nil {
		return nil, NewError(InternalError, "failed to get notification", err.Error())
	}

	return notif, nil
}

// getNotificationStats returns notification statistics
func (h *Handler) getNotificationStats(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if h.notifications == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		SettingID string `json:"settingId"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if input.SettingID == "" {
		return nil, NewError(InvalidParams, "settingId is required", nil)
	}

	stats, err := h.notificationsFor(ctx).GetStats(ctx, input.SettingID)
	if err != nil {
		return nil, NewError(InternalError, "failed to get stats", err.Error())
	}

	return stats, nil
}

// getDeliveryHistory returns delivery history for a notification
func (h *Handler) getDeliveryHistory(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if h.notifications == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		NotificationID string `json:"notificationId"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if input.NotificationID == "" {
		return nil, NewError(InvalidParams, "notificationId is required", nil)
	}

	history, err := h.notificationsFor(ctx).GetDeliveryHistory(ctx, input.NotificationID)
	if err != nil {
		return nil, NewError(InternalError, "failed to get delivery history", err.Error())
	}

	return history, nil
}

// testNotificationSetting tests a notification setting
func (h *Handler) testNotificationSetting(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if h.notifications == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if input.ID == "" {
		return nil, NewError(InvalidParams, "id is required", nil)
	}

	result, err := h.notificationsFor(ctx).TestSetting(ctx, input.ID)
	if err != nil {
		return nil, NewError(InternalError, "failed to test setting", err.Error())
	}

	return result, nil
}

// retryNotification retries a failed notification
func (h *Handler) retryNotification(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if h.notifications == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if input.ID == "" {
		return nil, NewError(InvalidParams, "id is required", nil)
	}

	if err := h.notificationsFor(ctx).RetryNotification(ctx, input.ID); err != nil {
		return nil, NewError(InternalError, "failed to retry notification", err.Error())
	}

	return map[string]bool{"success": true}, nil
}

// cancelNotification cancels a pending notification
func (h *Handler) cancelNotification(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if h.notifications == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if input.ID == "" {
		return nil, NewError(InvalidParams, "id is required", nil)
	}

	if err := h.notificationsFor(ctx).CancelNotification(ctx, input.ID); err != nil {
		return nil, NewError(InternalError, "failed to cancel notification", err.Error())
	}

	return map[string]bool{"success": true}, nil
}

// Helper types and functions

// notificationFilterInput is a filter as a request gives it.
type notificationFilterInput = notifications.FilterInput

type notificationDestInput struct {
	WebhookURL      string   `json:"webhookURL,omitempty"`
	WebhookSecret   string   `json:"webhookSecret,omitempty"`
	EmailTo         []string `json:"emailTo,omitempty"`
	EmailSubject    string   `json:"emailSubject,omitempty"`
	SlackWebhookURL string   `json:"slackWebhookURL,omitempty"`
	SlackChannel    string   `json:"slackChannel,omitempty"`
	SlackUsername   string   `json:"slackUsername,omitempty"`
}

func parseJSONRPCNotifyFilter(input *notificationFilterInput) (*notifications.NotifyFilter, error) {
	if input == nil {
		return nil, nil
	}
	return input.Parse()
}

func parseJSONRPCDestination(input notificationDestInput) notifications.Destination {
	return notifications.Destination{
		WebhookURL:      input.WebhookURL,
		WebhookSecret:   input.WebhookSecret,
		EmailTo:         input.EmailTo,
		EmailSubject:    input.EmailSubject,
		SlackWebhookURL: input.SlackWebhookURL,
		SlackChannel:    input.SlackChannel,
		SlackUsername:   input.SlackUsername,
	}
}

// GetNotificationMethods returns a helper message about available notification methods
func GetNotificationMethods() []string {
	return []string{
		"notification_getSettings",
		"notification_getSetting",
		"notification_createSetting",
		"notification_updateSetting",
		"notification_deleteSetting",
		"notification_list",
		"notification_get",
		"notification_getStats",
		"notification_getHistory",
		"notification_test",
		"notification_retry",
		"notification_cancel",
		"notification_checkExpressions",
	}
}

// checkNotificationExpressions is a dry run of a setting's condition and
// payload over an optional sample log or transaction
// (notifications.ExpressionCheck); nothing is stored or sent.
func (h *Handler) checkNotificationExpressions(_ context.Context, params json.RawMessage) (interface{}, *Error) {
	checker, ok := h.notifications.(interface {
		CheckExpressions(notifications.ExpressionCheck) (*notifications.ExpressionCheckResult, error)
	})
	if !ok {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}
	var input notifications.ExpressionCheck
	if err := json.Unmarshal(params, &input); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}
	out, err := checker.CheckExpressions(input)
	if err != nil {
		if notifications.IsSampleError(err) {
			return nil, NewError(InvalidParams, err.Error(), nil)
		}
		return nil, NewError(InternalError, "failed to check expressions", err.Error())
	}
	return out, nil
}

// notificationsFor is the notification service as the request's API key
// sees it: its own settings only, everything for an operator key.
func (h *Handler) notificationsFor(ctx context.Context) notifications.Service {
	label, _ := middleware.APIKeyFromContext(ctx)
	return notifications.ForCaller(h.notifications, label)
}
