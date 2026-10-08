package graphql

import (
	"github.com/graphql-go/graphql"
)

// WithCoreQueries adds core blockchain queries (block, transaction, receipt, logs)
func (b *SchemaBuilder) WithCoreQueries() *SchemaBuilder {
	s := b.schema

	b.queries["latestHeight"] = &graphql.Field{
		Type:    graphql.NewNonNull(bigIntType),
		Resolve: s.resolveLatestHeight,
	}
	b.queries["streamSequence"] = &graphql.Field{
		Type: bigIntType,
		Description: "The sequence of the last committed change stream event, null when events are not kept. " +
			"Read it before a snapshot of the state and subscribe with fromSequence set to the next sequence",
		Resolve: s.resolveStreamSequence,
	}
	b.queries["block"] = &graphql.Field{
		Type: blockType,
		Args: graphql.FieldConfigArgument{
			"number": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveBlock,
	}
	b.queries["blockByHash"] = &graphql.Field{
		Type: blockType,
		Args: graphql.FieldConfigArgument{
			"hash": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(hashType),
			},
		},
		Resolve: s.resolveBlockByHash,
	}
	b.queries["blocks"] = &graphql.Field{
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
	}
	b.queries["blocksRange"] = &graphql.Field{
		Type:        graphql.NewNonNull(blockRangeResultType),
		Description: "Get blocks in a specific range (optimized for frontend catch-up). Maximum 100 blocks per request.",
		Args: graphql.FieldConfigArgument{
			"startNumber": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Starting block number (inclusive)",
			},
			"endNumber": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Ending block number (inclusive)",
			},
			"includeTransactions": &graphql.ArgumentConfig{
				Type:         graphql.Boolean,
				Description:  "Include transaction data in response (default: true)",
				DefaultValue: true,
			},
			"includeReceipts": &graphql.ArgumentConfig{
				Type:         graphql.Boolean,
				Description:  "Include receipt data for each transaction (default: false)",
				DefaultValue: false,
			},
		},
		Resolve: s.resolveBlocksRange,
	}
	b.queries["transaction"] = &graphql.Field{
		Type: transactionType,
		Args: graphql.FieldConfigArgument{
			"hash": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(hashType),
			},
		},
		Resolve: s.resolveTransaction,
	}
	b.queries["transactions"] = &graphql.Field{
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
	}
	b.queries["transactionsByAddress"] = &graphql.Field{
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
	}
	b.queries["receipt"] = &graphql.Field{
		Type: receiptType,
		Args: graphql.FieldConfigArgument{
			"transactionHash": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(hashType),
			},
		},
		Resolve: s.resolveReceipt,
	}
	b.queries["receiptsByBlock"] = &graphql.Field{
		Type: graphql.NewList(graphql.NewNonNull(receiptType)),
		Args: graphql.FieldConfigArgument{
			"blockNumber": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveReceiptsByBlock,
	}
	b.queries["logs"] = &graphql.Field{
		Type: graphql.NewNonNull(logConnectionType),
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(logFilterType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
			"decode": &graphql.ArgumentConfig{
				Type:        graphql.Boolean,
				Description: "Decode logs using ABI if available",
			},
		},
		Resolve: s.resolveLogs,
	}
	b.queries["blockCount"] = &graphql.Field{
		Type:    graphql.NewNonNull(bigIntType),
		Resolve: s.resolveBlockCount,
	}
	b.queries["transactionCount"] = &graphql.Field{
		Type:    graphql.NewNonNull(bigIntType),
		Resolve: s.resolveTransactionCount,
	}

	return b
}
