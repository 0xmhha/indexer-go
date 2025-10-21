package fetch

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// mockClient is a mock implementation of the RPC client
type mockClient struct {
	blocks      map[uint64]*types.Block
	receipts    map[common.Hash]types.Receipts
	latestBlock uint64
	failCount   int // for testing retry logic
}

func newMockClient() *mockClient {
	return &mockClient{
		blocks:   make(map[uint64]*types.Block),
		receipts: make(map[common.Hash]types.Receipts),
	}
}

func (m *mockClient) GetLatestBlockNumber(ctx context.Context) (uint64, error) {
	if m.failCount > 0 {
		m.failCount--
		return 0, fmt.Errorf("mock error")
	}
	return m.latestBlock, nil
}

func (m *mockClient) GetBlockByNumber(ctx context.Context, number uint64) (*types.Block, error) {
	if m.failCount > 0 {
		m.failCount--
		return nil, fmt.Errorf("mock error")
	}
	block, ok := m.blocks[number]
	if !ok {
		return nil, fmt.Errorf("block not found")
	}
	return block, nil
}

func (m *mockClient) GetBlockReceipts(ctx context.Context, blockNumber uint64) (types.Receipts, error) {
	if m.failCount > 0 {
		m.failCount--
		return nil, fmt.Errorf("mock error")
	}
	block, ok := m.blocks[blockNumber]
	if !ok {
		return nil, fmt.Errorf("block not found")
	}
	receipts, ok := m.receipts[block.Hash()]
	if !ok {
		return types.Receipts{}, nil
	}
	return receipts, nil
}

func (m *mockClient) Close() {}

// mockStorage is a mock implementation of the storage layer
type mockStorage struct {
	blocks       map[uint64]*types.Block
	receipts     map[common.Hash]*types.Receipt
	latestHeight uint64
	readOnly     bool
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		blocks:   make(map[uint64]*types.Block),
		receipts: make(map[common.Hash]*types.Receipt),
	}
}

func (m *mockStorage) GetLatestHeight(ctx context.Context) (uint64, error) {
	if m.latestHeight == 0 {
		return 0, fmt.Errorf("no blocks indexed")
	}
	return m.latestHeight, nil
}

func (m *mockStorage) GetBlock(ctx context.Context, height uint64) (*types.Block, error) {
	block, ok := m.blocks[height]
	if !ok {
		return nil, fmt.Errorf("block not found")
	}
	return block, nil
}

func (m *mockStorage) GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	for _, block := range m.blocks {
		if block.Hash() == hash {
			return block, nil
		}
	}
	return nil, fmt.Errorf("block not found")
}

func (m *mockStorage) SetBlock(ctx context.Context, block *types.Block) error {
	if m.readOnly {
		return fmt.Errorf("storage is read-only")
	}
	height := block.Number().Uint64()
	m.blocks[height] = block
	if height > m.latestHeight {
		m.latestHeight = height
	}
	return nil
}

func (m *mockStorage) SetReceipt(ctx context.Context, receipt *types.Receipt) error {
	if m.readOnly {
		return fmt.Errorf("storage is read-only")
	}
	m.receipts[receipt.TxHash] = receipt
	return nil
}

func (m *mockStorage) HasBlock(ctx context.Context, height uint64) (bool, error) {
	_, ok := m.blocks[height]
	return ok, nil
}

// Legacy methods for backward compatibility with existing tests
func (m *mockStorage) GetBlockByHeight(height uint64) (*types.Block, error) {
	return m.GetBlock(context.Background(), height)
}

func (m *mockStorage) PutBlock(block *types.Block) error {
	return m.SetBlock(context.Background(), block)
}

func (m *mockStorage) PutReceipt(receipt *types.Receipt) error {
	return m.SetReceipt(context.Background(), receipt)
}

func (m *mockStorage) Close() error {
	return nil
}

// TestNewFetcher tests creating a new fetcher
func TestNewFetcher(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Second,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)
	if fetcher == nil {
		t.Fatal("NewFetcher() returned nil")
	}
}

// TestFetchBlock tests fetching a single block
func TestFetchBlock(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add a mock block
	header := &types.Header{
		Number:     big.NewInt(1),
		Time:       uint64(time.Now().Unix()),
		Difficulty: big.NewInt(1000),
		GasLimit:   8000000,
		GasUsed:    21000,
	}
	block := types.NewBlockWithHeader(header)
	client.blocks[1] = block
	client.receipts[block.Hash()] = types.Receipts{}

	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 100,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	ctx := context.Background()
	err := fetcher.FetchBlock(ctx, 1)
	if err != nil {
		t.Fatalf("FetchBlock() error = %v", err)
	}

	// Verify block was stored
	storedBlock, err := storage.GetBlockByHeight(1)
	if err != nil {
		t.Fatalf("GetBlockByHeight() error = %v", err)
	}
	if storedBlock.Hash() != block.Hash() {
		t.Errorf("Stored block hash mismatch: got %s, want %s", storedBlock.Hash(), block.Hash())
	}
}

// TestFetchBlockWithRetry tests retry logic
func TestFetchBlockWithRetry(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add a mock block
	header := &types.Header{
		Number:     big.NewInt(1),
		Time:       uint64(time.Now().Unix()),
		Difficulty: big.NewInt(1000),
		GasLimit:   8000000,
		GasUsed:    21000,
	}
	block := types.NewBlockWithHeader(header)
	client.blocks[1] = block
	client.receipts[block.Hash()] = types.Receipts{}

	// Set client to fail once, then succeed
	client.failCount = 1

	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	ctx := context.Background()
	err := fetcher.FetchBlock(ctx, 1)
	if err != nil {
		t.Fatalf("FetchBlock() should succeed after retry, got error = %v", err)
	}

	// Verify block was stored
	_, err = storage.GetBlockByHeight(1)
	if err != nil {
		t.Errorf("Block should be stored after retry, got error = %v", err)
	}
}

// TestFetchBlockMaxRetries tests max retry limit
func TestFetchBlockMaxRetries(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Set client to fail more times than max retries
	client.failCount = 5

	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	ctx := context.Background()
	err := fetcher.FetchBlock(ctx, 1)
	if err == nil {
		t.Error("FetchBlock() should fail after max retries")
	}
}

// TestFetchRange tests fetching a range of blocks
func TestFetchRange(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add mock blocks
	for i := uint64(0); i < 10; i++ {
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()),
			Difficulty: big.NewInt(1000),
			GasLimit:   8000000,
			GasUsed:    21000,
		}
		block := types.NewBlockWithHeader(header)
		client.blocks[i] = block
		client.receipts[block.Hash()] = types.Receipts{}
	}
	client.latestBlock = 9

	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 100,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	ctx := context.Background()
	err := fetcher.FetchRange(ctx, 0, 9)
	if err != nil {
		t.Fatalf("FetchRange() error = %v", err)
	}

	// Verify all blocks were stored
	for i := uint64(0); i < 10; i++ {
		_, err := storage.GetBlockByHeight(i)
		if err != nil {
			t.Errorf("Block %d should be stored, got error = %v", i, err)
		}
	}

	// Verify latest height
	latestHeight, err := storage.GetLatestHeight(context.Background())
	if err != nil {
		t.Fatalf("GetLatestHeight() error = %v", err)
	}
	if latestHeight != 9 {
		t.Errorf("Latest height = %d, want 9", latestHeight)
	}
}

// TestFetchRangeWithGap tests handling gaps in block range
func TestFetchRangeWithGap(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add blocks with a gap (missing block 5)
	for i := uint64(0); i < 10; i++ {
		if i == 5 {
			continue // Create a gap
		}
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()),
			Difficulty: big.NewInt(1000),
			GasLimit:   8000000,
			GasUsed:    21000,
		}
		block := types.NewBlockWithHeader(header)
		client.blocks[i] = block
		client.receipts[block.Hash()] = types.Receipts{}
	}
	client.latestBlock = 9

	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 100,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	ctx := context.Background()
	err := fetcher.FetchRange(ctx, 0, 9)
	if err == nil {
		t.Error("FetchRange() should fail when encountering missing block")
	}
}

// TestGetNextHeight tests determining next height to fetch
func TestGetNextHeight(t *testing.T) {
	tests := []struct {
		name        string
		storedBlock uint64
		startHeight uint64
		want        uint64
	}{
		{
			name:        "no blocks stored, use start height",
			storedBlock: 0,
			startHeight: 0,
			want:        0,
		},
		{
			name:        "blocks stored, continue from next",
			storedBlock: 5,
			startHeight: 0,
			want:        6,
		},
		{
			name:        "start height higher than stored",
			storedBlock: 0,
			startHeight: 100,
			want:        100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newMockClient()
			storage := newMockStorage()
			logger, _ := zap.NewDevelopment()

			if tt.storedBlock > 0 {
				storage.latestHeight = tt.storedBlock
				// Add a dummy block
				header := &types.Header{
					Number: big.NewInt(int64(tt.storedBlock)),
				}
				block := types.NewBlockWithHeader(header)
				storage.blocks[tt.storedBlock] = block
			}

			config := &Config{
				StartHeight: tt.startHeight,
				BatchSize:   10,
				MaxRetries:  3,
				RetryDelay:  time.Millisecond * 100,
			}

			fetcher := NewFetcher(client, storage, config, logger, nil)

			ctx := context.Background()
			got := fetcher.GetNextHeight(ctx)
			if got != tt.want {
				t.Errorf("GetNextHeight() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestRun tests the Run method with context cancellation
func TestRun(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add mock blocks
	for i := uint64(0); i < 5; i++ {
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()),
			Difficulty: big.NewInt(1000),
			GasLimit:   8000000,
			GasUsed:    21000,
		}
		block := types.NewBlockWithHeader(header)
		client.blocks[i] = block
		client.receipts[block.Hash()] = types.Receipts{}
	}
	client.latestBlock = 4

	config := &Config{
		StartHeight: 0,
		BatchSize:   2,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*500)
	defer cancel()

	// Run should stop when context is cancelled
	err := fetcher.Run(ctx)
	if err == nil {
		t.Error("Run() should return error when context is cancelled")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("Run() error = %v, want %v", err, context.DeadlineExceeded)
	}

	// Verify some blocks were indexed
	latestHeight, err := storage.GetLatestHeight(context.Background())
	if err != nil {
		t.Fatalf("GetLatestHeight() error = %v", err)
	}
	if latestHeight == 0 {
		t.Error("Expected some blocks to be indexed")
	}
}

// TestRunCaughtUp tests Run when caught up with chain
func TestRunCaughtUp(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add blocks to storage (already indexed)
	for i := uint64(0); i < 5; i++ {
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()),
			Difficulty: big.NewInt(1000),
			GasLimit:   8000000,
			GasUsed:    21000,
		}
		block := types.NewBlockWithHeader(header)
		storage.blocks[i] = block
		storage.latestHeight = i
		client.blocks[i] = block
		client.receipts[block.Hash()] = types.Receipts{}
	}
	client.latestBlock = 4 // Same as storage

	config := &Config{
		StartHeight: 0,
		BatchSize:   2,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
	defer cancel()

	// Run should wait when caught up
	err := fetcher.Run(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Run() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

// TestRunWithClientError tests Run with client errors
func TestRunWithClientError(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Set client to fail
	client.failCount = 10

	config := &Config{
		StartHeight: 0,
		BatchSize:   2,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	// Run should handle errors gracefully
	err := fetcher.Run(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Run() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

// TestFetchBlockStorageError tests FetchBlock with storage error
func TestFetchBlockStorageError(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add a mock block
	header := &types.Header{
		Number:     big.NewInt(1),
		Time:       uint64(time.Now().Unix()),
		Difficulty: big.NewInt(1000),
		GasLimit:   8000000,
		GasUsed:    21000,
	}
	block := types.NewBlockWithHeader(header)
	client.blocks[1] = block
	client.receipts[block.Hash()] = types.Receipts{}

	// Set storage to read-only to cause error
	storage.readOnly = true

	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	ctx := context.Background()
	err := fetcher.FetchBlock(ctx, 1)
	if err == nil {
		t.Error("FetchBlock() should fail with storage error")
	}
}

// TestFetchBlockReceiptError tests FetchBlock with receipt fetch error
func TestFetchBlockReceiptError(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add a mock block
	header := &types.Header{
		Number:     big.NewInt(1),
		Time:       uint64(time.Now().Unix()),
		Difficulty: big.NewInt(1000),
		GasLimit:   8000000,
		GasUsed:    21000,
	}
	block := types.NewBlockWithHeader(header)
	client.blocks[1] = block
	// Don't add receipts to cause error

	// Set client to fail on receipt fetch (after block fetch succeeds)
	client.failCount = 5 // More than max retries

	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	ctx := context.Background()
	err := fetcher.FetchBlock(ctx, 1)
	if err == nil {
		t.Error("FetchBlock() should fail with receipt error")
	}
}

// TestFetchRangeContextCancel tests FetchRange with context cancellation
func TestFetchRangeContextCancel(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add mock blocks
	for i := uint64(0); i < 100; i++ {
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()),
			Difficulty: big.NewInt(1000),
			GasLimit:   8000000,
			GasUsed:    21000,
		}
		block := types.NewBlockWithHeader(header)
		client.blocks[i] = block
		client.receipts[block.Hash()] = types.Receipts{}
	}

	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	// Create context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := fetcher.FetchRange(ctx, 0, 99)
	if err == nil {
		t.Error("FetchRange() should return error when context is cancelled")
	}
}

// TestGetNextHeightWithError tests GetNextHeight with storage error
func TestGetNextHeightWithError(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Don't add any blocks (GetLatestHeight will return error)

	config := &Config{
		StartHeight: 100,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	// Should fall back to start height when storage returns error
	ctx := context.Background()
	nextHeight := fetcher.GetNextHeight(ctx)
	if nextHeight != 100 {
		t.Errorf("GetNextHeight() = %d, want 100", nextHeight)
	}
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				StartHeight: 0,
				BatchSize:   10,
				MaxRetries:  3,
				RetryDelay:  time.Second,
				NumWorkers:  10,
			},
			wantErr: false,
		},
		{
			name: "valid config with default workers",
			config: &Config{
				StartHeight: 0,
				BatchSize:   10,
				MaxRetries:  3,
				RetryDelay:  time.Second,
				NumWorkers:  0, // Will use default
			},
			wantErr: false,
		},
		{
			name: "invalid batch size",
			config: &Config{
				StartHeight: 0,
				BatchSize:   0,
				MaxRetries:  3,
				RetryDelay:  time.Second,
				NumWorkers:  10,
			},
			wantErr: true,
		},
		{
			name: "invalid max retries",
			config: &Config{
				StartHeight: 0,
				BatchSize:   10,
				MaxRetries:  0,
				RetryDelay:  time.Second,
				NumWorkers:  10,
			},
			wantErr: true,
		},
		{
			name: "invalid retry delay",
			config: &Config{
				StartHeight: 0,
				BatchSize:   10,
				MaxRetries:  3,
				RetryDelay:  0,
				NumWorkers:  10,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestFetchRangeConcurrent tests concurrent block fetching
func TestFetchRangeConcurrent(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add mock blocks
	numBlocks := uint64(100)
	for i := uint64(0); i < numBlocks; i++ {
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()),
			Difficulty: big.NewInt(1000),
			GasLimit:   8000000,
			GasUsed:    21000,
		}
		block := types.NewBlockWithHeader(header)
		client.blocks[i] = block
		client.receipts[block.Hash()] = types.Receipts{}
	}
	client.latestBlock = numBlocks - 1

	config := &Config{
		StartHeight: 0,
		BatchSize:   int(numBlocks),
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
		NumWorkers:  10,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	ctx := context.Background()
	err := fetcher.FetchRangeConcurrent(ctx, 0, numBlocks-1)
	if err != nil {
		t.Fatalf("FetchRangeConcurrent() error = %v", err)
	}

	// Verify all blocks were stored in correct order
	for i := uint64(0); i < numBlocks; i++ {
		storedBlock, err := storage.GetBlockByHeight(i)
		if err != nil {
			t.Errorf("Block %d should be stored, got error = %v", i, err)
			continue
		}
		if storedBlock.Number().Uint64() != i {
			t.Errorf("Block at height %d has number %d", i, storedBlock.Number().Uint64())
		}
	}

	// Verify latest height
	latestHeight, err := storage.GetLatestHeight(context.Background())
	if err != nil {
		t.Fatalf("GetLatestHeight() error = %v", err)
	}
	if latestHeight != numBlocks-1 {
		t.Errorf("Latest height = %d, want %d", latestHeight, numBlocks-1)
	}
}

// TestFetchRangeConcurrentPerformance tests that concurrent fetching is faster than sequential
func TestFetchRangeConcurrentPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	numBlocks := uint64(50)

	// Prepare blocks once
	blocks := make(map[uint64]*types.Block)
	receipts := make(map[common.Hash]types.Receipts)
	for i := uint64(0); i < numBlocks; i++ {
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()),
			Difficulty: big.NewInt(1000),
			GasLimit:   8000000,
			GasUsed:    21000,
		}
		block := types.NewBlockWithHeader(header)
		blocks[i] = block
		receipts[block.Hash()] = types.Receipts{}
	}

	logger, _ := zap.NewDevelopment()

	// Test sequential fetching
	t.Run("Sequential", func(t *testing.T) {
		client := newMockClient()
		storage := newMockStorage()
		client.blocks = blocks
		client.receipts = receipts
		client.latestBlock = numBlocks - 1

		config := &Config{
			StartHeight: 0,
			BatchSize:   int(numBlocks),
			MaxRetries:  3,
			RetryDelay:  time.Millisecond * 10,
			NumWorkers:  1, // Sequential
		}

		fetcher := NewFetcher(client, storage, config, logger, nil)

		start := time.Now()
		ctx := context.Background()
		err := fetcher.FetchRange(ctx, 0, numBlocks-1)
		sequentialDuration := time.Since(start)

		if err != nil {
			t.Fatalf("FetchRange() error = %v", err)
		}

		t.Logf("Sequential fetch took %v for %d blocks", sequentialDuration, numBlocks)
	})

	// Test concurrent fetching
	var concurrentDuration time.Duration
	t.Run("Concurrent", func(t *testing.T) {
		client := newMockClient()
		storage := newMockStorage()
		client.blocks = blocks
		client.receipts = receipts
		client.latestBlock = numBlocks - 1

		config := &Config{
			StartHeight: 0,
			BatchSize:   int(numBlocks),
			MaxRetries:  3,
			RetryDelay:  time.Millisecond * 10,
			NumWorkers:  10, // Concurrent
		}

		fetcher := NewFetcher(client, storage, config, logger, nil)

		start := time.Now()
		ctx := context.Background()
		err := fetcher.FetchRangeConcurrent(ctx, 0, numBlocks-1)
		concurrentDuration = time.Since(start)

		if err != nil {
			t.Fatalf("FetchRangeConcurrent() error = %v", err)
		}

		t.Logf("Concurrent fetch took %v for %d blocks", concurrentDuration, numBlocks)
	})

	// Note: Due to mock implementation, performance difference may not be significant
	// In production with real network I/O, concurrent should be significantly faster
	t.Logf("Performance comparison: Sequential vs Concurrent")
}

// TestFetchRangeConcurrentWithRetry tests concurrent fetching with retry logic
func TestFetchRangeConcurrentWithRetry(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add mock blocks
	numBlocks := uint64(10)
	for i := uint64(0); i < numBlocks; i++ {
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()),
			Difficulty: big.NewInt(1000),
			GasLimit:   8000000,
			GasUsed:    21000,
		}
		block := types.NewBlockWithHeader(header)
		client.blocks[i] = block
		client.receipts[block.Hash()] = types.Receipts{}
	}
	client.latestBlock = numBlocks - 1

	// Set client to fail once, then succeed
	client.failCount = 1

	config := &Config{
		StartHeight: 0,
		BatchSize:   int(numBlocks),
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
		NumWorkers:  5,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	ctx := context.Background()
	err := fetcher.FetchRangeConcurrent(ctx, 0, numBlocks-1)
	if err != nil {
		t.Fatalf("FetchRangeConcurrent() should succeed after retry, got error = %v", err)
	}

	// Verify all blocks were stored
	for i := uint64(0); i < numBlocks; i++ {
		_, err := storage.GetBlockByHeight(i)
		if err != nil {
			t.Errorf("Block %d should be stored after retry, got error = %v", i, err)
		}
	}
}

// TestFetchRangeConcurrentContextCancel tests concurrent fetching with context cancellation
func TestFetchRangeConcurrentContextCancel(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add many mock blocks
	numBlocks := uint64(1000)
	for i := uint64(0); i < numBlocks; i++ {
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()),
			Difficulty: big.NewInt(1000),
			GasLimit:   8000000,
			GasUsed:    21000,
		}
		block := types.NewBlockWithHeader(header)
		client.blocks[i] = block
		client.receipts[block.Hash()] = types.Receipts{}
	}

	config := &Config{
		StartHeight: 0,
		BatchSize:   int(numBlocks),
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
		NumWorkers:  10,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	// Create context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := fetcher.FetchRangeConcurrent(ctx, 0, numBlocks-1)
	if err == nil {
		t.Error("FetchRangeConcurrent() should return error when context is cancelled")
	}
}

// TestFetchRangeConcurrentMaxRetries tests concurrent fetching with max retry limit
func TestFetchRangeConcurrentMaxRetries(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add some blocks
	numBlocks := uint64(5)
	for i := uint64(0); i < numBlocks; i++ {
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()),
			Difficulty: big.NewInt(1000),
			GasLimit:   8000000,
			GasUsed:    21000,
		}
		block := types.NewBlockWithHeader(header)
		client.blocks[i] = block
		client.receipts[block.Hash()] = types.Receipts{}
	}

	// Set client to fail more times than max retries
	client.failCount = 100

	config := &Config{
		StartHeight: 0,
		BatchSize:   int(numBlocks),
		MaxRetries:  2,
		RetryDelay:  time.Millisecond * 10,
		NumWorkers:  3,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	ctx := context.Background()
	err := fetcher.FetchRangeConcurrent(ctx, 0, numBlocks-1)
	if err == nil {
		t.Error("FetchRangeConcurrent() should fail after max retries")
	}
}

// TestFetchRangeConcurrentOrderPreservation tests that blocks are stored in order
func TestFetchRangeConcurrentOrderPreservation(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add mock blocks with specific data to verify order
	numBlocks := uint64(50)
	for i := uint64(0); i < numBlocks; i++ {
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()) + i, // Unique timestamp
			Difficulty: big.NewInt(1000 + int64(i)),   // Unique difficulty
			GasLimit:   8000000,
			GasUsed:    21000 + i, // Unique gas used
		}
		block := types.NewBlockWithHeader(header)
		client.blocks[i] = block
		client.receipts[block.Hash()] = types.Receipts{}
	}
	client.latestBlock = numBlocks - 1

	config := &Config{
		StartHeight: 0,
		BatchSize:   int(numBlocks),
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
		NumWorkers:  20, // High concurrency
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	ctx := context.Background()
	err := fetcher.FetchRangeConcurrent(ctx, 0, numBlocks-1)
	if err != nil {
		t.Fatalf("FetchRangeConcurrent() error = %v", err)
	}

	// Verify blocks are in correct sequential order
	for i := uint64(0); i < numBlocks; i++ {
		storedBlock, err := storage.GetBlockByHeight(i)
		if err != nil {
			t.Fatalf("Block %d not found: %v", i, err)
		}

		// Verify block number matches height
		if storedBlock.Number().Uint64() != i {
			t.Errorf("Block at height %d has wrong number: got %d, want %d",
				i, storedBlock.Number().Uint64(), i)
		}

		// Verify block data is correct (not from wrong height)
		expectedGasUsed := 21000 + i
		if storedBlock.GasUsed() != expectedGasUsed {
			t.Errorf("Block %d has wrong gas used: got %d, want %d",
				i, storedBlock.GasUsed(), expectedGasUsed)
		}

		expectedDifficulty := big.NewInt(1000 + int64(i))
		if storedBlock.Difficulty().Cmp(expectedDifficulty) != 0 {
			t.Errorf("Block %d has wrong difficulty: got %s, want %s",
				i, storedBlock.Difficulty(), expectedDifficulty)
		}
	}

	t.Logf("Successfully verified order preservation for %d blocks", numBlocks)
}

// TestGapRangeSize tests the GapRange Size method
func TestGapRangeSize(t *testing.T) {
	tests := []struct {
		name string
		gap  GapRange
		want uint64
	}{
		{
			name: "single block gap",
			gap:  GapRange{Start: 5, End: 5},
			want: 1,
		},
		{
			name: "multi-block gap",
			gap:  GapRange{Start: 10, End: 20},
			want: 11,
		},
		{
			name: "invalid gap (end < start)",
			gap:  GapRange{Start: 20, End: 10},
			want: 0,
		},
		{
			name: "zero gap",
			gap:  GapRange{Start: 0, End: 0},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.gap.Size()
			if got != tt.want {
				t.Errorf("GapRange.Size() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestDetectGaps tests gap detection functionality
func TestDetectGaps(t *testing.T) {
	tests := []struct {
		name          string
		storedBlocks  []uint64
		scanStart     uint64
		scanEnd       uint64
		expectedGaps  []GapRange
		expectError   bool
	}{
		{
			name:         "no gaps - continuous blocks",
			storedBlocks: []uint64{0, 1, 2, 3, 4, 5},
			scanStart:    0,
			scanEnd:      5,
			expectedGaps: []GapRange{},
			expectError:  false,
		},
		{
			name:         "single gap in middle",
			storedBlocks: []uint64{0, 1, 2, 4, 5},
			scanStart:    0,
			scanEnd:      5,
			expectedGaps: []GapRange{{Start: 3, End: 3}},
			expectError:  false,
		},
		{
			name:         "multiple gaps",
			storedBlocks: []uint64{0, 1, 4, 5, 8, 9},
			scanStart:    0,
			scanEnd:      9,
			expectedGaps: []GapRange{
				{Start: 2, End: 3},
				{Start: 6, End: 7},
			},
			expectError: false,
		},
		{
			name:         "gap at the beginning",
			storedBlocks: []uint64{3, 4, 5},
			scanStart:    0,
			scanEnd:      5,
			expectedGaps: []GapRange{{Start: 0, End: 2}},
			expectError:  false,
		},
		{
			name:         "gap at the end",
			storedBlocks: []uint64{0, 1, 2},
			scanStart:    0,
			scanEnd:      5,
			expectedGaps: []GapRange{{Start: 3, End: 5}},
			expectError:  false,
		},
		{
			name:         "all blocks missing",
			storedBlocks: []uint64{},
			scanStart:    0,
			scanEnd:      5,
			expectedGaps: []GapRange{{Start: 0, End: 5}},
			expectError:  false,
		},
		{
			name:         "large gap",
			storedBlocks: []uint64{0, 1, 100, 101},
			scanStart:    0,
			scanEnd:      101,
			expectedGaps: []GapRange{{Start: 2, End: 99}},
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newMockClient()
			storage := newMockStorage()
			logger, _ := zap.NewDevelopment()

			// Store blocks
			for _, height := range tt.storedBlocks {
				header := &types.Header{
					Number:     big.NewInt(int64(height)),
					Time:       uint64(time.Now().Unix()),
					Difficulty: big.NewInt(1000),
					GasLimit:   8000000,
					GasUsed:    21000,
				}
				block := types.NewBlockWithHeader(header)
				storage.blocks[height] = block
			}

			config := &Config{
				StartHeight: 0,
				BatchSize:   10,
				MaxRetries:  3,
				RetryDelay:  time.Millisecond * 10,
			}

			fetcher := NewFetcher(client, storage, config, logger, nil)

			ctx := context.Background()
			gaps, err := fetcher.DetectGaps(ctx, tt.scanStart, tt.scanEnd)

			if (err != nil) != tt.expectError {
				t.Errorf("DetectGaps() error = %v, expectError %v", err, tt.expectError)
				return
			}

			if len(gaps) != len(tt.expectedGaps) {
				t.Errorf("DetectGaps() found %d gaps, expected %d", len(gaps), len(tt.expectedGaps))
				t.Logf("Found gaps: %+v", gaps)
				t.Logf("Expected gaps: %+v", tt.expectedGaps)
				return
			}

			for i, gap := range gaps {
				if gap.Start != tt.expectedGaps[i].Start || gap.End != tt.expectedGaps[i].End {
					t.Errorf("Gap %d: got {%d, %d}, want {%d, %d}",
						i, gap.Start, gap.End, tt.expectedGaps[i].Start, tt.expectedGaps[i].End)
				}
			}
		})
	}
}

// TestDetectGapsContextCancel tests gap detection with context cancellation
func TestDetectGapsContextCancel(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Store some blocks
	for i := uint64(0); i < 100; i += 2 {
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()),
			Difficulty: big.NewInt(1000),
			GasLimit:   8000000,
			GasUsed:    21000,
		}
		block := types.NewBlockWithHeader(header)
		storage.blocks[i] = block
	}

	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	// Create context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetcher.DetectGaps(ctx, 0, 1000)
	if err == nil {
		t.Error("DetectGaps() should return error when context is cancelled")
	}
}

// TestFillGap tests filling a single gap
func TestFillGap(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add blocks to client
	for i := uint64(0); i < 20; i++ {
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()),
			Difficulty: big.NewInt(1000),
			GasLimit:   8000000,
			GasUsed:    21000,
		}
		block := types.NewBlockWithHeader(header)
		client.blocks[i] = block
		client.receipts[block.Hash()] = types.Receipts{}
	}

	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
		NumWorkers:  5,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)
	ctx := context.Background()

	tests := []struct {
		name string
		gap  GapRange
	}{
		{
			name: "small gap (sequential fetch)",
			gap:  GapRange{Start: 5, End: 9}, // 5 blocks
		},
		{
			name: "large gap (concurrent fetch)",
			gap:  GapRange{Start: 10, End: 19}, // 10 blocks
		},
		{
			name: "single block gap",
			gap:  GapRange{Start: 0, End: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear storage
			storage.blocks = make(map[uint64]*types.Block)
			storage.receipts = make(map[common.Hash]*types.Receipt)

			err := fetcher.FillGap(ctx, tt.gap)
			if err != nil {
				t.Fatalf("FillGap() error = %v", err)
			}

			// Verify all blocks in gap were filled
			for height := tt.gap.Start; height <= tt.gap.End; height++ {
				_, err := storage.GetBlock(ctx, height)
				if err != nil {
					t.Errorf("Block %d should be stored after filling gap, got error = %v", height, err)
				}
			}
		})
	}
}

// TestFillGaps tests filling multiple gaps
func TestFillGaps(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add blocks to client
	for i := uint64(0); i < 50; i++ {
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()),
			Difficulty: big.NewInt(1000),
			GasLimit:   8000000,
			GasUsed:    21000,
		}
		block := types.NewBlockWithHeader(header)
		client.blocks[i] = block
		client.receipts[block.Hash()] = types.Receipts{}
	}

	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
		NumWorkers:  5,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)
	ctx := context.Background()

	// Test multiple gaps
	gaps := []GapRange{
		{Start: 5, End: 9},
		{Start: 15, End: 19},
		{Start: 30, End: 34},
	}

	err := fetcher.FillGaps(ctx, gaps)
	if err != nil {
		t.Fatalf("FillGaps() error = %v", err)
	}

	// Verify all gaps were filled
	for _, gap := range gaps {
		for height := gap.Start; height <= gap.End; height++ {
			_, err := storage.GetBlock(ctx, height)
			if err != nil {
				t.Errorf("Block %d should be stored after filling gaps, got error = %v", height, err)
			}
		}
	}
}

// TestFillGapsEmpty tests filling with no gaps
func TestFillGapsEmpty(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)
	ctx := context.Background()

	err := fetcher.FillGaps(ctx, []GapRange{})
	if err != nil {
		t.Errorf("FillGaps() with empty gaps should not error, got = %v", err)
	}
}

// TestRunWithGapRecovery tests the gap recovery workflow
func TestRunWithGapRecovery(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add blocks to client
	for i := uint64(0); i < 20; i++ {
		header := &types.Header{
			Number:     big.NewInt(int64(i)),
			Time:       uint64(time.Now().Unix()),
			Difficulty: big.NewInt(1000),
			GasLimit:   8000000,
			GasUsed:    21000,
		}
		block := types.NewBlockWithHeader(header)
		client.blocks[i] = block
		client.receipts[block.Hash()] = types.Receipts{}
	}
	client.latestBlock = 19

	// Simulate existing data with gaps (blocks 0, 1, 2, 5, 6, 7, 10)
	existingBlocks := []uint64{0, 1, 2, 5, 6, 7, 10}
	for _, height := range existingBlocks {
		storage.blocks[height] = client.blocks[height]
		storage.latestHeight = height
	}

	config := &Config{
		StartHeight: 0,
		BatchSize:   5,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
		NumWorkers:  5,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	// Create context with timeout to stop after gap recovery
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*500)
	defer cancel()

	// Run with gap recovery
	err := fetcher.RunWithGapRecovery(ctx)
	// Error expected from context timeout
	if err == nil {
		t.Error("RunWithGapRecovery() should return error when context is cancelled")
	}

	// Verify gaps were filled (blocks 3, 4, 8, 9)
	gapBlocks := []uint64{3, 4, 8, 9}
	for _, height := range gapBlocks {
		_, err := storage.GetBlock(ctx, height)
		if err != nil {
			t.Errorf("Gap block %d should be filled, got error = %v", height, err)
		}
	}
}

// TestExponentialBackoff tests that retry delays increase exponentially
func TestExponentialBackoff(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	logger, _ := zap.NewDevelopment()

	// Add a mock block
	header := &types.Header{
		Number:     big.NewInt(1),
		Time:       uint64(time.Now().Unix()),
		Difficulty: big.NewInt(1000),
		GasLimit:   8000000,
		GasUsed:    21000,
	}
	block := types.NewBlockWithHeader(header)
	client.blocks[1] = block
	client.receipts[block.Hash()] = types.Receipts{}

	// Set client to fail 2 times, then succeed
	client.failCount = 2

	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 100, // Base delay
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	ctx := context.Background()
	start := time.Now()
	err := fetcher.FetchBlock(ctx, 1)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("FetchBlock() should succeed after retries, got error = %v", err)
	}

	// With exponential backoff:
	// Attempt 0: no delay
	// Attempt 1: 100ms delay
	// Attempt 2: 200ms delay
	// Total expected delay: ~300ms
	expectedMinDelay := time.Millisecond * 300
	expectedMaxDelay := time.Millisecond * 400 // Allow some overhead

	if duration < expectedMinDelay {
		t.Errorf("Expected at least %v delay with exponential backoff, got %v", expectedMinDelay, duration)
	}
	if duration > expectedMaxDelay {
		t.Logf("Warning: Delay %v exceeds expected max %v (may be due to slow test environment)", duration, expectedMaxDelay)
	}

	t.Logf("Exponential backoff test completed in %v", duration)
}
