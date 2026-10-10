package graphql

import (
	"context"
	"fmt"
	"time"

	"github.com/0xmhha/indexer-go/pkg/api/middleware"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/graphql-go/graphql"
)

// SetNotificationService sets the notification service for resolvers
func (s *Schema) SetNotificationService(service notifications.Service) {
	s.notificationService = service
}

// resolveNotificationSettings returns all notification settings
func (s *Schema) resolveNotificationSettings(p graphql.ResolveParams) (interface{}, error) {
	if s.notificationService == nil {
		return []interface{}{}, nil
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	filter := parseNotificationSettingsFilter(p.Args["filter"])

	settings, err := s.notificationsFor(ctx).ListSettings(ctx, filter)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(settings))
	for _, setting := range settings {
		result = append(result, notificationSettingToMap(setting))
	}

	return result, nil
}

// resolveNotificationSetting returns a single notification setting
func (s *Schema) resolveNotificationSetting(p graphql.ResolveParams) (interface{}, error) {
	if s.notificationService == nil {
		return nil, fmt.Errorf("notification service not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	setting, err := s.notificationsFor(ctx).GetSetting(ctx, id)
	if err != nil {
		return nil, err
	}
	if setting == nil {
		return nil, nil
	}

	return notificationSettingToMap(setting), nil
}

// resolveNotifications returns notifications matching the filter
func (s *Schema) resolveNotifications(p graphql.ResolveParams) (interface{}, error) {
	if s.notificationService == nil {
		return []interface{}{}, nil
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	filter := parseNotificationsFilter(p.Args["filter"])

	notifs, err := s.notificationsFor(ctx).ListNotifications(ctx, filter)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(notifs))
	for _, notif := range notifs {
		result = append(result, notificationToMap(notif))
	}

	return result, nil
}

// resolveNotification returns a single notification
func (s *Schema) resolveNotification(p graphql.ResolveParams) (interface{}, error) {
	if s.notificationService == nil {
		return nil, fmt.Errorf("notification service not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	notif, err := s.notificationsFor(ctx).GetNotification(ctx, id)
	if err != nil {
		return nil, err
	}
	if notif == nil {
		return nil, nil
	}

	return notificationToMap(notif), nil
}

// resolveNotificationStats returns statistics for a notification setting
func (s *Schema) resolveNotificationStats(p graphql.ResolveParams) (interface{}, error) {
	if s.notificationService == nil {
		return nil, fmt.Errorf("notification service not enabled")
	}

	settingID, ok := p.Args["settingId"].(string)
	if !ok {
		return nil, fmt.Errorf("settingId is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	stats, err := s.notificationsFor(ctx).GetStats(ctx, settingID)
	if err != nil {
		return nil, err
	}

	return notificationStatsToMap(stats), nil
}

// resolveDeliveryHistory returns delivery history for a notification
func (s *Schema) resolveDeliveryHistory(p graphql.ResolveParams) (interface{}, error) {
	if s.notificationService == nil {
		return []interface{}{}, nil
	}

	notificationID, ok := p.Args["notificationId"].(string)
	if !ok {
		return nil, fmt.Errorf("notificationId is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	history, err := s.notificationsFor(ctx).GetDeliveryHistory(ctx, notificationID)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(history))
	for _, h := range history {
		result = append(result, deliveryHistoryToMap(h))
	}

	return result, nil
}

// Mutations

// resolveCreateNotificationSetting creates a new notification setting
func (s *Schema) resolveCreateNotificationSetting(p graphql.ResolveParams) (interface{}, error) {
	if s.notificationService == nil {
		return nil, fmt.Errorf("notification service not enabled")
	}

	input, ok := p.Args["input"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("input is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	setting, err := parseNotificationSettingInput(input)
	if err != nil {
		return nil, err
	}

	created, err := s.notificationsFor(ctx).CreateSetting(ctx, setting)
	if err != nil {
		return nil, err
	}

	return notificationSettingToMap(created), nil
}

// resolveUpdateNotificationSetting updates a notification setting
func (s *Schema) resolveUpdateNotificationSetting(p graphql.ResolveParams) (interface{}, error) {
	if s.notificationService == nil {
		return nil, fmt.Errorf("notification service not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	input, ok := p.Args["input"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("input is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	// Get existing setting
	existing, err := s.notificationsFor(ctx).GetSetting(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("setting not found: %s", id)
	}

	// Apply updates
	if err := applyNotificationSettingUpdates(existing, input); err != nil {
		return nil, err
	}

	updated, err := s.notificationsFor(ctx).UpdateSetting(ctx, existing)
	if err != nil {
		return nil, err
	}

	return notificationSettingToMap(updated), nil
}

// resolveDeleteNotificationSetting deletes a notification setting
func (s *Schema) resolveDeleteNotificationSetting(p graphql.ResolveParams) (interface{}, error) {
	if s.notificationService == nil {
		return nil, fmt.Errorf("notification service not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	if err := s.notificationsFor(ctx).DeleteSetting(ctx, id); err != nil {
		return nil, err
	}

	return true, nil
}

// resolveTestNotificationSetting tests a notification setting
func (s *Schema) resolveTestNotificationSetting(p graphql.ResolveParams) (interface{}, error) {
	if s.notificationService == nil {
		return nil, fmt.Errorf("notification service not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	result, err := s.notificationsFor(ctx).TestSetting(ctx, id)
	if err != nil {
		return nil, err
	}

	return deliveryResultToMap(result), nil
}

// resolveRetryNotification retries a failed notification
func (s *Schema) resolveRetryNotification(p graphql.ResolveParams) (interface{}, error) {
	if s.notificationService == nil {
		return nil, fmt.Errorf("notification service not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	if err := s.notificationsFor(ctx).RetryNotification(ctx, id); err != nil {
		return nil, err
	}

	return true, nil
}

// resolveCancelNotification cancels a pending notification
func (s *Schema) resolveCancelNotification(p graphql.ResolveParams) (interface{}, error) {
	if s.notificationService == nil {
		return nil, fmt.Errorf("notification service not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	if err := s.notificationsFor(ctx).CancelNotification(ctx, id); err != nil {
		return nil, err
	}

	return true, nil
}

// Helper functions

func parseNotificationSettingsFilter(v interface{}) *notifications.SettingsFilter {
	if v == nil {
		return &notifications.SettingsFilter{Limit: 100}
	}

	m, ok := v.(map[string]interface{})
	if !ok {
		return &notifications.SettingsFilter{Limit: 100}
	}

	filter := &notifications.SettingsFilter{
		Limit:  100,
		Offset: 0,
	}

	if types, ok := m["types"].([]interface{}); ok {
		for _, t := range types {
			if ts, ok := t.(string); ok {
				filter.Types = append(filter.Types, notifications.NotificationType(ts))
			}
		}
	}

	if enabled, ok := m["enabled"].(bool); ok {
		filter.Enabled = &enabled
	}

	if limit, ok := m["limit"].(int); ok {
		filter.Limit = limit
	}

	if offset, ok := m["offset"].(int); ok {
		filter.Offset = offset
	}

	return filter
}

func parseNotificationsFilter(v interface{}) *notifications.NotificationsFilter {
	if v == nil {
		return &notifications.NotificationsFilter{Limit: 100}
	}

	m, ok := v.(map[string]interface{})
	if !ok {
		return &notifications.NotificationsFilter{Limit: 100}
	}

	filter := &notifications.NotificationsFilter{
		Limit:  100,
		Offset: 0,
	}

	if settingID, ok := m["settingId"].(string); ok {
		filter.SettingID = settingID
	}

	if status, ok := m["status"].([]interface{}); ok {
		for _, s := range status {
			if ss, ok := s.(string); ok {
				filter.Status = append(filter.Status, notifications.DeliveryStatus(ss))
			}
		}
	}

	if eventTypes, ok := m["eventTypes"].([]interface{}); ok {
		for _, e := range eventTypes {
			if es, ok := e.(string); ok {
				filter.EventTypes = append(filter.EventTypes, notifications.EventType(es))
			}
		}
	}

	if limit, ok := m["limit"].(int); ok {
		filter.Limit = limit
	}

	if offset, ok := m["offset"].(int); ok {
		filter.Offset = offset
	}

	return filter
}

func parseNotificationSettingInput(input map[string]interface{}) (*notifications.NotificationSetting, error) {
	setting := &notifications.NotificationSetting{
		Enabled: true,
	}

	if name, ok := input["name"].(string); ok {
		setting.Name = name
	}

	if t, ok := input["type"].(string); ok {
		setting.Type = notifications.NotificationType(t)
	}

	if enabled, ok := input["enabled"].(bool); ok {
		setting.Enabled = enabled
	}
	if delivery, ok := input["delivery"].(string); ok {
		setting.Delivery = notifications.Delivery(delivery)
	}
	if condition, ok := input["condition"].(string); ok {
		setting.Condition = condition
	}
	if payload, ok := input["payload"].(string); ok {
		setting.Payload = payload
	}

	if eventTypes, ok := input["eventTypes"].([]interface{}); ok {
		for _, e := range eventTypes {
			if es, ok := e.(string); ok {
				setting.EventTypes = append(setting.EventTypes, notifications.EventType(es))
			}
		}
	}

	if filter, ok := input["filter"].(map[string]interface{}); ok {
		f, err := parseNotifyFilter(filter)
		if err != nil {
			return nil, err
		}
		setting.Filter = f
	}

	if dest, ok := input["destination"].(map[string]interface{}); ok {
		setting.Destination = parseNotificationDestination(dest)
	}

	return setting, nil
}

func parseNotifyFilter(m map[string]interface{}) (*notifications.NotifyFilter, error) {
	strs := func(v interface{}) []string {
		var out []string
		list, _ := v.([]interface{})
		for _, e := range list {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	in := notifications.FilterInput{Addresses: strs(m["addresses"]), Participants: strs(m["participants"])}
	if topics, ok := m["topics"].([]interface{}); ok {
		for _, position := range topics {
			in.Topics = append(in.Topics, strs(position))
		}
	}
	if minValue, ok := m["minValue"].(string); ok {
		in.MinValue = &minValue
	}
	if event, ok := m["event"].(string); ok {
		in.Event = event
	}
	return in.Parse()
}

func parseNotificationDestination(m map[string]interface{}) notifications.Destination {
	dest := notifications.Destination{}

	if url, ok := m["webhookURL"].(string); ok {
		dest.WebhookURL = url
	}
	if secret, ok := m["webhookSecret"].(string); ok {
		dest.WebhookSecret = secret
	}
	if emailTo, ok := m["emailTo"].([]interface{}); ok {
		for _, e := range emailTo {
			if es, ok := e.(string); ok {
				dest.EmailTo = append(dest.EmailTo, es)
			}
		}
	}
	if subject, ok := m["emailSubject"].(string); ok {
		dest.EmailSubject = subject
	}
	if url, ok := m["slackWebhookURL"].(string); ok {
		dest.SlackWebhookURL = url
	}
	if channel, ok := m["slackChannel"].(string); ok {
		dest.SlackChannel = channel
	}
	if username, ok := m["slackUsername"].(string); ok {
		dest.SlackUsername = username
	}

	return dest
}

func applyNotificationSettingUpdates(setting *notifications.NotificationSetting, input map[string]interface{}) error {
	if name, ok := input["name"].(string); ok {
		setting.Name = name
	}

	if enabled, ok := input["enabled"].(bool); ok {
		setting.Enabled = enabled
	}
	if delivery, ok := input["delivery"].(string); ok {
		setting.Delivery = notifications.Delivery(delivery)
	}
	if condition, ok := input["condition"].(string); ok {
		setting.Condition = condition
	}
	if payload, ok := input["payload"].(string); ok {
		setting.Payload = payload
	}

	if eventTypes, ok := input["eventTypes"].([]interface{}); ok {
		setting.EventTypes = nil
		for _, e := range eventTypes {
			if es, ok := e.(string); ok {
				setting.EventTypes = append(setting.EventTypes, notifications.EventType(es))
			}
		}
	}

	if filter, ok := input["filter"].(map[string]interface{}); ok {
		f, err := parseNotifyFilter(filter)
		if err != nil {
			return err
		}
		setting.Filter = f
	}

	if dest, ok := input["destination"].(map[string]interface{}); ok {
		setting.Destination = parseNotificationDestination(dest)
	}
	return nil
}

func notificationSettingToMap(setting *notifications.NotificationSetting) map[string]interface{} {
	result := map[string]interface{}{
		"id":          setting.ID,
		"name":        setting.Name,
		"type":        string(setting.Type),
		"enabled":     setting.Enabled,
		"delivery":    string(deliveryOf(setting)),
		"condition":   setting.Condition,
		"payload":     setting.Payload,
		"destination": notificationDestinationToMap(setting.Destination),
		"createdAt":   setting.CreatedAt.Format(time.RFC3339),
		"updatedAt":   setting.UpdatedAt.Format(time.RFC3339),
	}

	eventTypes := make([]string, 0, len(setting.EventTypes))
	for _, et := range setting.EventTypes {
		eventTypes = append(eventTypes, string(et))
	}
	result["eventTypes"] = eventTypes

	if setting.Filter != nil {
		result["filter"] = notifyFilterToMap(setting.Filter)
	}

	return result
}

func notificationDestinationToMap(dest notifications.Destination) map[string]interface{} {
	result := map[string]interface{}{}

	if dest.WebhookURL != "" {
		result["webhookURL"] = dest.WebhookURL
	}
	if dest.WebhookSecret != "" {
		result["webhookSecret"] = "***" // Mask the secret
	}
	if len(dest.EmailTo) > 0 {
		result["emailTo"] = dest.EmailTo
	}
	if dest.EmailSubject != "" {
		result["emailSubject"] = dest.EmailSubject
	}
	if dest.SlackWebhookURL != "" {
		result["slackWebhookURL"] = dest.SlackWebhookURL
	}
	if dest.SlackChannel != "" {
		result["slackChannel"] = dest.SlackChannel
	}
	if dest.SlackUsername != "" {
		result["slackUsername"] = dest.SlackUsername
	}

	return result
}

func notifyFilterToMap(filter *notifications.NotifyFilter) map[string]interface{} {
	result := map[string]interface{}{}

	if len(filter.Addresses) > 0 {
		addresses := make([]string, 0, len(filter.Addresses))
		for _, a := range filter.Addresses {
			addresses = append(addresses, a.Hex())
		}
		result["addresses"] = addresses
	}

	if filter.MinValue != nil && *filter.MinValue != "" {
		result["minValue"] = *filter.MinValue
	}
	if len(filter.Topics) > 0 {
		topics := make([][]string, len(filter.Topics))
		for i, position := range filter.Topics {
			topics[i] = []string{}
			for _, t := range position {
				topics[i] = append(topics[i], t.Hex())
			}
		}
		result["topics"] = topics
	}
	if filter.Event != "" {
		result["event"] = filter.Event
	}
	if len(filter.Participants) > 0 {
		participants := make([]string, 0, len(filter.Participants))
		for _, a := range filter.Participants {
			participants = append(participants, a.Hex())
		}
		result["participants"] = participants
	}

	return result
}

func notificationToMap(notif *notifications.Notification) map[string]interface{} {
	result := map[string]interface{}{
		"id":         notif.ID,
		"settingId":  notif.SettingID,
		"type":       string(notif.Type),
		"eventType":  string(notif.EventType),
		"status":     string(notif.Status),
		"retryCount": notif.RetryCount,
		"createdAt":  notif.CreatedAt.Format(time.RFC3339),
	}

	if notif.Error != "" {
		result["error"] = notif.Error
	}

	if notif.Payload != nil {
		result["blockNumber"] = fmt.Sprintf("%d", notif.Payload.BlockNumber)
		result["blockHash"] = notif.Payload.BlockHash.Hex()
	}

	if notif.SentAt != nil {
		result["sentAt"] = notif.SentAt.Format(time.RFC3339)
	}

	if notif.NextRetry != nil {
		result["nextRetry"] = notif.NextRetry.Format(time.RFC3339)
	}

	return result
}

func notificationStatsToMap(stats *notifications.NotificationStats) map[string]interface{} {
	result := map[string]interface{}{
		"settingId":     stats.SettingID,
		"totalSent":     int(stats.TotalSent),
		"totalFailed":   int(stats.TotalFailed),
		"successRate":   stats.SuccessRate,
		"avgDeliveryMs": stats.AvgDeliveryMs,
	}

	if stats.LastSentAt != nil {
		result["lastSentAt"] = stats.LastSentAt.Format(time.RFC3339)
	}

	if stats.LastFailedAt != nil {
		result["lastFailedAt"] = stats.LastFailedAt.Format(time.RFC3339)
	}

	return result
}

func deliveryHistoryToMap(history *notifications.DeliveryHistory) map[string]interface{} {
	result := map[string]interface{}{
		"notificationId": history.NotificationID,
		"settingId":      history.SettingID,
		"attempt":        history.Attempt,
		"timestamp":      history.Timestamp.Format(time.RFC3339),
	}

	if history.Result != nil {
		result["success"] = history.Result.Success
		result["durationMs"] = int(history.Result.Duration)
		if history.Result.StatusCode > 0 {
			result["statusCode"] = history.Result.StatusCode
		}
		if history.Result.Error != "" {
			result["error"] = history.Result.Error
		}
	}

	return result
}

func deliveryResultToMap(result *notifications.DeliveryResult) map[string]interface{} {
	return map[string]interface{}{
		"success":    result.Success,
		"statusCode": result.StatusCode,
		"error":      result.Error,
		"durationMs": int(result.Duration),
	}
}

// errAPIKeyRequired answers a notification operation without a valid API
// key: settings hold webhook destinations and secrets, and creating one
// makes the server send requests.
type errAPIKeyRequired struct{}

func (errAPIKeyRequired) Error() string {
	return "notification operations need an API key (api.keys; X-API-Key header)"
}

// Extensions implements gqlerrors.ExtendedError.
func (errAPIKeyRequired) Extensions() map[string]interface{} {
	return map[string]interface{}{"code": "UNAUTHENTICATED"}
}

// requireAPIKey runs resolve only for requests whose API key the server
// accepted (middleware.APIKeyIdentify).
func requireAPIKey(resolve graphql.FieldResolveFn) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		if _, ok := middleware.APIKeyFromContext(extractContext(p.Context)); !ok {
			return nil, errAPIKeyRequired{}
		}
		return resolve(p)
	}
}

// notificationsFor is the notification service as the request's API key
// sees it: its own settings only, everything for an operator key.
func (s *Schema) notificationsFor(ctx context.Context) notifications.Service {
	label, _ := middleware.APIKeyFromContext(ctx)
	return notifications.ForCaller(s.notificationService, label)
}

// deliveryOf is a setting's delivery, durable when unset.
func deliveryOf(st *notifications.NotificationSetting) notifications.Delivery {
	if st.Delivery == "" {
		return notifications.DeliveryDurable
	}
	return st.Delivery
}
