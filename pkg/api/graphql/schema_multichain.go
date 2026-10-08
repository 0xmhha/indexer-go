package graphql

import (
	"github.com/graphql-go/graphql"
)

// WithMultiChainQueries adds multi-chain management queries and mutations
func (b *SchemaBuilder) WithMultiChainQueries() *SchemaBuilder {
	s := b.schema

	// Queries
	b.queries["chains"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(chainType))),
		Description: "Get all registered chains",
		Resolve:     s.resolveChains,
	}
	b.queries["chain"] = &graphql.Field{
		Type:        chainType,
		Description: "Get a specific chain by ID",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Chain identifier",
			},
		},
		Resolve: s.resolveChain,
	}
	b.queries["chainHealth"] = &graphql.Field{
		Type:        healthStatusType,
		Description: "Get health status of a chain",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Chain identifier",
			},
		},
		Resolve: s.resolveChainHealth,
	}

	// Mutations
	b.mutations["registerChain"] = &graphql.Field{
		Type:        graphql.NewNonNull(chainType),
		Description: "Register a new chain",
		Args: graphql.FieldConfigArgument{
			"input": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(registerChainInputType),
				Description: "Chain registration details",
			},
		},
		Resolve: s.resolveRegisterChain,
	}
	b.mutations["startChain"] = &graphql.Field{
		Type:        graphql.NewNonNull(chainType),
		Description: "Start a registered chain",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Chain identifier",
			},
		},
		Resolve: s.resolveStartChain,
	}
	b.mutations["stopChain"] = &graphql.Field{
		Type:        graphql.NewNonNull(chainType),
		Description: "Stop a running chain",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Chain identifier",
			},
		},
		Resolve: s.resolveStopChain,
	}
	b.mutations["unregisterChain"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Boolean),
		Description: "Unregister a chain (must be stopped first)",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Chain identifier",
			},
		},
		Resolve: s.resolveUnregisterChain,
	}

	return b
}
