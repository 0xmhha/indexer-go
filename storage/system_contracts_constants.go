package storage

import (
	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/ethereum/go-ethereum/common"
)

// Re-export system contract addresses from constants package for backward compatibility
var (
	NativeCoinAdapterAddress = constants.NativeCoinAdapterAddress
	GovValidatorAddress      = constants.GovValidatorAddress
	GovMasterMinterAddress   = constants.GovMasterMinterAddress
	GovMinterAddress         = constants.GovMinterAddress
	GovCouncilAddress        = constants.GovCouncilAddress
)

// SystemContractAddresses is a map for quick lookup
var SystemContractAddresses = constants.SystemContractAddresses

// IsSystemContract returns true if the address is a system contract
func IsSystemContract(addr common.Address) bool {
	return constants.IsSystemContract(addr)
}

// SystemContractTokenMetadata contains pre-defined metadata for system contracts
type SystemContractTokenMetadata = constants.SystemContractTokenMetadata

// SystemContractTokenMetadataMap maps system contract addresses to their token metadata
var SystemContractTokenMetadataMap = constants.SystemContractTokenMetadataMap

// GetSystemContractTokenMetadata returns token metadata for a system contract
func GetSystemContractTokenMetadata(addr common.Address) *SystemContractTokenMetadata {
	return constants.GetSystemContractTokenMetadata(addr)
}

// Re-export event signatures from constants package for backward compatibility
var (
	// NativeCoinAdapter events
	EventSigTransfer            = constants.EventSigTransfer
	EventSigApproval            = constants.EventSigApproval
	EventSigMint                = constants.EventSigMint
	EventSigBurn                = constants.EventSigBurn
	EventSigMinterConfigured    = constants.EventSigMinterConfigured
	EventSigMinterRemoved       = constants.EventSigMinterRemoved
	EventSigMasterMinterChanged = constants.EventSigMasterMinterChanged

	// GovBase events
	EventSigProposalCreated              = constants.EventSigProposalCreated
	EventSigProposalVoted                = constants.EventSigProposalVoted
	EventSigProposalApproved             = constants.EventSigProposalApproved
	EventSigProposalRejected             = constants.EventSigProposalRejected
	EventSigProposalExecuted             = constants.EventSigProposalExecuted
	EventSigProposalFailed               = constants.EventSigProposalFailed
	EventSigProposalExpired              = constants.EventSigProposalExpired
	EventSigProposalCancelled            = constants.EventSigProposalCancelled
	EventSigMemberAdded                  = constants.EventSigMemberAdded
	EventSigMemberRemoved                = constants.EventSigMemberRemoved
	EventSigMemberChanged                = constants.EventSigMemberChanged
	EventSigQuorumUpdated                = constants.EventSigQuorumUpdated
	EventSigMaxProposalsPerMemberUpdated = constants.EventSigMaxProposalsPerMemberUpdated

	// GovValidator events
	EventSigGasTipUpdated = constants.EventSigGasTipUpdated

	// GovMasterMinter events
	EventSigMaxMinterAllowanceUpdated = constants.EventSigMaxMinterAllowanceUpdated
	EventSigEmergencyPaused           = constants.EventSigEmergencyPaused
	EventSigEmergencyUnpaused         = constants.EventSigEmergencyUnpaused

	// GovMinter events
	EventSigDepositMintProposed = constants.EventSigDepositMintProposed
	EventSigBurnPrepaid         = constants.EventSigBurnPrepaid
	EventSigBurnExecuted        = constants.EventSigBurnExecuted

	// GovCouncil events
	EventSigAddressBlacklisted       = constants.EventSigAddressBlacklisted
	EventSigAddressUnblacklisted     = constants.EventSigAddressUnblacklisted
	EventSigAuthorizedAccountAdded   = constants.EventSigAuthorizedAccountAdded
	EventSigAuthorizedAccountRemoved = constants.EventSigAuthorizedAccountRemoved
	EventSigProposalExecutionSkipped = constants.EventSigProposalExecutionSkipped
)

// EventSignatureToName maps event signatures to human-readable names
var EventSignatureToName = constants.EventSignatureToName

// GetEventName returns the human-readable name for an event signature
func GetEventName(sig common.Hash) string {
	return constants.GetEventName(sig)
}
