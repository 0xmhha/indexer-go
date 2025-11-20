package graphql

import (
	"github.com/0xmhha/indexer-go/storage"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// Schema holds the GraphQL schema
type Schema struct {
	schema  graphql.Schema
	storage storage.Storage
	logger  *zap.Logger
}

// NewSchema creates a new GraphQL schema
func NewSchema(store storage.Storage, logger *zap.Logger) (*Schema, error) {
	s := &Schema{
		storage: store,
		logger:  logger,
	}

	// Create query type
	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"latestHeight": &graphql.Field{
				Type:    graphql.NewNonNull(bigIntType),
				Resolve: s.resolveLatestHeight,
			},
			"block": &graphql.Field{
				Type: blockType,
				Args: graphql.FieldConfigArgument{
					"number": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(bigIntType),
					},
				},
				Resolve: s.resolveBlock,
			},
			"blockByHash": &graphql.Field{
				Type: blockType,
				Args: graphql.FieldConfigArgument{
					"hash": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(hashType),
					},
				},
				Resolve: s.resolveBlockByHash,
			},
			"blocks": &graphql.Field{
				Type: graphql.NewNonNull(blockConnectionType),
				Args: graphql.FieldConfigArgument{
					"filter": &graphql.ArgumentConfig{
						Type: blockFilterType,
					},
					"pagination": &graphql.ArgumentConfig{
						Type: paginationInputType,
					},
				},
				Resolve: s.resolveBlocks,
			},
			"transaction": &graphql.Field{
				Type: transactionType,
				Args: graphql.FieldConfigArgument{
					"hash": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(hashType),
					},
				},
				Resolve: s.resolveTransaction,
			},
			"transactions": &graphql.Field{
				Type: graphql.NewNonNull(transactionConnectionType),
				Args: graphql.FieldConfigArgument{
					"filter": &graphql.ArgumentConfig{
						Type: transactionFilterType,
					},
					"pagination": &graphql.ArgumentConfig{
						Type: paginationInputType,
					},
				},
				Resolve: s.resolveTransactions,
			},
			"transactionsByAddress": &graphql.Field{
				Type: graphql.NewNonNull(transactionConnectionType),
				Args: graphql.FieldConfigArgument{
					"address": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(addressType),
					},
					"pagination": &graphql.ArgumentConfig{
						Type: paginationInputType,
					},
				},
				Resolve: s.resolveTransactionsByAddress,
			},
			"receipt": &graphql.Field{
				Type: receiptType,
				Args: graphql.FieldConfigArgument{
					"transactionHash": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(hashType),
					},
				},
				Resolve: s.resolveReceipt,
			},
			"receiptsByBlock": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(receiptType)),
				Args: graphql.FieldConfigArgument{
					"blockNumber": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(bigIntType),
					},
				},
				Resolve: s.resolveReceiptsByBlock,
			},
			"logs": &graphql.Field{
				Type: graphql.NewNonNull(logConnectionType),
				Args: graphql.FieldConfigArgument{
					"filter": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(logFilterType),
					},
					"pagination": &graphql.ArgumentConfig{
						Type: paginationInputType,
					},
				},
				Resolve: s.resolveLogs,
			},
			// Historical data queries
			"blocksByTimeRange": &graphql.Field{
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
			},
			"blockByTimestamp": &graphql.Field{
				Type: blockType,
				Args: graphql.FieldConfigArgument{
					"timestamp": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(bigIntType),
					},
				},
				Resolve: s.resolveBlockByTimestamp,
			},
			"transactionsByAddressFiltered": &graphql.Field{
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
			},
			"addressBalance": &graphql.Field{
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
			},
			"balanceHistory": &graphql.Field{
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
			},
			"blockCount": &graphql.Field{
				Type:    graphql.NewNonNull(bigIntType),
				Resolve: s.resolveBlockCount,
			},
			"transactionCount": &graphql.Field{
				Type:    graphql.NewNonNull(bigIntType),
				Resolve: s.resolveTransactionCount,
			},
			// Analytics queries
			"topMiners": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(minerStatsType))),
				Args: graphql.FieldConfigArgument{
					"limit": &graphql.ArgumentConfig{
						Type: graphql.Int,
					},
				},
				Resolve: s.resolveTopMiners,
			},
			"tokenBalances": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(tokenBalanceType))),
				Args: graphql.FieldConfigArgument{
					"address": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(addressType),
					},
				},
				Resolve: s.resolveTokenBalances,
			},
		},
	})

	// Create schema
	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query: queryType,
	})
	if err != nil {
		return nil, err
	}

	s.schema = schema
	return s, nil
}

// Schema returns the GraphQL schema
func (s *Schema) Schema() graphql.Schema {
	return s.schema
}
