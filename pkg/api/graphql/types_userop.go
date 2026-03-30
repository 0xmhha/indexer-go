package graphql

import "github.com/graphql-go/graphql"

// initUserOpTypes initializes EIP-4337 Account Abstraction types
func initUserOpTypes() {
	userOperationType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "UserOperation",
		Description: "EIP-4337 UserOperation event record",
		Fields: graphql.Fields{
			"userOpHash": &graphql.Field{
				Type:        graphql.NewNonNull(hashType),
				Description: "UserOperation hash",
			},
			"txHash": &graphql.Field{
				Type:        graphql.NewNonNull(hashType),
				Description: "Transaction hash containing this UserOperation",
			},
			"blockNumber": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block number",
			},
			"blockHash": &graphql.Field{
				Type:        graphql.NewNonNull(hashType),
				Description: "Block hash",
			},
			"txIndex": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Transaction index in block",
			},
			"logIndex": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Log index in transaction",
			},
			"sender": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Sender (smart account) address",
			},
			"paymaster": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Paymaster address (zero address if no paymaster)",
			},
			"nonce": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "UserOperation nonce",
			},
			"success": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether the UserOperation executed successfully",
			},
			"actualGasCost": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Actual gas cost paid",
			},
			"actualUserOpFeePerGas": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Actual fee per gas for the UserOperation",
			},
			"bundler": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Bundler address (tx.from of handleOps transaction)",
			},
			"entryPoint": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "EntryPoint contract address that emitted the event",
			},
			"timestamp": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block timestamp",
			},
		},
	})

	userOperationConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "UserOperationConnection",
		Description: "Paginated list of UserOperations",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(userOperationType))),
				Description: "List of UserOperations",
			},
			"totalCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Total count of matching UserOperations",
			},
			"pageInfo": &graphql.Field{
				Type:        graphql.NewNonNull(pageInfoType),
				Description: "Pagination information",
			},
		},
	})

	accountDeployedType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "AccountDeployed",
		Description: "EIP-4337 account deployment event",
		Fields: graphql.Fields{
			"userOpHash": &graphql.Field{
				Type:        graphql.NewNonNull(hashType),
				Description: "UserOperation hash that triggered the deployment",
			},
			"sender": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Deployed account address",
			},
			"factory": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Factory contract that deployed the account",
			},
			"paymaster": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Paymaster address",
			},
			"txHash": &graphql.Field{
				Type:        graphql.NewNonNull(hashType),
				Description: "Transaction hash",
			},
			"blockNumber": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block number",
			},
			"logIndex": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Log index",
			},
			"timestamp": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block timestamp",
			},
		},
	})

	userOpRevertType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "UserOpRevert",
		Description: "UserOperation revert reason",
		Fields: graphql.Fields{
			"userOpHash": &graphql.Field{
				Type:        graphql.NewNonNull(hashType),
				Description: "UserOperation hash",
			},
			"sender": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Sender address",
			},
			"nonce": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "UserOperation nonce",
			},
			"revertReason": &graphql.Field{
				Type:        graphql.NewNonNull(bytesType),
				Description: "Revert reason bytes",
			},
			"txHash": &graphql.Field{
				Type:        graphql.NewNonNull(hashType),
				Description: "Transaction hash",
			},
			"blockNumber": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block number",
			},
			"logIndex": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Log index",
			},
			"revertType": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Revert type: 'execution' or 'postop'",
			},
			"timestamp": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block timestamp",
			},
		},
	})

	bundlerStatsType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "BundlerStats",
		Description: "Aggregated statistics for a bundler address",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Bundler address",
			},
			"totalOps": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Total UserOperations bundled",
			},
			"successfulOps": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Successfully executed UserOperations",
			},
			"failedOps": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Failed UserOperations",
			},
			"totalGasSponsored": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total gas cost across all operations",
			},
			"lastActivityBlock": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Last block with activity",
			},
			"lastActivityTime": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Last activity timestamp",
			},
		},
	})

	paymasterStatsType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "PaymasterStats",
		Description: "Aggregated statistics for a paymaster address",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Paymaster address",
			},
			"totalOps": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Total UserOperations sponsored",
			},
			"successfulOps": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Successfully executed UserOperations",
			},
			"failedOps": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Failed UserOperations",
			},
			"totalGasSponsored": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total gas sponsored",
			},
			"lastActivityBlock": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Last block with activity",
			},
			"lastActivityTime": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Last activity timestamp",
			},
		},
	})

	// Connection types for paginated list queries
	bundlerStatsConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "BundlerStatsConnection",
		Description: "Paginated list of bundler statistics",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(bundlerStatsType))),
				Description: "List of bundler statistics",
			},
			"totalCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Total count of known bundlers",
			},
			"pageInfo": &graphql.Field{
				Type:        graphql.NewNonNull(pageInfoType),
				Description: "Pagination information",
			},
		},
	})

	paymasterStatsConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "PaymasterStatsConnection",
		Description: "Paginated list of paymaster statistics",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(paymasterStatsType))),
				Description: "List of paymaster statistics",
			},
			"totalCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Total count of known paymasters",
			},
			"pageInfo": &graphql.Field{
				Type:        graphql.NewNonNull(pageInfoType),
				Description: "Pagination information",
			},
		},
	})
}
