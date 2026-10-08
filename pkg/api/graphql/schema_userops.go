package graphql

import (
	"github.com/graphql-go/graphql"
)

// WithUserOpQueries adds ERC-4337 Account Abstraction queries
func (b *SchemaBuilder) WithUserOpQueries() *SchemaBuilder {
	s := b.schema

	// UserOperation type
	userOperationType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "UserOperation",
		Description: "ERC-4337 UserOperation included in a bundle",
		Fields: graphql.Fields{
			"hash":                 &graphql.Field{Type: graphql.NewNonNull(hashType)},
			"sender":               &graphql.Field{Type: graphql.NewNonNull(addressType)},
			"nonce":                &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
			"callData":             &graphql.Field{Type: graphql.NewNonNull(bytesType)},
			"callGasLimit":         &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
			"verificationGasLimit": &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
			"preVerificationGas":   &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
			"maxFeePerGas":         &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
			"maxPriorityFeePerGas": &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
			"signature":            &graphql.Field{Type: graphql.NewNonNull(bytesType)},
			"entryPoint":           &graphql.Field{Type: graphql.NewNonNull(addressType)},
			"entryPointVersion":    &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
			"transactionHash":      &graphql.Field{Type: graphql.NewNonNull(hashType)},
			"blockNumber":          &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
			"blockHash":            &graphql.Field{Type: graphql.NewNonNull(hashType)},
			"bundleIndex":          &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
			"bundler":              &graphql.Field{Type: graphql.NewNonNull(addressType)},
			"factory":              &graphql.Field{Type: addressType},
			"paymaster":            &graphql.Field{Type: addressType},
			"status":               &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean)},
			"revertReason":         &graphql.Field{Type: bytesType},
			"gasUsed":              &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
			"actualGasCost":        &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
			"sponsorType":          &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
			"userLogsStartIndex":   &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
			"userLogsCount":        &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
			"timestamp":            &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
		},
	})

	// UserOperationConnection type
	userOperationConnectionType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "UserOperationConnection",
		Description: "Paginated list of UserOperations",
		Fields: graphql.Fields{
			"nodes":      &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(userOperationType)))},
			"totalCount": &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
			"pageInfo":   &graphql.Field{Type: graphql.NewNonNull(pageInfoType)},
		},
	})

	// BundlerStats type
	bundlerStatsType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "BundlerStats",
		Description: "Bundler statistics",
		Fields: graphql.Fields{
			"address":      &graphql.Field{Type: graphql.NewNonNull(addressType)},
			"totalBundles": &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
			"totalOps":     &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
		},
	})

	bundlerStatsConnectionType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "BundlerStatsConnection",
		Description: "Paginated list of bundler stats",
		Fields: graphql.Fields{
			"nodes":      &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(bundlerStatsType)))},
			"totalCount": &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
			"pageInfo":   &graphql.Field{Type: graphql.NewNonNull(pageInfoType)},
		},
	})

	// FactoryStats type
	factoryStatsType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "FactoryStats",
		Description: "Factory statistics",
		Fields: graphql.Fields{
			"address":       &graphql.Field{Type: graphql.NewNonNull(addressType)},
			"totalAccounts": &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
		},
	})

	factoryStatsConnectionType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "FactoryStatsConnection",
		Description: "Paginated list of factory stats",
		Fields: graphql.Fields{
			"nodes":      &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(factoryStatsType)))},
			"totalCount": &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
			"pageInfo":   &graphql.Field{Type: graphql.NewNonNull(pageInfoType)},
		},
	})

	// PaymasterStats type
	paymasterStatsType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "PaymasterStats",
		Description: "Paymaster statistics",
		Fields: graphql.Fields{
			"address":  &graphql.Field{Type: graphql.NewNonNull(addressType)},
			"totalOps": &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
		},
	})

	paymasterStatsConnectionType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "PaymasterStatsConnection",
		Description: "Paginated list of paymaster stats",
		Fields: graphql.Fields{
			"nodes":      &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(paymasterStatsType)))},
			"totalCount": &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
			"pageInfo":   &graphql.Field{Type: graphql.NewNonNull(pageInfoType)},
		},
	})

	// SmartAccount type
	smartAccountType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "SmartAccount",
		Description: "ERC-4337 smart contract account",
		Fields: graphql.Fields{
			"address":           &graphql.Field{Type: graphql.NewNonNull(addressType)},
			"creationOpHash":    &graphql.Field{Type: hashType},
			"creationTxHash":    &graphql.Field{Type: hashType},
			"creationTimestamp": &graphql.Field{Type: bigIntType},
			"factory":           &graphql.Field{Type: addressType},
			"totalOps":          &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
		},
	})

	smartAccountConnectionType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "SmartAccountConnection",
		Description: "Paginated list of smart accounts",
		Fields: graphql.Fields{
			"nodes":      &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(smartAccountType)))},
			"totalCount": &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
			"pageInfo":   &graphql.Field{Type: graphql.NewNonNull(pageInfoType)},
		},
	})

	// Query: userOperation(hash)
	b.queries["userOperation"] = &graphql.Field{
		Type:        userOperationType,
		Description: "Get a specific UserOperation by hash",
		Args: graphql.FieldConfigArgument{
			"hash": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "UserOperation hash",
			},
		},
		Resolve: s.resolveUserOperation,
	}

	// Query: userOperations(pagination, sender)
	b.queries["userOperations"] = &graphql.Field{
		Type:        graphql.NewNonNull(userOperationConnectionType),
		Description: "Get UserOperations with optional sender filter",
		Args: graphql.FieldConfigArgument{
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
			"sender": &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "Filter by sender address",
			},
		},
		Resolve: s.resolveUserOperations,
	}

	// Query: userOperationsBySender(sender, pagination)
	b.queries["userOperationsBySender"] = &graphql.Field{
		Type:        graphql.NewNonNull(userOperationConnectionType),
		Description: "Get UserOperations by sender address",
		Args: graphql.FieldConfigArgument{
			"sender": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Sender address",
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveUserOperationsByAddress,
	}

	// Query: bundlers(pagination)
	b.queries["bundlers"] = &graphql.Field{
		Type:        graphql.NewNonNull(bundlerStatsConnectionType),
		Description: "Get bundler list with pagination",
		Args: graphql.FieldConfigArgument{
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveBundlers,
	}

	// Query: bundler(address)
	b.queries["bundler"] = &graphql.Field{
		Type:        bundlerStatsType,
		Description: "Get a specific bundler's stats",
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Bundler address",
			},
		},
		Resolve: s.resolveBundler,
	}

	// Query: factories(pagination)
	b.queries["factories"] = &graphql.Field{
		Type:        graphql.NewNonNull(factoryStatsConnectionType),
		Description: "Get factory list with pagination",
		Args: graphql.FieldConfigArgument{
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveFactories,
	}

	// Query: factory(address)
	b.queries["factory"] = &graphql.Field{
		Type:        factoryStatsType,
		Description: "Get a specific factory's stats",
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Factory address",
			},
		},
		Resolve: s.resolveFactory,
	}

	// Query: paymasters(pagination)
	b.queries["paymasters"] = &graphql.Field{
		Type:        graphql.NewNonNull(paymasterStatsConnectionType),
		Description: "Get paymaster list with pagination",
		Args: graphql.FieldConfigArgument{
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolvePaymasters,
	}

	// Query: paymaster(address)
	b.queries["paymaster"] = &graphql.Field{
		Type:        paymasterStatsType,
		Description: "Get a specific paymaster's stats",
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Paymaster address",
			},
		},
		Resolve: s.resolvePaymaster,
	}

	// Query: smartAccounts(pagination)
	b.queries["smartAccounts"] = &graphql.Field{
		Type:        graphql.NewNonNull(smartAccountConnectionType),
		Description: "Get smart account list with pagination",
		Args: graphql.FieldConfigArgument{
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveSmartAccounts,
	}

	// Query: smartAccount(address)
	b.queries["smartAccount"] = &graphql.Field{
		Type:        smartAccountType,
		Description: "Get a specific smart account",
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Smart account address",
			},
		},
		Resolve: s.resolveSmartAccount,
	}

	// Query: userOperationCount
	b.queries["userOperationCount"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Int),
		Description: "Get total UserOperation count",
		Resolve:     s.resolveUserOpCount,
	}

	return b
}
