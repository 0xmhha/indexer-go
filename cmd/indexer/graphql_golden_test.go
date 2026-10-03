package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
)

const goldenGraphQL = "testdata/golden/graphql.json"

// graphqlGoldenQueries are read through the production GraphQL schema after
// indexing the reference scenario. They cover features that the removed
// storage wrapper used to disable (system contract events, consensus
// queries that type-asserted the concrete storage).
var graphqlGoldenQueries = []struct {
	name  string
	query string
}{
	{"mintEvents", `{ mintEvents(filter: {fromBlock: "0", toBlock: "20"}) { totalCount nodes { blockNumber minter to amount } } }`},
	{"burnEvents", `{ burnEvents(filter: {fromBlock: "0", toBlock: "20"}) { totalCount nodes { blockNumber burner amount } } }`},
	{"allValidatorsSigningStats", `{ allValidatorsSigningStats(fromBlock: "0", toBlock: "20") { totalCount } }`},
}

// TestGraphQLGolden pins API results for the reference scenario.
// Regenerate with: go test ./cmd/indexer -run TestGraphQLGolden -update
func TestGraphQLGolden(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")

	app := startAppMode(t, srv, dir, atomicMode)
	defer app.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))

	h, err := graphql.NewHandler(app.storage, zap.NewNop())
	require.NoError(t, err)

	results := map[string]any{}
	for _, q := range graphqlGoldenQueries {
		res := h.ExecuteQuery(q.query, nil)
		require.Empty(t, res.Errors, "%s: %v", q.name, res.Errors)
		results[q.name] = res.Data
	}
	got, err := json.MarshalIndent(results, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')

	if *updateGolden {
		require.NoError(t, os.WriteFile(goldenGraphQL, got, 0o644))
		return
	}
	want, err := os.ReadFile(goldenGraphQL)
	require.NoError(t, err, "missing golden file; run with -update")
	require.Equal(t, string(bytes.TrimSpace(want)), string(bytes.TrimSpace(got)))
}
