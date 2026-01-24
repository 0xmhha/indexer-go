package consensus

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsensusData_ParticipationRate(t *testing.T) {
	tests := []struct {
		name          string
		validators    []common.Address
		commitSigners []common.Address
		expectedRate  float64
	}{
		{
			name:          "full participation",
			validators:    generateAddresses(4),
			commitSigners: generateAddresses(4),
			expectedRate:  100.0,
		},
		{
			name:          "75% participation",
			validators:    generateAddresses(4),
			commitSigners: generateAddresses(3),
			expectedRate:  75.0,
		},
		{
			name:          "zero validators",
			validators:    []common.Address{},
			commitSigners: []common.Address{},
			expectedRate:  0.0,
		},
		{
			name:          "50% participation",
			validators:    generateAddresses(10),
			commitSigners: generateAddresses(5),
			expectedRate:  50.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cd := &ConsensusData{
				Validators:    tt.validators,
				CommitSigners: tt.commitSigners,
				CommitCount:   len(tt.commitSigners),
			}

			rate := cd.ParticipationRate()
			assert.Equal(t, tt.expectedRate, rate)
		})
	}
}

func TestConsensusData_IsHealthy(t *testing.T) {
	tests := []struct {
		name          string
		round         uint32
		validators    int
		commitCount   int
		expectHealthy bool
		description   string
	}{
		{
			name:          "healthy - round 0 with quorum",
			round:         0,
			validators:    4,
			commitCount:   3, // 3/4 = 75% >= 67%
			expectHealthy: true,
			description:   "Round 0 and >= 2/3 participation",
		},
		{
			name:          "unhealthy - round change",
			round:         1,
			validators:    4,
			commitCount:   3,
			expectHealthy: false,
			description:   "Round > 0 indicates consensus delay",
		},
		{
			name:          "unhealthy - insufficient quorum",
			round:         0,
			validators:    4,
			commitCount:   2, // 2/4 = 50% < 67%
			expectHealthy: false,
			description:   "Less than 2/3 participation",
		},
		{
			name:          "healthy - exact quorum",
			round:         0,
			validators:    3,
			commitCount:   2,     // 2/3 = 67% (exact minimum)
			expectHealthy: false, // Actually this should be false because (3*2/3)+1 = 3
			description:   "Exactly 2/3 participation",
		},
		{
			name:          "healthy - large validator set",
			round:         0,
			validators:    21,
			commitCount:   15, // 15/21 = 71% >= 67%
			expectHealthy: true,
			description:   "Large validator set with good participation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cd := &ConsensusData{
				Round:       tt.round,
				Validators:  generateAddresses(tt.validators),
				CommitCount: tt.commitCount,
			}

			healthy := cd.IsHealthy()
			assert.Equal(t, tt.expectHealthy, healthy, tt.description)
		})
	}
}

func TestConsensusData_CalculateMissedValidators(t *testing.T) {
	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
		common.HexToAddress("0x4444444444444444444444444444444444444444"),
	}

	tests := []struct {
		name               string
		prepareSigners     []common.Address
		commitSigners      []common.Address
		expectedMissedPrep []common.Address
		expectedMissedComm []common.Address
	}{
		{
			name:               "all participated",
			prepareSigners:     validators,
			commitSigners:      validators,
			expectedMissedPrep: []common.Address{},
			expectedMissedComm: []common.Address{},
		},
		{
			name:               "one missed prepare",
			prepareSigners:     validators[:3],
			commitSigners:      validators,
			expectedMissedPrep: []common.Address{validators[3]},
			expectedMissedComm: []common.Address{},
		},
		{
			name:               "one missed commit",
			prepareSigners:     validators,
			commitSigners:      validators[:3],
			expectedMissedPrep: []common.Address{},
			expectedMissedComm: []common.Address{validators[3]},
		},
		{
			name:               "two missed both",
			prepareSigners:     validators[:2],
			commitSigners:      validators[:2],
			expectedMissedPrep: []common.Address{validators[2], validators[3]},
			expectedMissedComm: []common.Address{validators[2], validators[3]},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cd := &ConsensusData{
				Validators:     validators,
				PrepareSigners: tt.prepareSigners,
				CommitSigners:  tt.commitSigners,
			}

			cd.CalculateMissedValidators()

			assert.ElementsMatch(t, tt.expectedMissedPrep, cd.MissedPrepare)
			assert.ElementsMatch(t, tt.expectedMissedComm, cd.MissedCommit)
		})
	}
}

func TestWBFTExtra_Structure(t *testing.T) {
	// Test that WBFTExtra structure can be properly initialized
	extra := &WBFTExtra{
		VanityData:   make([]byte, 32),
		RandaoReveal: make([]byte, 96),
		PrevRound:    0,
		Round:        0,
		GasTip:       big.NewInt(1000),
		PrevPreparedSeal: &WBFTAggregatedSeal{
			Sealers:   []byte{0xFF, 0xFF},
			Signature: make([]byte, 96),
		},
		PrevCommittedSeal: &WBFTAggregatedSeal{
			Sealers:   []byte{0xFF, 0xFF},
			Signature: make([]byte, 96),
		},
		PreparedSeal: &WBFTAggregatedSeal{
			Sealers:   []byte{0xFF, 0xFF},
			Signature: make([]byte, 96),
		},
		CommittedSeal: &WBFTAggregatedSeal{
			Sealers:   []byte{0xFF, 0xFF},
			Signature: make([]byte, 96),
		},
		EpochInfo: &EpochInfoRaw{
			Candidates: []*CandidateRaw{
				{
					Address:   common.HexToAddress("0x1111111111111111111111111111111111111111"),
					Diligence: 1000000,
				},
			},
			Validators:    []uint32{0, 1, 2, 3},
			BLSPublicKeys: [][]byte{make([]byte, 48), make([]byte, 48)},
		},
	}

	require.NotNil(t, extra)
	assert.Equal(t, 32, len(extra.VanityData))
	assert.Equal(t, 96, len(extra.RandaoReveal))
	assert.Equal(t, uint32(0), extra.Round)
	assert.Equal(t, big.NewInt(1000), extra.GasTip)
	assert.NotNil(t, extra.PreparedSeal)
	assert.NotNil(t, extra.CommittedSeal)
	assert.NotNil(t, extra.EpochInfo)
}

func TestRoundAnalysis_Calculations(t *testing.T) {
	analysis := &RoundAnalysis{
		StartBlock:            100,
		EndBlock:              200,
		TotalBlocks:           100,
		BlocksWithRoundChange: 25,
		RoundDistribution: []RoundDistribution{
			{Round: 0, Count: 75},
			{Round: 1, Count: 20},
			{Round: 2, Count: 5},
		},
	}

	// Verify basic calculations
	assert.Equal(t, uint64(100), analysis.TotalBlocks)
	assert.Equal(t, uint64(25), analysis.BlocksWithRoundChange)

	// Verify distribution sums to total
	var totalCount uint64
	for _, dist := range analysis.RoundDistribution {
		totalCount += dist.Count
	}
	assert.Equal(t, analysis.TotalBlocks, totalCount)

	// Calculate expected percentages
	expectedPercentages := []float64{75.0, 20.0, 5.0}
	for i, dist := range analysis.RoundDistribution {
		expected := expectedPercentages[i]
		assert.InDelta(t, expected, float64(dist.Count)/float64(analysis.TotalBlocks)*100, 0.01)
	}
}

func TestEpochData_Structure(t *testing.T) {
	epochData := &EpochData{
		EpochNumber:    10,
		ValidatorCount: 4,
		Validators: []ValidatorInfo{
			{
				Address:   common.HexToAddress("0x1111111111111111111111111111111111111111"),
				Index:     0,
				BLSPubKey: make([]byte, 48),
			},
			{
				Address:   common.HexToAddress("0x2222222222222222222222222222222222222222"),
				Index:     1,
				BLSPubKey: make([]byte, 48),
			},
		},
		CandidateCount: 2,
		Candidates: []CandidateInfo{
			{
				Address:   common.HexToAddress("0x3333333333333333333333333333333333333333"),
				Diligence: 1500000,
			},
			{
				Address:   common.HexToAddress("0x4444444444444444444444444444444444444444"),
				Diligence: 1200000,
			},
		},
	}

	assert.Equal(t, uint64(10), epochData.EpochNumber)
	assert.Equal(t, 4, epochData.ValidatorCount)
	assert.Equal(t, 2, len(epochData.Validators))
	assert.Equal(t, 2, epochData.CandidateCount)
	assert.Equal(t, uint64(1500000), epochData.Candidates[0].Diligence)
}

func TestBlockParticipation_Fields(t *testing.T) {
	participation := &BlockParticipation{
		BlockNumber:   1000,
		WasProposer:   true,
		SignedPrepare: true,
		SignedCommit:  true,
		Round:         0,
	}

	assert.Equal(t, uint64(1000), participation.BlockNumber)
	assert.True(t, participation.WasProposer)
	assert.True(t, participation.SignedPrepare)
	assert.True(t, participation.SignedCommit)
	assert.Equal(t, uint32(0), participation.Round)
}

// Helper function to generate test addresses
func generateAddresses(count int) []common.Address {
	addresses := make([]common.Address, count)
	for i := 0; i < count; i++ {
		// Create deterministic addresses for testing
		addr := common.Address{}
		addr[0] = byte(i + 1)
		addresses[i] = addr
	}
	return addresses
}
