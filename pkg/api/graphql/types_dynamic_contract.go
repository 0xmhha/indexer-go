package graphql

import "github.com/graphql-go/graphql"

// Dynamic contract types for registered contract event parsing

var (
	// RegisteredContract type
	registeredContractType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "RegisteredContract",
		Description: "A contract registered for dynamic event parsing",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Contract address",
			},
			"name": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Contract name",
			},
			"abi": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Contract ABI (JSON string)",
			},
			"registeredAt": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Registration timestamp",
			},
			"blockNumber": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block number when registered",
			},
			"isVerified": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether the contract is verified",
			},
			"events": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(graphql.String))),
				Description: "List of event names in the ABI",
			},
		},
	})

	// DynamicContractEvent type
	dynamicContractEventType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "DynamicContractEvent",
		Description: "An event parsed from a registered contract",
		Fields: graphql.Fields{
			"contract": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Contract address",
			},
			"contractName": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Contract name",
			},
			"eventName": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Event name",
			},
			"blockNumber": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block number",
			},
			"txHash": &graphql.Field{
				Type:        graphql.NewNonNull(hashType),
				Description: "Transaction hash",
			},
			"logIndex": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Log index in the transaction",
			},
			"data": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Event data as JSON string",
			},
			"timestamp": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Timestamp",
			},
		},
	})

	// RegisterContractInput type
	registerContractInputType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        "RegisterContractInput",
		Description: "Input for registering a contract for dynamic event parsing",
		Fields: graphql.InputObjectConfigFieldMap{
			"address": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Contract address",
			},
			"name": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Human-readable contract name",
			},
			"abi": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Contract ABI JSON string",
			},
			"blockNumber": &graphql.InputObjectFieldConfig{
				Type:        bigIntType,
				Description: "Block number to start parsing from",
			},
		},
	})

	// DynamicContractEventFilter input type
	dynamicContractEventFilterType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        "DynamicContractEventFilter",
		Description: "Filter for querying dynamic contract events",
		Fields: graphql.InputObjectConfigFieldMap{
			"contract": &graphql.InputObjectFieldConfig{
				Type:        addressType,
				Description: "Filter by contract address",
			},
			"eventNames": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewList(graphql.NewNonNull(graphql.String)),
				Description: "Filter by event names",
			},
			"fromBlock": &graphql.InputObjectFieldConfig{
				Type:        bigIntType,
				Description: "Filter by block range start",
			},
			"toBlock": &graphql.InputObjectFieldConfig{
				Type:        bigIntType,
				Description: "Filter by block range end",
			},
		},
	})

	// DynamicContractSubscriptionFilter input type
	dynamicContractSubscriptionFilterType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        "DynamicContractSubscriptionFilter",
		Description: "Filter for dynamic contract event subscriptions",
		Fields: graphql.InputObjectConfigFieldMap{
			"contract": &graphql.InputObjectFieldConfig{
				Type:        addressType,
				Description: "Filter by contract address",
			},
			"eventNames": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewList(graphql.NewNonNull(graphql.String)),
				Description: "Filter by event names",
			},
		},
	})
)
