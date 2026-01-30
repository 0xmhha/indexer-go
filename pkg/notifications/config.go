package notifications

import "time"

// Config holds the notification service configuration.
type Config struct {
	// Enabled determines if the notification service is active.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Webhook configuration
	Webhook WebhookConfig `yaml:"webhook" json:"webhook"`

	// Email configuration
	Email EmailConfig `yaml:"email" json:"email"`

	// Slack configuration
	Slack SlackConfig `yaml:"slack" json:"slack"`

	// Retry configuration
	Retry RetryConfig `yaml:"retry" json:"retry"`

	// Queue configuration
	Queue QueueConfig `yaml:"queue" json:"queue"`

	// Storage configuration
	Storage StorageConfig `yaml:"storage" json:"storage"`
}

// WebhookConfig holds webhook-specific configuration.
type WebhookConfig struct {
	// Enabled determines if webhook notifications are available.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Timeout for webhook HTTP requests.
	Timeout time.Duration `yaml:"timeout" json:"timeout"`

	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int `yaml:"max_retries" json:"max_retries"`

	// MaxConcurrent is the maximum concurrent webhook deliveries.
	MaxConcurrent int `yaml:"max_concurrent" json:"max_concurrent"`

	// AllowedHosts restricts webhook URLs to specific hosts (empty = allow all).
	AllowedHosts []string `yaml:"allowed_hosts" json:"allowed_hosts"`

	// SignatureHeader is the header name for HMAC signature.
	SignatureHeader string `yaml:"signature_header" json:"signature_header"`
}

// EmailConfig holds email-specific configuration.
type EmailConfig struct {
	// Enabled determines if email notifications are available.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// SMTPHost is the SMTP server hostname.
	SMTPHost string `yaml:"smtp_host" json:"smtp_host"`

	// SMTPPort is the SMTP server port.
	SMTPPort int `yaml:"smtp_port" json:"smtp_port"`

	// SMTPUsername for authentication.
	SMTPUsername string `yaml:"smtp_username" json:"smtp_username"`

	// SMTPPassword for authentication.
	SMTPPassword string `yaml:"smtp_password" json:"smtp_password"`

	// FromAddress is the sender email address.
	FromAddress string `yaml:"from_address" json:"from_address"`

	// FromName is the sender display name.
	FromName string `yaml:"from_name" json:"from_name"`

	// UseTLS enables TLS for SMTP connection.
	UseTLS bool `yaml:"use_tls" json:"use_tls"`

	// MaxRecipients per email.
	MaxRecipients int `yaml:"max_recipients" json:"max_recipients"`

	// RateLimitPerMinute limits emails per minute.
	RateLimitPerMinute int `yaml:"rate_limit_per_minute" json:"rate_limit_per_minute"`

	// TemplateDir is the directory containing email templates.
	TemplateDir string `yaml:"template_dir" json:"template_dir"`
}

// SlackConfig holds Slack-specific configuration.
type SlackConfig struct {
	// Enabled determines if Slack notifications are available.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Timeout for Slack API requests.
	Timeout time.Duration `yaml:"timeout" json:"timeout"`

	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int `yaml:"max_retries" json:"max_retries"`

	// DefaultUsername is the default bot username.
	DefaultUsername string `yaml:"default_username" json:"default_username"`

	// DefaultIconEmoji is the default bot icon.
	DefaultIconEmoji string `yaml:"default_icon_emoji" json:"default_icon_emoji"`

	// RateLimitPerMinute limits Slack messages per minute.
	RateLimitPerMinute int `yaml:"rate_limit_per_minute" json:"rate_limit_per_minute"`
}

// RetryConfig holds retry behavior configuration.
type RetryConfig struct {
	// InitialDelay is the initial delay before first retry.
	InitialDelay time.Duration `yaml:"initial_delay" json:"initial_delay"`

	// MaxDelay is the maximum delay between retries.
	MaxDelay time.Duration `yaml:"max_delay" json:"max_delay"`

	// Multiplier for exponential backoff.
	Multiplier float64 `yaml:"multiplier" json:"multiplier"`

	// MaxAttempts is the maximum total attempts (including initial).
	MaxAttempts int `yaml:"max_attempts" json:"max_attempts"`
}

// QueueConfig holds notification queue configuration.
type QueueConfig struct {
	// BufferSize is the size of the notification queue buffer.
	BufferSize int `yaml:"buffer_size" json:"buffer_size"`

	// Workers is the number of concurrent delivery workers.
	Workers int `yaml:"workers" json:"workers"`

	// BatchSize is the maximum batch size for processing.
	BatchSize int `yaml:"batch_size" json:"batch_size"`

	// FlushInterval is how often to flush pending notifications.
	FlushInterval time.Duration `yaml:"flush_interval" json:"flush_interval"`
}

// StorageConfig holds notification storage configuration.
type StorageConfig struct {
	// HistoryRetention is how long to keep delivery history.
	HistoryRetention time.Duration `yaml:"history_retention" json:"history_retention"`

	// MaxSettingsPerUser limits notification settings per user.
	MaxSettingsPerUser int `yaml:"max_settings_per_user" json:"max_settings_per_user"`

	// MaxPendingNotifications limits pending notifications.
	MaxPendingNotifications int `yaml:"max_pending_notifications" json:"max_pending_notifications"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Enabled: false,
		Webhook: WebhookConfig{
			Enabled:         true,
			Timeout:         10 * time.Second,
			MaxRetries:      3,
			MaxConcurrent:   10,
			AllowedHosts:    []string{},
			SignatureHeader: "X-Signature-256",
		},
		Email: EmailConfig{
			Enabled:            false,
			SMTPPort:           587,
			UseTLS:             true,
			MaxRecipients:      10,
			RateLimitPerMinute: 60,
		},
		Slack: SlackConfig{
			Enabled:            true,
			Timeout:            10 * time.Second,
			MaxRetries:         3,
			DefaultUsername:    "Indexer Bot",
			DefaultIconEmoji:   ":robot_face:",
			RateLimitPerMinute: 30,
		},
		Retry: RetryConfig{
			InitialDelay: 1 * time.Second,
			MaxDelay:     5 * time.Minute,
			Multiplier:   2.0,
			MaxAttempts:  5,
		},
		Queue: QueueConfig{
			BufferSize:    1000,
			Workers:       5,
			BatchSize:     50,
			FlushInterval: 1 * time.Second,
		},
		Storage: StorageConfig{
			HistoryRetention:        7 * 24 * time.Hour,
			MaxSettingsPerUser:      100,
			MaxPendingNotifications: 10000,
		},
	}
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.Webhook.Enabled {
		if c.Webhook.Timeout <= 0 {
			c.Webhook.Timeout = 10 * time.Second
		}
		if c.Webhook.MaxRetries < 0 {
			c.Webhook.MaxRetries = 3
		}
		if c.Webhook.MaxConcurrent <= 0 {
			c.Webhook.MaxConcurrent = 10
		}
	}

	if c.Email.Enabled {
		if c.Email.SMTPHost == "" {
			return &ConfigError{Field: "email.smtp_host", Message: "SMTP host is required when email is enabled"}
		}
		if c.Email.FromAddress == "" {
			return &ConfigError{Field: "email.from_address", Message: "from address is required when email is enabled"}
		}
	}

	if c.Retry.MaxAttempts <= 0 {
		c.Retry.MaxAttempts = 5
	}
	if c.Retry.Multiplier <= 0 {
		c.Retry.Multiplier = 2.0
	}

	if c.Queue.BufferSize <= 0 {
		c.Queue.BufferSize = 1000
	}
	if c.Queue.Workers <= 0 {
		c.Queue.Workers = 5
	}

	return nil
}

// ConfigError represents a configuration validation error.
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return "notification config error: " + e.Field + ": " + e.Message
}
