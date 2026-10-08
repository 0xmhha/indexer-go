package graphql

import (
	"github.com/graphql-go/graphql"
)

// WithModuleQueries adds ERC-7579 Module queries
func (b *SchemaBuilder) WithModuleQueries() *SchemaBuilder {
	s := b.schema

	// Account modules (grouped by type)
	b.queries["accountModules"] = &graphql.Field{
		Type:        graphql.NewNonNull(accountModulesType),
		Description: "Get all modules installed on a smart account, grouped by type",
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Smart account address",
			},
		},
		Resolve: s.resolveAccountModules,
	}

	// Installed modules (filterable, paginated)
	b.queries["installedModules"] = &graphql.Field{
		Type:        graphql.NewNonNull(installedModuleConnectionType),
		Description: "Get installed modules with optional filtering by account or module type",
		Args: graphql.FieldConfigArgument{
			"account": &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "Filter by smart account address",
			},
			"moduleType": &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "Filter by module type (VALIDATOR, EXECUTOR, FALLBACK, HOOK)",
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveInstalledModules,
	}

	// Module stats
	b.queries["moduleStats"] = &graphql.Field{
		Type:        moduleStatsType,
		Description: "Get aggregate statistics for a module contract",
		Args: graphql.FieldConfigArgument{
			"module": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Module contract address",
			},
		},
		Resolve: s.resolveModuleStats,
	}

	// List module stats (paginated)
	b.queries["listModuleStats"] = &graphql.Field{
		Type:        graphql.NewNonNull(moduleStatsConnectionType),
		Description: "List all module stats with pagination",
		Args: graphql.FieldConfigArgument{
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveListModuleStats,
	}

	// Module event count
	b.queries["moduleEventCount"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Int),
		Description: "Get the total count of module install records (one per account and module; uninstalls are not counted)",
		Resolve:     s.resolveModuleEventCount,
	}

	return b
}
