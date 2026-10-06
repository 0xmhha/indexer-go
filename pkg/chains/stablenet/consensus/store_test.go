package consensus

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

func TestPebbleStorage_SaveGetWBFTBlockExtra(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	st := s
	ctx := context.Background()

	// Create WBFT block extra
	blockNumber := uint64(100)
	extra := &WBFTBlockExtra{
		BlockNumber: blockNumber,
		BlockHash:   common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		Round:       1,
		Timestamp:   1000,
	}

	// Save WBFT block extra
	err := st.SaveWBFTBlockExtra(ctx, extra)
	if err != nil {
		t.Fatalf("SaveWBFTBlockExtra() error = %v", err)
	}

	// Get WBFT block extra
	retrieved, err := st.GetWBFTBlockExtra(ctx, blockNumber)
	if err != nil {
		t.Fatalf("GetWBFTBlockExtra() error = %v", err)
	}

	// Verify fields
	if retrieved.BlockNumber != extra.BlockNumber {
		t.Errorf("BlockNumber = %d, want %d", retrieved.BlockNumber, extra.BlockNumber)
	}
	if retrieved.BlockHash != extra.BlockHash {
		t.Errorf("BlockHash = %v, want %v", retrieved.BlockHash, extra.BlockHash)
	}
	if retrieved.Round != extra.Round {
		t.Errorf("Round = %d, want %d", retrieved.Round, extra.Round)
	}
}

func TestPebbleStorage_GetWBFTBlockExtra_NotFound(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	st := s
	ctx := context.Background()

	_, err := st.GetWBFTBlockExtra(ctx, 999)
	if err != port.ErrNotFound {
		t.Errorf("GetWBFTBlockExtra() error = %v, want port.ErrNotFound", err)
	}
}

func TestPebbleStorage_GetWBFTBlockExtraByHash(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	st := s
	ctx := context.Background()

	// Create and save block first (needed for hash index)
	blockNumber := uint64(100)

	// Need to set block hash index
	block := createTestBlockWithMiner(blockNumber, common.Address{}, 100000, 1000)
	err := st.SetBlock(ctx, modelBlock(block))
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	// Save WBFT extra
	extra := &WBFTBlockExtra{
		BlockNumber: blockNumber,
		BlockHash:   block.Hash(),
		Round:       2,
		Timestamp:   1000,
	}
	err = st.SaveWBFTBlockExtra(ctx, extra)
	if err != nil {
		t.Fatalf("SaveWBFTBlockExtra() error = %v", err)
	}

	// Get by hash
	retrieved, err := st.GetWBFTBlockExtraByHash(ctx, block.Hash())
	if err != nil {
		t.Fatalf("GetWBFTBlockExtraByHash() error = %v", err)
	}

	if retrieved.BlockNumber != blockNumber {
		t.Errorf("BlockNumber = %d, want %d", retrieved.BlockNumber, blockNumber)
	}
	if retrieved.Round != 2 {
		t.Errorf("Round = %d, want 2", retrieved.Round)
	}
}

func TestPebbleStorage_SaveGetEpochInfo(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	st := s
	ctx := context.Background()

	// Create epoch info
	epochNumber := uint64(5)
	epochInfo := &EpochInfo{
		EpochNumber: epochNumber,
		BlockNumber: 1000,
		Validators:  []uint32{0, 1, 2},
		Candidates: []Candidate{
			{Address: common.HexToAddress("0x1111111111111111111111111111111111111111"), Diligence: 100},
		},
	}

	// Save epoch info
	err := st.SaveEpochInfo(ctx, epochInfo)
	if err != nil {
		t.Fatalf("SaveEpochInfo() error = %v", err)
	}

	// Get epoch info
	retrieved, err := st.GetEpochInfo(ctx, epochNumber)
	if err != nil {
		t.Fatalf("GetEpochInfo() error = %v", err)
	}

	// Verify fields
	if retrieved.EpochNumber != epochInfo.EpochNumber {
		t.Errorf("EpochNumber = %d, want %d", retrieved.EpochNumber, epochInfo.EpochNumber)
	}
	if retrieved.BlockNumber != epochInfo.BlockNumber {
		t.Errorf("BlockNumber = %d, want %d", retrieved.BlockNumber, epochInfo.BlockNumber)
	}
	if len(retrieved.Validators) != len(epochInfo.Validators) {
		t.Errorf("Validators length = %d, want %d", len(retrieved.Validators), len(epochInfo.Validators))
	}
}

func TestPebbleStorage_GetEpochInfo_NotFound(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	st := s
	ctx := context.Background()

	_, err := st.GetEpochInfo(ctx, 999)
	if err != port.ErrNotFound {
		t.Errorf("GetEpochInfo() error = %v, want port.ErrNotFound", err)
	}
}

func TestPebbleStorage_GetLatestEpochInfo(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	st := s
	ctx := context.Background()

	// Save multiple epochs
	epochs := []*EpochInfo{
		{EpochNumber: 1, BlockNumber: 1000, Validators: []uint32{0}},
		{EpochNumber: 2, BlockNumber: 2000, Validators: []uint32{0}},
		{EpochNumber: 3, BlockNumber: 3000, Validators: []uint32{0}},
	}

	for _, epoch := range epochs {
		err := st.SaveEpochInfo(ctx, epoch)
		if err != nil {
			t.Fatalf("SaveEpochInfo() error = %v", err)
		}
	}

	// Get latest epoch
	latest, err := st.GetLatestEpochInfo(ctx)
	if err != nil {
		t.Fatalf("GetLatestEpochInfo() error = %v", err)
	}

	// Should be epoch 3
	if latest.EpochNumber != 3 {
		t.Errorf("Latest EpochNumber = %d, want 3", latest.EpochNumber)
	}
}

func TestPebbleStorage_GetLatestEpochInfo_NotFound(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	st := s
	ctx := context.Background()

	_, err := st.GetLatestEpochInfo(ctx)
	if err != port.ErrNotFound {
		t.Errorf("GetLatestEpochInfo() error = %v, want port.ErrNotFound", err)
	}
}

func TestPebbleStorage_UpdateValidatorSigningStats(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	st := s
	ctx := context.Background()

	blockNumber := uint64(100)
	validator1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	validator2 := common.HexToAddress("0x2222222222222222222222222222222222222222")

	// Create signing activities
	activities := []*ValidatorSigningActivity{
		{
			BlockNumber:      blockNumber,
			ValidatorAddress: validator1,
			ValidatorIndex:   0,
			SignedPrepare:    true,
			SignedCommit:     true,
		},
		{
			BlockNumber:      blockNumber,
			ValidatorAddress: validator2,
			ValidatorIndex:   1,
			SignedPrepare:    false,
			SignedCommit:     true,
		},
	}

	// Update stats
	err := st.UpdateValidatorSigningStats(ctx, blockNumber, activities)
	if err != nil {
		t.Fatalf("UpdateValidatorSigningStats() error = %v", err)
	}

	// Get signing activity for validator1
	activityList, err := st.GetValidatorSigningActivity(ctx, validator1, blockNumber, blockNumber, 10, 0)
	if err != nil {
		t.Fatalf("GetValidatorSigningActivity() error = %v", err)
	}

	if len(activityList) != 1 {
		t.Errorf("GetValidatorSigningActivity() returned %d activities, want 1", len(activityList))
	}

	if activityList[0].SignedPrepare != true {
		t.Errorf("SignedPrepare = %v, want true", activityList[0].SignedPrepare)
	}
}

func TestPebbleStorage_GetValidatorSigningStats(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	st := s
	ctx := context.Background()

	validator := common.HexToAddress("0x1111111111111111111111111111111111111111")
	fromBlock := uint64(100)
	toBlock := uint64(110)

	// Initially should return empty stats (not port.ErrNotFound)
	stats, err := st.GetValidatorSigningStats(ctx, validator, fromBlock, toBlock)
	if err != nil {
		t.Fatalf("GetValidatorSigningStats() error = %v", err)
	}

	// Should return zero stats
	if stats.PrepareSignCount != 0 {
		t.Errorf("PrepareSignCount = %d, want 0", stats.PrepareSignCount)
	}
	if stats.ValidatorAddress != validator {
		t.Errorf("ValidatorAddress = %v, want %v", stats.ValidatorAddress, validator)
	}
}

func TestPebbleStorage_GetAllValidatorsSigningStats(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	st := s
	ctx := context.Background()

	// Add some activities
	validator1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	validator2 := common.HexToAddress("0x2222222222222222222222222222222222222222")

	activities1 := []*ValidatorSigningActivity{
		{BlockNumber: 100, ValidatorAddress: validator1, SignedPrepare: true, SignedCommit: true},
	}
	activities2 := []*ValidatorSigningActivity{
		{BlockNumber: 101, ValidatorAddress: validator2, SignedPrepare: true, SignedCommit: false},
	}

	err := st.UpdateValidatorSigningStats(ctx, 100, activities1)
	if err != nil {
		t.Fatalf("UpdateValidatorSigningStats() error = %v", err)
	}
	err = st.UpdateValidatorSigningStats(ctx, 101, activities2)
	if err != nil {
		t.Fatalf("UpdateValidatorSigningStats() error = %v", err)
	}

	// Get all stats
	stats, err := st.GetAllValidatorsSigningStats(ctx, 100, 110, 10, 0)
	if err != nil {
		t.Fatalf("GetAllValidatorsSigningStats() error = %v", err)
	}

	if len(stats) != 2 {
		t.Errorf("GetAllValidatorsSigningStats() returned %d stats, want 2", len(stats))
	}
}

func TestPebbleStorage_GetValidatorSigningActivity(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	st := s
	ctx := context.Background()

	validator := common.HexToAddress("0x1111111111111111111111111111111111111111")

	// Add multiple blocks of activity
	for i := uint64(100); i < 105; i++ {
		activities := []*ValidatorSigningActivity{
			{
				BlockNumber:      i,
				ValidatorAddress: validator,
				ValidatorIndex:   0,
				SignedPrepare:    i%2 == 0, // Alternate true/false
				SignedCommit:     true,
			},
		}
		err := st.UpdateValidatorSigningStats(ctx, i, activities)
		if err != nil {
			t.Fatalf("UpdateValidatorSigningStats() error = %v", err)
		}
	}

	// Get activity
	activityList, err := st.GetValidatorSigningActivity(ctx, validator, 100, 104, 10, 0)
	if err != nil {
		t.Fatalf("GetValidatorSigningActivity() error = %v", err)
	}

	if len(activityList) != 5 {
		t.Errorf("GetValidatorSigningActivity() returned %d activities, want 5", len(activityList))
	}

	// Test pagination
	activityList, err = st.GetValidatorSigningActivity(ctx, validator, 100, 104, 2, 0)
	if err != nil {
		t.Fatalf("GetValidatorSigningActivity() with limit error = %v", err)
	}

	if len(activityList) != 2 {
		t.Errorf("GetValidatorSigningActivity(limit=2) returned %d activities, want 2", len(activityList))
	}
}

func TestPebbleStorage_GetBlockSigners(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	st := s
	ctx := context.Background()

	blockNumber := uint64(100)

	// Create and save block with WBFT extra
	block := createTestBlockWithMiner(blockNumber, common.Address{}, 100000, 1000)
	err := st.SetBlock(ctx, modelBlock(block))
	if err != nil {
		t.Fatalf("SetBlock() error = %v", err)
	}

	// Save WBFT extra
	extra := &WBFTBlockExtra{
		BlockNumber: blockNumber,
		BlockHash:   block.Hash(),
		Round:       1,
		Timestamp:   1000,
	}
	err = st.SaveWBFTBlockExtra(ctx, extra)
	if err != nil {
		t.Fatalf("SaveWBFTBlockExtra() error = %v", err)
	}

	// Get block signers (should not error even if no signers)
	preparers, committers, err := st.GetBlockSigners(ctx, blockNumber)
	if err != nil {
		t.Fatalf("GetBlockSigners() error = %v", err)
	}

	// Just verify it returns slices (may be empty if no signers)
	if preparers == nil {
		t.Error("Preparers should not be nil")
	}
	if committers == nil {
		t.Error("Committers should not be nil")
	}
}

func TestPebbleStorage_WBFT_ClosedStorage(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	st := s
	ctx := context.Background()

	// Close storage
	_ = st.Close()

	// All operations should return port.ErrClosed
	_, err := st.GetWBFTBlockExtra(ctx, 100)
	if !errors.Is(err, port.ErrClosed) {
		t.Errorf("GetWBFTBlockExtra() on closed storage error = %v, want port.ErrClosed", err)
	}

	_, err = st.GetWBFTBlockExtraByHash(ctx, common.Hash{})
	if !errors.Is(err, port.ErrClosed) {
		t.Errorf("GetWBFTBlockExtraByHash() on closed storage error = %v, want port.ErrClosed", err)
	}

	_, err = st.GetEpochInfo(ctx, 1)
	if !errors.Is(err, port.ErrClosed) {
		t.Errorf("GetEpochInfo() on closed storage error = %v, want port.ErrClosed", err)
	}

	_, err = st.GetLatestEpochInfo(ctx)
	if !errors.Is(err, port.ErrClosed) {
		t.Errorf("GetLatestEpochInfo() on closed storage error = %v, want port.ErrClosed", err)
	}

	_, err = st.GetValidatorSigningStats(ctx, common.Address{}, 0, 100)
	if !errors.Is(err, port.ErrClosed) {
		t.Errorf("GetValidatorSigningStats() on closed storage error = %v, want port.ErrClosed", err)
	}

	err = st.SaveWBFTBlockExtra(ctx, &WBFTBlockExtra{})
	if !errors.Is(err, port.ErrClosed) {
		t.Errorf("SaveWBFTBlockExtra() on closed storage error = %v, want port.ErrClosed", err)
	}

	err = st.SaveEpochInfo(ctx, &EpochInfo{})
	if !errors.Is(err, port.ErrClosed) {
		t.Errorf("SaveEpochInfo() on closed storage error = %v, want port.ErrClosed", err)
	}
}
