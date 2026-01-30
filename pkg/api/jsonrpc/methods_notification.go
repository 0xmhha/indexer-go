package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/ethereum/go-ethereum/common"
)

// notificationService holds the notification service reference
var notificationService notifications.Service

// SetNotificationService sets the notification service for JSON-RPC handlers
func (h *Handler) SetNotificationService(service notifications.Service) {
	notificationService = service
}

// Notification methods

// getNotificationSettings returns notification settings
func (h *Handler) getNotificationSettings(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if notificationService == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		Types   []string `json:"types,omitempty"`
		Enabled *bool    `json:"enabled,omitempty"`
		Limit   int      `json:"limit,omitempty"`
		Offset  int      `json:"offset,omitempty"`
	}

	if params != nil && len(params) > 0 {
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

	settings, err := notificationService.ListSettings(ctx, filter)
	if err != nil {
		return nil, NewError(InternalError, "failed to list settings", err.Error())
	}

	return settings, nil
}

// getNotificationSetting returns a single notification setting
func (h *Handler) getNotificationSetting(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if notificationService == nil {
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

	setting, err := notificationService.GetSetting(ctx, input.ID)
	if err != nil {
		return nil, NewError(InternalError, "failed to get setting", err.Error())
	}

	return setting, nil
}

// createNotificationSetting creates a new notification setting
func (h *Handler) createNotificationSetting(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if notificationService == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		Name        string                    `json:"name"`
		Type        string                    `json:"type"`
		Enabled     *bool                     `json:"enabled,omitempty"`
		EventTypes  []string                  `json:"eventTypes"`
		Filter      *notificationFilterInput  `json:"filter,omitempty"`
		Destination notificationDestInput     `json:"destination"`
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

	for _, et := range input.EventTypes {
		setting.EventTypes = append(setting.EventTypes, notifications.EventType(et))
	}

	if input.Filter != nil {
		setting.Filter = parseJSONRPCNotifyFilter(input.Filter)
	}

	setting.Destination = parseJSONRPCDestination(input.Destination)

	created, err := notificationService.CreateSetting(ctx, setting)
	if err != nil {
		return nil, NewError(InternalError, "failed to create setting", err.Error())
	}

	return created, nil
}

// updateNotificationSetting updates a notification setting
func (h *Handler) updateNotificationSetting(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if notificationService == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		ID          string                    `json:"id"`
		Name        *string                   `json:"name,omitempty"`
		Enabled     *bool                     `json:"enabled,omitempty"`
		EventTypes  []string                  `json:"eventTypes,omitempty"`
		Filter      *notificationFilterInput  `json:"filter,omitempty"`
		Destination *notificationDestInput    `json:"destination,omitempty"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if input.ID == "" {
		return nil, NewError(InvalidParams, "id is required", nil)
	}

	existing, err := notificationService.GetSetting(ctx, input.ID)
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

	if len(input.EventTypes) > 0 {
		existing.EventTypes = nil
		for _, et := range input.EventTypes {
			existing.EventTypes = append(existing.EventTypes, notifications.EventType(et))
		}
	}

	if input.Filter != nil {
		existing.Filter = parseJSONRPCNotifyFilter(input.Filter)
	}

	if input.Destination != nil {
		existing.Destination = parseJSONRPCDestination(*input.Destination)
	}

	updated, err := notificationService.UpdateSetting(ctx, existing)
	if err != nil {
		return nil, NewError(InternalError, "failed to update setting", err.Error())
	}

	return updated, nil
}

// deleteNotificationSetting deletes a notification setting
func (h *Handler) deleteNotificationSetting(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if notificationService == nil {
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

	if err := notificationService.DeleteSetting(ctx, input.ID); err != nil {
		return nil, NewError(InternalError, "failed to delete setting", err.Error())
	}

	return map[string]bool{"success": true}, nil
}

// getNotifications returns notifications
func (h *Handler) getNotifications(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if notificationService == nil {
		return nil, NewError(InternalError, "notification service not enabled", nil)
	}

	var input struct {
		SettingID  string   `json:"settingId,omitempty"`
		Status     []string `json:"status,omitempty"`
		EventTypes []string `json:"eventTypes,omitempty"`
		Limit      int      `json:"limit,omitempty"`
		Offset     int      `json:"offset,omitempty"`
	}

	if params != nil && len(params) > 0 {
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

	notifs, err := notificationService.ListNotifications(ctx, filter)
	if err != nil {
		return nil, NewError(InternalError, "failed to list notifications", err.Error())
	}

	return notifs, nil
}

// getNotification returns a single notification
func (h *Handler) getNotification(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if notificationService == nil {
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

	notif, err := notificationService.GetNotification(ctx, input.ID)
	if err != nil {
		return nil, NewError(InternalError, "failed to get notification", err.Error())
	}

	return notif, nil
}

// getNotificationStats returns notification statistics
func (h *Handler) getNotificationStats(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if notificationService == nil {
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

	stats, err := notificationService.GetStats(ctx, input.SettingID)
	if err != nil {
		return nil, NewError(InternalError, "failed to get stats", err.Error())
	}

	return stats, nil
}

// getDeliveryHistory returns delivery history for a notification
func (h *Handler) getDeliveryHistory(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if notificationService == nil {
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

	history, err := notificationService.GetDeliveryHistory(ctx, input.NotificationID)
	if err != nil {
		return nil, NewError(InternalError, "failed to get delivery history", err.Error())
	}

	return history, nil
}

// testNotificationSetting tests a notification setting
func (h *Handler) testNotificationSetting(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if notificationService == nil {
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

	result, err := notificationService.TestSetting(ctx, input.ID)
	if err != nil {
		return nil, NewError(InternalError, "failed to test setting", err.Error())
	}

	return result, nil
}

// retryNotification retries a failed notification
func (h *Handler) retryNotification(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if notificationService == nil {
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

	if err := notificationService.RetryNotification(ctx, input.ID); err != nil {
		return nil, NewError(InternalError, "failed to retry notification", err.Error())
	}

	return map[string]bool{"success": true}, nil
}

// cancelNotification cancels a pending notification
func (h *Handler) cancelNotification(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	if notificationService == nil {
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

	if err := notificationService.CancelNotification(ctx, input.ID); err != nil {
		return nil, NewError(InternalError, "failed to cancel notification", err.Error())
	}

	return map[string]bool{"success": true}, nil
}

// Helper types and functions

type notificationFilterInput struct {
	Addresses []string `json:"addresses,omitempty"`
	MinValue  *string  `json:"minValue,omitempty"`
}

type notificationDestInput struct {
	WebhookURL     string   `json:"webhookURL,omitempty"`
	WebhookSecret  string   `json:"webhookSecret,omitempty"`
	EmailTo        []string `json:"emailTo,omitempty"`
	EmailSubject   string   `json:"emailSubject,omitempty"`
	SlackWebhookURL string  `json:"slackWebhookURL,omitempty"`
	SlackChannel   string   `json:"slackChannel,omitempty"`
	SlackUsername  string   `json:"slackUsername,omitempty"`
}

func parseJSONRPCNotifyFilter(input *notificationFilterInput) *notifications.NotifyFilter {
	if input == nil {
		return nil
	}

	filter := &notifications.NotifyFilter{}

	for _, addr := range input.Addresses {
		if common.IsHexAddress(addr) {
			filter.Addresses = append(filter.Addresses, common.HexToAddress(addr))
		}
	}

	if input.MinValue != nil && *input.MinValue != "" {
		filter.MinValue = input.MinValue
	}

	return filter
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

// registerNotificationMethods registers all notification methods
func registerNotificationMethods() map[string]func(ctx context.Context, h *Handler, params json.RawMessage) (interface{}, *Error) {
	return map[string]func(ctx context.Context, h *Handler, params json.RawMessage) (interface{}, *Error){
		"notification_getSettings": func(ctx context.Context, h *Handler, params json.RawMessage) (interface{}, *Error) {
			return h.getNotificationSettings(ctx, params)
		},
		"notification_getSetting": func(ctx context.Context, h *Handler, params json.RawMessage) (interface{}, *Error) {
			return h.getNotificationSetting(ctx, params)
		},
		"notification_createSetting": func(ctx context.Context, h *Handler, params json.RawMessage) (interface{}, *Error) {
			return h.createNotificationSetting(ctx, params)
		},
		"notification_updateSetting": func(ctx context.Context, h *Handler, params json.RawMessage) (interface{}, *Error) {
			return h.updateNotificationSetting(ctx, params)
		},
		"notification_deleteSetting": func(ctx context.Context, h *Handler, params json.RawMessage) (interface{}, *Error) {
			return h.deleteNotificationSetting(ctx, params)
		},
		"notification_list": func(ctx context.Context, h *Handler, params json.RawMessage) (interface{}, *Error) {
			return h.getNotifications(ctx, params)
		},
		"notification_get": func(ctx context.Context, h *Handler, params json.RawMessage) (interface{}, *Error) {
			return h.getNotification(ctx, params)
		},
		"notification_getStats": func(ctx context.Context, h *Handler, params json.RawMessage) (interface{}, *Error) {
			return h.getNotificationStats(ctx, params)
		},
		"notification_getHistory": func(ctx context.Context, h *Handler, params json.RawMessage) (interface{}, *Error) {
			return h.getDeliveryHistory(ctx, params)
		},
		"notification_test": func(ctx context.Context, h *Handler, params json.RawMessage) (interface{}, *Error) {
			return h.testNotificationSetting(ctx, params)
		},
		"notification_retry": func(ctx context.Context, h *Handler, params json.RawMessage) (interface{}, *Error) {
			return h.retryNotification(ctx, params)
		},
		"notification_cancel": func(ctx context.Context, h *Handler, params json.RawMessage) (interface{}, *Error) {
			return h.cancelNotification(ctx, params)
		},
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
	}
}

// validateNotificationType validates a notification type
func validateNotificationType(t string) error {
	switch notifications.NotificationType(t) {
	case notifications.NotificationTypeWebhook,
		notifications.NotificationTypeEmail,
		notifications.NotificationTypeSlack:
		return nil
	default:
		return fmt.Errorf("invalid notification type: %s", t)
	}
}
