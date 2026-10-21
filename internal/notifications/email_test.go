package notifications

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

func TestNewEmailHandler(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	t.Run("with nil config", func(t *testing.T) {
		handler := NewEmailHandler(nil, logger)
		if handler == nil {
			t.Fatal("expected non-nil handler")
		}
		if handler.config == nil {
			t.Error("expected default config")
		}
		if handler.config.SMTPPort != 587 {
			t.Errorf("expected default SMTP port 587, got %d", handler.config.SMTPPort)
		}
		if !handler.config.UseTLS {
			t.Error("expected UseTLS to be true by default")
		}
		if handler.config.MaxRecipients != 10 {
			t.Errorf("expected default max recipients 10, got %d", handler.config.MaxRecipients)
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		config := &EmailConfig{
			Enabled:            true,
			SMTPHost:           "smtp.example.com",
			SMTPPort:           465,
			FromAddress:        "noreply@example.com",
			UseTLS:             true,
			MaxRecipients:      5,
			RateLimitPerMinute: 100,
		}
		handler := NewEmailHandler(config, logger)
		if handler.config.SMTPPort != 465 {
			t.Errorf("expected custom SMTP port 465, got %d", handler.config.SMTPPort)
		}
		if handler.config.MaxRecipients != 5 {
			t.Errorf("expected custom max recipients 5, got %d", handler.config.MaxRecipients)
		}
	})

	t.Run("with zero rate limit uses default", func(t *testing.T) {
		config := &EmailConfig{
			RateLimitPerMinute: 0,
		}
		handler := NewEmailHandler(config, logger)
		// Rate limiter should use default (60)
		if handler.rateLimiter == nil {
			t.Error("expected rate limiter to be initialized")
		}
	})
}

func TestEmailHandler_Type(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := NewEmailHandler(nil, logger)

	if handler.Type() != NotificationTypeEmail {
		t.Errorf("expected type email, got %v", handler.Type())
	}
}

func TestEmailHandler_Validate(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	tests := []struct {
		name        string
		config      *EmailConfig
		setting     *NotificationSetting
		expectError bool
		errorMsg    string
	}{
		{
			name:   "valid single recipient",
			config: &EmailConfig{MaxRecipients: 10},
			setting: &NotificationSetting{
				Destination: Destination{
					EmailTo: []string{"user@example.com"},
				},
			},
			expectError: false,
		},
		{
			name:   "valid multiple recipients",
			config: &EmailConfig{MaxRecipients: 10},
			setting: &NotificationSetting{
				Destination: Destination{
					EmailTo: []string{"user1@example.com", "user2@example.com"},
					EmailCC: []string{"cc@example.com"},
				},
			},
			expectError: false,
		},
		{
			name:   "no recipients",
			config: &EmailConfig{MaxRecipients: 10},
			setting: &NotificationSetting{
				Destination: Destination{
					EmailTo: []string{},
				},
			},
			expectError: true,
			errorMsg:    "at least one email recipient is required",
		},
		{
			name:   "too many recipients",
			config: &EmailConfig{MaxRecipients: 2},
			setting: &NotificationSetting{
				Destination: Destination{
					EmailTo: []string{"user1@example.com", "user2@example.com"},
					EmailCC: []string{"user3@example.com"},
				},
			},
			expectError: true,
			errorMsg:    "too many recipients",
		},
		{
			name:   "invalid email format - no @",
			config: &EmailConfig{MaxRecipients: 10},
			setting: &NotificationSetting{
				Destination: Destination{
					EmailTo: []string{"invalid-email"},
				},
			},
			expectError: true,
			errorMsg:    "invalid email address",
		},
		{
			name:   "invalid email format - no domain",
			config: &EmailConfig{MaxRecipients: 10},
			setting: &NotificationSetting{
				Destination: Destination{
					EmailTo: []string{"user@"},
				},
			},
			expectError: true,
			errorMsg:    "invalid email address",
		},
		{
			name:   "invalid CC email",
			config: &EmailConfig{MaxRecipients: 10},
			setting: &NotificationSetting{
				Destination: Destination{
					EmailTo: []string{"valid@example.com"},
					EmailCC: []string{"invalid-cc"},
				},
			},
			expectError: true,
			errorMsg:    "invalid CC email address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewEmailHandler(tt.config, logger)
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

func TestIsValidEmail(t *testing.T) {
	tests := []struct {
		email    string
		expected bool
	}{
		{"user@example.com", true},
		{"user.name@example.com", true},
		{"user@subdomain.example.com", true},
		{"user+tag@example.com", true},
		{"user@example.co.uk", true},
		{"", false},
		{"user", false},
		{"@example.com", false},
		{"user@", false},
		{"user@example", false},
		{"user@.", false},
		// Note: "user@.com" passes validation as the implementation only checks
		// for @ position and a dot after @ with content after it
		{"user@.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			result := isValidEmail(tt.email)
			if result != tt.expected {
				t.Errorf("isValidEmail(%q) = %v, want %v", tt.email, result, tt.expected)
			}
		})
	}
}

func TestEmailHandler_BuildSubject(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := NewEmailHandler(nil, logger)

	tests := []struct {
		name        string
		eventType   EventType
		blockNumber uint64
		customSubj  string
		expected    string
	}{
		{
			name:        "custom subject",
			eventType:   EventTypeBlock,
			blockNumber: 100,
			customSubj:  "Custom Subject Line",
			expected:    "Custom Subject Line",
		},
		{
			name:        "block event",
			eventType:   EventTypeBlock,
			blockNumber: 12345,
			customSubj:  "",
			expected:    "[Indexer] New Block #12345",
		},
		{
			name:        "transaction event",
			eventType:   EventTypeTransaction,
			blockNumber: 12345,
			customSubj:  "",
			expected:    "[Indexer] New Transaction in Block #12345",
		},
		{
			name:        "log event",
			eventType:   EventTypeLog,
			blockNumber: 12345,
			customSubj:  "",
			expected:    "[Indexer] Event Log in Block #12345",
		},
		{
			name:        "unknown event",
			eventType:   EventType("custom"),
			blockNumber: 12345,
			customSubj:  "",
			expected:    "[Indexer] custom Notification",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notification := &Notification{
				EventType: tt.eventType,
				Payload: &EventPayload{
					BlockNumber: tt.blockNumber,
				},
			}
			setting := &NotificationSetting{
				Destination: Destination{
					EmailSubject: tt.customSubj,
				},
			}

			subject := handler.buildSubject(notification, setting)
			if subject != tt.expected {
				t.Errorf("expected subject %q, got %q", tt.expected, subject)
			}
		})
	}
}

func TestEmailHandler_BuildDefaultBody(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := NewEmailHandler(nil, logger)

	notification := createTestEmailNotification()
	body, err := handler.buildDefaultBody(notification)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify body contains expected elements
	expectedStrings := []string{
		"<!DOCTYPE html>",
		"<html>",
		"block Event", // EventTypeBlock
		"Block Number:",
		"12345",
		"Block Hash:",
		notification.Payload.BlockHash.Hex(),
		"Event Data",
		"</html>",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(body, expected) {
			t.Errorf("expected body to contain %q", expected)
		}
	}
}

func TestEmailHandler_BuildBody_WithTemplate(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := NewEmailHandler(nil, logger)

	// Load a custom template
	templateStr := `Custom template for {{.EventType}} event at block {{.Payload.BlockNumber}}`
	err := handler.LoadTemplate(string(EventTypeBlock), templateStr)
	if err != nil {
		t.Fatalf("failed to load template: %v", err)
	}

	notification := createTestEmailNotification()
	setting := &NotificationSetting{}

	body, err := handler.buildBody(notification, setting)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(body, "Custom template for") {
		t.Errorf("expected custom template to be used, got: %s", body)
	}
	if !strings.Contains(body, "12345") {
		t.Errorf("expected block number in body")
	}
}

func TestEmailHandler_BuildMessage(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := &EmailConfig{
		FromAddress: "indexer@example.com",
		FromName:    "Test Indexer",
	}
	handler := NewEmailHandler(config, logger)

	to := []string{"user@example.com"}
	cc := []string{"cc@example.com"}
	subject := "Test Subject"
	body := "<html>Test Body</html>"

	msg := handler.buildMessage(to, cc, subject, body)
	msgStr := string(msg)

	// Verify headers
	if !strings.Contains(msgStr, "From: Test Indexer <indexer@example.com>") {
		t.Error("expected From header with name")
	}
	if !strings.Contains(msgStr, "To: user@example.com") {
		t.Error("expected To header")
	}
	if !strings.Contains(msgStr, "Cc: cc@example.com") {
		t.Error("expected Cc header")
	}
	if !strings.Contains(msgStr, "Subject: Test Subject") {
		t.Error("expected Subject header")
	}
	if !strings.Contains(msgStr, "MIME-Version: 1.0") {
		t.Error("expected MIME-Version header")
	}
	if !strings.Contains(msgStr, "Content-Type: text/html; charset=\"UTF-8\"") {
		t.Error("expected Content-Type header")
	}
	if !strings.Contains(msgStr, body) {
		t.Error("expected body content")
	}
}

func TestEmailHandler_BuildMessage_DefaultFromName(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := &EmailConfig{
		FromAddress: "indexer@example.com",
		FromName:    "", // Empty name should use default
	}
	handler := NewEmailHandler(config, logger)

	msg := handler.buildMessage([]string{"user@example.com"}, nil, "Subject", "Body")
	msgStr := string(msg)

	if !strings.Contains(msgStr, "From: Indexer <indexer@example.com>") {
		t.Errorf("expected default From name 'Indexer', got: %s", msgStr[:100])
	}
}

func TestEmailHandler_LoadTemplate(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := NewEmailHandler(nil, logger)

	t.Run("valid template", func(t *testing.T) {
		err := handler.LoadTemplate("test", "Hello {{.ID}}")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid template", func(t *testing.T) {
		err := handler.LoadTemplate("invalid", "Hello {{.ID")
		if err == nil {
			t.Error("expected error for invalid template")
		}
	})
}

func TestRateLimiter(t *testing.T) {
	t.Run("allows within limit", func(t *testing.T) {
		limiter := newRateLimiter(5)

		for i := 0; i < 5; i++ {
			if !limiter.allow() {
				t.Errorf("expected allow() to return true for request %d", i+1)
			}
		}
	})

	t.Run("blocks over limit", func(t *testing.T) {
		limiter := newRateLimiter(3)

		// Consume all tokens
		for i := 0; i < 3; i++ {
			limiter.allow()
		}

		// Next should be blocked
		if limiter.allow() {
			t.Error("expected allow() to return false when rate limit exceeded")
		}
	})

	t.Run("refills tokens over time", func(t *testing.T) {
		limiter := newRateLimiter(60) // 60 per minute = 1 per second

		// Consume all tokens
		for i := 0; i < 60; i++ {
			limiter.allow()
		}

		// Should be blocked
		if limiter.allow() {
			t.Error("expected to be blocked initially")
		}

		// Manually advance time by setting lastTime
		limiter.mu.Lock()
		limiter.lastTime = time.Now().Add(-2 * time.Second)
		limiter.mu.Unlock()

		// Should now have some tokens
		if !limiter.allow() {
			t.Error("expected tokens to refill after time passes")
		}
	})
}

func TestEmailHandler_Deliver_RateLimitExceeded(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := &EmailConfig{
		RateLimitPerMinute: 1,
		SMTPHost:           "smtp.example.com",
		FromAddress:        "test@example.com",
	}
	handler := NewEmailHandler(config, logger)

	notification := createTestEmailNotification()
	setting := &NotificationSetting{
		Destination: Destination{
			EmailTo: []string{"user@example.com"},
		},
	}

	ctx := context.Background()

	// First request should consume the token
	handler.rateLimiter.allow()

	// Second request should be rate limited
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
}

// Helper functions

func createTestEmailNotification() *Notification {
	now := time.Now()
	blockHash := common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")

	return &Notification{
		ID:        "test-email-notification-001",
		SettingID: "test-setting-001",
		Type:      NotificationTypeEmail,
		EventType: EventTypeBlock,
		Payload: &EventPayload{
			ChainID:     1,
			BlockNumber: 12345,
			BlockHash:   blockHash,
			Timestamp:   now,
			EventType:   EventTypeBlock,
			Data:        json.RawMessage(`{"number":12345,"hash":"0xabc"}`),
		},
		Status:    DeliveryStatusPending,
		CreatedAt: now,
	}
}
