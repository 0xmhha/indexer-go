package events

import (
	"context"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
)

// ========== EventTransformer Tests ==========

func TestNewEventTransformer(t *testing.T) {
	tr := NewEventTransformer()
	if tr == nil {
		t.Fatal("expected non-nil transformer")
	}
}

func TestEventTransformer_ToMintEvent(t *testing.T) {
	tr := NewEventTransformer()

	minter := common.HexToAddress("0x1111111111111111111111111111111111111111")
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	amount := big.NewInt(1000)

	event := &ParsedEvent{
		EventName:   "Mint",
		BlockNumber: 100,
		TxHash:      common.HexToHash("0xabc"),
		Data: map[string]interface{}{
			"minter": minter,
			"to":     to,
			"amount": amount,
		},
	}

	mint, err := tr.ToMintEvent(event)
	if err != nil {
		t.Fatalf("ToMintEvent error: %v", err)
	}

	if mint.Minter != minter {
		t.Errorf("expected minter %s, got %s", minter.Hex(), mint.Minter.Hex())
	}
	if mint.To != to {
		t.Errorf("expected to %s, got %s", to.Hex(), mint.To.Hex())
	}
	if mint.Amount.Cmp(amount) != 0 {
		t.Errorf("expected amount %s, got %s", amount, mint.Amount)
	}
	if mint.BlockNumber != 100 {
		t.Errorf("expected block 100, got %d", mint.BlockNumber)
	}
}

func TestEventTransformer_ToMintEvent_MissingFields(t *testing.T) {
	tr := NewEventTransformer()

	tests := []struct {
		name string
		data map[string]interface{}
	}{
		{"missing minter", map[string]interface{}{"to": common.Address{}, "amount": big.NewInt(0)}},
		{"missing to", map[string]interface{}{"minter": common.Address{}, "amount": big.NewInt(0)}},
		{"missing amount", map[string]interface{}{"minter": common.Address{}, "to": common.Address{}}},
		{"wrong minter type", map[string]interface{}{"minter": "bad", "to": common.Address{}, "amount": big.NewInt(0)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := &ParsedEvent{EventName: "Mint", Data: tc.data}
			_, err := tr.ToMintEvent(event)
			if err == nil {
				t.Error("expected error for missing/invalid field")
			}
		})
	}
}

func TestEventTransformer_ToBurnEvent(t *testing.T) {
	tr := NewEventTransformer()

	burner := common.HexToAddress("0x1111111111111111111111111111111111111111")
	amount := big.NewInt(500)

	event := &ParsedEvent{
		EventName:   "Burn",
		BlockNumber: 200,
		TxHash:      common.HexToHash("0xdef"),
		Data: map[string]interface{}{
			"burner":       burner,
			"amount":       amount,
			"withdrawalId": "WD-001",
		},
	}

	burn, err := tr.ToBurnEvent(event)
	if err != nil {
		t.Fatalf("ToBurnEvent error: %v", err)
	}

	if burn.Burner != burner {
		t.Errorf("expected burner %s, got %s", burner.Hex(), burn.Burner.Hex())
	}
	if burn.Amount.Cmp(amount) != 0 {
		t.Errorf("expected amount %s, got %s", amount, burn.Amount)
	}
	if burn.WithdrawalID != "WD-001" {
		t.Errorf("expected WD-001, got %s", burn.WithdrawalID)
	}
}

func TestEventTransformer_ToBurnEvent_MissingFields(t *testing.T) {
	tr := NewEventTransformer()

	tests := []struct {
		name string
		data map[string]interface{}
	}{
		{"missing burner", map[string]interface{}{"amount": big.NewInt(0)}},
		{"missing amount", map[string]interface{}{"burner": common.Address{}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := &ParsedEvent{EventName: "Burn", Data: tc.data}
			_, err := tr.ToBurnEvent(event)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestEventTransformer_ToProposal(t *testing.T) {
	tr := NewEventTransformer()

	proposer := common.HexToAddress("0x1111111111111111111111111111111111111111")
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	proposalID := big.NewInt(42)

	event := &ParsedEvent{
		EventName:       "ProposalCreated",
		ContractAddress: contract,
		BlockNumber:     300,
		TxHash:          common.HexToHash("0x123"),
		Data: map[string]interface{}{
			"proposalId":        proposalID,
			"proposer":          proposer,
			"requiredApprovals": uint64(3),
			"createdAt":         uint64(1700000000),
		},
	}

	proposal, err := tr.ToProposal(event)
	if err != nil {
		t.Fatalf("ToProposal error: %v", err)
	}

	if proposal.ProposalID.Cmp(proposalID) != 0 {
		t.Errorf("expected proposalID %s, got %s", proposalID, proposal.ProposalID)
	}
	if proposal.Proposer != proposer {
		t.Errorf("expected proposer %s, got %s", proposer.Hex(), proposal.Proposer.Hex())
	}
	if proposal.Status != storage.ProposalStatusVoting {
		t.Errorf("expected Voting status, got %s", proposal.Status)
	}
	if proposal.RequiredApprovals != 3 {
		t.Errorf("expected 3 required approvals, got %d", proposal.RequiredApprovals)
	}
}

func TestEventTransformer_ToProposal_MissingFields(t *testing.T) {
	tr := NewEventTransformer()

	tests := []struct {
		name string
		data map[string]interface{}
	}{
		{"missing proposalId", map[string]interface{}{"proposer": common.Address{}}},
		{"missing proposer", map[string]interface{}{"proposalId": big.NewInt(1)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := &ParsedEvent{EventName: "ProposalCreated", Data: tc.data}
			_, err := tr.ToProposal(event)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestEventTransformer_ToDepositMintProposal(t *testing.T) {
	tr := NewEventTransformer()

	requester := common.HexToAddress("0x1111111111111111111111111111111111111111")
	beneficiary := common.HexToAddress("0x2222222222222222222222222222222222222222")

	event := &ParsedEvent{
		EventName:   "DepositMintProposed",
		BlockNumber: 400,
		TxHash:      common.HexToHash("0x456"),
		Data: map[string]interface{}{
			"proposalId":    big.NewInt(10),
			"requester":     requester,
			"beneficiary":   beneficiary,
			"amount":        big.NewInt(1000),
			"depositId":     "DEP-001",
			"bankReference": "BANK-REF-001",
		},
	}

	proposal, err := tr.ToDepositMintProposal(event)
	if err != nil {
		t.Fatalf("ToDepositMintProposal error: %v", err)
	}

	if proposal.Requester != requester {
		t.Errorf("expected requester %s, got %s", requester.Hex(), proposal.Requester.Hex())
	}
	if proposal.DepositID != "DEP-001" {
		t.Errorf("expected DEP-001, got %s", proposal.DepositID)
	}
}

func TestEventTransformer_ToDepositMintProposal_MissingProposalId(t *testing.T) {
	tr := NewEventTransformer()
	event := &ParsedEvent{
		EventName: "DepositMintProposed",
		Data:      map[string]interface{}{},
	}
	_, err := tr.ToDepositMintProposal(event)
	if err == nil {
		t.Error("expected error for missing proposalId")
	}
}

func TestEventTransformer_ToProposalVote(t *testing.T) {
	tr := NewEventTransformer()

	voter := common.HexToAddress("0x1111111111111111111111111111111111111111")
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")

	event := &ParsedEvent{
		EventName:       "VoteCast",
		ContractAddress: contract,
		BlockNumber:     500,
		TxHash:          common.HexToHash("0x789"),
		Data: map[string]interface{}{
			"proposalId": big.NewInt(42),
			"voter":      voter,
			"approval":   true,
		},
	}

	vote, err := tr.ToProposalVote(event)
	if err != nil {
		t.Fatalf("ToProposalVote error: %v", err)
	}

	if vote.Voter != voter {
		t.Errorf("expected voter %s, got %s", voter.Hex(), vote.Voter.Hex())
	}
	if !vote.Approval {
		t.Error("expected approval=true")
	}
	if vote.Contract != contract {
		t.Errorf("expected contract %s, got %s", contract.Hex(), vote.Contract.Hex())
	}
}

func TestEventTransformer_ToProposalVote_MissingFields(t *testing.T) {
	tr := NewEventTransformer()

	tests := []struct {
		name string
		data map[string]interface{}
	}{
		{"missing proposalId", map[string]interface{}{"voter": common.Address{}}},
		{"missing voter", map[string]interface{}{"proposalId": big.NewInt(1)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := &ParsedEvent{EventName: "VoteCast", Data: tc.data}
			_, err := tr.ToProposalVote(event)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

// ========== Handler EventName Tests ==========

func TestLoggingHandler_EventName(t *testing.T) {
	handler := NewLoggingHandler(nil)
	if handler.EventName() != "*" {
		t.Errorf("expected *, got %s", handler.EventName())
	}
}

func TestEventBusPublisher_EventName(t *testing.T) {
	publisher := NewEventBusPublisher(nil)
	if publisher.EventName() != "*" {
		t.Errorf("expected *, got %s", publisher.EventName())
	}
}

func TestEventBusPublisher_Handle_NilBus(t *testing.T) {
	publisher := NewEventBusPublisher(nil)
	ctx := context.Background()
	event := &ParsedEvent{EventName: "Transfer", Data: make(map[string]interface{})}

	if err := publisher.Handle(ctx, event); err != nil {
		t.Fatalf("expected no error with nil bus: %v", err)
	}
}

// ========== Concrete Handler Tests ==========

// mockMintStorage implements the storage interface for MintEventHandler
type mockMintStorage struct {
	stored []*storage.MintEvent
	err    error
}

func (m *mockMintStorage) StoreMintEvent(ctx context.Context, event *storage.MintEvent) error {
	m.stored = append(m.stored, event)
	return m.err
}

func TestMintEventHandler(t *testing.T) {
	store := &mockMintStorage{}
	handler := NewMintEventHandler(store)

	if handler.EventName() != "Mint" {
		t.Errorf("expected Mint, got %s", handler.EventName())
	}

	event := &ParsedEvent{
		EventName:   "Mint",
		BlockNumber: 100,
		TxHash:      common.HexToHash("0xabc"),
		Data: map[string]interface{}{
			"minter": common.HexToAddress("0x1"),
			"to":     common.HexToAddress("0x2"),
			"amount": big.NewInt(1000),
		},
	}

	ctx := context.Background()
	if err := handler.Handle(ctx, event); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	if len(store.stored) != 1 {
		t.Errorf("expected 1 stored event, got %d", len(store.stored))
	}
}

func TestMintEventHandler_InvalidEvent(t *testing.T) {
	store := &mockMintStorage{}
	handler := NewMintEventHandler(store)

	event := &ParsedEvent{
		EventName: "Mint",
		Data:      map[string]interface{}{},
	}

	ctx := context.Background()
	if err := handler.Handle(ctx, event); err == nil {
		t.Error("expected error for invalid mint event data")
	}
}

// mockBurnStorage implements the storage interface for BurnEventHandler
type mockBurnStorage struct {
	stored []*storage.BurnEvent
	err    error
}

func (m *mockBurnStorage) StoreBurnEvent(ctx context.Context, event *storage.BurnEvent) error {
	m.stored = append(m.stored, event)
	return m.err
}

func TestBurnEventHandler(t *testing.T) {
	store := &mockBurnStorage{}
	handler := NewBurnEventHandler(store)

	if handler.EventName() != "Burn" {
		t.Errorf("expected Burn, got %s", handler.EventName())
	}

	event := &ParsedEvent{
		EventName:   "Burn",
		BlockNumber: 200,
		TxHash:      common.HexToHash("0xdef"),
		Data: map[string]interface{}{
			"burner": common.HexToAddress("0x1"),
			"amount": big.NewInt(500),
		},
	}

	ctx := context.Background()
	if err := handler.Handle(ctx, event); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(store.stored) != 1 {
		t.Errorf("expected 1 stored event, got %d", len(store.stored))
	}
}

// mockProposalStorage for ProposalCreatedHandler
type mockProposalStorage struct {
	stored []*storage.Proposal
	err    error
}

func (m *mockProposalStorage) StoreProposal(ctx context.Context, proposal *storage.Proposal) error {
	m.stored = append(m.stored, proposal)
	return m.err
}

func TestProposalCreatedHandler(t *testing.T) {
	store := &mockProposalStorage{}
	handler := NewProposalCreatedHandler(store)

	if handler.EventName() != "ProposalCreated" {
		t.Errorf("expected ProposalCreated, got %s", handler.EventName())
	}

	event := &ParsedEvent{
		EventName:   "ProposalCreated",
		BlockNumber: 300,
		TxHash:      common.HexToHash("0x123"),
		Data: map[string]interface{}{
			"proposalId": big.NewInt(1),
			"proposer":   common.HexToAddress("0x1"),
		},
	}

	ctx := context.Background()
	if err := handler.Handle(ctx, event); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(store.stored) != 1 {
		t.Errorf("expected 1 stored proposal, got %d", len(store.stored))
	}
}

// mockDepositStorage for DepositMintProposedHandler
type mockDepositStorage struct {
	stored []*storage.DepositMintProposal
	err    error
}

func (m *mockDepositStorage) StoreDepositMintProposal(ctx context.Context, proposal *storage.DepositMintProposal) error {
	m.stored = append(m.stored, proposal)
	return m.err
}

func TestDepositMintProposedHandler(t *testing.T) {
	store := &mockDepositStorage{}
	handler := NewDepositMintProposedHandler(store)

	if handler.EventName() != "DepositMintProposed" {
		t.Errorf("expected DepositMintProposed, got %s", handler.EventName())
	}

	event := &ParsedEvent{
		EventName:   "DepositMintProposed",
		BlockNumber: 400,
		TxHash:      common.HexToHash("0x456"),
		Data: map[string]interface{}{
			"proposalId": big.NewInt(10),
		},
	}

	ctx := context.Background()
	if err := handler.Handle(ctx, event); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(store.stored) != 1 {
		t.Errorf("expected 1 stored deposit, got %d", len(store.stored))
	}
}

// mockVoteStorage for VoteCastHandler
type mockVoteStorage struct {
	stored []*storage.ProposalVote
	err    error
}

func (m *mockVoteStorage) StoreProposalVote(ctx context.Context, vote *storage.ProposalVote) error {
	m.stored = append(m.stored, vote)
	return m.err
}

func TestVoteCastHandler(t *testing.T) {
	store := &mockVoteStorage{}
	handler := NewVoteCastHandler(store)

	if handler.EventName() != "VoteCast" {
		t.Errorf("expected VoteCast, got %s", handler.EventName())
	}

	event := &ParsedEvent{
		EventName:   "VoteCast",
		BlockNumber: 500,
		TxHash:      common.HexToHash("0x789"),
		Data: map[string]interface{}{
			"proposalId": big.NewInt(42),
			"voter":      common.HexToAddress("0x1"),
			"approval":   true,
		},
	}

	ctx := context.Background()
	if err := handler.Handle(ctx, event); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(store.stored) != 1 {
		t.Errorf("expected 1 stored vote, got %d", len(store.stored))
	}
}
