package events

import (
	"context"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// Re-export system contract addresses from constants package for backward compatibility
var (
	NativeCoinAdapterAddress = constants.NativeCoinAdapterAddress
	GovValidatorAddress      = constants.GovValidatorAddress
	GovMasterMinterAddress   = constants.GovMasterMinterAddress
	GovMinterAddress         = constants.GovMinterAddress
	GovCouncilAddress        = constants.GovCouncilAddress
)

// Re-export event signatures from constants package for backward compatibility
var (
	// NativeCoinAdapter events
	EventSigMint                = constants.EventSigMint
	EventSigBurn                = constants.EventSigBurn
	EventSigMinterConfigured    = constants.EventSigMinterConfigured
	EventSigMinterRemoved       = constants.EventSigMinterRemoved
	EventSigMasterMinterChanged = constants.EventSigMasterMinterChanged
	EventSigTransfer            = constants.EventSigTransfer
	EventSigApproval            = constants.EventSigApproval

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

// SystemContractEventParser parses and indexes system contract events
type SystemContractEventParser struct {
	storage  storage.SystemContractWriter
	logger   *zap.Logger
	eventBus *EventBus
}

// NewSystemContractEventParser creates a new system contract event parser
func NewSystemContractEventParser(storage storage.SystemContractWriter, logger *zap.Logger) *SystemContractEventParser {
	return &SystemContractEventParser{
		storage: storage,
		logger:  logger,
	}
}

// SetEventBus sets the event bus for publishing system contract events
func (p *SystemContractEventParser) SetEventBus(eventBus *EventBus) {
	p.eventBus = eventBus
}

// publishEvent publishes a system contract event to the event bus
func (p *SystemContractEventParser) publishEvent(contract common.Address, eventName SystemContractEventType, log *types.Log, data map[string]interface{}) {
	if p.eventBus == nil {
		return
	}

	event := NewSystemContractEvent(
		contract,
		eventName,
		log.BlockNumber,
		log.TxHash,
		log.Index,
		data,
	)

	p.eventBus.Publish(event)
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
	case EventSigMaxProposalsPerMemberUpdated:
		return p.parseMaxProposalsPerMemberUpdatedEvent(ctx, log)

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
	case EventSigProposalExecutionSkipped:
		return p.parseProposalExecutionSkippedEvent(ctx, log)

	default:
		// Unknown event, skip silently
		return nil
	}
}

// isSystemContract checks if an address is a system contract
func isSystemContract(addr common.Address) bool {
	return constants.IsSystemContract(addr)
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

	minter := common.BytesToAddress(log.Topics[1].Bytes())
	to := common.BytesToAddress(log.Topics[2].Bytes())
	amount := new(big.Int).SetBytes(log.Data)

	event := &storage.MintEvent{
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		Minter:      minter,
		To:          to,
		Amount:      amount,
		Timestamp:   0, // Will be set by storage layer
	}

	if err := p.storage.StoreMintEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store mint event: %w", err)
	}

	// Update total supply
	if err := p.storage.UpdateTotalSupply(ctx, amount); err != nil {
		return fmt.Errorf("failed to update total supply: %w", err)
	}

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventMint, log, map[string]interface{}{
		"minter": minter.Hex(),
		"to":     to.Hex(),
		"amount": amount.String(),
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventBurn, log, map[string]interface{}{
		"burner": event.Burner.Hex(),
		"amount": amount.String(),
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventMinterConfigured, log, map[string]interface{}{
		"minter":    minter.Hex(),
		"allowance": allowance.String(),
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventMinterRemoved, log, map[string]interface{}{
		"minter": minter.Hex(),
	})

	return nil
}

// parseMasterMinterChangedEvent parses MasterMinterChanged(address indexed newMasterMinter)
func (p *SystemContractEventParser) parseMasterMinterChangedEvent(ctx context.Context, log *types.Log) error {
	newMasterMinter := common.BytesToAddress(log.Topics[1].Bytes())

	// This event is informational only, no storage action needed for now
	// Could be extended in the future to track master minter history
	p.logger.Debug("master minter changed",
		zap.String("newMasterMinter", newMasterMinter.Hex()),
		zap.Uint64("blockNumber", log.BlockNumber))

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventMasterMinterChanged, log, map[string]interface{}{
		"newMasterMinter": newMasterMinter.Hex(),
	})

	return nil
}

// Placeholder parsers for other events (to be implemented)
// These will be implemented in subsequent iterations

// parseProposalCreatedEvent parses ProposalCreated(uint256 indexed proposalId, address indexed proposer, bytes32 actionType, uint256 memberVersion, uint256 requiredApprovals, bytes callData)
func (p *SystemContractEventParser) parseProposalCreatedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalCreated event: expected 3 topics, got %d", len(log.Topics))
	}

	// Parse topics
	// Topics[0] = event signature
	// Topics[1] = proposalId (indexed)
	// Topics[2] = proposer (indexed)
	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())
	proposer := common.BytesToAddress(log.Topics[2].Bytes())

	// Parse data: actionType (32 bytes) + memberVersion (32 bytes) + requiredApprovals (32 bytes) + callData offset (32 bytes) + callData
	if len(log.Data) < 128 {
		return fmt.Errorf("invalid ProposalCreated event: data too short, got %d bytes", len(log.Data))
	}

	// actionType: bytes32 at offset 0
	var actionType [32]byte
	copy(actionType[:], log.Data[0:32])

	// memberVersion: uint256 at offset 32
	memberVersion := new(big.Int).SetBytes(log.Data[32:64])

	// requiredApprovals: uint256 at offset 64
	requiredApprovals := uint32(new(big.Int).SetBytes(log.Data[64:96]).Uint64())

	// callData: dynamic bytes starting at offset pointed by data[96:128]
	callDataOffset := new(big.Int).SetBytes(log.Data[96:128]).Uint64()
	var callData []byte
	if callDataOffset < uint64(len(log.Data)) && callDataOffset+32 <= uint64(len(log.Data)) {
		callDataLength := new(big.Int).SetBytes(log.Data[callDataOffset : callDataOffset+32]).Uint64()
		callDataStart := callDataOffset + 32
		callDataEnd := callDataStart + callDataLength
		if callDataEnd <= uint64(len(log.Data)) {
			callData = log.Data[callDataStart:callDataEnd]
		}
	}

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
		CreatedAt:         0, // Will be set from block timestamp by storage layer
		ExecutedAt:        nil,
		BlockNumber:       log.BlockNumber,
		TxHash:            log.TxHash,
	}

	if err := p.storage.StoreProposal(ctx, proposal); err != nil {
		return fmt.Errorf("failed to store proposal: %w", err)
	}

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventProposalCreated, log, map[string]interface{}{
		"proposalId":        proposalID.String(),
		"proposer":          proposer.Hex(),
		"actionType":        common.Bytes2Hex(actionType[:]),
		"memberVersion":     memberVersion.String(),
		"requiredApprovals": requiredApprovals,
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventProposalVoted, log, map[string]interface{}{
		"proposalId": proposalID.String(),
		"voter":      voter.Hex(),
		"approval":   approval,
	})

	return nil
}

// parseProposalApprovedEvent parses ProposalApproved(uint256 indexed proposalId, address indexed approver, uint256 approved, uint256 rejected)
func (p *SystemContractEventParser) parseProposalApprovedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalApproved event: expected 3 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())
	approver := common.BytesToAddress(log.Topics[2].Bytes())

	// Update proposal status to Approved
	if err := p.storage.UpdateProposalStatus(ctx, log.Address, proposalID, storage.ProposalStatusApproved, 0); err != nil {
		return fmt.Errorf("failed to update proposal status: %w", err)
	}

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventProposalApproved, log, map[string]interface{}{
		"proposalId": proposalID.String(),
		"approver":   approver.Hex(),
	})

	return nil
}

// parseProposalRejectedEvent parses ProposalRejected(uint256 indexed proposalId, address indexed rejector, uint256 approved, uint256 rejected)
func (p *SystemContractEventParser) parseProposalRejectedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalRejected event: expected 3 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())
	rejector := common.BytesToAddress(log.Topics[2].Bytes())

	// Update proposal status to Rejected
	if err := p.storage.UpdateProposalStatus(ctx, log.Address, proposalID, storage.ProposalStatusRejected, 0); err != nil {
		return fmt.Errorf("failed to update proposal status: %w", err)
	}

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventProposalRejected, log, map[string]interface{}{
		"proposalId": proposalID.String(),
		"rejector":   rejector.Hex(),
	})

	return nil
}

// parseProposalExecutedEvent parses ProposalExecuted(uint256 indexed proposalId, address indexed executor, bool success)
func (p *SystemContractEventParser) parseProposalExecutedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalExecuted event: expected 3 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())
	executor := common.BytesToAddress(log.Topics[2].Bytes())

	// Parse success from data
	var success bool
	if len(log.Data) >= 32 {
		success = new(big.Int).SetBytes(log.Data[0:32]).Uint64() != 0
	}

	// Update proposal status to Executed with current block number as execution time
	if err := p.storage.UpdateProposalStatus(ctx, log.Address, proposalID, storage.ProposalStatusExecuted, log.BlockNumber); err != nil {
		return fmt.Errorf("failed to update proposal status: %w", err)
	}

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventProposalExecuted, log, map[string]interface{}{
		"proposalId": proposalID.String(),
		"executor":   executor.Hex(),
		"success":    success,
	})

	return nil
}

// parseProposalFailedEvent parses ProposalFailed(uint256 indexed proposalId, address indexed executor, bytes reason)
func (p *SystemContractEventParser) parseProposalFailedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalFailed event: expected 3 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())
	executor := common.BytesToAddress(log.Topics[2].Bytes())

	// Update proposal status to Failed
	if err := p.storage.UpdateProposalStatus(ctx, log.Address, proposalID, storage.ProposalStatusFailed, log.BlockNumber); err != nil {
		return fmt.Errorf("failed to update proposal status: %w", err)
	}

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventProposalFailed, log, map[string]interface{}{
		"proposalId": proposalID.String(),
		"executor":   executor.Hex(),
	})

	return nil
}

// parseProposalExpiredEvent parses ProposalExpired(uint256 indexed proposalId, address indexed executor)
func (p *SystemContractEventParser) parseProposalExpiredEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalExpired event: expected 3 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())
	executor := common.BytesToAddress(log.Topics[2].Bytes())

	// Update proposal status to Expired
	if err := p.storage.UpdateProposalStatus(ctx, log.Address, proposalID, storage.ProposalStatusExpired, 0); err != nil {
		return fmt.Errorf("failed to update proposal status: %w", err)
	}

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventProposalExpired, log, map[string]interface{}{
		"proposalId": proposalID.String(),
		"executor":   executor.Hex(),
	})

	return nil
}

// parseProposalCancelledEvent parses ProposalCancelled(uint256 indexed proposalId, address indexed canceller)
func (p *SystemContractEventParser) parseProposalCancelledEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalCancelled event: expected 3 topics, got %d", len(log.Topics))
	}

	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())
	canceller := common.BytesToAddress(log.Topics[2].Bytes())

	// Update proposal status to Cancelled
	if err := p.storage.UpdateProposalStatus(ctx, log.Address, proposalID, storage.ProposalStatusCancelled, 0); err != nil {
		return fmt.Errorf("failed to update proposal status: %w", err)
	}

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventProposalCancelled, log, map[string]interface{}{
		"proposalId": proposalID.String(),
		"canceller":  canceller.Hex(),
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventMemberAdded, log, map[string]interface{}{
		"member":       member.Hex(),
		"totalMembers": totalMembers,
		"newQuorum":    newQuorum,
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventMemberRemoved, log, map[string]interface{}{
		"member":       member.Hex(),
		"totalMembers": totalMembers,
		"newQuorum":    newQuorum,
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventMemberChanged, log, map[string]interface{}{
		"oldMember": oldMember.Hex(),
		"newMember": newMember.Hex(),
	})

	return nil
}

// parseQuorumUpdatedEvent parses QuorumUpdated(uint32 oldQuorum, uint32 newQuorum)
func (p *SystemContractEventParser) parseQuorumUpdatedEvent(ctx context.Context, log *types.Log) error {
	// This event is informational only, quorum updates are tracked via MemberAdded/Removed
	// Log for debugging purposes
	var oldQuorum, newQuorum uint32
	if len(log.Data) >= 64 {
		oldQuorum = uint32(new(big.Int).SetBytes(log.Data[0:32]).Uint64())
		newQuorum = uint32(new(big.Int).SetBytes(log.Data[32:64]).Uint64())
		p.logger.Debug("quorum updated",
			zap.String("contract", log.Address.Hex()),
			zap.Uint32("oldQuorum", oldQuorum),
			zap.Uint32("newQuorum", newQuorum),
			zap.Uint64("blockNumber", log.BlockNumber))
	}

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventQuorumUpdated, log, map[string]interface{}{
		"oldQuorum": oldQuorum,
		"newQuorum": newQuorum,
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventGasTipUpdated, log, map[string]interface{}{
		"oldTip":  oldTip.String(),
		"newTip":  newTip.String(),
		"updater": updater.Hex(),
	})

	return nil
}

// parseMaxMinterAllowanceUpdatedEvent parses MaxMinterAllowanceUpdated(uint256 oldLimit, uint256 newLimit)
func (p *SystemContractEventParser) parseMaxMinterAllowanceUpdatedEvent(ctx context.Context, log *types.Log) error {
	// This event is informational only, no storage action needed
	var oldLimit, newLimit *big.Int
	if len(log.Data) >= 64 {
		oldLimit = new(big.Int).SetBytes(log.Data[0:32])
		newLimit = new(big.Int).SetBytes(log.Data[32:64])
		p.logger.Debug("max minter allowance updated",
			zap.String("oldLimit", oldLimit.String()),
			zap.String("newLimit", newLimit.String()),
			zap.Uint64("blockNumber", log.BlockNumber))
	}

	// Publish event to EventBus
	oldLimitStr := "0"
	newLimitStr := "0"
	if oldLimit != nil {
		oldLimitStr = oldLimit.String()
	}
	if newLimit != nil {
		newLimitStr = newLimit.String()
	}
	p.publishEvent(log.Address, SystemContractEventMaxMinterAllowanceUpdated, log, map[string]interface{}{
		"oldLimit": oldLimitStr,
		"newLimit": newLimitStr,
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventEmergencyPaused, log, map[string]interface{}{
		"proposalId": proposalID.String(),
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventEmergencyUnpaused, log, map[string]interface{}{
		"proposalId": proposalID.String(),
	})

	return nil
}

// parseDepositMintProposedEvent parses DepositMintProposed(uint256 indexed proposalId, string indexed depositId, address indexed requester, address beneficiary, uint256 amount, string bankReference)
func (p *SystemContractEventParser) parseDepositMintProposedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 4 {
		return fmt.Errorf("invalid DepositMintProposed event: expected 4 topics, got %d", len(log.Topics))
	}

	// Parse topics
	// Topics[0] = event signature
	// Topics[1] = proposalId (indexed uint256)
	// Topics[2] = depositId hash (indexed string becomes keccak256 hash, not directly usable)
	// Topics[3] = requester (indexed address)
	proposalID := new(big.Int).SetBytes(log.Topics[1].Bytes())
	// depositIdHash := log.Topics[2] // Indexed string is hashed, we need to parse from data or use hash
	requester := common.BytesToAddress(log.Topics[3].Bytes())

	// Parse data: beneficiary (address padded to 32 bytes) + amount (uint256) + bankReference offset + depositId (from callData)
	if len(log.Data) < 96 {
		return fmt.Errorf("invalid DepositMintProposed event: data too short, got %d bytes", len(log.Data))
	}

	// beneficiary: address at offset 0 (padded to 32 bytes)
	beneficiary := common.BytesToAddress(log.Data[12:32])

	// amount: uint256 at offset 32
	amount := new(big.Int).SetBytes(log.Data[32:64])

	// bankReference: dynamic string starting at offset pointed by data[64:96]
	var bankReference string
	bankRefOffset := new(big.Int).SetBytes(log.Data[64:96]).Uint64()
	if bankRefOffset < uint64(len(log.Data)) && bankRefOffset+32 <= uint64(len(log.Data)) {
		bankRefLength := new(big.Int).SetBytes(log.Data[bankRefOffset : bankRefOffset+32]).Uint64()
		bankRefStart := bankRefOffset + 32
		bankRefEnd := bankRefStart + bankRefLength
		if bankRefEnd <= uint64(len(log.Data)) {
			bankReference = string(log.Data[bankRefStart:bankRefEnd])
		}
	}

	proposal := &storage.DepositMintProposal{
		ProposalID:    proposalID,
		Requester:     requester,
		Beneficiary:   beneficiary,
		Amount:        amount,
		DepositID:     "", // Indexed string is hashed in topics, need to track via proposal lookup
		BankReference: bankReference,
		Status:        storage.ProposalStatusVoting,
		BlockNumber:   log.BlockNumber,
		TxHash:        log.TxHash,
		Timestamp:     0, // Will be set by storage layer
	}

	if err := p.storage.StoreDepositMintProposal(ctx, proposal); err != nil {
		return fmt.Errorf("failed to store deposit mint proposal: %w", err)
	}

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventDepositMintProposed, log, map[string]interface{}{
		"proposalId":    proposalID.String(),
		"requester":     requester.Hex(),
		"beneficiary":   beneficiary.Hex(),
		"amount":        amount.String(),
		"bankReference": bankReference,
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventBurnPrepaid, log, map[string]interface{}{
		"user":   user.Hex(),
		"amount": amount.String(),
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventBurnExecuted, log, map[string]interface{}{
		"from":         from.Hex(),
		"amount":       amount.String(),
		"withdrawalId": withdrawalID,
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventAddressBlacklisted, log, map[string]interface{}{
		"account":    account.Hex(),
		"proposalId": proposalID.String(),
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventAddressUnblacklisted, log, map[string]interface{}{
		"account":    account.Hex(),
		"proposalId": proposalID.String(),
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventAuthorizedAccountAdded, log, map[string]interface{}{
		"account":    account.Hex(),
		"proposalId": proposalID.String(),
	})

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

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventAuthorizedAccountRemoved, log, map[string]interface{}{
		"account":    account.Hex(),
		"proposalId": proposalID.String(),
	})

	// Could be extended to track authorized accounts in the future
	return nil
}

// parseMaxProposalsPerMemberUpdatedEvent parses MaxProposalsPerMemberUpdated(uint256 oldMax, uint256 newMax)
func (p *SystemContractEventParser) parseMaxProposalsPerMemberUpdatedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 1 {
		return fmt.Errorf("invalid MaxProposalsPerMemberUpdated event: expected 1 topic, got %d", len(log.Topics))
	}
	if len(log.Data) != 64 {
		return fmt.Errorf("invalid MaxProposalsPerMemberUpdated event: expected 64 bytes data, got %d", len(log.Data))
	}

	oldMax := new(big.Int).SetBytes(log.Data[0:32]).Uint64()
	newMax := new(big.Int).SetBytes(log.Data[32:64]).Uint64()

	event := &storage.MaxProposalsUpdateEvent{
		Contract:    log.Address,
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		OldMax:      oldMax,
		NewMax:      newMax,
		Timestamp:   0, // Will be set by storage layer
	}

	if err := p.storage.StoreMaxProposalsUpdateEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store max proposals update event: %w", err)
	}

	p.logger.Debug("max proposals per member updated",
		zap.String("contract", log.Address.Hex()),
		zap.Uint64("oldMax", oldMax),
		zap.Uint64("newMax", newMax),
		zap.Uint64("blockNumber", log.BlockNumber))

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventMaxProposalsUpdated, log, map[string]interface{}{
		"oldMax": oldMax,
		"newMax": newMax,
	})

	return nil
}

// parseProposalExecutionSkippedEvent parses ProposalExecutionSkipped(address indexed account, uint256 indexed proposalId, string reason)
func (p *SystemContractEventParser) parseProposalExecutionSkippedEvent(ctx context.Context, log *types.Log) error {
	if len(log.Topics) != 3 {
		return fmt.Errorf("invalid ProposalExecutionSkipped event: expected 3 topics, got %d", len(log.Topics))
	}

	account := common.BytesToAddress(log.Topics[1].Bytes())
	proposalID := new(big.Int).SetBytes(log.Topics[2].Bytes())

	// Parse reason from data (dynamic string)
	var reason string
	if len(log.Data) >= 64 {
		// First 32 bytes: offset to string
		offset := new(big.Int).SetBytes(log.Data[0:32]).Uint64()
		if offset+32 <= uint64(len(log.Data)) {
			length := new(big.Int).SetBytes(log.Data[offset : offset+32]).Uint64()
			dataStart := offset + 32
			if dataStart+length <= uint64(len(log.Data)) {
				reason = string(log.Data[dataStart : dataStart+length])
			}
		}
	}

	event := &storage.ProposalExecutionSkippedEvent{
		Contract:    log.Address,
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		Account:     account,
		ProposalID:  proposalID,
		Reason:      reason,
		Timestamp:   0, // Will be set by storage layer
	}

	if err := p.storage.StoreProposalExecutionSkippedEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to store proposal execution skipped event: %w", err)
	}

	p.logger.Debug("proposal execution skipped",
		zap.String("account", account.Hex()),
		zap.String("proposalId", proposalID.String()),
		zap.String("reason", reason),
		zap.Uint64("blockNumber", log.BlockNumber))

	// Publish event to EventBus
	p.publishEvent(log.Address, SystemContractEventProposalExecutionSkipped, log, map[string]interface{}{
		"account":    account.Hex(),
		"proposalId": proposalID.String(),
		"reason":     reason,
	})

	return nil
}
