package graphql

import (
	"github.com/graphql-go/graphql"
)

// WithMutations adds GraphQL mutations
func (b *SchemaBuilder) WithMutations() *SchemaBuilder {
	s := b.schema

	b.mutations["verifyContract"] = &graphql.Field{
		Type: graphql.NewNonNull(contractVerificationType),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Contract address to verify",
			},
			"sourceCode": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Solidity source code",
			},
			"compilerVersion": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Solidity compiler version (e.g., 0.8.20)",
			},
			"optimizationEnabled": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether optimization was enabled",
			},
			"optimizationRuns": &graphql.ArgumentConfig{
				Type:        graphql.Int,
				Description: "Number of optimization runs (default: 200)",
			},
			"constructorArguments": &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "Constructor arguments (hex encoded)",
			},
			"contractName": &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "Contract name (required for multiple contracts)",
			},
			"licenseType": &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "License type (e.g., MIT, Apache-2.0)",
			},
		},
		Description: "Verify a contract's source code",
		Resolve:     s.resolveVerifyContract,
	}

	return b
}
