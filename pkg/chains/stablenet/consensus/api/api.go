// Package api serves StableNet WBFT consensus data over GraphQL and
// JSON-RPC. It registers its queries, subscriptions and methods with the
// API registries when linked in, so the API packages do not depend on it.
package api

import (
	"context"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/consensus"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// backend is what the resolvers read: WBFT data and stored blocks.
type backend interface {
	consensus.WBFTReader
	consensus.WBFTWriter
	GetBlock(ctx context.Context, height uint64) (*model.Block, error)
}

// Schema holds the resolvers of the consensus queries. storage is nil when
// the indexer's storage cannot hold WBFT data; the queries then fail.
type Schema struct {
	storage backend
	logger  *zap.Logger
}

func newSchema(s any, logger *zap.Logger) *Schema {
	if logger == nil {
		logger = zap.NewNop()
	}
	out := &Schema{logger: logger}
	if store, err := consensus.Open(s, logger); err == nil {
		out.storage = store
	}
	return out
}

func init() {
	initTypes()
	registerSubscriptions()
	registerMethods()
	graphql.RegisterExtension(consensus.KeyspaceOwner, func(e *graphql.Extension) {
		addQueries(e, newSchema(e.Storage(), e.Logger()))
	})
}
