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
	// Test with all non-nil seals
	original := &WBFTExtraRLP{
		VanityData:   []byte("test vanity data"),
		RandaoReveal: []byte("randao reveal bytes"),
		PrevRound:    5,
		PrevPreparedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{0x01},
			Signature: make([]byte, 96),
		},
		PrevCommittedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{0x02},
			Signature: make([]byte, 96),
		},
		Round: 10,
		PreparedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{0x03},
			Signature: make([]byte, 96),
		},
		CommittedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{0x04},
			Signature: make([]byte, 96),
		},
		GasTip: big.NewInt(1000000000),
		EpochInfo: &EpochInfoRLP{
			Candidates: []*CandidateRLP{
				{
					Addr:      common.HexToAddress("0x1234567890123456789012345678901234567890").Bytes(),
					Diligence: 100,
				},
			},
			Validators:    []uint32{0},
			BLSPublicKeys: [][]byte{make([]byte, 48)},
		},
	}

	// Encode
	var buf bytes.Buffer
	if err := original.EncodeRLP(&buf); err != nil {
		t.Fatalf("EncodeRLP failed: %v", err)
	}

	// Decode
	decoded := &WBFTExtraRLP{}
	s := rlp.NewStream(bytes.NewReader(buf.Bytes()), 0)
	if err := decoded.DecodeRLP(s); err != nil {
		t.Fatalf("DecodeRLP failed: %v", err)
	}

	// Verify basic fields
	if !bytes.Equal(decoded.VanityData, original.VanityData) {
		t.Errorf("VanityData mismatch")
	}
	if !bytes.Equal(decoded.RandaoReveal, original.RandaoReveal) {
		t.Errorf("RandaoReveal mismatch")
	}
	if decoded.PrevRound != original.PrevRound {
		t.Errorf("PrevRound mismatch: got %d, want %d", decoded.PrevRound, original.PrevRound)
	}
	if decoded.Round != original.Round {
		t.Errorf("Round mismatch: got %d, want %d", decoded.Round, original.Round)
	}
	if decoded.GasTip.Cmp(original.GasTip) != 0 {
		t.Errorf("GasTip mismatch: got %s, want %s", decoded.GasTip, original.GasTip)
	}

	// Verify seals are present
	if decoded.PrevPreparedSeal == nil {
		t.Error("PrevPreparedSeal should not be nil")
	}
	if decoded.PrevCommittedSeal == nil {
		t.Error("PrevCommittedSeal should not be nil")
	}
	if decoded.PreparedSeal == nil {
		t.Error("PreparedSeal should not be nil")
	}
	if decoded.CommittedSeal == nil {
		t.Error("CommittedSeal should not be nil")
	}

	// Verify EpochInfo
	if decoded.EpochInfo == nil {
		t.Error("EpochInfo should not be nil")
	} else {
		if len(decoded.EpochInfo.Candidates) != 1 {
			t.Errorf("expected 1 candidate, got %d", len(decoded.EpochInfo.Candidates))
		}
		if len(decoded.EpochInfo.Validators) != 1 {
			t.Errorf("expected 1 validator, got %d", len(decoded.EpochInfo.Validators))
		}
	}
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
	// Create a valid WBFT extra with all fields properly initialized
	// RLP encoding requires all struct fields to be non-nil for proper encoding/decoding
	wbftExtra := &WBFTExtraRLP{
		VanityData:   []byte("test"),
		RandaoReveal: []byte("randao"),
		PrevRound:    1,
		PrevPreparedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{0x01},
			Signature: make([]byte, 96),
		},
		PrevCommittedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{0x02},
			Signature: make([]byte, 96),
		},
		Round: 2,
		PreparedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{0x03},
			Signature: make([]byte, 96),
		},
		CommittedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{0x04},
			Signature: make([]byte, 96),
		},
		GasTip: big.NewInt(1000000000),
		// Empty EpochInfo instead of nil (RLP can't properly encode/decode nil structs)
		EpochInfo: &EpochInfoRLP{
			Candidates:    []*CandidateRLP{},
			Validators:    []uint32{},
			BLSPublicKeys: [][]byte{},
		},
	}

	// Encode to RLP
	extraBytes, err := rlp.EncodeToBytes(wbftExtra)
	if err != nil {
		t.Fatalf("Failed to encode WBFT extra: %v", err)
	}

	header := &types.Header{
		Number: big.NewInt(100),
		Extra:  extraBytes,
	}

	result, err := ParseWBFTExtra(header)
	if err != nil {
		t.Fatalf("ParseWBFTExtra failed: %v", err)
	}

	if result.BlockNumber != 100 {
		t.Errorf("BlockNumber mismatch: got %d, want 100", result.BlockNumber)
	}
	if result.PrevRound != 1 {
		t.Errorf("PrevRound mismatch: got %d, want 1", result.PrevRound)
	}
	if result.Round != 2 {
		t.Errorf("Round mismatch: got %d, want 2", result.Round)
	}
	if result.GasTip.Cmp(big.NewInt(1000000000)) != 0 {
		t.Errorf("GasTip mismatch: got %s, want 1000000000", result.GasTip)
	}

	// Verify seals were converted
	if result.PrevPreparedSeal == nil {
		t.Error("PrevPreparedSeal should not be nil")
	}
	if result.PrevCommittedSeal == nil {
		t.Error("PrevCommittedSeal should not be nil")
	}
	if result.PreparedSeal == nil {
		t.Error("PreparedSeal should not be nil")
	}
	if result.CommittedSeal == nil {
		t.Error("CommittedSeal should not be nil")
	}
}

func TestParseWBFTExtra_WithEpochInfo(t *testing.T) {
	// Create a valid WBFT extra with epoch info
	// All seal fields must be initialized (not nil) for RLP encoding to work properly
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	wbftExtra := &WBFTExtraRLP{
		VanityData:   []byte("test"),
		RandaoReveal: []byte("randao"),
		PrevRound:    1,
		PrevPreparedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{},
			Signature: []byte{},
		},
		PrevCommittedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{},
			Signature: []byte{},
		},
		Round: 2,
		PreparedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{},
			Signature: []byte{},
		},
		CommittedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{},
			Signature: []byte{},
		},
		GasTip: big.NewInt(0),
		EpochInfo: &EpochInfoRLP{
			Candidates: []*CandidateRLP{
				{
					Addr:      addr.Bytes(),
					Diligence: 100,
				},
			},
			Validators:    []uint32{0},
			BLSPublicKeys: [][]byte{make([]byte, 48)},
		},
	}

	// Encode to RLP
	extraBytes, err := rlp.EncodeToBytes(wbftExtra)
	if err != nil {
		t.Fatalf("Failed to encode WBFT extra: %v", err)
	}

	header := &types.Header{
		Number: big.NewInt(100),
		Time:   1234567890,
		Extra:  extraBytes,
	}

	result, err := ParseWBFTExtra(header)
	if err != nil {
		t.Fatalf("ParseWBFTExtra failed: %v", err)
	}

	if result.EpochInfo == nil {
		t.Fatal("EpochInfo should not be nil")
	}

	if len(result.EpochInfo.Candidates) != 1 {
		t.Errorf("expected 1 candidate, got %d", len(result.EpochInfo.Candidates))
	}

	if result.EpochInfo.Candidates[0].Address != addr {
		t.Errorf("Candidate address mismatch: got %s, want %s",
			result.EpochInfo.Candidates[0].Address.Hex(), addr.Hex())
	}

	if result.EpochInfo.Candidates[0].Diligence != 100 {
		t.Errorf("Candidate diligence mismatch: got %d, want 100",
			result.EpochInfo.Candidates[0].Diligence)
	}

	if result.Timestamp != 1234567890 {
		t.Errorf("Timestamp mismatch: got %d, want 1234567890", result.Timestamp)
	}
}

func TestParseWBFTExtra_InvalidCandidateAddressLength(t *testing.T) {
	// Create WBFT extra with invalid candidate address length
	// All seal fields must be initialized for RLP encoding
	wbftExtra := &WBFTExtraRLP{
		VanityData:   []byte("test"),
		RandaoReveal: []byte("randao"),
		PrevRound:    1,
		PrevPreparedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{},
			Signature: []byte{},
		},
		PrevCommittedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{},
			Signature: []byte{},
		},
		Round: 2,
		PreparedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{},
			Signature: []byte{},
		},
		CommittedSeal: &WBFTAggregatedSealRLP{
			Sealers:   []byte{},
			Signature: []byte{},
		},
		GasTip: big.NewInt(0),
		EpochInfo: &EpochInfoRLP{
			Candidates: []*CandidateRLP{
				{
					Addr:      []byte{0x01, 0x02}, // Invalid length (not 20 bytes)
					Diligence: 100,
				},
			},
			Validators:    []uint32{0},
			BLSPublicKeys: [][]byte{make([]byte, 48)},
		},
	}

	// Encode to RLP
	extraBytes, err := rlp.EncodeToBytes(wbftExtra)
	if err != nil {
		t.Fatalf("Failed to encode WBFT extra: %v", err)
	}

	header := &types.Header{
		Number: big.NewInt(100),
		Extra:  extraBytes,
	}

	_, err = ParseWBFTExtra(header)
	if err == nil {
		t.Error("expected error for invalid candidate address length")
	}
}

func TestParseWBFTExtra_InvalidRLP(t *testing.T) {
	header := &types.Header{
		Number: big.NewInt(100),
		Extra:  []byte{0x01, 0x02, 0x03}, // Invalid RLP data
	}
	_, err := ParseWBFTExtra(header)
	if err == nil {
		t.Error("expected error for invalid RLP data")
	}
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

func TestSealerSet(t *testing.T) {
	// SealerSet is just an alias for []byte
	ss := SealerSet([]byte{0x01, 0x02, 0x03})
	if len(ss) != 3 {
		t.Errorf("expected length 3, got %d", len(ss))
	}
	if ss[0] != 0x01 {
		t.Errorf("expected first byte 0x01, got 0x%02x", ss[0])
	}
}

func TestCandidateRLP(t *testing.T) {
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	candidate := &CandidateRLP{
		Addr:      addr.Bytes(),
		Diligence: 150,
	}

	if len(candidate.Addr) != 20 {
		t.Errorf("expected address length 20, got %d", len(candidate.Addr))
	}
	if candidate.Diligence != 150 {
		t.Errorf("expected diligence 150, got %d", candidate.Diligence)
	}
}

func TestEpochInfoRLP(t *testing.T) {
	epochInfo := &EpochInfoRLP{
		Candidates: []*CandidateRLP{
			{
				Addr:      common.HexToAddress("0x1111111111111111111111111111111111111111").Bytes(),
				Diligence: 100,
			},
			{
				Addr:      common.HexToAddress("0x2222222222222222222222222222222222222222").Bytes(),
				Diligence: 200,
			},
		},
		Validators:    []uint32{0, 1},
		BLSPublicKeys: [][]byte{make([]byte, 48), make([]byte, 48)},
	}

	if len(epochInfo.Candidates) != 2 {
		t.Errorf("expected 2 candidates, got %d", len(epochInfo.Candidates))
	}
	if len(epochInfo.Validators) != 2 {
		t.Errorf("expected 2 validators, got %d", len(epochInfo.Validators))
	}
	if len(epochInfo.BLSPublicKeys) != 2 {
		t.Errorf("expected 2 BLS public keys, got %d", len(epochInfo.BLSPublicKeys))
	}
}

func TestExtractSigners_MultipleByteBitmap(t *testing.T) {
	// Create 10 candidates
	candidates := make([]Candidate, 10)
	validators := make([]uint32, 10)
	for i := range 10 {
		candidates[i] = Candidate{
			Address:   common.BigToAddress(big.NewInt(int64(i + 1))),
			Diligence: uint64(100 + i),
		}
		validators[i] = uint32(i)
	}

	// Bitmap: 0x09 0x01 = bits 0, 3, 8 set (validators 0, 3, 8)
	bitmap := []byte{0x09, 0x01}

	signers, err := ExtractSigners(bitmap, validators, candidates)
	if err != nil {
		t.Fatalf("ExtractSigners failed: %v", err)
	}

	if len(signers) != 3 {
		t.Fatalf("Expected 3 signers, got %d", len(signers))
	}
}

func TestWBFTAggregatedSealRLP_DecodeRLP_Invalid(t *testing.T) {
	// Test decoding with invalid data
	invalidData := []byte{0xc0} // Empty RLP list

	decoded := new(WBFTAggregatedSealRLP)
	s := rlp.NewStream(bytes.NewReader(invalidData), 0)
	err := decoded.DecodeRLP(s)
	if err == nil {
		t.Error("expected error for invalid RLP data")
	}
}

// Tests for WBFT encoding/decoding helper functions

func TestEncodeWBFTAggregatedSeal_Nil(t *testing.T) {
	data, err := EncodeWBFTAggregatedSeal(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil for nil seal, got %v", data)
	}
}

func TestEncodeWBFTAggregatedSeal_Valid(t *testing.T) {
	seal := &WBFTAggregatedSeal{
		Sealers:   []byte{0x01, 0x02, 0x03},
		Signature: make([]byte, 96),
	}

	data, err := EncodeWBFTAggregatedSeal(seal)
	if err != nil {
		t.Fatalf("EncodeWBFTAggregatedSeal failed: %v", err)
	}
	if data == nil {
		t.Fatal("expected non-nil encoded data")
	}
	if len(data) == 0 {
		t.Error("expected non-empty encoded data")
	}
}

func TestDecodeWBFTAggregatedSeal_Nil(t *testing.T) {
	seal, err := DecodeWBFTAggregatedSeal(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seal != nil {
		t.Errorf("expected nil for nil data, got %+v", seal)
	}
}

func TestDecodeWBFTAggregatedSeal_Empty(t *testing.T) {
	seal, err := DecodeWBFTAggregatedSeal([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seal != nil {
		t.Errorf("expected nil for empty data, got %+v", seal)
	}
}

func TestEncodeDecodeWBFTAggregatedSeal_RoundTrip(t *testing.T) {
	original := &WBFTAggregatedSeal{
		Sealers:   []byte{0x01, 0x02, 0x03, 0x04},
		Signature: make([]byte, 96),
	}
	// Fill signature with some data
	for i := range original.Signature {
		original.Signature[i] = byte(i % 256)
	}

	// Encode
	data, err := EncodeWBFTAggregatedSeal(original)
	if err != nil {
		t.Fatalf("EncodeWBFTAggregatedSeal failed: %v", err)
	}

	// Decode
	decoded, err := DecodeWBFTAggregatedSeal(data)
	if err != nil {
		t.Fatalf("DecodeWBFTAggregatedSeal failed: %v", err)
	}

	// Verify
	if !bytes.Equal(decoded.Sealers, original.Sealers) {
		t.Errorf("Sealers mismatch: got %v, want %v", decoded.Sealers, original.Sealers)
	}
	if !bytes.Equal(decoded.Signature, original.Signature) {
		t.Errorf("Signature mismatch")
	}
}

func TestDecodeWBFTAggregatedSeal_InvalidRLP(t *testing.T) {
	invalidData := []byte{0x01, 0x02, 0x03} // Invalid RLP
	_, err := DecodeWBFTAggregatedSeal(invalidData)
	if err == nil {
		t.Error("expected error for invalid RLP data")
	}
}

func TestEncodeEpochInfo_Nil(t *testing.T) {
	data, err := EncodeEpochInfo(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil for nil epoch info, got %v", data)
	}
}

func TestEncodeEpochInfo_Valid(t *testing.T) {
	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")

	epochInfo := &EpochInfo{
		EpochNumber:   10,
		BlockNumber:   1000,
		Candidates:    []Candidate{
			{Address: addr1, Diligence: 100},
			{Address: addr2, Diligence: 200},
		},
		Validators:    []uint32{0, 1},
		BLSPublicKeys: [][]byte{make([]byte, 48), make([]byte, 48)},
	}

	data, err := EncodeEpochInfo(epochInfo)
	if err != nil {
		t.Fatalf("EncodeEpochInfo failed: %v", err)
	}
	if data == nil {
		t.Fatal("expected non-nil encoded data")
	}
	if len(data) == 0 {
		t.Error("expected non-empty encoded data")
	}
}

func TestDecodeEpochInfo_Nil(t *testing.T) {
	epochInfo, err := DecodeEpochInfo(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if epochInfo != nil {
		t.Errorf("expected nil for nil data, got %+v", epochInfo)
	}
}

func TestDecodeEpochInfo_Empty(t *testing.T) {
	epochInfo, err := DecodeEpochInfo([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if epochInfo != nil {
		t.Errorf("expected nil for empty data, got %+v", epochInfo)
	}
}

func TestEncodeDecodeEpochInfo_RoundTrip(t *testing.T) {
	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")

	original := &EpochInfo{
		EpochNumber: 10,
		BlockNumber: 1000,
		Candidates: []Candidate{
			{Address: addr1, Diligence: 100},
			{Address: addr2, Diligence: 200},
		},
		Validators:    []uint32{0, 1},
		BLSPublicKeys: [][]byte{make([]byte, 48), make([]byte, 48)},
	}

	// Encode
	data, err := EncodeEpochInfo(original)
	if err != nil {
		t.Fatalf("EncodeEpochInfo failed: %v", err)
	}

	// Decode
	decoded, err := DecodeEpochInfo(data)
	if err != nil {
		t.Fatalf("DecodeEpochInfo failed: %v", err)
	}

	// Verify - note that EpochNumber and BlockNumber are not encoded/decoded
	if len(decoded.Candidates) != len(original.Candidates) {
		t.Errorf("Candidates count mismatch: got %d, want %d", len(decoded.Candidates), len(original.Candidates))
	}

	for i, c := range decoded.Candidates {
		if c.Address != original.Candidates[i].Address {
			t.Errorf("Candidate[%d].Address mismatch", i)
		}
		if c.Diligence != original.Candidates[i].Diligence {
			t.Errorf("Candidate[%d].Diligence mismatch", i)
		}
	}

	if len(decoded.Validators) != len(original.Validators) {
		t.Errorf("Validators count mismatch")
	}

	if len(decoded.BLSPublicKeys) != len(original.BLSPublicKeys) {
		t.Errorf("BLSPublicKeys count mismatch")
	}
}

func TestDecodeEpochInfo_InvalidRLP(t *testing.T) {
	invalidData := []byte{0x01, 0x02, 0x03} // Invalid RLP
	_, err := DecodeEpochInfo(invalidData)
	if err == nil {
		t.Error("expected error for invalid RLP data")
	}
}

func TestDecodeEpochInfo_InvalidCandidateAddressLength(t *testing.T) {
	// Create epoch info with invalid candidate address via RLP encoding
	epochInfoRLP := &EpochInfoRLP{
		Candidates: []*CandidateRLP{
			{
				Addr:      []byte{0x01, 0x02}, // Invalid length (not 20 bytes)
				Diligence: 100,
			},
		},
		Validators:    []uint32{0},
		BLSPublicKeys: [][]byte{make([]byte, 48)},
	}

	data, err := rlp.EncodeToBytes(epochInfoRLP)
	if err != nil {
		t.Fatalf("Failed to encode: %v", err)
	}

	_, err = DecodeEpochInfo(data)
	if err == nil {
		t.Error("expected error for invalid candidate address length")
	}
}
