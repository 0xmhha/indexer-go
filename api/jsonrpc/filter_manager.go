package jsonrpc

import (
	"context"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// FilterType represents the type of filter
type FilterType int

const (
	// LogFilterType is for event log filtering
	LogFilterType FilterType = iota
	// BlockFilterType is for new block notifications
	BlockFilterType
	// PendingTxFilterType is for pending transaction notifications
	PendingTxFilterType
)

// Filter represents an active filter
type Filter struct {
	// ID is the unique filter identifier
	ID string

	// Type is the filter type
	Type FilterType

	// LogFilter contains the log filter criteria (only for LogFilterType)
	LogFilter *storage.LogFilter

	// LastPollBlock tracks the last block checked for changes
	LastPollBlock uint64

	// CreatedAt is when the filter was created
	CreatedAt time.Time

	// LastPollAt is when the filter was last polled
	LastPollAt time.Time

	// Decode indicates whether to decode logs using ABI
	Decode bool
}

// FilterManager manages active filters
type FilterManager struct {
	filters     map[string]*Filter
	mu          sync.RWMutex
	nextID      uint64
	timeout     time.Duration
	cleanupDone chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewFilterManager creates a new filter manager
func NewFilterManager(timeout time.Duration) *FilterManager {
	ctx, cancel := context.WithCancel(context.Background())
	fm := &FilterManager{
		filters:     make(map[string]*Filter),
		nextID:      1,
		timeout:     timeout,
		cleanupDone: make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Start cleanup goroutine
	go fm.cleanupLoop()

	return fm
}

// NewFilter creates a new filter and returns its ID
func (fm *FilterManager) NewFilter(filterType FilterType, logFilter *storage.LogFilter, lastBlock uint64, decode bool) string {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	id := fm.generateID()
	now := time.Now()

	filter := &Filter{
		ID:            id,
		Type:          filterType,
		LogFilter:     logFilter,
		LastPollBlock: lastBlock,
		CreatedAt:     now,
		LastPollAt:    now,
		Decode:        decode,
	}

	fm.filters[id] = filter
	return id
}

// GetFilter retrieves a filter by ID and updates last poll time
func (fm *FilterManager) GetFilter(id string) (*Filter, bool) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	filter, exists := fm.filters[id]
	if exists {
		filter.LastPollAt = time.Now()
	}
	return filter, exists
}

// UpdateLastPollBlock updates the last polled block for a filter
func (fm *FilterManager) UpdateLastPollBlock(id string, blockNumber uint64) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if filter, exists := fm.filters[id]; exists {
		filter.LastPollBlock = blockNumber
		filter.LastPollAt = time.Now()
	}
}

// RemoveFilter removes a filter by ID
func (fm *FilterManager) RemoveFilter(id string) bool {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	_, exists := fm.filters[id]
	if exists {
		delete(fm.filters, id)
	}
	return exists
}

// FilterCount returns the number of active filters
func (fm *FilterManager) FilterCount() int {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return len(fm.filters)
}

// Close stops the filter manager and cleanup goroutine
func (fm *FilterManager) Close() {
	fm.cancel()
	<-fm.cleanupDone
}

// generateID generates a unique filter ID
func (fm *FilterManager) generateID() string {
	id := fm.nextID
	fm.nextID++
	hash := common.Hash{}
	hash.SetBytes(common.LeftPadBytes([]byte{byte(id)}, 32))
	return hash.Hex()
}

// cleanupLoop periodically removes expired filters
func (fm *FilterManager) cleanupLoop() {
	defer close(fm.cleanupDone)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-fm.ctx.Done():
			return
		case <-ticker.C:
			fm.cleanup()
		}
	}
}

// cleanup removes expired filters
func (fm *FilterManager) cleanup() {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	now := time.Now()
	for id, filter := range fm.filters {
		if now.Sub(filter.LastPollAt) > fm.timeout {
			delete(fm.filters, id)
		}
	}
}

// GetLogsSinceLastPoll returns new logs since the last poll for a log filter
func (fm *FilterManager) GetLogsSinceLastPoll(ctx context.Context, store storage.Storage, filterID string) ([]*types.Log, uint64, error) {
	filter, exists := fm.GetFilter(filterID)
	if !exists {
		return nil, 0, nil
	}

	if filter.Type != LogFilterType {
		return nil, 0, nil
	}

	// Get current block height
	currentHeight, err := store.GetLatestHeight(ctx)
	if err != nil && err != storage.ErrNotFound {
		return nil, 0, err
	}

	// If no new blocks, return empty
	if currentHeight <= filter.LastPollBlock {
		return nil, currentHeight, nil
	}

	// Create a filter for new blocks only
	logFilter := &storage.LogFilter{
		FromBlock: filter.LastPollBlock + 1,
		ToBlock:   currentHeight,
		Addresses: filter.LogFilter.Addresses,
		Topics:    filter.LogFilter.Topics,
	}

	// Get logs
	logs, err := store.GetLogs(ctx, logFilter)
	if err != nil {
		return nil, 0, err
	}

	return logs, currentHeight, nil
}

// GetBlockHashesSinceLastPoll returns new block hashes since the last poll for a block filter
func (fm *FilterManager) GetBlockHashesSinceLastPoll(ctx context.Context, store storage.Storage, filterID string) ([]common.Hash, uint64, error) {
	filter, exists := fm.GetFilter(filterID)
	if !exists {
		return nil, 0, nil
	}

	if filter.Type != BlockFilterType {
		return nil, 0, nil
	}

	// Get current block height
	currentHeight, err := store.GetLatestHeight(ctx)
	if err != nil && err != storage.ErrNotFound {
		return nil, 0, err
	}

	// If no new blocks, return empty
	if currentHeight <= filter.LastPollBlock {
		return nil, currentHeight, nil
	}

	// Get block hashes for new blocks
	var hashes []common.Hash
	for blockNum := filter.LastPollBlock + 1; blockNum <= currentHeight; blockNum++ {
		block, err := store.GetBlock(ctx, blockNum)
		if err != nil {
			if err == storage.ErrNotFound {
				continue
			}
			return nil, 0, err
		}
		hashes = append(hashes, block.Hash())
	}

	return hashes, currentHeight, nil
}

// GetPendingTransactionsSinceLastPoll returns new pending transactions
// Note: This is a placeholder as we don't track pending transactions yet
func (fm *FilterManager) GetPendingTransactionsSinceLastPoll(ctx context.Context, store storage.Storage, filterID string) ([]common.Hash, error) {
	filter, exists := fm.GetFilter(filterID)
	if !exists {
		return nil, nil
	}

	if filter.Type != PendingTxFilterType {
		return nil, nil
	}

	// TODO: Implement pending transaction tracking
	// For now, return empty list
	return []common.Hash{}, nil
}
