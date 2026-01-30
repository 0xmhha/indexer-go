// Package notifications provides notification delivery for blockchain events.
package notifications

import (
	"encoding/json"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// NotificationType represents the type of notification channel.
type NotificationType string

const (
	NotificationTypeWebhook NotificationType = "webhook"
	NotificationTypeEmail   NotificationType = "email"
	NotificationTypeSlack   NotificationType = "slack"
)

// EventType represents blockchain event types that can trigger notifications.
type EventType string

const (
	EventTypeBlock            EventType = "block"
	EventTypeTransaction      EventType = "transaction"
	EventTypeLog              EventType = "log"
	EventTypeContractCreation EventType = "contract_creation"
	EventTypeTokenTransfer    EventType = "token_transfer"
)

// DeliveryStatus represents the status of a notification delivery.
type DeliveryStatus string

const (
	DeliveryStatusPending   DeliveryStatus = "pending"
	DeliveryStatusSent      DeliveryStatus = "sent"
	DeliveryStatusFailed    DeliveryStatus = "failed"
	DeliveryStatusRetrying  DeliveryStatus = "retrying"
	DeliveryStatusCancelled DeliveryStatus = "cancelled"
)

// NotificationSetting represents a user's notification configuration.
type NotificationSetting struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Type        NotificationType `json:"type"`
	Enabled     bool             `json:"enabled"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	EventTypes  []EventType      `json:"event_types"`
	Filter      *NotifyFilter    `json:"filter,omitempty"`
	Destination Destination      `json:"destination"`
}

// NotifyFilter defines conditions for triggering notifications.
type NotifyFilter struct {
	Addresses     []common.Address `json:"addresses,omitempty"`
	Topics        [][]common.Hash  `json:"topics,omitempty"`
	ContractTypes []string         `json:"contract_types,omitempty"`
	MinValue      *string          `json:"min_value,omitempty"`
}

// Destination contains channel-specific delivery settings.
type Destination struct {
	// Webhook settings
	WebhookURL     string            `json:"webhook_url,omitempty"`
	WebhookHeaders map[string]string `json:"webhook_headers,omitempty"`
	WebhookSecret  string            `json:"webhook_secret,omitempty"`

	// Email settings
	EmailTo      []string `json:"email_to,omitempty"`
	EmailCC      []string `json:"email_cc,omitempty"`
	EmailSubject string   `json:"email_subject,omitempty"`

	// Slack settings
	SlackWebhookURL string `json:"slack_webhook_url,omitempty"`
	SlackChannel    string `json:"slack_channel,omitempty"`
	SlackUsername   string `json:"slack_username,omitempty"`
}

// Notification represents a notification to be delivered.
type Notification struct {
	ID         string           `json:"id"`
	SettingID  string           `json:"setting_id"`
	Type       NotificationType `json:"type"`
	EventType  EventType        `json:"event_type"`
	Payload    *EventPayload    `json:"payload"`
	Status     DeliveryStatus   `json:"status"`
	RetryCount int              `json:"retry_count"`
	NextRetry  *time.Time       `json:"next_retry,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	SentAt     *time.Time       `json:"sent_at,omitempty"`
	Error      string           `json:"error,omitempty"`
}

// EventPayload contains the blockchain event data.
type EventPayload struct {
	ChainID     uint64          `json:"chain_id"`
	BlockNumber uint64          `json:"block_number"`
	BlockHash   common.Hash     `json:"block_hash"`
	Timestamp   time.Time       `json:"timestamp"`
	EventType   EventType       `json:"event_type"`
	Data        json.RawMessage `json:"data"`
}

// BlockEventData contains block-specific event data.
type BlockEventData struct {
	Number       uint64      `json:"number"`
	Hash         common.Hash `json:"hash"`
	ParentHash   common.Hash `json:"parent_hash"`
	Miner        string      `json:"miner"`
	GasUsed      uint64      `json:"gas_used"`
	GasLimit     uint64      `json:"gas_limit"`
	TxCount      int         `json:"tx_count"`
	BaseFeePerGas *string    `json:"base_fee_per_gas,omitempty"`
}

// TransactionEventData contains transaction-specific event data.
type TransactionEventData struct {
	Hash        common.Hash     `json:"hash"`
	From        common.Address  `json:"from"`
	To          *common.Address `json:"to,omitempty"`
	Value       string          `json:"value"`
	Gas         uint64          `json:"gas"`
	GasPrice    string          `json:"gas_price"`
	Nonce       uint64          `json:"nonce"`
	Input       string          `json:"input"`
	Status      uint64          `json:"status"`
	BlockNumber uint64          `json:"block_number"`
}

// LogEventData contains log-specific event data.
type LogEventData struct {
	Address     common.Address `json:"address"`
	Topics      []common.Hash  `json:"topics"`
	Data        string         `json:"data"`
	BlockNumber uint64         `json:"block_number"`
	TxHash      common.Hash    `json:"tx_hash"`
	TxIndex     uint           `json:"tx_index"`
	LogIndex    uint           `json:"log_index"`
	Removed     bool           `json:"removed"`
}

// DeliveryResult contains the result of a notification delivery attempt.
type DeliveryResult struct {
	Success      bool      `json:"success"`
	StatusCode   int       `json:"status_code,omitempty"`
	ResponseBody string    `json:"response_body,omitempty"`
	Error        string    `json:"error,omitempty"`
	DeliveredAt  time.Time `json:"delivered_at"`
	Duration     int64     `json:"duration_ms"`
}

// DeliveryHistory tracks notification delivery attempts.
type DeliveryHistory struct {
	NotificationID string           `json:"notification_id"`
	SettingID      string           `json:"setting_id"`
	Attempt        int              `json:"attempt"`
	Result         *DeliveryResult  `json:"result"`
	Timestamp      time.Time        `json:"timestamp"`
}

// NotificationStats contains statistics for a notification setting.
type NotificationStats struct {
	SettingID      string    `json:"setting_id"`
	TotalSent      int64     `json:"total_sent"`
	TotalFailed    int64     `json:"total_failed"`
	TotalPending   int64     `json:"total_pending"`
	LastSentAt     *time.Time `json:"last_sent_at,omitempty"`
	LastFailedAt   *time.Time `json:"last_failed_at,omitempty"`
	AvgDeliveryMs  float64   `json:"avg_delivery_ms"`
	SuccessRate    float64   `json:"success_rate"`
}
