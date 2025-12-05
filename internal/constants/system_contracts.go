package constants

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// System Contract Addresses for StableOne chain
// These are the canonical addresses for system contracts deployed at genesis
var (
	// NativeCoinAdapterAddress (0x1000) - Base coin mint/burn/transfer management
	NativeCoinAdapterAddress = common.HexToAddress("0x0000000000000000000000000000000000001000")

	// GovValidatorAddress (0x1001) - Validator management and WBFT parameters
	GovValidatorAddress = common.HexToAddress("0x0000000000000000000000000000000000001001")

	// GovMasterMinterAddress (0x1002) - Minter permission management
	GovMasterMinterAddress = common.HexToAddress("0x0000000000000000000000000000000000001002")

	// GovMinterAddress (0x1003) - Actual mint/burn execution
	GovMinterAddress = common.HexToAddress("0x0000000000000000000000000000000000001003")

	// GovCouncilAddress (0x1004) - Blacklist and permission management
	GovCouncilAddress = common.HexToAddress("0x0000000000000000000000000000000000001004")
)

// SystemContractAddresses is a map for quick lookup
var SystemContractAddresses = map[common.Address]bool{
	NativeCoinAdapterAddress: true,
	GovValidatorAddress:      true,
	GovMasterMinterAddress:   true,
	GovMinterAddress:         true,
	GovCouncilAddress:        true,
}

// SystemContractName maps addresses to human-readable names
var SystemContractName = map[common.Address]string{
	NativeCoinAdapterAddress: "NativeCoinAdapter",
	GovValidatorAddress:      "GovValidator",
	GovMasterMinterAddress:   "GovMasterMinter",
	GovMinterAddress:         "GovMinter",
	GovCouncilAddress:        "GovCouncil",
}

// IsSystemContract returns true if the address is a system contract
func IsSystemContract(addr common.Address) bool {
	return SystemContractAddresses[addr]
}

// GetSystemContractName returns the name of a system contract
func GetSystemContractName(addr common.Address) string {
	if name, ok := SystemContractName[addr]; ok {
		return name
	}
	return ""
}

// SystemContractTokenMetadata contains pre-defined metadata for system contracts
type SystemContractTokenMetadata struct {
	Name     string
	Symbol   string
	Decimals int
}

// SystemContractTokenMetadataMap maps system contract addresses to their token metadata
var SystemContractTokenMetadataMap = map[common.Address]SystemContractTokenMetadata{
	NativeCoinAdapterAddress: {
		Name:     DefaultNativeTokenName,
		Symbol:   DefaultNativeTokenSymbol,
		Decimals: DefaultNativeTokenDecimals,
	},
}

// GetSystemContractTokenMetadata returns token metadata for a system contract
func GetSystemContractTokenMetadata(addr common.Address) *SystemContractTokenMetadata {
	if metadata, ok := SystemContractTokenMetadataMap[addr]; ok {
		return &metadata
	}
	return nil
}

// =============================================================================
// Event Signatures
// =============================================================================

// NativeCoinAdapter (0x1000) event signatures
var (
	// ERC-20 standard events
	EventSigTransfer = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	EventSigApproval = crypto.Keccak256Hash([]byte("Approval(address,address,uint256)"))

	// Minting/Burning events
	EventSigMint = crypto.Keccak256Hash([]byte("Mint(address,address,uint256)"))
	EventSigBurn = crypto.Keccak256Hash([]byte("Burn(address,uint256)"))

	// Minter management events
	EventSigMinterConfigured    = crypto.Keccak256Hash([]byte("MinterConfigured(address,uint256)"))
	EventSigMinterRemoved       = crypto.Keccak256Hash([]byte("MinterRemoved(address)"))
	EventSigMasterMinterChanged = crypto.Keccak256Hash([]byte("MasterMinterChanged(address)"))
)

// GovBase common events (all Gov contracts)
var (
	// Proposal management events
	EventSigProposalCreated   = crypto.Keccak256Hash([]byte("ProposalCreated(uint256,address,bytes32,bytes,uint256,uint256,uint256)"))
	EventSigProposalVoted     = crypto.Keccak256Hash([]byte("ProposalVoted(uint256,address,bool,uint256,uint256)"))
	EventSigProposalApproved  = crypto.Keccak256Hash([]byte("ProposalApproved(uint256,address,uint256,uint256)"))
	EventSigProposalRejected  = crypto.Keccak256Hash([]byte("ProposalRejected(uint256,address,uint256,uint256)"))
	EventSigProposalExecuted  = crypto.Keccak256Hash([]byte("ProposalExecuted(uint256,address,bool)"))
	EventSigProposalFailed    = crypto.Keccak256Hash([]byte("ProposalFailed(uint256,address,bytes)"))
	EventSigProposalExpired   = crypto.Keccak256Hash([]byte("ProposalExpired(uint256,address)"))
	EventSigProposalCancelled = crypto.Keccak256Hash([]byte("ProposalCancelled(uint256,address)"))

	// Member management events
	EventSigMemberAdded   = crypto.Keccak256Hash([]byte("MemberAdded(address,uint256,uint32)"))
	EventSigMemberRemoved = crypto.Keccak256Hash([]byte("MemberRemoved(address,uint256,uint32)"))
	EventSigMemberChanged = crypto.Keccak256Hash([]byte("MemberChanged(address,address)"))
	EventSigQuorumUpdated = crypto.Keccak256Hash([]byte("QuorumUpdated(uint32,uint32)"))

	// Configuration events
	EventSigMaxProposalsPerMemberUpdated = crypto.Keccak256Hash([]byte("MaxProposalsPerMemberUpdated(uint256,uint256)"))
)

// GovValidator (0x1001) specific events
var (
	EventSigGasTipUpdated = crypto.Keccak256Hash([]byte("GasTipUpdated(uint256,uint256,address)"))
)

// GovMasterMinter (0x1002) specific events
var (
	EventSigMaxMinterAllowanceUpdated = crypto.Keccak256Hash([]byte("MaxMinterAllowanceUpdated(uint256,uint256)"))
	EventSigEmergencyPaused           = crypto.Keccak256Hash([]byte("EmergencyPaused(uint256)"))
	EventSigEmergencyUnpaused         = crypto.Keccak256Hash([]byte("EmergencyUnpaused(uint256)"))
)

// GovMinter (0x1003) specific events
var (
	EventSigDepositMintProposed = crypto.Keccak256Hash([]byte("DepositMintProposed(uint256,address,uint256,string)"))
	EventSigBurnPrepaid         = crypto.Keccak256Hash([]byte("BurnPrepaid(address,uint256)"))
	EventSigBurnExecuted        = crypto.Keccak256Hash([]byte("BurnExecuted(address,uint256,string)"))
)

// GovCouncil (0x1004) specific events
var (
	EventSigAddressBlacklisted       = crypto.Keccak256Hash([]byte("AddressBlacklisted(address,uint256)"))
	EventSigAddressUnblacklisted     = crypto.Keccak256Hash([]byte("AddressUnblacklisted(address,uint256)"))
	EventSigAuthorizedAccountAdded   = crypto.Keccak256Hash([]byte("AuthorizedAccountAdded(address,uint256)"))
	EventSigAuthorizedAccountRemoved = crypto.Keccak256Hash([]byte("AuthorizedAccountRemoved(address,uint256)"))
	EventSigProposalExecutionSkipped = crypto.Keccak256Hash([]byte("ProposalExecutionSkipped(address,uint256,string)"))
)

// EventSignatureToName maps event signatures to human-readable names for logging
var EventSignatureToName = map[common.Hash]string{
	// NativeCoinAdapter
	EventSigTransfer:            "Transfer",
	EventSigApproval:            "Approval",
	EventSigMint:                "Mint",
	EventSigBurn:                "Burn",
	EventSigMinterConfigured:    "MinterConfigured",
	EventSigMinterRemoved:       "MinterRemoved",
	EventSigMasterMinterChanged: "MasterMinterChanged",

	// GovBase common
	EventSigProposalCreated:              "ProposalCreated",
	EventSigProposalVoted:                "ProposalVoted",
	EventSigProposalApproved:             "ProposalApproved",
	EventSigProposalRejected:             "ProposalRejected",
	EventSigProposalExecuted:             "ProposalExecuted",
	EventSigProposalFailed:               "ProposalFailed",
	EventSigProposalExpired:              "ProposalExpired",
	EventSigProposalCancelled:            "ProposalCancelled",
	EventSigMemberAdded:                  "MemberAdded",
	EventSigMemberRemoved:                "MemberRemoved",
	EventSigMemberChanged:                "MemberChanged",
	EventSigQuorumUpdated:                "QuorumUpdated",
	EventSigMaxProposalsPerMemberUpdated: "MaxProposalsPerMemberUpdated",

	// GovValidator
	EventSigGasTipUpdated: "GasTipUpdated",

	// GovMasterMinter
	EventSigMaxMinterAllowanceUpdated: "MaxMinterAllowanceUpdated",
	EventSigEmergencyPaused:           "EmergencyPaused",
	EventSigEmergencyUnpaused:         "EmergencyUnpaused",

	// GovMinter
	EventSigDepositMintProposed: "DepositMintProposed",
	EventSigBurnPrepaid:         "BurnPrepaid",
	EventSigBurnExecuted:        "BurnExecuted",

	// GovCouncil
	EventSigAddressBlacklisted:       "AddressBlacklisted",
	EventSigAddressUnblacklisted:     "AddressUnblacklisted",
	EventSigAuthorizedAccountAdded:   "AuthorizedAccountAdded",
	EventSigAuthorizedAccountRemoved: "AuthorizedAccountRemoved",
	EventSigProposalExecutionSkipped: "ProposalExecutionSkipped",
}

// GetEventName returns the human-readable name for an event signature
func GetEventName(sig common.Hash) string {
	if name, ok := EventSignatureToName[sig]; ok {
		return name
	}
	return "Unknown"
}
