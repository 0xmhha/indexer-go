package eventbus

import (
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"
)

// createKafkaSASLMechanism creates the appropriate SASL mechanism from config
func createKafkaSASLMechanism(cfg config.EventBusKafkaConfig) (sasl.Mechanism, error) {
	switch cfg.SASLMechanism {
	case "PLAIN":
		return plain.Mechanism{
			Username: cfg.SASLUsername,
			Password: cfg.SASLPassword,
		}, nil
	case "SCRAM-SHA-256":
		mechanism, err := scram.Mechanism(scram.SHA256, cfg.SASLUsername, cfg.SASLPassword)
		if err != nil {
			return nil, err
		}
		return mechanism, nil
	case "SCRAM-SHA-512":
		mechanism, err := scram.Mechanism(scram.SHA512, cfg.SASLUsername, cfg.SASLPassword)
		if err != nil {
			return nil, err
		}
		return mechanism, nil
	default:
		return nil, fmt.Errorf("unsupported SASL mechanism: %s", cfg.SASLMechanism)
	}
}

// buildKafkaDialer creates a kafka.Dialer configured with SASL/TLS from config
func buildKafkaDialer(cfg config.EventBusKafkaConfig) (*kafka.Dialer, error) {
	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
	}

	if cfg.TLS.Enabled {
		dialer.TLS = &tls.Config{
			InsecureSkipVerify: cfg.TLS.InsecureSkipVerify,
			ServerName:         cfg.TLS.ServerName,
		}
	}

	if cfg.SASLUsername != "" && cfg.SASLPassword != "" {
		mechanism, err := createKafkaSASLMechanism(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create SASL mechanism: %w", err)
		}
		dialer.SASLMechanism = mechanism
	}

	// Wrap with TLS if the dialer has a TLS config
	if dialer.TLS != nil {
		dialer.DualStack = true
	}

	return dialer, nil
}

// buildKafkaTransport creates a kafka.Transport configured with SASL/TLS from config
func buildKafkaTransport(cfg config.EventBusKafkaConfig) (*kafka.Transport, error) {
	var tlsConfig *tls.Config
	if cfg.TLS.Enabled {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: cfg.TLS.InsecureSkipVerify,
			ServerName:         cfg.TLS.ServerName,
		}
	}

	if cfg.SASLUsername != "" && cfg.SASLPassword != "" {
		mechanism, err := createKafkaSASLMechanism(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create SASL mechanism: %w", err)
		}
		return &kafka.Transport{
			SASL: mechanism,
			TLS:  tlsConfig,
		}, nil
	}

	if tlsConfig != nil {
		return &kafka.Transport{
			TLS: tlsConfig,
		}, nil
	}

	return nil, nil
}

// testKafkaBrokerConnectivity tests basic TCP connectivity to at least one broker
func testKafkaBrokerConnectivity(brokers []string, timeout time.Duration) error {
	for _, broker := range brokers {
		conn, err := net.DialTimeout("tcp", broker, timeout)
		if err == nil {
			conn.Close()
			return nil
		}
	}
	return fmt.Errorf("%w: unable to reach any Kafka broker", ErrConnectionFailed)
}
