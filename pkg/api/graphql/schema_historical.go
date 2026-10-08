package graphql

import (
	"github.com/graphql-go/graphql"
)

// WithHistoricalQueries adds historical data queries
func (b *SchemaBuilder) WithHistoricalQueries() *SchemaBuilder {
	s := b.schema

	b.queries["blocksByTimeRange"] = &graphql.Field{
		Type: graphql.NewNonNull(blockConnectionType),
		Args: graphql.FieldConfigArgument{
			"fromTime": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toTime": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveBlocksByTimeRange,
	}
	b.queries["blockByTimestamp"] = &graphql.Field{
		Type: blockType,
		Args: graphql.FieldConfigArgument{
			"timestamp": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveBlockByTimestamp,
	}
	b.queries["transactionsByAddressFiltered"] = &graphql.Field{
		Type: graphql.NewNonNull(transactionConnectionType),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"filter": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(historicalTransactionFilterType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveTransactionsByAddressFiltered,
	}
	b.queries["addressBalance"] = &graphql.Field{
		Type: graphql.NewNonNull(bigIntType),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"blockNumber": &graphql.ArgumentConfig{
				Type: bigIntType,
			},
		},
		Resolve: s.resolveAddressBalance,
	}
	b.queries["balanceHistory"] = &graphql.Field{
		Type: graphql.NewNonNull(balanceHistoryConnectionType),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveBalanceHistory,
	}

	return b
}
