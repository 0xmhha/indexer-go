package eventbus

import (
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// createKafkaSASLMechanism Tests
// ============================================================================

func TestCreateKafkaSASLMechanism_PLAIN(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		SASLMechanism: "PLAIN",
		SASLUsername:   "user",
		SASLPassword:   "pass",
	}

	mechanism, err := createKafkaSASLMechanism(cfg)
	require.NoError(t, err)
	assert.NotNil(t, mechanism)
}

func TestCreateKafkaSASLMechanism_SCRAM256(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		SASLMechanism: "SCRAM-SHA-256",
		SASLUsername:   "user",
		SASLPassword:   "pass",
	}

	mechanism, err := createKafkaSASLMechanism(cfg)
	require.NoError(t, err)
	assert.NotNil(t, mechanism)
}

func TestCreateKafkaSASLMechanism_SCRAM512(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		SASLMechanism: "SCRAM-SHA-512",
		SASLUsername:   "user",
		SASLPassword:   "pass",
	}

	mechanism, err := createKafkaSASLMechanism(cfg)
	require.NoError(t, err)
	assert.NotNil(t, mechanism)
}

func TestCreateKafkaSASLMechanism_Unsupported(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		SASLMechanism: "UNKNOWN",
		SASLUsername:   "user",
		SASLPassword:   "pass",
	}

	mechanism, err := createKafkaSASLMechanism(cfg)
	assert.Error(t, err)
	assert.Nil(t, mechanism)
	assert.Contains(t, err.Error(), "unsupported SASL mechanism")
}

// ============================================================================
// buildKafkaDialer Tests
// ============================================================================

func TestBuildKafkaDialer_NoAuth(t *testing.T) {
	cfg := config.EventBusKafkaConfig{}

	dialer, err := buildKafkaDialer(cfg)
	require.NoError(t, err)
	require.NotNil(t, dialer)
	assert.Nil(t, dialer.TLS)
	assert.Nil(t, dialer.SASLMechanism)
}

func TestBuildKafkaDialer_WithTLS(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		TLS: config.TLSConfig{
			Enabled:    true,
			ServerName: "kafka.example.com",
		},
	}

	dialer, err := buildKafkaDialer(cfg)
	require.NoError(t, err)
	require.NotNil(t, dialer)
	assert.NotNil(t, dialer.TLS)
	assert.Equal(t, "kafka.example.com", dialer.TLS.ServerName)
}

func TestBuildKafkaDialer_WithSASL(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		SASLMechanism: "PLAIN",
		SASLUsername:   "user",
		SASLPassword:   "pass",
	}

	dialer, err := buildKafkaDialer(cfg)
	require.NoError(t, err)
	require.NotNil(t, dialer)
	assert.NotNil(t, dialer.SASLMechanism)
}

func TestBuildKafkaDialer_WithSASLAndTLS(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		SASLMechanism: "PLAIN",
		SASLUsername:   "user",
		SASLPassword:   "pass",
		TLS: config.TLSConfig{
			Enabled: true,
		},
	}

	dialer, err := buildKafkaDialer(cfg)
	require.NoError(t, err)
	require.NotNil(t, dialer)
	assert.NotNil(t, dialer.TLS)
	assert.NotNil(t, dialer.SASLMechanism)
}

func TestBuildKafkaDialer_UnsupportedSASL(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		SASLMechanism: "INVALID",
		SASLUsername:   "user",
		SASLPassword:   "pass",
	}

	dialer, err := buildKafkaDialer(cfg)
	assert.Error(t, err)
	assert.Nil(t, dialer)
}

// ============================================================================
// buildKafkaTransport Tests
// ============================================================================

func TestBuildKafkaTransport_NoAuth(t *testing.T) {
	cfg := config.EventBusKafkaConfig{}

	transport, err := buildKafkaTransport(cfg)
	require.NoError(t, err)
	assert.Nil(t, transport) // No TLS, no SASL → nil transport
}

func TestBuildKafkaTransport_WithTLS(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		TLS: config.TLSConfig{
			Enabled:    true,
			ServerName: "kafka.example.com",
		},
	}

	transport, err := buildKafkaTransport(cfg)
	require.NoError(t, err)
	require.NotNil(t, transport)
	assert.NotNil(t, transport.TLS)
}

func TestBuildKafkaTransport_WithSASL(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		SASLMechanism: "PLAIN",
		SASLUsername:   "user",
		SASLPassword:   "pass",
	}

	transport, err := buildKafkaTransport(cfg)
	require.NoError(t, err)
	require.NotNil(t, transport)
	assert.NotNil(t, transport.SASL)
}

func TestBuildKafkaTransport_WithSASLAndTLS(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		SASLMechanism: "PLAIN",
		SASLUsername:   "user",
		SASLPassword:   "pass",
		TLS: config.TLSConfig{
			Enabled: true,
		},
	}

	transport, err := buildKafkaTransport(cfg)
	require.NoError(t, err)
	require.NotNil(t, transport)
	assert.NotNil(t, transport.TLS)
	assert.NotNil(t, transport.SASL)
}

func TestBuildKafkaTransport_UnsupportedSASL(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		SASLMechanism: "INVALID",
		SASLUsername:   "user",
		SASLPassword:   "pass",
	}

	transport, err := buildKafkaTransport(cfg)
	assert.Error(t, err)
	assert.Nil(t, transport)
}

// ============================================================================
// testKafkaBrokerConnectivity Tests
// ============================================================================

func TestTestKafkaBrokerConnectivity_NoReachable(t *testing.T) {
	// Use an unreachable address with very short timeout
	err := testKafkaBrokerConnectivity([]string{"192.0.2.1:9999"}, 100*time.Millisecond)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrConnectionFailed)
}

func TestTestKafkaBrokerConnectivity_EmptyBrokers(t *testing.T) {
	err := testKafkaBrokerConnectivity([]string{}, 100*time.Millisecond)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrConnectionFailed)
}
