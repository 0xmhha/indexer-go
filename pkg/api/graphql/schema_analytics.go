package graphql

import (
	"github.com/graphql-go/graphql"
)

// WithAnalyticsQueries adds analytics and statistics queries
func (b *SchemaBuilder) WithAnalyticsQueries() *SchemaBuilder {
	s := b.schema

	b.queries["topMiners"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(minerStatsType))),
		Args: graphql.FieldConfigArgument{
			"limit": &graphql.ArgumentConfig{
				Type:        graphql.Int,
				Description: "Maximum number of miners to return (max: 100, default: 10)",
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type:        bigIntType,
				Description: "Start block number (0 = genesis)",
			},
			"toBlock": &graphql.ArgumentConfig{
				Type:        bigIntType,
				Description: "End block number (0 = latest)",
			},
		},
		Description: "Get top miners by block count in a given block range",
		Resolve:     s.resolveTopMiners,
	}
	b.queries["tokenBalances"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(tokenBalanceType))),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Address to query token balances for",
			},
			"tokenType": &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "Filter by token type (ERC20, ERC721, ERC1155)",
			},
		},
		Description: "Get token balances for an address",
		Resolve:     s.resolveTokenBalances,
	}
	b.queries["search"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(searchResultType))),
		Args: graphql.FieldConfigArgument{
			"query": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Search query (block number, hash, or address)",
			},
			"types": &graphql.ArgumentConfig{
				Type:        graphql.NewList(graphql.String),
				Description: "Optional filter for result types (block, transaction, address, contract)",
			},
			"limit": &graphql.ArgumentConfig{
				Type:         graphql.Int,
				DefaultValue: 10,
				Description:  "Maximum number of results to return",
			},
		},
		Description: "Unified search across blocks, transactions, and addresses",
		Resolve:     s.resolveSearch,
	}
	b.queries["contractVerification"] = &graphql.Field{
		Type: contractVerificationType,
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Contract address to get verification data for",
			},
		},
		Description: "Get contract verification data",
		Resolve:     s.resolveContractVerification,
	}
	b.queries["gasStats"] = &graphql.Field{
		Type: gasStatsType,
		Args: graphql.FieldConfigArgument{
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Description: "Get gas usage statistics for a block range",
		Resolve:     s.resolveGasStats,
	}
	b.queries["addressGasStats"] = &graphql.Field{
		Type: addressGasStatsType,
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
		},
		Description: "Get gas usage statistics for a specific address",
		Resolve:     s.resolveAddressGasStats,
	}
	b.queries["topAddressesByGasUsed"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressGasStatsType))),
		Args: graphql.FieldConfigArgument{
			"limit": &graphql.ArgumentConfig{
				Type: graphql.Int,
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Description: "Get top addresses by total gas used",
		Resolve:     s.resolveTopAddressesByGasUsed,
	}
	b.queries["topAddressesByTxCount"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressActivityStatsType))),
		Args: graphql.FieldConfigArgument{
			"limit": &graphql.ArgumentConfig{
				Type: graphql.Int,
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Description: "Get top addresses by transaction count",
		Resolve:     s.resolveTopAddressesByTxCount,
	}
	b.queries["networkMetrics"] = &graphql.Field{
		Type: networkMetricsType,
		Args: graphql.FieldConfigArgument{
			"fromTime": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toTime": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Description: "Get network activity metrics for a time range",
		Resolve:     s.resolveNetworkMetrics,
	}
	b.queries["addressStats"] = &graphql.Field{
		Type: graphql.NewNonNull(addressStatsType),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
		},
		Description: "Get aggregated statistics for an address",
		Resolve:     s.resolveAddressStats,
	}

	return b
}
