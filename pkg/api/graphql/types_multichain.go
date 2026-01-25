package graphql

import (
	"github.com/graphql-go/graphql"
)

var (
	// Chain status enum
	chainStatusEnumType *graphql.Enum

	// Chain type
	chainType *graphql.Object

	// SyncProgress type
	syncProgressType *graphql.Object

	// HealthStatus type
	healthStatusType *graphql.Object

	// BlockWithChain type for multi-chain subscriptions
	blockWithChainType *graphql.Object

	// ChainConnection type
	chainConnectionType *graphql.Object

	// Input types
	registerChainInputType *graphql.InputObject
)

func initMultiChainTypes() {
	// ChainStatus enum
	chainStatusEnumType = graphql.NewEnum(graphql.EnumConfig{
		Name:        "ChainStatus",
		Description: "Status of a chain instance",
		Values: graphql.EnumValueConfigMap{
			"REGISTERED": &graphql.EnumValueConfig{
				Value:       "registered",
				Description: "Chain is registered but not started",
			},
			"STARTING": &graphql.EnumValueConfig{
				Value:       "starting",
				Description: "Chain is starting up",
			},
			"SYNCING": &graphql.EnumValueConfig{
				Value:       "syncing",
				Description: "Chain is syncing blocks",
			},
			"ACTIVE": &graphql.EnumValueConfig{
				Value:       "active",
				Description: "Chain is fully synced and active",
			},
			"STOPPING": &graphql.EnumValueConfig{
				Value:       "stopping",
				Description: "Chain is stopping",
			},
			"STOPPED": &graphql.EnumValueConfig{
				Value:       "stopped",
				Description: "Chain is stopped",
			},
			"ERROR": &graphql.EnumValueConfig{
				Value:       "error",
				Description: "Chain encountered an error",
			},
		},
	})

	// SyncProgress type
	syncProgressType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "SyncProgress",
		Description: "Synchronization progress information",
		Fields: graphql.Fields{
			"currentBlock": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Current block height",
			},
			"targetBlock": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Target block height (latest on chain)",
			},
			"percentage": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Float),
				Description: "Sync completion percentage (0-100)",
			},
			"estimatedTimeRemaining": &graphql.Field{
				Type:        bigIntType,
				Description: "Estimated time remaining in seconds",
			},
			"blocksPerSecond": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Float),
				Description: "Current sync speed in blocks per second",
			},
			"isSynced": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether the chain is fully synced",
			},
		},
	})

	// HealthStatus type
	healthStatusType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "HealthStatus",
		Description: "Health status of a chain connection",
		Fields: graphql.Fields{
			"isHealthy": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether the chain connection is healthy",
			},
			"lastHeartbeat": &graphql.Field{
				Type:        graphql.String,
				Description: "Last successful heartbeat time (RFC3339)",
			},
			"latencyMs": &graphql.Field{
				Type:        bigIntType,
				Description: "RPC latency in milliseconds",
			},
			"consecutiveFailures": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Number of consecutive failures",
			},
			"lastError": &graphql.Field{
				Type:        graphql.String,
				Description: "Last error message if any",
			},
			"uptimePercentage": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Float),
				Description: "Uptime percentage (0-100)",
			},
		},
	})

	// Chain type
	chainType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "Chain",
		Description: "A registered blockchain network",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Unique chain identifier",
			},
			"name": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Human-readable chain name",
			},
			"chainId": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Numeric chain ID",
			},
			"rpcEndpoint": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "HTTP RPC endpoint URL",
			},
			"wsEndpoint": &graphql.Field{
				Type:        graphql.String,
				Description: "WebSocket RPC endpoint URL",
			},
			"adapterType": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Chain adapter type (evm, stableone, etc.)",
			},
			"status": &graphql.Field{
				Type:        graphql.NewNonNull(chainStatusEnumType),
				Description: "Current chain status",
			},
			"syncProgress": &graphql.Field{
				Type:        syncProgressType,
				Description: "Sync progress information",
			},
			"health": &graphql.Field{
				Type:        healthStatusType,
				Description: "Health status information",
			},
			"enabled": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether the chain is enabled",
			},
			"latestHeight": &graphql.Field{
				Type:        bigIntType,
				Description: "Latest indexed block height",
			},
			"startHeight": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Starting block height for indexing",
			},
			"registeredAt": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Registration timestamp (RFC3339)",
			},
		},
	})

	// BlockWithChain type for multi-chain subscriptions
	blockWithChainType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "BlockWithChain",
		Description: "Block with associated chain information",
		Fields: graphql.Fields{
			"chainId": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Chain identifier",
			},
			"block": &graphql.Field{
				Type:        graphql.NewNonNull(blockType),
				Description: "Block data",
			},
		},
	})

	// ChainConnection type for pagination
	chainConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "ChainConnection",
		Description: "Paginated chain list",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(chainType))),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// RegisterChainInput type
	registerChainInputType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        "RegisterChainInput",
		Description: "Input for registering a new chain",
		Fields: graphql.InputObjectConfigFieldMap{
			"name": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Human-readable chain name",
			},
			"rpcEndpoint": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "HTTP RPC endpoint URL",
			},
			"wsEndpoint": &graphql.InputObjectFieldConfig{
				Type:        graphql.String,
				Description: "WebSocket RPC endpoint URL (optional)",
			},
			"chainId": &graphql.InputObjectFieldConfig{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Numeric chain ID",
			},
			"adapterType": &graphql.InputObjectFieldConfig{
				Type:        graphql.String,
				Description: "Chain adapter type (auto-detected if not specified)",
			},
			"startHeight": &graphql.InputObjectFieldConfig{
				Type:        bigIntType,
				Description: "Starting block height (0 for genesis)",
			},
		},
	})
}
