package events

import (
	"context"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// mockContractParser implements ContractParser for testing
type mockContractParser struct {
	address    common.Address
	name       string
	events     []string
	canParse   bool
	parseEvent *ParsedEvent
	parseErr   error
}

func (m *mockContractParser) ContractAddress() common.Address { return m.address }
func (m *mockContractParser) ContractName() string            { return m.name }
func (m *mockContractParser) SupportedEvents() []string       { return m.events }
func (m *mockContractParser) CanParse(log *types.Log) bool    { return m.canParse }
func (m *mockContractParser) Parse(ctx context.Context, log *types.Log) (*ParsedEvent, error) {
	return m.parseEvent, m.parseErr
}

// mockEventHandler implements EventHandler for testing
type mockEventHandler struct {
	eventName string
	handleErr error
	handled   []*ParsedEvent
}

func (m *mockEventHandler) EventName() string { return m.eventName }
func (m *mockEventHandler) Handle(ctx context.Context, event *ParsedEvent) error {
	m.handled = append(m.handled, event)
	return m.handleErr
}

// mockStorageHandler implements StorageHandler for testing
type mockStorageHandler struct {
	eventTypes []string
	storeErr   error
	stored     []*ParsedEvent
}

func (m *mockStorageHandler) EventTypes() []string { return m.eventTypes }
func (m *mockStorageHandler) Store(ctx context.Context, event *ParsedEvent) error {
	m.stored = append(m.stored, event)
	return m.storeErr
}

func TestNewParserRegistry(t *testing.T) {
	bus := NewEventBus(100, 100)
	reg := NewParserRegistry(bus)

	if reg == nil {
		t.Fatal("expected non-nil registry")
	}
	if len(reg.parsers) != 0 {
		t.Error("expected empty parsers map")
	}
}

func TestParserRegistry_RegisterParser(t *testing.T) {
	reg := NewParserRegistry(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	parser := &mockContractParser{
		address: addr,
		name:    "TestContract",
		events:  []string{"Transfer", "Approval"},
	}

	if err := reg.RegisterParser(parser); err != nil {
		t.Fatalf("RegisterParser error: %v", err)
	}

	// Duplicate registration should fail
	if err := reg.RegisterParser(parser); err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestParserRegistry_GetParser(t *testing.T) {
	reg := NewParserRegistry(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	parser := &mockContractParser{address: addr, name: "TestContract"}
	_ = reg.RegisterParser(parser)

	// Found
	p, ok := reg.GetParser(addr)
	if !ok {
		t.Fatal("expected parser to be found")
	}
	if p.ContractName() != "TestContract" {
		t.Errorf("expected TestContract, got %s", p.ContractName())
	}

	// Not found
	_, ok = reg.GetParser(common.HexToAddress("0x2222222222222222222222222222222222222222"))
	if ok {
		t.Error("expected parser not found")
	}
}

func TestParserRegistry_UnregisterParser(t *testing.T) {
	reg := NewParserRegistry(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	parser := &mockContractParser{address: addr, name: "TestContract"}
	_ = reg.RegisterParser(parser)

	reg.UnregisterParser(addr)

	_, ok := reg.GetParser(addr)
	if ok {
		t.Error("expected parser to be removed")
	}

	// Re-registration should work
	if err := reg.RegisterParser(parser); err != nil {
		t.Fatalf("re-registration should work: %v", err)
	}
}

func TestParserRegistry_RegisterABI(t *testing.T) {
	reg := NewParserRegistry(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	abiJSON := `[{"type":"event","name":"Transfer","inputs":[{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"value","type":"uint256"}]}]`

	contractABI, err := NewContractABI(addr, "ERC20", abiJSON)
	if err != nil {
		t.Fatalf("NewContractABI error: %v", err)
	}

	if err := reg.RegisterABI(contractABI); err != nil {
		t.Fatalf("RegisterABI error: %v", err)
	}

	// Duplicate should fail
	if err := reg.RegisterABI(contractABI); err == nil {
		t.Error("expected error for duplicate ABI registration")
	}

	// GetABI should work
	abi, ok := reg.GetABI(addr)
	if !ok {
		t.Fatal("expected ABI to be found")
	}
	if abi.Name != "ERC20" {
		t.Errorf("expected ERC20, got %s", abi.Name)
	}
}

func TestParserRegistry_RegisterABIFromJSON(t *testing.T) {
	reg := NewParserRegistry(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	abiJSON := `[{"type":"event","name":"Transfer","inputs":[{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"value","type":"uint256"}]}]`

	if err := reg.RegisterABIFromJSON(addr, "ERC20", abiJSON); err != nil {
		t.Fatalf("RegisterABIFromJSON error: %v", err)
	}

	// Invalid ABI
	if err := reg.RegisterABIFromJSON(common.Address{}, "Bad", "invalid json"); err == nil {
		t.Error("expected error for invalid ABI JSON")
	}
}

func TestParserRegistry_RegisterHandler(t *testing.T) {
	reg := NewParserRegistry(nil)

	handler := &mockEventHandler{eventName: "Transfer"}
	reg.RegisterHandler(handler)

	// Verify handler is registered
	if len(reg.handlers["Transfer"]) != 1 {
		t.Errorf("expected 1 handler, got %d", len(reg.handlers["Transfer"]))
	}

	// Register another handler for same event
	handler2 := &mockEventHandler{eventName: "Transfer"}
	reg.RegisterHandler(handler2)

	if len(reg.handlers["Transfer"]) != 2 {
		t.Errorf("expected 2 handlers, got %d", len(reg.handlers["Transfer"]))
	}
}

func TestParserRegistry_RegisterStorageHandler(t *testing.T) {
	reg := NewParserRegistry(nil)

	handler := &mockStorageHandler{eventTypes: []string{"Transfer", "Approval"}}
	reg.RegisterStorageHandler(handler)

	if len(reg.storageHandlers["Transfer"]) != 1 {
		t.Error("expected storage handler for Transfer")
	}
	if len(reg.storageHandlers["Approval"]) != 1 {
		t.Error("expected storage handler for Approval")
	}
}

func TestParserRegistry_SetDefaultHandler(t *testing.T) {
	reg := NewParserRegistry(nil)

	handler := &mockEventHandler{eventName: "*"}
	reg.SetDefaultHandler(handler)

	if reg.defaultHandler == nil {
		t.Error("expected default handler to be set")
	}
}

func TestParserRegistry_HandleEvent(t *testing.T) {
	reg := NewParserRegistry(nil)
	ctx := context.Background()

	handler := &mockEventHandler{eventName: "Transfer"}
	reg.RegisterHandler(handler)

	event := &ParsedEvent{EventName: "Transfer", BlockNumber: 100}
	if err := reg.HandleEvent(ctx, event); err != nil {
		t.Fatalf("HandleEvent error: %v", err)
	}

	if len(handler.handled) != 1 {
		t.Errorf("expected 1 handled event, got %d", len(handler.handled))
	}
}

func TestParserRegistry_HandleEvent_DefaultHandler(t *testing.T) {
	reg := NewParserRegistry(nil)
	ctx := context.Background()

	defaultHandler := &mockEventHandler{eventName: "*"}
	reg.SetDefaultHandler(defaultHandler)

	// No specific handler for "Unknown" event, should use default
	event := &ParsedEvent{EventName: "Unknown", BlockNumber: 100}
	if err := reg.HandleEvent(ctx, event); err != nil {
		t.Fatalf("HandleEvent error: %v", err)
	}

	if len(defaultHandler.handled) != 1 {
		t.Errorf("expected default handler to process event")
	}
}

func TestParserRegistry_HandleEvent_Error(t *testing.T) {
	reg := NewParserRegistry(nil)
	ctx := context.Background()

	handler := &mockEventHandler{eventName: "Transfer", handleErr: fmt.Errorf("handler failed")}
	reg.RegisterHandler(handler)

	event := &ParsedEvent{EventName: "Transfer"}
	err := reg.HandleEvent(ctx, event)
	if err == nil {
		t.Error("expected error from handler")
	}
}

func TestParserRegistry_StoreEvent(t *testing.T) {
	reg := NewParserRegistry(nil)
	ctx := context.Background()

	handler := &mockStorageHandler{eventTypes: []string{"Transfer"}}
	reg.RegisterStorageHandler(handler)

	event := &ParsedEvent{EventName: "Transfer", BlockNumber: 100}
	if err := reg.StoreEvent(ctx, event); err != nil {
		t.Fatalf("StoreEvent error: %v", err)
	}

	if len(handler.stored) != 1 {
		t.Errorf("expected 1 stored event, got %d", len(handler.stored))
	}
}

func TestParserRegistry_StoreEvent_Error(t *testing.T) {
	reg := NewParserRegistry(nil)
	ctx := context.Background()

	handler := &mockStorageHandler{
		eventTypes: []string{"Transfer"},
		storeErr:   fmt.Errorf("storage failed"),
	}
	reg.RegisterStorageHandler(handler)

	event := &ParsedEvent{EventName: "Transfer"}
	if err := reg.StoreEvent(ctx, event); err == nil {
		t.Error("expected storage error")
	}
}

func TestParserRegistry_ParseLog_CustomParser(t *testing.T) {
	reg := NewParserRegistry(nil)
	ctx := context.Background()
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	expected := &ParsedEvent{EventName: "Transfer", BlockNumber: 42}
	parser := &mockContractParser{
		address:    addr,
		canParse:   true,
		parseEvent: expected,
	}
	_ = reg.RegisterParser(parser)

	log := &types.Log{
		Address: addr,
		Topics:  []common.Hash{common.HexToHash("0xddf252ad")},
	}

	result, err := reg.ParseLog(ctx, log)
	if err != nil {
		t.Fatalf("ParseLog error: %v", err)
	}
	if result.EventName != "Transfer" {
		t.Errorf("expected Transfer, got %s", result.EventName)
	}
}

func TestParserRegistry_ParseLog_NoParser(t *testing.T) {
	reg := NewParserRegistry(nil)
	ctx := context.Background()

	log := &types.Log{
		Address: common.HexToAddress("0xdead"),
		Topics:  []common.Hash{common.HexToHash("0xbeef")},
	}

	_, err := reg.ParseLog(ctx, log)
	if err == nil {
		t.Error("expected error for unregistered address")
	}
}

func TestParserRegistry_ListRegisteredContracts(t *testing.T) {
	reg := NewParserRegistry(nil)
	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")

	// Register via custom parser
	_ = reg.RegisterParser(&mockContractParser{address: addr1, name: "Contract1"})

	// Register via ABI
	abiJSON := `[{"type":"event","name":"Transfer","inputs":[]}]`
	_ = reg.RegisterABIFromJSON(addr2, "Contract2", abiJSON)

	contracts := reg.ListRegisteredContracts()
	if len(contracts) != 2 {
		t.Errorf("expected 2 contracts, got %d", len(contracts))
	}
}

func TestParserRegistry_GetContractInfo(t *testing.T) {
	reg := NewParserRegistry(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	parser := &mockContractParser{
		address: addr,
		name:    "TestContract",
		events:  []string{"Transfer", "Approval"},
	}
	_ = reg.RegisterParser(parser)

	info := reg.GetContractInfo(addr)
	if info.Name != "TestContract" {
		t.Errorf("expected TestContract, got %s", info.Name)
	}
	if !info.HasCustomParser {
		t.Error("expected HasCustomParser=true")
	}
	if len(info.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(info.Events))
	}
}

func TestParserRegistry_GetContractInfo_WithABI(t *testing.T) {
	reg := NewParserRegistry(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	abiJSON := `[{"type":"event","name":"Transfer","inputs":[{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"value","type":"uint256"}]}]`
	_ = reg.RegisterABIFromJSON(addr, "ERC20Token", abiJSON)

	info := reg.GetContractInfo(addr)
	if info.Name != "ERC20Token" {
		t.Errorf("expected ERC20Token, got %s", info.Name)
	}
	if !info.HasABI {
		t.Error("expected HasABI=true")
	}
}

func TestParserRegistry_GetContractInfo_Unknown(t *testing.T) {
	reg := NewParserRegistry(nil)
	addr := common.HexToAddress("0xdead")

	info := reg.GetContractInfo(addr)
	if info.HasCustomParser || info.HasABI {
		t.Error("expected no parser info for unknown address")
	}
}
