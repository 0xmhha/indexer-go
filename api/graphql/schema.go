package graphql

import (
	"github.com/graphql-go/graphql"
	"github.com/0xmhha/indexer-go/storage"
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
