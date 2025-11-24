package graphql

import (
	"context"
	"fmt"

	abiDecoder "github.com/0xmhha/indexer-go/abi"
	"github.com/0xmhha/indexer-go/storage"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// Schema holds the GraphQL schema
type Schema struct {
	schema     graphql.Schema
	storage    storage.Storage
	logger     *zap.Logger
	abiDecoder *abiDecoder.Decoder
}

// NewSchema creates a new GraphQL schema
func NewSchema(store storage.Storage, logger *zap.Logger) (*Schema, error) {
	s := &Schema{
		storage:    store,
		logger:     logger,
		abiDecoder: abiDecoder.NewDecoder(),
	}

	// Load all stored ABIs into the decoder
	if err := s.loadStoredABIs(context.Background()); err != nil {
		logger.Warn("failed to load stored ABIs", zap.Error(err))
		// Don't fail initialization, ABIs can be loaded later
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
					"decode": &graphql.ArgumentConfig{
						Type:        graphql.Boolean,
						Description: "Decode logs using ABI if available",
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
				Type:    epochInfoType,
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

			// Address Indexing Queries
			"contractCreation": &graphql.Field{
				Type: contractCreationType,
				Args: graphql.FieldConfigArgument{
					"address": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(addressType),
					},
				},
				Resolve: s.resolveContractCreation,
			},
			"contractsByCreator": &graphql.Field{
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
			},
			"internalTransactions": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(internalTransactionType)),
				Args: graphql.FieldConfigArgument{
					"txHash": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(hashType),
					},
				},
				Resolve: s.resolveInternalTransactions,
			},
			"internalTransactionsByAddress": &graphql.Field{
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
			},
			"erc20Transfer": &graphql.Field{
				Type: erc20TransferType,
				Args: graphql.FieldConfigArgument{
					"txHash": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(hashType),
					},
					"logIndex": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.Int),
					},
				},
				Resolve: s.resolveERC20Transfer,
			},
			"erc20TransfersByToken": &graphql.Field{
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
			},
			"erc20TransfersByAddress": &graphql.Field{
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
			},
			"erc721Transfer": &graphql.Field{
				Type: erc721TransferType,
				Args: graphql.FieldConfigArgument{
					"txHash": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(hashType),
					},
					"logIndex": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.Int),
					},
				},
				Resolve: s.resolveERC721Transfer,
			},
			"erc721TransfersByToken": &graphql.Field{
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
			},
			"erc721TransfersByAddress": &graphql.Field{
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
			},
			"erc721Owner": &graphql.Field{
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
			},
		},
	})

	// Create subscription type
	subscriptionType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Subscription",
		Fields: graphql.Fields{
			"newBlock": &graphql.Field{
				Type:        graphql.NewNonNull(blockType),
				Description: "Subscribe to new blocks as they are indexed",
			},
			"newTransaction": &graphql.Field{
				Type:        graphql.NewNonNull(transactionType),
				Description: "Subscribe to new transactions as they are indexed",
			},
			"newPendingTransactions": &graphql.Field{
				Type: graphql.NewNonNull(transactionType),
				Args: graphql.FieldConfigArgument{
					"limit": &graphql.ArgumentConfig{
						Type: graphql.Int,
					},
				},
				Description: "Subscribe to new pending transactions (if available)",
			},
			"logs": &graphql.Field{
				Type: graphql.NewNonNull(logType),
				Args: graphql.FieldConfigArgument{
					"filter": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(logFilterType),
					},
				},
				Description: "Subscribe to new logs matching a filter",
			},
		},
	})

	// Create schema
	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query:        queryType,
		Subscription: subscriptionType,
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
