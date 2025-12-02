package events

import (
	"context"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
)

// LoggingHandler logs all events (useful for debugging)
type LoggingHandler struct {
	logger Logger
}

// Logger interface for logging
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// NewLoggingHandler creates a new logging handler
func NewLoggingHandler(logger Logger) *LoggingHandler {
	return &LoggingHandler{logger: logger}
}

// EventName returns wildcard to handle all events
func (h *LoggingHandler) EventName() string {
	return "*"
}

// Handle logs the event
func (h *LoggingHandler) Handle(ctx context.Context, event *ParsedEvent) error {
	h.logger.Debug("Event received",
		"contract", event.ContractAddress.Hex(),
		"event", event.EventName,
		"block", event.BlockNumber,
		"tx", event.TxHash.Hex(),
	)
	return nil
}

// EventBusPublisher publishes events to EventBus
type EventBusPublisher struct {
	eventBus *EventBus
}

// NewEventBusPublisher creates a new EventBus publisher
func NewEventBusPublisher(eventBus *EventBus) *EventBusPublisher {
	return &EventBusPublisher{eventBus: eventBus}
}

// EventName returns wildcard to handle all events
func (h *EventBusPublisher) EventName() string {
	return "*"
}

// Handle publishes the event to EventBus
func (h *EventBusPublisher) Handle(ctx context.Context, event *ParsedEvent) error {
	if h.eventBus == nil {
		return nil
	}

	sysEvent := NewSystemContractEvent(
		event.ContractAddress,
		SystemContractEventType(event.EventName),
		event.BlockNumber,
		event.TxHash,
		event.LogIndex,
		event.Data,
	)
	h.eventBus.Publish(sysEvent)
	return nil
}

// EventTransformer transforms ParsedEvent data to typed structs
type EventTransformer struct{}

// NewEventTransformer creates a new event transformer
func NewEventTransformer() *EventTransformer {
	return &EventTransformer{}
}

// ToMintEvent converts a ParsedEvent to storage.MintEvent
func (t *EventTransformer) ToMintEvent(event *ParsedEvent) (*storage.MintEvent, error) {
	minter, ok := event.Data["minter"].(common.Address)
	if !ok {
		return nil, fmt.Errorf("missing or invalid minter field")
	}

	to, ok := event.Data["to"].(common.Address)
	if !ok {
		return nil, fmt.Errorf("missing or invalid to field")
	}

	amount, ok := event.Data["amount"].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("missing or invalid amount field")
	}

	return &storage.MintEvent{
		BlockNumber: event.BlockNumber,
		TxHash:      event.TxHash,
		Minter:      minter,
		To:          to,
		Amount:      amount,
	}, nil
}

// ToBurnEvent converts a ParsedEvent to storage.BurnEvent
func (t *EventTransformer) ToBurnEvent(event *ParsedEvent) (*storage.BurnEvent, error) {
	burner, ok := event.Data["burner"].(common.Address)
	if !ok {
		return nil, fmt.Errorf("missing or invalid burner field")
	}

	amount, ok := event.Data["amount"].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("missing or invalid amount field")
	}

	withdrawalID, _ := event.Data["withdrawalId"].(string)

	return &storage.BurnEvent{
		BlockNumber:  event.BlockNumber,
		TxHash:       event.TxHash,
		Burner:       burner,
		Amount:       amount,
		WithdrawalID: withdrawalID,
	}, nil
}

// ToProposal converts a ParsedEvent to storage.Proposal
func (t *EventTransformer) ToProposal(event *ParsedEvent) (*storage.Proposal, error) {
	proposalID, ok := event.Data["proposalId"].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("missing or invalid proposalId field")
	}

	proposer, ok := event.Data["proposer"].(common.Address)
	if !ok {
		return nil, fmt.Errorf("missing or invalid proposer field")
	}

	proposal := &storage.Proposal{
		Contract:    event.ContractAddress,
		ProposalID:  proposalID,
		Proposer:    proposer,
		Status:      storage.ProposalStatusVoting,
		BlockNumber: event.BlockNumber,
		TxHash:      event.TxHash,
	}

	// Optional fields
	if actionType, ok := event.Data["actionType"].([32]byte); ok {
		proposal.ActionType = actionType
	}
	if callData, ok := event.Data["callData"].([]byte); ok {
		proposal.CallData = callData
	}
	if memberVersion, ok := event.Data["memberVersion"].(*big.Int); ok {
		proposal.MemberVersion = memberVersion
	}
	if requiredApprovals, ok := event.Data["requiredApprovals"].(uint64); ok {
		proposal.RequiredApprovals = uint32(requiredApprovals)
	}
	if createdAt, ok := event.Data["createdAt"].(uint64); ok {
		proposal.CreatedAt = createdAt
	}

	return proposal, nil
}

// ToDepositMintProposal converts a ParsedEvent to storage.DepositMintProposal
func (t *EventTransformer) ToDepositMintProposal(event *ParsedEvent) (*storage.DepositMintProposal, error) {
	proposalID, ok := event.Data["proposalId"].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("missing or invalid proposalId field")
	}

	proposal := &storage.DepositMintProposal{
		ProposalID:  proposalID,
		Status:      storage.ProposalStatusVoting,
		BlockNumber: event.BlockNumber,
		TxHash:      event.TxHash,
	}

	if requester, ok := event.Data["requester"].(common.Address); ok {
		proposal.Requester = requester
	}
	if beneficiary, ok := event.Data["beneficiary"].(common.Address); ok {
		proposal.Beneficiary = beneficiary
	}
	if amount, ok := event.Data["amount"].(*big.Int); ok {
		proposal.Amount = amount
	}
	if depositID, ok := event.Data["depositId"].(string); ok {
		proposal.DepositID = depositID
	}
	if bankReference, ok := event.Data["bankReference"].(string); ok {
		proposal.BankReference = bankReference
	}

	return proposal, nil
}

// ToProposalVote converts a ParsedEvent to storage.ProposalVote
func (t *EventTransformer) ToProposalVote(event *ParsedEvent) (*storage.ProposalVote, error) {
	proposalID, ok := event.Data["proposalId"].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("missing or invalid proposalId field")
	}

	voter, ok := event.Data["voter"].(common.Address)
	if !ok {
		return nil, fmt.Errorf("missing or invalid voter field")
	}

	approval, _ := event.Data["approval"].(bool)

	return &storage.ProposalVote{
		Contract:    event.ContractAddress,
		ProposalID:  proposalID,
		Voter:       voter,
		Approval:    approval,
		BlockNumber: event.BlockNumber,
		TxHash:      event.TxHash,
	}, nil
}

// MintEventHandler handles Mint events
type MintEventHandler struct {
	transformer *EventTransformer
	storage     interface {
		StoreMintEvent(ctx context.Context, event *storage.MintEvent) error
	}
}

// NewMintEventHandler creates a new mint event handler
func NewMintEventHandler(strg interface {
	StoreMintEvent(ctx context.Context, event *storage.MintEvent) error
}) *MintEventHandler {
	return &MintEventHandler{
		transformer: NewEventTransformer(),
		storage:     strg,
	}
}

// EventName returns the event name
func (h *MintEventHandler) EventName() string {
	return "Mint"
}

// Handle processes and stores the mint event
func (h *MintEventHandler) Handle(ctx context.Context, event *ParsedEvent) error {
	mintEvent, err := h.transformer.ToMintEvent(event)
	if err != nil {
		return err
	}
	return h.storage.StoreMintEvent(ctx, mintEvent)
}

// BurnEventHandler handles Burn events
type BurnEventHandler struct {
	transformer *EventTransformer
	storage     interface {
		StoreBurnEvent(ctx context.Context, event *storage.BurnEvent) error
	}
}

// NewBurnEventHandler creates a new burn event handler
func NewBurnEventHandler(strg interface {
	StoreBurnEvent(ctx context.Context, event *storage.BurnEvent) error
}) *BurnEventHandler {
	return &BurnEventHandler{
		transformer: NewEventTransformer(),
		storage:     strg,
	}
}

// EventName returns the event name
func (h *BurnEventHandler) EventName() string {
	return "Burn"
}

// Handle processes and stores the burn event
func (h *BurnEventHandler) Handle(ctx context.Context, event *ParsedEvent) error {
	burnEvent, err := h.transformer.ToBurnEvent(event)
	if err != nil {
		return err
	}
	return h.storage.StoreBurnEvent(ctx, burnEvent)
}

// ProposalCreatedHandler handles ProposalCreated events
type ProposalCreatedHandler struct {
	transformer *EventTransformer
	storage     interface {
		StoreProposal(ctx context.Context, proposal *storage.Proposal) error
	}
}

// NewProposalCreatedHandler creates a new proposal created handler
func NewProposalCreatedHandler(strg interface {
	StoreProposal(ctx context.Context, proposal *storage.Proposal) error
}) *ProposalCreatedHandler {
	return &ProposalCreatedHandler{
		transformer: NewEventTransformer(),
		storage:     strg,
	}
}

// EventName returns the event name
func (h *ProposalCreatedHandler) EventName() string {
	return "ProposalCreated"
}

// Handle processes and stores the proposal
func (h *ProposalCreatedHandler) Handle(ctx context.Context, event *ParsedEvent) error {
	proposal, err := h.transformer.ToProposal(event)
	if err != nil {
		return err
	}
	return h.storage.StoreProposal(ctx, proposal)
}

// DepositMintProposedHandler handles DepositMintProposed events
type DepositMintProposedHandler struct {
	transformer *EventTransformer
	storage     interface {
		StoreDepositMintProposal(ctx context.Context, proposal *storage.DepositMintProposal) error
	}
}

// NewDepositMintProposedHandler creates a new deposit mint proposed handler
func NewDepositMintProposedHandler(strg interface {
	StoreDepositMintProposal(ctx context.Context, proposal *storage.DepositMintProposal) error
}) *DepositMintProposedHandler {
	return &DepositMintProposedHandler{
		transformer: NewEventTransformer(),
		storage:     strg,
	}
}

// EventName returns the event name
func (h *DepositMintProposedHandler) EventName() string {
	return "DepositMintProposed"
}

// Handle processes and stores the deposit mint proposal
func (h *DepositMintProposedHandler) Handle(ctx context.Context, event *ParsedEvent) error {
	proposal, err := h.transformer.ToDepositMintProposal(event)
	if err != nil {
		return err
	}
	return h.storage.StoreDepositMintProposal(ctx, proposal)
}

// VoteCastHandler handles VoteCast events
type VoteCastHandler struct {
	transformer *EventTransformer
	storage     interface {
		StoreProposalVote(ctx context.Context, vote *storage.ProposalVote) error
	}
}

// NewVoteCastHandler creates a new vote cast handler
func NewVoteCastHandler(strg interface {
	StoreProposalVote(ctx context.Context, vote *storage.ProposalVote) error
}) *VoteCastHandler {
	return &VoteCastHandler{
		transformer: NewEventTransformer(),
		storage:     strg,
	}
}

// EventName returns the event name
func (h *VoteCastHandler) EventName() string {
	return "VoteCast"
}

// Handle processes and stores the vote
func (h *VoteCastHandler) Handle(ctx context.Context, event *ParsedEvent) error {
	vote, err := h.transformer.ToProposalVote(event)
	if err != nil {
		return err
	}
	return h.storage.StoreProposalVote(ctx, vote)
}
