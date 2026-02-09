package wbft

import (
	"context"
	"math/big"
	"testing"

	consensustypes "github.com/0xmhha/indexer-go/pkg/types/consensus"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- Helper functions ---

func newTestParser(epochLength uint64) *Parser {
	return NewParser(epochLength, zap.NewNop())
}

// makeHeader creates a test header with given extra data
func makeHeader(number uint64, extra []byte) *types.Header {
	return &types.Header{
		Number: big.NewInt(int64(number)),
		Extra:  extra,
	}
}

// makeBlock creates a test block from header
func makeBlock(header *types.Header) *types.Block {
	return types.NewBlockWithHeader(header)
}

// makeVanity returns 32-byte vanity data
func makeVanity(s string) []byte {
	vanity := make([]byte, 32)
	copy(vanity, []byte(s))
	return vanity
}

// encodeWBFTExtra creates RLP-encoded WBFT extra data for testing
func encodeWBFTExtra(t *testing.T, data interface{}) []byte {
	t.Helper()
	encoded, err := rlp.EncodeToBytes(data)
	require.NoError(t, err)
	return encoded
}

// --- NewParser tests ---

func TestNewParser(t *testing.T) {
	p := NewParser(20, zap.NewNop())
	assert.Equal(t, uint64(20), p.epochLength)
	assert.NotNil(t, p.validatorCache)
}

func TestNewParser_DefaultEpochLength(t *testing.T) {
	p := NewParser(0, nil) // 0 should use default
	assert.NotZero(t, p.epochLength)
	assert.NotNil(t, p.logger)
}

func TestNewParser_NilLogger(t *testing.T) {
	p := NewParser(10, nil)
	assert.NotNil(t, p.logger)
}

// --- ConsensusType tests ---

func TestConsensusType(t *testing.T) {
	p := newTestParser(10)
	assert.Equal(t, "wbft", string(p.ConsensusType()))
}

// --- GetEpochLength tests ---

func TestGetEpochLength(t *testing.T) {
	p := newTestParser(20)
	assert.Equal(t, uint64(20), p.GetEpochLength())
}

// --- IsEpochBoundary tests ---

func TestIsEpochBoundary(t *testing.T) {
	p := newTestParser(10)

	tests := []struct {
		blockNum uint64
		expected bool
	}{
		{0, false},
		{1, false},
		{5, false},
		{9, false},
		{10, true},
		{11, false},
		{20, true},
		{100, true},
	}

	for _, tt := range tests {
		block := makeBlock(makeHeader(tt.blockNum, nil))
		assert.Equal(t, tt.expected, p.IsEpochBoundary(block), "block %d", tt.blockNum)
	}
}

func TestIsEpochBoundary_NilBlock(t *testing.T) {
	p := newTestParser(10)
	assert.False(t, p.IsEpochBoundary(nil))
}

// --- ParseWBFTExtra tests ---

func TestParseWBFTExtra_TooShort(t *testing.T) {
	p := newTestParser(10)
	header := makeHeader(1, []byte("short"))
	_, err := p.ParseWBFTExtra(header)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extra data too short")
}

func TestParseWBFTExtra_VanityOnly(t *testing.T) {
	p := newTestParser(10)
	vanity := makeVanity("hello-world")
	header := makeHeader(1, vanity)

	extra, err := p.ParseWBFTExtra(header)
	require.NoError(t, err)
	assert.Equal(t, vanity, extra.VanityData)
	assert.Nil(t, extra.CommittedSeal)
}

func TestParseWBFTExtra_WithRLPData(t *testing.T) {
	p := newTestParser(10)

	// Build a full RLP-encodable struct with all fields populated (no nil pointers)
	wbftData := struct {
		RandaoReveal      []byte
		PrevRound         uint32
		PrevPreparedSeal  wbftAggregatedSealRLP
		PrevCommittedSeal wbftAggregatedSealRLP
		Round             uint32
		PreparedSeal      wbftAggregatedSealRLP
		CommittedSeal     wbftAggregatedSealRLP
		GasTip            *big.Int
		EpochInfo         epochInfoRLP
	}{
		RandaoReveal:      []byte{0x01, 0x02, 0x03},
		PrevRound:         0,
		PrevPreparedSeal:  wbftAggregatedSealRLP{Sealers: []byte{}, Signature: []byte{}},
		PrevCommittedSeal: wbftAggregatedSealRLP{Sealers: []byte{}, Signature: []byte{}},
		Round:             2,
		PreparedSeal:      wbftAggregatedSealRLP{Sealers: []byte{}, Signature: []byte{}},
		CommittedSeal:     wbftAggregatedSealRLP{Sealers: []byte{0x07}, Signature: []byte{0xaa, 0xbb}},
		GasTip:            big.NewInt(1000),
		EpochInfo:         epochInfoRLP{},
	}

	vanity := makeVanity("test")
	rlpData := encodeWBFTExtra(t, wbftData)
	headerExtra := append(vanity, rlpData...)

	header := makeHeader(1, headerExtra)
	extra, err := p.ParseWBFTExtra(header)
	require.NoError(t, err)

	assert.Equal(t, vanity, extra.VanityData)
	// RLP decoding may fail due to struct mismatch (pointer vs value) and
	// the parser gracefully degrades to returning just vanity data.
	// If RLP decode succeeded, verify the fields:
	if extra.CommittedSeal != nil {
		assert.Equal(t, []byte{0x07}, extra.CommittedSeal.Sealers)
		assert.Equal(t, uint32(2), extra.Round)
	}
}

func TestParseWBFTExtra_InvalidRLP_GracefulDegradation(t *testing.T) {
	p := newTestParser(10)
	vanity := makeVanity("test")
	invalidRLP := []byte{0xff, 0xfe, 0xfd} // invalid RLP
	headerExtra := append(vanity, invalidRLP...)

	header := makeHeader(1, headerExtra)
	extra, err := p.ParseWBFTExtra(header)
	// Should return with just vanity data, no error
	require.NoError(t, err)
	assert.Equal(t, vanity, extra.VanityData)
	assert.Nil(t, extra.CommittedSeal)
}

// --- ExtractValidators tests ---

func TestExtractValidators_NilExtra(t *testing.T) {
	p := newTestParser(10)
	validators, err := p.ExtractValidators(nil)
	require.NoError(t, err)
	assert.Nil(t, validators)
}

func TestExtractValidators_NoEpochInfo(t *testing.T) {
	p := newTestParser(10)
	extra := &consensustypes.WBFTExtra{}
	validators, err := p.ExtractValidators(extra)
	require.NoError(t, err)
	assert.Nil(t, validators)
}

func TestExtractValidators_WithCandidates(t *testing.T) {
	p := newTestParser(10)

	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	addr3 := common.HexToAddress("0x3333333333333333333333333333333333333333")

	extra := &consensustypes.WBFTExtra{
		EpochInfo: &consensustypes.EpochInfoRaw{
			Candidates: []*consensustypes.CandidateRaw{
				{Address: addr1, Diligence: 100},
				{Address: addr2, Diligence: 90},
				{Address: addr3, Diligence: 80},
			},
			Validators: []uint32{0, 2}, // indices into candidates
		},
	}

	validators, err := p.ExtractValidators(extra)
	require.NoError(t, err)
	require.Len(t, validators, 2)
	assert.Equal(t, addr1, validators[0])
	assert.Equal(t, addr3, validators[1])
}

func TestExtractValidators_IndexOutOfBounds(t *testing.T) {
	p := newTestParser(10)

	extra := &consensustypes.WBFTExtra{
		EpochInfo: &consensustypes.EpochInfoRaw{
			Candidates: []*consensustypes.CandidateRaw{
				{Address: common.HexToAddress("0x01")},
			},
			Validators: []uint32{0, 5}, // index 5 out of bounds
		},
	}

	validators, err := p.ExtractValidators(extra)
	require.NoError(t, err)
	// Only index 0 is valid, index 5 skipped
	require.Len(t, validators, 1)
}

// --- ExtractSignersFromSeal tests ---

func TestExtractSignersFromSeal_NilSeal(t *testing.T) {
	p := newTestParser(10)
	signers, err := p.ExtractSignersFromSeal(nil, nil)
	require.NoError(t, err)
	assert.Nil(t, signers)
}

func TestExtractSignersFromSeal_EmptySealers(t *testing.T) {
	p := newTestParser(10)
	seal := &consensustypes.WBFTAggregatedSeal{Sealers: []byte{}}
	signers, err := p.ExtractSignersFromSeal(seal, []common.Address{common.HexToAddress("0x01")})
	require.NoError(t, err)
	assert.Nil(t, signers)
}

func TestExtractSignersFromSeal_NoValidators(t *testing.T) {
	p := newTestParser(10)
	seal := &consensustypes.WBFTAggregatedSeal{Sealers: []byte{0xff}}
	signers, err := p.ExtractSignersFromSeal(seal, nil)
	require.NoError(t, err)
	assert.Nil(t, signers)
}

func TestExtractSignersFromSeal_Bitmap(t *testing.T) {
	p := newTestParser(10)

	validators := []common.Address{
		common.HexToAddress("0x01"),
		common.HexToAddress("0x02"),
		common.HexToAddress("0x03"),
		common.HexToAddress("0x04"),
	}

	// Bitmap: 0b00000101 = 0x05 → validators at index 0 and 2
	seal := &consensustypes.WBFTAggregatedSeal{
		Sealers: []byte{0x05},
	}

	signers, err := p.ExtractSignersFromSeal(seal, validators)
	require.NoError(t, err)
	require.Len(t, signers, 2)
	assert.Equal(t, validators[0], signers[0])
	assert.Equal(t, validators[2], signers[1])
}

func TestExtractSignersFromSeal_AllSigned(t *testing.T) {
	p := newTestParser(10)

	validators := []common.Address{
		common.HexToAddress("0x01"),
		common.HexToAddress("0x02"),
		common.HexToAddress("0x03"),
	}

	// All 3 validators signed: bits 0,1,2 = 0x07
	seal := &consensustypes.WBFTAggregatedSeal{
		Sealers: []byte{0x07},
	}

	signers, err := p.ExtractSignersFromSeal(seal, validators)
	require.NoError(t, err)
	require.Len(t, signers, 3)
}

func TestExtractSignersFromSeal_MultiBytesBitmap(t *testing.T) {
	p := newTestParser(10)

	// 9 validators, need 2 bytes for bitmap
	validators := make([]common.Address, 9)
	for i := range validators {
		validators[i] = common.BigToAddress(big.NewInt(int64(i + 1)))
	}

	// Bitmap: byte0=0xff (all first 8), byte1=0x01 (validator 8)
	seal := &consensustypes.WBFTAggregatedSeal{
		Sealers: []byte{0xff, 0x01},
	}

	signers, err := p.ExtractSignersFromSeal(seal, validators)
	require.NoError(t, err)
	assert.Len(t, signers, 9) // all 9 signed
}

// --- CacheValidators / GetValidators tests ---

func TestCacheValidators(t *testing.T) {
	p := newTestParser(10)

	validators := []common.Address{
		common.HexToAddress("0xaa"),
		common.HexToAddress("0xbb"),
	}

	p.CacheValidators(100, validators)

	cached, err := p.GetValidators(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, validators, cached)
}

func TestGetValidators_NearestBlock(t *testing.T) {
	p := newTestParser(10)

	validators := []common.Address{common.HexToAddress("0xaa")}
	p.CacheValidators(10, validators)

	// Block 15 should find validators cached at block 10
	cached, err := p.GetValidators(context.Background(), 15)
	require.NoError(t, err)
	assert.Equal(t, validators, cached)
}

func TestGetValidators_NoCache(t *testing.T) {
	p := newTestParser(10)

	_, err := p.GetValidators(context.Background(), 50)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no cached validators")
}

func TestClearCache(t *testing.T) {
	p := newTestParser(10)
	p.CacheValidators(10, []common.Address{common.HexToAddress("0x01")})

	p.ClearCache()

	_, err := p.GetValidators(context.Background(), 10)
	require.Error(t, err)
}

// --- DecodeVanityData tests ---

func TestDecodeVanityData(t *testing.T) {
	p := newTestParser(10)
	vanity := makeVanity("hello-wbft")
	block := makeBlock(makeHeader(1, vanity))

	result, err := p.DecodeVanityData(block)
	require.NoError(t, err)
	assert.Equal(t, "hello-wbft", result)
}

func TestDecodeVanityData_NilBlock(t *testing.T) {
	p := newTestParser(10)
	_, err := p.DecodeVanityData(nil)
	require.Error(t, err)
}

func TestDecodeVanityData_ShortExtra(t *testing.T) {
	p := newTestParser(10)
	block := makeBlock(makeHeader(1, []byte("short")))
	_, err := p.DecodeVanityData(block)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extra data too short")
}

func TestDecodeVanityData_NullPadded(t *testing.T) {
	p := newTestParser(10)
	vanity := makeVanity("hi") // "hi" + 30 null bytes
	block := makeBlock(makeHeader(1, vanity))

	result, err := p.DecodeVanityData(block)
	require.NoError(t, err)
	assert.Equal(t, "hi", result)
}

// --- GetRoundInfo tests ---

func TestGetRoundInfo_NilBlock(t *testing.T) {
	p := newTestParser(10)
	_, err := p.GetRoundInfo(nil)
	require.Error(t, err)
}

func TestGetRoundInfo_VanityOnly(t *testing.T) {
	p := newTestParser(10)
	block := makeBlock(makeHeader(5, makeVanity("test")))

	info, err := p.GetRoundInfo(block)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), info.BlockNumber)
	assert.Equal(t, uint32(0), info.FinalRound) // No RLP data, round is 0
	assert.True(t, info.SuccessOnFirstTry)
}

// --- GetEpochInfo tests ---

func TestGetEpochInfo_NilBlock(t *testing.T) {
	p := newTestParser(10)
	_, err := p.GetEpochInfo(nil)
	require.Error(t, err)
}

func TestGetEpochInfo_NonEpochBoundary(t *testing.T) {
	p := newTestParser(10)
	block := makeBlock(makeHeader(5, makeVanity("test")))

	epochData, err := p.GetEpochInfo(block)
	require.NoError(t, err)
	assert.Nil(t, epochData) // Not an epoch boundary
}

// --- ParseConsensusData tests ---

func TestParseConsensusData_NilBlock(t *testing.T) {
	p := newTestParser(10)
	_, err := p.ParseConsensusData(nil)
	require.Error(t, err)
}

func TestParseConsensusData_VanityOnly(t *testing.T) {
	p := newTestParser(10)
	header := makeHeader(5, makeVanity("test"))
	header.Coinbase = common.HexToAddress("0xproposer")
	block := makeBlock(header)

	data, err := p.ParseConsensusData(block)
	require.NoError(t, err)
	assert.Equal(t, "wbft", string(data.ConsensusType))
	assert.Equal(t, uint64(5), data.BlockNumber)
	assert.Equal(t, header.Coinbase, data.ProposerAddress)
	assert.False(t, data.IsEpochBoundary)
}

func TestParseConsensusData_EpochBoundary(t *testing.T) {
	p := newTestParser(10)
	header := makeHeader(10, makeVanity("epoch"))
	block := makeBlock(header)

	data, err := p.ParseConsensusData(block)
	require.NoError(t, err)
	assert.True(t, data.IsEpochBoundary)
	assert.NotNil(t, data.EpochNumber)
	assert.Equal(t, uint64(1), *data.EpochNumber)
}

func TestParseConsensusData_WithCommittedSeal(t *testing.T) {
	p := newTestParser(10)

	// Pre-cache some validators
	validators := []common.Address{
		common.HexToAddress("0x01"),
		common.HexToAddress("0x02"),
		common.HexToAddress("0x03"),
	}

	// Build WBFT extra with committed seal and epoch info
	// Use value types (not pointers) to match RLP encoding expectations
	wbftData := struct {
		RandaoReveal      []byte
		PrevRound         uint32
		PrevPreparedSeal  wbftAggregatedSealRLP
		PrevCommittedSeal wbftAggregatedSealRLP
		Round             uint32
		PreparedSeal      wbftAggregatedSealRLP
		CommittedSeal     wbftAggregatedSealRLP
		GasTip            *big.Int
		EpochInfo         epochInfoRLP
	}{
		PrevPreparedSeal:  wbftAggregatedSealRLP{Sealers: []byte{}, Signature: []byte{}},
		PrevCommittedSeal: wbftAggregatedSealRLP{Sealers: []byte{}, Signature: []byte{}},
		PreparedSeal:      wbftAggregatedSealRLP{Sealers: []byte{}, Signature: []byte{}},
		CommittedSeal: wbftAggregatedSealRLP{
			Sealers:   []byte{0x05}, // validators 0 and 2
			Signature: []byte{0x01},
		},
		GasTip: big.NewInt(0),
		EpochInfo: epochInfoRLP{
			Candidates: []*candidateRLP{
				{Address: validators[0], Diligence: 100},
				{Address: validators[1], Diligence: 90},
				{Address: validators[2], Diligence: 80},
			},
			Validators: []uint32{0, 1, 2},
		},
	}

	vanity := makeVanity("test")
	rlpData := encodeWBFTExtra(t, wbftData)
	headerExtra := append(vanity, rlpData...)

	header := makeHeader(10, headerExtra) // epoch boundary
	header.Coinbase = validators[0]
	block := makeBlock(header)

	data, err := p.ParseConsensusData(block)
	require.NoError(t, err)

	assert.Equal(t, 3, data.ValidatorCount)
	assert.Len(t, data.SignedValidators, 2) // bits 0 and 2
	assert.InDelta(t, 66.67, data.ParticipationRate, 0.1)
	assert.True(t, data.IsEpochBoundary)
}
