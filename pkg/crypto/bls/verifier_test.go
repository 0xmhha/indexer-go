package bls

import (
	"math/big"
	"testing"

	consensustypes "github.com/0xmhha/indexer-go/pkg/types/consensus"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewVerifier(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		v := NewVerifier()
		require.NotNil(t, v)
		assert.InDelta(t, 66.67, v.minParticipationPct, 0.01)
		assert.False(t, v.skipVerification)
	})

	t.Run("with custom options", func(t *testing.T) {
		logger := zap.NewNop()
		v := NewVerifier(
			WithLogger(logger),
			WithMinParticipation(50.0),
			WithSkipVerification(true),
		)
		require.NotNil(t, v)
		assert.Equal(t, logger, v.logger)
		assert.Equal(t, 50.0, v.minParticipationPct)
		assert.True(t, v.skipVerification)
	})
}

func TestVerifySeal(t *testing.T) {
	header := &types.Header{
		Number:     big.NewInt(100),
		ParentHash: common.HexToHash("0x1234"),
		Coinbase:   common.HexToAddress("0x5678"),
		Extra:      make([]byte, 64), // 32 vanity + some data
	}

	validators := []consensustypes.ValidatorInfo{
		{Address: common.HexToAddress("0x1111"), Index: 0},
		{Address: common.HexToAddress("0x2222"), Index: 1},
		{Address: common.HexToAddress("0x3333"), Index: 2},
	}

	t.Run("nil seal returns error", func(t *testing.T) {
		v := NewVerifier()
		result := v.VerifySeal(header, nil, validators, 0)
		assert.Error(t, result.Error)
		assert.False(t, result.Valid)
	})

	t.Run("invalid signature length", func(t *testing.T) {
		v := NewVerifier()
		seal := &consensustypes.WBFTAggregatedSeal{
			Signature: make([]byte, 48), // Wrong length
			Sealers:   []byte{0x07},     // All 3 validators
		}
		result := v.VerifySeal(header, seal, validators, 0)
		assert.Error(t, result.Error)
		assert.Contains(t, result.Error.Error(), "invalid seal signature length")
	})

	t.Run("no validators returns error", func(t *testing.T) {
		v := NewVerifier()
		seal := &consensustypes.WBFTAggregatedSeal{
			Signature: make([]byte, SignatureLength),
			Sealers:   []byte{0x07},
		}
		result := v.VerifySeal(header, seal, []consensustypes.ValidatorInfo{}, 0)
		assert.ErrorIs(t, result.Error, ErrNoValidators)
	})

	t.Run("empty sealers returns error", func(t *testing.T) {
		v := NewVerifier()
		seal := &consensustypes.WBFTAggregatedSeal{
			Signature: make([]byte, SignatureLength),
			Sealers:   []byte{0x00}, // No signers
		}
		result := v.VerifySeal(header, seal, validators, 0)
		assert.ErrorIs(t, result.Error, ErrNoSigners)
	})

	t.Run("insufficient signers for quorum", func(t *testing.T) {
		v := NewVerifier(WithMinParticipation(66.67))
		seal := &consensustypes.WBFTAggregatedSeal{
			Signature: make([]byte, SignatureLength),
			Sealers:   []byte{0x01}, // Only 1 of 3 validators (33%)
		}
		result := v.VerifySeal(header, seal, validators, 0)
		assert.ErrorIs(t, result.Error, ErrInsufficientSigners)
		assert.False(t, result.HasQuorum)
	})

	t.Run("with skip verification - valid with quorum", func(t *testing.T) {
		v := NewVerifier(WithSkipVerification(true))
		seal := &consensustypes.WBFTAggregatedSeal{
			Signature: make([]byte, SignatureLength),
			Sealers:   []byte{0x07}, // All 3 validators signed
		}
		result := v.VerifySeal(header, seal, validators, 0)
		assert.NoError(t, result.Error)
		assert.True(t, result.Valid)
		assert.True(t, result.HasQuorum)
		assert.Equal(t, 3, result.SignerCount)
		assert.InDelta(t, 100.0, result.ParticipationPct, 0.01)
	})

	t.Run("with skip verification - 2 of 3 signers (66.67%)", func(t *testing.T) {
		v := NewVerifier(WithSkipVerification(true), WithMinParticipation(66.0))
		seal := &consensustypes.WBFTAggregatedSeal{
			Signature: make([]byte, SignatureLength),
			Sealers:   []byte{0x03}, // Validators 0 and 1 signed
		}
		result := v.VerifySeal(header, seal, validators, 0)
		assert.NoError(t, result.Error)
		assert.True(t, result.Valid)
		assert.True(t, result.HasQuorum)
		assert.Equal(t, 2, result.SignerCount)
		assert.InDelta(t, 66.67, result.ParticipationPct, 0.01)
	})

	t.Run("full verification requires BLS keys", func(t *testing.T) {
		v := NewVerifier() // skipVerification is false
		seal := &consensustypes.WBFTAggregatedSeal{
			Signature: make([]byte, SignatureLength),
			Sealers:   []byte{0x07}, // All validators signed
		}
		// Validators without BLS keys
		result := v.VerifySeal(header, seal, validators, 0)
		assert.Error(t, result.Error)
		assert.Contains(t, result.Error.Error(), "BLS")
	})
}

func TestExtractSignersFromBitmap(t *testing.T) {
	v := NewVerifier()

	validators := []consensustypes.ValidatorInfo{
		{Address: common.HexToAddress("0x1111"), Index: 0},
		{Address: common.HexToAddress("0x2222"), Index: 1},
		{Address: common.HexToAddress("0x3333"), Index: 2},
		{Address: common.HexToAddress("0x4444"), Index: 3},
		{Address: common.HexToAddress("0x5555"), Index: 4},
		{Address: common.HexToAddress("0x6666"), Index: 5},
		{Address: common.HexToAddress("0x7777"), Index: 6},
		{Address: common.HexToAddress("0x8888"), Index: 7},
		{Address: common.HexToAddress("0x9999"), Index: 8},
	}

	tests := []struct {
		name           string
		bitmap         []byte
		validators     []consensustypes.ValidatorInfo
		expectedCount  int
		expectedFirst  common.Address
		expectedLast   common.Address
		expectedIndices []int
	}{
		{
			name:           "all 8 validators in first byte",
			bitmap:         []byte{0xFF}, // 11111111
			validators:     validators[:8],
			expectedCount:  8,
			expectedFirst:  common.HexToAddress("0x1111"),
			expectedLast:   common.HexToAddress("0x8888"),
			expectedIndices: []int{0, 1, 2, 3, 4, 5, 6, 7},
		},
		{
			name:           "first validator only",
			bitmap:         []byte{0x01}, // 00000001
			validators:     validators[:8],
			expectedCount:  1,
			expectedFirst:  common.HexToAddress("0x1111"),
			expectedLast:   common.HexToAddress("0x1111"),
			expectedIndices: []int{0},
		},
		{
			name:           "last validator only",
			bitmap:         []byte{0x80}, // 10000000
			validators:     validators[:8],
			expectedCount:  1,
			expectedFirst:  common.HexToAddress("0x8888"),
			expectedLast:   common.HexToAddress("0x8888"),
			expectedIndices: []int{7},
		},
		{
			name:           "alternating validators",
			bitmap:         []byte{0x55}, // 01010101
			validators:     validators[:8],
			expectedCount:  4,
			expectedFirst:  common.HexToAddress("0x1111"),
			expectedLast:   common.HexToAddress("0x7777"),
			expectedIndices: []int{0, 2, 4, 6},
		},
		{
			name:           "9th validator in second byte",
			bitmap:         []byte{0x00, 0x01}, // 00000000 00000001
			validators:     validators,
			expectedCount:  1,
			expectedFirst:  common.HexToAddress("0x9999"),
			expectedLast:   common.HexToAddress("0x9999"),
			expectedIndices: []int{8},
		},
		{
			name:           "empty bitmap",
			bitmap:         []byte{0x00},
			validators:     validators[:8],
			expectedCount:  0,
			expectedIndices: []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signers, indices := v.extractSignersFromBitmap(tt.bitmap, tt.validators)
			assert.Equal(t, tt.expectedCount, len(signers))
			assert.Equal(t, tt.expectedIndices, indices)

			if tt.expectedCount > 0 {
				assert.Equal(t, tt.expectedFirst, signers[0])
				assert.Equal(t, tt.expectedLast, signers[len(signers)-1])
			}
		})
	}
}

func TestCheckParticipation(t *testing.T) {
	v := NewVerifier(WithMinParticipation(66.67))

	t.Run("nil seal returns zero", func(t *testing.T) {
		count, pct, quorum := v.CheckParticipation(nil, 10)
		assert.Equal(t, 0, count)
		assert.Equal(t, 0.0, pct)
		assert.False(t, quorum)
	})

	t.Run("zero validators returns zero", func(t *testing.T) {
		seal := &consensustypes.WBFTAggregatedSeal{
			Sealers: []byte{0xFF},
		}
		count, pct, quorum := v.CheckParticipation(seal, 0)
		assert.Equal(t, 0, count)
		assert.Equal(t, 0.0, pct)
		assert.False(t, quorum)
	})

	t.Run("full participation has quorum", func(t *testing.T) {
		seal := &consensustypes.WBFTAggregatedSeal{
			Sealers: []byte{0x0F}, // 4 validators signed
		}
		count, pct, quorum := v.CheckParticipation(seal, 4)
		assert.Equal(t, 4, count)
		assert.InDelta(t, 100.0, pct, 0.01)
		assert.True(t, quorum)
	})

	t.Run("2/3 participation has quorum", func(t *testing.T) {
		seal := &consensustypes.WBFTAggregatedSeal{
			Sealers: []byte{0x07}, // 3 validators signed
		}
		count, pct, quorum := v.CheckParticipation(seal, 4)
		assert.Equal(t, 3, count)
		assert.InDelta(t, 75.0, pct, 0.01)
		assert.True(t, quorum)
	})

	t.Run("1/3 participation no quorum", func(t *testing.T) {
		seal := &consensustypes.WBFTAggregatedSeal{
			Sealers: []byte{0x01}, // 1 validator signed
		}
		count, pct, quorum := v.CheckParticipation(seal, 3)
		assert.Equal(t, 1, count)
		assert.InDelta(t, 33.33, pct, 0.01)
		assert.False(t, quorum)
	})
}

func TestVerifyCommittedSeal(t *testing.T) {
	header := &types.Header{
		Number: big.NewInt(100),
		Extra:  make([]byte, 64),
	}

	validators := []consensustypes.ValidatorInfo{
		{Address: common.HexToAddress("0x1111"), Index: 0},
		{Address: common.HexToAddress("0x2222"), Index: 1},
		{Address: common.HexToAddress("0x3333"), Index: 2},
	}

	t.Run("nil wbft extra returns error", func(t *testing.T) {
		v := NewVerifier()
		result := v.VerifyCommittedSeal(header, nil, validators)
		assert.Error(t, result.Error)
		assert.Contains(t, result.Error.Error(), "no committed seal")
	})

	t.Run("nil committed seal returns error", func(t *testing.T) {
		v := NewVerifier()
		extra := &consensustypes.WBFTExtra{
			CommittedSeal: nil,
		}
		result := v.VerifyCommittedSeal(header, extra, validators)
		assert.Error(t, result.Error)
	})

	t.Run("valid committed seal with skip verification", func(t *testing.T) {
		v := NewVerifier(WithSkipVerification(true))
		extra := &consensustypes.WBFTExtra{
			Round: 0,
			CommittedSeal: &consensustypes.WBFTAggregatedSeal{
				Signature: make([]byte, SignatureLength),
				Sealers:   []byte{0x07}, // All validators
			},
		}
		result := v.VerifyCommittedSeal(header, extra, validators)
		assert.NoError(t, result.Error)
		assert.True(t, result.Valid)
	})
}
