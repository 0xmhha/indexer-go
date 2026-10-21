package api

import (
	gql "github.com/graphql-go/graphql"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
)

// addQueries adds the system contract queries.
func addQueries(e *graphql.Extension, s *Schema) {

	e.AddQuery("totalSupply", &gql.Field{
		Type:    gql.NewNonNull(graphql.BigIntType),
		Resolve: s.resolveTotalSupply,
	})
	e.AddQuery("activeMinters", &gql.Field{
		Type:    gql.NewNonNull(gql.NewList(gql.NewNonNull(minterInfoType))),
		Resolve: s.resolveActiveMinters,
	})
	e.AddQuery("activeMinterAddresses", &gql.Field{
		Type:        gql.NewNonNull(gql.NewList(gql.NewNonNull(graphql.AddressType))),
		Description: "Returns only the addresses of active minters (simplified version of activeMinters)",
		Resolve:     s.resolveActiveMinterAddresses,
	})
	e.AddQuery("minterAllowance", &gql.Field{
		Type: gql.NewNonNull(graphql.BigIntType),
		Args: gql.FieldConfigArgument{
			"minter": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.AddressType),
			},
		},
		Resolve: s.resolveMinterAllowance,
	})
	e.AddQuery("activeValidators", &gql.Field{
		Type:    gql.NewNonNull(gql.NewList(gql.NewNonNull(validatorInfoType))),
		Resolve: s.resolveActiveValidators,
	})
	e.AddQuery("activeValidatorAddresses", &gql.Field{
		Type:        gql.NewNonNull(gql.NewList(gql.NewNonNull(graphql.AddressType))),
		Description: "Returns only the addresses of active validators (simplified version of activeValidators)",
		Resolve:     s.resolveActiveValidatorAddresses,
	})
	e.AddQuery("blacklistedAddresses", &gql.Field{
		Type:    gql.NewNonNull(gql.NewList(gql.NewNonNull(graphql.AddressType))),
		Resolve: s.resolveBlacklistedAddresses,
	})
	e.AddQuery("proposals", &gql.Field{
		Type: gql.NewNonNull(proposalConnectionType),
		Args: gql.FieldConfigArgument{
			"filter": &gql.ArgumentConfig{
				Type:        proposalFilterType,
				Description: "Optional filter criteria. If not provided, returns all proposals.",
			},
			"pagination": &gql.ArgumentConfig{
				Type: graphql.PaginationInputType(),
			},
		},
		Resolve: s.resolveProposals,
	})
	e.AddQuery("proposal", &gql.Field{
		Type: proposalType,
		Args: gql.FieldConfigArgument{
			"contract": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"proposalId": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
		Resolve: s.resolveProposal,
	})
	e.AddQuery("proposalVotes", &gql.Field{
		Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(proposalVoteType))),
		Args: gql.FieldConfigArgument{
			"contract": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"proposalId": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.BigIntType),
			},
		},
		Resolve: s.resolveProposalVotes,
	})
	e.AddQuery("mintEvents", &gql.Field{
		Type: gql.NewNonNull(mintEventConnectionType),
		Args: gql.FieldConfigArgument{
			"filter": &gql.ArgumentConfig{
				Type: gql.NewNonNull(systemContractEventFilterType),
			},
			"pagination": &gql.ArgumentConfig{
				Type: graphql.PaginationInputType(),
			},
		},
		Resolve: s.resolveMintEvents,
	})
	e.AddQuery("burnEvents", &gql.Field{
		Type: gql.NewNonNull(burnEventConnectionType),
		Args: gql.FieldConfigArgument{
			"filter": &gql.ArgumentConfig{
				Type: gql.NewNonNull(systemContractEventFilterType),
			},
			"pagination": &gql.ArgumentConfig{
				Type: graphql.PaginationInputType(),
			},
		},
		Resolve: s.resolveBurnEvents,
	})
	e.AddQuery("minterHistory", &gql.Field{
		Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(minterConfigEventType))),
		Args: gql.FieldConfigArgument{
			"minter": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.AddressType),
			},
		},
		Resolve: s.resolveMinterHistory,
	})
	e.AddQuery("validatorHistory", &gql.Field{
		Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(validatorChangeEventType))),
		Args: gql.FieldConfigArgument{
			"validator": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.AddressType),
			},
		},
		Resolve: s.resolveValidatorHistory,
	})
	e.AddQuery("gasTipHistory", &gql.Field{
		Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(gasTipUpdateEventType))),
		Args: gql.FieldConfigArgument{
			"filter": &gql.ArgumentConfig{
				Type: gql.NewNonNull(systemContractEventFilterType),
			},
		},
		Resolve: s.resolveGasTipHistory,
	})
	e.AddQuery("blacklistHistory", &gql.Field{
		Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(blacklistEventType))),
		Args: gql.FieldConfigArgument{
			"address": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.AddressType),
			},
		},
		Resolve: s.resolveBlacklistHistory,
	})
	e.AddQuery("memberHistory", &gql.Field{
		Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(memberChangeEventType))),
		Args: gql.FieldConfigArgument{
			"contract": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.AddressType),
			},
		},
		Resolve: s.resolveMemberHistory,
	})
	e.AddQuery("emergencyPauseHistory", &gql.Field{
		Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(emergencyPauseEventType))),
		Args: gql.FieldConfigArgument{
			"contract": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.AddressType),
			},
		},
		Resolve: s.resolveEmergencyPauseHistory,
	})
	e.AddQuery("depositMintProposals", &gql.Field{
		Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(depositMintProposalType))),
		Args: gql.FieldConfigArgument{
			"filter": &gql.ArgumentConfig{
				Type: gql.NewNonNull(systemContractEventFilterType),
			},
		},
		Resolve: s.resolveDepositMintProposals,
	})

	// Phase 2.3: Add missing system contract queries
	e.AddQuery("minterConfigHistory", &gql.Field{
		Type:        gql.NewNonNull(gql.NewList(gql.NewNonNull(minterConfigEventType))),
		Description: "Returns minter configuration change history across all minters in a block range",
		Args: gql.FieldConfigArgument{
			"filter": &gql.ArgumentConfig{
				Type: gql.NewNonNull(systemContractEventFilterType),
			},
		},
		Resolve: s.resolveMinterConfigHistory,
	})
	e.AddQuery("burnHistory", &gql.Field{
		Type:        gql.NewNonNull(burnEventConnectionType),
		Description: "Alias for burnEvents - returns token burn history",
		Args: gql.FieldConfigArgument{
			"filter": &gql.ArgumentConfig{
				Type: gql.NewNonNull(systemContractEventFilterType),
			},
			"pagination": &gql.ArgumentConfig{
				Type: graphql.PaginationInputType(),
			},
		},
		Resolve: s.resolveBurnEvents, // Reuse existing resolver
	})
	e.AddQuery("authorizedAccounts", &gql.Field{
		Type:        gql.NewNonNull(gql.NewList(gql.NewNonNull(graphql.AddressType))),
		Description: "Returns list of authorized accounts from GovCouncil contract",
		Resolve:     s.resolveAuthorizedAccounts,
	})

	e.AddQuery("maxProposalsUpdateHistory", &gql.Field{
		Type:        gql.NewNonNull(gql.NewList(gql.NewNonNull(maxProposalsUpdateEventType))),
		Description: "Returns max proposals per member update history for a governance contract",
		Args: gql.FieldConfigArgument{
			"contract": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.AddressType),
			},
		},
		Resolve: s.resolveMaxProposalsUpdateHistory,
	})
	e.AddQuery("proposalExecutionSkippedEvents", &gql.Field{
		Type:        gql.NewNonNull(gql.NewList(gql.NewNonNull(proposalExecutionSkippedEventType))),
		Description: "Returns proposal execution skipped events for a governance contract",
		Args: gql.FieldConfigArgument{
			"contract": &gql.ArgumentConfig{
				Type: gql.NewNonNull(graphql.AddressType),
			},
			"proposalId": &gql.ArgumentConfig{
				Type: graphql.BigIntType,
			},
		},
		Resolve: s.resolveProposalExecutionSkippedEvents,
	})

}
