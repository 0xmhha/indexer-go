package events

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const erc20ABI = `[
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

func TestNewContractABI(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	abi, err := NewContractABI(addr, "ERC20", erc20ABI)
	if err != nil {
		t.Fatalf("NewContractABI error: %v", err)
	}

	if abi.Address != addr {
		t.Errorf("expected address %s, got %s", addr.Hex(), abi.Address.Hex())
	}
	if abi.Name != "ERC20" {
		t.Errorf("expected name ERC20, got %s", abi.Name)
	}
	if len(abi.EventSigs) != 2 {
		t.Errorf("expected 2 event sigs, got %d", len(abi.EventSigs))
	}
}

func TestNewContractABI_InvalidJSON(t *testing.T) {
	addr := common.HexToAddress("0x1")
	_, err := NewContractABI(addr, "Bad", "invalid json")
	if err == nil {
		t.Error("expected error for invalid ABI JSON")
	}
}

func TestContractABI_GetEventName(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	abi, _ := NewContractABI(addr, "ERC20", erc20ABI)

	// Transfer event signature: keccak256("Transfer(address,address,uint256)")
	transferSig := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	name, ok := abi.GetEventName(transferSig)
	if !ok {
		t.Fatal("expected Transfer event to be found")
	}
	if name != "Transfer" {
		t.Errorf("expected Transfer, got %s", name)
	}

	// Unknown signature
	_, ok = abi.GetEventName(common.HexToHash("0xdeadbeef"))
	if ok {
		t.Error("expected unknown signature not found")
	}
}

func TestContractABI_GetEvent(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	abi, _ := NewContractABI(addr, "ERC20", erc20ABI)

	event, ok := abi.GetEvent("Transfer")
	if !ok {
		t.Fatal("expected Transfer event")
	}
	if event.Name != "Transfer" {
		t.Errorf("expected Transfer, got %s", event.Name)
	}

	_, ok = abi.GetEvent("NonExistent")
	if ok {
		t.Error("expected event not found")
	}
}

func TestNewABILogParser(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	contractABI, _ := NewContractABI(addr, "ERC20", erc20ABI)
	parser := NewABILogParser(contractABI)

	if parser == nil {
		t.Fatal("expected non-nil parser")
	}
}

func TestABILogParser_CanParse(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	contractABI, _ := NewContractABI(addr, "ERC20", erc20ABI)
	parser := NewABILogParser(contractABI)

	transferSig := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

	// Matching address and topic
	log := &types.Log{
		Address: addr,
		Topics:  []common.Hash{transferSig},
	}
	if !parser.CanParse(log) {
		t.Error("expected parser to handle this log")
	}

	// Wrong address
	log2 := &types.Log{
		Address: common.HexToAddress("0x9999"),
		Topics:  []common.Hash{transferSig},
	}
	if parser.CanParse(log2) {
		t.Error("expected parser to reject wrong address")
	}

	// No topics
	log3 := &types.Log{
		Address: addr,
		Topics:  []common.Hash{},
	}
	if parser.CanParse(log3) {
		t.Error("expected parser to reject log with no topics")
	}

	// Unknown topic
	log4 := &types.Log{
		Address: addr,
		Topics:  []common.Hash{common.HexToHash("0xdeadbeef")},
	}
	if parser.CanParse(log4) {
		t.Error("expected parser to reject unknown topic")
	}
}

func TestABILogParser_Parse_Transfer(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	contractABI, _ := NewContractABI(addr, "ERC20", erc20ABI)
	parser := NewABILogParser(contractABI)

	from := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	to := common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	transferSig := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

	// Encode value as ABI uint256
	value := big.NewInt(1000)
	paddedValue := common.LeftPadBytes(value.Bytes(), 32)

	log := &types.Log{
		Address:     addr,
		Topics:      []common.Hash{transferSig, common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())},
		Data:        paddedValue,
		BlockNumber: 100,
		TxHash:      common.HexToHash("0xabc"),
		Index:       5,
	}

	parsed, err := parser.Parse(log)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if parsed.EventName != "Transfer" {
		t.Errorf("expected Transfer, got %s", parsed.EventName)
	}
	if parsed.ContractName != "ERC20" {
		t.Errorf("expected ERC20, got %s", parsed.ContractName)
	}
	if parsed.BlockNumber != 100 {
		t.Errorf("expected block 100, got %d", parsed.BlockNumber)
	}
	if parsed.LogIndex != 5 {
		t.Errorf("expected log index 5, got %d", parsed.LogIndex)
	}

	// Check parsed data
	if parsedFrom, ok := parsed.Data["from"].(common.Address); ok {
		if parsedFrom != from {
			t.Errorf("expected from %s, got %s", from.Hex(), parsedFrom.Hex())
		}
	} else {
		t.Error("expected from field to be common.Address")
	}

	if parsedTo, ok := parsed.Data["to"].(common.Address); ok {
		if parsedTo != to {
			t.Errorf("expected to %s, got %s", to.Hex(), parsedTo.Hex())
		}
	} else {
		t.Error("expected to field to be common.Address")
	}

	if parsedValue, ok := parsed.Data["value"].(*big.Int); ok {
		if parsedValue.Cmp(value) != 0 {
			t.Errorf("expected value %s, got %s", value, parsedValue)
		}
	} else {
		t.Error("expected value field to be *big.Int")
	}
}

func TestABILogParser_Parse_NoTopics(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	contractABI, _ := NewContractABI(addr, "ERC20", erc20ABI)
	parser := NewABILogParser(contractABI)

	log := &types.Log{Address: addr, Topics: []common.Hash{}}
	_, err := parser.Parse(log)
	if err == nil {
		t.Error("expected error for log with no topics")
	}
}

func TestABILogParser_Parse_UnknownEvent(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	contractABI, _ := NewContractABI(addr, "ERC20", erc20ABI)
	parser := NewABILogParser(contractABI)

	log := &types.Log{
		Address: addr,
		Topics:  []common.Hash{common.HexToHash("0xdeadbeef")},
	}
	_, err := parser.Parse(log)
	if err == nil {
		t.Error("expected error for unknown event")
	}
}

// ========== ParsedEvent Tests ==========

func TestParsedEvent_Fields(t *testing.T) {
	event := &ParsedEvent{
		ContractAddress: common.HexToAddress("0x1"),
		ContractName:    "TestContract",
		EventName:       "Transfer",
		EventSig:        common.HexToHash("0xddf252ad"),
		BlockNumber:     100,
		TxHash:          common.HexToHash("0xabc"),
		LogIndex:        5,
		Data:            map[string]interface{}{"key": "value"},
		Timestamp:       1700000000,
	}

	if event.EventName != "Transfer" {
		t.Errorf("expected Transfer, got %s", event.EventName)
	}
	if event.BlockNumber != 100 {
		t.Errorf("expected block 100, got %d", event.BlockNumber)
	}
}

// ========== convertABIValue Tests ==========

func TestConvertABIValue(t *testing.T) {
	// Test [32]byte -> common.Hash
	var bytes32 [32]byte
	copy(bytes32[:], []byte("test"))
	result := convertABIValue(bytes32)
	if _, ok := result.(common.Hash); !ok {
		t.Error("expected [32]byte to convert to common.Hash")
	}

	// Test []byte passthrough
	bytesVal := []byte{1, 2, 3}
	result = convertABIValue(bytesVal)
	if _, ok := result.([]byte); !ok {
		t.Error("expected []byte passthrough")
	}

	// Test *big.Int passthrough
	bigVal := big.NewInt(42)
	result = convertABIValue(bigVal)
	if _, ok := result.(*big.Int); !ok {
		t.Error("expected *big.Int passthrough")
	}

	// Test common.Address passthrough
	addrVal := common.HexToAddress("0x1")
	result = convertABIValue(addrVal)
	if _, ok := result.(common.Address); !ok {
		t.Error("expected common.Address passthrough")
	}

	// Test bool passthrough
	result = convertABIValue(true)
	if v, ok := result.(bool); !ok || !v {
		t.Error("expected bool passthrough")
	}

	// Test string passthrough
	result = convertABIValue("hello")
	if v, ok := result.(string); !ok || v != "hello" {
		t.Error("expected string passthrough")
	}
}

// ========== EventField Tests ==========

func TestEventField(t *testing.T) {
	field := &EventField{
		Name:    "from",
		Type:    "address",
		Indexed: true,
		Value:   common.HexToAddress("0x1"),
	}

	if field.Name != "from" {
		t.Errorf("expected name 'from', got '%s'", field.Name)
	}
	if !field.Indexed {
		t.Error("expected indexed=true")
	}
}
