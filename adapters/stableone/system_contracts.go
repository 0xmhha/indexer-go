package stableone

import (
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/types/chain"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// Ensure SystemContractsHandler implements chain.SystemContractsHandler
var _ chain.SystemContractsHandler = (*SystemContractsHandler)(nil)

// SystemContractsHandler implements chain.SystemContractsHandler for StableOne
type SystemContractsHandler struct {
	logger *zap.Logger
	// Event signature to name mapping for quick lookup
	eventSigToName map[common.Hash]string
	// Contract address to name mapping
	contractNames map[common.Address]string
}

// NewSystemContractsHandler creates a new system contracts handler
func NewSystemContractsHandler(logger *zap.Logger) *SystemContractsHandler {
	handler := &SystemContractsHandler{
		logger:         logger,
		eventSigToName: make(map[common.Hash]string),
		contractNames:  make(map[common.Address]string),
	}

	// Initialize contract names from constants
	handler.contractNames = map[common.Address]string{
		constants.NativeCoinAdapterAddress: "NativeCoinAdapter",
		constants.GovValidatorAddress:      "GovValidator",
		constants.GovMasterMinterAddress:   "GovMasterMinter",
		constants.GovMinterAddress:         "GovMinter",
		constants.GovCouncilAddress:        "GovCouncil",
	}

	// Initialize event signatures from constants
	handler.eventSigToName = constants.EventSignatureToName

	return handler
}

// IsSystemContract checks if an address is a system contract
func (h *SystemContractsHandler) IsSystemContract(addr common.Address) bool {
	return constants.IsSystemContract(addr)
}

// GetSystemContractName returns the name of a system contract
func (h *SystemContractsHandler) GetSystemContractName(addr common.Address) string {
	return constants.GetSystemContractName(addr)
}

// GetSystemContractAddresses returns all system contract addresses
func (h *SystemContractsHandler) GetSystemContractAddresses() []common.Address {
	return []common.Address{
		constants.NativeCoinAdapterAddress,
		constants.GovValidatorAddress,
		constants.GovMasterMinterAddress,
		constants.GovMinterAddress,
		constants.GovCouncilAddress,
	}
}

// ParseSystemContractEvent parses an event from a system contract
func (h *SystemContractsHandler) ParseSystemContractEvent(log *types.Log) (*chain.SystemContractEvent, error) {
	if log == nil {
		return nil, fmt.Errorf("log is nil")
	}

	if !h.IsSystemContract(log.Address) {
		return nil, fmt.Errorf("address %s is not a system contract", log.Address.Hex())
	}

	if len(log.Topics) == 0 {
		return nil, fmt.Errorf("log has no topics")
	}

	eventSig := log.Topics[0]
	eventName := constants.GetEventName(eventSig)

	event := &chain.SystemContractEvent{
		ContractAddress: log.Address,
		ContractName:    h.GetSystemContractName(log.Address),
		EventName:       eventName,
		BlockNumber:     log.BlockNumber,
		TxHash:          log.TxHash,
		LogIndex:        log.Index,
		Data:            make(map[string]interface{}),
	}

	// Parse event data based on event type and contract
	if err := h.decodeEventData(log, event); err != nil {
		h.logger.Debug("Failed to decode event data",
			zap.String("event", eventName),
			zap.String("contract", event.ContractName),
			zap.Error(err),
		)
		// Continue with basic event info even if decoding fails
	}

	return event, nil
}

// decodeEventData decodes event-specific data from a log
func (h *SystemContractsHandler) decodeEventData(log *types.Log, event *chain.SystemContractEvent) error {
	switch event.EventName {
	case "Transfer":
		return h.decodeTransferEvent(log, event)
	case "Mint":
		return h.decodeMintEvent(log, event)
	case "Burn":
		return h.decodeBurnEvent(log, event)
	case "MemberAdded":
		return h.decodeMemberAddedEvent(log, event)
	case "MemberRemoved":
		return h.decodeMemberRemovedEvent(log, event)
	case "ProposalCreated":
		return h.decodeProposalCreatedEvent(log, event)
	case "ProposalVoted":
		return h.decodeProposalVotedEvent(log, event)
	case "ProposalExecuted":
		return h.decodeProposalExecutedEvent(log, event)
	case "AddressBlacklisted":
		return h.decodeAddressBlacklistedEvent(log, event)
	case "AddressUnblacklisted":
		return h.decodeAddressUnblacklistedEvent(log, event)
	default:
		// Store raw data for unknown events
		event.Data["rawTopics"] = log.Topics
		event.Data["rawData"] = common.Bytes2Hex(log.Data)
	}

	return nil
}

// decodeTransferEvent decodes a Transfer(address,address,uint256) event
func (h *SystemContractsHandler) decodeTransferEvent(log *types.Log, event *chain.SystemContractEvent) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("insufficient topics for Transfer event")
	}

	event.Data["from"] = common.HexToAddress(log.Topics[1].Hex())
	event.Data["to"] = common.HexToAddress(log.Topics[2].Hex())

	if len(log.Data) >= 32 {
		value := new(big.Int).SetBytes(log.Data[:32])
		event.Data["value"] = value.String()
	}

	return nil
}

// decodeMintEvent decodes a Mint(address,address,uint256) event
func (h *SystemContractsHandler) decodeMintEvent(log *types.Log, event *chain.SystemContractEvent) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("insufficient topics for Mint event")
	}

	event.Data["minter"] = common.HexToAddress(log.Topics[1].Hex())
	event.Data["to"] = common.HexToAddress(log.Topics[2].Hex())

	if len(log.Data) >= 32 {
		value := new(big.Int).SetBytes(log.Data[:32])
		event.Data["amount"] = value.String()
	}

	return nil
}

// decodeBurnEvent decodes a Burn(address,uint256) event
func (h *SystemContractsHandler) decodeBurnEvent(log *types.Log, event *chain.SystemContractEvent) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("insufficient topics for Burn event")
	}

	event.Data["burner"] = common.HexToAddress(log.Topics[1].Hex())

	if len(log.Data) >= 32 {
		value := new(big.Int).SetBytes(log.Data[:32])
		event.Data["amount"] = value.String()
	}

	return nil
}

// decodeMemberAddedEvent decodes a MemberAdded(address,uint256,uint32) event
func (h *SystemContractsHandler) decodeMemberAddedEvent(log *types.Log, event *chain.SystemContractEvent) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("insufficient topics for MemberAdded event")
	}

	event.Data["member"] = common.HexToAddress(log.Topics[1].Hex())

	// Decode non-indexed parameters from data
	if len(log.Data) >= 64 {
		proposalId := new(big.Int).SetBytes(log.Data[:32])
		event.Data["proposalId"] = proposalId.String()

		memberCount := new(big.Int).SetBytes(log.Data[32:64])
		event.Data["memberCount"] = memberCount.Uint64()
	}

	return nil
}

// decodeMemberRemovedEvent decodes a MemberRemoved(address,uint256,uint32) event
func (h *SystemContractsHandler) decodeMemberRemovedEvent(log *types.Log, event *chain.SystemContractEvent) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("insufficient topics for MemberRemoved event")
	}

	event.Data["member"] = common.HexToAddress(log.Topics[1].Hex())

	if len(log.Data) >= 64 {
		proposalId := new(big.Int).SetBytes(log.Data[:32])
		event.Data["proposalId"] = proposalId.String()

		memberCount := new(big.Int).SetBytes(log.Data[32:64])
		event.Data["memberCount"] = memberCount.Uint64()
	}

	return nil
}

// decodeProposalCreatedEvent decodes a ProposalCreated event
func (h *SystemContractsHandler) decodeProposalCreatedEvent(log *types.Log, event *chain.SystemContractEvent) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("insufficient topics for ProposalCreated event")
	}

	proposalId := new(big.Int).SetBytes(log.Topics[1].Bytes())
	event.Data["proposalId"] = proposalId.String()
	event.Data["proposer"] = common.HexToAddress(log.Topics[2].Hex())

	return nil
}

// decodeProposalVotedEvent decodes a ProposalVoted event
func (h *SystemContractsHandler) decodeProposalVotedEvent(log *types.Log, event *chain.SystemContractEvent) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("insufficient topics for ProposalVoted event")
	}

	proposalId := new(big.Int).SetBytes(log.Topics[1].Bytes())
	event.Data["proposalId"] = proposalId.String()
	event.Data["voter"] = common.HexToAddress(log.Topics[2].Hex())

	// Parse approval flag from data
	if len(log.Data) >= 32 {
		approvalByte := log.Data[31]
		event.Data["approved"] = approvalByte != 0
	}

	return nil
}

// decodeProposalExecutedEvent decodes a ProposalExecuted event
func (h *SystemContractsHandler) decodeProposalExecutedEvent(log *types.Log, event *chain.SystemContractEvent) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("insufficient topics for ProposalExecuted event")
	}

	proposalId := new(big.Int).SetBytes(log.Topics[1].Bytes())
	event.Data["proposalId"] = proposalId.String()
	event.Data["executor"] = common.HexToAddress(log.Topics[2].Hex())

	if len(log.Data) >= 32 {
		successByte := log.Data[31]
		event.Data["success"] = successByte != 0
	}

	return nil
}

// decodeAddressBlacklistedEvent decodes an AddressBlacklisted event
func (h *SystemContractsHandler) decodeAddressBlacklistedEvent(log *types.Log, event *chain.SystemContractEvent) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("insufficient topics for AddressBlacklisted event")
	}

	event.Data["account"] = common.HexToAddress(log.Topics[1].Hex())

	if len(log.Data) >= 32 {
		proposalId := new(big.Int).SetBytes(log.Data[:32])
		event.Data["proposalId"] = proposalId.String()
	}

	return nil
}

// decodeAddressUnblacklistedEvent decodes an AddressUnblacklisted event
func (h *SystemContractsHandler) decodeAddressUnblacklistedEvent(log *types.Log, event *chain.SystemContractEvent) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("insufficient topics for AddressUnblacklisted event")
	}

	event.Data["account"] = common.HexToAddress(log.Topics[1].Hex())

	if len(log.Data) >= 32 {
		proposalId := new(big.Int).SetBytes(log.Data[:32])
		event.Data["proposalId"] = proposalId.String()
	}

	return nil
}

// GetTokenMetadata returns token metadata for a system contract (if applicable)
func (h *SystemContractsHandler) GetTokenMetadata(addr common.Address) *constants.SystemContractTokenMetadata {
	return constants.GetSystemContractTokenMetadata(addr)
}

// GetEventABI returns the ABI for a specific event (for advanced decoding)
func (h *SystemContractsHandler) GetEventABI(eventName string) (*abi.Event, error) {
	// This would return the full ABI for advanced decoding
	// For now, return nil as we use manual decoding
	return nil, fmt.Errorf("ABI-based decoding not implemented")
}

// GetContractType returns the type of system contract
func (h *SystemContractsHandler) GetContractType(addr common.Address) string {
	switch addr {
	case constants.NativeCoinAdapterAddress:
		return "token"
	case constants.GovValidatorAddress:
		return "governance"
	case constants.GovMasterMinterAddress:
		return "governance"
	case constants.GovMinterAddress:
		return "minting"
	case constants.GovCouncilAddress:
		return "governance"
	default:
		return "unknown"
	}
}
