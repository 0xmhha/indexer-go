package storage

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	consensustypes "github.com/0xmhha/indexer-go/pkg/types/consensus"
)

func TestConsensusStorage_SaveAndGetConsensusData(t *testing.T) {
	// Create temporary directory for test database
	tmpDir, err := os.MkdirTemp("", "consensus_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create storage
	cfg := &Config{
		Path:                  filepath.Join(tmpDir, "test.db"),
		Cache:                 64,
		CompactionConcurrency: 1,
		MaxOpenFiles:          100,
		WriteBuffer:           64,
	}
	pebbleStorage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer pebbleStorage.Close()

	logger := zap.NewNop()
	cs := NewConsensusStorage(pebbleStorage, logger)

	// Create test consensus data
	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
		common.HexToAddress("0x4444444444444444444444444444444444444444"),
	}

	consensusData := &consensustypes.ConsensusData{
		BlockNumber:    100,
		BlockHash:      common.HexToHash("0xaabbcc"),
		Round:          0,
		PrevRound:      0,
		RoundChanged:   false,
		Proposer:       validators[0],
		Validators:     validators,
		PrepareSigners: validators[:3],
		CommitSigners:  validators[:3],
		PrepareCount:   3,
		CommitCount:    3,
		RandaoReveal:   make([]byte, 96),
		GasTip:         big.NewInt(1000),
		Timestamp:      1000000,
	}

	// Calculate missed validators
	consensusData.CalculateMissedValidators()

	// Create and save block (required by GetConsensusData)
	block := createTestBlockWithMiner(100, consensusData.Proposer, 100000, consensusData.Timestamp)
	err = pebbleStorage.SetBlock(context.Background(), block)
	require.NoError(t, err)

	// Save consensus data
	err = cs.SaveConsensusData(context.Background(), consensusData)
	require.NoError(t, err)

	// Retrieve consensus data
	retrieved, err := cs.GetConsensusData(context.Background(), 100)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Verify data
	assert.Equal(t, consensusData.BlockNumber, retrieved.BlockNumber)
	assert.Equal(t, consensusData.BlockHash, retrieved.BlockHash)
	assert.Equal(t, consensusData.Round, retrieved.Round)
	assert.Equal(t, 3, retrieved.CommitCount)
	assert.Equal(t, consensusData.GasTip, retrieved.GasTip)
}

func TestConsensusStorage_SaveConsensusDataWithEpoch(t *testing.T) {
	// Create temporary directory for test database
	tmpDir, err := os.MkdirTemp("", "consensus_epoch_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create storage
	cfg := &Config{
		Path:                  filepath.Join(tmpDir, "test.db"),
		Cache:                 64,
		CompactionConcurrency: 1,
		MaxOpenFiles:          100,
		WriteBuffer:           64,
	}
	pebbleStorage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer pebbleStorage.Close()

	logger := zap.NewNop()
	cs := NewConsensusStorage(pebbleStorage, logger)

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
	}

	// Create consensus data with epoch info
	consensusData := &consensustypes.ConsensusData{
		BlockNumber:     1000,
		BlockHash:       common.HexToHash("0xddee11"),
		Round:           0,
		PrevRound:       0,
		RoundChanged:    false,
		Proposer:        validators[0],
		Validators:      validators,
		PrepareSigners:  validators,
		CommitSigners:   validators,
		PrepareCount:    2,
		CommitCount:     2,
		Timestamp:       2000000,
		IsEpochBoundary: true,
		EpochInfo: &consensustypes.EpochData{
			EpochNumber:    1,
			ValidatorCount: 2,
			CandidateCount: 3,
			Validators: []consensustypes.ValidatorInfo{
				{
					Address:   validators[0],
					Index:     0,
					BLSPubKey: make([]byte, 48),
				},
				{
					Address:   validators[1],
					Index:     1,
					BLSPubKey: make([]byte, 48),
				},
			},
			Candidates: []consensustypes.CandidateInfo{
				{Address: validators[0], Diligence: 1000000},
				{Address: validators[1], Diligence: 900000},
				{Address: common.HexToAddress("0x3333333333333333333333333333333333333333"), Diligence: 800000},
			},
		},
	}

	// Save consensus data with epoch info
	err = cs.SaveConsensusData(context.Background(), consensusData)
	require.NoError(t, err)

	// Retrieve epoch info
	epochData, err := cs.GetEpochInfo(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, epochData)

	assert.Equal(t, uint64(1), epochData.EpochNumber)
	assert.Equal(t, 2, epochData.ValidatorCount)
	assert.Equal(t, 3, epochData.CandidateCount)
	assert.Equal(t, 2, len(epochData.Validators))
	assert.Equal(t, 3, len(epochData.Candidates))

	// Verify validator info
	assert.Equal(t, validators[0], epochData.Validators[0].Address)
	assert.Equal(t, uint32(0), epochData.Validators[0].Index)

	// Verify candidate info
	assert.Equal(t, uint64(1000000), epochData.Candidates[0].Diligence)
}

func TestConsensusStorage_GetValidatorStats(t *testing.T) {
	// Create temporary directory for test database
	tmpDir, err := os.MkdirTemp("", "validator_stats_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create storage
	cfg := &Config{
		Path:                  filepath.Join(tmpDir, "test.db"),
		Cache:                 64,
		CompactionConcurrency: 1,
		MaxOpenFiles:          100,
		WriteBuffer:           64,
	}
	pebbleStorage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer pebbleStorage.Close()

	logger := zap.NewNop()
	cs := NewConsensusStorage(pebbleStorage, logger)

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	}

	// Save multiple blocks with different participation
	for i := uint64(100); i <= 110; i++ {
		var commitSigners []common.Address
		if i%3 == 0 {
			// First validator misses every 3rd block
			commitSigners = validators[1:]
		} else {
			commitSigners = validators
		}

		consensusData := &consensustypes.ConsensusData{
			BlockNumber:    i,
			BlockHash:      common.HexToHash("0xaa"),
			Round:          0,
			Proposer:       validators[0],
			Validators:     validators,
			PrepareSigners: commitSigners,
			CommitSigners:  commitSigners,
			PrepareCount:   len(commitSigners),
			CommitCount:    len(commitSigners),
			Timestamp:      1000000 + i,
		}

		consensusData.CalculateMissedValidators()
		err = cs.SaveConsensusData(context.Background(), consensusData)
		require.NoError(t, err)
	}

	// Get stats for first validator
	stats, err := cs.GetValidatorStats(context.Background(), validators[0], 100, 110)
	require.NoError(t, err)
	require.NotNil(t, stats)

	assert.Equal(t, validators[0], stats.Address)
	assert.Equal(t, uint64(11), stats.TotalBlocks)
	// Validator 0 should miss blocks 102, 105, 108 (3 blocks)
	assert.Equal(t, uint64(8), stats.CommitsSigned)
	assert.Equal(t, uint64(3), stats.CommitsMissed)
}

func TestConsensusStorage_GetValidatorParticipation(t *testing.T) {
	// Create temporary directory for test database
	tmpDir, err := os.MkdirTemp("", "validator_participation_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create storage
	cfg := &Config{
		Path:                  filepath.Join(tmpDir, "test.db"),
		Cache:                 64,
		CompactionConcurrency: 1,
		MaxOpenFiles:          100,
		WriteBuffer:           64,
	}
	pebbleStorage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer pebbleStorage.Close()

	logger := zap.NewNop()
	cs := NewConsensusStorage(pebbleStorage, logger)

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
	}

	// Save blocks
	for i := uint64(100); i <= 105; i++ {
		consensusData := &consensustypes.ConsensusData{
			BlockNumber:    i,
			BlockHash:      common.HexToHash("0xbb"),
			Round:          0,
			Proposer:       validators[0],
			Validators:     validators,
			PrepareSigners: validators,
			CommitSigners:  validators,
			PrepareCount:   2,
			CommitCount:    2,
			Timestamp:      1000000 + i,
		}

		consensusData.CalculateMissedValidators()
		err = cs.SaveConsensusData(context.Background(), consensusData)
		require.NoError(t, err)
	}

	// Get participation for first validator
	participation, err := cs.GetValidatorParticipation(context.Background(), validators[0], 100, 105, 100, 0)
	require.NoError(t, err)
	require.NotNil(t, participation)

	assert.Equal(t, validators[0], participation.Address)
	assert.Equal(t, uint64(100), participation.StartBlock)
	assert.Equal(t, uint64(105), participation.EndBlock)
	assert.Equal(t, uint64(6), participation.TotalBlocks)
	assert.Equal(t, uint64(6), participation.BlocksCommitted)
	assert.Equal(t, uint64(0), participation.BlocksMissed)
	assert.Equal(t, 100.0, participation.ParticipationRate)
	assert.Equal(t, 6, len(participation.Blocks))
}

func TestConsensusStorage_GetAllValidatorStats(t *testing.T) {
	// Create temporary directory for test database
	tmpDir, err := os.MkdirTemp("", "all_validators_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create storage
	cfg := &Config{
		Path:                  filepath.Join(tmpDir, "test.db"),
		Cache:                 64,
		CompactionConcurrency: 1,
		MaxOpenFiles:          100,
		WriteBuffer:           64,
	}
	pebbleStorage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer pebbleStorage.Close()

	logger := zap.NewNop()
	cs := NewConsensusStorage(pebbleStorage, logger)

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	}

	// Save blocks
	for i := uint64(100); i <= 105; i++ {
		consensusData := &consensustypes.ConsensusData{
			BlockNumber:    i,
			BlockHash:      common.HexToHash("0xcc"),
			Round:          0,
			Proposer:       validators[0],
			Validators:     validators,
			PrepareSigners: validators,
			CommitSigners:  validators,
			PrepareCount:   3,
			CommitCount:    3,
			Timestamp:      1000000 + i,
		}

		consensusData.CalculateMissedValidators()
		err = cs.SaveConsensusData(context.Background(), consensusData)
		require.NoError(t, err)
	}

	// Get stats for all validators
	statsMap, err := cs.GetAllValidatorStats(context.Background(), 100, 105, 100, 0)
	require.NoError(t, err)
	require.NotNil(t, statsMap)

	assert.Equal(t, 3, len(statsMap))

	// Check each validator's stats
	for _, validator := range validators {
		stats, exists := statsMap[validator]
		assert.True(t, exists)
		assert.Equal(t, uint64(6), stats.TotalBlocks)
		assert.Equal(t, uint64(6), stats.CommitsSigned)
		assert.Equal(t, 100.0, stats.ParticipationRate)
	}
}

func TestConsensusStorage_GetLatestEpochInfo(t *testing.T) {
	// Create temporary directory for test database
	tmpDir, err := os.MkdirTemp("", "latest_epoch_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create storage
	cfg := &Config{
		Path:                  filepath.Join(tmpDir, "test.db"),
		Cache:                 64,
		CompactionConcurrency: 1,
		MaxOpenFiles:          100,
		WriteBuffer:           64,
	}
	pebbleStorage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer pebbleStorage.Close()

	logger := zap.NewNop()
	cs := NewConsensusStorage(pebbleStorage, logger)

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
	}

	// Save epoch 1
	consensusData1 := &consensustypes.ConsensusData{
		BlockNumber:     1000,
		BlockHash:       common.HexToHash("0xeee"),
		IsEpochBoundary: true,
		EpochInfo: &consensustypes.EpochData{
			EpochNumber:    1,
			ValidatorCount: 2,
			CandidateCount: 2,
			Validators: []consensustypes.ValidatorInfo{
				{Address: validators[0], Index: 0, BLSPubKey: make([]byte, 48)},
				{Address: validators[1], Index: 1, BLSPubKey: make([]byte, 48)},
			},
			Candidates: []consensustypes.CandidateInfo{
				{Address: validators[0], Diligence: 1000000},
				{Address: validators[1], Diligence: 900000},
			},
		},
		Validators:     validators,
		PrepareSigners: validators,
		CommitSigners:  validators,
		Timestamp:      2000000,
	}
	err = cs.SaveConsensusData(context.Background(), consensusData1)
	require.NoError(t, err)

	// Save epoch 2
	consensusData2 := &consensustypes.ConsensusData{
		BlockNumber:     2000,
		BlockHash:       common.HexToHash("0xfff"),
		IsEpochBoundary: true,
		EpochInfo: &consensustypes.EpochData{
			EpochNumber:    2,
			ValidatorCount: 2,
			CandidateCount: 2,
			Validators: []consensustypes.ValidatorInfo{
				{Address: validators[0], Index: 0, BLSPubKey: make([]byte, 48)},
				{Address: validators[1], Index: 1, BLSPubKey: make([]byte, 48)},
			},
			Candidates: []consensustypes.CandidateInfo{
				{Address: validators[0], Diligence: 1100000},
				{Address: validators[1], Diligence: 950000},
			},
		},
		Validators:     validators,
		PrepareSigners: validators,
		CommitSigners:  validators,
		Timestamp:      3000000,
	}
	err = cs.SaveConsensusData(context.Background(), consensusData2)
	require.NoError(t, err)

	// Get latest epoch info
	latestEpoch, err := cs.GetLatestEpochInfo(context.Background())
	require.NoError(t, err)
	require.NotNil(t, latestEpoch)

	// Should be epoch 2
	assert.Equal(t, uint64(2), latestEpoch.EpochNumber)
	assert.Equal(t, 2, latestEpoch.ValidatorCount)
	assert.Equal(t, uint64(1100000), latestEpoch.Candidates[0].Diligence)
}
