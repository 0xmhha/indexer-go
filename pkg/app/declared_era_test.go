package app

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestDeclaredModeReadsEra1Archives: in the declared modes (per block and
// ranges of finalized blocks) the blocks held by era1 files are read from
// the files and never from the node, the rest from the node, and the
// database equals one indexed from the node alone.
func TestDeclaredModeReadsEra1Archives(t *testing.T) {
	const head, split = 300, 150
	for name, mode := range map[string]func(*config.Config){"declared per block": declaredMode, "declared ranges": sparseMode} {
		t.Run(name, func(t *testing.T) {
			sc := longReceipts(head)
			srv := testchain.NewServer(sc.Chain)
			defer srv.Close()

			eraDir := t.TempDir()
			require.NoError(t, sc.Chain.WriteEra1(filepath.Join(eraDir, "test-00000-00000000.era1"), 0, 99))
			require.NoError(t, sc.Chain.WriteEra1(filepath.Join(eraDir, "test-00001-00000000.era1"), 100, split))
			withEra := func(c *config.Config) { c.Source.EraDir = eraDir }

			dir := filepath.Join(t.TempDir(), "era")
			app := startRecordsApp(t, srv, dir, sc, mode, withEra)
			srv.ResetBlockLoads() // startup compares the hash where the archive meets the node
			runLiveUntil(t, app, head)
			app.Shutdown()
			loads := srv.BlockLoads()
			for n := uint64(0); n <= split; n++ {
				require.Zero(t, loads[n], "block %d is in the archive but was read from the node", n)
			}
			if name == "declared per block" {
				require.NotZero(t, loads[split+1], "blocks after the archive come from the node")
			}

			nodeOnly := filepath.Join(t.TempDir(), "node")
			fresh := startRecordsApp(t, srv, nodeOnly, sc, mode)
			runLiveUntil(t, fresh, head)
			fresh.Shutdown()

			requireOnlyDeclaredData(t, dir)
			diff := testchain.DiffKeyspace(dumpDir(t, nodeOnly), dumpDir(t, dir), 0)
			require.Empty(t, diff, "era1 + node differs from node only: %v", testchain.SummarizeDiff(diff))
		})
	}
}
