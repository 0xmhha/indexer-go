package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/config"
)

// testnetDrafts are the per-chain configuration drafts of the testnet
// deployment (docs/examples/testnets), one process each.
var testnetDrafts = []string{"stablenet-testnet", "bsc-testnet", "sepolia", "arc-testnet"}

// TestTestnetDraftsLoadOnceResolved: with its UNCONFIRMED_* inputs filled
// in, every chain's draft is a valid single-chain configuration, the Arc
// feed included; unresolved, it is refused.
func TestTestnetDraftsLoadOnceResolved(t *testing.T) {
	const enode = "enode://79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8@192.0.2.1:30303"
	resolve := strings.NewReplacer(
		`"UNCONFIRMED_STABLENET_START_HEIGHT"`, "101",
		`"UNCONFIRMED_BSC_START_HEIGHT"`, "101",
		`"UNCONFIRMED_SEPOLIA_START_HEIGHT"`, "101",
		`"UNCONFIRMED_ARC_START_HEIGHT"`, "101",
		`"UNCONFIRMED_ARC_FINALIZED_HEIGHT"`, "100",
		`"UNCONFIRMED_ARC_FINALIZED_HASH"`, `"0x`+strings.Repeat("2", 64)+`"`,
		`"UNCONFIRMED_ARC_FORK_HASH"`, `"910609e1"`,
		`"UNCONFIRMED_ARC_FORK_NEXT"`, "0",
		`"UNCONFIRMED_ARC_ENODE"`, `"`+enode+`"`,
	)
	// The entrypoint supplies the DSN from the secret file at runtime.
	t.Setenv("INDEXER_DB_POSTGRES_DSN", "postgres://fixture@127.0.0.1:5432/fixture")
	for _, chain := range testnetDrafts {
		t.Run(chain, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "examples", "testnets", chain+".draft.yaml"))
			require.NoError(t, err)

			unresolved := filepath.Join(t.TempDir(), "unresolved.yaml")
			require.NoError(t, os.WriteFile(unresolved, raw, 0o600))
			_, err = config.LoadUnvalidated(unresolved)
			require.Error(t, err, "placeholders must not decode")

			resolved := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(resolved, []byte(resolve.Replace(string(raw))), 0o600))
			cfg, err := config.LoadUnvalidated(resolved)
			require.NoError(t, err)
			require.NoError(t, ValidateConfig(cfg))
			require.False(t, cfg.MultiChainMode(), "one process per chain")
			require.Equal(t, chain == "arc-testnet", cfg.Source.Feed != nil, "only Arc selects the feed")
			require.NoError(t, preflightFeeds(cfg, false))
		})
	}
}
