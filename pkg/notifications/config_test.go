package notifications

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Fatal("expected non-nil config")
	}

	// Check defaults
	if config.Enabled {
		t.Error("expected Enabled to be false by default")
	}

	// Webhook defaults
	if !config.Webhook.Enabled {
		t.Error("expected Webhook.Enabled to be true by default")
	}
	if config.Webhook.Timeout != 10*time.Second {
		t.Errorf("expected Webhook.Timeout 10s, got %v", config.Webhook.Timeout)
	}
	if config.Webhook.MaxRetries != 3 {
		t.Errorf("expected Webhook.MaxRetries 3, got %d", config.Webhook.MaxRetries)
	}
	if config.Webhook.MaxConcurrent != 10 {
		t.Errorf("expected Webhook.MaxConcurrent 10, got %d", config.Webhook.MaxConcurrent)
	}
	if config.Webhook.SignatureHeader != "X-Signature-256" {
		t.Errorf("expected Webhook.SignatureHeader X-Signature-256, got %s", config.Webhook.SignatureHeader)
	}

	// Email defaults
	if config.Email.Enabled {
		t.Error("expected Email.Enabled to be false by default")
	}
	if config.Email.SMTPPort != 587 {
		t.Errorf("expected Email.SMTPPort 587, got %d", config.Email.SMTPPort)
	}
	if !config.Email.UseTLS {
		t.Error("expected Email.UseTLS to be true by default")
	}
	if config.Email.MaxRecipients != 10 {
		t.Errorf("expected Email.MaxRecipients 10, got %d", config.Email.MaxRecipients)
	}
	if config.Email.RateLimitPerMinute != 60 {
		t.Errorf("expected Email.RateLimitPerMinute 60, got %d", config.Email.RateLimitPerMinute)
	}

	// Slack defaults
	if !config.Slack.Enabled {
		t.Error("expected Slack.Enabled to be true by default")
	}
	if config.Slack.Timeout != 10*time.Second {
		t.Errorf("expected Slack.Timeout 10s, got %v", config.Slack.Timeout)
	}
	if config.Slack.DefaultUsername != "Indexer Bot" {
		t.Errorf("expected Slack.DefaultUsername 'Indexer Bot', got %s", config.Slack.DefaultUsername)
	}
	if config.Slack.DefaultIconEmoji != ":robot_face:" {
		t.Errorf("expected Slack.DefaultIconEmoji ':robot_face:', got %s", config.Slack.DefaultIconEmoji)
	}
	if config.Slack.RateLimitPerMinute != 30 {
		t.Errorf("expected Slack.RateLimitPerMinute 30, got %d", config.Slack.RateLimitPerMinute)
	}

	// Retry defaults
	if config.Retry.InitialDelay != 1*time.Second {
		t.Errorf("expected Retry.InitialDelay 1s, got %v", config.Retry.InitialDelay)
	}
	if config.Retry.MaxDelay != 5*time.Minute {
		t.Errorf("expected Retry.MaxDelay 5m, got %v", config.Retry.MaxDelay)
	}
	if config.Retry.Multiplier != 2.0 {
		t.Errorf("expected Retry.Multiplier 2.0, got %f", config.Retry.Multiplier)
	}
	if config.Retry.MaxAttempts != 5 {
		t.Errorf("expected Retry.MaxAttempts 5, got %d", config.Retry.MaxAttempts)
	}

	// Queue defaults
	if config.Queue.BufferSize != 1000 {
		t.Errorf("expected Queue.BufferSize 1000, got %d", config.Queue.BufferSize)
	}
	if config.Queue.Workers != 5 {
		t.Errorf("expected Queue.Workers 5, got %d", config.Queue.Workers)
	}
	if config.Queue.BatchSize != 50 {
		t.Errorf("expected Queue.BatchSize 50, got %d", config.Queue.BatchSize)
	}
	if config.Queue.FlushInterval != 1*time.Second {
		t.Errorf("expected Queue.FlushInterval 1s, got %v", config.Queue.FlushInterval)
	}

	// Storage defaults
	if config.Storage.HistoryRetention != 7*24*time.Hour {
		t.Errorf("expected Storage.HistoryRetention 168h, got %v", config.Storage.HistoryRetention)
	}
	if config.Storage.MaxSettingsPerUser != 100 {
		t.Errorf("expected Storage.MaxSettingsPerUser 100, got %d", config.Storage.MaxSettingsPerUser)
	}
	if config.Storage.MaxPendingNotifications != 10000 {
		t.Errorf("expected Storage.MaxPendingNotifications 10000, got %d", config.Storage.MaxPendingNotifications)
	}
}

func TestConfig_Validate(t *testing.T) {
	t.Run("disabled config passes", func(t *testing.T) {
		config := &Config{Enabled: false}
		err := config.Validate()
		if err != nil {
			t.Errorf("expected no error for disabled config, got %v", err)
		}
	})

	t.Run("enabled with defaults", func(t *testing.T) {
		config := DefaultConfig()
		config.Enabled = true
		err := config.Validate()
		if err != nil {
			t.Errorf("expected no error for default config, got %v", err)
		}
	})

	t.Run("enabled email without SMTP host", func(t *testing.T) {
		config := &Config{
			Enabled: true,
			Email: EmailConfig{
				Enabled:     true,
				SMTPHost:    "",
				FromAddress: "test@example.com",
			},
		}
		err := config.Validate()
		if err == nil {
			t.Error("expected error for email without SMTP host")
		}
		if configErr, ok := err.(*ConfigError); ok {
			if configErr.Field != "email.smtp_host" {
				t.Errorf("expected field 'email.smtp_host', got %s", configErr.Field)
			}
		}
	})

	t.Run("enabled email without from address", func(t *testing.T) {
		config := &Config{
			Enabled: true,
			Email: EmailConfig{
				Enabled:     true,
				SMTPHost:    "smtp.example.com",
				FromAddress: "",
			},
		}
		err := config.Validate()
		if err == nil {
			t.Error("expected error for email without from address")
		}
		if configErr, ok := err.(*ConfigError); ok {
			if configErr.Field != "email.from_address" {
				t.Errorf("expected field 'email.from_address', got %s", configErr.Field)
			}
		}
	})

	t.Run("auto-corrects invalid values", func(t *testing.T) {
		config := &Config{
			Enabled: true,
			Webhook: WebhookConfig{
				Enabled:       true,
				Timeout:       0, // Invalid - should be corrected
				MaxRetries:    -1,
				MaxConcurrent: 0,
			},
			Retry: RetryConfig{
				MaxAttempts: 0,
				Multiplier:  0,
			},
			Queue: QueueConfig{
				BufferSize: 0,
				Workers:    0,
			},
		}

		err := config.Validate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check auto-corrected values
		if config.Webhook.Timeout != 10*time.Second {
			t.Errorf("expected auto-corrected Timeout 10s, got %v", config.Webhook.Timeout)
		}
		if config.Webhook.MaxRetries != 3 {
			t.Errorf("expected auto-corrected MaxRetries 3, got %d", config.Webhook.MaxRetries)
		}
		if config.Webhook.MaxConcurrent != 10 {
			t.Errorf("expected auto-corrected MaxConcurrent 10, got %d", config.Webhook.MaxConcurrent)
		}
		if config.Retry.MaxAttempts != 5 {
			t.Errorf("expected auto-corrected MaxAttempts 5, got %d", config.Retry.MaxAttempts)
		}
		if config.Retry.Multiplier != 2.0 {
			t.Errorf("expected auto-corrected Multiplier 2.0, got %f", config.Retry.Multiplier)
		}
		if config.Queue.BufferSize != 1000 {
			t.Errorf("expected auto-corrected BufferSize 1000, got %d", config.Queue.BufferSize)
		}
		if config.Queue.Workers != 5 {
			t.Errorf("expected auto-corrected Workers 5, got %d", config.Queue.Workers)
		}
	})
}

func TestConfigError(t *testing.T) {
	err := &ConfigError{
		Field:   "test.field",
		Message: "test message",
	}

	expected := "notification config error: test.field: test message"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}
