package fetch

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	consensustypes "github.com/0xmhha/indexer-go/types/consensus"
)

// Helper function to create a test block with WBFT extra data
func createTestBlock(blockNumber uint64, round uint32, validators []common.Address) *types.Block {
	// Create WBFT RLP structure
	wbftRLP := &wbftExtraRLP{
		RandaoReveal: make([]byte, 96),
		PrevRound:    0,
		Round:        round,
		GasTip:       big.NewInt(1000),
		PreparedSeal: &sealRLP{
			Sealers:   EncodeSealersToBitmap([]int{0, 1, 2}, len(validators)),
			Signature: make([]byte, 96),
		},
		CommittedSeal: &sealRLP{
			Sealers:   EncodeSealersToBitmap([]int{0, 1, 2}, len(validators)),
			Signature: make([]byte, 96),
		},
	}

	// If we have validators, add epoch info
	if len(validators) > 0 {
		candidates := make([]*candidateRLP, len(validators))
		validatorIndices := make([]uint32, len(validators))
		blsKeys := make([][]byte, len(validators))

		for i, addr := range validators {
			candidates[i] = &candidateRLP{
				Address:   addr,
				Diligence: 1000000 - uint64(i*100000),
			}
			validatorIndices[i] = uint32(i)
			blsKeys[i] = make([]byte, 48)
		}

		wbftRLP.EpochInfo = &epochInfoRLP{
			Candidates:    candidates,
			Validators:    validatorIndices,
			BLSPublicKeys: blsKeys,
		}
	}

	// Encode to RLP
	rlpData, _ := rlp.EncodeToBytes(wbftRLP)

	// Create extra data
	extraData := make([]byte, WBFTExtraVanity+len(rlpData))
	copy(extraData[:WBFTExtraVanity], make([]byte, WBFTExtraVanity))
	copy(extraData[WBFTExtraVanity:], rlpData)

	// Create header
	header := &types.Header{
		Number:   big.NewInt(int64(blockNumber)),
		Coinbase: validators[0], // First validator is proposer
		Extra:    extraData,
		Time:     1000000 + blockNumber,
	}

	return types.NewBlock(header, nil, nil, nil, nil)
}

func TestConsensusFetcher_GetConsensusData(t *testing.T) {
	logger := zap.NewNop()
	client := newMockClient()
	fetcher := NewConsensusFetcher(client, logger)

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
		common.HexToAddress("0x4444444444444444444444444444444444444444"),
	}

	tests := []struct {
		name        string
		blockNumber uint64
		setupBlock  func() *types.Block
		wantErr     bool
		validate    func(*testing.T, *consensustypes.ConsensusData)
	}{
		{
			name:        "successful extraction - round 0",
			blockNumber: 100,
			setupBlock: func() *types.Block {
				return createTestBlock(100, 0, validators)
			},
			wantErr: false,
			validate: func(t *testing.T, cd *consensustypes.ConsensusData) {
				assert.Equal(t, uint64(100), cd.BlockNumber)
				assert.Equal(t, uint32(0), cd.Round)
				assert.False(t, cd.RoundChanged)
				assert.Equal(t, validators[0], cd.Proposer)
				assert.Equal(t, 4, len(cd.Validators))
				assert.Equal(t, 3, cd.CommitCount)
				assert.True(t, cd.IsEpochBoundary)
			},
		},
		{
			name:        "successful extraction - round 1",
			blockNumber: 200,
			setupBlock: func() *types.Block {
				return createTestBlock(200, 1, validators)
			},
			wantErr: false,
			validate: func(t *testing.T, cd *consensustypes.ConsensusData) {
				assert.Equal(t, uint64(200), cd.BlockNumber)
				assert.Equal(t, uint32(1), cd.Round)
				assert.True(t, cd.RoundChanged)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock block
			block := tt.setupBlock()
			client.blocks[tt.blockNumber] = block

			// Get consensus data
			cd, err := fetcher.GetConsensusData(context.Background(), tt.blockNumber)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cd)

			if tt.validate != nil {
				tt.validate(t, cd)
			}
		})
	}
}

func TestConsensusFetcher_ExtractConsensusData(t *testing.T) {
	logger := zap.NewNop()
	client := newMockClient()
	fetcher := NewConsensusFetcher(client, logger)

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	}

	block := createTestBlock(100, 0, validators)

	cd, err := fetcher.ExtractConsensusData(block.Header())

	require.NoError(t, err)
	require.NotNil(t, cd)

	assert.Equal(t, uint64(100), cd.BlockNumber)
	assert.Equal(t, validators[0], cd.Proposer)
	assert.Equal(t, 3, len(cd.Validators))
	assert.Equal(t, 3, cd.CommitCount)
	assert.NotNil(t, cd.EpochInfo)
}

func TestConsensusFetcher_GetValidatorsAtBlock(t *testing.T) {
	logger := zap.NewNop()
	client := newMockClient()
	fetcher := NewConsensusFetcher(client, logger)

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
		common.HexToAddress("0x4444444444444444444444444444444444444444"),
	}

	block := createTestBlock(100, 0, validators)
	client.blocks[100] = block

	result, err := fetcher.GetValidatorsAtBlock(context.Background(), 100)

	require.NoError(t, err)
	assert.Equal(t, 4, len(result))
	assert.ElementsMatch(t, validators, result)
}

func TestConsensusFetcher_GetValidatorParticipation(t *testing.T) {
	logger := zap.NewNop()
	client := newMockClient()
	fetcher := NewConsensusFetcher(client, logger)

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	}

	// Setup multiple blocks
	for i := uint64(100); i <= 105; i++ {
		client.blocks[i] = createTestBlock(i, 0, validators)
	}

	// Get participation for first validator
	participation, err := fetcher.GetValidatorParticipation(
		context.Background(),
		validators[0],
		100,
		105,
	)

	require.NoError(t, err)
	require.NotNil(t, participation)

	assert.Equal(t, validators[0], participation.Address)
	assert.Equal(t, uint64(100), participation.StartBlock)
	assert.Equal(t, uint64(105), participation.EndBlock)
	assert.Equal(t, uint64(6), participation.TotalBlocks)
	assert.Equal(t, 6, len(participation.Blocks))

	// First validator is proposer in all blocks
	assert.Equal(t, uint64(6), participation.BlocksProposed)
}

func TestConsensusFetcher_AnalyzeRoundChanges(t *testing.T) {
	logger := zap.NewNop()
	client := newMockClient()
	fetcher := NewConsensusFetcher(client, logger)

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	}

	// Setup blocks with different rounds
	client.blocks[100] = createTestBlock(100, 0, validators) // Round 0
	client.blocks[101] = createTestBlock(101, 0, validators) // Round 0
	client.blocks[102] = createTestBlock(102, 1, validators) // Round 1
	client.blocks[103] = createTestBlock(103, 0, validators) // Round 0
	client.blocks[104] = createTestBlock(104, 2, validators) // Round 2

	analysis, err := fetcher.AnalyzeRoundChanges(context.Background(), 100, 104)

	require.NoError(t, err)
	require.NotNil(t, analysis)

	assert.Equal(t, uint64(100), analysis.StartBlock)
	assert.Equal(t, uint64(104), analysis.EndBlock)
	assert.Equal(t, uint64(5), analysis.TotalBlocks)
	assert.Equal(t, uint64(2), analysis.BlocksWithRoundChange) // Blocks 102 and 104
	assert.Equal(t, uint32(2), analysis.MaxRound)
	assert.Equal(t, float64(40.0), analysis.RoundChangeRate) // 2/5 = 40%

	// Check round distribution
	assert.Equal(t, 3, len(analysis.RoundDistribution))
}

func TestConsensusFetcher_ParseEpochInfo(t *testing.T) {
	logger := zap.NewNop()
	client := newMockClient()
	fetcher := NewConsensusFetcher(client, logger)

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	}

	block := createTestBlock(1000, 0, validators)

	epochInfo, err := fetcher.ParseEpochInfo(block.Header())

	require.NoError(t, err)
	require.NotNil(t, epochInfo)

	assert.Equal(t, 3, epochInfo.ValidatorCount)
	assert.Equal(t, 3, epochInfo.CandidateCount)
	assert.Equal(t, 3, len(epochInfo.Validators))
}

func TestConsensusFetcher_ExtractWBFTExtra(t *testing.T) {
	logger := zap.NewNop()
	client := newMockClient()
	fetcher := NewConsensusFetcher(client, logger)

	validators := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
	}

	block := createTestBlock(100, 1, validators)

	extra, err := fetcher.ExtractWBFTExtra(block.Header())

	require.NoError(t, err)
	require.NotNil(t, extra)

	assert.Equal(t, uint32(1), extra.Round)
	assert.Equal(t, big.NewInt(1000), extra.GasTip)
	assert.NotNil(t, extra.PreparedSeal)
	assert.NotNil(t, extra.CommittedSeal)
	assert.NotNil(t, extra.EpochInfo)
}
