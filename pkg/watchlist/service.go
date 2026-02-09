package watchlist

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Service defines the watchlist service interface
type Service interface {
	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	// Address management
	WatchAddress(ctx context.Context, req *WatchRequest) (*WatchedAddress, error)
	UnwatchAddress(ctx context.Context, addressID string) error
	GetWatchedAddress(ctx context.Context, addressID string) (*WatchedAddress, error)
	ListWatchedAddresses(ctx context.Context, filter *ListFilter) ([]*WatchedAddress, error)
	GetWatchedAddressByEthAddress(ctx context.Context, chainID string, addr common.Address) (*WatchedAddress, error)

	// Subscriber management
	Subscribe(ctx context.Context, addressID string, subscriber *Subscriber) (string, error)
	Unsubscribe(ctx context.Context, subscriptionID string) error

	// Event processing (called by Fetcher)
	ProcessBlock(ctx context.Context, chainID string, block *types.Block, receipts []*types.Receipt) error

	// Event retrieval
	GetRecentEvents(ctx context.Context, addressID string, limit int) ([]*WatchEvent, error)
}

// WatchlistService implements the Service interface
type WatchlistService struct {
	config       *Config
	storage      storage.Storage
	eventBus     *events.EventBus
	logger       *zap.Logger
	matcher      *EventMatcher
	subscribers  map[string]*Subscriber // subscriptionID -> Subscriber
	addrSubs     map[string][]string    // addressID -> []subscriptionID

	ctx        context.Context
	cancelFunc context.CancelFunc
	mu         sync.RWMutex
	subMu      sync.RWMutex
	isRunning  bool
}

// Config holds configuration for the watchlist service
type Config struct {
	Enabled           bool          `yaml:"enabled"`
	BloomFilter       *BloomConfig  `yaml:"bloom_filter"`
	HistoryRetention  time.Duration `yaml:"history_retention"` // How long to keep events
	MaxAddressesTotal int           `yaml:"max_addresses"`     // Max total watched addresses
}

// DefaultConfig returns the default watchlist service configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:           true,
		BloomFilter:       DefaultBloomConfig(),
		HistoryRetention:  720 * time.Hour, // 30 days
		MaxAddressesTotal: 100000,
	}
}

// NewService creates a new watchlist service
func NewService(
	config *Config,
	storage storage.Storage,
	eventBus *events.EventBus,
	logger *zap.Logger,
) *WatchlistService {
	if config == nil {
		config = DefaultConfig()
	}

	return &WatchlistService{
		config:      config,
		storage:     storage,
		eventBus:    eventBus,
		logger:      logger.Named("watchlist"),
		matcher:     NewEventMatcher(),
		subscribers: make(map[string]*Subscriber),
		addrSubs:    make(map[string][]string),
	}
}

// Start initializes and starts the watchlist service
func (s *WatchlistService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRunning {
		return nil
	}

	s.ctx, s.cancelFunc = context.WithCancel(ctx)
	s.logger.Info("starting watchlist service")

	// Load existing watched addresses from storage
	if err := s.loadWatchedAddresses(ctx); err != nil {
		return err
	}

	s.isRunning = true
	s.logger.Info("watchlist service started",
		zap.Int("watchedAddresses", s.matcher.GetWatchedAddressCount("")),
	)

	return nil
}

// Stop gracefully stops the watchlist service
func (s *WatchlistService) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return nil
	}

	s.logger.Info("stopping watchlist service")

	if s.cancelFunc != nil {
		s.cancelFunc()
	}

	s.isRunning = false
	s.logger.Info("watchlist service stopped")

	return nil
}

// WatchAddress adds an address to the watchlist
func (s *WatchlistService) WatchAddress(ctx context.Context, req *WatchRequest) (*WatchedAddress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate request
	if req.Address == (common.Address{}) {
		return nil, ErrInvalidAddress
	}

	if req.Filter == nil {
		req.Filter = DefaultWatchFilter()
	}

	// Check if address is already watched
	existing, err := s.getWatchedAddressByEthAddressLocked(ctx, req.ChainID, req.Address)
	if err == nil && existing != nil {
		return nil, ErrAddressAlreadyExists
	}

	// Create new watched address
	now := time.Now()
	watched := &WatchedAddress{
		ID:        uuid.New().String(),
		Address:   req.Address,
		ChainID:   req.ChainID,
		Label:     req.Label,
		Filter:    req.Filter,
		CreatedAt: now,
		UpdatedAt: now,
		Stats: &WatchStats{
			TotalEvents: 0,
		},
	}

	// Store watched address
	if err := s.storeWatchedAddress(ctx, watched); err != nil {
		return nil, err
	}

	// Add to matcher
	s.matcher.AddAddress(watched)

	// Store bloom filter
	if bf := s.matcher.GetBloomFilter(req.ChainID); bf != nil {
		if err := s.storeBloomFilter(ctx, req.ChainID, bf); err != nil {
			s.logger.Warn("failed to store bloom filter", zap.Error(err))
		}
	}

	s.logger.Info("address added to watchlist",
		zap.String("id", watched.ID),
		zap.String("address", watched.Address.Hex()),
		zap.String("chainId", watched.ChainID),
	)

	return watched, nil
}

// UnwatchAddress removes an address from the watchlist
func (s *WatchlistService) UnwatchAddress(ctx context.Context, addressID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get the watched address
	watched, err := s.getWatchedAddressLocked(ctx, addressID)
	if err != nil {
		return err
	}

	// Remove from matcher
	s.matcher.RemoveAddress(watched.ChainID, watched.Address)

	// Delete from storage
	if err := s.deleteWatchedAddress(ctx, watched); err != nil {
		return err
	}

	// Remove subscribers for this address
	s.subMu.Lock()
	for _, subID := range s.addrSubs[addressID] {
		delete(s.subscribers, subID)
	}
	delete(s.addrSubs, addressID)
	s.subMu.Unlock()

	s.logger.Info("address removed from watchlist",
		zap.String("id", addressID),
		zap.String("address", watched.Address.Hex()),
	)

	return nil
}

// GetWatchedAddress returns a watched address by ID
func (s *WatchlistService) GetWatchedAddress(ctx context.Context, addressID string) (*WatchedAddress, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getWatchedAddressLocked(ctx, addressID)
}

// GetWatchedAddressByEthAddress returns a watched address by ethereum address
func (s *WatchlistService) GetWatchedAddressByEthAddress(ctx context.Context, chainID string, addr common.Address) (*WatchedAddress, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getWatchedAddressByEthAddressLocked(ctx, chainID, addr)
}

// ListWatchedAddresses returns all watched addresses matching the filter
func (s *WatchlistService) ListWatchedAddresses(ctx context.Context, filter *ListFilter) ([]*WatchedAddress, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if filter == nil {
		filter = &ListFilter{Limit: 100}
	}
	if filter.Limit <= 0 {
		filter.Limit = 100
	}

	// Determine prefix based on filter
	var prefix []byte
	if filter.ChainID != "" {
		prefix = ChainAddressesKeyPrefix(filter.ChainID)
	} else {
		prefix = WatchedAddressKeyPrefix()
	}

	// Iterate over addresses
	addresses := make([]*WatchedAddress, 0, filter.Limit)
	count := 0
	skipped := 0

	err := s.storage.Iterate(ctx, prefix, func(key, value []byte) bool {
		// Handle offset
		if skipped < filter.Offset {
			skipped++
			return true
		}

		// Handle limit
		if count >= filter.Limit {
			return false
		}

		// For chain-specific queries, the value is the addressID
		// We need to fetch the actual address data
		var watched *WatchedAddress
		if filter.ChainID != "" {
			addressID := string(value)
			var err error
			watched, err = s.getWatchedAddressLocked(ctx, addressID)
			if err != nil {
				return true // Skip on error
			}
		} else {
			// Direct address data
			if err := json.Unmarshal(value, &watched); err != nil {
				return true // Skip on error
			}
		}

		addresses = append(addresses, watched)
		count++
		return true
	})

	if err != nil {
		return nil, NewWatchlistError("list", err)
	}

	return addresses, nil
}

// Subscribe creates a subscription to watch events for an address
func (s *WatchlistService) Subscribe(ctx context.Context, addressID string, subscriber *Subscriber) (string, error) {
	s.subMu.Lock()
	defer s.subMu.Unlock()

	// Verify address exists
	s.mu.RLock()
	_, err := s.getWatchedAddressLocked(ctx, addressID)
	s.mu.RUnlock()
	if err != nil {
		return "", err
	}

	// Generate subscription ID if not provided
	if subscriber.ID == "" {
		subscriber.ID = uuid.New().String()
	}
	subscriber.AddressID = addressID
	subscriber.CreatedAt = time.Now()

	// Store subscriber
	s.subscribers[subscriber.ID] = subscriber
	s.addrSubs[addressID] = append(s.addrSubs[addressID], subscriber.ID)

	s.logger.Debug("subscription created",
		zap.String("subscriptionId", subscriber.ID),
		zap.String("addressId", addressID),
	)

	return subscriber.ID, nil
}

// Unsubscribe removes a subscription
func (s *WatchlistService) Unsubscribe(ctx context.Context, subscriptionID string) error {
	s.subMu.Lock()
	defer s.subMu.Unlock()

	sub, exists := s.subscribers[subscriptionID]
	if !exists {
		return ErrSubscriberNotFound
	}

	// Remove from subscribers
	delete(s.subscribers, subscriptionID)

	// Remove from address subscriptions
	if subs, ok := s.addrSubs[sub.AddressID]; ok {
		filtered := make([]string, 0, len(subs)-1)
		for _, id := range subs {
			if id != subscriptionID {
				filtered = append(filtered, id)
			}
		}
		s.addrSubs[sub.AddressID] = filtered
	}

	s.logger.Debug("subscription removed",
		zap.String("subscriptionId", subscriptionID),
	)

	return nil
}

// ProcessBlock processes a block for watched address events
func (s *WatchlistService) ProcessBlock(ctx context.Context, chainID string, block *types.Block, receipts []*types.Receipt) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.isRunning {
		return ErrServiceNotRunning
	}

	// Quick check if we have any watched addresses for this chain
	if !s.matcher.HasWatchedAddresses(chainID) {
		return nil
	}

	// Build receipt map for quick lookup
	receiptMap := make(map[common.Hash]*types.Receipt, len(receipts))
	for _, receipt := range receipts {
		receiptMap[receipt.TxHash] = receipt
	}

	// Process transactions
	for _, tx := range block.Transactions() {
		receipt := receiptMap[tx.Hash()]
		if receipt == nil {
			continue
		}

		// Match transaction
		txEvents := s.matcher.MatchTransaction(
			chainID,
			tx,
			receipt,
			block.NumberU64(),
			block.Hash(),
			block.Time(),
		)

		// Process matched events
		for _, event := range txEvents {
			if err := s.processMatchedEvent(ctx, event); err != nil {
				s.logger.Warn("failed to process matched event",
					zap.Error(err),
					zap.String("eventType", string(event.EventType)),
				)
			}
		}

		// Match logs for ERC20/ERC721 transfers
		logEvents := s.matcher.MatchLogs(
			chainID,
			receipt.Logs,
			block.NumberU64(),
			block.Hash(),
			block.Time(),
		)

		for _, event := range logEvents {
			if err := s.processMatchedEvent(ctx, event); err != nil {
				s.logger.Warn("failed to process matched event",
					zap.Error(err),
					zap.String("eventType", string(event.EventType)),
				)
			}
		}
	}

	return nil
}

// GetRecentEvents returns recent events for a watched address
func (s *WatchlistService) GetRecentEvents(ctx context.Context, addressID string, limit int) ([]*WatchEvent, error) {
	if limit <= 0 {
		limit = 50
	}

	// Iterate over event index for this address
	prefix := EventIndexKeyPrefix(addressID)
	eventsList := make([]*WatchEvent, 0, limit)

	err := s.storage.Iterate(ctx, prefix, func(key, value []byte) bool {
		if len(eventsList) >= limit {
			return false
		}

		var event WatchEvent
		if err := json.Unmarshal(value, &event); err != nil {
			return true // Skip on error
		}

		eventsList = append(eventsList, &event)
		return true
	})

	if err != nil {
		return nil, NewWatchlistError("get_events", err)
	}

	return eventsList, nil
}

// processMatchedEvent processes a matched event
func (s *WatchlistService) processMatchedEvent(ctx context.Context, event *WatchEvent) error {
	// Generate event ID
	event.ID = uuid.New().String()
	event.Timestamp = time.Now()

	// Store event
	if err := s.storeEvent(ctx, event); err != nil {
		return err
	}

	// Update address stats
	if err := s.updateAddressStats(ctx, event.AddressID, event.EventType); err != nil {
		s.logger.Warn("failed to update address stats", zap.Error(err))
	}

	// Publish to event bus
	s.publishEvent(event)

	// Notify subscribers
	s.notifySubscribers(event)

	return nil
}

// publishEvent publishes an event to the event bus
func (s *WatchlistService) publishEvent(event *WatchEvent) {
	if s.eventBus == nil {
		return
	}

	// Create event bus event
	busEvent := &WatchEventBusEvent{
		WatchEvent: event,
	}

	s.eventBus.Publish(busEvent)
}

// notifySubscribers notifies all subscribers of an event
func (s *WatchlistService) notifySubscribers(event *WatchEvent) {
	s.subMu.RLock()
	defer s.subMu.RUnlock()

	subs := s.addrSubs[event.AddressID]
	for _, subID := range subs {
		if sub, ok := s.subscribers[subID]; ok {
			// Update last delivery time
			sub.LastDelivery = time.Now()

			s.logger.Debug("notifying subscriber",
				zap.String("subscriptionId", subID),
				zap.String("eventId", event.ID),
			)
		}
	}
}

// loadWatchedAddresses loads all watched addresses from storage
func (s *WatchlistService) loadWatchedAddresses(ctx context.Context) error {
	prefix := WatchedAddressKeyPrefix()
	count := 0

	err := s.storage.Iterate(ctx, prefix, func(key, value []byte) bool {
		var watched WatchedAddress
		if err := json.Unmarshal(value, &watched); err != nil {
			s.logger.Warn("failed to unmarshal watched address",
				zap.Error(err),
				zap.String("key", string(key)),
			)
			return true // Continue
		}

		s.matcher.AddAddress(&watched)
		count++
		return true
	})

	if err != nil {
		return NewWatchlistError("load", err)
	}

	s.logger.Info("loaded watched addresses", zap.Int("count", count))
	return nil
}

// storeWatchedAddress stores a watched address in storage
func (s *WatchlistService) storeWatchedAddress(ctx context.Context, watched *WatchedAddress) error {
	data, err := json.Marshal(watched)
	if err != nil {
		return NewWatchlistError("marshal", err)
	}

	// Store main record
	if err := s.storage.Put(ctx, WatchedAddressKey(watched.ID), data); err != nil {
		return NewWatchlistError("store", err)
	}

	// Store chain index
	if err := s.storage.Put(ctx, ChainAddressesKey(watched.ChainID, watched.ID), []byte(watched.ID)); err != nil {
		return NewWatchlistError("store_index", err)
	}

	// Store address lookup index
	if err := s.storage.Put(ctx, AddressByEthAddressKey(watched.ChainID, watched.Address), []byte(watched.ID)); err != nil {
		return NewWatchlistError("store_addr_index", err)
	}

	return nil
}

// deleteWatchedAddress deletes a watched address from storage
func (s *WatchlistService) deleteWatchedAddress(ctx context.Context, watched *WatchedAddress) error {
	// Delete main record
	if err := s.storage.Delete(ctx, WatchedAddressKey(watched.ID)); err != nil {
		return NewWatchlistError("delete", err)
	}

	// Delete chain index
	if err := s.storage.Delete(ctx, ChainAddressesKey(watched.ChainID, watched.ID)); err != nil {
		s.logger.Warn("failed to delete chain index", zap.Error(err))
	}

	// Delete address lookup index
	if err := s.storage.Delete(ctx, AddressByEthAddressKey(watched.ChainID, watched.Address)); err != nil {
		s.logger.Warn("failed to delete address index", zap.Error(err))
	}

	return nil
}

// getWatchedAddressLocked retrieves a watched address (must hold read lock)
func (s *WatchlistService) getWatchedAddressLocked(ctx context.Context, addressID string) (*WatchedAddress, error) {
	data, err := s.storage.Get(ctx, WatchedAddressKey(addressID))
	if err != nil {
		if err == storage.ErrNotFound {
			return nil, ErrAddressNotFound
		}
		return nil, NewWatchlistError("get", err)
	}

	var watched WatchedAddress
	if err := json.Unmarshal(data, &watched); err != nil {
		return nil, NewWatchlistError("unmarshal", err)
	}

	return &watched, nil
}

// getWatchedAddressByEthAddressLocked retrieves a watched address by ethereum address
func (s *WatchlistService) getWatchedAddressByEthAddressLocked(ctx context.Context, chainID string, addr common.Address) (*WatchedAddress, error) {
	// Lookup address ID
	data, err := s.storage.Get(ctx, AddressByEthAddressKey(chainID, addr))
	if err != nil {
		if err == storage.ErrNotFound {
			return nil, ErrAddressNotFound
		}
		return nil, NewWatchlistError("lookup", err)
	}

	addressID := string(data)
	return s.getWatchedAddressLocked(ctx, addressID)
}

// storeBloomFilter stores a bloom filter to storage
func (s *WatchlistService) storeBloomFilter(ctx context.Context, chainID string, bf *BloomFilter) error {
	data := bf.Bytes()
	return s.storage.Put(ctx, BloomFilterKey(chainID), data)
}

// storeEvent stores a watch event
func (s *WatchlistService) storeEvent(ctx context.Context, event *WatchEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return NewWatchlistError("marshal_event", err)
	}

	// Store event data
	logIndex := uint(0)
	if event.LogIndex != nil {
		logIndex = *event.LogIndex
	}
	eventKey := WatchEventKey(event.ChainID, event.BlockNumber, event.TxHash, logIndex)
	if err := s.storage.Put(ctx, eventKey, data); err != nil {
		return NewWatchlistError("store_event", err)
	}

	// Store event index
	indexKey := EventIndexKey(event.AddressID, event.Timestamp.UnixNano(), event.ID)
	if err := s.storage.Put(ctx, indexKey, data); err != nil {
		return NewWatchlistError("store_event_index", err)
	}

	return nil
}

// updateAddressStats updates statistics for a watched address
func (s *WatchlistService) updateAddressStats(ctx context.Context, addressID string, eventType WatchEventType) error {
	watched, err := s.getWatchedAddressLocked(ctx, addressID)
	if err != nil {
		return err
	}

	if watched.Stats == nil {
		watched.Stats = &WatchStats{}
	}

	// Update stats
	watched.Stats.TotalEvents++
	watched.Stats.LastEventAt = time.Now()

	switch eventType {
	case WatchEventTypeTxFrom:
		watched.Stats.TxFromCount++
	case WatchEventTypeTxTo:
		watched.Stats.TxToCount++
	case WatchEventTypeERC20Transfer:
		watched.Stats.ERC20Count++
	case WatchEventTypeERC721Transfer:
		watched.Stats.ERC721Count++
	case WatchEventTypeLog:
		watched.Stats.LogCount++
	}

	watched.UpdatedAt = time.Now()

	// Store updated address
	data, err := json.Marshal(watched)
	if err != nil {
		return err
	}

	return s.storage.Put(ctx, WatchedAddressKey(addressID), data)
}

// WatchEventBusEvent wraps a WatchEvent for the event bus
type WatchEventBusEvent struct {
	WatchEvent *WatchEvent
}

// Type returns the event type
func (e *WatchEventBusEvent) Type() events.EventType {
	return events.EventType("watch_event")
}

// Timestamp returns when the event was created
func (e *WatchEventBusEvent) Timestamp() time.Time {
	if e.WatchEvent != nil {
		return e.WatchEvent.Timestamp
	}
	return time.Time{}
}
