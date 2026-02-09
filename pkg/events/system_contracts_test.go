package events

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// mockSystemContractWriter implements storage.SystemContractWriter for testing
type mockSystemContractWriter struct {
	mintEvents                    []*storage.MintEvent
	burnEvents                    []*storage.BurnEvent
	minterConfigEvents            []*storage.MinterConfigEvent
	proposals                     []*storage.Proposal
	proposalVotes                 []*storage.ProposalVote
	gasTipEvents                  []*storage.GasTipUpdateEvent
	blacklistEvents               []*storage.BlacklistEvent
	memberChangeEvents            []*storage.MemberChangeEvent
	emergencyPauseEvents          []*storage.EmergencyPauseEvent
	depositMintProposals          []*storage.DepositMintProposal
	maxProposalsEvents            []*storage.MaxProposalsUpdateEvent
	proposalExecSkippedEvents     []*storage.ProposalExecutionSkippedEvent
	authorizedAccountEvents       []*storage.AuthorizedAccountEvent
	totalSupplyDelta              *big.Int
	activeMinters                 map[common.Address]bool
	activeValidators              map[common.Address]bool
	blacklistStatus               map[common.Address]bool
	proposalStatusUpdates         []proposalStatusUpdate
	storeErr                      error
}

type proposalStatusUpdate struct {
	contract   common.Address
	proposalID *big.Int
	status     storage.ProposalStatus
	executedAt uint64
}

func newMockWriter() *mockSystemContractWriter {
	return &mockSystemContractWriter{
		totalSupplyDelta: big.NewInt(0),
		activeMinters:    make(map[common.Address]bool),
		activeValidators: make(map[common.Address]bool),
		blacklistStatus:  make(map[common.Address]bool),
	}
}

func (m *mockSystemContractWriter) IndexSystemContractEvent(_ context.Context, _ *types.Log) error {
	return nil
}
func (m *mockSystemContractWriter) IndexSystemContractEvents(_ context.Context, _ []*types.Log) error {
	return nil
}
func (m *mockSystemContractWriter) StoreMintEvent(_ context.Context, e *storage.MintEvent) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.mintEvents = append(m.mintEvents, e)
	return nil
}
func (m *mockSystemContractWriter) StoreBurnEvent(_ context.Context, e *storage.BurnEvent) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.burnEvents = append(m.burnEvents, e)
	return nil
}
func (m *mockSystemContractWriter) StoreMinterConfigEvent(_ context.Context, e *storage.MinterConfigEvent) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.minterConfigEvents = append(m.minterConfigEvents, e)
	return nil
}
func (m *mockSystemContractWriter) StoreProposal(_ context.Context, p *storage.Proposal) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.proposals = append(m.proposals, p)
	return nil
}
func (m *mockSystemContractWriter) UpdateProposalStatus(_ context.Context, contract common.Address, proposalID *big.Int, status storage.ProposalStatus, executedAt uint64) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.proposalStatusUpdates = append(m.proposalStatusUpdates, proposalStatusUpdate{contract, proposalID, status, executedAt})
	return nil
}
func (m *mockSystemContractWriter) StoreProposalVote(_ context.Context, v *storage.ProposalVote) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.proposalVotes = append(m.proposalVotes, v)
	return nil
}
func (m *mockSystemContractWriter) StoreGasTipUpdateEvent(_ context.Context, e *storage.GasTipUpdateEvent) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.gasTipEvents = append(m.gasTipEvents, e)
	return nil
}
func (m *mockSystemContractWriter) StoreBlacklistEvent(_ context.Context, e *storage.BlacklistEvent) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.blacklistEvents = append(m.blacklistEvents, e)
	return nil
}
func (m *mockSystemContractWriter) StoreValidatorChangeEvent(_ context.Context, _ *storage.ValidatorChangeEvent) error {
	return m.storeErr
}
func (m *mockSystemContractWriter) StoreMemberChangeEvent(_ context.Context, e *storage.MemberChangeEvent) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.memberChangeEvents = append(m.memberChangeEvents, e)
	return nil
}
func (m *mockSystemContractWriter) StoreEmergencyPauseEvent(_ context.Context, e *storage.EmergencyPauseEvent) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.emergencyPauseEvents = append(m.emergencyPauseEvents, e)
	return nil
}
func (m *mockSystemContractWriter) StoreDepositMintProposal(_ context.Context, p *storage.DepositMintProposal) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.depositMintProposals = append(m.depositMintProposals, p)
	return nil
}
func (m *mockSystemContractWriter) StoreMaxProposalsUpdateEvent(_ context.Context, e *storage.MaxProposalsUpdateEvent) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.maxProposalsEvents = append(m.maxProposalsEvents, e)
	return nil
}
func (m *mockSystemContractWriter) StoreProposalExecutionSkippedEvent(_ context.Context, e *storage.ProposalExecutionSkippedEvent) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.proposalExecSkippedEvents = append(m.proposalExecSkippedEvents, e)
	return nil
}
func (m *mockSystemContractWriter) StoreAuthorizedAccountEvent(_ context.Context, e *storage.AuthorizedAccountEvent) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.authorizedAccountEvents = append(m.authorizedAccountEvents, e)
	return nil
}
func (m *mockSystemContractWriter) UpdateTotalSupply(_ context.Context, delta *big.Int) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.totalSupplyDelta.Add(m.totalSupplyDelta, delta)
	return nil
}
func (m *mockSystemContractWriter) UpdateActiveMinter(_ context.Context, minter common.Address, _ *big.Int, active bool) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.activeMinters[minter] = active
	return nil
}
func (m *mockSystemContractWriter) UpdateActiveValidator(_ context.Context, validator common.Address, active bool) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.activeValidators[validator] = active
	return nil
}
func (m *mockSystemContractWriter) UpdateBlacklistStatus(_ context.Context, addr common.Address, blacklisted bool) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.blacklistStatus[addr] = blacklisted
	return nil
}

// Helper to create a test parser
func newTestParser() (*SystemContractEventParser, *mockSystemContractWriter) {
	mock := newMockWriter()
	parser := NewSystemContractEventParser(mock, zap.NewNop())
	return parser, mock
}

// ========== Core Tests ==========

func TestNewSystemContractEventParser(t *testing.T) {
	parser, _ := newTestParser()
	if parser == nil {
		t.Fatal("expected non-nil parser")
	}
}

func TestSystemContractEventParser_SetEventBus(t *testing.T) {
	parser, _ := newTestParser()
	bus := NewEventBus(100, 100)
	parser.SetEventBus(bus)
	if parser.eventBus != bus {
		t.Error("expected event bus to be set")
	}
}

func TestSystemContractEventParser_PublishEvent_NilBus(t *testing.T) {
	parser, _ := newTestParser()
	// Should not panic with nil eventBus
	parser.publishEvent(common.Address{}, SystemContractEventMint, &types.Log{}, nil)
}

func TestSystemContractEventParser_PublishEvent_WithBus(t *testing.T) {
	parser, _ := newTestParser()
	bus := NewEventBus(100, 100)
	parser.SetEventBus(bus)
	// Should not panic
	parser.publishEvent(common.Address{}, SystemContractEventMint, &types.Log{}, map[string]interface{}{"test": "value"})
}

// ========== ParseAndIndexLogs Tests ==========

func TestParseAndIndexLogs_Empty(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()
	if err := parser.ParseAndIndexLogs(ctx, nil); err != nil {
		t.Fatalf("expected no error: %v", err)
	}
}

func TestParseAndIndexLogs_NonSystemContract(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()
	log := &types.Log{
		Address: common.HexToAddress("0xdead"),
		Topics:  []common.Hash{common.HexToHash("0xbeef")},
	}
	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("expected no error for non-system contract: %v", err)
	}
}

func TestParseAndIndexLogs_NoTopics(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()
	log := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{},
	}
	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("expected no error for log with no topics: %v", err)
	}
}

func TestParseAndIndexLogs_UnknownEvent(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()
	log := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{common.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")},
	}
	// Unknown events are silently skipped
	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("expected no error for unknown event: %v", err)
	}
}

func TestParseAndIndexLogs_ContinuesOnError(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()

	// Mint event with invalid data (wrong topic count) - will error but continue
	invalidLog := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{constants.EventSigMint}, // Missing indexed topics
	}
	validLog := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{common.HexToHash("0xffffffff")}, // Unknown, silently skipped
	}

	// Should not return error even though first log fails
	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{invalidLog, validLog}); err != nil {
		t.Fatalf("expected no error (continues on error): %v", err)
	}
}

// ========== Mint Event Tests ==========

func TestParseMintEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	minter := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	to := common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	amount := big.NewInt(1000)

	log := &types.Log{
		Address:     constants.NativeCoinAdapterAddress,
		Topics:      []common.Hash{constants.EventSigMint, common.BytesToHash(minter.Bytes()), common.BytesToHash(to.Bytes())},
		Data:        common.LeftPadBytes(amount.Bytes(), 32),
		BlockNumber: 100,
		TxHash:      common.HexToHash("0xabc"),
	}

	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("ParseAndIndexLogs error: %v", err)
	}

	if len(mock.mintEvents) != 1 {
		t.Fatalf("expected 1 mint event, got %d", len(mock.mintEvents))
	}
	if mock.mintEvents[0].Minter != minter {
		t.Errorf("expected minter %s, got %s", minter.Hex(), mock.mintEvents[0].Minter.Hex())
	}
	if mock.mintEvents[0].To != to {
		t.Errorf("expected to %s, got %s", to.Hex(), mock.mintEvents[0].To.Hex())
	}
	if mock.mintEvents[0].Amount.Cmp(amount) != 0 {
		t.Errorf("expected amount %s, got %s", amount, mock.mintEvents[0].Amount)
	}
	if mock.totalSupplyDelta.Cmp(amount) != 0 {
		t.Errorf("expected total supply delta %s, got %s", amount, mock.totalSupplyDelta)
	}
}

func TestParseMintEvent_InvalidTopics(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()

	log := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{constants.EventSigMint}, // Missing indexed topics
		Data:    common.LeftPadBytes(big.NewInt(1).Bytes(), 32),
	}

	// Error is logged but doesn't stop processing
	parser.ParseAndIndexLogs(ctx, []*types.Log{log})
}

func TestParseMintEvent_InvalidData(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()

	minter := common.HexToAddress("0xaaaa")
	to := common.HexToAddress("0xbbbb")

	log := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{constants.EventSigMint, common.BytesToHash(minter.Bytes()), common.BytesToHash(to.Bytes())},
		Data:    []byte{0x01, 0x02}, // Invalid data length
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})
}

// ========== Burn Event Tests ==========

func TestParseBurnEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	burner := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	amount := big.NewInt(500)

	log := &types.Log{
		Address:     constants.NativeCoinAdapterAddress,
		Topics:      []common.Hash{constants.EventSigBurn, common.BytesToHash(burner.Bytes())},
		Data:        common.LeftPadBytes(amount.Bytes(), 32),
		BlockNumber: 200,
	}

	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("error: %v", err)
	}

	if len(mock.burnEvents) != 1 {
		t.Fatalf("expected 1 burn event, got %d", len(mock.burnEvents))
	}
	if mock.burnEvents[0].Burner != burner {
		t.Errorf("expected burner %s", burner.Hex())
	}
	// Total supply should decrease
	expected := new(big.Int).Neg(amount)
	if mock.totalSupplyDelta.Cmp(expected) != 0 {
		t.Errorf("expected total supply delta %s, got %s", expected, mock.totalSupplyDelta)
	}
}

// ========== Minter Config Event Tests ==========

func TestParseMinterConfiguredEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	minter := common.HexToAddress("0xaaaa")
	allowance := big.NewInt(10000)

	log := &types.Log{
		Address:     constants.NativeCoinAdapterAddress,
		Topics:      []common.Hash{constants.EventSigMinterConfigured, common.BytesToHash(minter.Bytes())},
		Data:        common.LeftPadBytes(allowance.Bytes(), 32),
		BlockNumber: 300,
	}

	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("error: %v", err)
	}

	if len(mock.minterConfigEvents) != 1 {
		t.Fatalf("expected 1 minter config event, got %d", len(mock.minterConfigEvents))
	}
	if mock.minterConfigEvents[0].Action != "configured" {
		t.Errorf("expected action 'configured', got '%s'", mock.minterConfigEvents[0].Action)
	}
	if !mock.activeMinters[minter] {
		t.Error("expected minter to be active")
	}
}

func TestParseMinterRemovedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	minter := common.HexToAddress("0xaaaa")
	log := &types.Log{
		Address:     constants.NativeCoinAdapterAddress,
		Topics:      []common.Hash{constants.EventSigMinterRemoved, common.BytesToHash(minter.Bytes())},
		BlockNumber: 400,
	}

	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("error: %v", err)
	}

	if len(mock.minterConfigEvents) != 1 {
		t.Fatalf("expected 1 minter config event")
	}
	if mock.minterConfigEvents[0].Action != "removed" {
		t.Errorf("expected action 'removed'")
	}
}

func TestParseMasterMinterChangedEvent(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()

	newMaster := common.HexToAddress("0xbbbb")
	log := &types.Log{
		Address:     constants.NativeCoinAdapterAddress,
		Topics:      []common.Hash{constants.EventSigMasterMinterChanged, common.BytesToHash(newMaster.Bytes())},
		BlockNumber: 500,
	}

	// Informational only, should not error
	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("error: %v", err)
	}
}

// ========== Proposal Event Tests ==========

func TestParseProposalCreatedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	proposalID := big.NewInt(1)
	proposer := common.HexToAddress("0xaaaa")
	actionType := common.LeftPadBytes([]byte("addMember"), 32)
	memberVersion := common.LeftPadBytes(big.NewInt(1).Bytes(), 32)
	requiredApprovals := common.LeftPadBytes(big.NewInt(3).Bytes(), 32)
	// callData offset (pointing to offset 128) + callData length (4) + callData
	callDataOffset := common.LeftPadBytes(big.NewInt(128).Bytes(), 32)
	callDataLength := common.LeftPadBytes(big.NewInt(4).Bytes(), 32)
	callData := common.RightPadBytes([]byte{0x01, 0x02, 0x03, 0x04}, 32)

	data := make([]byte, 0, 224)
	data = append(data, actionType...)
	data = append(data, memberVersion...)
	data = append(data, requiredApprovals...)
	data = append(data, callDataOffset...)
	data = append(data, callDataLength...)
	data = append(data, callData...)

	log := &types.Log{
		Address:     constants.GovValidatorAddress,
		Topics:      []common.Hash{constants.EventSigProposalCreated, common.BytesToHash(proposalID.Bytes()), common.BytesToHash(proposer.Bytes())},
		Data:        data,
		BlockNumber: 600,
		TxHash:      common.HexToHash("0xdef"),
	}

	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("error: %v", err)
	}

	if len(mock.proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(mock.proposals))
	}
	if mock.proposals[0].ProposalID.Cmp(proposalID) != 0 {
		t.Errorf("expected proposal ID %s", proposalID)
	}
	if mock.proposals[0].Proposer != proposer {
		t.Errorf("expected proposer %s", proposer.Hex())
	}
	if mock.proposals[0].Status != storage.ProposalStatusVoting {
		t.Errorf("expected status Voting")
	}
}

func TestParseProposalCreatedEvent_DataTooShort(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()

	log := &types.Log{
		Address: constants.GovValidatorAddress,
		Topics:  []common.Hash{constants.EventSigProposalCreated, common.BytesToHash(big.NewInt(1).Bytes()), common.BytesToHash(common.Address{}.Bytes())},
		Data:    []byte{0x01}, // Too short
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})
}

func TestParseProposalVotedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	proposalID := big.NewInt(1)
	voter := common.HexToAddress("0xaaaa")
	// approval=true (uint256), approved=2 (uint256), rejected=1 (uint256)
	data := make([]byte, 96)
	copy(data[31:32], []byte{0x01}) // approval = true
	copy(data[62:64], []byte{0x00, 0x02}) // approved = 2
	copy(data[94:96], []byte{0x00, 0x01}) // rejected = 1

	log := &types.Log{
		Address:     constants.GovValidatorAddress,
		Topics:      []common.Hash{constants.EventSigProposalVoted, common.BytesToHash(proposalID.Bytes()), common.BytesToHash(voter.Bytes())},
		Data:        data,
		BlockNumber: 700,
	}

	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("error: %v", err)
	}

	if len(mock.proposalVotes) != 1 {
		t.Fatalf("expected 1 vote, got %d", len(mock.proposalVotes))
	}
	if !mock.proposalVotes[0].Approval {
		t.Error("expected approval=true")
	}
}

func TestParseProposalApprovedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	proposalID := big.NewInt(1)
	approver := common.HexToAddress("0xaaaa")

	log := &types.Log{
		Address:     constants.GovValidatorAddress,
		Topics:      []common.Hash{constants.EventSigProposalApproved, common.BytesToHash(proposalID.Bytes()), common.BytesToHash(approver.Bytes())},
		BlockNumber: 800,
	}

	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("error: %v", err)
	}

	if len(mock.proposalStatusUpdates) != 1 {
		t.Fatalf("expected 1 status update")
	}
	if mock.proposalStatusUpdates[0].status != storage.ProposalStatusApproved {
		t.Errorf("expected Approved status")
	}
}

func TestParseProposalRejectedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	proposalID := big.NewInt(2)
	log := &types.Log{
		Address: constants.GovValidatorAddress,
		Topics:  []common.Hash{constants.EventSigProposalRejected, common.BytesToHash(proposalID.Bytes()), common.BytesToHash(common.Address{}.Bytes())},
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})
	if len(mock.proposalStatusUpdates) != 1 || mock.proposalStatusUpdates[0].status != storage.ProposalStatusRejected {
		t.Error("expected Rejected status update")
	}
}

func TestParseProposalExecutedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	proposalID := big.NewInt(3)
	log := &types.Log{
		Address:     constants.GovValidatorAddress,
		Topics:      []common.Hash{constants.EventSigProposalExecuted, common.BytesToHash(proposalID.Bytes()), common.BytesToHash(common.Address{}.Bytes())},
		BlockNumber: 900,
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})
	if len(mock.proposalStatusUpdates) != 1 || mock.proposalStatusUpdates[0].status != storage.ProposalStatusExecuted {
		t.Error("expected Executed status update")
	}
}

func TestParseProposalFailedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	proposalID := big.NewInt(4)
	log := &types.Log{
		Address: constants.GovValidatorAddress,
		Topics:  []common.Hash{constants.EventSigProposalFailed, common.BytesToHash(proposalID.Bytes()), common.BytesToHash(common.Address{}.Bytes())},
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})
	if len(mock.proposalStatusUpdates) != 1 || mock.proposalStatusUpdates[0].status != storage.ProposalStatusFailed {
		t.Error("expected Failed status update")
	}
}

func TestParseProposalExpiredEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	proposalID := big.NewInt(5)
	log := &types.Log{
		Address: constants.GovValidatorAddress,
		Topics:  []common.Hash{constants.EventSigProposalExpired, common.BytesToHash(proposalID.Bytes()), common.BytesToHash(common.Address{}.Bytes())},
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})
	if len(mock.proposalStatusUpdates) != 1 || mock.proposalStatusUpdates[0].status != storage.ProposalStatusExpired {
		t.Error("expected Expired status update")
	}
}

func TestParseProposalCancelledEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	proposalID := big.NewInt(6)
	log := &types.Log{
		Address: constants.GovValidatorAddress,
		Topics:  []common.Hash{constants.EventSigProposalCancelled, common.BytesToHash(proposalID.Bytes()), common.BytesToHash(common.Address{}.Bytes())},
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})
	if len(mock.proposalStatusUpdates) != 1 || mock.proposalStatusUpdates[0].status != storage.ProposalStatusCancelled {
		t.Error("expected Cancelled status update")
	}
}

// ========== Member Event Tests ==========

func TestParseMemberAddedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	member := common.HexToAddress("0xcccc")
	// Data: totalMembers (32) + newQuorum (32)
	data := make([]byte, 64)
	copy(data[0:32], common.LeftPadBytes(big.NewInt(5).Bytes(), 32))  // totalMembers = 5
	copy(data[32:64], common.LeftPadBytes(big.NewInt(3).Bytes(), 32)) // newQuorum = 3

	log := &types.Log{
		Address:     constants.GovValidatorAddress,
		Topics:      []common.Hash{constants.EventSigMemberAdded, common.BytesToHash(member.Bytes())},
		Data:        data,
		BlockNumber: 1000,
	}

	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("error: %v", err)
	}

	if len(mock.memberChangeEvents) != 1 {
		t.Fatalf("expected 1 member change event")
	}
	if mock.memberChangeEvents[0].Action != "added" {
		t.Errorf("expected action 'added'")
	}
	if !mock.activeValidators[member] {
		t.Error("expected validator to be active")
	}
}

func TestParseMemberRemovedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	member := common.HexToAddress("0xcccc")
	data := make([]byte, 64)
	copy(data[0:32], common.LeftPadBytes(big.NewInt(4).Bytes(), 32))
	copy(data[32:64], common.LeftPadBytes(big.NewInt(2).Bytes(), 32))

	log := &types.Log{
		Address:     constants.GovValidatorAddress,
		Topics:      []common.Hash{constants.EventSigMemberRemoved, common.BytesToHash(member.Bytes())},
		Data:        data,
		BlockNumber: 1100,
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})

	if len(mock.memberChangeEvents) != 1 {
		t.Fatalf("expected 1 member change event")
	}
	if mock.memberChangeEvents[0].Action != "removed" {
		t.Errorf("expected action 'removed'")
	}
}

func TestParseMemberChangedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	oldMember := common.HexToAddress("0xcccc")
	newMember := common.HexToAddress("0xdddd")

	log := &types.Log{
		Address:     constants.GovValidatorAddress,
		Topics:      []common.Hash{constants.EventSigMemberChanged, common.BytesToHash(oldMember.Bytes()), common.BytesToHash(newMember.Bytes())},
		BlockNumber: 1200,
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})

	if len(mock.memberChangeEvents) != 1 {
		t.Fatalf("expected 1 member change event")
	}
	if mock.memberChangeEvents[0].Action != "changed" {
		t.Errorf("expected action 'changed'")
	}
}

// ========== GasTip Event Tests ==========

func TestParseGasTipUpdatedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	updater := common.HexToAddress("0xaaaa")
	data := make([]byte, 64)
	copy(data[0:32], common.LeftPadBytes(big.NewInt(100).Bytes(), 32)) // oldTip
	copy(data[32:64], common.LeftPadBytes(big.NewInt(200).Bytes(), 32)) // newTip

	log := &types.Log{
		Address:     constants.GovValidatorAddress,
		Topics:      []common.Hash{constants.EventSigGasTipUpdated, common.BytesToHash(updater.Bytes())},
		Data:        data,
		BlockNumber: 1300,
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})

	if len(mock.gasTipEvents) != 1 {
		t.Fatalf("expected 1 gas tip event, got %d", len(mock.gasTipEvents))
	}
	if mock.gasTipEvents[0].NewTip.Cmp(big.NewInt(200)) != 0 {
		t.Errorf("expected newTip 200")
	}
}

// ========== Emergency Pause Event Tests ==========

func TestParseEmergencyPausedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	proposalID := big.NewInt(10)
	log := &types.Log{
		Address:     constants.GovMasterMinterAddress,
		Topics:      []common.Hash{constants.EventSigEmergencyPaused, common.BytesToHash(proposalID.Bytes())},
		BlockNumber: 1400,
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})

	if len(mock.emergencyPauseEvents) != 1 {
		t.Fatalf("expected 1 emergency pause event")
	}
	if mock.emergencyPauseEvents[0].Action != "paused" {
		t.Errorf("expected action 'paused', got '%s'", mock.emergencyPauseEvents[0].Action)
	}
}

func TestParseEmergencyUnpausedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	proposalID := big.NewInt(11)
	log := &types.Log{
		Address:     constants.GovMasterMinterAddress,
		Topics:      []common.Hash{constants.EventSigEmergencyUnpaused, common.BytesToHash(proposalID.Bytes())},
		BlockNumber: 1500,
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})

	if len(mock.emergencyPauseEvents) != 1 {
		t.Fatalf("expected 1 emergency pause event")
	}
	if mock.emergencyPauseEvents[0].Action != "unpaused" {
		t.Errorf("expected action 'unpaused'")
	}
}

// ========== DepositMintProposed Event Tests ==========

func TestParseDepositMintProposedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	proposalID := big.NewInt(20)
	depositIDHash := common.HexToHash("0x1234")
	requester := common.HexToAddress("0xaaaa")
	beneficiary := common.HexToAddress("0xbbbb")
	amount := big.NewInt(5000)

	// Data: beneficiary (32 padded) + amount (32) + bankRef offset (32) + bankRef length (32) + bankRef data
	data := make([]byte, 0, 192)
	data = append(data, common.LeftPadBytes(beneficiary.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(96).Bytes(), 32)...) // offset to bankRef
	data = append(data, common.LeftPadBytes(big.NewInt(7).Bytes(), 32)...)  // bankRef length
	data = append(data, common.RightPadBytes([]byte("REF-001"), 32)...)     // bankRef data

	log := &types.Log{
		Address:     constants.GovMinterAddress,
		Topics:      []common.Hash{constants.EventSigDepositMintProposed, common.BytesToHash(proposalID.Bytes()), depositIDHash, common.BytesToHash(requester.Bytes())},
		Data:        data,
		BlockNumber: 1600,
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})

	if len(mock.depositMintProposals) != 1 {
		t.Fatalf("expected 1 deposit mint proposal, got %d", len(mock.depositMintProposals))
	}
	if mock.depositMintProposals[0].Amount.Cmp(amount) != 0 {
		t.Errorf("expected amount %s", amount)
	}
	if mock.depositMintProposals[0].BankReference != "REF-001" {
		t.Errorf("expected bank reference 'REF-001', got '%s'", mock.depositMintProposals[0].BankReference)
	}
}

// ========== BurnPrepaid/BurnExecuted Event Tests ==========

func TestParseBurnPrepaidEvent(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()

	user := common.HexToAddress("0xaaaa")
	amount := big.NewInt(300)

	log := &types.Log{
		Address:     constants.GovMinterAddress,
		Topics:      []common.Hash{constants.EventSigBurnPrepaid, common.BytesToHash(user.Bytes())},
		Data:        common.LeftPadBytes(amount.Bytes(), 32),
		BlockNumber: 1700,
	}

	// Informational only, should not error
	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("error: %v", err)
	}
}

func TestParseBurnExecutedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	from := common.HexToAddress("0xaaaa")
	amount := big.NewInt(400)

	// Data: offset (32) + length (32) + string data
	data := make([]byte, 0, 128)
	data = append(data, common.LeftPadBytes(big.NewInt(32).Bytes(), 32)...) // offset
	data = append(data, common.LeftPadBytes(big.NewInt(6).Bytes(), 32)...)  // length
	data = append(data, common.RightPadBytes([]byte("WD-001"), 32)...)      // withdrawalId

	log := &types.Log{
		Address:     constants.GovMinterAddress,
		Topics:      []common.Hash{constants.EventSigBurnExecuted, common.BytesToHash(from.Bytes()), common.BytesToHash(amount.Bytes())},
		Data:        data,
		BlockNumber: 1800,
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})

	if len(mock.burnEvents) != 1 {
		t.Fatalf("expected 1 burn event, got %d", len(mock.burnEvents))
	}
	if mock.burnEvents[0].WithdrawalID != "WD-001" {
		t.Errorf("expected withdrawalId 'WD-001', got '%s'", mock.burnEvents[0].WithdrawalID)
	}
}

// ========== Blacklist Event Tests ==========

func TestParseAddressBlacklistedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	account := common.HexToAddress("0xbad")
	proposalID := big.NewInt(30)

	log := &types.Log{
		Address:     constants.GovCouncilAddress,
		Topics:      []common.Hash{constants.EventSigAddressBlacklisted, common.BytesToHash(account.Bytes()), common.BytesToHash(proposalID.Bytes())},
		BlockNumber: 1900,
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})

	if len(mock.blacklistEvents) != 1 {
		t.Fatalf("expected 1 blacklist event")
	}
	if mock.blacklistEvents[0].Action != "blacklisted" {
		t.Errorf("expected action 'blacklisted'")
	}
	if !mock.blacklistStatus[account] {
		t.Error("expected account to be blacklisted")
	}
}

func TestParseAddressUnblacklistedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	account := common.HexToAddress("0xbad")
	proposalID := big.NewInt(31)

	log := &types.Log{
		Address:     constants.GovCouncilAddress,
		Topics:      []common.Hash{constants.EventSigAddressUnblacklisted, common.BytesToHash(account.Bytes()), common.BytesToHash(proposalID.Bytes())},
		BlockNumber: 2000,
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})

	if len(mock.blacklistEvents) != 1 {
		t.Fatalf("expected 1 blacklist event")
	}
	if mock.blacklistEvents[0].Action != "unblacklisted" {
		t.Errorf("expected action 'unblacklisted'")
	}
}

// ========== AuthorizedAccount Event Tests ==========

func TestParseAuthorizedAccountAddedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	account := common.HexToAddress("0xaaaa")
	proposalID := big.NewInt(40)

	log := &types.Log{
		Address:     constants.GovCouncilAddress,
		Topics:      []common.Hash{constants.EventSigAuthorizedAccountAdded, common.BytesToHash(account.Bytes()), common.BytesToHash(proposalID.Bytes())},
		BlockNumber: 2100,
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})

	if len(mock.authorizedAccountEvents) != 1 {
		t.Fatalf("expected 1 authorized account event")
	}
	if mock.authorizedAccountEvents[0].Action != "added" {
		t.Errorf("expected action 'added'")
	}
}

func TestParseAuthorizedAccountRemovedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	account := common.HexToAddress("0xaaaa")
	proposalID := big.NewInt(41)

	log := &types.Log{
		Address:     constants.GovCouncilAddress,
		Topics:      []common.Hash{constants.EventSigAuthorizedAccountRemoved, common.BytesToHash(account.Bytes()), common.BytesToHash(proposalID.Bytes())},
		BlockNumber: 2200,
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})

	if len(mock.authorizedAccountEvents) != 1 {
		t.Fatalf("expected 1 authorized account event")
	}
	if mock.authorizedAccountEvents[0].Action != "removed" {
		t.Errorf("expected action 'removed'")
	}
}

// ========== MaxProposals/ProposalExecutionSkipped Tests ==========

func TestParseMaxProposalsPerMemberUpdatedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	data := make([]byte, 64)
	copy(data[0:32], common.LeftPadBytes(big.NewInt(5).Bytes(), 32))  // oldMax = 5
	copy(data[32:64], common.LeftPadBytes(big.NewInt(10).Bytes(), 32)) // newMax = 10

	log := &types.Log{
		Address:     constants.GovCouncilAddress,
		Topics:      []common.Hash{constants.EventSigMaxProposalsPerMemberUpdated},
		Data:        data,
		BlockNumber: 2300,
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})

	if len(mock.maxProposalsEvents) != 1 {
		t.Fatalf("expected 1 max proposals event")
	}
	if mock.maxProposalsEvents[0].OldMax != 5 {
		t.Errorf("expected oldMax=5, got %d", mock.maxProposalsEvents[0].OldMax)
	}
	if mock.maxProposalsEvents[0].NewMax != 10 {
		t.Errorf("expected newMax=10, got %d", mock.maxProposalsEvents[0].NewMax)
	}
}

func TestParseProposalExecutionSkippedEvent(t *testing.T) {
	parser, mock := newTestParser()
	ctx := context.Background()

	account := common.HexToAddress("0xaaaa")
	proposalID := big.NewInt(50)

	// Data: offset (32) + reason length (32) + reason data
	data := make([]byte, 0, 128)
	data = append(data, common.LeftPadBytes(big.NewInt(32).Bytes(), 32)...)      // offset
	data = append(data, common.LeftPadBytes(big.NewInt(14).Bytes(), 32)...)      // length
	data = append(data, common.RightPadBytes([]byte("already paused"), 32)...)   // reason

	log := &types.Log{
		Address:     constants.GovCouncilAddress,
		Topics:      []common.Hash{constants.EventSigProposalExecutionSkipped, common.BytesToHash(account.Bytes()), common.BytesToHash(proposalID.Bytes())},
		Data:        data,
		BlockNumber: 2400,
	}

	parser.ParseAndIndexLogs(ctx, []*types.Log{log})

	if len(mock.proposalExecSkippedEvents) != 1 {
		t.Fatalf("expected 1 proposal execution skipped event")
	}
	if mock.proposalExecSkippedEvents[0].Reason != "already paused" {
		t.Errorf("expected reason 'already paused', got '%s'", mock.proposalExecSkippedEvents[0].Reason)
	}
}

// ========== Informational Event Tests ==========

func TestParseQuorumUpdatedEvent(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()

	log := &types.Log{
		Address:     constants.GovValidatorAddress,
		Topics:      []common.Hash{constants.EventSigQuorumUpdated},
		Data:        make([]byte, 64), // oldQuorum + newQuorum
		BlockNumber: 2500,
	}

	// Informational only
	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("error: %v", err)
	}
}

func TestParseMaxMinterAllowanceUpdatedEvent(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()

	data := make([]byte, 64)
	copy(data[24:32], big.NewInt(1000).Bytes())
	copy(data[56:64], big.NewInt(2000).Bytes())

	log := &types.Log{
		Address:     constants.GovMasterMinterAddress,
		Topics:      []common.Hash{constants.EventSigMaxMinterAllowanceUpdated},
		Data:        data,
		BlockNumber: 2600,
	}

	// Informational only
	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("error: %v", err)
	}
}

func TestParseMaxMinterAllowanceUpdatedEvent_ShortData(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()

	log := &types.Log{
		Address:     constants.GovMasterMinterAddress,
		Topics:      []common.Hash{constants.EventSigMaxMinterAllowanceUpdated},
		Data:        []byte{0x01}, // Short data
		BlockNumber: 2700,
	}

	// Should not error
	if err := parser.ParseAndIndexLogs(ctx, []*types.Log{log}); err != nil {
		t.Fatalf("error: %v", err)
	}
}

// ========== Storage Error Tests ==========

func TestParseMintEvent_StorageError(t *testing.T) {
	parser, mock := newTestParser()
	mock.storeErr = fmt.Errorf("storage unavailable")
	ctx := context.Background()

	minter := common.HexToAddress("0xaaaa")
	to := common.HexToAddress("0xbbbb")

	log := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{constants.EventSigMint, common.BytesToHash(minter.Bytes()), common.BytesToHash(to.Bytes())},
		Data:    common.LeftPadBytes(big.NewInt(100).Bytes(), 32),
	}

	// Error is logged but ParseAndIndexLogs continues
	parser.ParseAndIndexLogs(ctx, []*types.Log{log})
}

// ========== isSystemContract Tests ==========

func TestIsSystemContract(t *testing.T) {
	tests := []struct {
		addr     common.Address
		expected bool
	}{
		{constants.NativeCoinAdapterAddress, true},
		{constants.GovValidatorAddress, true},
		{constants.GovMasterMinterAddress, true},
		{constants.GovMinterAddress, true},
		{constants.GovCouncilAddress, true},
		{common.HexToAddress("0xdead"), false},
		{common.Address{}, false},
	}

	for _, tt := range tests {
		result := isSystemContract(tt.addr)
		if result != tt.expected {
			t.Errorf("isSystemContract(%s) = %v, want %v", tt.addr.Hex(), result, tt.expected)
		}
	}
}

// ========== Topic Validation Tests ==========

func TestParseBurnEvent_InvalidTopics(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()

	log := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{constants.EventSigBurn}, // Missing indexed topic
		Data:    common.LeftPadBytes(big.NewInt(1).Bytes(), 32),
	}
	parser.ParseAndIndexLogs(ctx, []*types.Log{log})
}

func TestParseMinterConfiguredEvent_InvalidTopics(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()

	log := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{constants.EventSigMinterConfigured}, // Missing indexed topic
		Data:    common.LeftPadBytes(big.NewInt(1).Bytes(), 32),
	}
	parser.ParseAndIndexLogs(ctx, []*types.Log{log})
}

func TestParseMinterRemovedEvent_InvalidTopics(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()

	log := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{constants.EventSigMinterRemoved}, // Missing indexed topic
	}
	parser.ParseAndIndexLogs(ctx, []*types.Log{log})
}

func TestParseEmergencyPausedEvent_InvalidTopics(t *testing.T) {
	parser, _ := newTestParser()
	ctx := context.Background()

	log := &types.Log{
		Address: constants.GovMasterMinterAddress,
		Topics:  []common.Hash{constants.EventSigEmergencyPaused}, // Missing indexed topic
	}
	parser.ParseAndIndexLogs(ctx, []*types.Log{log})
}
