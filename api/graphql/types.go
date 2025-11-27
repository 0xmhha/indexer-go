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

	// DecodedLog type
	decodedLogType *graphql.Object

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
	minerStatsType           *graphql.Object
	tokenBalanceType         *graphql.Object
	gasStatsType             *graphql.Object
	addressGasStatsType      *graphql.Object
	networkMetricsType       *graphql.Object
	addressActivityStatsType *graphql.Object
	searchResultType         *graphql.Object

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

	// Enhanced consensus types
	consensusDataType          *graphql.Object
	validatorStatsType         *graphql.Object
	validatorParticipationType *graphql.Object
	blockParticipationType     *graphql.Object
	roundAnalysisType          *graphql.Object
	roundDistributionType      *graphql.Object
	validatorSetType           *graphql.Object
	validatorActivityType      *graphql.Object
	validatorChangeType        *graphql.Object
	epochDataType              *graphql.Object
	validatorInfoEnhancedType  *graphql.Object
	candidateInfoType          *graphql.Object

	// Address indexing types
	contractCreationType              *graphql.Object
	internalTransactionType           *graphql.Object
	erc20TransferType                 *graphql.Object
	erc721TransferType                *graphql.Object
	contractCreationConnectionType    *graphql.Object
	internalTransactionConnectionType *graphql.Object
	erc20TransferConnectionType       *graphql.Object
	erc721TransferConnectionType      *graphql.Object

	// Contract verification types
	contractVerificationType *graphql.Object
)

func init() {
	initTypes()
}

// initCoreTypes initializes core blockchain types (Block, Transaction, Receipt, Log)
func initCoreTypes() {
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

	// DecodedLog type - represents decoded event log data
	decodedLogType = graphql.NewObject(graphql.ObjectConfig{
		Name: "DecodedLog",
		Fields: graphql.Fields{
			"eventName": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Name of the decoded event",
			},
			"args": &graphql.Field{
				Type:        graphql.String, // JSON string of arguments
				Description: "Decoded event arguments as JSON",
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
			"decoded": &graphql.Field{
				Type:        decodedLogType,
				Description: "Decoded event log data (if ABI is available)",
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
}

// initConnectionTypes initializes connection/pagination types
func initConnectionTypes() {
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
}

// initHistoricalDataTypes initializes historical balance tracking types
func initHistoricalDataTypes() {
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
}

func initTypes() {
	// Initialize core blockchain types
	initCoreTypes()

	// Initialize connection/pagination types
	initConnectionTypes()

	// Initialize historical data types
	initHistoricalDataTypes()

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
			"lastBlockTime": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Timestamp of the last block mined",
			},
			"percentage": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Float),
				Description: "Percentage of total blocks mined in the range",
			},
			"totalRewards": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total mining rewards (transaction fees) in Wei",
			},
		},
	})

	// TokenBalance type
	tokenBalanceType = graphql.NewObject(graphql.ObjectConfig{
		Name: "TokenBalance",
		Fields: graphql.Fields{
			"contractAddress": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "The token contract address",
			},
			"tokenType": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Token standard (ERC20, ERC721, ERC1155)",
			},
			"balance": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Token balance (for ERC20) or count (for NFTs)",
			},
			"tokenId": &graphql.Field{
				Type:        graphql.String,
				Description: "Token ID for ERC721/ERC1155, empty for ERC20",
			},
			"name": &graphql.Field{
				Type:        graphql.String,
				Description: "Token name (e.g., 'Wrapped Ether')",
			},
			"symbol": &graphql.Field{
				Type:        graphql.String,
				Description: "Token symbol (e.g., 'WETH')",
			},
			"decimals": &graphql.Field{
				Type:        graphql.Int,
				Description: "Number of decimals (for ERC20 only)",
			},
			"metadata": &graphql.Field{
				Type:        graphql.String,
				Description: "Additional token metadata as JSON string",
			},
		},
	})

	// SearchResult type
	searchResultType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "SearchResult",
		Description: "Unified search result across blocks, transactions, and addresses",
		Fields: graphql.Fields{
			"type": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Type of result: block, transaction, address, or contract",
			},
			"value": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "The matched value (hash, address, or block number)",
			},
			"label": &graphql.Field{
				Type:        graphql.String,
				Description: "Human-readable display label",
			},
			"metadata": &graphql.Field{
				Type:        graphql.String,
				Description: "Additional metadata as JSON string",
			},
		},
	})

	// GasStats type
	gasStatsType = graphql.NewObject(graphql.ObjectConfig{
		Name: "GasStats",
		Fields: graphql.Fields{
			"totalGasUsed": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total gas used in the range",
			},
			"totalGasLimit": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total gas limit in the range",
			},
			"averageGasUsed": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Average gas used per block",
			},
			"averageGasPrice": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Average gas price",
			},
			"blockCount": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Number of blocks in the range",
			},
			"transactionCount": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Number of transactions in the range",
			},
		},
	})

	// AddressGasStats type
	addressGasStatsType = graphql.NewObject(graphql.ObjectConfig{
		Name: "AddressGasStats",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "The address",
			},
			"totalGasUsed": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total gas used by this address",
			},
			"transactionCount": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Number of transactions",
			},
			"averageGasPerTx": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Average gas per transaction",
			},
			"totalFeesPaid": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total fees paid (gas * gasPrice)",
			},
		},
	})

	// NetworkMetrics type
	networkMetricsType = graphql.NewObject(graphql.ObjectConfig{
		Name: "NetworkMetrics",
		Fields: graphql.Fields{
			"tps": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Float),
				Description: "Transactions per second",
			},
			"blockTime": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Float),
				Description: "Average block time in seconds",
			},
			"totalBlocks": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total number of blocks",
			},
			"totalTransactions": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total number of transactions",
			},
			"averageBlockSize": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Average block size in gas",
			},
			"timePeriod": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Time period for this metric (in seconds)",
			},
		},
	})

	// AddressActivityStats type
	addressActivityStatsType = graphql.NewObject(graphql.ObjectConfig{
		Name: "AddressActivityStats",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "The address",
			},
			"transactionCount": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total number of transactions",
			},
			"totalGasUsed": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total gas used",
			},
			"lastActivityBlock": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Most recent block with activity",
			},
			"firstActivityBlock": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "First block with activity",
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
		Name:        "ProposalFilter",
		Description: "Filter criteria for querying proposals. All fields are optional.",
		Fields: graphql.InputObjectConfigFieldMap{
			"contract": &graphql.InputObjectFieldConfig{
				Type:        addressType, // Nullable - allows querying all proposals
				Description: "Filter by contract address. If not provided, returns proposals from all contracts.",
			},
			"status": &graphql.InputObjectFieldConfig{
				Type:        proposalStatusEnumType,
				Description: "Filter by proposal status. If not provided, returns proposals with any status.",
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

	// ========== Enhanced Consensus Types ==========

	// ValidatorInfoEnhanced type (for EpochData)
	validatorInfoEnhancedType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ValidatorInfoDetailed",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"index": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"blsPubKey": &graphql.Field{
				Type: bytesType,
			},
		},
	})

	// CandidateInfo type
	candidateInfoType = graphql.NewObject(graphql.ObjectConfig{
		Name: "CandidateInfo",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"diligence": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// EpochData type (enhanced)
	epochDataType = graphql.NewObject(graphql.ObjectConfig{
		Name: "EpochData",
		Fields: graphql.Fields{
			"epochNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"validatorCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"candidateCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"validators": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(validatorInfoEnhancedType))),
			},
			"candidates": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(candidateInfoType))),
			},
		},
	})

	// ConsensusData type
	consensusDataType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ConsensusData",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blockHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"round": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"prevRound": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"roundChanged": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"proposer": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"validators": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
			},
			"prepareSigners": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
			},
			"commitSigners": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
			},
			"prepareCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"commitCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"missedPrepare": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
			},
			"missedCommit": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
			},
			"vanityData": &graphql.Field{
				Type: bytesType,
			},
			"randaoReveal": &graphql.Field{
				Type: bytesType,
			},
			"gasTip": &graphql.Field{
				Type: bigIntType,
			},
			"epochInfo": &graphql.Field{
				Type: epochDataType,
			},
			"isEpochBoundary": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"participationRate": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Float),
			},
			"isHealthy": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
		},
	})

	// BlockParticipation type
	blockParticipationType = graphql.NewObject(graphql.ObjectConfig{
		Name: "BlockParticipation",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"wasProposer": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
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
		},
	})

	// ValidatorParticipation type
	validatorParticipationType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ValidatorParticipation",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"startBlock": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"endBlock": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"totalBlocks": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blocksProposed": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blocksCommitted": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blocksMissed": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"participationRate": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Float),
			},
			"blocks": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(blockParticipationType))),
			},
		},
	})

	// ValidatorStats type (enhanced)
	validatorStatsType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ValidatorStats",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"totalBlocks": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blocksProposed": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"preparesSigned": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"commitsSigned": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"preparesMissed": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"commitsMissed": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"participationRate": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Float),
			},
			"lastProposedBlock": &graphql.Field{
				Type: bigIntType,
			},
			"lastCommittedBlock": &graphql.Field{
				Type: bigIntType,
			},
			"lastSeenBlock": &graphql.Field{
				Type: bigIntType,
			},
		},
	})

	// RoundDistribution type
	roundDistributionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "RoundDistribution",
		Fields: graphql.Fields{
			"round": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"count": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"percentage": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Float),
			},
		},
	})

	// RoundAnalysis type
	roundAnalysisType = graphql.NewObject(graphql.ObjectConfig{
		Name: "RoundAnalysis",
		Fields: graphql.Fields{
			"startBlock": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"endBlock": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"totalBlocks": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blocksWithRoundChange": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"roundChangeRate": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Float),
			},
			"averageRound": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Float),
			},
			"maxRound": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"roundDistribution": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(roundDistributionType))),
			},
		},
	})

	// ValidatorSet type
	validatorSetType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ValidatorSet",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"validators": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
			},
			"epochNumber": &graphql.Field{
				Type: bigIntType,
			},
		},
	})

	// ValidatorActivity type
	validatorActivityType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ValidatorActivity",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"isActive": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"lastSeenBlock": &graphql.Field{
				Type: bigIntType,
			},
			"lastProposedBlock": &graphql.Field{
				Type: bigIntType,
			},
			"recentParticipationRate": &graphql.Field{
				Type: graphql.Float,
			},
		},
	})

	// ValidatorChange type
	validatorChangeType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ValidatorChange",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"epochNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"added": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
			},
			"removed": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(addressType))),
			},
			"totalValidators": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
		},
	})

	// ========== Address Indexing Types ==========

	// ContractCreation type
	contractCreationType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ContractCreation",
		Fields: graphql.Fields{
			"contractAddress": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"creator": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"bytecodeSize": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
		},
	})

	// InternalTransaction type
	internalTransactionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "InternalTransaction",
		Fields: graphql.Fields{
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"index": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"type": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			"from": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"to": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"value": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"gas": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"gasUsed": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"input": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"output": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"error": &graphql.Field{
				Type: graphql.String,
			},
			"depth": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
		},
	})

	// ERC20Transfer type
	erc20TransferType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ERC20Transfer",
		Fields: graphql.Fields{
			"contractAddress": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"from": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"to": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"value": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"logIndex": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// ERC721Transfer type
	erc721TransferType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ERC721Transfer",
		Fields: graphql.Fields{
			"contractAddress": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"from": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"to": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"tokenId": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"logIndex": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// Connection types
	contractCreationConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ContractCreationConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(contractCreationType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	internalTransactionConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "InternalTransactionConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(internalTransactionType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	erc20TransferConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ERC20TransferConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(erc20TransferType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	erc721TransferConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ERC721TransferConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(erc721TransferType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// Contract verification type
	contractVerificationType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ContractVerification",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"isVerified": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"name": &graphql.Field{
				Type: graphql.String,
			},
			"compilerVersion": &graphql.Field{
				Type: graphql.String,
			},
			"optimizationEnabled": &graphql.Field{
				Type: graphql.Boolean,
			},
			"optimizationRuns": &graphql.Field{
				Type: graphql.Int,
			},
			"sourceCode": &graphql.Field{
				Type: graphql.String,
			},
			"abi": &graphql.Field{
				Type: graphql.String,
			},
			"constructorArguments": &graphql.Field{
				Type: graphql.String,
			},
			"verifiedAt": &graphql.Field{
				Type: graphql.String,
			},
			"licenseType": &graphql.Field{
				Type: graphql.String,
			},
		},
	})
}
