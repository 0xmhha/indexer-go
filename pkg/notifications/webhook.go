package notifications

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

// WebhookHandler handles webhook notification delivery.
type WebhookHandler struct {
	config *WebhookConfig
	client *http.Client
	logger *zap.Logger
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(config *WebhookConfig, logger *zap.Logger) *WebhookHandler {
	if config == nil {
		config = &WebhookConfig{
			Enabled:         true,
			Timeout:         10 * time.Second,
			MaxRetries:      3,
			MaxConcurrent:   10,
			SignatureHeader: "X-Signature-256",
		}
	}

	return &WebhookHandler{
		config: config,
		client: &http.Client{
			Timeout: config.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		logger: logger.Named("webhook"),
	}
}

// Type returns the notification type.
func (h *WebhookHandler) Type() NotificationType {
	return NotificationTypeWebhook
}

// Validate validates a webhook notification setting.
func (h *WebhookHandler) Validate(setting *NotificationSetting) error {
	if setting.Destination.WebhookURL == "" {
		return fmt.Errorf("webhook URL is required")
	}

	// Validate URL format
	parsedURL, err := url.Parse(setting.Destination.WebhookURL)
	if err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("webhook URL must use http or https scheme")
	}

	// Check allowed hosts if configured
	if len(h.config.AllowedHosts) > 0 {
		allowed := false
		for _, host := range h.config.AllowedHosts {
			if strings.EqualFold(parsedURL.Host, host) || strings.HasSuffix(parsedURL.Host, "."+host) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("webhook host %s is not in allowed hosts list", parsedURL.Host)
		}
	}

	return nil
}

// Deliver delivers a webhook notification.
func (h *WebhookHandler) Deliver(ctx context.Context, notification *Notification, setting *NotificationSetting) (*DeliveryResult, error) {
	start := time.Now()
	result := &DeliveryResult{
		DeliveredAt: start,
	}

	// Prepare payload
	webhookPayload := &WebhookPayload{
		ID:        notification.ID,
		EventType: string(notification.EventType),
		Timestamp: notification.CreatedAt.UTC().Format(time.RFC3339),
		Data:      notification.Payload,
	}

	payloadBytes, err := json.Marshal(webhookPayload)
	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("failed to marshal payload: %v", err)
		result.Duration = time.Since(start).Milliseconds()
		return result, err
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, setting.Destination.WebhookURL, bytes.NewReader(payloadBytes))
	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("failed to create request: %v", err)
		result.Duration = time.Since(start).Milliseconds()
		return result, err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Indexer-Webhook/1.0")
	req.Header.Set("X-Webhook-ID", notification.ID)
	req.Header.Set("X-Event-Type", string(notification.EventType))

	// Add custom headers
	for key, value := range setting.Destination.WebhookHeaders {
		req.Header.Set(key, value)
	}

	// Add signature if secret is configured
	if setting.Destination.WebhookSecret != "" {
		signature := h.computeSignature(payloadBytes, setting.Destination.WebhookSecret)
		headerName := h.config.SignatureHeader
		if headerName == "" {
			headerName = "X-Signature-256"
		}
		req.Header.Set(headerName, "sha256="+signature)
	}

	// Send request
	resp, err := h.client.Do(req)
	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("request failed: %v", err)
		result.Duration = time.Since(start).Milliseconds()
		return result, err
	}
	defer resp.Body.Close()

	// Read response body
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*10)) // Limit to 10KB
	result.StatusCode = resp.StatusCode
	result.ResponseBody = string(bodyBytes)
	result.Duration = time.Since(start).Milliseconds()

	// Check status code
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Success = true
		h.logger.Debug("webhook delivered successfully",
			zap.String("notification_id", notification.ID),
			zap.Int("status_code", resp.StatusCode),
			zap.Int64("duration_ms", result.Duration))
	} else {
		result.Success = false
		result.Error = fmt.Sprintf("webhook returned status %d", resp.StatusCode)
		h.logger.Warn("webhook delivery failed",
			zap.String("notification_id", notification.ID),
			zap.Int("status_code", resp.StatusCode),
			zap.String("response", result.ResponseBody))
	}

	return result, nil
}

// computeSignature computes HMAC-SHA256 signature for the payload.
func (h *WebhookHandler) computeSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// WebhookPayload is the payload sent to webhook endpoints.
type WebhookPayload struct {
	ID        string        `json:"id"`
	EventType string        `json:"event_type"`
	Timestamp string        `json:"timestamp"`
	Data      *EventPayload `json:"data"`
}

// VerifyWebhookSignature verifies a webhook signature.
// This can be used by webhook recipients to verify the authenticity of the request.
func VerifyWebhookSignature(payload []byte, signature, secret string) bool {
	// Remove "sha256=" prefix if present
	signature = strings.TrimPrefix(signature, "sha256=")

	expected, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	actual := mac.Sum(nil)

	return hmac.Equal(expected, actual)
}
