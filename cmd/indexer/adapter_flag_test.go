package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/adapters/detector"
)

// TestAdapterFlagSelectsProfile: --adapter names the chain for both the
// chain profile and the adapter, also by the chain's former name.
func TestAdapterFlagSelectsProfile(t *testing.T) {
	sc := testchain.BuildDefault() // reports a generic client version
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)

	for flag, want := range map[string]string{"": "evm", "stableone": "stablenet", "stablenet": "stablenet"} {
		cfg := config.NewConfig()
		cfg.RPC.Endpoint = srv.URL()
		cfg.RPC.Timeout = 5 * time.Second
		cfg.Database.Path = filepath.Join(t.TempDir(), "db")
		cfg.API.Enabled = false
		app, err := NewApp(cfg, zap.NewNop(), false, flag)
		require.NoError(t, err, flag)
		require.NotNil(t, app.profile, flag)
		require.Equal(t, want, app.profile.ID(), "--adapter %q", flag)
		if want == "stablenet" {
			require.Equal(t, detector.NodeTypeStableOne, app.nodeInfo.Type, "--adapter %q", flag)
		}
		app.Shutdown()
	}
}
