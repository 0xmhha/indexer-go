package api

import (
	gql "github.com/graphql-go/graphql"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
)

// addQueries adds the WBFT consensus queries and subscriptions.
func addQueries(e *graphql.Extension, s *Schema) {

	e.AddQuery("wbftBlockExtra", &gql.Field{
		Type: wbftBlockExtraType,
		Args: gql.FieldConfigArgument{
			"blockNumber": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
		Resolve: s.resolveWBFTBlockExtra,
	})
	// Alias for frontend compatibility
	e.AddQuery("wbftBlock", &gql.Field{
		Type:        wbftBlockExtraType,
		Description: "Alias for wbftBlockExtra - returns WBFT block consensus data",
		Args: gql.FieldConfigArgument{
			"number": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
		Resolve: func(p gql.ResolveParams) (interface{}, error) {
			// Map 'number' argument to 'blockNumber' for the resolver
			p.Args["blockNumber"] = p.Args["number"]
			return s.resolveWBFTBlockExtra(p)
		},
	})
	e.AddQuery("wbftBlockExtraByHash", &gql.Field{
		Type: wbftBlockExtraType,
		Args: gql.FieldConfigArgument{
			"blockHash": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.HashType),
			},
		},
		Resolve: s.resolveWBFTBlockExtraByHash,
	})
	e.AddQuery("epochInfo", &gql.Field{
		Type: epochInfoType,
		Args: gql.FieldConfigArgument{
			"epochNumber": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
		Resolve: s.resolveEpochInfo,
	})
	// Alias for frontend compatibility
	e.AddQuery("epochByNumber", &gql.Field{
		Type:        epochInfoType,
		Description: "Alias for epochInfo - returns epoch information by epoch number",
		Args: gql.FieldConfigArgument{
			"number": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
		Resolve: func(p gql.ResolveParams) (interface{}, error) {
			// Map 'number' argument to 'epochNumber' for the resolver
			p.Args["epochNumber"] = p.Args["number"]
			return s.resolveEpochInfo(p)
		},
	})
	e.AddQuery("latestEpochInfo", &gql.Field{
		Type:    epochInfoType,
		Resolve: s.resolveLatestEpochInfo,
	})
	e.AddQuery("epochs", &gql.Field{
		Type:        gql.NewNonNull(epochSummaryConnectionType),
		Description: "Get paginated list of epochs (latest first)",
		Args: gql.FieldConfigArgument{
			"pagination": &gql.ArgumentConfig{
				Type: graphql.PaginationInputType(),
			},
		},
		Resolve: s.resolveEpochs,
	})
	e.AddQuery("validatorSigningStats", &gql.Field{
		Type: validatorSigningStatsType,
		Args: gql.FieldConfigArgument{
			"validatorAddress": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"fromBlock": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"toBlock": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
		Resolve: s.resolveValidatorSigningStats,
	})
	e.AddQuery("allValidatorsSigningStats", &gql.Field{
		Type: gql.NewNonNull(validatorSigningStatsConnectionType),
		Args: gql.FieldConfigArgument{
			"fromBlock": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"toBlock": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"pagination": &gql.ArgumentConfig{
				Type: graphql.PaginationInputType(),
			},
		},
		Resolve: s.resolveAllValidatorsSigningStats,
	})
	e.AddQuery("validatorSigningActivity", &gql.Field{
		Type: gql.NewNonNull(validatorSigningActivityConnectionType),
		Args: gql.FieldConfigArgument{
			"validatorAddress": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"fromBlock": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"toBlock": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"pagination": &gql.ArgumentConfig{
				Type: graphql.PaginationInputType(),
			},
		},
		Resolve: s.resolveValidatorSigningActivity,
	})
	e.AddQuery("blockSigners", &gql.Field{
		Type: blockSignersType,
		Args: gql.FieldConfigArgument{
			"blockNumber": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
		Resolve: s.resolveBlockSigners,
	})
	e.AddQuery("consensusData", &gql.Field{
		Type: consensusDataType,
		Args: gql.FieldConfigArgument{
			"blockNumber": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
		Resolve: s.resolveConsensusData,
	})
	e.AddQuery("validatorStats", &gql.Field{
		Type: validatorStatsType,
		Args: gql.FieldConfigArgument{
			"address": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"fromBlock": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"toBlock": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
		Resolve: s.resolveValidatorStats,
	})
	e.AddQuery("validatorParticipation", &gql.Field{
		Type: validatorParticipationType,
		Args: gql.FieldConfigArgument{
			"address": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"fromBlock": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"toBlock": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"pagination": &gql.ArgumentConfig{
				Type: graphql.PaginationInputType(),
			},
		},
		Resolve: s.resolveValidatorParticipation,
	})
	e.AddQuery("allValidatorStats", &gql.Field{
		Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(validatorStatsType))),
		Args: gql.FieldConfigArgument{
			"fromBlock": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"toBlock": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"pagination": &gql.ArgumentConfig{
				Type: graphql.PaginationInputType(),
			},
		},
		Resolve: s.resolveAllValidatorStats,
	})
	e.AddQuery("epochData", &gql.Field{
		Type: epochDataType,
		Args: gql.FieldConfigArgument{
			"epochNumber": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
		Resolve: s.resolveEpochData,
	})
	e.AddQuery("latestEpochData", &gql.Field{
		Type:    epochDataType,
		Resolve: s.resolveLatestEpochData,
	})

	e.AddSubscription("consensusBlock", &gql.Field{
		Type:        gql.NewNonNull(consensusBlockSubType),
		Description: "Subscribe to new consensus block events with validator participation data",
	})
	e.AddSubscription("consensusFork", &gql.Field{
		Type:        gql.NewNonNull(consensusForkSubType),
		Description: "Subscribe to chain fork detection events",
	})
	e.AddSubscription("consensusValidatorChange", &gql.Field{
		Type:        gql.NewNonNull(consensusValidatorChangeSubType),
		Description: "Subscribe to validator set change events at epoch boundaries",
	})
	e.AddSubscription("consensusError", &gql.Field{
		Type:        gql.NewNonNull(consensusErrorSubType),
		Description: "Subscribe to consensus errors and anomalies (round changes, low participation)",
	})
}
