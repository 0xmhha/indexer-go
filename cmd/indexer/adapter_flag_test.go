package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
)

// TestAdapterFlagSelectsProfile: --adapter names the chain profile, also by
// the chain's former name; a name that is no profile (anvil) falls back to
// detection.
func TestAdapterFlagSelectsProfile(t *testing.T) {
	sc := testchain.BuildDefault() // reports a generic client version
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)

	for flag, want := range map[string]string{"": "evm", "anvil": "evm", "stableone": "stablenet", "stablenet": "stablenet"} {
		cfg := config.NewConfig()
		cfg.RPC.Endpoint = srv.URL()
		cfg.RPC.Timeout = 5 * time.Second
		setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
		cfg.API.Enabled = false
		app, err := NewApp(cfg, zap.NewNop(), false, flag)
		require.NoError(t, err, flag)
		require.NotNil(t, app.profile, flag)
		require.Equal(t, want, app.profile.ID(), "--adapter %q", flag)
		app.Shutdown()
	}
}
