package events

import (
	"context"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// SystemContractParserAdapter adapts SystemContractEventParser to ContractParser interface
type SystemContractParserAdapter struct {
	parser   *SystemContractEventParser
	address  common.Address
	name     string
	eventSig map[common.Hash]string
}

// NewNativeCoinAdapterParser creates a parser for NativeCoinAdapter contract
func NewNativeCoinAdapterParser(storage storage.SystemContractWriter, logger *zap.Logger, eventBus *EventBus) *SystemContractParserAdapter {
	parser := NewSystemContractEventParser(storage, logger)
	parser.SetEventBus(eventBus)

	eventSig := map[common.Hash]string{
		EventSigMint:                "Mint",
		EventSigBurn:                "Burn",
		EventSigMinterConfigured:    "MinterConfigured",
		EventSigMinterRemoved:       "MinterRemoved",
		EventSigMasterMinterChanged: "MasterMinterChanged",
		EventSigTransfer:            "Transfer",
		EventSigApproval:            "Approval",
	}

	return &SystemContractParserAdapter{
		parser:   parser,
		address:  NativeCoinAdapterAddress,
		name:     "NativeCoinAdapter",
		eventSig: eventSig,
	}
}

// NewGovValidatorParser creates a parser for GovValidator contract
func NewGovValidatorParser(storage storage.SystemContractWriter, logger *zap.Logger, eventBus *EventBus) *SystemContractParserAdapter {
	parser := NewSystemContractEventParser(storage, logger)
	parser.SetEventBus(eventBus)

	eventSig := map[common.Hash]string{
		EventSigGasTipUpdated:                "GasTipUpdated",
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
	}

	return &SystemContractParserAdapter{
		parser:   parser,
		address:  GovValidatorAddress,
		name:     "GovValidator",
		eventSig: eventSig,
	}
}

// NewGovMasterMinterParser creates a parser for GovMasterMinter contract
func NewGovMasterMinterParser(storage storage.SystemContractWriter, logger *zap.Logger, eventBus *EventBus) *SystemContractParserAdapter {
	parser := NewSystemContractEventParser(storage, logger)
	parser.SetEventBus(eventBus)

	eventSig := map[common.Hash]string{
		EventSigMaxMinterAllowanceUpdated:    "MaxMinterAllowanceUpdated",
		EventSigEmergencyPaused:              "EmergencyPaused",
		EventSigEmergencyUnpaused:            "EmergencyUnpaused",
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
	}

	return &SystemContractParserAdapter{
		parser:   parser,
		address:  GovMasterMinterAddress,
		name:     "GovMasterMinter",
		eventSig: eventSig,
	}
}

// NewGovMinterParser creates a parser for GovMinter contract
func NewGovMinterParser(storage storage.SystemContractWriter, logger *zap.Logger, eventBus *EventBus) *SystemContractParserAdapter {
	parser := NewSystemContractEventParser(storage, logger)
	parser.SetEventBus(eventBus)

	eventSig := map[common.Hash]string{
		EventSigDepositMintProposed:          "DepositMintProposed",
		EventSigBurnPrepaid:                  "BurnPrepaid",
		EventSigBurnExecuted:                 "BurnExecuted",
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
	}

	return &SystemContractParserAdapter{
		parser:   parser,
		address:  GovMinterAddress,
		name:     "GovMinter",
		eventSig: eventSig,
	}
}

// NewGovCouncilParser creates a parser for GovCouncil contract
func NewGovCouncilParser(storage storage.SystemContractWriter, logger *zap.Logger, eventBus *EventBus) *SystemContractParserAdapter {
	parser := NewSystemContractEventParser(storage, logger)
	parser.SetEventBus(eventBus)

	eventSig := map[common.Hash]string{
		EventSigAddressBlacklisted:           "AddressBlacklisted",
		EventSigAddressUnblacklisted:         "AddressUnblacklisted",
		EventSigAuthorizedAccountAdded:       "AuthorizedAccountAdded",
		EventSigAuthorizedAccountRemoved:     "AuthorizedAccountRemoved",
		EventSigProposalExecutionSkipped:     "ProposalExecutionSkipped",
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
	}

	return &SystemContractParserAdapter{
		parser:   parser,
		address:  GovCouncilAddress,
		name:     "GovCouncil",
		eventSig: eventSig,
	}
}

// ContractAddress returns the contract address
func (a *SystemContractParserAdapter) ContractAddress() common.Address {
	return a.address
}

// ContractName returns the contract name
func (a *SystemContractParserAdapter) ContractName() string {
	return a.name
}

// SupportedEvents returns supported event names
func (a *SystemContractParserAdapter) SupportedEvents() []string {
	events := make([]string, 0, len(a.eventSig))
	for _, name := range a.eventSig {
		events = append(events, name)
	}
	return events
}

// CanParse checks if this adapter can parse the log
func (a *SystemContractParserAdapter) CanParse(log *types.Log) bool {
	if log.Address != a.address {
		return false
	}
	if len(log.Topics) == 0 {
		return false
	}
	_, ok := a.eventSig[log.Topics[0]]
	return ok
}

// Parse parses the log and returns a ParsedEvent
func (a *SystemContractParserAdapter) Parse(ctx context.Context, log *types.Log) (*ParsedEvent, error) {
	if len(log.Topics) == 0 {
		return nil, nil
	}

	eventName, ok := a.eventSig[log.Topics[0]]
	if !ok {
		return nil, nil
	}

	// Use existing parser to index the event
	if err := a.parser.parseAndIndexLog(ctx, log); err != nil {
		return nil, err
	}

	return &ParsedEvent{
		ContractAddress: log.Address,
		ContractName:    a.name,
		EventName:       eventName,
		EventSig:        log.Topics[0],
		BlockNumber:     log.BlockNumber,
		TxHash:          log.TxHash,
		LogIndex:        log.Index,
		RawLog:          log,
	}, nil
}

// SystemContractParserFactory creates all system contract parsers
type SystemContractParserFactory struct {
	storage  storage.SystemContractWriter
	logger   *zap.Logger
	eventBus *EventBus
}

// NewSystemContractParserFactory creates a new factory
func NewSystemContractParserFactory(storage storage.SystemContractWriter, logger *zap.Logger, eventBus *EventBus) *SystemContractParserFactory {
	return &SystemContractParserFactory{
		storage:  storage,
		logger:   logger,
		eventBus: eventBus,
	}
}

// CreateAllParsers creates parsers for all system contracts
func (f *SystemContractParserFactory) CreateAllParsers() []ContractParser {
	return []ContractParser{
		NewNativeCoinAdapterParser(f.storage, f.logger, f.eventBus),
		NewGovValidatorParser(f.storage, f.logger, f.eventBus),
		NewGovMasterMinterParser(f.storage, f.logger, f.eventBus),
		NewGovMinterParser(f.storage, f.logger, f.eventBus),
		NewGovCouncilParser(f.storage, f.logger, f.eventBus),
	}
}

// RegisterAllParsers registers all system contract parsers with the registry
func (f *SystemContractParserFactory) RegisterAllParsers(registry *ParserRegistry) error {
	parsers := f.CreateAllParsers()
	for _, parser := range parsers {
		if err := registry.RegisterParser(parser); err != nil {
			return err
		}
	}
	return nil
}

// SetupDynamicParser creates and configures a DynamicEventParser with all system contracts
func SetupDynamicParser(storage storage.SystemContractWriter, logger *zap.Logger) *DynamicEventParser {
	eventBus := NewEventBus(constants.DefaultPublishBufferSize, constants.DefaultSubscribeBufferSize)
	parser := NewDynamicEventParser(eventBus)

	factory := NewSystemContractParserFactory(storage, logger, eventBus)
	parsers := factory.CreateAllParsers()

	for _, p := range parsers {
		_ = parser.RegisterCustomParser(p)
	}

	return parser
}
