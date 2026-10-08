package graphql

import (
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/graphql-go/graphql"
)

// WithContractRegistrationService sets the contract registration service
func (b *SchemaBuilder) WithContractRegistrationService(service *events.ContractRegistrationService) *SchemaBuilder {
	b.schema.contractRegistrationService = service
	return b
}

// WithDynamicContractQueries adds dynamic contract registration queries and mutations
func (b *SchemaBuilder) WithDynamicContractQueries() *SchemaBuilder {
	s := b.schema

	// Queries
	b.queries["registeredContract"] = &graphql.Field{
		Type:        registeredContractType,
		Description: "Get a registered contract by address",
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Contract address",
			},
		},
		Resolve: s.resolveRegisteredContract,
	}
	b.queries["registeredContracts"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(registeredContractType))),
		Description: "Get all registered contracts",
		Resolve:     s.resolveRegisteredContracts,
	}
	b.queries["dynamicContractEvents"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(dynamicContractEventType))),
		Description: "Get events from registered contracts",
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(dynamicContractEventFilterType),
				Description: "Event filter",
			},
			"pagination": &graphql.ArgumentConfig{
				Type:        paginationInputType,
				Description: "Pagination options",
			},
		},
		Resolve: s.resolveDynamicContractEvents,
	}

	// Mutations
	b.mutations["registerContract"] = &graphql.Field{
		Type:        graphql.NewNonNull(registeredContractType),
		Description: "Register a contract for dynamic event parsing",
		Args: graphql.FieldConfigArgument{
			"input": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(registerContractInputType),
				Description: "Contract registration input",
			},
		},
		Resolve: s.resolveRegisterContract,
	}
	b.mutations["unregisterContract"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Boolean),
		Description: "Unregister a contract from dynamic event parsing",
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Contract address to unregister",
			},
		},
		Resolve: s.resolveUnregisterContract,
	}

	return b
}
