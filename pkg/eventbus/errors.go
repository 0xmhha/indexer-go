package eventbus

import "errors"

// Common errors for event bus operations
var (
	// ErrPublishFailed indicates that publishing an event failed
	ErrPublishFailed = errors.New("failed to publish event: channel full or bus stopped")

	// ErrNotConnected indicates the distributed event bus is not connected
	ErrNotConnected = errors.New("event bus is not connected")

	// ErrAlreadyConnected indicates the event bus is already connected
	ErrAlreadyConnected = errors.New("event bus is already connected")

	// ErrConnectionFailed indicates a connection failure to the distributed backend
	ErrConnectionFailed = errors.New("failed to connect to event bus backend")

	// ErrSerializationFailed indicates event serialization failure
	ErrSerializationFailed = errors.New("failed to serialize event")

	// ErrDeserializationFailed indicates event deserialization failure
	ErrDeserializationFailed = errors.New("failed to deserialize event")

	// ErrInvalidEventType indicates an unknown or invalid event type
	ErrInvalidEventType = errors.New("invalid event type")

	// ErrSubscriptionNotFound indicates the subscription was not found
	ErrSubscriptionNotFound = errors.New("subscription not found")

	// ErrInvalidConfiguration indicates invalid event bus configuration
	ErrInvalidConfiguration = errors.New("invalid event bus configuration")

	// ErrTimeout indicates an operation timed out
	ErrTimeout = errors.New("operation timed out")

	// ErrShutdown indicates the event bus is shutting down
	ErrShutdown = errors.New("event bus is shutting down")

	// ErrChannelClosed indicates the channel was closed unexpectedly
	ErrChannelClosed = errors.New("channel was closed")
)
