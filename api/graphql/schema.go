package graphql

import (
	"github.com/0xmhha/indexer-go/storage"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// Schema holds the GraphQL schema
type Schema struct {
	schema  graphql.Schema
	storage storage.Storage
	logger  *zap.Logger
}

// NewSchema creates a new GraphQL schema
func NewSchema(store storage.Storage, logger *zap.Logger) (*Schema, error) {
	s := &Schema{
		storage: store,
		logger:  logger,
	}

	// Create query type
	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"latestHeight": &graphql.Field{
				Type:    graphql.NewNonNull(bigIntType),
				Resolve: s.resolveLatestHeight,
			},
			"block": &graphql.Field{
				Type: blockType,
				Args: graphql.FieldConfigArgument{
					"number": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(bigIntType),
					},
				},
				Resolve: s.resolveBlock,
			},
			"blockByHash": &graphql.Field{
				Type: blockType,
				Args: graphql.FieldConfigArgument{
					"hash": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(hashType),
					},
				},
				Resolve: s.resolveBlockByHash,
			},
			"blocks": &graphql.Field{
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
			},
			"transaction": &graphql.Field{
				Type: transactionType,
				Args: graphql.FieldConfigArgument{
					"hash": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(hashType),
					},
				},
				Resolve: s.resolveTransaction,
			},
			"transactions": &graphql.Field{
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
			},
			"transactionsByAddress": &graphql.Field{
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
			},
			"receipt": &graphql.Field{
				Type: receiptType,
				Args: graphql.FieldConfigArgument{
					"transactionHash": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(hashType),
					},
				},
				Resolve: s.resolveReceipt,
			},
			"receiptsByBlock": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(receiptType)),
				Args: graphql.FieldConfigArgument{
					"blockNumber": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(bigIntType),
					},
				},
				Resolve: s.resolveReceiptsByBlock,
			},
			"logs": &graphql.Field{
				Type: graphql.NewNonNull(logConnectionType),
				Args: graphql.FieldConfigArgument{
					"filter": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(logFilterType),
					},
					"pagination": &graphql.ArgumentConfig{
						Type: paginationInputType,
					},
				},
				Resolve: s.resolveLogs,
			},
			// Historical data queries
			"blocksByTimeRange": &graphql.Field{
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
			},
			"blockByTimestamp": &graphql.Field{
				Type: blockType,
				Args: graphql.FieldConfigArgument{
					"timestamp": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(bigIntType),
					},
				},
				Resolve: s.resolveBlockByTimestamp,
			},
			"transactionsByAddressFiltered": &graphql.Field{
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
			},
			"addressBalance": &graphql.Field{
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
			},
			"balanceHistory": &graphql.Field{
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
			},
			"blockCount": &graphql.Field{
				Type:    graphql.NewNonNull(bigIntType),
				Resolve: s.resolveBlockCount,
			},
			"transactionCount": &graphql.Field{
				Type:    graphql.NewNonNull(bigIntType),
				Resolve: s.resolveTransactionCount,
			},
			// Analytics queries
			"topMiners": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(minerStatsType))),
				Args: graphql.FieldConfigArgument{
					"limit": &graphql.ArgumentConfig{
						Type: graphql.Int,
					},
				},
				Resolve: s.resolveTopMiners,
			},
			"tokenBalances": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(tokenBalanceType))),
				Args: graphql.FieldConfigArgument{
					"address": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(addressType),
					},
				},
				Resolve: s.resolveTokenBalances,
			},
			// System contract queries
			"totalSupply": &graphql.Field{
				Type:    graphql.NewNonNull(bigIntType),
				Resolve: s.resolveTotalSupply,
			},
			"activeMinters": &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(minterInfoType))),
				Resolve: s.resolveActiveMinters,
			},
			"minterAllowance": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
				Args: graphql.FieldConfigArgument{
					"minter": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(addressType),
					},
				},
				Resolve: s.resolveMinterAllowance,
			},
			"activeValidators": &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(validatorInfoType))),
				Resolve: s.resolveActiveValidators,
			},
			"blacklistedAddresses": &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
				Resolve: s.resolveBlacklistedAddresses,
			},
			"proposals": &graphql.Field{
				Type: graphql.NewNonNull(proposalConnectionType),
				Args: graphql.FieldConfigArgument{
					"filter": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(proposalFilterType),
					},
					"pagination": &graphql.ArgumentConfig{
						Type: paginationInputType,
					},
				},
				Resolve: s.resolveProposals,
			},
			"proposal": &graphql.Field{
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
			},
			"proposalVotes": &graphql.Field{
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
			},
			"mintEvents": &graphql.Field{
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
			},
			"burnEvents": &graphql.Field{
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
			},
			"minterHistory": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(minterConfigEventType))),
				Args: graphql.FieldConfigArgument{
					"minter": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(addressType),
					},
				},
				Resolve: s.resolveMinterHistory,
			},
			"validatorHistory": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(validatorChangeEventType))),
				Args: graphql.FieldConfigArgument{
					"validator": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(addressType),
					},
				},
				Resolve: s.resolveValidatorHistory,
			},
			"gasTipHistory": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(gasTipUpdateEventType))),
				Args: graphql.FieldConfigArgument{
					"filter": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(systemContractEventFilterType),
					},
				},
				Resolve: s.resolveGasTipHistory,
			},
			"blacklistHistory": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(blacklistEventType))),
				Args: graphql.FieldConfigArgument{
					"address": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(addressType),
					},
				},
				Resolve: s.resolveBlacklistHistory,
			},
			"memberHistory": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(memberChangeEventType))),
				Args: graphql.FieldConfigArgument{
					"contract": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(addressType),
					},
				},
				Resolve: s.resolveMemberHistory,
			},
			"emergencyPauseHistory": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(emergencyPauseEventType))),
				Args: graphql.FieldConfigArgument{
					"contract": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(addressType),
					},
				},
				Resolve: s.resolveEmergencyPauseHistory,
			},
			"depositMintProposals": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(depositMintProposalType))),
				Args: graphql.FieldConfigArgument{
					"filter": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(systemContractEventFilterType),
					},
				},
				Resolve: s.resolveDepositMintProposals,
			},
			// WBFT Consensus Queries
			"wbftBlockExtra": &graphql.Field{
				Type: wbftBlockExtraType,
				Args: graphql.FieldConfigArgument{
					"blockNumber": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(bigIntType),
					},
				},
				Resolve: s.resolveWBFTBlockExtra,
			},
			"wbftBlockExtraByHash": &graphql.Field{
				Type: wbftBlockExtraType,
				Args: graphql.FieldConfigArgument{
					"blockHash": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(hashType),
					},
				},
				Resolve: s.resolveWBFTBlockExtraByHash,
			},
			"epochInfo": &graphql.Field{
				Type: epochInfoType,
				Args: graphql.FieldConfigArgument{
					"epochNumber": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(bigIntType),
					},
				},
				Resolve: s.resolveEpochInfo,
			},
			"latestEpochInfo": &graphql.Field{
				Type: epochInfoType,
				Resolve: s.resolveLatestEpochInfo,
			},
			"validatorSigningStats": &graphql.Field{
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
			},
			"allValidatorsSigningStats": &graphql.Field{
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
			},
			"validatorSigningActivity": &graphql.Field{
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
			},
			"blockSigners": &graphql.Field{
				Type: blockSignersType,
				Args: graphql.FieldConfigArgument{
					"blockNumber": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(bigIntType),
					},
				},
				Resolve: s.resolveBlockSigners,
			},
		},
	})

	// Create schema
	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query: queryType,
	})
	if err != nil {
		return nil, err
	}

	s.schema = schema
	return s, nil
}

// Schema returns the GraphQL schema
func (s *Schema) Schema() graphql.Schema {
	return s.schema
}
