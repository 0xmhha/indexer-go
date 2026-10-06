package fetch

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

// mockClient is a mock implementation of the RPC client
type mockClient struct {
	blocks      map[uint64]*types.Block
	receipts    map[common.Hash]types.Receipts
	latestBlock uint64
	failCount   int // for testing retry logic
	mu          sync.Mutex
}

// takeFailure consumes one injected failure. The fetcher calls the client
// from several workers, so the counter is guarded.
func (m *mockClient) takeFailure() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failCount > 0 {
		m.failCount--
		return true
	}
	return false
}

func newMockClient() *mockClient {
	return &mockClient{
		blocks:   make(map[uint64]*types.Block),
		receipts: make(map[common.Hash]types.Receipts),
	}
}

func (m *mockClient) GetLatestBlockNumber(ctx context.Context) (uint64, error) {
	if m.takeFailure() {
		return 0, fmt.Errorf("mock error")
	}
	return m.latestBlock, nil
}

func (m *mockClient) GetBlockByNumber(ctx context.Context, number uint64) (*types.Block, error) {
	if m.takeFailure() {
		return nil, fmt.Errorf("mock error")
	}
	block, ok := m.blocks[number]
	if !ok {
		return nil, fmt.Errorf("block not found")
	}
	return block, nil
}

func (m *mockClient) GetBlockReceipts(ctx context.Context, blockNumber uint64) (types.Receipts, error) {
	if m.takeFailure() {
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

func (m *mockClient) GetTransactionByHash(ctx context.Context, hash common.Hash) (*types.Transaction, bool, error) {
	// Not used in current tests, return nil
	return nil, false, fmt.Errorf("transaction not found")
}

func (m *mockClient) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	// Return a default balance for testing
	return big.NewInt(0), nil
}

func (m *mockClient) Close() {}

// mockStorage is a mock implementation of the storage layer
type mockStorage struct {
	blocks       map[uint64]*types.Block
	receipts     map[common.Hash]*types.Receipt
	latestHeight uint64
	readOnly     bool
	balances     map[common.Address]*big.Int // For balance tracking
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		blocks:   make(map[uint64]*types.Block),
		receipts: make(map[common.Hash]*types.Receipt),
		balances: make(map[common.Address]*big.Int),
	}
}

func (m *mockStorage) GetLatestHeight(ctx context.Context) (uint64, error) {
	if m.latestHeight == 0 {
		return 0, fmt.Errorf("no blocks indexed")
	}
	return m.latestHeight, nil
}

func (m *mockStorage) gethGetBlock(ctx context.Context, height uint64) (*types.Block, error) {
	block, ok := m.blocks[height]
	if !ok {
		return nil, fmt.Errorf("block not found")
	}
	return block, nil
}

func (m *mockStorage) gethGetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	for _, block := range m.blocks {
		if block.Hash() == hash {
			return block, nil
		}
	}
	return nil, fmt.Errorf("block not found")
}

func (m *mockStorage) gethSetBlock(ctx context.Context, block *types.Block) error {
	if m.readOnly {
		return fmt.Errorf("storage is read-only")
	}
	height := block.Number().Uint64()
	m.blocks[height] = block
	return nil
}

func (m *mockStorage) SetLatestHeight(ctx context.Context, height uint64) error {
	if m.readOnly {
		return fmt.Errorf("storage is read-only")
	}
	m.latestHeight = height
	return nil
}

func (m *mockStorage) gethSetReceipt(ctx context.Context, receipt *types.Receipt) error {
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

func (m *mockStorage) HasReceipt(ctx context.Context, hash common.Hash) (bool, error) {
	_, ok := m.receipts[hash]
	return ok, nil
}

func (m *mockStorage) GetMissingReceipts(ctx context.Context, blockNumber uint64) ([]common.Hash, error) {
	block, ok := m.blocks[blockNumber]
	if !ok {
		return nil, fmt.Errorf("block not found")
	}

	var missing []common.Hash
	for _, tx := range block.Transactions() {
		if _, ok := m.receipts[tx.Hash()]; !ok {
			missing = append(missing, tx.Hash())
		}
	}
	return missing, nil
}

// Legacy methods for backward compatibility with existing tests
func (m *mockStorage) GetBlockByHeight(height uint64) (*types.Block, error) {
	return m.gethGetBlock(context.Background(), height)
}

func (m *mockStorage) PutBlock(block *types.Block) error {
	return m.gethSetBlock(context.Background(), block)
}

func (m *mockStorage) PutReceipt(receipt *types.Receipt) error {
	return m.gethSetReceipt(context.Background(), receipt)
}

func (m *mockStorage) Close() error {
	return nil
}

// UpdateBalance updates the balance for an address (implements HistoricalWriter)
func (m *mockStorage) UpdateBalance(ctx context.Context, addr common.Address, blockNumber uint64, delta *big.Int, txHash common.Hash) error {
	currentBalance, ok := m.balances[addr]
	if !ok {
		currentBalance = big.NewInt(0)
	}
	newBalance := new(big.Int).Add(currentBalance, delta)
	m.balances[addr] = newBalance
	return nil
}

// SetBlockTimestamp indexes a block by timestamp (implements HistoricalWriter)
func (m *mockStorage) SetBlockTimestamp(ctx context.Context, timestamp uint64, height uint64) error {
	return nil
}

// SetBalance sets the balance for an address (implements HistoricalWriter)
func (m *mockStorage) SetBalance(ctx context.Context, addr common.Address, blockNumber uint64, balance *big.Int) error {
	m.balances[addr] = balance
	return nil
}

// GetAddressBalance returns the current balance for an address (implements HistoricalReader)
func (m *mockStorage) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	balance, ok := m.balances[addr]
	if !ok {
		return big.NewInt(0), nil
	}
	return balance, nil
}

// GetBalanceHistory returns empty history (implements HistoricalReader)
func (m *mockStorage) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]port.BalanceSnapshot, error) {
	// Return empty history to indicate no previous balance records
	return []port.BalanceSnapshot{}, nil
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
		name         string
		storedBlocks []uint64
		scanStart    uint64
		scanEnd      uint64
		expectedGaps []GapRange
		expectError  bool
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

// TestProcessBalanceTracking tests that balance changes are tracked correctly
func TestProcessBalanceTracking(t *testing.T) {
	t.Skip("Balance tracking test needs investigation - mockStorage interface implementation issue")
	client := newMockClient()
	storage := newMockStorage()
	logger := zap.NewNop()

	// Generate a private key for testing
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	// Derive address from private key
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		t.Fatal("Failed to cast public key to ECDSA")
	}
	from := crypto.PubkeyToAddress(*publicKeyECDSA)
	to := common.HexToAddress("0xABCDEF1234567890ABCDEF1234567890ABCDEF12")

	// Create a transaction with value transfer
	value := big.NewInt(1000000000000000000) // 1 ETH in Wei
	gasPrice := big.NewInt(20000000000)      // 20 Gwei
	gasLimit := uint64(21000)
	chainID := big.NewInt(1) // Mainnet chain ID

	// Create and sign transaction
	signer := types.NewEIP155Signer(chainID)
	baseTx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		To:       &to,
		Value:    value,
		Gas:      gasLimit,
		GasPrice: gasPrice,
		Data:     nil,
	})

	// Sign the transaction
	tx, err := types.SignTx(baseTx, signer, privateKey)
	if err != nil {
		t.Fatalf("Failed to sign transaction: %v", err)
	}

	// Verify sender can be recovered
	recoveredFrom, err := types.Sender(signer, tx)
	if err != nil {
		t.Fatalf("Failed to recover sender: %v", err)
	}
	if recoveredFrom != from {
		t.Fatalf("Recovered sender %v != expected %v", recoveredFrom, from)
	}

	// Create block with the transaction
	header := &types.Header{
		Number:     big.NewInt(1),
		GasUsed:    gasLimit,
		Difficulty: big.NewInt(1),
		Time:       uint64(time.Now().Unix()),
	}
	block := types.NewBlockWithHeader(header).WithBody(types.Body{Transactions: []*types.Transaction{tx}})

	// Create receipt
	receipt := &types.Receipt{
		TxHash:           tx.Hash(),
		GasUsed:          gasLimit,
		BlockNumber:      big.NewInt(1),
		TransactionIndex: 0,
		Status:           types.ReceiptStatusSuccessful,
	}

	// Setup mock client and storage
	client.blocks[1] = block
	client.receipts[block.Hash()] = types.Receipts{receipt}
	client.latestBlock = 1

	config := &Config{
		StartHeight: 0,
		BatchSize:   1,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond * 10,
	}

	fetcher := NewFetcher(client, storage, config, logger, nil)

	ctx := context.Background()
	err = fetcher.FetchBlock(ctx, 1)
	if err != nil {
		t.Fatalf("FetchBlock() error = %v", err)
	}

	// Verify sender balance decreased by (value + gas cost)
	gasCost := new(big.Int).Mul(big.NewInt(int64(gasLimit)), gasPrice)
	expectedSenderDelta := new(big.Int).Neg(new(big.Int).Add(value, gasCost))

	senderBalance, ok := storage.balances[from]
	if !ok {
		t.Error("Sender balance not tracked")
	} else if senderBalance.Cmp(expectedSenderDelta) != 0 {
		t.Errorf("Sender balance = %v, want %v", senderBalance, expectedSenderDelta)
	}

	// Verify receiver balance increased by value
	receiverBalance, ok := storage.balances[to]
	if !ok {
		t.Error("Receiver balance not tracked")
	} else if receiverBalance.Cmp(value) != 0 {
		t.Errorf("Receiver balance = %v, want %v", receiverBalance, value)
	}

	t.Logf("Balance tracking test passed:")
	t.Logf("  From: %s, Balance: %v", from.Hex(), senderBalance)
	t.Logf("  To: %s, Balance: %v", to.Hex(), receiverBalance)
	t.Logf("  Value transferred: %v Wei", value)
	t.Logf("  Gas cost: %v Wei", gasCost)
}

func (m *mockStorage) GetBlock(ctx context.Context, height uint64) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlock(ctx, height))
}

func (m *mockStorage) GetBlockByHash(ctx context.Context, hash common.Hash) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlockByHash(ctx, hash))
}

func (m *mockStorage) SetBlock(ctx context.Context, b *model.Block) error {
	gb, err := gethconv.BlockToGeth(b)
	if err != nil {
		return err
	}
	return m.gethSetBlock(ctx, gb)
}

func (m *mockStorage) SetReceipt(ctx context.Context, r *model.Receipt) error {
	return m.gethSetReceipt(ctx, gethconv.ReceiptToGeth(r))
}

func (m *mockStorage) GetBlocks(ctx context.Context, start, end uint64) ([]*model.Block, error) {
	var out []*model.Block
	for h := start; h <= end; h++ {
		if b, ok := m.blocks[h]; ok {
			mb, err := modelBlockOf(b, nil)
			if err != nil {
				return nil, err
			}
			out = append(out, mb)
		}
	}
	return out, nil
}

func (m *mockStorage) GetTransaction(ctx context.Context, hash common.Hash) (*model.Transaction, *port.TxLocation, error) {
	return nil, nil, port.ErrNotFound
}

func (m *mockStorage) GetReceipt(ctx context.Context, hash common.Hash) (*model.Receipt, error) {
	r, ok := m.receipts[hash]
	if !ok {
		return nil, port.ErrNotFound
	}
	return modelReceiptOf(r, nil)
}
