package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// TestReindexClearsAllChainData indexes both scenarios (including undo
// records and a rollback's orphans) and reindexes: only preserved keys may
// remain (the scenarios store no user data such as contract verification;
// the schema marker, and the outbox sequence and relay cursor so the events
// of the new indexing continue the numbering consumers have seen), and the
// database must open again.
func TestReindexClearsAllChainData(t *testing.T) {
	for name, chain := range map[string]*testchain.Chain{
		"evm":       testchain.BuildDefault().Chain,
		"stablenet": testchain.BuildStableNet().Chain,
	} {
		t.Run(name, func(t *testing.T) {
			srv := testchain.NewServer(chain)
			defer srv.Close()
			dir := filepath.Join(t.TempDir(), "db")
			runSession(t, srv, dir, 0, chain.Head())

			require.NoError(t, reindexData(dir, zap.NewNop()))
			left, err := testchain.DumpKeyspace(dir, nil)
			require.NoError(t, err)
			var keys []string
		next:
			for _, e := range left {
				for _, p := range storage.PrefixesOf(storage.Preserved) {
					if strings.HasPrefix(string(e.Key), p) {
						continue next
					}
				}
				keys = append(keys, string(e.Key))
			}
			require.Empty(t, keys, "keys left after reindex")

			db, err := storage.NewPebbleStorage(storage.DefaultConfig(dir))
			require.NoError(t, err, "the database opens after a reindex")
			require.NoError(t, db.Close())
		})
	}
}

// TestReindexKeepsVerifiedDatabaseOpenable: a reindex preserves contract
// verification data, so it must keep the schema marker too; without it the
// remaining data looks like an unmarked schema 1 database and the indexer
// refuses to start.
func TestReindexKeepsVerifiedDatabaseOpenable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "db")
	db, err := storage.NewPebbleStorage(storage.DefaultConfig(dir))
	require.NoError(t, err)
	addr := common.HexToAddress("0x1")
	require.NoError(t, db.SetABI(context.Background(), addr, []byte(`[]`)))
	require.NoError(t, db.Close())

	require.NoError(t, reindexData(dir, zap.NewNop()))
	db, err = storage.NewPebbleStorage(storage.DefaultConfig(dir))
	require.NoError(t, err, "the database opens after a reindex")
	defer func() { _ = db.Close() }()
	abi, err := db.GetABI(context.Background(), addr)
	require.NoError(t, err)
	require.Equal(t, []byte(`[]`), abi, "verification data is preserved")
}
