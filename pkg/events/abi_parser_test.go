package events

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const testABIJSON = `[
	{"type":"event","name":"Transfer","inputs":[
		{"indexed":true,"name":"from","type":"address"},
		{"indexed":true,"name":"to","type":"address"},
		{"indexed":false,"name":"value","type":"uint256"}
	]},
	{"type":"event","name":"Approval","inputs":[
		{"indexed":true,"name":"owner","type":"address"},
		{"indexed":true,"name":"spender","type":"address"},
		{"indexed":false,"name":"value","type":"uint256"}
	]}
]`

func TestNewABIParser(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	abi, _ := NewContractABI(addr, "ERC20", testABIJSON)
	parser := NewABIParser(abi, nil)

	if parser == nil {
		t.Fatal("expected non-nil parser")
	}
	if parser.ContractAddress() != addr {
		t.Errorf("expected address %s, got %s", addr.Hex(), parser.ContractAddress().Hex())
	}
	if parser.ContractName() != "ERC20" {
		t.Errorf("expected ERC20, got %s", parser.ContractName())
	}
}

func TestABIParser_SupportedEvents(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	abi, _ := NewContractABI(addr, "ERC20", testABIJSON)
	parser := NewABIParser(abi, nil)

	events := parser.SupportedEvents()
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
}

func TestABIParser_CanParse(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	abi, _ := NewContractABI(addr, "ERC20", testABIJSON)
	parser := NewABIParser(abi, nil)

	transferSig := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

	// Matching address and topic
	log := &types.Log{Address: addr, Topics: []common.Hash{transferSig}}
	if !parser.CanParse(log) {
		t.Error("expected CanParse=true for matching log")
	}

	// Wrong address
	log2 := &types.Log{Address: common.HexToAddress("0x9999"), Topics: []common.Hash{transferSig}}
	if parser.CanParse(log2) {
		t.Error("expected CanParse=false for wrong address")
	}

	// No topics
	log3 := &types.Log{Address: addr, Topics: []common.Hash{}}
	if parser.CanParse(log3) {
		t.Error("expected CanParse=false for no topics")
	}

	// Unknown topic
	log4 := &types.Log{Address: addr, Topics: []common.Hash{common.HexToHash("0xdead")}}
	if parser.CanParse(log4) {
		t.Error("expected CanParse=false for unknown topic")
	}
}

func TestABIParser_Parse(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	abi, _ := NewContractABI(addr, "ERC20", testABIJSON)
	parser := NewABIParser(abi, nil)

	from := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	to := common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	transferSig := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	paddedValue := common.LeftPadBytes([]byte{0x03, 0xe8}, 32) // 1000

	log := &types.Log{
		Address:     addr,
		Topics:      []common.Hash{transferSig, common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())},
		Data:        paddedValue,
		BlockNumber: 100,
		TxHash:      common.HexToHash("0xabc"),
		Index:       5,
	}

	ctx := context.Background()
	parsed, err := parser.Parse(ctx, log)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if parsed.EventName != "Transfer" {
		t.Errorf("expected Transfer, got %s", parsed.EventName)
	}
}

// ========== ABIParserFactory Tests ==========

func TestNewABIParserFactory(t *testing.T) {
	factory := NewABIParserFactory(nil)
	if factory == nil {
		t.Fatal("expected non-nil factory")
	}
}

func TestABIParserFactory_CreateFromJSON(t *testing.T) {
	factory := NewABIParserFactory(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	parser, err := factory.CreateFromJSON(addr, "ERC20", testABIJSON)
	if err != nil {
		t.Fatalf("CreateFromJSON error: %v", err)
	}
	if parser.ContractName() != "ERC20" {
		t.Errorf("expected ERC20, got %s", parser.ContractName())
	}
}

func TestABIParserFactory_CreateFromJSON_InvalidABI(t *testing.T) {
	factory := NewABIParserFactory(nil)
	_, err := factory.CreateFromJSON(common.Address{}, "Bad", "invalid json")
	if err == nil {
		t.Error("expected error for invalid ABI JSON")
	}
}

func TestABIParserFactory_CreateFromABI(t *testing.T) {
	factory := NewABIParserFactory(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	abi, _ := NewContractABI(addr, "ERC20", testABIJSON)

	parser := factory.CreateFromABI(abi)
	if parser == nil {
		t.Fatal("expected non-nil parser")
	}
	if parser.ContractAddress() != addr {
		t.Errorf("expected address %s", addr.Hex())
	}
}

// ========== DynamicEventParser Tests ==========

func TestNewDynamicEventParser(t *testing.T) {
	bus := NewEventBus(100, 100)
	dep := NewDynamicEventParser(bus)
	if dep == nil {
		t.Fatal("expected non-nil DynamicEventParser")
	}
}

func TestDynamicEventParser_RegisterContractABI(t *testing.T) {
	dep := NewDynamicEventParser(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	if err := dep.RegisterContractABI(addr, "ERC20", testABIJSON); err != nil {
		t.Fatalf("RegisterContractABI error: %v", err)
	}

	// Should be registered
	if !dep.IsContractRegistered(addr) {
		t.Error("expected contract to be registered")
	}

	// Unknown address should not be registered
	if dep.IsContractRegistered(common.HexToAddress("0xdead")) {
		t.Error("expected unknown address not registered")
	}
}

func TestDynamicEventParser_RegisterContractABI_InvalidJSON(t *testing.T) {
	dep := NewDynamicEventParser(nil)
	err := dep.RegisterContractABI(common.Address{}, "Bad", "invalid")
	if err == nil {
		t.Error("expected error for invalid ABI")
	}
}

func TestDynamicEventParser_RegisterCustomParser(t *testing.T) {
	dep := NewDynamicEventParser(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	parser := &mockContractParser{address: addr, name: "Custom"}
	if err := dep.RegisterCustomParser(parser); err != nil {
		t.Fatalf("RegisterCustomParser error: %v", err)
	}
	if !dep.IsContractRegistered(addr) {
		t.Error("expected custom parser to be registered")
	}
}

func TestDynamicEventParser_RegisterHandler(t *testing.T) {
	dep := NewDynamicEventParser(nil)
	handler := &mockEventHandler{eventName: "Transfer"}
	dep.RegisterHandler(handler)
	// No error expected, just verify it doesn't panic
}

func TestDynamicEventParser_RegisterStorageHandler(t *testing.T) {
	dep := NewDynamicEventParser(nil)
	handler := &mockStorageHandler{eventTypes: []string{"Transfer"}}
	dep.RegisterStorageHandler(handler)
	// No error expected
}

func TestDynamicEventParser_UnregisterContract(t *testing.T) {
	dep := NewDynamicEventParser(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	_ = dep.RegisterContractABI(addr, "ERC20", testABIJSON)
	dep.UnregisterContract(addr)

	if dep.IsContractRegistered(addr) {
		t.Error("expected contract to be unregistered")
	}
}

func TestDynamicEventParser_ParseLog(t *testing.T) {
	dep := NewDynamicEventParser(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_ = dep.RegisterContractABI(addr, "ERC20", testABIJSON)

	from := common.HexToAddress("0xaaaa")
	to := common.HexToAddress("0xbbbb")
	transferSig := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	paddedValue := common.LeftPadBytes([]byte{0x01}, 32)

	log := &types.Log{
		Address: addr,
		Topics:  []common.Hash{transferSig, common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())},
		Data:    paddedValue,
	}

	ctx := context.Background()
	parsed, err := dep.ParseLog(ctx, log)
	if err != nil {
		t.Fatalf("ParseLog error: %v", err)
	}
	if parsed.EventName != "Transfer" {
		t.Errorf("expected Transfer, got %s", parsed.EventName)
	}
}

func TestDynamicEventParser_ParseLog_UnknownAddress(t *testing.T) {
	dep := NewDynamicEventParser(nil)
	ctx := context.Background()

	log := &types.Log{
		Address: common.HexToAddress("0xdead"),
		Topics:  []common.Hash{common.HexToHash("0xbeef")},
	}

	_, err := dep.ParseLog(ctx, log)
	if err == nil {
		t.Error("expected error for unregistered address")
	}
}

func TestDynamicEventParser_ListContracts(t *testing.T) {
	dep := NewDynamicEventParser(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_ = dep.RegisterContractABI(addr, "ERC20", testABIJSON)

	contracts := dep.ListContracts()
	if len(contracts) != 1 {
		t.Errorf("expected 1 contract, got %d", len(contracts))
	}
}

func TestDynamicEventParser_GetContractInfo(t *testing.T) {
	dep := NewDynamicEventParser(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_ = dep.RegisterContractABI(addr, "ERC20", testABIJSON)

	info := dep.GetContractInfo(addr)
	if info.Name != "ERC20" {
		t.Errorf("expected ERC20, got %s", info.Name)
	}
}

func TestDynamicEventParser_ProcessLog(t *testing.T) {
	dep := NewDynamicEventParser(nil)
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	parser := &mockContractParser{
		address:    addr,
		canParse:   true,
		parseEvent: &ParsedEvent{EventName: "Transfer", BlockNumber: 42},
	}
	_ = dep.RegisterCustomParser(parser)

	ctx := context.Background()
	log := &types.Log{
		Address: addr,
		Topics:  []common.Hash{common.HexToHash("0xddf252ad")},
	}

	result, err := dep.ProcessLog(ctx, log)
	if err != nil {
		t.Fatalf("ProcessLog error: %v", err)
	}
	if result.EventName != "Transfer" {
		t.Errorf("expected Transfer, got %s", result.EventName)
	}
}
