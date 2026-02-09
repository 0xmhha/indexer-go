package factory

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// ========== flexibleUint64 Tests ==========

func TestFlexibleUint64_UnmarshalJSON_HexString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected uint64
	}{
		{"zero hex", `"0x0"`, 0},
		{"small hex", `"0x1a"`, 26},
		{"large hex", `"0xffffffff"`, 4294967295},
		{"block number", `"0x10f447"`, 1111111},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f flexibleUint64
			if err := f.UnmarshalJSON([]byte(tc.input)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if uint64(f) != tc.expected {
				t.Errorf("expected %d, got %d", tc.expected, uint64(f))
			}
		})
	}
}

func TestFlexibleUint64_UnmarshalJSON_Number(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected uint64
	}{
		{"zero", `0`, 0},
		{"small number", `42`, 42},
		{"large number", `4294967295`, 4294967295},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f flexibleUint64
			if err := f.UnmarshalJSON([]byte(tc.input)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if uint64(f) != tc.expected {
				t.Errorf("expected %d, got %d", tc.expected, uint64(f))
			}
		})
	}
}

func TestFlexibleUint64_UnmarshalJSON_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"invalid string", `"not_a_number"`},
		{"boolean", `true`},
		{"null", `null`},
		{"negative", `-1`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f flexibleUint64
			if err := f.UnmarshalJSON([]byte(tc.input)); err == nil {
				t.Error("expected error for invalid input")
			}
		})
	}
}

// ========== rpcTransaction Tests ==========

func TestRpcTransaction_UnmarshalJSON_StandardTx(t *testing.T) {
	// Standard EIP-1559 transaction (type 0x02)
	txJSON := `{
		"type": "0x2",
		"chainId": "0x1",
		"nonce": "0x1",
		"maxPriorityFeePerGas": "0x3b9aca00",
		"maxFeePerGas": "0x77359400",
		"gas": "0x5208",
		"to": "0x0000000000000000000000000000000000000001",
		"value": "0xde0b6b3a7640000",
		"input": "0x",
		"accessList": [],
		"v": "0x1",
		"r": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		"s": "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		"blockNumber": "0x100",
		"blockHash": "0x0000000000000000000000000000000000000000000000000000000000000001"
	}`

	var tx rpcTransaction
	if err := json.Unmarshal([]byte(txJSON), &tx); err != nil {
		t.Fatalf("failed to unmarshal standard tx: %v", err)
	}

	if tx.tx == nil {
		t.Fatal("expected non-nil transaction")
	}

	if tx.IsFeeDelegation() {
		t.Error("standard tx should not be fee delegation")
	}

	if tx.OriginalType() != 2 {
		t.Errorf("expected type 2, got %d", tx.OriginalType())
	}

	if tx.FeePayer() != nil {
		t.Error("standard tx should not have fee payer")
	}
}

func TestRpcTransaction_UnmarshalJSON_LegacyTx(t *testing.T) {
	// Legacy transaction (type 0x0)
	txJSON := `{
		"type": "0x0",
		"nonce": "0x0",
		"gasPrice": "0x3b9aca00",
		"gas": "0x5208",
		"to": "0x0000000000000000000000000000000000000001",
		"value": "0x0",
		"input": "0x",
		"v": "0x1c",
		"r": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		"s": "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	}`

	var tx rpcTransaction
	if err := json.Unmarshal([]byte(txJSON), &tx); err != nil {
		t.Fatalf("failed to unmarshal legacy tx: %v", err)
	}

	if tx.tx == nil {
		t.Fatal("expected non-nil transaction")
	}

	if tx.IsFeeDelegation() {
		t.Error("legacy tx should not be fee delegation")
	}

	if tx.OriginalType() != 0 {
		t.Errorf("expected type 0, got %d", tx.OriginalType())
	}
}

func TestRpcTransaction_UnmarshalJSON_FeeDelegationTx(t *testing.T) {
	feePayer := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	txJSON := `{
		"type": "0x16",
		"chainId": "0x1",
		"nonce": "0x5",
		"maxPriorityFeePerGas": "0x3b9aca00",
		"maxFeePerGas": "0x77359400",
		"gas": "0x5208",
		"to": "0x0000000000000000000000000000000000000001",
		"value": "0xde0b6b3a7640000",
		"input": "0xabcdef",
		"accessList": [],
		"v": "0x1",
		"r": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		"s": "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		"feePayer": "0x1234567890abcdef1234567890abcdef12345678",
		"feePayerSignatures": [{
			"v": "0x1b",
			"r": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"s": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}]
	}`

	var tx rpcTransaction
	if err := json.Unmarshal([]byte(txJSON), &tx); err != nil {
		t.Fatalf("failed to unmarshal fee delegation tx: %v", err)
	}

	if tx.tx == nil {
		t.Fatal("expected non-nil transaction")
	}

	// Should be identified as fee delegation
	if !tx.IsFeeDelegation() {
		t.Error("expected fee delegation tx")
	}

	// Original type should be 0x16
	if tx.OriginalType() != FeeDelegateDynamicFeeTxType {
		t.Errorf("expected original type 0x16, got 0x%x", tx.OriginalType())
	}

	// Fee payer should be set
	if tx.FeePayer() == nil {
		t.Fatal("expected non-nil fee payer")
	}
	if *tx.FeePayer() != feePayer {
		t.Errorf("expected fee payer %s, got %s", feePayer.Hex(), tx.FeePayer().Hex())
	}

	// Fee payer signature should be set
	v, r, s := tx.FeePayerSignature()
	if v == nil || r == nil || s == nil {
		t.Fatal("expected non-nil fee payer signature")
	}
	if v.Cmp(big.NewInt(0x1b)) != 0 {
		t.Errorf("expected fee payer V=0x1b, got %s", v.Text(16))
	}

	// Underlying tx should be DynamicFeeTx (type 2) for go-ethereum compat
	if tx.tx.Type() != 2 {
		t.Errorf("expected underlying type 2 (DynamicFeeTx), got %d", tx.tx.Type())
	}

	// Verify parsed fields
	if tx.tx.Nonce() != 5 {
		t.Errorf("expected nonce 5, got %d", tx.tx.Nonce())
	}
	if tx.tx.Gas() != 0x5208 {
		t.Errorf("expected gas 21000, got %d", tx.tx.Gas())
	}
}

func TestRpcTransaction_UnmarshalJSON_FeeDelegation_NoFeePayer(t *testing.T) {
	txJSON := `{
		"type": "0x16",
		"chainId": "0x1",
		"nonce": "0x0",
		"maxPriorityFeePerGas": "0x0",
		"maxFeePerGas": "0x0",
		"gas": "0x5208",
		"to": "0x0000000000000000000000000000000000000001",
		"value": "0x0",
		"input": "0x",
		"accessList": [],
		"v": "0x1",
		"r": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		"s": "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		"feePayerSignatures": []
	}`

	var tx rpcTransaction
	if err := json.Unmarshal([]byte(txJSON), &tx); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if !tx.IsFeeDelegation() {
		t.Error("expected fee delegation tx")
	}

	// Fee payer should be nil since not provided
	if tx.FeePayer() != nil {
		t.Error("expected nil fee payer when not provided")
	}

	// Fee payer signature should be nil
	v, r, s := tx.FeePayerSignature()
	if v != nil || r != nil || s != nil {
		t.Error("expected nil fee payer signature when not provided")
	}
}

func TestRpcTransaction_UnmarshalJSON_FeeDelegation_NilValue(t *testing.T) {
	// Fee Delegation tx without value field - should default to 0
	txJSON := `{
		"type": "0x16",
		"chainId": "0x1",
		"nonce": "0x0",
		"maxPriorityFeePerGas": "0x0",
		"maxFeePerGas": "0x0",
		"gas": "0x5208",
		"to": "0x0000000000000000000000000000000000000001",
		"input": "0x",
		"accessList": [],
		"v": "0x1",
		"r": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		"s": "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	}`

	var tx rpcTransaction
	if err := json.Unmarshal([]byte(txJSON), &tx); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if tx.tx.Value().Cmp(big.NewInt(0)) != 0 {
		t.Errorf("expected value 0, got %s", tx.tx.Value())
	}
}

func TestRpcTransaction_UnmarshalJSON_UnsupportedType(t *testing.T) {
	// Unknown type that is neither standard nor fee delegation
	txJSON := `{
		"type": "0xff",
		"nonce": "0x0"
	}`

	var tx rpcTransaction
	err := json.Unmarshal([]byte(txJSON), &tx)
	if err == nil {
		t.Error("expected error for unsupported type")
	}
}

func TestRpcTransaction_UnmarshalJSON_InvalidJSON(t *testing.T) {
	var tx rpcTransaction
	err := json.Unmarshal([]byte(`{invalid json}`), &tx)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ========== parseRawBlockWithMetas Tests ==========

func TestParseRawBlockWithMetas_BasicBlock(t *testing.T) {
	blockJSON := `{
		"parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
		"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
		"miner": "0x0000000000000000000000000000000000000000",
		"stateRoot": "0x0000000000000000000000000000000000000000000000000000000000000001",
		"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
		"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
		"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		"difficulty": "0x0",
		"number": "0x100",
		"gasLimit": "0x1c9c380",
		"gasUsed": "0x0",
		"timestamp": "0x6597b000",
		"extraData": "0x",
		"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
		"nonce": "0x0000000000000000",
		"transactions": [],
		"uncles": []
	}`

	block, metas, err := parseRawBlockWithMetas(json.RawMessage(blockJSON))
	if err != nil {
		t.Fatalf("failed to parse block: %v", err)
	}

	if block == nil {
		t.Fatal("expected non-nil block")
	}

	if block.NumberU64() != 0x100 {
		t.Errorf("expected block number 256, got %d", block.NumberU64())
	}

	if block.GasLimit() != 0x1c9c380 {
		t.Errorf("expected gas limit 30000000, got %d", block.GasLimit())
	}

	if len(block.Transactions()) != 0 {
		t.Errorf("expected 0 transactions, got %d", len(block.Transactions()))
	}

	if len(metas) != 0 {
		t.Errorf("expected 0 fee delegation metas, got %d", len(metas))
	}
}

func TestParseRawBlockWithMetas_WithEIP4844Fields(t *testing.T) {
	blockJSON := `{
		"parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
		"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
		"miner": "0x0000000000000000000000000000000000000000",
		"stateRoot": "0x0000000000000000000000000000000000000000000000000000000000000001",
		"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
		"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
		"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		"difficulty": "0x0",
		"number": "0x200",
		"gasLimit": "0x1c9c380",
		"gasUsed": "0x5208",
		"timestamp": "0x6597b100",
		"extraData": "0x",
		"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
		"nonce": "0x0000000000000000",
		"baseFeePerGas": "0x3b9aca00",
		"blobGasUsed": "0x20000",
		"excessBlobGas": 131072,
		"parentBeaconBlockRoot": "0x0000000000000000000000000000000000000000000000000000000000000002",
		"transactions": [],
		"uncles": []
	}`

	block, _, err := parseRawBlockWithMetas(json.RawMessage(blockJSON))
	if err != nil {
		t.Fatalf("failed to parse block: %v", err)
	}

	if block.NumberU64() != 0x200 {
		t.Errorf("expected block number 512, got %d", block.NumberU64())
	}

	header := block.Header()

	if header.BaseFee == nil || header.BaseFee.Cmp(big.NewInt(0x3b9aca00)) != 0 {
		t.Errorf("expected base fee 1000000000, got %v", header.BaseFee)
	}

	if header.BlobGasUsed == nil || *header.BlobGasUsed != 0x20000 {
		t.Errorf("expected blob gas used 131072, got %v", header.BlobGasUsed)
	}

	if header.ExcessBlobGas == nil || *header.ExcessBlobGas != 131072 {
		t.Errorf("expected excess blob gas 131072, got %v", header.ExcessBlobGas)
	}

	if header.ParentBeaconRoot == nil {
		t.Error("expected non-nil parent beacon root")
	}
}

func TestParseRawBlockWithMetas_WithStandardTx(t *testing.T) {
	blockJSON := `{
		"parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
		"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
		"miner": "0x0000000000000000000000000000000000000000",
		"stateRoot": "0x0000000000000000000000000000000000000000000000000000000000000001",
		"transactionsRoot": "0x0000000000000000000000000000000000000000000000000000000000000002",
		"receiptsRoot": "0x0000000000000000000000000000000000000000000000000000000000000003",
		"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		"difficulty": "0x0",
		"number": "0x300",
		"gasLimit": "0x1c9c380",
		"gasUsed": "0x5208",
		"timestamp": "0x6597b200",
		"extraData": "0x",
		"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
		"nonce": "0x0000000000000000",
		"baseFeePerGas": "0x3b9aca00",
		"transactions": [{
			"type": "0x2",
			"chainId": "0x1",
			"nonce": "0x0",
			"maxPriorityFeePerGas": "0x3b9aca00",
			"maxFeePerGas": "0x77359400",
			"gas": "0x5208",
			"to": "0x0000000000000000000000000000000000000001",
			"value": "0xde0b6b3a7640000",
			"input": "0x",
			"accessList": [],
			"v": "0x1",
			"r": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			"s": "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
		}],
		"uncles": []
	}`

	block, metas, err := parseRawBlockWithMetas(json.RawMessage(blockJSON))
	if err != nil {
		t.Fatalf("failed to parse block: %v", err)
	}

	if len(block.Transactions()) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(block.Transactions()))
	}

	if len(metas) != 0 {
		t.Errorf("expected 0 fee delegation metas for standard tx, got %d", len(metas))
	}

	tx := block.Transactions()[0]
	if tx.Type() != 2 {
		t.Errorf("expected tx type 2, got %d", tx.Type())
	}
}

func TestParseRawBlockWithMetas_WithFeeDelegationTx(t *testing.T) {
	blockJSON := `{
		"parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
		"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
		"miner": "0x0000000000000000000000000000000000000000",
		"stateRoot": "0x0000000000000000000000000000000000000000000000000000000000000001",
		"transactionsRoot": "0x0000000000000000000000000000000000000000000000000000000000000002",
		"receiptsRoot": "0x0000000000000000000000000000000000000000000000000000000000000003",
		"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		"difficulty": "0x0",
		"number": "0x400",
		"gasLimit": "0x1c9c380",
		"gasUsed": "0xa410",
		"timestamp": "0x6597b300",
		"extraData": "0x",
		"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
		"nonce": "0x0000000000000000",
		"baseFeePerGas": "0x3b9aca00",
		"transactions": [
			{
				"type": "0x2",
				"chainId": "0x1",
				"nonce": "0x0",
				"maxPriorityFeePerGas": "0x3b9aca00",
				"maxFeePerGas": "0x77359400",
				"gas": "0x5208",
				"to": "0x0000000000000000000000000000000000000001",
				"value": "0x0",
				"input": "0x",
				"accessList": [],
				"v": "0x1",
				"r": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"s": "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
			},
			{
				"type": "0x16",
				"chainId": "0x1",
				"nonce": "0x1",
				"maxPriorityFeePerGas": "0x3b9aca00",
				"maxFeePerGas": "0x77359400",
				"gas": "0x5208",
				"to": "0x0000000000000000000000000000000000000002",
				"value": "0xde0b6b3a7640000",
				"input": "0x",
				"accessList": [],
				"v": "0x1",
				"r": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"s": "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				"feePayer": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"feePayerSignatures": [{
					"v": "0x1b",
					"r": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					"s": "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
				}]
			}
		],
		"uncles": []
	}`

	block, metas, err := parseRawBlockWithMetas(json.RawMessage(blockJSON))
	if err != nil {
		t.Fatalf("failed to parse block: %v", err)
	}

	if len(block.Transactions()) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(block.Transactions()))
	}

	// First tx is standard, no metadata
	// Second tx is fee delegation, should have metadata
	if len(metas) != 1 {
		t.Fatalf("expected 1 fee delegation meta, got %d", len(metas))
	}

	meta := metas[0]
	if meta.OriginalType != FeeDelegateDynamicFeeTxType {
		t.Errorf("expected original type 0x16, got 0x%x", meta.OriginalType)
	}
	if meta.BlockNumber != 0x400 {
		t.Errorf("expected block number 1024, got %d", meta.BlockNumber)
	}
	expectedFeePayer := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if meta.FeePayer != expectedFeePayer {
		t.Errorf("expected fee payer %s, got %s", expectedFeePayer.Hex(), meta.FeePayer.Hex())
	}
	if meta.FeePayerV == nil || meta.FeePayerV.Cmp(big.NewInt(0x1b)) != 0 {
		t.Errorf("expected fee payer V=0x1b, got %v", meta.FeePayerV)
	}
}

func TestParseRawBlockWithMetas_InvalidJSON(t *testing.T) {
	_, _, err := parseRawBlockWithMetas(json.RawMessage(`{invalid}`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseRawBlock(t *testing.T) {
	blockJSON := `{
		"parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
		"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
		"miner": "0x0000000000000000000000000000000000000000",
		"stateRoot": "0x0000000000000000000000000000000000000000000000000000000000000001",
		"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
		"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
		"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		"difficulty": "0x0",
		"number": "0x1",
		"gasLimit": "0x1c9c380",
		"gasUsed": "0x0",
		"timestamp": "0x6597b000",
		"extraData": "0x",
		"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
		"nonce": "0x0000000000000000",
		"transactions": [],
		"uncles": []
	}`

	block, err := parseRawBlock(json.RawMessage(blockJSON))
	if err != nil {
		t.Fatalf("parseRawBlock error: %v", err)
	}

	if block.NumberU64() != 1 {
		t.Errorf("expected block number 1, got %d", block.NumberU64())
	}
}

// ========== toBlockNumArg Tests ==========

func TestToBlockNumArg(t *testing.T) {
	tests := []struct {
		name     string
		input    *big.Int
		expected string
	}{
		{"nil (latest)", nil, "latest"},
		{"pending (-1)", big.NewInt(-1), "pending"},
		{"zero", big.NewInt(0), "0x0"},
		{"block 1", big.NewInt(1), "0x1"},
		{"block 256", big.NewInt(256), "0x100"},
		{"large block", big.NewInt(1000000), "0xf4240"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := toBlockNumArg(tc.input)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

// ========== FeeDelegationMeta Tests ==========

func TestFeeDelegationMeta_Fields(t *testing.T) {
	feePayer := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	txHash := common.HexToHash("0xabcdef")

	meta := &FeeDelegationMeta{
		TxHash:       txHash,
		BlockNumber:  100,
		OriginalType: FeeDelegateDynamicFeeTxType,
		FeePayer:     feePayer,
		FeePayerV:    big.NewInt(27),
		FeePayerR:    big.NewInt(12345),
		FeePayerS:    big.NewInt(67890),
	}

	if meta.TxHash != txHash {
		t.Errorf("expected tx hash %s, got %s", txHash.Hex(), meta.TxHash.Hex())
	}
	if meta.BlockNumber != 100 {
		t.Errorf("expected block 100, got %d", meta.BlockNumber)
	}
	if meta.OriginalType != 0x16 {
		t.Errorf("expected type 0x16, got 0x%x", meta.OriginalType)
	}
	if meta.FeePayer != feePayer {
		t.Errorf("expected fee payer %s, got %s", feePayer.Hex(), meta.FeePayer.Hex())
	}
}

// ========== rpcTransaction Method Tests ==========

func TestRpcTransaction_DefaultOriginalType(t *testing.T) {
	// When no originalType is set, OriginalType() returns the go-ethereum tx type
	tx := &rpcTransaction{}
	// tx.tx is nil, but OriginalType dereferences it - so let's test with a real tx
	txJSON := `{
		"type": "0x2",
		"chainId": "0x1",
		"nonce": "0x0",
		"maxPriorityFeePerGas": "0x0",
		"maxFeePerGas": "0x0",
		"gas": "0x5208",
		"to": "0x0000000000000000000000000000000000000001",
		"value": "0x0",
		"input": "0x",
		"accessList": [],
		"v": "0x1",
		"r": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		"s": "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	}`

	_ = tx
	var tx2 rpcTransaction
	if err := json.Unmarshal([]byte(txJSON), &tx2); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// originalType is nil for standard tx, so OriginalType returns tx.Type()
	if tx2.originalType != nil {
		t.Error("expected nil originalType for standard tx")
	}
	if tx2.OriginalType() != 2 {
		t.Errorf("expected OriginalType 2, got %d", tx2.OriginalType())
	}
}

func TestRpcTransaction_IsFeeDelegation_False(t *testing.T) {
	tx := &rpcTransaction{}
	if tx.IsFeeDelegation() {
		t.Error("empty rpcTransaction should not be fee delegation")
	}

	// Non-0x16 originalType
	otherType := uint8(0x02)
	tx2 := &rpcTransaction{originalType: &otherType}
	if tx2.IsFeeDelegation() {
		t.Error("type 0x02 should not be fee delegation")
	}
}

func TestRpcTransaction_FeePayer_Nil(t *testing.T) {
	tx := &rpcTransaction{}
	if tx.FeePayer() != nil {
		t.Error("expected nil fee payer for empty tx")
	}
}

func TestRpcTransaction_FeePayerSignature_Nil(t *testing.T) {
	tx := &rpcTransaction{}
	v, r, s := tx.FeePayerSignature()
	if v != nil || r != nil || s != nil {
		t.Error("expected nil signature for empty tx")
	}
}

// ========== FeeDelegateDynamicFeeTxType Constant Test ==========

func TestFeeDelegateDynamicFeeTxType_Value(t *testing.T) {
	if FeeDelegateDynamicFeeTxType != 0x16 {
		t.Errorf("expected FeeDelegateDynamicFeeTxType=0x16, got 0x%x", FeeDelegateDynamicFeeTxType)
	}
}
