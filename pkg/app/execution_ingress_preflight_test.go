package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/stretchr/testify/require"
)

const appIngressModule = "test.app-native"

var ingressPreparations atomic.Int32

type preflightIngress struct{}

func (preflightIngress) Check(context.Context) error { return nil }
func (preflightIngress) Run(context.Context) error   { return nil }
func (preflightIngress) Stop()                       {}
func init() {
	ingress.Register(appIngressModule, ingress.Module{Profile: "evm", Validate: func(ingress.Config) error { return nil }, Prepare: func(context.Context, ingress.Config, ingress.Dependencies) (ingress.Program, error) {
		ingressPreparations.Add(1)
		return preflightIngress{}, nil
	}})
}
func ingressAppConfig() *config.Config {
	c := executionAppConfig()
	c.Source.Execution.Socket = "/runtime/ipc/feed.sock"
	c.Indexer.StartHeight = 1
	c.Source.Execution.Ingress = &ingress.Config{Module: appIngressModule, Window: "native-raw", Native: ingress.NativeConfig{Executable: "/app/import", DataDir: "/runtime/native/node"}}
	c.WindowStorage = window.Layout{Volumes: map[string]window.VolumeSpec{"volume": {Root: "/runtime/data", ID: "fixture", AllowDirectory: true, MaxBytes: 64 << 20}}, Windows: map[string]window.WindowConfig{"native-raw": {Volume: "volume", Path: "/runtime/data/raw", MaxBytes: 32 << 20, SegmentBytes: 1 << 20, MaxPayloadBytes: 64 << 10, MaxSegmentRecords: 32, ReadBudgetBytes: 1 << 20, Retention: 24 * time.Hour}}}
	return c
}
func TestExecutionIngressPreflightAcceptsOnlyExclusivePreparedLayoutWithoutIO(t *testing.T) {
	c := ingressAppConfig()
	before := ingressPreparations.Load()
	require.NoError(t, preflightExecution(c, StartOptions{}))
	require.NoError(t, preflightFeeds(c, false), "native ingress must claim its window without enabling capture source.feed")
	require.Equal(t, before, ingressPreparations.Load(), "preflight must not prepare native or open transport")
	require.Nil(t, c.Source.Feed)
}
func TestExecutionIngressPreflightRejectsOverlapAndKeepsExistingLimits(t *testing.T) {
	for _, change := range []func(*config.Config){
		func(c *config.Config) { c.Source.Execution.Ingress.Module = "missing" },
		func(c *config.Config) { c.Source.Execution.Ingress.Native.DataDir = "/runtime/data/raw/native" },
		func(c *config.Config) { c.Source.Execution.Ingress.Native.DataDir = "/runtime/ipc/native" },
		func(c *config.Config) { c.Source.Execution.Socket = "/feed.sock" },
		func(c *config.Config) { c.WindowStorage.Windows = nil },
		func(c *config.Config) { c.Indexer.StartHeight = 2 },
		func(c *config.Config) { c.Database.Postgres.MaxConns = 8 },
		func(c *config.Config) { c.RPC.RateLimit = 6 },
	} {
		c := ingressAppConfig()
		change(c)
		require.Error(t, preflightFeeds(c, false))
	}
	require.Error(t, preflightFeeds(ingressAppConfig(), true))
}
