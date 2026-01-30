package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

func TestNewWebhookHandler(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	t.Run("with nil config", func(t *testing.T) {
		handler := NewWebhookHandler(nil, logger)
		if handler == nil {
			t.Fatal("expected non-nil handler")
		}
		if handler.config == nil {
			t.Error("expected default config")
		}
		if handler.config.Timeout != 10*time.Second {
			t.Errorf("expected default timeout 10s, got %v", handler.config.Timeout)
		}
		if handler.config.SignatureHeader != "X-Signature-256" {
			t.Errorf("expected default signature header, got %s", handler.config.SignatureHeader)
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		config := &WebhookConfig{
			Enabled:         true,
			Timeout:         5 * time.Second,
			MaxRetries:      5,
			SignatureHeader: "X-Custom-Sig",
		}
		handler := NewWebhookHandler(config, logger)
		if handler.config.Timeout != 5*time.Second {
			t.Errorf("expected custom timeout 5s, got %v", handler.config.Timeout)
		}
		if handler.config.SignatureHeader != "X-Custom-Sig" {
			t.Errorf("expected custom signature header, got %s", handler.config.SignatureHeader)
		}
	})
}

func TestWebhookHandler_Type(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := NewWebhookHandler(nil, logger)

	if handler.Type() != NotificationTypeWebhook {
		t.Errorf("expected type webhook, got %v", handler.Type())
	}
}

func TestWebhookHandler_Validate(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	tests := []struct {
		name        string
		config      *WebhookConfig
		setting     *NotificationSetting
		expectError bool
		errorMsg    string
	}{
		{
			name:   "valid URL",
			config: &WebhookConfig{Enabled: true},
			setting: &NotificationSetting{
				Destination: Destination{
					WebhookURL: "https://example.com/webhook",
				},
			},
			expectError: false,
		},
		{
			name:   "valid HTTP URL",
			config: &WebhookConfig{Enabled: true},
			setting: &NotificationSetting{
				Destination: Destination{
					WebhookURL: "http://localhost:8080/webhook",
				},
			},
			expectError: false,
		},
		{
			name:   "empty URL",
			config: &WebhookConfig{Enabled: true},
			setting: &NotificationSetting{
				Destination: Destination{
					WebhookURL: "",
				},
			},
			expectError: true,
			errorMsg:    "webhook URL is required",
		},
		{
			name:   "invalid scheme",
			config: &WebhookConfig{Enabled: true},
			setting: &NotificationSetting{
				Destination: Destination{
					WebhookURL: "ftp://example.com/webhook",
				},
			},
			expectError: true,
			errorMsg:    "must use http or https",
		},
		{
			name: "allowed hosts - valid",
			config: &WebhookConfig{
				Enabled:      true,
				AllowedHosts: []string{"example.com", "trusted.io"},
			},
			setting: &NotificationSetting{
				Destination: Destination{
					WebhookURL: "https://example.com/webhook",
				},
			},
			expectError: false,
		},
		{
			name: "allowed hosts - subdomain valid",
			config: &WebhookConfig{
				Enabled:      true,
				AllowedHosts: []string{"example.com"},
			},
			setting: &NotificationSetting{
				Destination: Destination{
					WebhookURL: "https://api.example.com/webhook",
				},
			},
			expectError: false,
		},
		{
			name: "allowed hosts - blocked",
			config: &WebhookConfig{
				Enabled:      true,
				AllowedHosts: []string{"example.com", "trusted.io"},
			},
			setting: &NotificationSetting{
				Destination: Destination{
					WebhookURL: "https://malicious.com/webhook",
				},
			},
			expectError: true,
			errorMsg:    "not in allowed hosts list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewWebhookHandler(tt.config, logger)
			err := handler.Validate(tt.setting)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errorMsg != "" && !containsString(err.Error(), tt.errorMsg) {
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

func TestWebhookHandler_Deliver(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	t.Run("successful delivery", func(t *testing.T) {
		var receivedBody []byte
		var receivedHeaders http.Header
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeaders = r.Header
			receivedBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		}))
		defer server.Close()

		handler := NewWebhookHandler(&WebhookConfig{
			Enabled: true,
			Timeout: 5 * time.Second,
		}, logger)

		notification := createTestNotification()
		setting := &NotificationSetting{
			Destination: Destination{
				WebhookURL: server.URL,
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

		// Verify headers
		if receivedHeaders.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", receivedHeaders.Get("Content-Type"))
		}
		if receivedHeaders.Get("X-Webhook-ID") != notification.ID {
			t.Errorf("expected X-Webhook-ID %s, got %s", notification.ID, receivedHeaders.Get("X-Webhook-ID"))
		}

		// Verify payload
		var payload WebhookPayload
		if err := json.Unmarshal(receivedBody, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}
		if payload.ID != notification.ID {
			t.Errorf("expected payload ID %s, got %s", notification.ID, payload.ID)
		}
	})

	t.Run("delivery with signature", func(t *testing.T) {
		var receivedSignature string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedSignature = r.Header.Get("X-Signature-256")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		handler := NewWebhookHandler(&WebhookConfig{
			Enabled:         true,
			Timeout:         5 * time.Second,
			SignatureHeader: "X-Signature-256",
		}, logger)

		notification := createTestNotification()
		setting := &NotificationSetting{
			Destination: Destination{
				WebhookURL:    server.URL,
				WebhookSecret: "test-secret-key",
			},
		}

		result, err := handler.Deliver(ctx, notification, setting)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Success {
			t.Errorf("expected success, got failure: %s", result.Error)
		}

		// Verify signature is present and starts with sha256=
		if receivedSignature == "" {
			t.Error("expected signature header to be set")
		}
		if len(receivedSignature) < 10 || receivedSignature[:7] != "sha256=" {
			t.Errorf("expected signature to start with sha256=, got %s", receivedSignature)
		}
	})

	t.Run("delivery with custom headers", func(t *testing.T) {
		var customHeaderValue string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			customHeaderValue = r.Header.Get("X-Custom-Header")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		handler := NewWebhookHandler(nil, logger)
		notification := createTestNotification()
		setting := &NotificationSetting{
			Destination: Destination{
				WebhookURL: server.URL,
				WebhookHeaders: map[string]string{
					"X-Custom-Header": "custom-value",
				},
			},
		}

		result, _ := handler.Deliver(ctx, notification, setting)
		if !result.Success {
			t.Errorf("expected success: %s", result.Error)
		}
		if customHeaderValue != "custom-value" {
			t.Errorf("expected custom header value, got %s", customHeaderValue)
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"internal error"}`))
		}))
		defer server.Close()

		handler := NewWebhookHandler(nil, logger)
		notification := createTestNotification()
		setting := &NotificationSetting{
			Destination: Destination{
				WebhookURL: server.URL,
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
		handler := NewWebhookHandler(&WebhookConfig{
			Enabled: true,
			Timeout: 1 * time.Second,
		}, logger)
		notification := createTestNotification()
		setting := &NotificationSetting{
			Destination: Destination{
				WebhookURL: "http://localhost:59999/nonexistent",
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
}

func TestWebhookHandler_ComputeSignature(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := NewWebhookHandler(nil, logger)

	payload := []byte(`{"test":"data"}`)
	secret := "test-secret"

	sig := handler.computeSignature(payload, secret)

	// Verify signature format (hex encoded)
	if len(sig) != 64 {
		t.Errorf("expected 64 character hex signature, got %d characters", len(sig))
	}

	// Same payload and secret should produce same signature
	sig2 := handler.computeSignature(payload, secret)
	if sig != sig2 {
		t.Error("expected consistent signature for same input")
	}

	// Different secret should produce different signature
	sig3 := handler.computeSignature(payload, "different-secret")
	if sig == sig3 {
		t.Error("expected different signature for different secret")
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	tests := []struct {
		name      string
		payload   []byte
		signature string
		secret    string
		expected  bool
	}{
		{
			name:      "valid signature with prefix",
			payload:   []byte(`{"test":"data"}`),
			signature: "sha256=", // Will be computed
			secret:    "test-secret",
			expected:  true,
		},
		{
			name:      "valid signature without prefix",
			payload:   []byte(`{"test":"data"}`),
			signature: "", // Will be computed
			secret:    "test-secret",
			expected:  true,
		},
		{
			name:      "invalid signature",
			payload:   []byte(`{"test":"data"}`),
			signature: "sha256=0000000000000000000000000000000000000000000000000000000000000000",
			secret:    "test-secret",
			expected:  false,
		},
		{
			name:      "invalid hex",
			payload:   []byte(`{"test":"data"}`),
			signature: "sha256=notvalidhex",
			secret:    "test-secret",
			expected:  false,
		},
	}

	// Pre-compute valid signatures
	logger, _ := zap.NewDevelopment()
	handler := NewWebhookHandler(nil, logger)

	for i, tt := range tests {
		if tt.expected && (tt.signature == "sha256=" || tt.signature == "") {
			sig := handler.computeSignature(tt.payload, tt.secret)
			if tests[i].signature == "sha256=" {
				tests[i].signature = "sha256=" + sig
			} else {
				tests[i].signature = sig
			}
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := VerifyWebhookSignature(tt.payload, tt.signature, tt.secret)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// Helper functions

func createTestNotification() *Notification {
	now := time.Now()
	blockHash := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	return &Notification{
		ID:        "test-notification-001",
		SettingID: "test-setting-001",
		Type:      NotificationTypeWebhook,
		EventType: EventTypeBlock,
		Payload: &EventPayload{
			ChainID:     1,
			BlockNumber: 12345,
			BlockHash:   blockHash,
			Timestamp:   now,
			EventType:   EventTypeBlock,
			Data:        json.RawMessage(`{"number":12345}`),
		},
		Status:    DeliveryStatusPending,
		CreatedAt: now,
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
