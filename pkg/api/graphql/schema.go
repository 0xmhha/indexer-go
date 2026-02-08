package graphql

import (
	"context"
	"fmt"

	abiDecoder "github.com/0xmhha/indexer-go/pkg/abi"
	"github.com/0xmhha/indexer-go/pkg/multichain"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/rpcproxy"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/verifier"
	"github.com/0xmhha/indexer-go/pkg/watchlist"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// Schema holds the GraphQL schema
type Schema struct {
	schema     graphql.Schema
	storage    storage.Storage
	logger     *zap.Logger
	abiDecoder *abiDecoder.Decoder
	verifier   verifier.Verifier
	rpcProxy   *rpcproxy.Proxy

	// Multi-chain and watchlist services
	chainManager     *multichain.Manager
	watchlistService watchlist.Service

	// Notification service
	notificationService notifications.Service
}

// SchemaBuilder helps construct a GraphQL schema using the Builder pattern
type SchemaBuilder struct {
	schema        *Schema
	queries       graphql.Fields
	mutations     graphql.Fields
	subscriptions graphql.Fields
}

// NewSchemaBuilder creates a new schema builder
func NewSchemaBuilder(store storage.Storage, logger *zap.Logger) *SchemaBuilder {
	return &SchemaBuilder{
		schema: &Schema{
			storage:    store,
			logger:     logger,
			abiDecoder: abiDecoder.NewDecoder(),
		},
		queries:       make(graphql.Fields),
		mutations:     make(graphql.Fields),
		subscriptions: make(graphql.Fields),
	}
}

// WithCoreQueries adds core blockchain queries (block, transaction, receipt, logs)
func (b *SchemaBuilder) WithCoreQueries() *SchemaBuilder {
	s := b.schema

	b.queries["latestHeight"] = &graphql.Field{
		Type:    graphql.NewNonNull(bigIntType),
		Resolve: s.resolveLatestHeight,
	}
	b.queries["block"] = &graphql.Field{
		Type: blockType,
		Args: graphql.FieldConfigArgument{
			"number": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveBlock,
	}
	b.queries["blockByHash"] = &graphql.Field{
		Type: blockType,
		Args: graphql.FieldConfigArgument{
			"hash": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(hashType),
			},
		},
		Resolve: s.resolveBlockByHash,
	}
	b.queries["blocks"] = &graphql.Field{
		Type: graphql.NewNonNull(blockConnectionType),
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type: blockFilterType,
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveBlocks,
	}
	b.queries["blocksRange"] = &graphql.Field{
		Type:        graphql.NewNonNull(blockRangeResultType),
		Description: "Get blocks in a specific range (optimized for frontend catch-up). Maximum 100 blocks per request.",
		Args: graphql.FieldConfigArgument{
			"startNumber": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Starting block number (inclusive)",
			},
			"endNumber": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Ending block number (inclusive)",
			},
			"includeTransactions": &graphql.ArgumentConfig{
				Type:         graphql.Boolean,
				Description:  "Include transaction data in response (default: true)",
				DefaultValue: true,
			},
			"includeReceipts": &graphql.ArgumentConfig{
				Type:         graphql.Boolean,
				Description:  "Include receipt data for each transaction (default: false)",
				DefaultValue: false,
			},
		},
		Resolve: s.resolveBlocksRange,
	}
	b.queries["transaction"] = &graphql.Field{
		Type: transactionType,
		Args: graphql.FieldConfigArgument{
			"hash": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(hashType),
			},
		},
		Resolve: s.resolveTransaction,
	}
	b.queries["transactions"] = &graphql.Field{
		Type: graphql.NewNonNull(transactionConnectionType),
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type: transactionFilterType,
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveTransactions,
	}
	b.queries["transactionsByAddress"] = &graphql.Field{
		Type: graphql.NewNonNull(transactionConnectionType),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveTransactionsByAddress,
	}
	b.queries["receipt"] = &graphql.Field{
		Type: receiptType,
		Args: graphql.FieldConfigArgument{
			"transactionHash": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(hashType),
			},
		},
		Resolve: s.resolveReceipt,
	}
	b.queries["receiptsByBlock"] = &graphql.Field{
		Type: graphql.NewList(graphql.NewNonNull(receiptType)),
		Args: graphql.FieldConfigArgument{
			"blockNumber": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveReceiptsByBlock,
	}
	b.queries["logs"] = &graphql.Field{
		Type: graphql.NewNonNull(logConnectionType),
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(logFilterType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
			"decode": &graphql.ArgumentConfig{
				Type:        graphql.Boolean,
				Description: "Decode logs using ABI if available",
			},
		},
		Resolve: s.resolveLogs,
	}
	b.queries["blockCount"] = &graphql.Field{
		Type:    graphql.NewNonNull(bigIntType),
		Resolve: s.resolveBlockCount,
	}
	b.queries["transactionCount"] = &graphql.Field{
		Type:    graphql.NewNonNull(bigIntType),
		Resolve: s.resolveTransactionCount,
	}

	return b
}

// WithHistoricalQueries adds historical data queries
func (b *SchemaBuilder) WithHistoricalQueries() *SchemaBuilder {
	s := b.schema

	b.queries["blocksByTimeRange"] = &graphql.Field{
		Type: graphql.NewNonNull(blockConnectionType),
		Args: graphql.FieldConfigArgument{
			"fromTime": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toTime": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveBlocksByTimeRange,
	}
	b.queries["blockByTimestamp"] = &graphql.Field{
		Type: blockType,
		Args: graphql.FieldConfigArgument{
			"timestamp": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveBlockByTimestamp,
	}
	b.queries["transactionsByAddressFiltered"] = &graphql.Field{
		Type: graphql.NewNonNull(transactionConnectionType),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"filter": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(historicalTransactionFilterType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveTransactionsByAddressFiltered,
	}
	b.queries["addressBalance"] = &graphql.Field{
		Type: graphql.NewNonNull(bigIntType),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"blockNumber": &graphql.ArgumentConfig{
				Type: bigIntType,
			},
		},
		Resolve: s.resolveAddressBalance,
	}
	b.queries["balanceHistory"] = &graphql.Field{
		Type: graphql.NewNonNull(balanceHistoryConnectionType),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveBalanceHistory,
	}

	return b
}

// WithAnalyticsQueries adds analytics and statistics queries
func (b *SchemaBuilder) WithAnalyticsQueries() *SchemaBuilder {
	s := b.schema

	b.queries["topMiners"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(minerStatsType))),
		Args: graphql.FieldConfigArgument{
			"limit": &graphql.ArgumentConfig{
				Type:        graphql.Int,
				Description: "Maximum number of miners to return (max: 100, default: 10)",
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type:        bigIntType,
				Description: "Start block number (0 = genesis)",
			},
			"toBlock": &graphql.ArgumentConfig{
				Type:        bigIntType,
				Description: "End block number (0 = latest)",
			},
		},
		Description: "Get top miners by block count in a given block range",
		Resolve:     s.resolveTopMiners,
	}
	b.queries["tokenBalances"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(tokenBalanceType))),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Address to query token balances for",
			},
			"tokenType": &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "Filter by token type (ERC20, ERC721, ERC1155)",
			},
		},
		Description: "Get token balances for an address",
		Resolve:     s.resolveTokenBalances,
	}
	b.queries["search"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(searchResultType))),
		Args: graphql.FieldConfigArgument{
			"query": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Search query (block number, hash, or address)",
			},
			"types": &graphql.ArgumentConfig{
				Type:        graphql.NewList(graphql.String),
				Description: "Optional filter for result types (block, transaction, address, contract)",
			},
			"limit": &graphql.ArgumentConfig{
				Type:         graphql.Int,
				DefaultValue: 10,
				Description:  "Maximum number of results to return",
			},
		},
		Description: "Unified search across blocks, transactions, and addresses",
		Resolve:     s.resolveSearch,
	}
	b.queries["contractVerification"] = &graphql.Field{
		Type: contractVerificationType,
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Contract address to get verification data for",
			},
		},
		Description: "Get contract verification data",
		Resolve:     s.resolveContractVerification,
	}
	b.queries["gasStats"] = &graphql.Field{
		Type: gasStatsType,
		Args: graphql.FieldConfigArgument{
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Description: "Get gas usage statistics for a block range",
		Resolve:     s.resolveGasStats,
	}
	b.queries["addressGasStats"] = &graphql.Field{
		Type: addressGasStatsType,
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Description: "Get gas usage statistics for a specific address",
		Resolve:     s.resolveAddressGasStats,
	}
	b.queries["topAddressesByGasUsed"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressGasStatsType))),
		Args: graphql.FieldConfigArgument{
			"limit": &graphql.ArgumentConfig{
				Type: graphql.Int,
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Description: "Get top addresses by total gas used",
		Resolve:     s.resolveTopAddressesByGasUsed,
	}
	b.queries["topAddressesByTxCount"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressActivityStatsType))),
		Args: graphql.FieldConfigArgument{
			"limit": &graphql.ArgumentConfig{
				Type: graphql.Int,
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Description: "Get top addresses by transaction count",
		Resolve:     s.resolveTopAddressesByTxCount,
	}
	b.queries["networkMetrics"] = &graphql.Field{
		Type: networkMetricsType,
		Args: graphql.FieldConfigArgument{
			"fromTime": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toTime": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Description: "Get network activity metrics for a time range",
		Resolve:     s.resolveNetworkMetrics,
	}
	b.queries["addressStats"] = &graphql.Field{
		Type: graphql.NewNonNull(addressStatsType),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
		},
		Description: "Get aggregated statistics for an address",
		Resolve:     s.resolveAddressStats,
	}

	return b
}

// WithSystemContractQueries adds system contract related queries
func (b *SchemaBuilder) WithSystemContractQueries() *SchemaBuilder {
	s := b.schema

	b.queries["totalSupply"] = &graphql.Field{
		Type:    graphql.NewNonNull(bigIntType),
		Resolve: s.resolveTotalSupply,
	}
	b.queries["activeMinters"] = &graphql.Field{
		Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(minterInfoType))),
		Resolve: s.resolveActiveMinters,
	}
	b.queries["activeMinterAddresses"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
		Description: "Returns only the addresses of active minters (simplified version of activeMinters)",
		Resolve:     s.resolveActiveMinterAddresses,
	}
	b.queries["minterAllowance"] = &graphql.Field{
		Type: graphql.NewNonNull(bigIntType),
		Args: graphql.FieldConfigArgument{
			"minter": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
		},
		Resolve: s.resolveMinterAllowance,
	}
	b.queries["activeValidators"] = &graphql.Field{
		Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(validatorInfoType))),
		Resolve: s.resolveActiveValidators,
	}
	b.queries["activeValidatorAddresses"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
		Description: "Returns only the addresses of active validators (simplified version of activeValidators)",
		Resolve:     s.resolveActiveValidatorAddresses,
	}
	b.queries["blacklistedAddresses"] = &graphql.Field{
		Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
		Resolve: s.resolveBlacklistedAddresses,
	}
	b.queries["proposals"] = &graphql.Field{
		Type: graphql.NewNonNull(proposalConnectionType),
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type:        proposalFilterType,
				Description: "Optional filter criteria. If not provided, returns all proposals.",
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveProposals,
	}
	b.queries["proposal"] = &graphql.Field{
		Type: proposalType,
		Args: graphql.FieldConfigArgument{
			"contract": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"proposalId": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveProposal,
	}
	b.queries["proposalVotes"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(proposalVoteType))),
		Args: graphql.FieldConfigArgument{
			"contract": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"proposalId": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveProposalVotes,
	}
	b.queries["mintEvents"] = &graphql.Field{
		Type: graphql.NewNonNull(mintEventConnectionType),
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(systemContractEventFilterType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveMintEvents,
	}
	b.queries["burnEvents"] = &graphql.Field{
		Type: graphql.NewNonNull(burnEventConnectionType),
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(systemContractEventFilterType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveBurnEvents,
	}
	b.queries["minterHistory"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(minterConfigEventType))),
		Args: graphql.FieldConfigArgument{
			"minter": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
		},
		Resolve: s.resolveMinterHistory,
	}
	b.queries["validatorHistory"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(validatorChangeEventType))),
		Args: graphql.FieldConfigArgument{
			"validator": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
		},
		Resolve: s.resolveValidatorHistory,
	}
	b.queries["gasTipHistory"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(gasTipUpdateEventType))),
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(systemContractEventFilterType),
			},
		},
		Resolve: s.resolveGasTipHistory,
	}
	b.queries["blacklistHistory"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(blacklistEventType))),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
		},
		Resolve: s.resolveBlacklistHistory,
	}
	b.queries["memberHistory"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(memberChangeEventType))),
		Args: graphql.FieldConfigArgument{
			"contract": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
		},
		Resolve: s.resolveMemberHistory,
	}
	b.queries["emergencyPauseHistory"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(emergencyPauseEventType))),
		Args: graphql.FieldConfigArgument{
			"contract": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
		},
		Resolve: s.resolveEmergencyPauseHistory,
	}
	b.queries["depositMintProposals"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(depositMintProposalType))),
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(systemContractEventFilterType),
			},
		},
		Resolve: s.resolveDepositMintProposals,
	}

	// Phase 2.3: Add missing system contract queries
	b.queries["minterConfigHistory"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(minterConfigEventType))),
		Description: "Returns minter configuration change history across all minters in a block range",
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(systemContractEventFilterType),
			},
		},
		Resolve: s.resolveMinterConfigHistory,
	}
	b.queries["burnHistory"] = &graphql.Field{
		Type:        graphql.NewNonNull(burnEventConnectionType),
		Description: "Alias for burnEvents - returns token burn history",
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(systemContractEventFilterType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveBurnEvents, // Reuse existing resolver
	}
	b.queries["authorizedAccounts"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
		Description: "Returns list of authorized accounts from GovCouncil contract",
		Resolve:     s.resolveAuthorizedAccounts,
	}

	return b
}

// WithConsensusQueries adds WBFT consensus related queries
func (b *SchemaBuilder) WithConsensusQueries() *SchemaBuilder {
	s := b.schema

	b.queries["wbftBlockExtra"] = &graphql.Field{
		Type: wbftBlockExtraType,
		Args: graphql.FieldConfigArgument{
			"blockNumber": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveWBFTBlockExtra,
	}
	// Alias for frontend compatibility
	b.queries["wbftBlock"] = &graphql.Field{
		Type:        wbftBlockExtraType,
		Description: "Alias for wbftBlockExtra - returns WBFT block consensus data",
		Args: graphql.FieldConfigArgument{
			"number": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			// Map 'number' argument to 'blockNumber' for the resolver
			p.Args["blockNumber"] = p.Args["number"]
			return s.resolveWBFTBlockExtra(p)
		},
	}
	b.queries["wbftBlockExtraByHash"] = &graphql.Field{
		Type: wbftBlockExtraType,
		Args: graphql.FieldConfigArgument{
			"blockHash": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(hashType),
			},
		},
		Resolve: s.resolveWBFTBlockExtraByHash,
	}
	b.queries["epochInfo"] = &graphql.Field{
		Type: epochInfoType,
		Args: graphql.FieldConfigArgument{
			"epochNumber": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveEpochInfo,
	}
	// Alias for frontend compatibility
	b.queries["epochByNumber"] = &graphql.Field{
		Type:        epochInfoType,
		Description: "Alias for epochInfo - returns epoch information by epoch number",
		Args: graphql.FieldConfigArgument{
			"number": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			// Map 'number' argument to 'epochNumber' for the resolver
			p.Args["epochNumber"] = p.Args["number"]
			return s.resolveEpochInfo(p)
		},
	}
	b.queries["latestEpochInfo"] = &graphql.Field{
		Type:    epochInfoType,
		Resolve: s.resolveLatestEpochInfo,
	}
	b.queries["epochs"] = &graphql.Field{
		Type:        graphql.NewNonNull(epochSummaryConnectionType),
		Description: "Get paginated list of epochs (latest first)",
		Args: graphql.FieldConfigArgument{
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveEpochs,
	}
	b.queries["validatorSigningStats"] = &graphql.Field{
		Type: validatorSigningStatsType,
		Args: graphql.FieldConfigArgument{
			"validatorAddress": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveValidatorSigningStats,
	}
	b.queries["allValidatorsSigningStats"] = &graphql.Field{
		Type: graphql.NewNonNull(validatorSigningStatsConnectionType),
		Args: graphql.FieldConfigArgument{
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveAllValidatorsSigningStats,
	}
	b.queries["validatorSigningActivity"] = &graphql.Field{
		Type: graphql.NewNonNull(validatorSigningActivityConnectionType),
		Args: graphql.FieldConfigArgument{
			"validatorAddress": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveValidatorSigningActivity,
	}
	b.queries["blockSigners"] = &graphql.Field{
		Type: blockSignersType,
		Args: graphql.FieldConfigArgument{
			"blockNumber": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveBlockSigners,
	}
	b.queries["consensusData"] = &graphql.Field{
		Type: consensusDataType,
		Args: graphql.FieldConfigArgument{
			"blockNumber": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveConsensusData,
	}
	b.queries["validatorStats"] = &graphql.Field{
		Type: validatorStatsType,
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveValidatorStats,
	}
	b.queries["validatorParticipation"] = &graphql.Field{
		Type: validatorParticipationType,
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveValidatorParticipation,
	}
	b.queries["allValidatorStats"] = &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(validatorStatsType))),
		Args: graphql.FieldConfigArgument{
			"fromBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveAllValidatorStats,
	}
	b.queries["epochData"] = &graphql.Field{
		Type: epochDataType,
		Args: graphql.FieldConfigArgument{
			"epochNumber": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveEpochData,
	}
	b.queries["latestEpochData"] = &graphql.Field{
		Type:    epochDataType,
		Resolve: s.resolveLatestEpochData,
	}

	return b
}

// WithAddressIndexingQueries adds address indexing related queries (contract creation, token transfers)
func (b *SchemaBuilder) WithAddressIndexingQueries() *SchemaBuilder {
	s := b.schema

	b.queries["addressOverview"] = &graphql.Field{
		Type: graphql.NewNonNull(addressOverviewType),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
		},
		Resolve: s.resolveAddressOverview,
	}
	b.queries["contractCreation"] = &graphql.Field{
		Type: contractCreationType,
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
		},
		Resolve: s.resolveContractCreation,
	}
	b.queries["contracts"] = &graphql.Field{
		Type: contractCreationConnectionType,
		Args: graphql.FieldConfigArgument{
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveContracts,
	}
	b.queries["contractsByCreator"] = &graphql.Field{
		Type: contractCreationConnectionType,
		Args: graphql.FieldConfigArgument{
			"creator": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveContractsByCreator,
	}
	b.queries["internalTransactions"] = &graphql.Field{
		Type: graphql.NewList(graphql.NewNonNull(internalTransactionType)),
		Args: graphql.FieldConfigArgument{
			"transactionHash": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(hashType),
			},
		},
		Resolve: s.resolveInternalTransactions,
	}
	b.queries["internalTransactionsByAddress"] = &graphql.Field{
		Type: internalTransactionConnectionType,
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"isFrom": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveInternalTransactionsByAddress,
	}
	b.queries["erc20Transfer"] = &graphql.Field{
		Type: erc20TransferType,
		Args: graphql.FieldConfigArgument{
			"transactionHash": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(hashType),
			},
			"logIndex": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(graphql.Int),
			},
		},
		Resolve: s.resolveERC20Transfer,
	}
	b.queries["erc20TransfersByToken"] = &graphql.Field{
		Type: erc20TransferConnectionType,
		Args: graphql.FieldConfigArgument{
			"token": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveERC20TransfersByToken,
	}
	b.queries["erc20TransfersByAddress"] = &graphql.Field{
		Type: erc20TransferConnectionType,
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"isFrom": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveERC20TransfersByAddress,
	}
	b.queries["erc721Transfer"] = &graphql.Field{
		Type: erc721TransferType,
		Args: graphql.FieldConfigArgument{
			"transactionHash": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(hashType),
			},
			"logIndex": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(graphql.Int),
			},
		},
		Resolve: s.resolveERC721Transfer,
	}
	b.queries["erc721TransfersByToken"] = &graphql.Field{
		Type: erc721TransferConnectionType,
		Args: graphql.FieldConfigArgument{
			"token": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveERC721TransfersByToken,
	}
	b.queries["erc721TransfersByAddress"] = &graphql.Field{
		Type: erc721TransferConnectionType,
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"isFrom": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveERC721TransfersByAddress,
	}
	b.queries["erc721Owner"] = &graphql.Field{
		Type: addressType,
		Args: graphql.FieldConfigArgument{
			"token": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"tokenId": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveERC721Owner,
	}
	b.queries["nftsByOwner"] = &graphql.Field{
		Type:        graphql.NewNonNull(nftOwnershipConnectionType),
		Description: "Get all NFTs owned by a specific address",
		Args: graphql.FieldConfigArgument{
			"owner": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveNFTsByOwner,
	}

	return b
}

// WithSetCodeQueries adds EIP-7702 SetCode transaction queries
func (b *SchemaBuilder) WithSetCodeQueries() *SchemaBuilder {
	s := b.schema

	// Single authorization query by txHash and authIndex
	b.queries["setCodeAuthorization"] = &graphql.Field{
		Type:        setCodeAuthorizationType,
		Description: "Get a specific SetCode authorization by transaction hash and authorization index",
		Args: graphql.FieldConfigArgument{
			"txHash": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(hashType),
				Description: "Transaction hash",
			},
			"authIndex": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Authorization index within the transaction",
			},
		},
		Resolve: s.resolveSetCodeAuthorization,
	}

	// All authorizations in a transaction
	b.queries["setCodeAuthorizationsByTx"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(setCodeAuthorizationType))),
		Description: "Get all SetCode authorizations in a transaction",
		Args: graphql.FieldConfigArgument{
			"txHash": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(hashType),
				Description: "Transaction hash",
			},
		},
		Resolve: s.resolveSetCodeAuthorizationsByTx,
	}

	// Authorizations by target address (code delegation target)
	b.queries["setCodeAuthorizationsByTarget"] = &graphql.Field{
		Type:        graphql.NewNonNull(setCodeAuthorizationConnectionType),
		Description: "Get SetCode authorizations where the address is the delegation target",
		Args: graphql.FieldConfigArgument{
			"target": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Target address (code delegation target)",
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveSetCodeAuthorizationsByTarget,
	}

	// Authorizations by authority address (signer)
	b.queries["setCodeAuthorizationsByAuthority"] = &graphql.Field{
		Type:        graphql.NewNonNull(setCodeAuthorizationConnectionType),
		Description: "Get SetCode authorizations where the address is the authority (signer)",
		Args: graphql.FieldConfigArgument{
			"authority": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Authority address (signer of the authorization)",
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveSetCodeAuthorizationsByAuthority,
	}

	// Address SetCode info (delegation status and stats)
	b.queries["addressSetCodeInfo"] = &graphql.Field{
		Type:        graphql.NewNonNull(addressSetCodeInfoType),
		Description: "Get SetCode information for an address including delegation status and activity stats",
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Address to query",
			},
		},
		Resolve: s.resolveAddressSetCodeInfo,
	}

	// SetCode transactions in a block
	b.queries["setCodeTransactionsInBlock"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(transactionType))),
		Description: "Get all SetCode transactions in a specific block",
		Args: graphql.FieldConfigArgument{
			"blockNumber": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block number",
			},
		},
		Resolve: s.resolveSetCodeTransactionsInBlock,
	}

	// Recent SetCode transactions
	b.queries["recentSetCodeTransactions"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(transactionType))),
		Description: "Get recent SetCode transactions",
		Args: graphql.FieldConfigArgument{
			"limit": &graphql.ArgumentConfig{
				Type:        graphql.Int,
				Description: "Maximum number of transactions to return (max: 100, default: 10)",
			},
		},
		Resolve: s.resolveRecentSetCodeTransactions,
	}

	// Total SetCode transaction count
	b.queries["setCodeTransactionCount"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Int),
		Description: "Get the total count of SetCode transactions",
		Resolve:     s.resolveSetCodeTransactionCount,
	}

	return b
}

// WithSubscriptions adds GraphQL subscriptions
func (b *SchemaBuilder) WithSubscriptions() *SchemaBuilder {
	b.subscriptions["newBlock"] = &graphql.Field{
		Type:        graphql.NewNonNull(blockType),
		Description: "Subscribe to new blocks as they are indexed",
	}
	b.subscriptions["newTransaction"] = &graphql.Field{
		Type:        graphql.NewNonNull(transactionType),
		Description: "Subscribe to new transactions as they are indexed",
	}
	b.subscriptions["newPendingTransactions"] = &graphql.Field{
		Type: graphql.NewNonNull(transactionType),
		Args: graphql.FieldConfigArgument{
			"limit": &graphql.ArgumentConfig{
				Type: graphql.Int,
			},
		},
		Description: "Subscribe to new pending transactions (if available)",
	}
	b.subscriptions["logs"] = &graphql.Field{
		Type: graphql.NewNonNull(logType),
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(logFilterType),
			},
		},
		Description: "Subscribe to new logs matching a filter",
	}

	// Consensus subscriptions
	b.subscriptions["consensusBlock"] = &graphql.Field{
		Type:        graphql.NewNonNull(consensusBlockSubType),
		Description: "Subscribe to new consensus block events with validator participation data",
	}
	b.subscriptions["consensusFork"] = &graphql.Field{
		Type:        graphql.NewNonNull(consensusForkSubType),
		Description: "Subscribe to chain fork detection events",
	}
	b.subscriptions["consensusValidatorChange"] = &graphql.Field{
		Type:        graphql.NewNonNull(consensusValidatorChangeSubType),
		Description: "Subscribe to validator set change events at epoch boundaries",
	}
	b.subscriptions["consensusError"] = &graphql.Field{
		Type:        graphql.NewNonNull(consensusErrorSubType),
		Description: "Subscribe to consensus errors and anomalies (round changes, low participation)",
	}

	return b
}

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

// WithRPCProxy sets the RPC proxy service for the schema
func (b *SchemaBuilder) WithRPCProxy(proxy *rpcproxy.Proxy) *SchemaBuilder {
	b.schema.rpcProxy = proxy
	return b
}

// WithRPCProxyQueries adds RPC proxy related queries (contractCall, transactionStatus, internalTransactionsRPC)
func (b *SchemaBuilder) WithRPCProxyQueries() *SchemaBuilder {
	builder := &schemaBuilder{
		schema:  b.schema,
		queries: b.queries,
	}
	builder.buildRPCProxyQueries()
	return b
}

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

// WithNotificationService sets the notification service for the schema
func (b *SchemaBuilder) WithNotificationService(service notifications.Service) *SchemaBuilder {
	b.schema.notificationService = service
	return b
}

// WithNotificationQueries adds notification management queries and mutations
func (b *SchemaBuilder) WithNotificationQueries() *SchemaBuilder {
	s := b.schema

	// Queries
	b.queries["notificationSettings"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(notificationSettingType))),
		Description: "List all notification settings",
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type:        notificationSettingsFilterInputType,
				Description: "Filter options",
			},
		},
		Resolve: s.resolveNotificationSettings,
	}
	b.queries["notificationSetting"] = &graphql.Field{
		Type:        notificationSettingType,
		Description: "Get a notification setting by ID",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Setting identifier",
			},
		},
		Resolve: s.resolveNotificationSetting,
	}
	b.queries["notifications"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(notificationType))),
		Description: "List notifications with optional filtering",
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type:        notificationsFilterInputType,
				Description: "Filter options",
			},
		},
		Resolve: s.resolveNotifications,
	}
	b.queries["notification"] = &graphql.Field{
		Type:        notificationType,
		Description: "Get a notification by ID",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Notification identifier",
			},
		},
		Resolve: s.resolveNotification,
	}
	b.queries["notificationStats"] = &graphql.Field{
		Type:        notificationStatsType,
		Description: "Get statistics for a notification setting",
		Args: graphql.FieldConfigArgument{
			"settingId": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Setting identifier",
			},
		},
		Resolve: s.resolveNotificationStats,
	}
	b.queries["deliveryHistory"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(deliveryHistoryType))),
		Description: "Get delivery history for a notification",
		Args: graphql.FieldConfigArgument{
			"notificationId": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Notification identifier",
			},
		},
		Resolve: s.resolveDeliveryHistory,
	}

	// Mutations
	b.mutations["createNotificationSetting"] = &graphql.Field{
		Type:        graphql.NewNonNull(notificationSettingType),
		Description: "Create a new notification setting",
		Args: graphql.FieldConfigArgument{
			"input": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(createNotificationSettingInputType),
				Description: "Setting configuration",
			},
		},
		Resolve: s.resolveCreateNotificationSetting,
	}
	b.mutations["updateNotificationSetting"] = &graphql.Field{
		Type:        graphql.NewNonNull(notificationSettingType),
		Description: "Update an existing notification setting",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Setting identifier",
			},
			"input": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(updateNotificationSettingInputType),
				Description: "Fields to update",
			},
		},
		Resolve: s.resolveUpdateNotificationSetting,
	}
	b.mutations["deleteNotificationSetting"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Boolean),
		Description: "Delete a notification setting",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Setting identifier",
			},
		},
		Resolve: s.resolveDeleteNotificationSetting,
	}
	b.mutations["testNotificationSetting"] = &graphql.Field{
		Type:        graphql.NewNonNull(deliveryResultType),
		Description: "Test a notification setting by sending a sample notification",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Setting identifier",
			},
		},
		Resolve: s.resolveTestNotificationSetting,
	}
	b.mutations["retryNotification"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Boolean),
		Description: "Retry a failed notification",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Notification identifier",
			},
		},
		Resolve: s.resolveRetryNotification,
	}
	b.mutations["cancelNotification"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Boolean),
		Description: "Cancel a pending notification",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Notification identifier",
			},
		},
		Resolve: s.resolveCancelNotification,
	}

	return b
}

// WithWatchlistQueries adds watchlist management queries and mutations
func (b *SchemaBuilder) WithWatchlistQueries() *SchemaBuilder {
	s := b.schema

	// Queries
	b.queries["watchedAddresses"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(watchedAddressType))),
		Description: "Get all watched addresses with optional filtering",
		Args: graphql.FieldConfigArgument{
			"chainId": &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "Filter by chain ID",
			},
			"limit": &graphql.ArgumentConfig{
				Type:         graphql.Int,
				DefaultValue: 100,
				Description:  "Maximum number of results",
			},
			"offset": &graphql.ArgumentConfig{
				Type:         graphql.Int,
				DefaultValue: 0,
				Description:  "Pagination offset",
			},
		},
		Resolve: s.resolveWatchedAddresses,
	}
	b.queries["watchedAddress"] = &graphql.Field{
		Type:        watchedAddressType,
		Description: "Get a specific watched address by ID",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Watched address identifier",
			},
		},
		Resolve: s.resolveWatchedAddress,
	}
	b.queries["watchEvents"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(watchEventType))),
		Description: "Get watch events with optional filtering",
		Args: graphql.FieldConfigArgument{
			"addressId": &graphql.ArgumentConfig{
				Type:        graphql.ID,
				Description: "Filter by watched address ID",
			},
			"chainId": &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "Filter by chain ID",
			},
			"eventType": &graphql.ArgumentConfig{
				Type:        watchEventTypeEnumType,
				Description: "Filter by event type",
			},
			"limit": &graphql.ArgumentConfig{
				Type:         graphql.Int,
				DefaultValue: 100,
				Description:  "Maximum number of results",
			},
			"offset": &graphql.ArgumentConfig{
				Type:         graphql.Int,
				DefaultValue: 0,
				Description:  "Pagination offset",
			},
		},
		Resolve: s.resolveWatchEvents,
	}

	// Mutations
	b.mutations["watchAddress"] = &graphql.Field{
		Type:        graphql.NewNonNull(watchedAddressType),
		Description: "Add an address to the watchlist",
		Args: graphql.FieldConfigArgument{
			"input": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(watchAddressInputType),
				Description: "Watch address details",
			},
		},
		Resolve: s.resolveWatchAddress,
	}
	b.mutations["unwatchAddress"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Boolean),
		Description: "Remove an address from the watchlist",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Watched address identifier",
			},
		},
		Resolve: s.resolveUnwatchAddress,
	}
	b.mutations["updateWatchFilter"] = &graphql.Field{
		Type:        graphql.NewNonNull(watchedAddressType),
		Description: "Update the filter for a watched address",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Watched address identifier",
			},
			"filter": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(watchFilterInputType),
				Description: "New filter configuration",
			},
		},
		Resolve: s.resolveUpdateWatchFilter,
	}

	return b
}

// Build constructs the final GraphQL schema
func (b *SchemaBuilder) Build() (*Schema, error) {
	// Load stored ABIs
	if err := b.schema.loadStoredABIs(context.Background()); err != nil {
		b.schema.logger.Warn("failed to load stored ABIs", zap.Error(err))
		// Don't fail initialization, ABIs can be loaded later
	}

	// Create query type
	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Query",
		Fields: b.queries,
	})

	// Create subscription type
	subscriptionType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Subscription",
		Fields: b.subscriptions,
	})

	// Create mutation type
	mutationType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Mutation",
		Fields: b.mutations,
	})

	// Create schema
	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query:        queryType,
		Mutation:     mutationType,
		Subscription: subscriptionType,
	})
	if err != nil {
		return nil, err
	}

	b.schema.schema = schema
	return b.schema, nil
}

// NewSchema creates a new GraphQL schema using the builder pattern
func NewSchema(store storage.Storage, logger *zap.Logger) (*Schema, error) {
	return NewSchemaBuilder(store, logger).
		WithCoreQueries().
		WithHistoricalQueries().
		WithAnalyticsQueries().
		WithSystemContractQueries().
		WithConsensusQueries().
		WithAddressIndexingQueries().
		WithSetCodeQueries().
		WithFeeDelegationQueries().
		WithTokenMetadataQueries().
		WithTokenHolderQueries().
		WithSubscriptions().
		WithMutations().
		Build()
}

// WithFeeDelegationQueries adds fee delegation related queries
func (b *SchemaBuilder) WithFeeDelegationQueries() *SchemaBuilder {
	builder := &schemaBuilder{
		schema:  b.schema,
		queries: b.queries,
	}
	builder.buildFeeDelegationQueries()
	return b
}

// Schema returns the GraphQL schema
func (s *Schema) Schema() graphql.Schema {
	return s.schema
}

// loadStoredABIs loads all ABIs from storage into the decoder
func (s *Schema) loadStoredABIs(ctx context.Context) error {
	addresses, err := s.storage.ListABIs(ctx)
	if err != nil {
		return fmt.Errorf("failed to list ABIs: %w", err)
	}

	loaded := 0
	for _, addr := range addresses {
		abiJSON, err := s.storage.GetABI(ctx, addr)
		if err != nil {
			s.logger.Warn("failed to get ABI",
				zap.String("address", addr.Hex()),
				zap.Error(err),
			)
			continue
		}

		// Load into decoder
		if err := s.abiDecoder.LoadABI(addr, "", string(abiJSON)); err != nil {
			s.logger.Warn("failed to load ABI into decoder",
				zap.String("address", addr.Hex()),
				zap.Error(err),
			)
			continue
		}

		loaded++
	}

	s.logger.Info("loaded ABIs from storage into GraphQL schema",
		zap.Int("total", len(addresses)),
		zap.Int("loaded", loaded),
	)

	return nil
}

// SetVerifier sets the contract verifier for the schema
func (s *Schema) SetVerifier(v verifier.Verifier) {
	s.verifier = v
}
