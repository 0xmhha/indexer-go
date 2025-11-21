package graphql

import (
	"github.com/graphql-go/graphql"
)

var (
	// Scalar types
	bytesType   = graphql.String
	bigIntType  = graphql.String
	addressType = graphql.String
	hashType    = graphql.String

	// Block type
	blockType *graphql.Object

	// Transaction type
	transactionType *graphql.Object

	// Receipt type
	receiptType *graphql.Object

	// Log type
	logType *graphql.Object

	// AccessListEntry type
	accessListEntryType *graphql.Object

	// FeePayerSignature type for Fee Delegation
	feePayerSignatureType *graphql.Object

	// PageInfo type
	pageInfoType *graphql.Object

	// BlockConnection type
	blockConnectionType *graphql.Object

	// TransactionConnection type
	transactionConnectionType *graphql.Object

	// LogConnection type
	logConnectionType *graphql.Object

	// Input types
	blockFilterType                 *graphql.InputObject
	transactionFilterType           *graphql.InputObject
	logFilterType                   *graphql.InputObject
	paginationInputType             *graphql.InputObject
	historicalTransactionFilterType *graphql.InputObject

	// Historical data types
	balanceSnapshotType          *graphql.Object
	balanceHistoryConnectionType *graphql.Object

	// Analytics types
	minerStatsType   *graphql.Object
	tokenBalanceType *graphql.Object

	// System contract types
	proposalStatusEnumType        *graphql.Enum
	mintEventType                 *graphql.Object
	burnEventType                 *graphql.Object
	minterConfigEventType         *graphql.Object
	proposalType                  *graphql.Object
	proposalVoteType              *graphql.Object
	gasTipUpdateEventType         *graphql.Object
	blacklistEventType            *graphql.Object
	validatorChangeEventType      *graphql.Object
	memberChangeEventType         *graphql.Object
	emergencyPauseEventType       *graphql.Object
	depositMintProposalType       *graphql.Object
	minterInfoType                *graphql.Object
	validatorInfoType             *graphql.Object
	systemContractEventFilterType *graphql.InputObject
	proposalFilterType            *graphql.InputObject
	mintEventConnectionType       *graphql.Object
	burnEventConnectionType       *graphql.Object
	proposalConnectionType        *graphql.Object

	// WBFT consensus types
	wbftAggregatedSealType                 *graphql.Object
	candidateType                          *graphql.Object
	epochInfoType                          *graphql.Object
	wbftBlockExtraType                     *graphql.Object
	validatorSigningStatsType              *graphql.Object
	validatorSigningActivityType           *graphql.Object
	blockSignersType                       *graphql.Object
	validatorSigningStatsConnectionType    *graphql.Object
	validatorSigningActivityConnectionType *graphql.Object
)

func init() {
	initTypes()
}

func initTypes() {
	// AccessListEntry type
	accessListEntryType = graphql.NewObject(graphql.ObjectConfig{
		Name: "AccessListEntry",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"storageKeys": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(hashType)),
			},
		},
	})

	// FeePayerSignature type for Fee Delegation transactions
	feePayerSignatureType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "FeePayerSignature",
		Description: "Signature from fee payer in Fee Delegation transactions",
		Fields: graphql.Fields{
			"v": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"r": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"s": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
		},
	})

	// Log type
	logType = graphql.NewObject(graphql.ObjectConfig{
		Name: "Log",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"topics": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(hashType)),
			},
			"data": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blockHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"transactionIndex": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"logIndex": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"removed": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
		},
	})

	// Receipt type
	receiptType = graphql.NewObject(graphql.ObjectConfig{
		Name: "Receipt",
		Fields: graphql.Fields{
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blockHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"transactionIndex": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"contractAddress": &graphql.Field{
				Type: addressType,
			},
			"gasUsed": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"cumulativeGasUsed": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"effectiveGasPrice": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"status": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"logs": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(logType)),
			},
			"logsBloom": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
		},
	})

	// Transaction type
	transactionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "Transaction",
		Fields: graphql.Fields{
			"hash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blockHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"transactionIndex": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"from": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"to": &graphql.Field{
				Type: addressType,
			},
			"value": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"gas": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"gasPrice": &graphql.Field{
				Type: bigIntType,
			},
			"maxFeePerGas": &graphql.Field{
				Type: bigIntType,
			},
			"maxPriorityFeePerGas": &graphql.Field{
				Type: bigIntType,
			},
			"type": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"input": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"nonce": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"v": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"r": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"s": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"chainId": &graphql.Field{
				Type: bigIntType,
			},
			"accessList": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(accessListEntryType)),
			},
			"receipt": &graphql.Field{
				Type: receiptType,
			},
			// Fee Delegation fields (type 0x16)
			"feePayer": &graphql.Field{
				Type:        addressType,
				Description: "Fee payer address for Fee Delegation transactions (type 0x16)",
			},
			"feePayerSignatures": &graphql.Field{
				Type:        graphql.NewList(graphql.NewNonNull(feePayerSignatureType)),
				Description: "Fee payer signatures for Fee Delegation transactions",
			},
		},
	})

	// Block type
	blockType = graphql.NewObject(graphql.ObjectConfig{
		Name: "Block",
		Fields: graphql.Fields{
			"number": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"hash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"parentHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"nonce": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"miner": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"difficulty": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"totalDifficulty": &graphql.Field{
				Type: bigIntType,
			},
			"gasLimit": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"gasUsed": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			// EIP-1559 fields
			"baseFeePerGas": &graphql.Field{
				Type:        bigIntType,
				Description: "Base fee per gas for EIP-1559 blocks (post-London)",
			},
			"extraData": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"size": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactions": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(transactionType)),
			},
			"transactionCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"uncles": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(hashType)),
			},
			// Post-merge fields
			"withdrawalsRoot": &graphql.Field{
				Type:        hashType,
				Description: "Withdrawals merkle root (post-Shanghai)",
			},
			// EIP-4844 blob fields
			"blobGasUsed": &graphql.Field{
				Type:        bigIntType,
				Description: "Total blob gas used in this block (EIP-4844)",
			},
			"excessBlobGas": &graphql.Field{
				Type:        bigIntType,
				Description: "Excess blob gas (EIP-4844)",
			},
		},
	})

	// PageInfo type
	pageInfoType = graphql.NewObject(graphql.ObjectConfig{
		Name: "PageInfo",
		Fields: graphql.Fields{
			"hasNextPage": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"hasPreviousPage": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"startCursor": &graphql.Field{
				Type: graphql.String,
			},
			"endCursor": &graphql.Field{
				Type: graphql.String,
			},
		},
	})

	// BlockConnection type
	blockConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "BlockConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(blockType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// TransactionConnection type
	transactionConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "TransactionConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(transactionType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// LogConnection type
	logConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "LogConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(logType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// Input types
	blockFilterType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "BlockFilter",
		Fields: graphql.InputObjectConfigFieldMap{
			"numberFrom": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"numberTo": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"timestampFrom": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"timestampTo": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"miner": &graphql.InputObjectFieldConfig{
				Type: addressType,
			},
		},
	})

	transactionFilterType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "TransactionFilter",
		Fields: graphql.InputObjectConfigFieldMap{
			"blockNumberFrom": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"blockNumberTo": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"from": &graphql.InputObjectFieldConfig{
				Type: addressType,
			},
			"to": &graphql.InputObjectFieldConfig{
				Type: addressType,
			},
			"type": &graphql.InputObjectFieldConfig{
				Type: graphql.Int,
			},
		},
	})

	logFilterType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "LogFilter",
		Fields: graphql.InputObjectConfigFieldMap{
			"address": &graphql.InputObjectFieldConfig{
				Type: addressType,
			},
			"topics": &graphql.InputObjectFieldConfig{
				Type: graphql.NewList(hashType),
			},
			"blockNumberFrom": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"blockNumberTo": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
		},
	})

	paginationInputType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "PaginationInput",
		Fields: graphql.InputObjectConfigFieldMap{
			"limit": &graphql.InputObjectFieldConfig{
				Type: graphql.Int,
			},
			"offset": &graphql.InputObjectFieldConfig{
				Type: graphql.Int,
			},
		},
	})

	historicalTransactionFilterType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "HistoricalTransactionFilter",
		Fields: graphql.InputObjectConfigFieldMap{
			"fromBlock": &graphql.InputObjectFieldConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.InputObjectFieldConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"minValue": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"maxValue": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"txType": &graphql.InputObjectFieldConfig{
				Type: graphql.Int,
			},
			"successOnly": &graphql.InputObjectFieldConfig{
				Type: graphql.Boolean,
			},
		},
	})

	// BalanceSnapshot type
	balanceSnapshotType = graphql.NewObject(graphql.ObjectConfig{
		Name: "BalanceSnapshot",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"balance": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"delta": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: hashType,
			},
		},
	})

	// BalanceHistoryConnection type
	balanceHistoryConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "BalanceHistoryConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(balanceSnapshotType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// MinerStats type
	minerStatsType = graphql.NewObject(graphql.ObjectConfig{
		Name: "MinerStats",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"blockCount": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"lastBlockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// TokenBalance type
	tokenBalanceType = graphql.NewObject(graphql.ObjectConfig{
		Name: "TokenBalance",
		Fields: graphql.Fields{
			"contractAddress": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"tokenType": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			"balance": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"tokenId": &graphql.Field{
				Type: bigIntType,
			},
		},
	})

	// ProposalStatus enum
	proposalStatusEnumType = graphql.NewEnum(graphql.EnumConfig{
		Name: "ProposalStatus",
		Values: graphql.EnumValueConfigMap{
			"NONE": &graphql.EnumValueConfig{
				Value: "NONE",
			},
			"VOTING": &graphql.EnumValueConfig{
				Value: "VOTING",
			},
			"APPROVED": &graphql.EnumValueConfig{
				Value: "APPROVED",
			},
			"EXECUTED": &graphql.EnumValueConfig{
				Value: "EXECUTED",
			},
			"CANCELLED": &graphql.EnumValueConfig{
				Value: "CANCELLED",
			},
			"EXPIRED": &graphql.EnumValueConfig{
				Value: "EXPIRED",
			},
			"FAILED": &graphql.EnumValueConfig{
				Value: "FAILED",
			},
			"REJECTED": &graphql.EnumValueConfig{
				Value: "REJECTED",
			},
		},
	})

	// MintEvent type
	mintEventType = graphql.NewObject(graphql.ObjectConfig{
		Name: "MintEvent",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"minter": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"to": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"amount": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// BurnEvent type
	burnEventType = graphql.NewObject(graphql.ObjectConfig{
		Name: "BurnEvent",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"burner": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"amount": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"withdrawalId": &graphql.Field{
				Type: graphql.String,
			},
		},
	})

	// MinterConfigEvent type
	minterConfigEventType = graphql.NewObject(graphql.ObjectConfig{
		Name: "MinterConfigEvent",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"minter": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"allowance": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"action": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// Proposal type
	proposalType = graphql.NewObject(graphql.ObjectConfig{
		Name: "Proposal",
		Fields: graphql.Fields{
			"contract": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"proposalId": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"proposer": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"actionType": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"callData": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"memberVersion": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"requiredApprovals": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"approved": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"rejected": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"status": &graphql.Field{
				Type: graphql.NewNonNull(proposalStatusEnumType),
			},
			"createdAt": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"executedAt": &graphql.Field{
				Type: bigIntType,
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
		},
	})

	// ProposalVote type
	proposalVoteType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ProposalVote",
		Fields: graphql.Fields{
			"contract": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"proposalId": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"voter": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"approval": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// GasTipUpdateEvent type
	gasTipUpdateEventType = graphql.NewObject(graphql.ObjectConfig{
		Name: "GasTipUpdateEvent",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"oldTip": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"newTip": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"updater": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// BlacklistEvent type
	blacklistEventType = graphql.NewObject(graphql.ObjectConfig{
		Name: "BlacklistEvent",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"account": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"action": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			"proposalId": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// ValidatorChangeEvent type
	validatorChangeEventType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ValidatorChangeEvent",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"validator": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"action": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			"oldValidator": &graphql.Field{
				Type: addressType,
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// MemberChangeEvent type
	memberChangeEventType = graphql.NewObject(graphql.ObjectConfig{
		Name: "MemberChangeEvent",
		Fields: graphql.Fields{
			"contract": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"member": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"action": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			"oldMember": &graphql.Field{
				Type: addressType,
			},
			"totalMembers": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"newQuorum": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// EmergencyPauseEvent type
	emergencyPauseEventType = graphql.NewObject(graphql.ObjectConfig{
		Name: "EmergencyPauseEvent",
		Fields: graphql.Fields{
			"contract": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"proposalId": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"action": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// DepositMintProposal type
	depositMintProposalType = graphql.NewObject(graphql.ObjectConfig{
		Name: "DepositMintProposal",
		Fields: graphql.Fields{
			"proposalId": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"to": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"amount": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"depositId": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			"status": &graphql.Field{
				Type: graphql.NewNonNull(proposalStatusEnumType),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// MinterInfo type
	minterInfoType = graphql.NewObject(graphql.ObjectConfig{
		Name: "MinterInfo",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"allowance": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"isActive": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
		},
	})

	// ValidatorInfo type
	validatorInfoType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ValidatorInfo",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"isActive": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
		},
	})

	// SystemContractEventFilter input type
	systemContractEventFilterType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "SystemContractEventFilter",
		Fields: graphql.InputObjectConfigFieldMap{
			"fromBlock": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"toBlock": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"minter": &graphql.InputObjectFieldConfig{
				Type: addressType,
			},
			"burner": &graphql.InputObjectFieldConfig{
				Type: addressType,
			},
			"status": &graphql.InputObjectFieldConfig{
				Type: proposalStatusEnumType,
			},
		},
	})

	// ProposalFilter input type
	proposalFilterType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "ProposalFilter",
		Fields: graphql.InputObjectConfigFieldMap{
			"contract": &graphql.InputObjectFieldConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"status": &graphql.InputObjectFieldConfig{
				Type: proposalStatusEnumType,
			},
		},
	})

	// MintEventConnection type
	mintEventConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "MintEventConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(mintEventType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// BurnEventConnection type
	burnEventConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "BurnEventConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(burnEventType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// ProposalConnection type
	proposalConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ProposalConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(proposalType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// ========== WBFT Consensus Types ==========

	// WBFTAggregatedSeal type
	wbftAggregatedSealType = graphql.NewObject(graphql.ObjectConfig{
		Name: "WBFTAggregatedSeal",
		Fields: graphql.Fields{
			"sealers": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"signature": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
		},
	})

	// Candidate type
	candidateType = graphql.NewObject(graphql.ObjectConfig{
		Name: "Candidate",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"diligence": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// EpochInfo type
	epochInfoType = graphql.NewObject(graphql.ObjectConfig{
		Name: "EpochInfo",
		Fields: graphql.Fields{
			"epochNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"candidates": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(candidateType))),
			},
			"validators": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(graphql.Int))),
			},
			"blsPublicKeys": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(bytesType))),
			},
		},
	})

	// WBFTBlockExtra type
	wbftBlockExtraType = graphql.NewObject(graphql.ObjectConfig{
		Name: "WBFTBlockExtra",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blockHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"randaoReveal": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"prevRound": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"prevPreparedSeal": &graphql.Field{
				Type: wbftAggregatedSealType,
			},
			"prevCommittedSeal": &graphql.Field{
				Type: wbftAggregatedSealType,
			},
			"round": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"preparedSeal": &graphql.Field{
				Type: wbftAggregatedSealType,
			},
			"committedSeal": &graphql.Field{
				Type: wbftAggregatedSealType,
			},
			"gasTip": &graphql.Field{
				Type: bigIntType,
			},
			"epochInfo": &graphql.Field{
				Type: epochInfoType,
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// ValidatorSigningStats type
	validatorSigningStatsType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ValidatorSigningStats",
		Fields: graphql.Fields{
			"validatorAddress": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"validatorIndex": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"prepareSignCount": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"prepareMissCount": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"commitSignCount": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"commitMissCount": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"fromBlock": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"signingRate": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Float),
			},
		},
	})

	// ValidatorSigningActivity type
	validatorSigningActivityType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ValidatorSigningActivity",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blockHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"validatorAddress": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"validatorIndex": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"signedPrepare": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"signedCommit": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"round": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// BlockSigners type
	blockSignersType = graphql.NewObject(graphql.ObjectConfig{
		Name: "BlockSigners",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"preparers": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
			},
			"committers": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
			},
		},
	})

	// ValidatorSigningStatsConnection type
	validatorSigningStatsConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ValidatorSigningStatsConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(validatorSigningStatsType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// ValidatorSigningActivityConnection type
	validatorSigningActivityConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ValidatorSigningActivityConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(validatorSigningActivityType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})
}
