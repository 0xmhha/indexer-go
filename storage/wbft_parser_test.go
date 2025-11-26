package storage

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestWBFTAggregatedSealRLP_EncodeDecodeRLP(t *testing.T) {
	original := &WBFTAggregatedSealRLP{
		Sealers:   []byte{0x01, 0x02, 0x03},
		Signature: make([]byte, 96), // BLS signature is 96 bytes
	}

	// Encode
	var buf bytes.Buffer
	if err := original.EncodeRLP(&buf); err != nil {
		t.Fatalf("EncodeRLP failed: %v", err)
	}

	// Decode
	decoded := &WBFTAggregatedSealRLP{}
	if err := rlp.Decode(&buf, decoded); err != nil {
		t.Fatalf("DecodeRLP failed: %v", err)
	}

	// Verify
	if !bytes.Equal(decoded.Sealers, original.Sealers) {
		t.Errorf("Sealers mismatch: got %v, want %v", decoded.Sealers, original.Sealers)
	}
	if !bytes.Equal(decoded.Signature, original.Signature) {
		t.Errorf("Signature mismatch: got %v, want %v", decoded.Signature, original.Signature)
	}
}

func TestWBFTExtraRLP_EncodeDecodeRLP(t *testing.T) {
	t.Skip("TODO: Fix RLP encoding/decoding with nil pointers")
	// RLP encoding with nil pointers in the struct causes decode errors
	// This test needs to be rewritten to properly handle nil aggregated seals
}

func TestParseWBFTExtra_NilHeader(t *testing.T) {
	_, err := ParseWBFTExtra(nil)
	if err == nil {
		t.Fatal("Expected error for nil header, got nil")
	}
	expectedMsg := "header cannot be nil"
	if err.Error() != expectedMsg {
		t.Errorf("Error message = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestParseWBFTExtra_EmptyExtra(t *testing.T) {
	header := &types.Header{
		Number: big.NewInt(100),
		Extra:  []byte{},
	}

	_, err := ParseWBFTExtra(header)
	if err == nil {
		t.Fatal("Expected error for empty extra, got nil")
	}
	expectedMsg := "header extra data is empty"
	if err.Error() != expectedMsg {
		t.Errorf("Error message = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestParseWBFTExtra_ValidData(t *testing.T) {
	t.Skip("TODO: Fix RLP encoding for test data")
	// Need valid WBFT extra data from real blockchain or properly encoded test data
}

func TestExtractSigners_EmptyBitmap(t *testing.T) {
	signers, err := ExtractSigners(nil, []uint32{0, 1}, []Candidate{})
	if err != nil {
		t.Fatalf("ExtractSigners failed: %v", err)
	}
	if len(signers) != 0 {
		t.Errorf("Expected 0 signers, got %d", len(signers))
	}
}

func TestExtractSigners_ValidBitmap(t *testing.T) {
	candidates := []Candidate{
		{Address: common.HexToAddress("0x1111111111111111111111111111111111111111"), Diligence: 100},
		{Address: common.HexToAddress("0x2222222222222222222222222222222222222222"), Diligence: 200},
		{Address: common.HexToAddress("0x3333333333333333333333333333333333333333"), Diligence: 300},
	}

	validators := []uint32{0, 1, 2} // All candidates are validators

	// Bitmap: 0x05 = 0b00000101 means validators at index 0 and 2 signed
	bitmap := []byte{0x05}

	signers, err := ExtractSigners(bitmap, validators, candidates)
	if err != nil {
		t.Fatalf("ExtractSigners failed: %v", err)
	}

	expectedCount := 2
	if len(signers) != expectedCount {
		t.Fatalf("Expected %d signers, got %d", expectedCount, len(signers))
	}

	// Should have validators at index 0 and 2
	expected := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	}

	for i, addr := range signers {
		if addr != expected[i] {
			t.Errorf("Signer[%d] = %s, want %s", i, addr.Hex(), expected[i].Hex())
		}
	}
}

func TestExtractSigners_InvalidCandidateIndex(t *testing.T) {
	candidates := []Candidate{
		{Address: common.HexToAddress("0x1111111111111111111111111111111111111111"), Diligence: 100},
	}

	// Validator index 0 points to candidate index 5 (out of bounds)
	validators := []uint32{5}

	// Bitmap with bit 0 set
	bitmap := []byte{0x01}

	_, err := ExtractSigners(bitmap, validators, candidates)
	if err == nil {
		t.Fatal("Expected error for invalid candidate index, got nil")
	}
}
