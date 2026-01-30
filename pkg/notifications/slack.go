package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// SlackHandler handles Slack notification delivery.
type SlackHandler struct {
	config      *SlackConfig
	client      *http.Client
	logger      *zap.Logger
	rateLimiter *rateLimiter
}

// NewSlackHandler creates a new Slack handler.
func NewSlackHandler(config *SlackConfig, logger *zap.Logger) *SlackHandler {
	if config == nil {
		config = &SlackConfig{
			Enabled:            true,
			Timeout:            10 * time.Second,
			MaxRetries:         3,
			DefaultUsername:    "Indexer Bot",
			DefaultIconEmoji:   ":robot_face:",
			RateLimitPerMinute: 30,
		}
	}

	rateLimit := config.RateLimitPerMinute
	if rateLimit <= 0 {
		rateLimit = 30
	}

	return &SlackHandler{
		config: config,
		client: &http.Client{
			Timeout: config.Timeout,
		},
		logger:      logger.Named("slack"),
		rateLimiter: newRateLimiter(rateLimit),
	}
}

// Type returns the notification type.
func (h *SlackHandler) Type() NotificationType {
	return NotificationTypeSlack
}

// Validate validates a Slack notification setting.
func (h *SlackHandler) Validate(setting *NotificationSetting) error {
	if setting.Destination.SlackWebhookURL == "" {
		return fmt.Errorf("Slack webhook URL is required")
	}

	// Validate that it looks like a Slack webhook URL
	if !isValidSlackWebhookURL(setting.Destination.SlackWebhookURL) {
		return fmt.Errorf("invalid Slack webhook URL format")
	}

	return nil
}

// isValidSlackWebhookURL checks if the URL looks like a valid Slack webhook.
func isValidSlackWebhookURL(url string) bool {
	// Slack webhook URLs typically start with https://hooks.slack.com/services/
	// but we allow other formats for custom Slack integrations
	return len(url) > 0 && (url[:8] == "https://" || url[:7] == "http://")
}

// Deliver delivers a Slack notification.
func (h *SlackHandler) Deliver(ctx context.Context, notification *Notification, setting *NotificationSetting) (*DeliveryResult, error) {
	start := time.Now()
	result := &DeliveryResult{
		DeliveredAt: start,
	}

	// Check rate limit
	if !h.rateLimiter.allow() {
		result.Success = false
		result.Error = "rate limit exceeded"
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("rate limit exceeded")
	}

	// Build Slack message
	message := h.buildMessage(notification, setting)

	// Marshal message
	payload, err := json.Marshal(message)
	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("failed to marshal message: %v", err)
		result.Duration = time.Since(start).Milliseconds()
		return result, err
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, setting.Destination.SlackWebhookURL, bytes.NewReader(payload))
	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("failed to create request: %v", err)
		result.Duration = time.Since(start).Milliseconds()
		return result, err
	}

	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := h.client.Do(req)
	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("request failed: %v", err)
		result.Duration = time.Since(start).Milliseconds()
		return result, err
	}
	defer resp.Body.Close()

	// Read response
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	result.StatusCode = resp.StatusCode
	result.ResponseBody = string(bodyBytes)
	result.Duration = time.Since(start).Milliseconds()

	// Slack returns "ok" on success
	if resp.StatusCode == http.StatusOK && result.ResponseBody == "ok" {
		result.Success = true
		h.logger.Debug("slack notification delivered",
			zap.String("notification_id", notification.ID),
			zap.Int64("duration_ms", result.Duration))
	} else {
		result.Success = false
		result.Error = fmt.Sprintf("slack returned status %d: %s", resp.StatusCode, result.ResponseBody)
		h.logger.Warn("slack delivery failed",
			zap.String("notification_id", notification.ID),
			zap.Int("status_code", resp.StatusCode),
			zap.String("response", result.ResponseBody))
	}

	return result, nil
}

// buildMessage builds a Slack message.
func (h *SlackHandler) buildMessage(notification *Notification, setting *NotificationSetting) *SlackMessage {
	username := setting.Destination.SlackUsername
	if username == "" {
		username = h.config.DefaultUsername
	}

	iconEmoji := h.config.DefaultIconEmoji
	if iconEmoji == "" {
		iconEmoji = ":robot_face:"
	}

	message := &SlackMessage{
		Username:  username,
		IconEmoji: iconEmoji,
	}

	if setting.Destination.SlackChannel != "" {
		message.Channel = setting.Destination.SlackChannel
	}

	// Build attachments based on event type
	message.Attachments = h.buildAttachments(notification)

	return message
}

// buildAttachments builds Slack message attachments.
func (h *SlackHandler) buildAttachments(notification *Notification) []SlackAttachment {
	var color string
	var title string

	switch notification.EventType {
	case EventTypeBlock:
		color = "#2563eb" // Blue
		title = fmt.Sprintf("🧱 New Block #%d", notification.Payload.BlockNumber)
	case EventTypeTransaction:
		color = "#16a34a" // Green
		title = fmt.Sprintf("💸 New Transaction in Block #%d", notification.Payload.BlockNumber)
	case EventTypeLog:
		color = "#9333ea" // Purple
		title = fmt.Sprintf("📋 Event Log in Block #%d", notification.Payload.BlockNumber)
	case EventTypeContractCreation:
		color = "#ea580c" // Orange
		title = fmt.Sprintf("📄 Contract Created in Block #%d", notification.Payload.BlockNumber)
	case EventTypeTokenTransfer:
		color = "#0891b2" // Cyan
		title = fmt.Sprintf("🔄 Token Transfer in Block #%d", notification.Payload.BlockNumber)
	default:
		color = "#6b7280" // Gray
		title = fmt.Sprintf("%s Event", notification.EventType)
	}

	fields := []SlackField{
		{
			Title: "Block Number",
			Value: fmt.Sprintf("%d", notification.Payload.BlockNumber),
			Short: true,
		},
		{
			Title: "Timestamp",
			Value: notification.Payload.Timestamp.UTC().Format(time.RFC3339),
			Short: true,
		},
		{
			Title: "Block Hash",
			Value: fmt.Sprintf("`%s`", notification.Payload.BlockHash.Hex()),
			Short: false,
		},
	}

	// Add event-specific details
	h.addEventSpecificFields(&fields, notification)

	attachment := SlackAttachment{
		Color:      color,
		Title:      title,
		Fields:     fields,
		Footer:     fmt.Sprintf("Notification ID: %s", notification.ID),
		FooterIcon: "https://cdn.example.com/indexer-icon.png",
		Ts:         notification.CreatedAt.Unix(),
	}

	return []SlackAttachment{attachment}
}

// addEventSpecificFields adds event-specific fields to the Slack message.
func (h *SlackHandler) addEventSpecificFields(fields *[]SlackField, notification *Notification) {
	// Parse the event data
	var data map[string]interface{}
	if err := json.Unmarshal(notification.Payload.Data, &data); err != nil {
		return
	}

	switch notification.EventType {
	case EventTypeTransaction:
		if from, ok := data["From"].(string); ok {
			*fields = append(*fields, SlackField{
				Title: "From",
				Value: fmt.Sprintf("`%s`", from),
				Short: true,
			})
		}
		if to, ok := data["To"].(string); ok {
			*fields = append(*fields, SlackField{
				Title: "To",
				Value: fmt.Sprintf("`%s`", to),
				Short: true,
			})
		}
		if value, ok := data["Value"].(string); ok && value != "0" {
			*fields = append(*fields, SlackField{
				Title: "Value",
				Value: value,
				Short: true,
			})
		}
	case EventTypeLog:
		if address, ok := data["Address"].(string); ok {
			*fields = append(*fields, SlackField{
				Title: "Contract",
				Value: fmt.Sprintf("`%s`", address),
				Short: false,
			})
		}
	}
}

// SlackMessage represents a Slack incoming webhook message.
type SlackMessage struct {
	Channel     string            `json:"channel,omitempty"`
	Username    string            `json:"username,omitempty"`
	IconEmoji   string            `json:"icon_emoji,omitempty"`
	IconURL     string            `json:"icon_url,omitempty"`
	Text        string            `json:"text,omitempty"`
	Attachments []SlackAttachment `json:"attachments,omitempty"`
	Blocks      []SlackBlock      `json:"blocks,omitempty"`
}

// SlackAttachment represents a Slack message attachment.
type SlackAttachment struct {
	Color      string       `json:"color,omitempty"`
	Pretext    string       `json:"pretext,omitempty"`
	AuthorName string       `json:"author_name,omitempty"`
	AuthorLink string       `json:"author_link,omitempty"`
	AuthorIcon string       `json:"author_icon,omitempty"`
	Title      string       `json:"title,omitempty"`
	TitleLink  string       `json:"title_link,omitempty"`
	Text       string       `json:"text,omitempty"`
	Fields     []SlackField `json:"fields,omitempty"`
	ImageURL   string       `json:"image_url,omitempty"`
	ThumbURL   string       `json:"thumb_url,omitempty"`
	Footer     string       `json:"footer,omitempty"`
	FooterIcon string       `json:"footer_icon,omitempty"`
	Ts         int64        `json:"ts,omitempty"`
}

// SlackField represents a field in a Slack attachment.
type SlackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

// SlackBlock represents a Slack Block Kit block.
type SlackBlock struct {
	Type     string      `json:"type"`
	Text     *SlackText  `json:"text,omitempty"`
	Elements interface{} `json:"elements,omitempty"`
}

// SlackText represents text in a Slack block.
type SlackText struct {
	Type string `json:"type"` // "plain_text" or "mrkdwn"
	Text string `json:"text"`
}
