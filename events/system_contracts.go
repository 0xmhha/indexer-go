package events

import (
	"context"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

// System contract addresses
var (
	NativeCoinAdapterAddress = common.HexToAddress("0x1000")
	GovValidatorAddress      = common.HexToAddress("0x1001")
	GovMasterMinterAddress   = common.HexToAddress("0x1002")
	GovMinterAddress         = common.HexToAddress("0x1003")
	GovCouncilAddress        = common.HexToAddress("0x1004")
)

// Event signatures for NativeCoinAdapter
var (
	EventSigMint                = crypto.Keccak256Hash([]byte("Mint(address,address,uint256)"))
	EventSigBurn                = crypto.Keccak256Hash([]byte("Burn(address,uint256)"))
	EventSigMinterConfigured    = crypto.Keccak256Hash([]byte("MinterConfigured(address,uint256)"))
	EventSigMinterRemoved       = crypto.Keccak256Hash([]byte("MinterRemoved(address)"))
	EventSigMasterMinterChanged = crypto.Keccak256Hash([]byte("MasterMinterChanged(address)"))
	EventSigTransfer            = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	EventSigApproval            = crypto.Keccak256Hash([]byte("Approval(address,address,uint256)"))
)

// Event signatures for GovBase (common governance events)
var (
	EventSigProposalCreated   = crypto.Keccak256Hash([]byte("ProposalCreated(uint256,address,bytes32,bytes,uint256,uint256,uint256)"))
	EventSigProposalVoted     = crypto.Keccak256Hash([]byte("ProposalVoted(uint256,address,bool,uint256,uint256)"))
	EventSigProposalApproved  = crypto.Keccak256Hash([]byte("ProposalApproved(uint256,address,uint256,uint256)"))
	EventSigProposalRejected  = crypto.Keccak256Hash([]byte("ProposalRejected(uint256,address,uint256,uint256)"))
	EventSigProposalExecuted  = crypto.Keccak256Hash([]byte("ProposalExecuted(uint256,address,bool)"))
	EventSigProposalFailed    = crypto.Keccak256Hash([]byte("ProposalFailed(uint256,address,bytes)"))
	EventSigProposalExpired   = crypto.Keccak256Hash([]byte("ProposalExpired(uint256,address)"))
	EventSigProposalCancelled = crypto.Keccak256Hash([]byte("ProposalCancelled(uint256,address)"))
	EventSigMemberAdded       = crypto.Keccak256Hash([]byte("MemberAdded(address,uint256,uint32)"))
	EventSigMemberRemoved     = crypto.Keccak256Hash([]byte("MemberRemoved(address,uint256,uint32)"))
	EventSigMemberChanged     = crypto.Keccak256Hash([]byte("MemberChanged(address,address)"))
	EventSigQuorumUpdated     = crypto.Keccak256Hash([]byte("QuorumUpdated(uint32,uint32)"))
)

// Event signatures for GovValidator
var (
	EventSigGasTipUpdated = crypto.Keccak256Hash([]byte("GasTipUpdated(uint256,uint256,address)"))
)

// Event signatures for GovMasterMinter
var (
	EventSigMaxMinterAllowanceUpdated = crypto.Keccak256Hash([]byte("MaxMinterAllowanceUpdated(uint256,uint256)"))
	EventSigEmergencyPaused           = crypto.Keccak256Hash([]byte("EmergencyPaused(uint256)"))
	EventSigEmergencyUnpaused         = crypto.Keccak256Hash([]byte("EmergencyUnpaused(uint256)"))
)

// Event signatures for GovMinter
var (
	EventSigDepositMintProposed = crypto.Keccak256Hash([]byte("DepositMintProposed(uint256,address,uint256,string)"))
	EventSigBurnPrepaid         = crypto.Keccak256Hash([]byte("BurnPrepaid(address,uint256)"))
	EventSigBurnExecuted        = crypto.Keccak256Hash([]byte("BurnExecuted(address,uint256,string)"))
)

// Event signatures for GovCouncil
var (
	EventSigAddressBlacklisted       = crypto.Keccak256Hash([]byte("AddressBlacklisted(address,uint256)"))
	EventSigAddressUnblacklisted     = crypto.Keccak256Hash([]byte("AddressUnblacklisted(address,uint256)"))
	EventSigAuthorizedAccountAdded   = crypto.Keccak256Hash([]byte("AuthorizedAccountAdded(address,uint256)"))
	EventSigAuthorizedAccountRemoved = crypto.Keccak256Hash([]byte("AuthorizedAccountRemoved(address,uint256)"))
)

// SystemContractEventParser parses and indexes system contract events
type SystemContractEventParser struct {
	storage storage.SystemContractWriter
	logger  *zap.Logger
}

// NewSystemContractEventParser creates a new system contract event parser
func NewSystemContractEventParser(storage storage.SystemContractWriter, logger *zap.Logger) *SystemContractEventParser {
	return &SystemContractEventParser{
		storage: storage,
		logger:  logger,
	}
}

// ParseAndIndexLogs parses and indexes multiple logs
func (p *SystemContractEventParser) ParseAndIndexLogs(ctx context.Context, logs []*types.Log) error {
	for _, log := range logs {
		if err := p.parseAndIndexLog(ctx, log); err != nil {
			p.logger.Error("failed to parse system contract log",
				zap.String("address", log.Address.Hex()),
				zap.String("txHash", log.TxHash.Hex()),
				zap.Uint64("blockNumber", log.BlockNumber),
				zap.Error(err))
			// Continue processing other logs
			continue
		}
	}
	return nil
}

// parseAndIndexLog parses and indexes a single log
func (p *SystemContractEventParser) parseAndIndexLog(ctx context.Context, log *types.Log) error {
	// Check if log is from system contract
	if !isSystemContract(log.Address) {
		return nil
	}

	// Route to appropriate parser based on event signature
	if len(log.Topics) == 0 {
		return nil
	}

	eventSig := log.Topics[0]

	switch eventSig {
	// NativeCoinAdapter events
	case EventSigMint:
		return p.parseMintEvent(ctx, log)
	case EventSigBurn:
		return p.parseBurnEvent(ctx, log)
	case EventSigMinterConfigured:
		return p.parseMinterConfiguredEvent(ctx, log)
	case EventSigMinterRemoved:
		return p.parseMinterRemovedEvent(ctx, log)
	case EventSigMasterMinterChanged:
		return p.parseMasterMinterChangedEvent(ctx, log)

	// GovBase events (common to all Gov contracts)
	case EventSigProposalCreated:
		return p.parseProposalCreatedEvent(ctx, log)
	case EventSigProposalVoted:
		return p.parseProposalVotedEvent(ctx, log)
	case EventSigProposalApproved:
		return p.parseProposalApprovedEvent(ctx, log)
	case EventSigProposalRejected:
		return p.parseProposalRejectedEvent(ctx, log)
	case EventSigProposalExecuted:
		return p.parseProposalExecutedEvent(ctx, log)
	case EventSigProposalFailed:
		return p.parseProposalFailedEvent(ctx, log)
	case EventSigProposalExpired:
		return p.parseProposalExpiredEvent(ctx, log)
	case EventSigProposalCancelled:
		return p.parseProposalCancelledEvent(ctx, log)
	case EventSigMemberAdded:
		return p.parseMemberAddedEvent(ctx, log)
	case EventSigMemberRemoved:
		return p.parseMemberRemovedEvent(ctx, log)
	case EventSigMemberChanged:
		return p.parseMemberChangedEvent(ctx, log)
	case EventSigQuorumUpdated:
		return p.parseQuorumUpdatedEvent(ctx, log)

	// GovValidator events
	case EventSigGasTipUpdated:
		return p.parseGasTipUpdatedEvent(ctx, log)

	// GovMasterMinter events
	case EventSigMaxMinterAllowanceUpdated:
		return p.parseMaxMinterAllowanceUpdatedEvent(ctx, log)
	case EventSigEmergencyPaused:
		return p.parseEmergencyPausedEvent(ctx, log)
	case EventSigEmergencyUnpaused:
		return p.parseEmergencyUnpausedEvent(ctx, log)

	// GovMinter events
	case EventSigDepositMintProposed:
		return p.parseDepositMintProposedEvent(ctx, log)
	case EventSigBurnPrepaid:
		return p.parseBurnPrepaidEvent(ctx, log)
	case EventSigBurnExecuted:
		return p.parseBurnExecutedEvent(ctx, log)

	// GovCouncil events
	case EventSigAddressBlacklisted:
		return p.parseAddressBlacklistedEvent(ctx, log)
	case EventSigAddressUnblacklisted:
		return p.parseAddressUnblacklistedEvent(ctx, log)
	case EventSigAuthorizedAccountAdded:
		return p.parseAuthorizedAccountAddedEvent(ctx, log)
	case EventSigAuthorizedAccountRemoved:
		return p.parseAuthorizedAccountRemovedEvent(ctx, log)

	default:
		// Unknown event, skip silently
		return nil
	}
}

// isSystemContract checks if an address is a system contract
func isSystemContract(addr common.Address) bool {
	return addr == NativeCoinAdapterAddress ||
		addr == GovValidatorAddress ||
		addr == GovMasterMinterAddress ||
		addr == GovMinterAddress ||
		addr == GovCouncilAddress
}

// NativeCoinAdapter event parsers

// parseMintEvent parses Mint(address indexed minter, address indexed to, uint256 amount)
func (p *SystemContractEventParser) parseMintEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid Mint event: expected 3 topics, got %d", len(log.Topics))
	}
	if len(log.Data) != 32 {
		return fmt.Errorf("invalid Mint event: expected 32 bytes data, got %d", len(log.Data))
	}

	event := &storage.MintEvent{
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		Minter:      common.BytesToAddress(log.Topics[1].Bytes()),
		To:          common.BytesToAddress(log.Topics[2].Bytes()),
		Amount:      new(big.Int).SetBytes(log.Data),
		Timestamp:   0, // Will be set by storage layer
	}

	if err := p.storage.StoreMintEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store mint event: %w", err)
	}

	// Update total supply
	if err := p.storage.UpdateTotalSupply(ctx, event.Amount); err != nil {
		return fmt.Errorf("failed to update total supply: %w", err)
	}

	return nil
}

// parseBurnEvent parses Burn(address indexed burner, uint256 amount)
func (p *SystemContractEventParser) parseBurnEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 2 {
		return fmt.Errorf("invalid Burn event: expected 2 topics, got %d", len(log.Topics))
	}
	if len(log.Data) != 32 {
		return fmt.Errorf("invalid Burn event: expected 32 bytes data, got %d", len(log.Data))
	}

	amount := new(big.Int).SetBytes(log.Data)

	event := &storage.BurnEvent{
		BlockNumber:  log.BlockNumber,
		TxHash:       log.TxHash,
		Burner:       common.BytesToAddress(log.Topics[1].Bytes()),
		Amount:       amount,
		Timestamp:    0,  // Will be set by storage layer
		WithdrawalID: "", // Not set for NativeCoinAdapter burns
	}

	if err := p.storage.StoreBurnEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store burn event: %w", err)
	}

	// Update total supply (decrease)
	negativeAmount := new(big.Int).Neg(amount)
	if err := p.storage.UpdateTotalSupply(ctx, negativeAmount); err != nil {
		return fmt.Errorf("failed to update total supply: %w", err)
	}

	return nil
}

// parseMinterConfiguredEvent parses MinterConfigured(address indexed minter, uint256 minterAllowedAmount)
func (p *SystemContractEventParser) parseMinterConfiguredEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 2 {
		return fmt.Errorf("invalid MinterConfigured event: expected 2 topics, got %d", len(log.Topics))
	}
	if len(log.Data) != 32 {
		return fmt.Errorf("invalid MinterConfigured event: expected 32 bytes data, got %d", len(log.Data))
	}

	minter := common.BytesToAddress(log.Topics[1].Bytes())
	allowance := new(big.Int).SetBytes(log.Data)

	event := &storage.MinterConfigEvent{
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		Minter:      minter,
		Allowance:   allowance,
		Action:      "configured",
		Timestamp:   0, // Will be set by storage layer
	}

	if err := p.storage.StoreMinterConfigEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store minter config event: %w", err)
	}

	// Update active minter index
	if err := p.storage.UpdateActiveMinter(ctx, minter, allowance, true); err != nil {
		return fmt.Errorf("failed to update active minter: %w", err)
	}

	return nil
}

// parseMinterRemovedEvent parses MinterRemoved(address indexed oldMinter)
func (p *SystemContractEventParser) parseMinterRemovedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 2 {
		return fmt.Errorf("invalid MinterRemoved event: expected 2 topics, got %d", len(log.Topics))
	}

	minter := common.BytesToAddress(log.Topics[1].Bytes())

	event := &storage.MinterConfigEvent{
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		Minter:      minter,
		Allowance:   big.NewInt(0),
		Action:      "removed",
		Timestamp:   0, // Will be set by storage layer
	}

	if err := p.storage.StoreMinterConfigEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store minter config event: %w", err)
	}

	// Update active minter index (remove)
	if err := p.storage.UpdateActiveMinter(ctx, minter, big.NewInt(0), false); err != nil {
		return fmt.Errorf("failed to update active minter: %w", err)
	}

	return nil
}

// parseMasterMinterChangedEvent parses MasterMinterChanged(address indexed newMasterMinter)
func (p *SystemContractEventParser) parseMasterMinterChangedEvent(ctx context.Context, log *types.Log) error {
	// This event is informational only, no storage action needed for now
	// Could be extended in the future to track master minter history
	p.logger.Debug("master minter changed",
		zap.String("newMasterMinter", common.BytesToAddress(log.Topics[1].Bytes()).Hex()),
		zap.Uint64("blockNumber", log.BlockNumber))
	return nil
}

// Placeholder parsers for other events (to be implemented)
// These will be implemented in subsequent iterations

// parseProposalCreatedEvent parses ProposalCreated(uint256 indexed proposalId, address indexed proposer, bytes32 indexed actionType, bytes callData, uint256 memberVersion, uint256 requiredApprovals, uint256 createdAt)
func (p *SystemContractEventParser) parseProposalCreatedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 4 {
		return fmt.Errorf("invalid ProposalCreated event: expected 4 topics, got %d", len(log.Topics))
	}

	// Parse topics
	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())
	proposer := common.BytesToAddress(log.Topics[2].Bytes())
	var actionType [32]byte
	copy(actionType[:], log.Topics[3].Bytes())

	// Parse data (callData is dynamic, followed by 3 uint256s)
	if len(log.Data) < 128 {
		return fmt.Errorf("invalid ProposalCreated event: data too short")
	}

	// Find where callData ends by reading its offset and length
	callDataOffset := new(big.Int).SetBytes(log.Data[0:32]).Uint64()
	if callDataOffset >= uint64(len(log.Data)) {
		return fmt.Errorf("invalid callData offset")
	}
	callDataLength := new(big.Int).SetBytes(log.Data[callDataOffset : callDataOffset+32]).Uint64()
	callDataStart := callDataOffset + 32
	callDataEnd := callDataStart + callDataLength

	// Pad to 32-byte boundary
	if callDataLength%32 != 0 {
		callDataEnd += 32 - (callDataLength % 32)
	}

	var callData []byte
	if callDataEnd <= uint64(len(log.Data)) {
		callData = log.Data[callDataStart : callDataStart+callDataLength]
	}

	// After dynamic callData, we have: memberVersion, requiredApprovals, createdAt
	staticDataStart := callDataEnd
	if staticDataStart+96 > uint64(len(log.Data)) {
		return fmt.Errorf("invalid ProposalCreated event: not enough data for static fields")
	}

	memberVersion := new(big.Int).SetBytes(log.Data[staticDataStart : staticDataStart+32])
	requiredApprovals := uint32(new(big.Int).SetBytes(log.Data[staticDataStart+32 : staticDataStart+64]).Uint64())
	createdAt := new(big.Int).SetBytes(log.Data[staticDataStart+64 : staticDataStart+96]).Uint64()

	proposal := &storage.Proposal{
		Contract:          log.Address,
		ProposalID:        proposalID,
		Proposer:          proposer,
		ActionType:        actionType,
		CallData:          callData,
		MemberVersion:     memberVersion,
		RequiredApprovals: requiredApprovals,
		Approved:          0,
		Rejected:          0,
		Status:            storage.ProposalStatusVoting,
		CreatedAt:         createdAt,
		ExecutedAt:        nil,
		BlockNumber:       log.BlockNumber,
		TxHash:            log.TxHash,
	}

	if err := p.storage.StoreProposal(ctx, proposal); err != nil {
		return fmt.Errorf("failed to store proposal: %w", err)
	}

	return nil
}

// parseProposalVotedEvent parses ProposalVoted(uint256 indexed proposalId, address indexed voter, bool approval, uint256 approved, uint256 rejected)
func (p *SystemContractEventParser) parseProposalVotedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalVoted event: expected 3 topics, got %d", len(log.Topics))
	}
	if len(log.Data) != 96 {
		return fmt.Errorf("invalid ProposalVoted event: expected 96 bytes data, got %d", len(log.Data))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())
	voter := common.BytesToAddress(log.Topics[2].Bytes())

	// Parse data: approval (bool as uint256), approved count, rejected count
	approval := new(big.Int).SetBytes(log.Data[0:32]).Uint64() != 0

	vote := &storage.ProposalVote{
		Contract:    log.Address,
		ProposalID:  proposalID,
		Voter:       voter,
		Approval:    approval,
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		Timestamp:   0, // Will be set by storage layer
	}

	if err := p.storage.StoreProposalVote(ctx, vote); err != nil {
		return fmt.Errorf("failed to store proposal vote: %w", err)
	}

	return nil
}

// parseProposalApprovedEvent parses ProposalApproved(uint256 indexed proposalId, address indexed approver, uint256 approved, uint256 rejected)
func (p *SystemContractEventParser) parseProposalApprovedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalApproved event: expected 3 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())

	// Update proposal status to Approved
	if err := p.storage.UpdateProposalStatus(ctx, log.Address, proposalID, storage.ProposalStatusApproved, 0); err != nil {
		return fmt.Errorf("failed to update proposal status: %w", err)
	}

	return nil
}

// parseProposalRejectedEvent parses ProposalRejected(uint256 indexed proposalId, address indexed rejector, uint256 approved, uint256 rejected)
func (p *SystemContractEventParser) parseProposalRejectedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalRejected event: expected 3 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())

	// Update proposal status to Rejected
	if err := p.storage.UpdateProposalStatus(ctx, log.Address, proposalID, storage.ProposalStatusRejected, 0); err != nil {
		return fmt.Errorf("failed to update proposal status: %w", err)
	}

	return nil
}

// parseProposalExecutedEvent parses ProposalExecuted(uint256 indexed proposalId, address indexed executor, bool success)
func (p *SystemContractEventParser) parseProposalExecutedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalExecuted event: expected 3 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())

	// Update proposal status to Executed with current block number as execution time
	if err := p.storage.UpdateProposalStatus(ctx, log.Address, proposalID, storage.ProposalStatusExecuted, log.BlockNumber); err != nil {
		return fmt.Errorf("failed to update proposal status: %w", err)
	}

	return nil
}

// parseProposalFailedEvent parses ProposalFailed(uint256 indexed proposalId, address indexed executor, bytes reason)
func (p *SystemContractEventParser) parseProposalFailedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalFailed event: expected 3 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())

	// Update proposal status to Failed
	if err := p.storage.UpdateProposalStatus(ctx, log.Address, proposalID, storage.ProposalStatusFailed, log.BlockNumber); err != nil {
		return fmt.Errorf("failed to update proposal status: %w", err)
	}

	return nil
}

// parseProposalExpiredEvent parses ProposalExpired(uint256 indexed proposalId, address indexed executor)
func (p *SystemContractEventParser) parseProposalExpiredEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalExpired event: expected 3 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())

	// Update proposal status to Expired
	if err := p.storage.UpdateProposalStatus(ctx, log.Address, proposalID, storage.ProposalStatusExpired, 0); err != nil {
		return fmt.Errorf("failed to update proposal status: %w", err)
	}

	return nil
}

// parseProposalCancelledEvent parses ProposalCancelled(uint256 indexed proposalId, address indexed canceller)
func (p *SystemContractEventParser) parseProposalCancelledEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalCancelled event: expected 3 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())

	// Update proposal status to Cancelled
	if err := p.storage.UpdateProposalStatus(ctx, log.Address, proposalID, storage.ProposalStatusCancelled, 0); err != nil {
		return fmt.Errorf("failed to update proposal status: %w", err)
	}

	return nil
}

// parseMemberAddedEvent parses MemberAdded(address indexed member, uint256 totalMembers, uint32 newQuorum)
func (p *SystemContractEventParser) parseMemberAddedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 2 {
		return fmt.Errorf("invalid MemberAdded event: expected 2 topics, got %d", len(log.Topics))
	}
	if len(log.Data) != 64 {
		return fmt.Errorf("invalid MemberAdded event: expected 64 bytes data, got %d", len(log.Data))
	}

	member := common.BytesToAddress(log.Topics[1].Bytes())
	totalMembers := new(big.Int).SetBytes(log.Data[0:32]).Uint64()
	newQuorum := uint32(new(big.Int).SetBytes(log.Data[32:64]).Uint64())

	event := &storage.MemberChangeEvent{
		Contract:     log.Address,
		BlockNumber:  log.BlockNumber,
		TxHash:       log.TxHash,
		Member:       member,
		Action:       "added",
		OldMember:    nil,
		TotalMembers: totalMembers,
		NewQuorum:    newQuorum,
		Timestamp:    0, // Will be set by storage layer
	}

	if err := p.storage.StoreMemberChangeEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store member change event: %w", err)
	}

	// For validators, update active validator index
	if log.Address == GovValidatorAddress {
		if err := p.storage.UpdateActiveValidator(ctx, member, true); err != nil {
			return fmt.Errorf("failed to update active validator: %w", err)
		}
	}

	return nil
}

// parseMemberRemovedEvent parses MemberRemoved(address indexed member, uint256 totalMembers, uint32 newQuorum)
func (p *SystemContractEventParser) parseMemberRemovedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 2 {
		return fmt.Errorf("invalid MemberRemoved event: expected 2 topics, got %d", len(log.Topics))
	}
	if len(log.Data) != 64 {
		return fmt.Errorf("invalid MemberRemoved event: expected 64 bytes data, got %d", len(log.Data))
	}

	member := common.BytesToAddress(log.Topics[1].Bytes())
	totalMembers := new(big.Int).SetBytes(log.Data[0:32]).Uint64()
	newQuorum := uint32(new(big.Int).SetBytes(log.Data[32:64]).Uint64())

	event := &storage.MemberChangeEvent{
		Contract:     log.Address,
		BlockNumber:  log.BlockNumber,
		TxHash:       log.TxHash,
		Member:       member,
		Action:       "removed",
		OldMember:    nil,
		TotalMembers: totalMembers,
		NewQuorum:    newQuorum,
		Timestamp:    0, // Will be set by storage layer
	}

	if err := p.storage.StoreMemberChangeEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store member change event: %w", err)
	}

	// For validators, update active validator index
	if log.Address == GovValidatorAddress {
		if err := p.storage.UpdateActiveValidator(ctx, member, false); err != nil {
			return fmt.Errorf("failed to update active validator: %w", err)
		}
	}

	return nil
}

// parseMemberChangedEvent parses MemberChanged(address indexed oldMember, address indexed newMember)
func (p *SystemContractEventParser) parseMemberChangedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid MemberChanged event: expected 3 topics, got %d", len(log.Topics))
	}

	oldMember := common.BytesToAddress(log.Topics[1].Bytes())
	newMember := common.BytesToAddress(log.Topics[2].Bytes())

	event := &storage.MemberChangeEvent{
		Contract:     log.Address,
		BlockNumber:  log.BlockNumber,
		TxHash:       log.TxHash,
		Member:       newMember,
		Action:       "changed",
		OldMember:    &oldMember,
		TotalMembers: 0, // Not provided in this event
		NewQuorum:    0, // Not provided in this event
		Timestamp:    0, // Will be set by storage layer
	}

	if err := p.storage.StoreMemberChangeEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store member change event: %w", err)
	}

	// For validators, update active validator index
	if log.Address == GovValidatorAddress {
		// Remove old, add new
		if err := p.storage.UpdateActiveValidator(ctx, oldMember, false); err != nil {
			return fmt.Errorf("failed to update active validator (old): %w", err)
		}
		if err := p.storage.UpdateActiveValidator(ctx, newMember, true); err != nil {
			return fmt.Errorf("failed to update active validator (new): %w", err)
		}

		// Store as validator change event
		validatorChangeEvent := &storage.ValidatorChangeEvent{
			BlockNumber:  log.BlockNumber,
			TxHash:       log.TxHash,
			Validator:    newMember,
			Action:       "changed",
			OldValidator: &oldMember,
			Timestamp:    0,
		}
		if err := p.storage.StoreValidatorChangeEvent(ctx, validatorChangeEvent); err != nil {
			return fmt.Errorf("failed to store validator change event: %w", err)
		}
	}

	return nil
}

// parseQuorumUpdatedEvent parses QuorumUpdated(uint32 oldQuorum, uint32 newQuorum)
func (p *SystemContractEventParser) parseQuorumUpdatedEvent(ctx context.Context, log *types.Log) error {
	// This event is informational only, quorum updates are tracked via MemberAdded/Removed
	// Log for debugging purposes
	if len(log.Data) >= 64 {
		oldQuorum := uint32(new(big.Int).SetBytes(log.Data[0:32]).Uint64())
		newQuorum := uint32(new(big.Int).SetBytes(log.Data[32:64]).Uint64())
		p.logger.Debug("quorum updated",
			zap.String("contract", log.Address.Hex()),
			zap.Uint32("oldQuorum", oldQuorum),
			zap.Uint32("newQuorum", newQuorum),
			zap.Uint64("blockNumber", log.BlockNumber))
	}
	return nil
}

// parseGasTipUpdatedEvent parses GasTipUpdated(uint256 oldTip, uint256 newTip, address indexed updater)
func (p *SystemContractEventParser) parseGasTipUpdatedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 2 {
		return fmt.Errorf("invalid GasTipUpdated event: expected 2 topics, got %d", len(log.Topics))
	}
	if len(log.Data) != 64 {
		return fmt.Errorf("invalid GasTipUpdated event: expected 64 bytes data, got %d", len(log.Data))
	}

	oldTip := new(big.Int).SetBytes(log.Data[0:32])
	newTip := new(big.Int).SetBytes(log.Data[32:64])
	updater := common.BytesToAddress(log.Topics[1].Bytes())

	event := &storage.GasTipUpdateEvent{
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		OldTip:      oldTip,
		NewTip:      newTip,
		Updater:     updater,
		Timestamp:   0, // Will be set by storage layer
	}

	if err := p.storage.StoreGasTipUpdateEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store gas tip update event: %w", err)
	}

	return nil
}

// parseMaxMinterAllowanceUpdatedEvent parses MaxMinterAllowanceUpdated(uint256 oldLimit, uint256 newLimit)
func (p *SystemContractEventParser) parseMaxMinterAllowanceUpdatedEvent(ctx context.Context, log *types.Log) error {
	// This event is informational only, no storage action needed
	if len(log.Data) >= 64 {
		oldLimit := new(big.Int).SetBytes(log.Data[0:32])
		newLimit := new(big.Int).SetBytes(log.Data[32:64])
		p.logger.Debug("max minter allowance updated",
			zap.String("oldLimit", oldLimit.String()),
			zap.String("newLimit", newLimit.String()),
			zap.Uint64("blockNumber", log.BlockNumber))
	}
	return nil
}

// parseEmergencyPausedEvent parses EmergencyPaused(uint256 indexed proposalId)
func (p *SystemContractEventParser) parseEmergencyPausedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 2 {
		return fmt.Errorf("invalid EmergencyPaused event: expected 2 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())

	event := &storage.EmergencyPauseEvent{
		Contract:    log.Address,
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		ProposalID:  proposalID,
		Action:      "paused",
		Timestamp:   0, // Will be set by storage layer
	}

	if err := p.storage.StoreEmergencyPauseEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store emergency pause event: %w", err)
	}

	return nil
}

// parseEmergencyUnpausedEvent parses EmergencyUnpaused(uint256 indexed proposalId)
func (p *SystemContractEventParser) parseEmergencyUnpausedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 2 {
		return fmt.Errorf("invalid EmergencyUnpaused event: expected 2 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())

	event := &storage.EmergencyPauseEvent{
		Contract:    log.Address,
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		ProposalID:  proposalID,
		Action:      "unpaused",
		Timestamp:   0, // Will be set by storage layer
	}

	if err := p.storage.StoreEmergencyPauseEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store emergency pause event: %w", err)
	}

	return nil
}

// parseDepositMintProposedEvent parses DepositMintProposed(uint256 indexed proposalId, address indexed to, uint256 indexed amount, string depositId)
func (p *SystemContractEventParser) parseDepositMintProposedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 4 {
		return fmt.Errorf("invalid DepositMintProposed event: expected 4 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())
	to := common.BytesToAddress(log.Topics[2].Bytes())
	amount := new(big.Int).SetBytes(log.Topics[3].Bytes())

	// Parse depositId from data (string is dynamic)
	var depositID string
	if len(log.Data) > 0 {
		// First 32 bytes: offset to string
		// Second 32 bytes: length of string
		// Remaining bytes: string data
		if len(log.Data) >= 64 {
			offset := new(big.Int).SetBytes(log.Data[0:32]).Uint64()
			if offset+32 <= uint64(len(log.Data)) {
				length := new(big.Int).SetBytes(log.Data[offset : offset+32]).Uint64()
				dataStart := offset + 32
				if dataStart+length <= uint64(len(log.Data)) {
					depositID = string(log.Data[dataStart : dataStart+length])
				}
			}
		}
	}

	proposal := &storage.DepositMintProposal{
		ProposalID:  proposalID,
		To:          to,
		Amount:      amount,
		DepositID:   depositID,
		Status:      storage.ProposalStatusVoting,
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		Timestamp:   0, // Will be set by storage layer
	}

	if err := p.storage.StoreDepositMintProposal(ctx, proposal); err != nil {
		return fmt.Errorf("failed to store deposit mint proposal: %w", err)
	}

	return nil
}

// parseBurnPrepaidEvent parses BurnPrepaid(address indexed user, uint256 amount)
func (p *SystemContractEventParser) parseBurnPrepaidEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 2 {
		return fmt.Errorf("invalid BurnPrepaid event: expected 2 topics, got %d", len(log.Topics))
	}
	if len(log.Data) != 32 {
		return fmt.Errorf("invalid BurnPrepaid event: expected 32 bytes data, got %d", len(log.Data))
	}

	user := common.BytesToAddress(log.Topics[1].Bytes())
	amount := new(big.Int).SetBytes(log.Data)

	// This is a prepaid burn, actual burn happens in BurnExecuted
	p.logger.Debug("burn prepaid",
		zap.String("user", user.Hex()),
		zap.String("amount", amount.String()),
		zap.Uint64("blockNumber", log.BlockNumber))

	return nil
}

// parseBurnExecutedEvent parses BurnExecuted(address indexed from, uint256 indexed amount, string withdrawalId)
func (p *SystemContractEventParser) parseBurnExecutedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid BurnExecuted event: expected 3 topics, got %d", len(log.Topics))
	}

	from := common.BytesToAddress(log.Topics[1].Bytes())
	amount := new(big.Int).SetBytes(log.Topics[2].Bytes())

	// Parse withdrawalId from data (string is dynamic)
	var withdrawalID string
	if len(log.Data) > 0 {
		// First 32 bytes: offset to string
		// Second 32 bytes: length of string
		// Remaining bytes: string data
		if len(log.Data) >= 64 {
			offset := new(big.Int).SetBytes(log.Data[0:32]).Uint64()
			if offset+32 <= uint64(len(log.Data)) {
				length := new(big.Int).SetBytes(log.Data[offset : offset+32]).Uint64()
				dataStart := offset + 32
				if dataStart+length <= uint64(len(log.Data)) {
					withdrawalID = string(log.Data[dataStart : dataStart+length])
				}
			}
		}
	}

	event := &storage.BurnEvent{
		BlockNumber:  log.BlockNumber,
		TxHash:       log.TxHash,
		Burner:       from,
		Amount:       amount,
		Timestamp:    0, // Will be set by storage layer
		WithdrawalID: withdrawalID,
	}

	if err := p.storage.StoreBurnEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store burn event: %w", err)
	}

	// Update total supply (decrease)
	negativeAmount := new(big.Int).Neg(amount)
	if err := p.storage.UpdateTotalSupply(ctx, negativeAmount); err != nil {
		return fmt.Errorf("failed to update total supply: %w", err)
	}

	return nil
}

// parseAddressBlacklistedEvent parses AddressBlacklisted(address indexed account, uint256 indexed proposalId)
func (p *SystemContractEventParser) parseAddressBlacklistedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid AddressBlacklisted event: expected 3 topics, got %d", len(log.Topics))
	}

	account := common.BytesToAddress(log.Topics[1].Bytes())
	proposalID := new(big.Int).SetBytes(log.Topics[2].Bytes())

	event := &storage.BlacklistEvent{
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		Account:     account,
		Action:      "blacklisted",
		ProposalID:  proposalID,
		Timestamp:   0, // Will be set by storage layer
	}

	if err := p.storage.StoreBlacklistEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store blacklist event: %w", err)
	}

	// Update blacklist status index
	if err := p.storage.UpdateBlacklistStatus(ctx, account, true); err != nil {
		return fmt.Errorf("failed to update blacklist status: %w", err)
	}

	return nil
}

// parseAddressUnblacklistedEvent parses AddressUnblacklisted(address indexed account, uint256 indexed proposalId)
func (p *SystemContractEventParser) parseAddressUnblacklistedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid AddressUnblacklisted event: expected 3 topics, got %d", len(log.Topics))
	}

	account := common.BytesToAddress(log.Topics[1].Bytes())
	proposalID := new(big.Int).SetBytes(log.Topics[2].Bytes())

	event := &storage.BlacklistEvent{
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		Account:     account,
		Action:      "unblacklisted",
		ProposalID:  proposalID,
		Timestamp:   0, // Will be set by storage layer
	}

	if err := p.storage.StoreBlacklistEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store blacklist event: %w", err)
	}

	// Update blacklist status index
	if err := p.storage.UpdateBlacklistStatus(ctx, account, false); err != nil {
		return fmt.Errorf("failed to update blacklist status: %w", err)
	}

	return nil
}

// parseAuthorizedAccountAddedEvent parses AuthorizedAccountAdded(address indexed account, uint256 indexed proposalId)
func (p *SystemContractEventParser) parseAuthorizedAccountAddedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid AuthorizedAccountAdded event: expected 3 topics, got %d", len(log.Topics))
	}

	account := common.BytesToAddress(log.Topics[1].Bytes())
	proposalID := new(big.Int).SetBytes(log.Topics[2].Bytes())

	// Log for informational purposes
	p.logger.Debug("authorized account added",
		zap.String("account", account.Hex()),
		zap.String("proposalId", proposalID.String()),
		zap.Uint64("blockNumber", log.BlockNumber))

	// Could be extended to track authorized accounts in the future
	return nil
}

// parseAuthorizedAccountRemovedEvent parses AuthorizedAccountRemoved(address indexed account, uint256 indexed proposalId)
func (p *SystemContractEventParser) parseAuthorizedAccountRemovedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid AuthorizedAccountRemoved event: expected 3 topics, got %d", len(log.Topics))
	}

	account := common.BytesToAddress(log.Topics[1].Bytes())
	proposalID := new(big.Int).SetBytes(log.Topics[2].Bytes())

	// Log for informational purposes
	p.logger.Debug("authorized account removed",
		zap.String("account", account.Hex()),
		zap.String("proposalId", proposalID.String()),
		zap.Uint64("blockNumber", log.BlockNumber))

	// Could be extended to track authorized accounts in the future
	return nil
}
