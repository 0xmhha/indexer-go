package app

import (
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/stretchr/testify/require"
)

func executionAppConfig() *config.Config {
	c := config.NewConfig()
	c.RPC.Endpoint = "http://127.0.0.1:8545"
	c.RPC.RateLimit = 5
	c.Database.Driver = config.DriverPostgres
	c.Database.Postgres.DSN = "postgres://localhost/indexer"
	c.Database.Postgres.MaxConns = 4
	c.Indexer.StatePrefetch = true
	c.Indexer.Finality = "head"
	c.Indexer.Confirmations = 0
	c.Source.Execution = &config.ExecutionSourceConfig{Socket: "/tmp/indexer-execution/feed.sock", AnchorHash: "0x" + strings.Repeat("11", 32), AnchorRoot: "0x" + strings.Repeat("22", 32)}
	return c
}
func TestExecutionPreflightRejectsDestructiveLegacyPathsBeforeIO(t *testing.T) {
	c := executionAppConfig()
	require.NoError(t, preflightExecution(c, StartOptions{}))
	for _, opts := range []StartOptions{{ClearData: true}, {Reindex: true}, {GapRecovery: true}} {
		require.ErrorContains(t, preflightExecution(c, opts), "execution")
	}
	c.Source.Execution.Socket = "relative"
	require.ErrorContains(t, preflightExecution(c, StartOptions{}), "execution")
	c.Source.Execution = nil
	require.NoError(t, preflightExecution(c, StartOptions{ClearData: true}))
}
