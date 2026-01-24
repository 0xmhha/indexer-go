package consensus

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatorStats_CalculateParticipationRate(t *testing.T) {
	tests := []struct {
		name          string
		totalBlocks   uint64
		commitsSigned uint64
		expectedRate  float64
	}{
		{
			name:          "100% participation",
			totalBlocks:   100,
			commitsSigned: 100,
			expectedRate:  100.0,
		},
		{
			name:          "75% participation",
			totalBlocks:   100,
			commitsSigned: 75,
			expectedRate:  75.0,
		},
		{
			name:          "50% participation",
			totalBlocks:   200,
			commitsSigned: 100,
			expectedRate:  50.0,
		},
		{
			name:          "zero blocks",
			totalBlocks:   0,
			commitsSigned: 0,
			expectedRate:  0.0,
		},
		{
			name:          "partial participation",
			totalBlocks:   150,
			commitsSigned: 120,
			expectedRate:  80.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vs := &ValidatorStats{
				TotalBlocks:   tt.totalBlocks,
				CommitsSigned: tt.commitsSigned,
			}

			vs.CalculateParticipationRate()
			assert.Equal(t, tt.expectedRate, vs.ParticipationRate)
		})
	}
}

func TestValidatorStats_UpdateWithBlock(t *testing.T) {
	validatorAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	otherAddr := common.HexToAddress("0x2222222222222222222222222222222222222222")

	tests := []struct {
		name          string
		initialStats  *ValidatorStats
		consensusData *ConsensusData
		expectedStats *ValidatorStats
	}{
		{
			name: "validator was proposer and signed all",
			initialStats: &ValidatorStats{
				Address:     validatorAddr,
				TotalBlocks: 10,
			},
			consensusData: &ConsensusData{
				BlockNumber:    100,
				Proposer:       validatorAddr,
				PrepareSigners: []common.Address{validatorAddr, otherAddr},
				CommitSigners:  []common.Address{validatorAddr, otherAddr},
				MissedPrepare:  []common.Address{},
				MissedCommit:   []common.Address{},
			},
			expectedStats: &ValidatorStats{
				Address:            validatorAddr,
				TotalBlocks:        11,
				BlocksProposed:     1,
				PreparesSigned:     1,
				CommitsSigned:      1,
				PreparesMissed:     0,
				CommitsMissed:      0,
				LastProposedBlock:  100,
				LastCommittedBlock: 100,
				LastSeenBlock:      100,
			},
		},
		{
			name: "validator not proposer but signed",
			initialStats: &ValidatorStats{
				Address:     validatorAddr,
				TotalBlocks: 20,
			},
			consensusData: &ConsensusData{
				BlockNumber:    200,
				Proposer:       otherAddr,
				PrepareSigners: []common.Address{validatorAddr, otherAddr},
				CommitSigners:  []common.Address{validatorAddr, otherAddr},
				MissedPrepare:  []common.Address{},
				MissedCommit:   []common.Address{},
			},
			expectedStats: &ValidatorStats{
				Address:            validatorAddr,
				TotalBlocks:        21,
				BlocksProposed:     0,
				PreparesSigned:     1,
				CommitsSigned:      1,
				PreparesMissed:     0,
				CommitsMissed:      0,
				LastCommittedBlock: 200,
				LastSeenBlock:      200,
			},
		},
		{
			name: "validator missed prepare and commit",
			initialStats: &ValidatorStats{
				Address:     validatorAddr,
				TotalBlocks: 30,
			},
			consensusData: &ConsensusData{
				BlockNumber:    300,
				Proposer:       otherAddr,
				PrepareSigners: []common.Address{otherAddr},
				CommitSigners:  []common.Address{otherAddr},
				MissedPrepare:  []common.Address{validatorAddr},
				MissedCommit:   []common.Address{validatorAddr},
			},
			expectedStats: &ValidatorStats{
				Address:        validatorAddr,
				TotalBlocks:    31,
				BlocksProposed: 0,
				PreparesSigned: 0,
				CommitsSigned:  0,
				PreparesMissed: 1,
				CommitsMissed:  1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vs := tt.initialStats
			vs.UpdateWithBlock(tt.consensusData, validatorAddr)

			assert.Equal(t, tt.expectedStats.TotalBlocks, vs.TotalBlocks)
			assert.Equal(t, tt.expectedStats.BlocksProposed, vs.BlocksProposed)
			assert.Equal(t, tt.expectedStats.PreparesSigned, vs.PreparesSigned)
			assert.Equal(t, tt.expectedStats.CommitsSigned, vs.CommitsSigned)
			assert.Equal(t, tt.expectedStats.PreparesMissed, vs.PreparesMissed)
			assert.Equal(t, tt.expectedStats.CommitsMissed, vs.CommitsMissed)

			if tt.expectedStats.LastProposedBlock > 0 {
				assert.Equal(t, tt.expectedStats.LastProposedBlock, vs.LastProposedBlock)
			}
			if tt.expectedStats.LastCommittedBlock > 0 {
				assert.Equal(t, tt.expectedStats.LastCommittedBlock, vs.LastCommittedBlock)
			}
			if tt.expectedStats.LastSeenBlock > 0 {
				assert.Equal(t, tt.expectedStats.LastSeenBlock, vs.LastSeenBlock)
			}
		})
	}
}

func TestValidatorSet_IsValidator(t *testing.T) {
	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	}

	vset := &ValidatorSet{
		BlockNumber: 100,
		Validators:  validators,
		Count:       len(validators),
	}

	tests := []struct {
		name     string
		address  common.Address
		expected bool
	}{
		{
			name:     "first validator",
			address:  validators[0],
			expected: true,
		},
		{
			name:     "middle validator",
			address:  validators[1],
			expected: true,
		},
		{
			name:     "last validator",
			address:  validators[2],
			expected: true,
		},
		{
			name:     "not a validator",
			address:  common.HexToAddress("0x9999999999999999999999999999999999999999"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := vset.IsValidator(tt.address)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidatorSet_AddValidator(t *testing.T) {
	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
	}

	vset := &ValidatorSet{
		BlockNumber: 100,
		Validators:  validators,
		Count:       len(validators),
	}

	newValidator := common.HexToAddress("0x3333333333333333333333333333333333333333")

	// Test adding new validator
	vset.AddValidator(newValidator)
	assert.Equal(t, 3, vset.Count)
	assert.True(t, vset.IsValidator(newValidator))

	// Test adding duplicate validator (should not increase count)
	vset.AddValidator(newValidator)
	assert.Equal(t, 3, vset.Count)
}

func TestValidatorSet_RemoveValidator(t *testing.T) {
	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	}

	vset := &ValidatorSet{
		BlockNumber: 100,
		Validators:  make([]common.Address, len(validators)),
		Count:       len(validators),
	}
	copy(vset.Validators, validators)

	// Test removing existing validator
	vset.RemoveValidator(validators[1])
	assert.Equal(t, 2, vset.Count)
	assert.False(t, vset.IsValidator(validators[1]))
	assert.True(t, vset.IsValidator(validators[0]))
	assert.True(t, vset.IsValidator(validators[2]))

	// Test removing non-existent validator (should not change count)
	nonExistent := common.HexToAddress("0x9999999999999999999999999999999999999999")
	vset.RemoveValidator(nonExistent)
	assert.Equal(t, 2, vset.Count)
}

func TestValidatorChange_CalculateChanges(t *testing.T) {
	tests := []struct {
		name               string
		previousValidators []common.Address
		newValidators      []common.Address
		expectedAdded      []common.Address
		expectedRemoved    []common.Address
	}{
		{
			name: "one added, one removed",
			previousValidators: []common.Address{
				common.HexToAddress("0x1111111111111111111111111111111111111111"),
				common.HexToAddress("0x2222222222222222222222222222222222222222"),
				common.HexToAddress("0x3333333333333333333333333333333333333333"),
			},
			newValidators: []common.Address{
				common.HexToAddress("0x1111111111111111111111111111111111111111"),
				common.HexToAddress("0x2222222222222222222222222222222222222222"),
				common.HexToAddress("0x4444444444444444444444444444444444444444"),
			},
			expectedAdded: []common.Address{
				common.HexToAddress("0x4444444444444444444444444444444444444444"),
			},
			expectedRemoved: []common.Address{
				common.HexToAddress("0x3333333333333333333333333333333333333333"),
			},
		},
		{
			name: "all validators changed",
			previousValidators: []common.Address{
				common.HexToAddress("0x1111111111111111111111111111111111111111"),
				common.HexToAddress("0x2222222222222222222222222222222222222222"),
			},
			newValidators: []common.Address{
				common.HexToAddress("0x3333333333333333333333333333333333333333"),
				common.HexToAddress("0x4444444444444444444444444444444444444444"),
			},
			expectedAdded: []common.Address{
				common.HexToAddress("0x3333333333333333333333333333333333333333"),
				common.HexToAddress("0x4444444444444444444444444444444444444444"),
			},
			expectedRemoved: []common.Address{
				common.HexToAddress("0x1111111111111111111111111111111111111111"),
				common.HexToAddress("0x2222222222222222222222222222222222222222"),
			},
		},
		{
			name: "no changes",
			previousValidators: []common.Address{
				common.HexToAddress("0x1111111111111111111111111111111111111111"),
				common.HexToAddress("0x2222222222222222222222222222222222222222"),
			},
			newValidators: []common.Address{
				common.HexToAddress("0x1111111111111111111111111111111111111111"),
				common.HexToAddress("0x2222222222222222222222222222222222222222"),
			},
			expectedAdded:   []common.Address{},
			expectedRemoved: []common.Address{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vc := &ValidatorChange{
				EpochNumber:        10,
				BlockNumber:        1000,
				PreviousValidators: tt.previousValidators,
				NewValidators:      tt.newValidators,
			}

			vc.CalculateChanges()

			assert.ElementsMatch(t, tt.expectedAdded, vc.AddedValidators)
			assert.ElementsMatch(t, tt.expectedRemoved, vc.RemovedValidators)
		})
	}
}

func TestValidatorParticipation_Structure(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	participation := &ValidatorParticipation{
		Address:           addr,
		StartBlock:        100,
		EndBlock:          200,
		TotalBlocks:       100,
		BlocksProposed:    10,
		BlocksCommitted:   90,
		BlocksMissed:      10,
		ParticipationRate: 90.0,
		Blocks: []BlockParticipation{
			{
				BlockNumber:   100,
				WasProposer:   true,
				SignedPrepare: true,
				SignedCommit:  true,
				Round:         0,
			},
		},
	}

	require.NotNil(t, participation)
	assert.Equal(t, addr, participation.Address)
	assert.Equal(t, uint64(100), participation.StartBlock)
	assert.Equal(t, uint64(200), participation.EndBlock)
	assert.Equal(t, uint64(100), participation.TotalBlocks)
	assert.Equal(t, uint64(10), participation.BlocksProposed)
	assert.Equal(t, uint64(90), participation.BlocksCommitted)
	assert.Equal(t, uint64(10), participation.BlocksMissed)
	assert.Equal(t, 90.0, participation.ParticipationRate)
	assert.Equal(t, 1, len(participation.Blocks))
}

func TestValidatorActivity_Fields(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	activity := &ValidatorActivity{
		Address:       addr,
		IsActive:      true,
		IsOnline:      true,
		LastSeen:      1000,
		BlocksAgo:     5,
		CurrentStreak: 100,
	}

	assert.Equal(t, addr, activity.Address)
	assert.True(t, activity.IsActive)
	assert.True(t, activity.IsOnline)
	assert.Equal(t, uint64(1000), activity.LastSeen)
	assert.Equal(t, uint64(5), activity.BlocksAgo)
	assert.Equal(t, uint64(100), activity.CurrentStreak)
}

func TestMinerStats_Structure(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	stats := &MinerStats{
		Address:     addr,
		BlockCount:  50,
		StartBlock:  100,
		EndBlock:    200,
		Percentage:  50.0,
		IsValidator: true,
	}

	assert.Equal(t, addr, stats.Address)
	assert.Equal(t, uint64(50), stats.BlockCount)
	assert.Equal(t, uint64(100), stats.StartBlock)
	assert.Equal(t, uint64(200), stats.EndBlock)
	assert.Equal(t, 50.0, stats.Percentage)
	assert.True(t, stats.IsValidator)
}
