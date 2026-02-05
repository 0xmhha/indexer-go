package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

func TestNewSlackHandler(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	t.Run("with nil config", func(t *testing.T) {
		handler := NewSlackHandler(nil, logger)
		if handler == nil {
			t.Fatal("expected non-nil handler")
		}
		if handler.config == nil {
			t.Error("expected default config")
		}
		if handler.config.Timeout != 10*time.Second {
			t.Errorf("expected default timeout 10s, got %v", handler.config.Timeout)
		}
		if handler.config.DefaultUsername != "Indexer Bot" {
			t.Errorf("expected default username 'Indexer Bot', got %s", handler.config.DefaultUsername)
		}
		if handler.config.DefaultIconEmoji != ":robot_face:" {
			t.Errorf("expected default icon emoji, got %s", handler.config.DefaultIconEmoji)
		}
		if handler.config.RateLimitPerMinute != 30 {
			t.Errorf("expected default rate limit 30, got %d", handler.config.RateLimitPerMinute)
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		config := &SlackConfig{
			Enabled:            true,
			Timeout:            5 * time.Second,
			DefaultUsername:    "Custom Bot",
			DefaultIconEmoji:   ":wave:",
			RateLimitPerMinute: 60,
		}
		handler := NewSlackHandler(config, logger)
		if handler.config.Timeout != 5*time.Second {
			t.Errorf("expected custom timeout 5s, got %v", handler.config.Timeout)
		}
		if handler.config.DefaultUsername != "Custom Bot" {
			t.Errorf("expected custom username, got %s", handler.config.DefaultUsername)
		}
	})

	t.Run("with zero rate limit uses default", func(t *testing.T) {
		config := &SlackConfig{
			RateLimitPerMinute: 0,
		}
		handler := NewSlackHandler(config, logger)
		if handler.rateLimiter == nil {
			t.Error("expected rate limiter to be initialized")
		}
	})
}

func TestSlackHandler_Type(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := NewSlackHandler(nil, logger)

	if handler.Type() != NotificationTypeSlack {
		t.Errorf("expected type slack, got %v", handler.Type())
	}
}

func TestSlackHandler_Validate(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := NewSlackHandler(nil, logger)

	tests := []struct {
		name        string
		setting     *NotificationSetting
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid Slack webhook URL",
			setting: &NotificationSetting{
				Destination: Destination{
					SlackWebhookURL: "https://hooks.slack.com/services/T00/B00/XXX",
				},
			},
			expectError: false,
		},
		{
			name: "valid custom webhook URL",
			setting: &NotificationSetting{
				Destination: Destination{
					SlackWebhookURL: "https://api.example.com/slack/webhook",
				},
			},
			expectError: false,
		},
		{
			name: "valid HTTP URL",
			setting: &NotificationSetting{
				Destination: Destination{
					SlackWebhookURL: "http://localhost:8080/webhook",
				},
			},
			expectError: false,
		},
		{
			name: "empty webhook URL",
			setting: &NotificationSetting{
				Destination: Destination{
					SlackWebhookURL: "",
				},
			},
			expectError: true,
			errorMsg:    "Slack webhook URL is required",
		},
		{
			name: "invalid URL format",
			setting: &NotificationSetting{
				Destination: Destination{
					SlackWebhookURL: "not-a-url",
				},
			},
			expectError: true,
			errorMsg:    "invalid Slack webhook URL format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.Validate(tt.setting)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestIsValidSlackWebhookURL(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"https://hooks.slack.com/services/T00/B00/XXX", true},
		{"https://api.slack.com/webhook", true},
		{"http://localhost:8080/webhook", true},
		{"https://example.com/webhook", true},
		{"", false},
		{"not-a-url", false},
		{"ftp://example.com/webhook", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := isValidSlackWebhookURL(tt.url)
			if result != tt.expected {
				t.Errorf("isValidSlackWebhookURL(%q) = %v, want %v", tt.url, result, tt.expected)
			}
		})
	}
}

func TestSlackHandler_Deliver(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	t.Run("successful delivery", func(t *testing.T) {
		var receivedBody []byte
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer server.Close()

		handler := NewSlackHandler(&SlackConfig{
			Enabled:         true,
			Timeout:         5 * time.Second,
			DefaultUsername: "Test Bot",
		}, logger)

		notification := createTestSlackNotification()
		setting := &NotificationSetting{
			Destination: Destination{
				SlackWebhookURL: server.URL,
			},
		}

		result, err := handler.Deliver(ctx, notification, setting)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result.Success {
			t.Errorf("expected success, got failure: %s", result.Error)
		}
		if result.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", result.StatusCode)
		}
		if result.ResponseBody != "ok" {
			t.Errorf("expected response body 'ok', got %s", result.ResponseBody)
		}

		// Verify payload
		var message SlackMessage
		if err := json.Unmarshal(receivedBody, &message); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}
		if message.Username != "Test Bot" {
			t.Errorf("expected username 'Test Bot', got %s", message.Username)
		}
		if len(message.Attachments) == 0 {
			t.Error("expected attachments in message")
		}
	})

	t.Run("delivery with custom channel and username", func(t *testing.T) {
		var message SlackMessage
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &message)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer server.Close()

		handler := NewSlackHandler(&SlackConfig{
			DefaultUsername: "Default Bot",
		}, logger)

		notification := createTestSlackNotification()
		setting := &NotificationSetting{
			Destination: Destination{
				SlackWebhookURL: server.URL,
				SlackChannel:    "#alerts",
				SlackUsername:   "Custom Bot",
			},
		}

		result, _ := handler.Deliver(ctx, notification, setting)
		if !result.Success {
			t.Errorf("expected success: %s", result.Error)
		}

		if message.Channel != "#alerts" {
			t.Errorf("expected channel '#alerts', got %s", message.Channel)
		}
		if message.Username != "Custom Bot" {
			t.Errorf("expected username 'Custom Bot', got %s", message.Username)
		}
	})

	t.Run("server returns non-ok response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("invalid_token"))
		}))
		defer server.Close()

		handler := NewSlackHandler(nil, logger)
		notification := createTestSlackNotification()
		setting := &NotificationSetting{
			Destination: Destination{
				SlackWebhookURL: server.URL,
			},
		}

		result, _ := handler.Deliver(ctx, notification, setting)
		if result.Success {
			t.Error("expected failure for non-ok response")
		}
		if !strings.Contains(result.Error, "invalid_token") {
			t.Errorf("expected error to contain response, got: %s", result.Error)
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
		}))
		defer server.Close()

		handler := NewSlackHandler(nil, logger)
		notification := createTestSlackNotification()
		setting := &NotificationSetting{
			Destination: Destination{
				SlackWebhookURL: server.URL,
			},
		}

		result, _ := handler.Deliver(ctx, notification, setting)
		if result.Success {
			t.Error("expected failure for 500 response")
		}
		if result.StatusCode != http.StatusInternalServerError {
			t.Errorf("expected status 500, got %d", result.StatusCode)
		}
	})

	t.Run("connection failure", func(t *testing.T) {
		handler := NewSlackHandler(&SlackConfig{
			Timeout: 1 * time.Second,
		}, logger)
		notification := createTestSlackNotification()
		setting := &NotificationSetting{
			Destination: Destination{
				SlackWebhookURL: "http://localhost:59998/nonexistent",
			},
		}

		result, err := handler.Deliver(ctx, notification, setting)
		if err == nil {
			t.Error("expected error for connection failure")
		}
		if result.Success {
			t.Error("expected failure")
		}
	})

	t.Run("rate limit exceeded", func(t *testing.T) {
		handler := NewSlackHandler(&SlackConfig{
			RateLimitPerMinute: 1,
		}, logger)

		notification := createTestSlackNotification()
		setting := &NotificationSetting{
			Destination: Destination{
				SlackWebhookURL: "https://example.com/webhook",
			},
		}

		// Consume the token
		handler.rateLimiter.allow()

		result, err := handler.Deliver(ctx, notification, setting)
		if err == nil {
			t.Error("expected rate limit error")
		}
		if result.Success {
			t.Error("expected failure when rate limited")
		}
		if !strings.Contains(result.Error, "rate limit") {
			t.Errorf("expected rate limit error message, got: %s", result.Error)
		}
	})
}

func TestSlackHandler_BuildMessage(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	t.Run("with defaults", func(t *testing.T) {
		handler := NewSlackHandler(&SlackConfig{
			DefaultUsername:  "Default Bot",
			DefaultIconEmoji: ":robot_face:",
		}, logger)

		notification := createTestSlackNotification()
		setting := &NotificationSetting{
			Destination: Destination{},
		}

		message := handler.buildMessage(notification, setting)

		if message.Username != "Default Bot" {
			t.Errorf("expected default username, got %s", message.Username)
		}
		if message.IconEmoji != ":robot_face:" {
			t.Errorf("expected default icon emoji, got %s", message.IconEmoji)
		}
		if message.Channel != "" {
			t.Errorf("expected empty channel, got %s", message.Channel)
		}
	})

	t.Run("with custom settings", func(t *testing.T) {
		handler := NewSlackHandler(&SlackConfig{
			DefaultUsername:  "Default Bot",
			DefaultIconEmoji: ":robot_face:",
		}, logger)

		notification := createTestSlackNotification()
		setting := &NotificationSetting{
			Destination: Destination{
				SlackChannel:  "#custom-channel",
				SlackUsername: "Custom Bot",
			},
		}

		message := handler.buildMessage(notification, setting)

		if message.Username != "Custom Bot" {
			t.Errorf("expected custom username, got %s", message.Username)
		}
		if message.Channel != "#custom-channel" {
			t.Errorf("expected custom channel, got %s", message.Channel)
		}
	})
}

func TestSlackHandler_BuildAttachments(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := NewSlackHandler(nil, logger)

	tests := []struct {
		name          string
		eventType     EventType
		expectedColor string
		expectedTitle string
	}{
		{
			name:          "block event",
			eventType:     EventTypeBlock,
			expectedColor: "#2563eb",
			expectedTitle: "🧱 New Block #12345",
		},
		{
			name:          "transaction event",
			eventType:     EventTypeTransaction,
			expectedColor: "#16a34a",
			expectedTitle: "💸 New Transaction in Block #12345",
		},
		{
			name:          "log event",
			eventType:     EventTypeLog,
			expectedColor: "#9333ea",
			expectedTitle: "📋 Event Log in Block #12345",
		},
		{
			name:          "contract creation event",
			eventType:     EventTypeContractCreation,
			expectedColor: "#ea580c",
			expectedTitle: "📄 Contract Created in Block #12345",
		},
		{
			name:          "token transfer event",
			eventType:     EventTypeTokenTransfer,
			expectedColor: "#0891b2",
			expectedTitle: "🔄 Token Transfer in Block #12345",
		},
		{
			name:          "unknown event",
			eventType:     EventType("custom"),
			expectedColor: "#6b7280",
			expectedTitle: "custom Event",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notification := createTestSlackNotificationWithType(tt.eventType)
			attachments := handler.buildAttachments(notification)

			if len(attachments) != 1 {
				t.Fatalf("expected 1 attachment, got %d", len(attachments))
			}

			attachment := attachments[0]
			if attachment.Color != tt.expectedColor {
				t.Errorf("expected color %s, got %s", tt.expectedColor, attachment.Color)
			}
			if attachment.Title != tt.expectedTitle {
				t.Errorf("expected title %q, got %q", tt.expectedTitle, attachment.Title)
			}

			// Verify basic fields
			if len(attachment.Fields) < 3 {
				t.Errorf("expected at least 3 fields, got %d", len(attachment.Fields))
			}
		})
	}
}

func TestSlackHandler_AddEventSpecificFields(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := NewSlackHandler(nil, logger)

	t.Run("transaction event with from/to/value", func(t *testing.T) {
		data := map[string]interface{}{
			"From":  "0x1234567890abcdef1234567890abcdef12345678",
			"To":    "0xabcdef1234567890abcdef1234567890abcdef12",
			"Value": "1000000000000000000",
		}
		dataBytes, _ := json.Marshal(data)

		notification := &Notification{
			EventType: EventTypeTransaction,
			Payload: &EventPayload{
				Data: dataBytes,
			},
		}

		var fields []SlackField
		handler.addEventSpecificFields(&fields, notification)

		if len(fields) != 3 {
			t.Errorf("expected 3 fields (from, to, value), got %d", len(fields))
		}
	})

	t.Run("transaction event with zero value", func(t *testing.T) {
		data := map[string]interface{}{
			"From":  "0x1234567890abcdef1234567890abcdef12345678",
			"To":    "0xabcdef1234567890abcdef1234567890abcdef12",
			"Value": "0",
		}
		dataBytes, _ := json.Marshal(data)

		notification := &Notification{
			EventType: EventTypeTransaction,
			Payload: &EventPayload{
				Data: dataBytes,
			},
		}

		var fields []SlackField
		handler.addEventSpecificFields(&fields, notification)

		// Value should not be added when it's "0"
		for _, f := range fields {
			if f.Title == "Value" {
				t.Error("expected zero value to be omitted")
			}
		}
	})

	t.Run("log event with address", func(t *testing.T) {
		data := map[string]interface{}{
			"Address": "0x1234567890abcdef1234567890abcdef12345678",
		}
		dataBytes, _ := json.Marshal(data)

		notification := &Notification{
			EventType: EventTypeLog,
			Payload: &EventPayload{
				Data: dataBytes,
			},
		}

		var fields []SlackField
		handler.addEventSpecificFields(&fields, notification)

		if len(fields) != 1 {
			t.Errorf("expected 1 field (Contract), got %d", len(fields))
		}
		if fields[0].Title != "Contract" {
			t.Errorf("expected field title 'Contract', got %s", fields[0].Title)
		}
	})

	t.Run("invalid JSON data", func(t *testing.T) {
		notification := &Notification{
			EventType: EventTypeTransaction,
			Payload: &EventPayload{
				Data: []byte("invalid-json"),
			},
		}

		var fields []SlackField
		handler.addEventSpecificFields(&fields, notification)

		// Should not panic, just return without adding fields
		if len(fields) != 0 {
			t.Errorf("expected 0 fields for invalid JSON, got %d", len(fields))
		}
	})
}

// Helper functions

func createTestSlackNotification() *Notification {
	return createTestSlackNotificationWithType(EventTypeBlock)
}

func createTestSlackNotificationWithType(eventType EventType) *Notification {
	now := time.Now()
	blockHash := common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")

	return &Notification{
		ID:        "test-slack-notification-001",
		SettingID: "test-setting-001",
		Type:      NotificationTypeSlack,
		EventType: eventType,
		Payload: &EventPayload{
			ChainID:     1,
			BlockNumber: 12345,
			BlockHash:   blockHash,
			Timestamp:   now,
			EventType:   eventType,
			Data:        json.RawMessage(`{"number":12345}`),
		},
		Status:    DeliveryStatusPending,
		CreatedAt: now,
	}
}
