package config

import (
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/feed"
	"github.com/stretchr/testify/require"
)

func executionConfigFixture() *Config {
	c := NewConfig()
	c.RPC.Endpoint = "http://127.0.0.1:8545"
	c.RPC.RateLimit = 5
	c.Database.Driver = DriverPostgres
	c.Database.Postgres.DSN = "postgres://localhost/indexer"
	c.Database.Postgres.MaxConns = 4
	c.Indexer.StatePrefetch = true
	c.Indexer.Finality = "head"
	c.Indexer.Confirmations = 0
	c.Source.Execution = &ExecutionSourceConfig{Socket: "/tmp/indexer-execution/feed.sock", AnchorHash: "0x" + strings.Repeat("11", 32), AnchorRoot: "0x" + strings.Repeat("22", 32)}
	return c
}
func TestExecutionSourceRequiresExplicitSafeModeBoundaries(t *testing.T) {
	require.NoError(t, executionConfigFixture().Validate())
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"relative_socket", func(c *Config) { c.Source.Execution.Socket = "relative.sock" }},
		{"pebble", func(c *Config) { c.Database.Driver = DriverPebble }},
		{"default_pool", func(c *Config) { c.Database.Postgres.MaxConns = 0 }},
		{"small_pool", func(c *Config) { c.Database.Postgres.MaxConns = 2 }},
		{"large_pool", func(c *Config) { c.Database.Postgres.MaxConns = 8 }},
		{"unlimited_rpc", func(c *Config) { c.RPC.RateLimit = 0 }},
		{"rpc_changed", func(c *Config) { c.RPC.RateLimit = 6 }},
		{"replay_rpc", func(c *Config) { c.RPC.Endpoint = "replay:///tmp/old" }},
		{"record_rpc", func(c *Config) { c.RPC.RecordDir = "/tmp/record" }},
		{"missing_hash", func(c *Config) { c.Source.Execution.AnchorHash = "" }},
		{"zero_root", func(c *Config) { c.Source.Execution.AnchorRoot = "0x" + strings.Repeat("00", 32) }},
		{"bad_root", func(c *Config) { c.Source.Execution.AnchorRoot = "0x12" }},
		{"queue_count", func(c *Config) { c.Source.Execution.QueueBlocks = 65537 }},
		{"queue_bytes", func(c *Config) { c.Source.Execution.QueueBytes = 2<<30 + 1 }},
		{"negative_queue", func(c *Config) { c.Source.Execution.QueueBytes = -1 }},
		{"timeout", func(c *Config) { c.Source.Execution.Timeout = 2 * time.Hour }},
		{"state_prefetch", func(c *Config) { c.Indexer.StatePrefetch = false }},
		{"api_role", func(c *Config) { c.Node.Role = RoleAPI }},
		{"declared", func(c *Config) { c.Indexer.Mode = ModeDeclared }},
		{"era", func(c *Config) { c.Source.EraDir = "/tmp/era" }},
		{"node_feed", func(c *Config) { c.Source.NodeFeedSocket = "/tmp/old.sock" }},
		{"managed_feed", func(c *Config) { c.Source.Feed = &feed.Config{} }},
		{"multichain", func(c *Config) { c.MultiChain.Enabled = true }},
		{"finality", func(c *Config) { c.Indexer.Finality = "finalized" }},
		{"confirmations", func(c *Config) { c.Indexer.Confirmations = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := executionConfigFixture()
			tc.change(c)
			require.ErrorContains(t, c.validateExecution(), "source.execution")
		})
	}
}
func TestExecutionSourceIsDefaultOffAndDoesNotChangeLegacyLimits(t *testing.T) {
	c := NewConfig()
	require.Nil(t, c.Source.Execution)
	c.RPC.RateLimit = 20
	c.Database.Postgres.MaxConns = 16
	require.NoError(t, c.validateExecution())
}
