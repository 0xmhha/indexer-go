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

	// DecodedParam type
	decodedParamType *graphql.Object

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

	// BlockRangeResult type for efficient bulk data transfer
	blockRangeResultType *graphql.Object

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
	transactionDirectionEnum        *graphql.Enum

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
	addressStatsType         *graphql.Object

	// System contract types

	// Address indexing types
	contractCreationType              *graphql.Object
	addressOverviewType               *graphql.Object
	internalTransactionType           *graphql.Object
	erc20TransferType                 *graphql.Object
	erc721TransferType                *graphql.Object
	nftOwnershipType                  *graphql.Object
	contractCreationConnectionType    *graphql.Object
	internalTransactionConnectionType *graphql.Object
	erc20TransferConnectionType       *graphql.Object
	erc721TransferConnectionType      *graphql.Object
	nftOwnershipConnectionType        *graphql.Object

	// Contract verification types
	contractVerificationType *graphql.Object

	// EIP-7702 SetCode types
	setCodeAuthBaseType                *graphql.Object // SetCodeAuthorization (basic, for Transaction.authorizationList)
	setCodeAuthorizationType           *graphql.Object // SetCodeAuthorizationWithTx (extended, with tx context)
	setCodeAuthorizationConnectionType *graphql.Object
	addressSetCodeInfoType             *graphql.Object

	// ERC-7579 Module types
	moduleTypeEnum                *graphql.Enum
	installedModuleType           *graphql.Object
	installedModuleConnectionType *graphql.Object
	moduleStatsType               *graphql.Object
	moduleStatsConnectionType     *graphql.Object
	accountModulesType            *graphql.Object
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

	// DecodedParam type - represents a decoded event parameter
	decodedParamType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "DecodedParam",
		Description: "A decoded event parameter",
		Fields: graphql.Fields{
			"name": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Parameter name (e.g., 'from', 'to', 'value')",
			},
			"type": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Solidity type (e.g., 'address', 'uint256', 'bytes32')",
			},
			"value": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Decoded value as string",
			},
			"indexed": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether this parameter is indexed",
			},
		},
	})

	// DecodedLog type - represents decoded event log data
	decodedLogType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "DecodedLog",
		Description: "Decoded event log with structured parameters",
		Fields: graphql.Fields{
			"eventName": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Name of the decoded event (e.g., 'Transfer')",
			},
			"eventSignature": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Full event signature (e.g., 'Transfer(address,address,uint256)')",
			},
			"params": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(decodedParamType))),
				Description: "Array of decoded parameters",
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
			"contractAddress": &graphql.Field{
				Type:        addressType,
				Description: "Contract address created by this transaction (null if not a contract creation)",
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
			"blockTimestamp": &graphql.Field{
				Type:        bigIntType,
				Description: "Timestamp of the block containing this transaction",
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
			// Alias for frontend compatibility (subscription uses txCount)
			"txCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Alias for transactionCount",
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if source, ok := p.Source.(map[string]interface{}); ok {
						return source["transactionCount"], nil
					}
					return 0, nil
				},
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

	// BlockRangeResult type for efficient bulk data transfer
	blockRangeResultType = graphql.NewObject(graphql.ObjectConfig{
		Name: "BlockRangeResult",
		Fields: graphql.Fields{
			"blocks": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(blockType))),
				Description: "List of blocks in the range",
			},
			"startNumber": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Start block number in the result",
			},
			"endNumber": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "End block number in the result",
			},
			"count": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Number of blocks returned",
			},
			"hasMore": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether there are more blocks after endNumber",
			},
			"latestHeight": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Latest known block height for sync status",
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

	// Initialize input/filter types
	initInputTypes()

	// Initialize statistics and analytics types
	initStatsTypes()

	// Initialize governance and system contract types

	// Initialize miscellaneous types (must be before consensus types due to contractVerificationType dependency)
	initMiscTypes()

	// Initialize token metadata types (must be before consensus types due to tokenMetadataType dependency in addressOverviewType)
	initTokenMetadataTypes()

	// Initialize consensus/WBFT types (uses contractVerificationType from initMiscTypes)
	initAddressIndexingTypes()

	// Initialize token transfer types
	initTokenTypes()

	// Initialize multi-chain types
	initMultiChainTypes()

	// Initialize reorganization and orphaned block types (uses core types)
	initReorgTypes()

	// Initialize EIP-7702 SetCode types
	initSetCodeTypes()

	// Initialize ERC-7579 Module types
	initModuleTypes()

}

// initInputTypes initializes GraphQL input types for filtering and pagination
func initInputTypes() {
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
				Type:        graphql.Int,
				Description: "Items to skip from the start; costs time proportional to the offset. Ignored when after is set.",
			},
			"after": &graphql.InputObjectFieldConfig{
				Type:        graphql.String,
				Description: "Continue after the page that returned this cursor (pageInfo.endCursor); the same cost at any depth.",
			},
		},
	})

	transactionDirectionEnum = graphql.NewEnum(graphql.EnumConfig{
		Name:        "TransactionDirection",
		Description: "Transaction direction filter",
		Values: graphql.EnumValueConfigMap{
			"SENT": &graphql.EnumValueConfig{
				Value:       "SENT",
				Description: "Transactions sent from the address",
			},
			"RECEIVED": &graphql.EnumValueConfig{
				Value:       "RECEIVED",
				Description: "Transactions received by the address",
			},
			"ALL": &graphql.EnumValueConfig{
				Value:       "ALL",
				Description: "All transactions (sent and received)",
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
			"isFeeDelegated": &graphql.InputObjectFieldConfig{
				Type:        graphql.Boolean,
				Description: "Filter by fee delegation status",
			},
			"methodId": &graphql.InputObjectFieldConfig{
				Type:        graphql.String,
				Description: "Filter by function selector (first 4 bytes of input data)",
			},
			"minGasUsed": &graphql.InputObjectFieldConfig{
				Type:        bigIntType,
				Description: "Filter by minimum gas used",
			},
			"maxGasUsed": &graphql.InputObjectFieldConfig{
				Type:        bigIntType,
				Description: "Filter by maximum gas used",
			},
			"direction": &graphql.InputObjectFieldConfig{
				Type:        transactionDirectionEnum,
				Description: "Filter by transaction direction (overrides txType)",
			},
			"fromTime": &graphql.InputObjectFieldConfig{
				Type:        bigIntType,
				Description: "Start time filter (Unix timestamp, overrides fromBlock)",
			},
			"toTime": &graphql.InputObjectFieldConfig{
				Type:        bigIntType,
				Description: "End time filter (Unix timestamp, overrides toBlock)",
			},
		},
	})
}

// initStatsTypes initializes GraphQL types for statistics and analytics
func initStatsTypes() {
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
			"address": &graphql.Field{
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

	// AddressStats type
	addressStatsType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "AddressStats",
		Description: "Aggregated statistics for an address",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"totalTransactions": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"sentCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"receivedCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"successCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"failedCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"totalGasUsed": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"totalGasCost": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"totalValueSent": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"totalValueReceived": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"contractInteractionCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"uniqueAddressCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"firstTransactionTimestamp": &graphql.Field{
				Type: bigIntType,
			},
			"lastTransactionTimestamp": &graphql.Field{
				Type: bigIntType,
			},
		},
	})
}

// initAddressIndexingTypes initializes GraphQL types for address indexing
func initAddressIndexingTypes() {
	// ========== Address Indexing Types ==========

	// ContractCreation type
	contractCreationType = graphql.NewObject(graphql.ObjectConfig{
		Name: "ContractCreation",
		Fields: graphql.Fields{
			"contractAddress": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"name": &graphql.Field{
				Type: graphql.String, // nullable - only available if contract is verified
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

	// AddressOverview type - comprehensive summary of an address
	addressOverviewType = graphql.NewObject(graphql.ObjectConfig{
		Name: "AddressOverview",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"isContract": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"balance": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"sentCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"receivedCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"internalTxCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"erc20TokenCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"erc721TokenCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"contractInfo": &graphql.Field{
				Type: contractCreationType,
			},
			"verificationInfo": &graphql.Field{
				Type: contractVerificationType,
			},
			"firstSeen": &graphql.Field{
				Type: bigIntType,
			},
			"lastSeen": &graphql.Field{
				Type: bigIntType,
			},
			// New fields for enhanced address overview
			"currentBalance": &graphql.Field{
				Type:        bigIntType,
				Description: "Current balance from RPC (real-time)",
			},
			"nonce": &graphql.Field{
				Type:        bigIntType,
				Description: "Current nonce (transaction count) from RPC",
			},
			"isToken": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether the address is a token contract",
			},
			"tokenMetadata": &graphql.Field{
				Type:        tokenMetadataType,
				Description: "Token metadata if the address is a token contract",
			},
			// EIP-7702 SetCode fields
			"hasDelegation": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether the address has an active EIP-7702 delegation",
			},
			"delegationTarget": &graphql.Field{
				Type:        addressType,
				Description: "Target address of EIP-7702 delegation (if any)",
			},
			"asAuthorityCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Number of times this address was used as SetCode authority",
			},
			"asTargetCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Number of times this address was used as SetCode target",
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
}

// initTokenTypes initializes GraphQL types for token transfers (ERC20, ERC721)
func initTokenTypes() {
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

	// NFTOwnership type
	nftOwnershipType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "NFTOwnership",
		Description: "Represents an NFT owned by an address",
		Fields: graphql.Fields{
			"contractAddress": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "NFT contract address",
			},
			"tokenId": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Token ID",
			},
			"owner": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Owner address",
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

	// NFTOwnershipConnection type
	nftOwnershipConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "NFTOwnershipConnection",
		Description: "Paginated list of NFTs owned by an address",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(nftOwnershipType)),
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

// initMiscTypes initializes miscellaneous GraphQL types
func initMiscTypes() {
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

// initSetCodeTypes initializes GraphQL types for EIP-7702 SetCode transactions
func initSetCodeTypes() {
	// SetCodeAuthorization base type (used in Transaction.authorizationList)
	setCodeAuthBaseType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "SetCodeAuthorization",
		Description: "EIP-7702 SetCode authorization entry",
		Fields: graphql.Fields{
			"chainId": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Chain ID for replay protection",
			},
			"address": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Delegate target contract address",
			},
			"nonce": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Authorization nonce",
			},
			"yParity": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Y parity of signature (0 or 1)",
			},
			"r": &graphql.Field{
				Type:        graphql.NewNonNull(bytesType),
				Description: "R signature value",
			},
			"s": &graphql.Field{
				Type:        graphql.NewNonNull(bytesType),
				Description: "S signature value",
			},
			"authority": &graphql.Field{
				Type:        addressType,
				Description: "Recovered signer address (authority who signed this authorization)",
			},
		},
	})

	// Add authorizationList field to Transaction type
	transactionType.AddFieldConfig("authorizationList", &graphql.Field{
		Type:        graphql.NewList(graphql.NewNonNull(setCodeAuthBaseType)),
		Description: "Authorization list for EIP-7702 SetCode transactions (type 0x04)",
	})

	// SetCodeAuthorization type with transaction reference
	setCodeAuthorizationType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "SetCodeAuthorizationWithTx",
		Description: "EIP-7702 SetCode authorization with transaction context",
		Fields: graphql.Fields{
			"txHash": &graphql.Field{
				Type:        graphql.NewNonNull(hashType),
				Description: "Transaction hash containing this authorization",
			},
			"blockNumber": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block number",
			},
			"blockHash": &graphql.Field{
				Type:        graphql.NewNonNull(hashType),
				Description: "Block hash",
			},
			"transactionIndex": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Transaction index in block",
			},
			"authorizationIndex": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Index of this authorization in the transaction's authorization list",
			},
			"chainId": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Chain ID for replay protection",
			},
			"address": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Target address that will have code installed (delegation source)",
			},
			"nonce": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Nonce of the authorizer account",
			},
			"yParity": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Y parity of signature (0 or 1)",
			},
			"r": &graphql.Field{
				Type:        graphql.NewNonNull(bytesType),
				Description: "R signature value",
			},
			"s": &graphql.Field{
				Type:        graphql.NewNonNull(bytesType),
				Description: "S signature value",
			},
			"authority": &graphql.Field{
				Type:        addressType,
				Description: "Recovered signer address (authority who signed this authorization)",
			},
			"applied": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether this authorization was successfully applied",
			},
			"error": &graphql.Field{
				Type:        graphql.String,
				Description: "Error message if authorization was not applied",
			},
			"timestamp": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block timestamp",
			},
		},
	})

	// SetCodeAuthorizationConnection for pagination
	setCodeAuthorizationConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "SetCodeAuthorizationConnection",
		Description: "Paginated list of SetCode authorizations",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(setCodeAuthorizationType))),
				Description: "List of SetCode authorizations",
			},
			"totalCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Total count of matching authorizations",
			},
			"pageInfo": &graphql.Field{
				Type:        graphql.NewNonNull(pageInfoType),
				Description: "Pagination information",
			},
		},
	})

	// AddressSetCodeInfo type for SetCode-specific address information
	addressSetCodeInfoType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "AddressSetCodeInfo",
		Description: "EIP-7702 SetCode information for an address",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "The address being queried",
			},
			"hasDelegation": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether this address currently has an active delegation",
			},
			"delegationTarget": &graphql.Field{
				Type:        addressType,
				Description: "Current delegation target address (if hasDelegation is true)",
			},
			"asTargetCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Number of times this address received delegation (as target)",
			},
			"asAuthorityCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Number of times this address signed authorization (as authority)",
			},
			"lastActivityBlock": &graphql.Field{
				Type:        bigIntType,
				Description: "Block number of most recent SetCode activity",
			},
			"lastActivityTimestamp": &graphql.Field{
				Type:        bigIntType,
				Description: "Timestamp of most recent SetCode activity",
			},
		},
	})

}

// initModuleTypes initializes ERC-7579 Module types
func initModuleTypes() {
	// ModuleType enum
	moduleTypeEnum = graphql.NewEnum(graphql.EnumConfig{
		Name:        "ModuleType",
		Description: "ERC-7579 module type",
		Values: graphql.EnumValueConfigMap{
			"VALIDATOR": &graphql.EnumValueConfig{
				Value:       "VALIDATOR",
				Description: "Validator module (type 1) - validates user operations",
			},
			"EXECUTOR": &graphql.EnumValueConfig{
				Value:       "EXECUTOR",
				Description: "Executor module (type 2) - executes operations on behalf of the account",
			},
			"FALLBACK": &graphql.EnumValueConfig{
				Value:       "FALLBACK",
				Description: "Fallback module (type 3) - handles fallback calls",
			},
			"HOOK": &graphql.EnumValueConfig{
				Value:       "HOOK",
				Description: "Hook module (type 4) - provides pre/post execution hooks",
			},
		},
	})

	// InstalledModule type
	installedModuleType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "InstalledModule",
		Description: "A module installed on an ERC-7579 modular smart account",
		Fields: graphql.Fields{
			"account": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Smart account address",
			},
			"module": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Module contract address",
			},
			"moduleType": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Module type (validator, executor, fallback, hook)",
			},
			"installedAt": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block number when the module was installed",
			},
			"installedTx": &graphql.Field{
				Type:        graphql.NewNonNull(hashType),
				Description: "Transaction hash of the install event",
			},
			"active": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether the module is currently active",
			},
			"removedAt": &graphql.Field{
				Type:        bigIntType,
				Description: "Block number when the module was removed (null if still active)",
			},
			"removedTx": &graphql.Field{
				Type:        hashType,
				Description: "Transaction hash of the uninstall event (null if still active)",
			},
			"timestamp": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Timestamp of the install event",
			},
		},
	})

	// InstalledModuleConnection for pagination
	installedModuleConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "InstalledModuleConnection",
		Description: "Paginated list of installed modules",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(installedModuleType))),
				Description: "List of installed modules",
			},
			"totalCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Total count of matching modules",
			},
			"pageInfo": &graphql.Field{
				Type:        graphql.NewNonNull(pageInfoType),
				Description: "Pagination information",
			},
		},
	})

	// ModuleStats type
	moduleStatsType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "ModuleStats",
		Description: "Aggregate statistics for a module contract",
		Fields: graphql.Fields{
			"module": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Module contract address",
			},
			"moduleType": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Module type (validator, executor, fallback, hook)",
			},
			"totalInstalls": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total number of installations across all accounts",
			},
			"activeInstalls": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Number of currently active installations",
			},
		},
	})

	// ModuleStatsConnection for pagination
	moduleStatsConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "ModuleStatsConnection",
		Description: "Paginated list of module stats",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(moduleStatsType))),
				Description: "List of module stats",
			},
			"totalCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Total count",
			},
			"pageInfo": &graphql.Field{
				Type:        graphql.NewNonNull(pageInfoType),
				Description: "Pagination information",
			},
		},
	})

	// AccountModules type
	accountModulesType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "AccountModules",
		Description: "All modules installed on a smart account, grouped by type",
		Fields: graphql.Fields{
			"account": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Smart account address",
			},
			"validators": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(installedModuleType))),
				Description: "Validator modules",
			},
			"executors": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(installedModuleType))),
				Description: "Executor modules",
			},
			"fallbacks": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(installedModuleType))),
				Description: "Fallback modules",
			},
			"hooks": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(installedModuleType))),
				Description: "Hook modules",
			},
		},
	})
}
