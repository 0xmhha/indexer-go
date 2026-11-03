package observeconfig

import (
	"github.com/0xmhha/indexer-go/internal/nativeobserve"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func valid() Config {
	return (Config{Enabled: true, CheckpointDir: "/private/observer", Database: Resource{ID: strings.Repeat("11", 32), Series: strings.Repeat("22", 32), BudgetBytes: 1000}, WAL: Resource{ID: strings.Repeat("33", 32), Series: strings.Repeat("44", 32), BudgetBytes: 1000}, Warn: 8000, Stop: 9000, Clear: 7000}).WithDefaults()
}
func TestObservationPolicyBoundsAndResourceIdentity(t *testing.T) {
	c := valid()
	require.NoError(t, c.Validate())
	require.Empty(t, (Config{}).WithDefaults().Sink)
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"relative", func(c *Config) { c.CheckpointDir = "HOLD" }}, {"unclean", func(c *Config) { c.CheckpointDir = "/private/../tmp" }},
		{"network_sink", func(c *Config) { c.Sink = "webhook" }}, {"zero_budget", func(c *Config) { c.Database.BudgetBytes = 0 }},
		{"same_resource", func(c *Config) { c.WAL.ID = c.Database.ID }}, {"zero_id", func(c *Config) { c.Database.ID = strings.Repeat("00", 32) }},
		{"bad_series", func(c *Config) { c.WAL.Series = "https://secret" }}, {"bad_operator", func(c *Config) { c.OperatorID = "operator" }},
		{"interval", func(c *Config) { c.Interval = time.Nanosecond }}, {"age", func(c *Config) { c.MaxAge = time.Second }},
		{"inbox", func(c *Config) { c.InboxSize = 1025 }}, {"timeout", func(c *Config) { c.DeliveryTimeout = 6 * time.Second }},
		{"threshold_order", func(c *Config) { c.Clear = c.Warn }}, {"threshold_range", func(c *Config) { c.Stop = 10001 }},
		{"count", func(c *Config) { c.ClearCount = 1 }}, {"negative_dwell", func(c *Config) { c.WarnDwell = -time.Second }},
	} {
		t.Run(tc.name, func(t *testing.T) { c := valid(); tc.change(&c); require.ErrorIs(t, c.Validate(), ErrConfig) })
	}
	source := nativeobserve.Source{Token: nativeobserve.SourceToken{1}, Incarnation: 1, Run: nativeobserve.RunID{2}}
	bindings := c.Bindings(source)
	catalog, a := nativeobserve.NewCatalog(bindings)
	require.True(t, a.Usable())
	require.NotNil(t, catalog)
	require.Equal(t, nativeobserve.Database, bindings[0].Scope.Kind)
	require.Equal(t, nativeobserve.Cluster, bindings[1].Scope.Kind)
	require.Equal(t, nativeobserve.Chain{}, bindings[0].Chain)
	copy := c
	copy.Warn = 8100
	require.NotEqual(t, c.Rules()[0].Policy, copy.Rules()[0].Policy)
}
