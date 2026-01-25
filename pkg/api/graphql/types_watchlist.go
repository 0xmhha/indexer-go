package graphql

import (
	"github.com/graphql-go/graphql"
)

var (
	// WatchEventType enum
	watchEventTypeEnumType *graphql.Enum

	// WatchFilter type
	watchFilterType *graphql.Object

	// WatchStats type
	watchStatsType *graphql.Object

	// WatchedAddress type
	watchedAddressType *graphql.Object

	// WatchEvent type
	watchEventType *graphql.Object

	// Connection types
	watchedAddressConnectionType *graphql.Object
	watchEventConnectionType     *graphql.Object

	// Input types
	watchAddressInputType  *graphql.InputObject
	watchFilterInputType   *graphql.InputObject
)

func initWatchlistTypes() {
	// WatchEventType enum
	watchEventTypeEnumType = graphql.NewEnum(graphql.EnumConfig{
		Name:        "WatchEventType",
		Description: "Type of watch event",
		Values: graphql.EnumValueConfigMap{
			"TX_FROM": &graphql.EnumValueConfig{
				Value:       "tx_from",
				Description: "Transaction where watched address is sender",
			},
			"TX_TO": &graphql.EnumValueConfig{
				Value:       "tx_to",
				Description: "Transaction where watched address is recipient",
			},
			"ERC20_TRANSFER": &graphql.EnumValueConfig{
				Value:       "erc20",
				Description: "ERC20 token transfer involving watched address",
			},
			"ERC721_TRANSFER": &graphql.EnumValueConfig{
				Value:       "erc721",
				Description: "ERC721 token transfer involving watched address",
			},
			"LOG": &graphql.EnumValueConfig{
				Value:       "log",
				Description: "Log emitted by watched address (contract)",
			},
		},
	})

	// WatchFilter type
	watchFilterType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "WatchFilter",
		Description: "Filter configuration for watched address events",
		Fields: graphql.Fields{
			"txFrom": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Monitor transactions where address is sender",
			},
			"txTo": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Monitor transactions where address is recipient",
			},
			"erc20": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Monitor ERC20 token transfers",
			},
			"erc721": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Monitor ERC721 token transfers",
			},
			"logs": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Monitor all logs emitted by address",
			},
			"minValue": &graphql.Field{
				Type:        bigIntType,
				Description: "Minimum transaction value filter (wei)",
			},
		},
	})

	// WatchStats type
	watchStatsType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "WatchStats",
		Description: "Statistics for a watched address",
		Fields: graphql.Fields{
			"totalEvents": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Total number of events detected",
			},
			"txFromCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Number of outgoing transactions",
			},
			"txToCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Number of incoming transactions",
			},
			"erc20Count": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Number of ERC20 transfer events",
			},
			"erc721Count": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Number of ERC721 transfer events",
			},
			"logCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Number of log events",
			},
			"lastEventAt": &graphql.Field{
				Type:        graphql.String,
				Description: "Timestamp of last event (RFC3339)",
			},
		},
	})

	// WatchedAddress type
	watchedAddressType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "WatchedAddress",
		Description: "A monitored blockchain address",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Unique identifier",
			},
			"address": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Ethereum address",
			},
			"chainId": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Chain identifier",
			},
			"label": &graphql.Field{
				Type:        graphql.String,
				Description: "User-defined label",
			},
			"filter": &graphql.Field{
				Type:        graphql.NewNonNull(watchFilterType),
				Description: "Event filter configuration",
			},
			"createdAt": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Creation timestamp (RFC3339)",
			},
			"stats": &graphql.Field{
				Type:        watchStatsType,
				Description: "Event statistics",
			},
			"recentEvents": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(watchEventType))),
				Description: "Recent events for this address",
				Args: graphql.FieldConfigArgument{
					"limit": &graphql.ArgumentConfig{
						Type:         graphql.Int,
						DefaultValue: 10,
						Description:  "Maximum number of events to return",
					},
				},
			},
		},
	})

	// WatchEvent type
	watchEventType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "WatchEvent",
		Description: "An event detected for a watched address",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Unique event identifier",
			},
			"addressId": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Watched address ID",
			},
			"chainId": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Chain identifier",
			},
			"eventType": &graphql.Field{
				Type:        graphql.NewNonNull(watchEventTypeEnumType),
				Description: "Type of event",
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
				Type:        graphql.Int,
				Description: "Log index (for log events)",
			},
			"data": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Event-specific data (JSON)",
			},
			"timestamp": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Event timestamp (RFC3339)",
			},
		},
	})

	// WatchedAddressConnection type
	watchedAddressConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "WatchedAddressConnection",
		Description: "Paginated watched address list",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(watchedAddressType))),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// WatchEventConnection type
	watchEventConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "WatchEventConnection",
		Description: "Paginated watch event list",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(watchEventType))),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// WatchFilterInput type
	watchFilterInputType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        "WatchFilterInput",
		Description: "Input for watch filter configuration",
		Fields: graphql.InputObjectConfigFieldMap{
			"txFrom": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Monitor transactions where address is sender",
			},
			"txTo": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Monitor transactions where address is recipient",
			},
			"erc20": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Monitor ERC20 token transfers",
			},
			"erc721": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Monitor ERC721 token transfers",
			},
			"logs": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Monitor all logs emitted by address",
			},
			"minValue": &graphql.InputObjectFieldConfig{
				Type:        bigIntType,
				Description: "Minimum transaction value filter (wei)",
			},
		},
	})

	// WatchAddressInput type
	watchAddressInputType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        "WatchAddressInput",
		Description: "Input for watching an address",
		Fields: graphql.InputObjectConfigFieldMap{
			"address": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Ethereum address to watch",
			},
			"chainId": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Chain identifier",
			},
			"label": &graphql.InputObjectFieldConfig{
				Type:        graphql.String,
				Description: "User-defined label",
			},
			"filter": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewNonNull(watchFilterInputType),
				Description: "Event filter configuration",
			},
		},
	})
}
