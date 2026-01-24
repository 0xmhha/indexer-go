package fetch

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	consensustypes "github.com/0xmhha/indexer-go/pkg/types/consensus"
)

func TestWBFTParser_ParseWBFTExtra(t *testing.T) {
	logger := zap.NewNop()
	parser := NewWBFTParser(logger)

	tests := []struct {
		name        string
		setupHeader func() *types.Header
		wantErr     bool
		validate    func(*testing.T, *consensustypes.WBFTExtra)
	}{
		{
			name: "valid WBFT extra data with seals",
			setupHeader: func() *types.Header {
				// Create RLP structure
				wbftRLP := &wbftExtraRLP{
					RandaoReveal: make([]byte, 96),
					PrevRound:    0,
					Round:        0,
					GasTip:       big.NewInt(1000),
					PreparedSeal: &sealRLP{
						Sealers:   []byte{0xFF},
						Signature: make([]byte, 96),
					},
					CommittedSeal: &sealRLP{
						Sealers:   []byte{0xFF},
						Signature: make([]byte, 96),
					},
				}

				// Encode to RLP
				rlpData, _ := rlp.EncodeToBytes(wbftRLP)

				// Create extra data with vanity + RLP
				extraData := make([]byte, WBFTExtraVanity+len(rlpData))
				copy(extraData[:WBFTExtraVanity], make([]byte, WBFTExtraVanity))
				copy(extraData[WBFTExtraVanity:], rlpData)

				return &types.Header{
					Number: big.NewInt(100),
					Extra:  extraData,
				}
			},
			wantErr: false,
			validate: func(t *testing.T, extra *consensustypes.WBFTExtra) {
				assert.NotNil(t, extra)
				assert.Equal(t, uint32(0), extra.Round)
				assert.Equal(t, big.NewInt(1000), extra.GasTip)
				assert.NotNil(t, extra.PreparedSeal)
				assert.NotNil(t, extra.CommittedSeal)
				assert.Equal(t, 96, len(extra.RandaoReveal))
			},
		},
		{
			name: "valid WBFT extra with epoch info",
			setupHeader: func() *types.Header {
				wbftRLP := &wbftExtraRLP{
					RandaoReveal: make([]byte, 96),
					PrevRound:    0,
					Round:        0,
					GasTip:       big.NewInt(1000),
					EpochInfo: &epochInfoRLP{
						Candidates: []*candidateRLP{
							{
								Address:   common.HexToAddress("0x1111111111111111111111111111111111111111"),
								Diligence: 1000000,
							},
							{
								Address:   common.HexToAddress("0x2222222222222222222222222222222222222222"),
								Diligence: 900000,
							},
						},
						Validators:    []uint32{0, 1},
						BLSPublicKeys: [][]byte{make([]byte, 48), make([]byte, 48)},
					},
				}

				rlpData, _ := rlp.EncodeToBytes(wbftRLP)
				extraData := make([]byte, WBFTExtraVanity+len(rlpData))
				copy(extraData[:WBFTExtraVanity], make([]byte, WBFTExtraVanity))
				copy(extraData[WBFTExtraVanity:], rlpData)

				return &types.Header{
					Number: big.NewInt(1000),
					Extra:  extraData,
				}
			},
			wantErr: false,
			validate: func(t *testing.T, extra *consensustypes.WBFTExtra) {
				assert.NotNil(t, extra)
				assert.NotNil(t, extra.EpochInfo)
				assert.Equal(t, 2, len(extra.EpochInfo.Candidates))
				assert.Equal(t, 2, len(extra.EpochInfo.Validators))
				assert.Equal(t, uint64(1000000), extra.EpochInfo.Candidates[0].Diligence)
			},
		},
		{
			name: "invalid - too short extra data",
			setupHeader: func() *types.Header {
				return &types.Header{
					Number: big.NewInt(100),
					Extra:  make([]byte, 16), // Less than WBFTExtraVanity
				}
			},
			wantErr: true,
		},
		{
			name: "invalid - nil header",
			setupHeader: func() *types.Header {
				return nil
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := tt.setupHeader()
			extra, err := parser.ParseWBFTExtra(header)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, extra)
			}
		})
	}
}

func TestWBFTParser_ExtractSignersFromSeal(t *testing.T) {
	logger := zap.NewNop()
	parser := NewWBFTParser(logger)

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
		common.HexToAddress("0x4444444444444444444444444444444444444444"),
		common.HexToAddress("0x5555555555555555555555555555555555555555"),
	}

	tests := []struct {
		name            string
		seal            *consensustypes.WBFTAggregatedSeal
		validators      []common.Address
		expectedCount   int
		expectedSigners []common.Address
	}{
		{
			name: "all validators signed",
			seal: &consensustypes.WBFTAggregatedSeal{
				Sealers:   []byte{0x1F}, // Binary: 00011111 (first 5 bits set)
				Signature: make([]byte, 96),
			},
			validators:      validators,
			expectedCount:   5,
			expectedSigners: validators,
		},
		{
			name: "first 3 validators signed",
			seal: &consensustypes.WBFTAggregatedSeal{
				Sealers:   []byte{0x07}, // Binary: 00000111 (first 3 bits set)
				Signature: make([]byte, 96),
			},
			validators:      validators,
			expectedCount:   3,
			expectedSigners: validators[:3],
		},
		{
			name: "alternating validators signed",
			seal: &consensustypes.WBFTAggregatedSeal{
				Sealers:   []byte{0x15}, // Binary: 00010101 (bits 0, 2, 4 set)
				Signature: make([]byte, 96),
			},
			validators:      validators,
			expectedCount:   3,
			expectedSigners: []common.Address{validators[0], validators[2], validators[4]},
		},
		{
			name:            "nil seal",
			seal:            nil,
			validators:      validators,
			expectedCount:   0,
			expectedSigners: []common.Address{},
		},
		{
			name: "empty validators",
			seal: &consensustypes.WBFTAggregatedSeal{
				Sealers:   []byte{0xFF},
				Signature: make([]byte, 96),
			},
			validators:      []common.Address{},
			expectedCount:   0,
			expectedSigners: []common.Address{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signers, err := parser.ExtractSignersFromSeal(tt.seal, tt.validators)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedCount, len(signers))

			if tt.expectedCount > 0 {
				assert.ElementsMatch(t, tt.expectedSigners, signers)
			}
		})
	}
}

func TestWBFTParser_ParseEpochInfo(t *testing.T) {
	logger := zap.NewNop()
	parser := NewWBFTParser(logger)

	tests := []struct {
		name         string
		header       *types.Header
		epochInfoRaw *consensustypes.EpochInfoRaw
		wantErr      bool
		validate     func(*testing.T, *consensustypes.EpochData)
	}{
		{
			name: "valid epoch info with 4 validators",
			header: &types.Header{
				Number: big.NewInt(1000),
			},
			epochInfoRaw: &consensustypes.EpochInfoRaw{
				Candidates: []*consensustypes.CandidateRaw{
					{
						Address:   common.HexToAddress("0x1111111111111111111111111111111111111111"),
						Diligence: 1500000,
					},
					{
						Address:   common.HexToAddress("0x2222222222222222222222222222222222222222"),
						Diligence: 1400000,
					},
					{
						Address:   common.HexToAddress("0x3333333333333333333333333333333333333333"),
						Diligence: 1300000,
					},
					{
						Address:   common.HexToAddress("0x4444444444444444444444444444444444444444"),
						Diligence: 1200000,
					},
				},
				Validators:    []uint32{0, 1, 2, 3},
				BLSPublicKeys: [][]byte{make([]byte, 48), make([]byte, 48), make([]byte, 48), make([]byte, 48)},
			},
			wantErr: false,
			validate: func(t *testing.T, epochData *consensustypes.EpochData) {
				assert.NotNil(t, epochData)
				assert.Equal(t, 4, epochData.ValidatorCount)
				assert.Equal(t, 4, epochData.CandidateCount)
				assert.Equal(t, 4, len(epochData.Validators))
				assert.Equal(t, 4, len(epochData.Candidates))
				assert.Equal(t, uint64(1500000), epochData.Candidates[0].Diligence)
			},
		},
		{
			name: "nil epoch info raw",
			header: &types.Header{
				Number: big.NewInt(1000),
			},
			epochInfoRaw: nil,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			epochData, err := parser.ParseEpochInfo(tt.header, tt.epochInfoRaw)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, epochData)
			}
		})
	}
}

func TestDecodeSealersFromBitmap(t *testing.T) {
	tests := []struct {
		name            string
		bitmap          []byte
		totalValidators int
		expected        []int
	}{
		{
			name:            "all validators in single byte",
			bitmap:          []byte{0xFF},
			totalValidators: 8,
			expected:        []int{0, 1, 2, 3, 4, 5, 6, 7},
		},
		{
			name:            "first 4 validators",
			bitmap:          []byte{0x0F},
			totalValidators: 8,
			expected:        []int{0, 1, 2, 3},
		},
		{
			name:            "alternating validators",
			bitmap:          []byte{0xAA},
			totalValidators: 8,
			expected:        []int{1, 3, 5, 7},
		},
		{
			name:            "multiple bytes",
			bitmap:          []byte{0xFF, 0x0F},
			totalValidators: 16,
			expected:        []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
		},
		{
			name:            "empty bitmap",
			bitmap:          []byte{0x00},
			totalValidators: 8,
			expected:        []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DecodeSealersFromBitmap(tt.bitmap, tt.totalValidators)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEncodeSealersToBitmap(t *testing.T) {
	tests := []struct {
		name            string
		indices         []int
		totalValidators int
		expected        []byte
	}{
		{
			name:            "all validators",
			indices:         []int{0, 1, 2, 3, 4, 5, 6, 7},
			totalValidators: 8,
			expected:        []byte{0xFF},
		},
		{
			name:            "first 4 validators",
			indices:         []int{0, 1, 2, 3},
			totalValidators: 8,
			expected:        []byte{0x0F},
		},
		{
			name:            "alternating validators",
			indices:         []int{1, 3, 5, 7},
			totalValidators: 8,
			expected:        []byte{0xAA},
		},
		{
			name:            "multiple bytes",
			indices:         []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
			totalValidators: 16,
			expected:        []byte{0xFF, 0x0F},
		},
		{
			name:            "empty indices",
			indices:         []int{},
			totalValidators: 8,
			expected:        []byte{0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EncodeSealersToBitmap(tt.indices, tt.totalValidators)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCompareSeals(t *testing.T) {
	seal1 := &consensustypes.WBFTAggregatedSeal{
		Sealers:   []byte{0xFF},
		Signature: make([]byte, 96),
	}

	seal2 := &consensustypes.WBFTAggregatedSeal{
		Sealers:   []byte{0xFF},
		Signature: make([]byte, 96),
	}

	seal3 := &consensustypes.WBFTAggregatedSeal{
		Sealers:   []byte{0x0F},
		Signature: make([]byte, 96),
	}

	tests := []struct {
		name     string
		seal1    *consensustypes.WBFTAggregatedSeal
		seal2    *consensustypes.WBFTAggregatedSeal
		expected bool
	}{
		{
			name:     "identical seals",
			seal1:    seal1,
			seal2:    seal2,
			expected: true,
		},
		{
			name:     "different seals",
			seal1:    seal1,
			seal2:    seal3,
			expected: false,
		},
		{
			name:     "both nil",
			seal1:    nil,
			seal2:    nil,
			expected: true,
		},
		{
			name:     "one nil",
			seal1:    seal1,
			seal2:    nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareSeals(tt.seal1, tt.seal2)
			assert.Equal(t, tt.expected, result)
		})
	}
}
