package graphql

import (
	"github.com/graphql-go/graphql"
)

// WithTokenMetadataQueries adds token metadata queries to the schema builder
func (b *SchemaBuilder) WithTokenMetadataQueries() *SchemaBuilder {
	s := b.schema

	b.queries["tokenMetadata"] = &graphql.Field{
		Type:        tokenMetadataType,
		Description: "Get token metadata by contract address",
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Token contract address",
			},
		},
		Resolve: s.resolveTokenMetadata,
	}

	b.queries["tokens"] = &graphql.Field{
		Type:        graphql.NewNonNull(tokenMetadataConnectionType),
		Description: "List tokens with optional standard filter and pagination",
		Args: graphql.FieldConfigArgument{
			"standard": &graphql.ArgumentConfig{
				Type:        tokenStandardEnumType,
				Description: "Filter by token standard (ERC20, ERC721, ERC1155)",
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveTokens,
	}

	b.queries["searchTokens"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(tokenMetadataType))),
		Description: "Search tokens by name or symbol",
		Args: graphql.FieldConfigArgument{
			"query": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Search query (matches name or symbol prefix)",
			},
			"limit": &graphql.ArgumentConfig{
				Type:        graphql.Int,
				Description: "Maximum number of results (default: 10)",
			},
		},
		Resolve: s.resolveSearchTokens,
	}

	b.queries["tokenCount"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Int),
		Description: "Get token count by standard",
		Args: graphql.FieldConfigArgument{
			"standard": &graphql.ArgumentConfig{
				Type:        tokenStandardEnumType,
				Description: "Filter by token standard (optional)",
			},
		},
		Resolve: s.resolveTokenCount,
	}

	return b
}

// WithTokenHolderQueries adds token holder queries to the schema builder
func (b *SchemaBuilder) WithTokenHolderQueries() *SchemaBuilder {
	s := b.schema

	b.queries["tokenHolders"] = &graphql.Field{
		Type:        graphql.NewNonNull(tokenHolderConnectionType),
		Description: "Get token holders sorted by balance (descending)",
		Args: graphql.FieldConfigArgument{
			"token": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Token contract address",
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveTokenHolders,
	}

	b.queries["tokenHolderCount"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Int),
		Description: "Get the number of unique holders for a token",
		Args: graphql.FieldConfigArgument{
			"token": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Token contract address",
			},
		},
		Resolve: s.resolveTokenHolderCount,
	}

	b.queries["tokenBalance"] = &graphql.Field{
		Type:        graphql.NewNonNull(bigIntType),
		Description: "Get the balance of a specific holder for a token",
		Args: graphql.FieldConfigArgument{
			"token": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Token contract address",
			},
			"holder": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Holder address",
			},
		},
		Resolve: s.resolveTokenBalance,
	}

	b.queries["tokenHolderStats"] = &graphql.Field{
		Type:        tokenHolderStatsType,
		Description: "Get aggregate statistics for a token",
		Args: graphql.FieldConfigArgument{
			"token": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Token contract address",
			},
		},
		Resolve: s.resolveTokenHolderStats,
	}

	return b
}
