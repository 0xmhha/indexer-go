// Package api serves StableNet fee delegation statistics over GraphQL. It
// registers its queries with the GraphQL extension registry when linked in,
// so the API package does not depend on it.
package api

import (
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/feedelegation"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Schema holds the resolvers of the fee delegation queries. stats is nil
// when the indexer's storage cannot serve them; the queries then fail.
// history is nil without storage; the time arguments are then ignored.
type Schema struct {
	history port.HistoricalReader
	stats   *feedelegation.Stats
	logger  *zap.Logger
}

func init() {
	graphql.RegisterExtension("stablenet.fee_delegation", func(e *graphql.Extension) {
		s := &Schema{history: e.Storage(), logger: e.Logger()}
		if s.logger == nil {
			s.logger = zap.NewNop()
		}
		if stats, err := feedelegation.Open(e.Storage()); err == nil {
			s.stats = stats
		}
		addQueries(e, s)
	})
}
