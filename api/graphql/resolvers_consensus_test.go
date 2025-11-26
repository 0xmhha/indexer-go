package graphql

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/indexer-go/storage"
	consensustypes "github.com/0xmhha/indexer-go/types/consensus"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/graphql-go/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// setupTestConsensusStorage creates a test consensus storage with sample data
func setupTestConsensusStorage(t *testing.T) (*storage.PebbleStorage, *storage.ConsensusStorage, func()) {
	tmpDir, err := os.MkdirTemp("", "consensus_api_test")
	require.NoError(t, err)

	cfg := &storage.Config{
		Path:                  filepath.Join(tmpDir, "test.db"),
		Cache:                 64,
		CompactionConcurrency: 1,
		MaxOpenFiles:          100,
		WriteBuffer:           64,
	}

	pebbleStorage, err := storage.NewPebbleStorage(cfg)
	require.NoError(t, err)

	logger := zap.NewNop()
	consensusStorage := storage.NewConsensusStorage(pebbleStorage, logger)

	cleanup := func() {
		pebbleStorage.Close()
		os.RemoveAll(tmpDir)
	}

	return pebbleStorage, consensusStorage, cleanup
}

// createTestConsensusData creates sample consensus data for testing
func createTestConsensusData() *consensustypes.ConsensusData {
	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
		common.HexToAddress("0x4444444444444444444444444444444444444444"),
	}

	data := &consensustypes.ConsensusData{
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
		IsEpochBoundary: true,
		EpochInfo: &consensustypes.EpochData{
			EpochNumber:    1,
			ValidatorCount: 4,
			CandidateCount: 4,
			Validators: []consensustypes.ValidatorInfo{
				{Address: validators[0], Index: 0, BLSPubKey: make([]byte, 48)},
				{Address: validators[1], Index: 1, BLSPubKey: make([]byte, 48)},
				{Address: validators[2], Index: 2, BLSPubKey: make([]byte, 48)},
				{Address: validators[3], Index: 3, BLSPubKey: make([]byte, 48)},
			},
			Candidates: []consensustypes.CandidateInfo{
				{Address: validators[0], Diligence: 1000000},
				{Address: validators[1], Diligence: 900000},
				{Address: validators[2], Diligence: 800000},
				{Address: validators[3], Diligence: 700000},
			},
		},
	}

	data.CalculateMissedValidators()
	return data
}

// createBlockFromConsensusData creates a block from consensus data
func createBlockFromConsensusData(data *consensustypes.ConsensusData) *types.Block {
	header := &types.Header{
		Number:   big.NewInt(int64(data.BlockNumber)),
		Coinbase: data.Proposer,
		Time:     data.Timestamp,
	}
	return types.NewBlock(header, nil, nil, nil, nil)
}

func TestResolveConsensusData(t *testing.T) {
	pebbleStorage, consensusStorage, cleanup := setupTestConsensusStorage(t)
	defer cleanup()

	// Create and save test data
	testData := createTestConsensusData()

	// Save a block so GetBlock works
	block := createBlockFromConsensusData(testData)
	err := pebbleStorage.SetBlock(context.Background(), block)
	require.NoError(t, err)

	err = consensusStorage.SaveConsensusData(context.Background(), testData)
	require.NoError(t, err)

	// Create schema
	logger := zap.NewNop()
	schema, err := NewSchema(pebbleStorage, logger)
	require.NoError(t, err)

	// Test query
	query := `
		query {
			consensusData(blockNumber: "100") {
				blockNumber
				blockHash
				round
				prevRound
				roundChanged
				proposer
				validators
				prepareSigners
				commitSigners
				prepareCount
				commitCount
				missedPrepare
				missedCommit
				timestamp
				participationRate
				isHealthy
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema.schema,
		RequestString: query,
		Context:       context.Background(),
	})

	require.Empty(t, result.Errors, "GraphQL query should not have errors")
	require.NotNil(t, result.Data)

	data := result.Data.(map[string]interface{})
	consensusData := data["consensusData"].(map[string]interface{})

	assert.Equal(t, "100", consensusData["blockNumber"])
	assert.Equal(t, testData.BlockHash.Hex(), consensusData["blockHash"])
	assert.Equal(t, 0, consensusData["round"])
	assert.Equal(t, false, consensusData["roundChanged"])
	assert.Equal(t, testData.Proposer.Hex(), consensusData["proposer"])
	assert.Equal(t, 3, consensusData["prepareCount"])
	assert.Equal(t, 3, consensusData["commitCount"])
	assert.Equal(t, true, consensusData["isHealthy"])

	// Check participation rate
	participationRate := consensusData["participationRate"].(float64)
	assert.Greater(t, participationRate, 70.0)
}

func TestResolveValidatorStats(t *testing.T) {
	pebbleStorage, consensusStorage, cleanup := setupTestConsensusStorage(t)
	defer cleanup()

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	}

	// Create multiple blocks
	for i := uint64(100); i <= 110; i++ {
		var commitSigners []common.Address
		if i%3 == 0 {
			commitSigners = validators[1:]
		} else {
			commitSigners = validators
		}

		data := &consensustypes.ConsensusData{
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

		data.CalculateMissedValidators()
		err := consensusStorage.SaveConsensusData(context.Background(), data)
		require.NoError(t, err)
	}

	// Create schema
	logger := zap.NewNop()
	schema, err := NewSchema(pebbleStorage, logger)
	require.NoError(t, err)

	// Test query for first validator
	query := `
		query {
			validatorStats(
				address: "0x1111111111111111111111111111111111111111"
				fromBlock: "100"
				toBlock: "110"
			) {
				address
				totalBlocks
				commitsSigned
				commitsMissed
				participationRate
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema.schema,
		RequestString: query,
		Context:       context.Background(),
	})

	require.Empty(t, result.Errors, "GraphQL query should not have errors")
	require.NotNil(t, result.Data)

	data := result.Data.(map[string]interface{})
	stats := data["validatorStats"].(map[string]interface{})

	assert.Equal(t, validators[0].Hex(), stats["address"])
	assert.Equal(t, "11", stats["totalBlocks"])
	assert.Equal(t, "8", stats["commitsSigned"])
	assert.Equal(t, "3", stats["commitsMissed"])

	participationRate := stats["participationRate"].(float64)
	assert.Greater(t, participationRate, 70.0)
	assert.Less(t, participationRate, 75.0)
}

func TestResolveValidatorParticipation(t *testing.T) {
	pebbleStorage, consensusStorage, cleanup := setupTestConsensusStorage(t)
	defer cleanup()

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
	}

	// Create blocks
	for i := uint64(100); i <= 105; i++ {
		data := &consensustypes.ConsensusData{
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

		data.CalculateMissedValidators()
		err := consensusStorage.SaveConsensusData(context.Background(), data)
		require.NoError(t, err)
	}

	// Create schema
	logger := zap.NewNop()
	schema, err := NewSchema(pebbleStorage, logger)
	require.NoError(t, err)

	// Test query
	query := `
		query {
			validatorParticipation(
				address: "0x1111111111111111111111111111111111111111"
				fromBlock: "100"
				toBlock: "105"
			) {
				address
				startBlock
				endBlock
				totalBlocks
				blocksCommitted
				blocksMissed
				participationRate
				blocks {
					blockNumber
					signedPrepare
					signedCommit
					round
				}
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema.schema,
		RequestString: query,
		Context:       context.Background(),
	})

	require.Empty(t, result.Errors, "GraphQL query should not have errors")
	require.NotNil(t, result.Data)

	data := result.Data.(map[string]interface{})
	participation := data["validatorParticipation"].(map[string]interface{})

	assert.Equal(t, validators[0].Hex(), participation["address"])
	assert.Equal(t, "100", participation["startBlock"])
	assert.Equal(t, "105", participation["endBlock"])
	assert.Equal(t, "6", participation["totalBlocks"])
	assert.Equal(t, "6", participation["blocksCommitted"])
	assert.Equal(t, "0", participation["blocksMissed"])
	assert.Equal(t, 100.0, participation["participationRate"])

	blocks := participation["blocks"].([]interface{})
	assert.Equal(t, 6, len(blocks))

	// Check first block
	firstBlock := blocks[0].(map[string]interface{})
	assert.Equal(t, true, firstBlock["signedPrepare"])
	assert.Equal(t, true, firstBlock["signedCommit"])
	assert.Equal(t, 0, firstBlock["round"])
}

func TestResolveAllValidatorStats(t *testing.T) {
	pebbleStorage, consensusStorage, cleanup := setupTestConsensusStorage(t)
	defer cleanup()

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	}

	// Create blocks
	for i := uint64(100); i <= 105; i++ {
		data := &consensustypes.ConsensusData{
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

		data.CalculateMissedValidators()
		err := consensusStorage.SaveConsensusData(context.Background(), data)
		require.NoError(t, err)
	}

	// Create schema
	logger := zap.NewNop()
	schema, err := NewSchema(pebbleStorage, logger)
	require.NoError(t, err)

	// Test query
	query := `
		query {
			allValidatorStats(
				fromBlock: "100"
				toBlock: "105"
			) {
				address
				totalBlocks
				commitsSigned
				participationRate
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema.schema,
		RequestString: query,
		Context:       context.Background(),
	})

	require.Empty(t, result.Errors, "GraphQL query should not have errors")
	require.NotNil(t, result.Data)

	data := result.Data.(map[string]interface{})
	statsList := data["allValidatorStats"].([]interface{})

	assert.Equal(t, 3, len(statsList))

	// Check that all validators have 100% participation
	for _, item := range statsList {
		stats := item.(map[string]interface{})
		assert.Equal(t, "6", stats["totalBlocks"])
		assert.Equal(t, "6", stats["commitsSigned"])
		assert.Equal(t, 100.0, stats["participationRate"])
	}
}

func TestResolveEpochData(t *testing.T) {
	pebbleStorage, consensusStorage, cleanup := setupTestConsensusStorage(t)
	defer cleanup()

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
	}

	// Create consensus data with epoch info
	data := &consensustypes.ConsensusData{
		BlockNumber:     1000,
		BlockHash:       common.HexToHash("0xddee11"),
		Round:           0,
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

	err := consensusStorage.SaveConsensusData(context.Background(), data)
	require.NoError(t, err)

	// Create schema
	logger := zap.NewNop()
	schema, err := NewSchema(pebbleStorage, logger)
	require.NoError(t, err)

	// Test query
	query := `
		query {
			epochData(epochNumber: "1") {
				epochNumber
				validatorCount
				candidateCount
				validators {
					address
					index
					blsPubKey
				}
				candidates {
					address
					diligence
				}
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema.schema,
		RequestString: query,
		Context:       context.Background(),
	})

	require.Empty(t, result.Errors, "GraphQL query should not have errors")
	require.NotNil(t, result.Data)

	responseData := result.Data.(map[string]interface{})
	epochData := responseData["epochData"].(map[string]interface{})

	assert.Equal(t, "1", epochData["epochNumber"])
	assert.Equal(t, 2, epochData["validatorCount"])
	assert.Equal(t, 3, epochData["candidateCount"])

	validatorList := epochData["validators"].([]interface{})
	assert.Equal(t, 2, len(validatorList))

	candidateList := epochData["candidates"].([]interface{})
	assert.Equal(t, 3, len(candidateList))

	// Check first validator
	firstValidator := validatorList[0].(map[string]interface{})
	assert.Equal(t, validators[0].Hex(), firstValidator["address"])
	assert.Equal(t, 0, firstValidator["index"])

	// Check first candidate
	firstCandidate := candidateList[0].(map[string]interface{})
	assert.Equal(t, validators[0].Hex(), firstCandidate["address"])
	assert.Equal(t, "1000000", firstCandidate["diligence"])
}

func TestResolveLatestEpochData(t *testing.T) {
	pebbleStorage, consensusStorage, cleanup := setupTestConsensusStorage(t)
	defer cleanup()

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
	}

	// Save epoch 1
	data1 := &consensustypes.ConsensusData{
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
	err := consensusStorage.SaveConsensusData(context.Background(), data1)
	require.NoError(t, err)

	// Save epoch 2
	data2 := &consensustypes.ConsensusData{
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
	err = consensusStorage.SaveConsensusData(context.Background(), data2)
	require.NoError(t, err)

	// Create schema
	logger := zap.NewNop()
	schema, err := NewSchema(pebbleStorage, logger)
	require.NoError(t, err)

	// Test query
	query := `
		query {
			latestEpochData {
				epochNumber
				validatorCount
				candidates {
					address
					diligence
				}
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema.schema,
		RequestString: query,
		Context:       context.Background(),
	})

	require.Empty(t, result.Errors, "GraphQL query should not have errors")
	require.NotNil(t, result.Data)

	responseData := result.Data.(map[string]interface{})
	epochData := responseData["latestEpochData"].(map[string]interface{})

	// Should be epoch 2
	assert.Equal(t, "2", epochData["epochNumber"])
	assert.Equal(t, 2, epochData["validatorCount"])

	candidateList := epochData["candidates"].([]interface{})
	firstCandidate := candidateList[0].(map[string]interface{})
	assert.Equal(t, "1100000", firstCandidate["diligence"])
}
