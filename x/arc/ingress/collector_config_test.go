package arcingress

import (
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNativeCollectorConfigRequiresExplicitSeparateBoundedInputs(t *testing.T) {
	valid := ingress.Config{Native: ingress.NativeConfig{Executable: "/app/arc-native-import", DataDir: "/native/node"}, Collector: ingress.CollectorConfig{Executable: "/app/indexer-arc-executor", SpoolDir: "/source/spool", MaxBytes: 256 << 20, MaxRecords: 4096}}
	require.NoError(t, collectorConfig(valid))
	for _, mutate := range []func(*ingress.Config){func(c *ingress.Config) { c.Collector.Executable = "" }, func(c *ingress.Config) { c.Collector.Executable = "relative" }, func(c *ingress.Config) { c.Collector.Executable = c.Native.Executable }, func(c *ingress.Config) { c.Collector.SpoolDir = "" }, func(c *ingress.Config) { c.Collector.SpoolDir = "/native/node/spool" }, func(c *ingress.Config) { c.Collector.Executable = "/source/spool/adapter" }, func(c *ingress.Config) { c.Collector.MaxRecords = -1 }, func(c *ingress.Config) { c.Collector.MaxRecords = 1<<20 + 1 }, func(c *ingress.Config) { c.Collector.MaxBytes = 64 << 10 }, func(c *ingress.Config) { c.Collector.MaxBytes = 64<<30 + 1 }} {
		c := valid
		mutate(&c)
		require.Error(t, collectorConfig(c), "managed collector needs explicit inputs and bounded independent storage")
	}
}
