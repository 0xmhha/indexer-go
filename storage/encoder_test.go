package storage

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestEncodeDecodeUint64(t *testing.T) {
	tests := []struct {
		name  string
		value uint64
	}{
		{"zero", 0},
		{"one", 1},
		{"small", 100},
		{"medium", 1000000},
		{"large", 18446744073709551615},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeUint64(tt.value)
			if len(encoded) != 8 {
				t.Errorf("EncodeUint64() length = %d, want 8", len(encoded))
			}

			decoded, err := DecodeUint64(encoded)
			if err != nil {
				t.Errorf("DecodeUint64() error = %v", err)
			}
			if decoded != tt.value {
				t.Errorf("DecodeUint64() = %d, want %d", decoded, tt.value)
			}
		})
	}
}

func TestDecodeUint64_InvalidData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"too short", []byte{1, 2, 3}},
		{"too long", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeUint64(tt.data)
			if err == nil {
				t.Error("DecodeUint64() should return error for invalid data")
			}
		})
	}
}

func TestEncodeDecodeTxLocation(t *testing.T) {
	tests := []struct {
		name string
		loc  *TxLocation
	}{
		{
			"genesis tx",
			&TxLocation{
				BlockHeight: 0,
				TxIndex:     0,
				BlockHash:   common.Hash{},
			},
		},
		{
			"regular tx",
			&TxLocation{
				BlockHeight: 1000,
				TxIndex:     5,
				BlockHash:   common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
			},
		},
		{
			"large values",
			&TxLocation{
				BlockHeight: 18446744073709551615,
				TxIndex:     18446744073709551615,
				BlockHash:   common.HexToHash("0xfedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeTxLocation(tt.loc)
			if err != nil {
				t.Errorf("EncodeTxLocation() error = %v", err)
			}
			if len(encoded) == 0 {
				t.Error("EncodeTxLocation() returned empty data")
			}

			decoded, err := DecodeTxLocation(encoded)
			if err != nil {
				t.Errorf("DecodeTxLocation() error = %v", err)
			}

			if decoded.BlockHeight != tt.loc.BlockHeight {
				t.Errorf("BlockHeight = %d, want %d", decoded.BlockHeight, tt.loc.BlockHeight)
			}
			if decoded.TxIndex != tt.loc.TxIndex {
				t.Errorf("TxIndex = %d, want %d", decoded.TxIndex, tt.loc.TxIndex)
			}
			if decoded.BlockHash != tt.loc.BlockHash {
				t.Errorf("BlockHash = %s, want %s", decoded.BlockHash.Hex(), tt.loc.BlockHash.Hex())
			}
		})
	}
}

func TestEncodeDecodeBlock(t *testing.T) {
	// Create a sample block
	header := &types.Header{
		ParentHash:  common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    common.HexToAddress("0x0000000000000000000000000000000000000000"),
		Root:        common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Bloom:       types.Bloom{},
		Difficulty:  big.NewInt(0),
		Number:      big.NewInt(0),
		GasLimit:    5000,
		GasUsed:     0,
		Time:        1234567890,
		Extra:       []byte{},
		MixDigest:   common.Hash{},
		Nonce:       types.BlockNonce{},
	}

	block := types.NewBlockWithHeader(header)

	// Encode
	encoded, err := EncodeBlock(block)
	if err != nil {
		t.Fatalf("EncodeBlock() error = %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("EncodeBlock() returned empty data")
	}

	// Decode
	decoded, err := DecodeBlock(encoded)
	if err != nil {
		t.Fatalf("DecodeBlock() error = %v", err)
	}

	// Verify
	if decoded.Hash() != block.Hash() {
		t.Errorf("Block hash mismatch: got %s, want %s", decoded.Hash().Hex(), block.Hash().Hex())
	}
	if decoded.Number().Cmp(block.Number()) != 0 {
		t.Errorf("Block number mismatch: got %d, want %d", decoded.Number(), block.Number())
	}
}

func TestEncodeDecodeTransaction(t *testing.T) {
	// Create a legacy transaction
	tx := types.NewTransaction(
		0,                                                                 // nonce
		common.HexToAddress("0x1234567890123456789012345678901234567890"), // to
		big.NewInt(1000000000),                                            // value
		21000,                                                             // gas limit
		big.NewInt(1000000000),                                            // gas price
		[]byte{},                                                          // data
	)

	// Encode
	encoded, err := EncodeTransaction(tx)
	if err != nil {
		t.Fatalf("EncodeTransaction() error = %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("EncodeTransaction() returned empty data")
	}

	// Decode
	decoded, err := DecodeTransaction(encoded)
	if err != nil {
		t.Fatalf("DecodeTransaction() error = %v", err)
	}

	// Verify
	if decoded.Hash() != tx.Hash() {
		t.Errorf("Transaction hash mismatch: got %s, want %s", decoded.Hash().Hex(), tx.Hash().Hex())
	}
	if decoded.Nonce() != tx.Nonce() {
		t.Errorf("Nonce mismatch: got %d, want %d", decoded.Nonce(), tx.Nonce())
	}
	if decoded.Value().Cmp(tx.Value()) != 0 {
		t.Errorf("Value mismatch: got %s, want %s", decoded.Value(), tx.Value())
	}
}

func TestEncodeDecodeReceipt(t *testing.T) {
	// Create a sample receipt
	// Note: TxHash, BlockHash, BlockNumber, TransactionIndex are not RLP-encoded
	// as they are derived fields
	receipt := &types.Receipt{
		Type:              types.LegacyTxType,
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
		Bloom:             types.Bloom{},
		Logs:              []*types.Log{},
		ContractAddress:   common.Address{},
		GasUsed:           21000,
	}

	// Encode
	encoded, err := EncodeReceipt(receipt)
	if err != nil {
		t.Fatalf("EncodeReceipt() error = %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("EncodeReceipt() returned empty data")
	}

	// Decode
	decoded, err := DecodeReceipt(encoded)
	if err != nil {
		t.Fatalf("DecodeReceipt() error = %v", err)
	}

	// Verify RLP-encoded fields only
	if decoded.Status != receipt.Status {
		t.Errorf("Status mismatch: got %d, want %d", decoded.Status, receipt.Status)
	}
	if decoded.CumulativeGasUsed != receipt.CumulativeGasUsed {
		t.Errorf("CumulativeGasUsed mismatch: got %d, want %d", decoded.CumulativeGasUsed, receipt.CumulativeGasUsed)
	}
	if decoded.Type != receipt.Type {
		t.Errorf("Type mismatch: got %d, want %d", decoded.Type, receipt.Type)
	}
}

func TestDecodeBlock_InvalidData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"invalid RLP", []byte{0xff, 0xff, 0xff}},
		{"garbage", []byte("not a valid block")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeBlock(tt.data)
			if err == nil {
				t.Error("DecodeBlock() should return error for invalid data")
			}
		})
	}
}

func TestDecodeTransaction_InvalidData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"invalid RLP", []byte{0xff, 0xff, 0xff}},
		{"garbage", []byte("not a valid transaction")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeTransaction(tt.data)
			if err == nil {
				t.Error("DecodeTransaction() should return error for invalid data")
			}
		})
	}
}

func TestDecodeReceipt_InvalidData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"invalid RLP", []byte{0xff, 0xff, 0xff}},
		{"garbage", []byte("not a valid receipt")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeReceipt(tt.data)
			if err == nil {
				t.Error("DecodeReceipt() should return error for invalid data")
			}
		})
	}
}

func TestEncodingConsistency(t *testing.T) {
	// Create a block
	header := &types.Header{
		ParentHash:  common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    common.HexToAddress("0x0000000000000000000000000000000000000000"),
		Root:        common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Bloom:       types.Bloom{},
		Difficulty:  big.NewInt(0),
		Number:      big.NewInt(100),
		GasLimit:    5000,
		GasUsed:     0,
		Time:        1234567890,
		Extra:       []byte{},
		MixDigest:   common.Hash{},
		Nonce:       types.BlockNonce{},
	}
	block := types.NewBlockWithHeader(header)

	// Encode twice
	encoded1, _ := EncodeBlock(block)
	encoded2, _ := EncodeBlock(block)

	// Should be identical
	if !bytes.Equal(encoded1, encoded2) {
		t.Error("EncodeBlock() is not deterministic")
	}
}

func BenchmarkEncodeBlock(b *testing.B) {
	header := &types.Header{
		ParentHash:  common.Hash{},
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    common.Address{},
		Root:        common.Hash{},
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Bloom:       types.Bloom{},
		Difficulty:  big.NewInt(0),
		Number:      big.NewInt(100),
		GasLimit:    5000,
		GasUsed:     0,
		Time:        1234567890,
		Extra:       []byte{},
		MixDigest:   common.Hash{},
		Nonce:       types.BlockNonce{},
	}
	block := types.NewBlockWithHeader(header)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EncodeBlock(block)
	}
}

func BenchmarkDecodeBlock(b *testing.B) {
	header := &types.Header{
		ParentHash:  common.Hash{},
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    common.Address{},
		Root:        common.Hash{},
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Bloom:       types.Bloom{},
		Difficulty:  big.NewInt(0),
		Number:      big.NewInt(100),
		GasLimit:    5000,
		GasUsed:     0,
		Time:        1234567890,
		Extra:       []byte{},
		MixDigest:   common.Hash{},
		Nonce:       types.BlockNonce{},
	}
	block := types.NewBlockWithHeader(header)
	encoded, _ := EncodeBlock(block)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeBlock(encoded)
	}
}

func TestEncodeBlock_Nil(t *testing.T) {
	_, err := EncodeBlock(nil)
	if err == nil {
		t.Error("EncodeBlock(nil) should return error")
	}
}

func TestEncodeTransaction_Nil(t *testing.T) {
	_, err := EncodeTransaction(nil)
	if err == nil {
		t.Error("EncodeTransaction(nil) should return error")
	}
}

func TestEncodeReceipt_Nil(t *testing.T) {
	_, err := EncodeReceipt(nil)
	if err == nil {
		t.Error("EncodeReceipt(nil) should return error")
	}
}

func TestEncodeTxLocation_Nil(t *testing.T) {
	_, err := EncodeTxLocation(nil)
	if err == nil {
		t.Error("EncodeTxLocation(nil) should return error")
	}
}

func TestDecodeTxLocation_InvalidData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"invalid RLP", []byte{0xff, 0xff, 0xff}},
		{"garbage", []byte("not valid")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeTxLocation(tt.data)
			if err == nil {
				t.Error("DecodeTxLocation() should return error for invalid data")
			}
		})
	}
}
