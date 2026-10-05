package api

import (
	gql "github.com/graphql-go/graphql"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
)

var (
	proposalStatusEnumType            *gql.Enum
	mintEventType                     *gql.Object
	burnEventType                     *gql.Object
	minterConfigEventType             *gql.Object
	proposalType                      *gql.Object
	proposalVoteType                  *gql.Object
	gasTipUpdateEventType             *gql.Object
	blacklistEventType                *gql.Object
	validatorChangeEventType          *gql.Object
	memberChangeEventType             *gql.Object
	emergencyPauseEventType           *gql.Object
	depositMintProposalType           *gql.Object
	maxProposalsUpdateEventType       *gql.Object
	proposalExecutionSkippedEventType *gql.Object
	minterInfoType                    *gql.Object
	validatorInfoType                 *gql.Object
	systemContractEventFilterType     *gql.InputObject
	proposalFilterType                *gql.InputObject
	mintEventConnectionType           *gql.Object
	burnEventConnectionType           *gql.Object
	proposalConnectionType            *gql.Object
)

// initTypes initializes the GraphQL types of system contract data.
func initTypes() {
	// ProposalStatus enum
	proposalStatusEnumType = gql.NewEnum(gql.EnumConfig{
		Name: "ProposalStatus",
		Values: gql.EnumValueConfigMap{
			"NONE": &gql.EnumValueConfig{
				Value: "NONE",
			},
			"VOTING": &gql.EnumValueConfig{
				Value: "VOTING",
			},
			"APPROVED": &gql.EnumValueConfig{
				Value: "APPROVED",
			},
			"EXECUTED": &gql.EnumValueConfig{
				Value: "EXECUTED",
			},
			"CANCELLED": &gql.EnumValueConfig{
				Value: "CANCELLED",
			},
			"EXPIRED": &gql.EnumValueConfig{
				Value: "EXPIRED",
			},
			"FAILED": &gql.EnumValueConfig{
				Value: "FAILED",
			},
			"REJECTED": &gql.EnumValueConfig{
				Value: "REJECTED",
			},
		},
	})

	// MintEvent type
	mintEventType = gql.NewObject(gql.ObjectConfig{
		Name: "MintEvent",
		Fields: gql.Fields{
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"transactionHash": &gql.Field{
				Type: gql.NewNonNull(graphql.HashType),
			},
			// Alias for frontend compatibility
			"txHash": &gql.Field{
				Type:        gql.NewNonNull(graphql.HashType),
				Description: "Alias for transactionHash",
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					if source, ok := p.Source.(map[string]interface{}); ok {
						return source["transactionHash"], nil
					}
					return nil, nil
				},
			},
			"minter": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"to": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"amount": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"timestamp": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
	})

	// BurnEvent type
	burnEventType = gql.NewObject(gql.ObjectConfig{
		Name: "BurnEvent",
		Fields: gql.Fields{
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"transactionHash": &gql.Field{
				Type: gql.NewNonNull(graphql.HashType),
			},
			// Alias for frontend compatibility
			"txHash": &gql.Field{
				Type:        gql.NewNonNull(graphql.HashType),
				Description: "Alias for transactionHash",
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					if source, ok := p.Source.(map[string]interface{}); ok {
						return source["transactionHash"], nil
					}
					return nil, nil
				},
			},
			"burner": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"amount": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"timestamp": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"withdrawalId": &gql.Field{
				Type: gql.String,
			},
			// Alias for frontend compatibility
			"burnTxId": &gql.Field{
				Type:        gql.String,
				Description: "Alias for withdrawalId - burn transaction identifier",
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					if source, ok := p.Source.(map[string]interface{}); ok {
						return source["withdrawalId"], nil
					}
					return nil, nil
				},
			},
		},
	})

	// MinterConfigEvent type
	minterConfigEventType = gql.NewObject(gql.ObjectConfig{
		Name: "MinterConfigEvent",
		Fields: gql.Fields{
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"transactionHash": &gql.Field{
				Type: gql.NewNonNull(graphql.HashType),
			},
			// Alias for frontend compatibility
			"txHash": &gql.Field{
				Type:        gql.NewNonNull(graphql.HashType),
				Description: "Alias for transactionHash",
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					if source, ok := p.Source.(map[string]interface{}); ok {
						return source["transactionHash"], nil
					}
					return nil, nil
				},
			},
			"minter": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"contract": &gql.Field{
				Type:        graphql.AddressType,
				Description: "Contract that emitted the event (NativeCoinAdapter or GovMasterMinter); null for records indexed before it was kept",
			},
			"allowance": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"action": &gql.Field{
				Type: gql.NewNonNull(gql.String),
			},
			// Derived field for frontend compatibility
			"isActive": &gql.Field{
				Type:        gql.NewNonNull(gql.Boolean),
				Description: "Whether the minter is active (derived from action field)",
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					if source, ok := p.Source.(map[string]interface{}); ok {
						action, _ := source["action"].(string)
						return action != "removed", nil
					}
					return false, nil
				},
			},
			"timestamp": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
	})

	// Proposal type
	proposalType = gql.NewObject(gql.ObjectConfig{
		Name: "Proposal",
		Fields: gql.Fields{
			"contract": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"proposalId": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"proposer": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"actionType": &gql.Field{
				Type: gql.NewNonNull(graphql.BytesType),
			},
			"callData": &gql.Field{
				Type: gql.NewNonNull(graphql.BytesType),
			},
			"memberVersion": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"requiredApprovals": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"approved": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"rejected": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"status": &gql.Field{
				Type: gql.NewNonNull(proposalStatusEnumType),
			},
			"createdAt": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"executedAt": &gql.Field{
				Type: graphql.BigIntType,
			},
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"transactionHash": &gql.Field{
				Type: gql.NewNonNull(graphql.HashType),
			},
		},
	})

	// ProposalVote type
	proposalVoteType = gql.NewObject(gql.ObjectConfig{
		Name: "ProposalVote",
		Fields: gql.Fields{
			"contract": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"proposalId": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"voter": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"approval": &gql.Field{
				Type: gql.NewNonNull(gql.Boolean),
			},
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"transactionHash": &gql.Field{
				Type: gql.NewNonNull(graphql.HashType),
			},
			"timestamp": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
	})

	// GasTipUpdateEvent type
	gasTipUpdateEventType = gql.NewObject(gql.ObjectConfig{
		Name: "GasTipUpdateEvent",
		Fields: gql.Fields{
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"transactionHash": &gql.Field{
				Type: gql.NewNonNull(graphql.HashType),
			},
			"oldTip": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"newTip": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"updater": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"timestamp": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
	})

	// BlacklistEvent type
	blacklistEventType = gql.NewObject(gql.ObjectConfig{
		Name: "BlacklistEvent",
		Fields: gql.Fields{
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"transactionHash": &gql.Field{
				Type: gql.NewNonNull(graphql.HashType),
			},
			"account": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"action": &gql.Field{
				Type: gql.NewNonNull(gql.String),
			},
			"proposalId": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"timestamp": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
	})

	// ValidatorChangeEvent type
	validatorChangeEventType = gql.NewObject(gql.ObjectConfig{
		Name: "ValidatorChangeEvent",
		Fields: gql.Fields{
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"transactionHash": &gql.Field{
				Type: gql.NewNonNull(graphql.HashType),
			},
			"validator": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"action": &gql.Field{
				Type: gql.NewNonNull(gql.String),
			},
			"oldValidator": &gql.Field{
				Type: graphql.AddressType,
			},
			"timestamp": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
	})

	// MemberChangeEvent type
	memberChangeEventType = gql.NewObject(gql.ObjectConfig{
		Name: "MemberChangeEvent",
		Fields: gql.Fields{
			"contract": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"transactionHash": &gql.Field{
				Type: gql.NewNonNull(graphql.HashType),
			},
			"member": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"action": &gql.Field{
				Type: gql.NewNonNull(gql.String),
			},
			"oldMember": &gql.Field{
				Type: graphql.AddressType,
			},
			"totalMembers": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"newQuorum": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"timestamp": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
	})

	// EmergencyPauseEvent type
	emergencyPauseEventType = gql.NewObject(gql.ObjectConfig{
		Name: "EmergencyPauseEvent",
		Fields: gql.Fields{
			"contract": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"transactionHash": &gql.Field{
				Type: gql.NewNonNull(graphql.HashType),
			},
			"proposalId": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"action": &gql.Field{
				Type: gql.NewNonNull(gql.String),
			},
			"timestamp": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
	})

	// DepositMintProposal type
	depositMintProposalType = gql.NewObject(gql.ObjectConfig{
		Name: "DepositMintProposal",
		Fields: gql.Fields{
			"proposalId": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"to": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"amount": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"depositId": &gql.Field{
				Type: gql.NewNonNull(gql.String),
			},
			"status": &gql.Field{
				Type: gql.NewNonNull(proposalStatusEnumType),
			},
			"blockNumber": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"transactionHash": &gql.Field{
				Type: gql.NewNonNull(graphql.HashType),
			},
			"timestamp": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
	})

	// MaxProposalsUpdateEvent type
	maxProposalsUpdateEventType = gql.NewObject(gql.ObjectConfig{
		Name: "MaxProposalsUpdateEvent",
		Fields: gql.Fields{
			"contract": &gql.Field{
				Type:        gql.NewNonNull(graphql.AddressType),
				Description: "Contract address",
			},
			"blockNumber": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Block number",
			},
			"transactionHash": &gql.Field{
				Type:        gql.NewNonNull(graphql.HashType),
				Description: "Transaction hash",
			},
			"oldMax": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "Previous max proposals per member",
			},
			"newMax": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "New max proposals per member",
			},
			"timestamp": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Event timestamp",
			},
		},
	})

	// ProposalExecutionSkippedEvent type
	proposalExecutionSkippedEventType = gql.NewObject(gql.ObjectConfig{
		Name: "ProposalExecutionSkippedEvent",
		Fields: gql.Fields{
			"contract": &gql.Field{
				Type:        gql.NewNonNull(graphql.AddressType),
				Description: "Contract address",
			},
			"blockNumber": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Block number",
			},
			"transactionHash": &gql.Field{
				Type:        gql.NewNonNull(graphql.HashType),
				Description: "Transaction hash",
			},
			"account": &gql.Field{
				Type:        gql.NewNonNull(graphql.AddressType),
				Description: "Account that triggered the skip",
			},
			"proposalId": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Proposal ID",
			},
			"reason": &gql.Field{
				Type:        gql.NewNonNull(gql.String),
				Description: "Reason for skipping execution",
			},
			"timestamp": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Event timestamp",
			},
		},
	})

	// MinterInfo type
	minterInfoType = gql.NewObject(gql.ObjectConfig{
		Name: "MinterInfo",
		Fields: gql.Fields{
			"address": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"allowance": &gql.Field{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
			"isActive": &gql.Field{
				Type: gql.NewNonNull(gql.Boolean),
			},
		},
	})

	// ValidatorInfo type
	validatorInfoType = gql.NewObject(gql.ObjectConfig{
		Name: "ValidatorInfo",
		Fields: gql.Fields{
			"address": &gql.Field{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"isActive": &gql.Field{
				Type: gql.NewNonNull(gql.Boolean),
			},
		},
	})

	// SystemContractEventFilter input type
	systemContractEventFilterType = gql.NewInputObject(gql.InputObjectConfig{
		Name: "SystemContractEventFilter",
		Fields: gql.InputObjectConfigFieldMap{
			"fromBlock": &gql.InputObjectFieldConfig{
				Type: graphql.BigIntType,
			},
			"toBlock": &gql.InputObjectFieldConfig{
				Type: graphql.BigIntType,
			},
			"address": &gql.InputObjectFieldConfig{
				Type:        graphql.AddressType,
				Description: "Filter by address (minter or burner)",
			},
			"minter": &gql.InputObjectFieldConfig{
				Type: graphql.AddressType,
			},
			"burner": &gql.InputObjectFieldConfig{
				Type: graphql.AddressType,
			},
			"status": &gql.InputObjectFieldConfig{
				Type: proposalStatusEnumType,
			},
		},
	})

	// ProposalFilter input type
	proposalFilterType = gql.NewInputObject(gql.InputObjectConfig{
		Name:        "ProposalFilter",
		Description: "Filter criteria for querying proposals. All fields are optional.",
		Fields: gql.InputObjectConfigFieldMap{
			"contract": &gql.InputObjectFieldConfig{
				Type:        graphql.AddressType, // Nullable - allows querying all proposals
				Description: "Filter by contract address. If not provided, returns proposals from all contracts.",
			},
			"status": &gql.InputObjectFieldConfig{
				Type:        proposalStatusEnumType,
				Description: "Filter by proposal status. If not provided, returns proposals with any status.",
			},
			"proposer": &gql.InputObjectFieldConfig{
				Type:        graphql.AddressType,
				Description: "Filter by proposer address. If not provided, returns proposals from all proposers.",
			},
		},
	})

	// MintEventConnection type
	mintEventConnectionType = gql.NewObject(gql.ObjectConfig{
		Name: "MintEventConnection",
		Fields: gql.Fields{
			"nodes": &gql.Field{
				Type: gql.NewList(gql.NewNonNull(mintEventType)),
			},
			"totalCount": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"pageInfo": &gql.Field{
				Type: gql.NewNonNull(graphql.PageInfoType()),
			},
		},
	})

	// BurnEventConnection type
	burnEventConnectionType = gql.NewObject(gql.ObjectConfig{
		Name: "BurnEventConnection",
		Fields: gql.Fields{
			"nodes": &gql.Field{
				Type: gql.NewList(gql.NewNonNull(burnEventType)),
			},
			"totalCount": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"pageInfo": &gql.Field{
				Type: gql.NewNonNull(graphql.PageInfoType()),
			},
		},
	})

	// ProposalConnection type
	proposalConnectionType = gql.NewObject(gql.ObjectConfig{
		Name: "ProposalConnection",
		Fields: gql.Fields{
			"nodes": &gql.Field{
				Type: gql.NewList(gql.NewNonNull(proposalType)),
			},
			"totalCount": &gql.Field{
				Type: gql.NewNonNull(gql.Int),
			},
			"pageInfo": &gql.Field{
				Type: gql.NewNonNull(graphql.PageInfoType()),
			},
		},
	})
}
