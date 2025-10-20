package websocket

import (
	"encoding/json"
)

// SubscriptionType represents the type of subscription
type SubscriptionType string

const (
	// SubscribeNewBlock subscribes to new block events
	SubscribeNewBlock SubscriptionType = "newBlock"

	// SubscribeNewTransaction subscribes to new transaction events
	SubscribeNewTransaction SubscriptionType = "newTransaction"
)

// Message represents a WebSocket message
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// SubscribeRequest represents a subscription request
type SubscribeRequest struct {
	Type SubscriptionType `json:"type"`
}

// UnsubscribeRequest represents an unsubscribe request
type UnsubscribeRequest struct {
	Type SubscriptionType `json:"type"`
}

// Event represents a subscription event
type Event struct {
	Type SubscriptionType `json:"type"`
	Data interface{}      `json:"data"`
}

// ErrorMessage represents an error message
type ErrorMessage struct {
	Error string `json:"error"`
}

// SuccessMessage represents a success message
type SuccessMessage struct {
	Message string `json:"message"`
}
