package config

import (
	"github.com/0xmhha/indexer-go/internal/observeconfig"
	"github.com/0xmhha/indexer-go/internal/opsguard"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestObservationDisabledConfigIsRecognized(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("rpc:\n  endpoint: http://127.0.0.1:1\noperations:\n  observation:\n    enabled: false\n    checkpoint_dir: HOLD\n    sink: disabled\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := NewConfig()
	c.Database.Path = filepath.Join(t.TempDir(), "db")
	if err := c.LoadFromFile(p); err != nil {
		t.Fatalf("disabled observation configuration rejected: %v", err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func observationPolicy() *observeconfig.Config {
	return &observeconfig.Config{Enabled: true, CheckpointDir: "/private/observer", Sink: "disabled", Database: observeconfig.Resource{ID: strings.Repeat("11", 32), Series: strings.Repeat("22", 32), BudgetBytes: 1000}, WAL: observeconfig.Resource{ID: strings.Repeat("33", 32), Series: strings.Repeat("44", 32), BudgetBytes: 1000}, Warn: 8000, Stop: 9000, Clear: 7000}
}
func TestObservationRequiresPoolFour(t *testing.T) {
	for _, tc := range []struct {
		name, driver string
		pool         int32
		valid        bool
	}{{"pebble", DriverPebble, 4, false}, {"default", DriverPostgres, 0, false}, {"large", DriverPostgres, 8, false}, {"four", DriverPostgres, 4, true}} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewConfig()
			c.RPC.Endpoint = "http://127.0.0.1:1"
			c.Database.Driver = tc.driver
			c.Database.Postgres.DSN = "postgres://localhost/fixture"
			c.Database.Postgres.MaxConns = tc.pool
			c.Operations.Observation = observationPolicy()
			err := c.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
func TestObservationSharedCadenceMustMatch(t *testing.T) {
	o := opsguard.Config{Observation: observationPolicy(), Capacity: &opsguard.CapacityConfig{PostgresStatsInterval: 2 * time.Minute}}
	require.ErrorIs(t, o.WithDefaults().Validate(), observeconfig.ErrConfig)
}

func TestObservationUnknownConfigurationIsRejected(t *testing.T) {
	for _, field := range []string{"endpoint: http://127.0.0.1:1", "database: {schema: chain_one}", "warn_bp: 8000"} {
		t.Run(field, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(p, []byte("operations:\n  observation:\n    enabled: false\n    "+field+"\n"), 0600))
			require.Error(t, NewConfig().LoadFromFile(p))
		})
	}
}
