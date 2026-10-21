package api

import (
	gql "github.com/graphql-go/graphql"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
)

var (
	wbftAggregatedSealType                 *gql.Object
	candidateType                          *gql.Object
	epochInfoType                          *gql.Object
	wbftBlockExtraType                     *gql.Object
	validatorSigningStatsType              *gql.Object
	validatorSigningActivityType           *gql.Object
	blockSignersType                       *gql.Object
	validatorSigningStatsConnectionType    *gql.Object
	validatorSigningActivityConnectionType *gql.Object
	epochSummaryType                       *gql.Object
	epochSummaryConnectionType             *gql.Object
	validatorInfoEnhancedType              *gql.Object
	candidateInfoType                      *gql.Object
	epochDataType                          *gql.Object
	consensusDataType                      *gql.Object
	blockParticipationType                 *gql.Object
	validatorParticipationType             *gql.Object
	validatorStatsType                     *gql.Object
	consensusBlockSubType                  *gql.Object
	consensusForkSubType                   *gql.Object
	consensusValidatorChangeSubType        *gql.Object
	consensusErrorSubType                  *gql.Object
)

// initTypes initializes the GraphQL types of WBFT consensus data.
func initTypes() {
	// ========== WBFT Consensus Types ==========

	// WBFTAggregatedSeal type
	wbftAggregatedSealType = gql.NewObject(gql.ObjectConfig{
		Name: "WBFTAggregatedSeal",
		Fields: gql.Fields{
			"sealers": &gql.Field{
				Type: gql.NewNonNull(graphql.BytesType),
			},
			"signature": &gql.Field{
				Type: gql.NewNonNull(graphql.BytesType),
			},
		},
	})

	// Candidate type
	candidateType = gql.NewObject(gql.ObjectConfig{
		Name: "Candidate",
		Fields: gql.Fields{
			"address": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"diligence": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
	})

	// EpochInfo type
	epochInfoType = gql.NewObject(gql.ObjectConfig{
		Name: "EpochInfo",
		Fields: gql.Fields{
			"epochNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"candidates": &gql.Field{
				Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(candidateType))),
			},
			"validators": &gql.Field{
				Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(gql.Int))),
			},
			"blsPublicKeys": &gql.Field{
				Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(graphql.BytesType))),
			},
			"validatorCount": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "Number of validators in this epoch",
			},
			"candidateCount": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "Number of candidates in this epoch",
			},
			"previousEpochValidatorCount": &gql.Field{
				Type:        gql.Int,
				Description: "Validator count from previous epoch (null for epoch 0)",
			},
			"timestamp": &gql.Field{
				Type:        graphql.BigIntType,
				Description: "Timestamp of the epoch boundary block",
			},
		},
	})

	// WBFTBlockExtra type
	wbftBlockExtraType = gql.NewObject(gql.ObjectConfig{
		Name: "WBFTBlockExtra",
		Fields: gql.Fields{
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"blockHash": &gql.Field{
				Type: gql.NewNonNull(graphql.HashType),
			},
			"randaoReveal": &gql.Field{
				Type: gql.NewNonNull(graphql.BytesType),
			},
			"prevRound": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"prevPreparedSeal": &gql.Field{
				Type: wbftAggregatedSealType,
			},
			"prevCommittedSeal": &gql.Field{
				Type: wbftAggregatedSealType,
			},
			"round": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"preparedSeal": &gql.Field{
				Type: wbftAggregatedSealType,
			},
			"committedSeal": &gql.Field{
				Type: wbftAggregatedSealType,
			},
			"gasTip": &gql.Field{
				Type: graphql.BigIntType,
			},
			"epochInfo": &gql.Field{
				Type: epochInfoType,
			},
			"timestamp": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
	})

	// ValidatorSigningStats type
	validatorSigningStatsType = gql.NewObject(gql.ObjectConfig{
		Name: "ValidatorSigningStats",
		Fields: gql.Fields{
			"validatorAddress": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"validatorIndex": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"prepareSignCount": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"prepareMissCount": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"commitSignCount": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"commitMissCount": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"fromBlock": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"toBlock": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"signingRate": &gql.Field{
				Type: gql.NewNonNull(gql.Float),
			},
			"blocksProposed": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Number of blocks proposed by this validator",
			},
			"totalBlocks": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Total number of blocks in the query range",
			},
			"proposalRate": &gql.Field{
				Type:        gql.Float,
				Description: "Block proposal rate percentage",
			},
		},
	})

	// ValidatorSigningActivity type
	validatorSigningActivityType = gql.NewObject(gql.ObjectConfig{
		Name: "ValidatorSigningActivity",
		Fields: gql.Fields{
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"blockHash": &gql.Field{
				Type: gql.NewNonNull(graphql.HashType),
			},
			"validatorAddress": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"validatorIndex": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"signedPrepare": &gql.Field{
				Type: gql.NewNonNull(gql.Boolean),
			},
			"signedCommit": &gql.Field{
				Type: gql.NewNonNull(gql.Boolean),
			},
			"round": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"timestamp": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
	})

	// BlockSigners type
	blockSignersType = gql.NewObject(gql.ObjectConfig{
		Name: "BlockSigners",
		Fields: gql.Fields{
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"preparers": &gql.Field{
				Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(graphql.AddressType))),
			},
			"committers": &gql.Field{
				Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(graphql.AddressType))),
			},
		},
	})

	// ValidatorSigningStatsConnection type
	validatorSigningStatsConnectionType = gql.NewObject(gql.ObjectConfig{
		Name: "ValidatorSigningStatsConnection",
		Fields: gql.Fields{
			"nodes": &gql.Field{
				Type: gql.NewList(gql.NewNonNull(validatorSigningStatsType)),
			},
			"totalCount": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"pageInfo": &gql.Field{
				Type: gql.NewNonNull(graphql.PageInfoType()),
			},
		},
	})

	// ValidatorSigningActivityConnection type
	validatorSigningActivityConnectionType = gql.NewObject(gql.ObjectConfig{
		Name: "ValidatorSigningActivityConnection",
		Fields: gql.Fields{
			"nodes": &gql.Field{
				Type: gql.NewList(gql.NewNonNull(validatorSigningActivityType)),
			},
			"totalCount": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"pageInfo": &gql.Field{
				Type: gql.NewNonNull(graphql.PageInfoType()),
			},
		},
	})

	// EpochSummary type (lightweight epoch data for list queries)
	epochSummaryType = gql.NewObject(gql.ObjectConfig{
		Name: "EpochSummary",
		Fields: gql.Fields{
			"epochNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"validatorCount": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"candidateCount": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"timestamp": &gql.Field{
				Type: graphql.BigIntType,
			},
		},
	})

	// EpochSummaryConnection type
	epochSummaryConnectionType = gql.NewObject(gql.ObjectConfig{
		Name: "EpochSummaryConnection",
		Fields: gql.Fields{
			"nodes": &gql.Field{
				Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(epochSummaryType))),
			},
			"totalCount": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"pageInfo": &gql.Field{
				Type: gql.NewNonNull(graphql.PageInfoType()),
			},
		},
	})

	// ========== Enhanced Consensus Types ==========

	// ValidatorInfoEnhanced type (for EpochData)
	validatorInfoEnhancedType = gql.NewObject(gql.ObjectConfig{
		Name: "ValidatorInfoDetailed",
		Fields: gql.Fields{
			"address": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"index": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"blsPubKey": &gql.Field{
				Type: graphql.BytesType,
			},
		},
	})

	// CandidateInfo type
	candidateInfoType = gql.NewObject(gql.ObjectConfig{
		Name: "CandidateInfo",
		Fields: gql.Fields{
			"address": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"diligence": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
	})

	// EpochData type (enhanced)
	epochDataType = gql.NewObject(gql.ObjectConfig{
		Name: "EpochData",
		Fields: gql.Fields{
			"epochNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"validatorCount": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"candidateCount": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"validators": &gql.Field{
				Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(validatorInfoEnhancedType))),
			},
			"candidates": &gql.Field{
				Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(candidateInfoType))),
			},
		},
	})

	// ConsensusData type
	consensusDataType = gql.NewObject(gql.ObjectConfig{
		Name: "ConsensusData",
		Fields: gql.Fields{
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"blockHash": &gql.Field{
				Type: gql.NewNonNull(graphql.HashType),
			},
			"round": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"prevRound": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"roundChanged": &gql.Field{
				Type: gql.NewNonNull(gql.Boolean),
			},
			"proposer": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"validators": &gql.Field{
				Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(graphql.AddressType))),
			},
			"prepareSigners": &gql.Field{
				Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(graphql.AddressType))),
			},
			"commitSigners": &gql.Field{
				Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(graphql.AddressType))),
			},
			"prepareCount": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"commitCount": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"missedPrepare": &gql.Field{
				Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(graphql.AddressType))),
			},
			"missedCommit": &gql.Field{
				Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(graphql.AddressType))),
			},
			"vanityData": &gql.Field{
				Type: graphql.BytesType,
			},
			"randaoReveal": &gql.Field{
				Type: graphql.BytesType,
			},
			"gasTip": &gql.Field{
				Type: graphql.BigIntType,
			},
			"epochInfo": &gql.Field{
				Type: epochDataType,
			},
			"isEpochBoundary": &gql.Field{
				Type: gql.NewNonNull(gql.Boolean),
			},
			"timestamp": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"participationRate": &gql.Field{
				Type: gql.NewNonNull(gql.Float),
			},
			"isHealthy": &gql.Field{
				Type: gql.NewNonNull(gql.Boolean),
			},
		},
	})

	// BlockParticipation type
	blockParticipationType = gql.NewObject(gql.ObjectConfig{
		Name: "BlockParticipation",
		Fields: gql.Fields{
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"wasProposer": &gql.Field{
				Type: gql.NewNonNull(gql.Boolean),
			},
			"signedPrepare": &gql.Field{
				Type: gql.NewNonNull(gql.Boolean),
			},
			"signedCommit": &gql.Field{
				Type: gql.NewNonNull(gql.Boolean),
			},
			"round": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
		},
	})

	// ValidatorParticipation type
	validatorParticipationType = gql.NewObject(gql.ObjectConfig{
		Name: "ValidatorParticipation",
		Fields: gql.Fields{
			"address": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"startBlock": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"endBlock": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"totalBlocks": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"blocksProposed": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"blocksCommitted": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"blocksMissed": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"participationRate": &gql.Field{
				Type: gql.NewNonNull(gql.Float),
			},
			"blocks": &gql.Field{
				Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(blockParticipationType))),
			},
		},
	})

	// ValidatorStats type (enhanced)
	validatorStatsType = gql.NewObject(gql.ObjectConfig{
		Name: "ValidatorStats",
		Fields: gql.Fields{
			"address": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"totalBlocks": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"blocksProposed": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"preparesSigned": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"commitsSigned": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"preparesMissed": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"commitsMissed": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"participationRate": &gql.Field{
				Type: gql.NewNonNull(gql.Float),
			},
			"lastProposedBlock": &gql.Field{
				Type: graphql.BigIntType,
			},
			"lastCommittedBlock": &gql.Field{
				Type: graphql.BigIntType,
			},
			"lastSeenBlock": &gql.Field{
				Type: graphql.BigIntType,
			},
		},
	})

	// ========== Consensus Subscription Types ==========

	// ConsensusBlockSub type - for consensusBlock subscription
	consensusBlockSubType = gql.NewObject(gql.ObjectConfig{
		Name:        "ConsensusBlockSub",
		Description: "Real-time consensus block data from subscription",
		Fields: gql.Fields{
			"blockNumber": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Block number",
			},
			"blockHash": &gql.Field{
				Type:        gql.NewNonNull(graphql.HashType),
				Description: "Block hash",
			},
			"timestamp": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Block timestamp",
			},
			"round": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "Consensus round number",
			},
			"prevRound": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "Previous round number",
			},
			"roundChanged": &gql.Field{
				Type:        gql.NewNonNull(gql.Boolean),
				Description: "Whether round changed from 0",
			},
			"proposer": &gql.Field{
				Type:        gql.NewNonNull(graphql.AddressType),
				Description: "Block proposer address",
			},
			"validatorCount": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "Total number of validators",
			},
			"prepareCount": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "Number of prepare signatures",
			},
			"commitCount": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "Number of commit signatures",
			},
			"participationRate": &gql.Field{
				Type:        gql.NewNonNull(gql.Float),
				Description: "Validator participation rate (0-100)",
			},
			"missedValidatorRate": &gql.Field{
				Type:        gql.NewNonNull(gql.Float),
				Description: "Rate of validators who missed commit (0-100)",
			},
			"isEpochBoundary": &gql.Field{
				Type:        gql.NewNonNull(gql.Boolean),
				Description: "Whether this block is at epoch boundary",
			},
			"epochNumber": &gql.Field{
				Type:        graphql.BigIntType,
				Description: "Epoch number (only at epoch boundaries)",
			},
			"epochValidators": &gql.Field{
				Type:        gql.NewList(gql.NewNonNull(graphql.AddressType)),
				Description: "Validator addresses for the epoch (only at epoch boundaries)",
			},
		},
	})

	// ConsensusForkSub type - for consensusFork subscription
	consensusForkSubType = gql.NewObject(gql.ObjectConfig{
		Name:        "ConsensusForkSub",
		Description: "Chain fork detection event from subscription",
		Fields: gql.Fields{
			"forkBlockNumber": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Block number where fork occurred",
			},
			"forkBlockHash": &gql.Field{
				Type:        gql.NewNonNull(graphql.HashType),
				Description: "Hash of the fork block",
			},
			"chain1Hash": &gql.Field{
				Type:        gql.NewNonNull(graphql.HashType),
				Description: "Hash of chain 1 tip",
			},
			"chain1Height": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Height of chain 1",
			},
			"chain1Weight": &gql.Field{
				Type:        gql.NewNonNull(gql.String),
				Description: "Total weight/difficulty of chain 1",
			},
			"chain2Hash": &gql.Field{
				Type:        gql.NewNonNull(graphql.HashType),
				Description: "Hash of chain 2 tip",
			},
			"chain2Height": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Height of chain 2",
			},
			"chain2Weight": &gql.Field{
				Type:        gql.NewNonNull(gql.String),
				Description: "Total weight/difficulty of chain 2",
			},
			"resolved": &gql.Field{
				Type:        gql.NewNonNull(gql.Boolean),
				Description: "Whether the fork has been resolved",
			},
			"winningChain": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "Winning chain (1 or 2, 0 if unresolved)",
			},
			"detectedAt": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Unix timestamp when fork was detected",
			},
			"detectionLag": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Blocks between fork and detection",
			},
		},
	})

	// ConsensusValidatorChangeSub type - for consensusValidatorChange subscription
	consensusValidatorChangeSubType = gql.NewObject(gql.ObjectConfig{
		Name:        "ConsensusValidatorChangeSub",
		Description: "Validator set change event from subscription",
		Fields: gql.Fields{
			"blockNumber": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Block number where change occurred",
			},
			"blockHash": &gql.Field{
				Type:        gql.NewNonNull(graphql.HashType),
				Description: "Block hash",
			},
			"timestamp": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Block timestamp",
			},
			"epochNumber": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Epoch number",
			},
			"isEpochBoundary": &gql.Field{
				Type:        gql.NewNonNull(gql.Boolean),
				Description: "Whether this is an epoch boundary",
			},
			"changeType": &gql.Field{
				Type:        gql.NewNonNull(gql.String),
				Description: "Type of change: added, removed, replaced, reordered",
			},
			"previousValidatorCount": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "Previous validator count",
			},
			"newValidatorCount": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "New validator count",
			},
			"addedValidators": &gql.Field{
				Type:        gql.NewList(gql.NewNonNull(graphql.AddressType)),
				Description: "Addresses of added validators",
			},
			"removedValidators": &gql.Field{
				Type:        gql.NewList(gql.NewNonNull(graphql.AddressType)),
				Description: "Addresses of removed validators",
			},
			"validatorSet": &gql.Field{
				Type:        gql.NewList(gql.NewNonNull(graphql.AddressType)),
				Description: "Current validator set after change",
			},
			"additionalInfo": &gql.Field{
				Type:        gql.String,
				Description: "Additional information (JSON encoded)",
			},
		},
	})

	// ConsensusErrorSub type - for consensusError subscription
	consensusErrorSubType = gql.NewObject(gql.ObjectConfig{
		Name:        "ConsensusErrorSub",
		Description: "Consensus error or anomaly event from subscription",
		Fields: gql.Fields{
			"blockNumber": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Block number where error occurred",
			},
			"blockHash": &gql.Field{
				Type:        gql.NewNonNull(graphql.HashType),
				Description: "Block hash",
			},
			"timestamp": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Block timestamp",
			},
			"errorType": &gql.Field{
				Type:        gql.NewNonNull(gql.String),
				Description: "Error type: round_change, missed_validators, low_participation, etc.",
			},
			"severity": &gql.Field{
				Type:        gql.NewNonNull(gql.String),
				Description: "Severity level: critical, high, medium, low",
			},
			"errorMessage": &gql.Field{
				Type:        gql.NewNonNull(gql.String),
				Description: "Human-readable error message",
			},
			"round": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "Consensus round number",
			},
			"expectedValidators": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "Expected number of validators",
			},
			"actualSigners": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "Actual number of signers",
			},
			"participationRate": &gql.Field{
				Type:        gql.NewNonNull(gql.Float),
				Description: "Current participation rate (0-100)",
			},
			"consensusImpacted": &gql.Field{
				Type:        gql.NewNonNull(gql.Boolean),
				Description: "Whether consensus was impacted",
			},
			"recoveryTime": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Blocks until recovery (0 if not applicable)",
			},
			"missedValidators": &gql.Field{
				Type:        gql.NewList(gql.NewNonNull(graphql.AddressType)),
				Description: "Addresses of validators who missed",
			},
			"errorDetails": &gql.Field{
				Type:        gql.String,
				Description: "Additional error details (JSON encoded)",
			},
		},
	})
}
