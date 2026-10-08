package graphql

import (
	"github.com/graphql-go/graphql"
)

// WithSubscriptions adds GraphQL subscriptions
func (b *SchemaBuilder) WithSubscriptions() *SchemaBuilder {
	b.subscriptions["newBlock"] = &graphql.Field{
		Type:        graphql.NewNonNull(blockType),
		Description: "Subscribe to new blocks as they are indexed",
	}
	b.subscriptions["newTransaction"] = &graphql.Field{
		Type:        graphql.NewNonNull(transactionType),
		Description: "Subscribe to new transactions as they are indexed",
	}
	b.subscriptions["newPendingTransactions"] = &graphql.Field{
		Type: graphql.NewNonNull(transactionType),
		Args: graphql.FieldConfigArgument{
			"limit": &graphql.ArgumentConfig{
				Type: graphql.Int,
			},
		},
		Description: "Subscribe to new pending transactions (if available)",
	}
	b.subscriptions["logs"] = &graphql.Field{
		Type: graphql.NewNonNull(logType),
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(logFilterType),
			},
		},
		Description: "Subscribe to new logs matching a filter",
	}

	b.subscriptions["reorg"] = &graphql.Field{
		Type:        graphql.NewNonNull(reorgType),
		Description: "Subscribe to chain reorganizations the indexer rolled back. Logs of the removed blocks follow on the logs subscription with removed: true; the removed blocks stay queryable with orphanedBlock",
	}

	return b
}
