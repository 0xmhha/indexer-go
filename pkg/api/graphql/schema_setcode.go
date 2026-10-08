package graphql

import (
	"github.com/graphql-go/graphql"
)

// WithSetCodeQueries adds EIP-7702 SetCode transaction queries
func (b *SchemaBuilder) WithSetCodeQueries() *SchemaBuilder {
	s := b.schema

	// Single authorization query by txHash and authIndex
	b.queries["setCodeAuthorization"] = &graphql.Field{
		Type:        setCodeAuthorizationType,
		Description: "Get a specific SetCode authorization by transaction hash and authorization index",
		Args: graphql.FieldConfigArgument{
			"txHash": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(hashType),
				Description: "Transaction hash",
			},
			"authIndex": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Authorization index within the transaction",
			},
		},
		Resolve: s.resolveSetCodeAuthorization,
	}

	// All authorizations in a transaction
	b.queries["setCodeAuthorizationsByTx"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(setCodeAuthorizationType))),
		Description: "Get all SetCode authorizations in a transaction",
		Args: graphql.FieldConfigArgument{
			"txHash": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(hashType),
				Description: "Transaction hash",
			},
		},
		Resolve: s.resolveSetCodeAuthorizationsByTx,
	}

	// Authorizations by target address (code delegation target)
	b.queries["setCodeAuthorizationsByTarget"] = &graphql.Field{
		Type:        graphql.NewNonNull(setCodeAuthorizationConnectionType),
		Description: "Get SetCode authorizations where the address is the delegation target",
		Args: graphql.FieldConfigArgument{
			"target": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Target address (code delegation target)",
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveSetCodeAuthorizationsByTarget,
	}

	// Authorizations by authority address (signer)
	b.queries["setCodeAuthorizationsByAuthority"] = &graphql.Field{
		Type:        graphql.NewNonNull(setCodeAuthorizationConnectionType),
		Description: "Get SetCode authorizations where the address is the authority (signer)",
		Args: graphql.FieldConfigArgument{
			"authority": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Authority address (signer of the authorization)",
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveSetCodeAuthorizationsByAuthority,
	}

	// Address SetCode info (delegation status and stats)
	b.queries["addressSetCodeInfo"] = &graphql.Field{
		Type:        graphql.NewNonNull(addressSetCodeInfoType),
		Description: "Get SetCode information for an address including delegation status and activity stats",
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Address to query",
			},
		},
		Resolve: s.resolveAddressSetCodeInfo,
	}

	// SetCode transactions in a block
	b.queries["setCodeTransactionsInBlock"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(transactionType))),
		Description: "Get all SetCode transactions in a specific block",
		Args: graphql.FieldConfigArgument{
			"blockNumber": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block number",
			},
		},
		Resolve: s.resolveSetCodeTransactionsInBlock,
	}

	// Recent SetCode transactions
	b.queries["recentSetCodeTransactions"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(transactionType))),
		Description: "Get recent SetCode transactions",
		Args: graphql.FieldConfigArgument{
			"limit": &graphql.ArgumentConfig{
				Type:        graphql.Int,
				Description: "Maximum number of transactions to return (max: 100, default: 10)",
			},
		},
		Resolve: s.resolveRecentSetCodeTransactions,
	}

	// Total SetCode transaction count
	b.queries["setCodeTransactionCount"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Int),
		Description: "Get the total count of SetCode transactions",
		Resolve:     s.resolveSetCodeTransactionCount,
	}

	return b
}
