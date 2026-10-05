// Package api serves StableNet system contract data (minting, burning,
// governance, blacklist) over GraphQL and JSON-RPC. It registers its
// queries, the systemContractEvents subscription and its methods with the
// API registries when linked in, so the API packages do not depend on it.
package api

import (
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	sc "github.com/0xmhha/indexer-go/pkg/chains/stablenet/systemcontracts"
)

// Schema holds the resolvers of the system contract queries. storage is
// nil when the indexer's storage cannot hold system contract data; the
// queries then fail.
type Schema struct {
	storage sc.SystemContractReader
	logger  *zap.Logger
}

func newSchema(s any, logger *zap.Logger) *Schema {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Schema{storage: readerOf(s, logger), logger: logger}
}

// readerOf returns the system contract reader of the indexer's storage: the
// storage itself when it serves system contract data, otherwise a store over
// its key-value access, or nil.
func readerOf(s any, logger *zap.Logger) sc.SystemContractReader {
	if r, ok := s.(sc.SystemContractReader); ok {
		return r
	}
	if store, err := sc.Open(s, logger); err == nil {
		return store
	}
	return nil
}

func init() {
	initTypes()
	registerSubscriptions()
	registerMethods()
	graphql.RegisterExtension(sc.KeyspaceOwner, func(e *graphql.Extension) {
		addQueries(e, newSchema(e.Storage(), e.Logger()))
	})
}
