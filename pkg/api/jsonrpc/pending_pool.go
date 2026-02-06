package jsonrpc

import (
	"context"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/ethereum/go-ethereum/common"
)

const (
	// DefaultPendingPoolSize is the maximum number of pending transactions to track
	DefaultPendingPoolSize = 10000

	// DefaultPendingTxTTL is how long to keep pending transactions before expiring
	DefaultPendingTxTTL = 5 * time.Minute

	// PendingPoolCleanupInterval is how often to clean up expired transactions
	PendingPoolCleanupInterval = 30 * time.Second

	// PendingPoolSubscriptionID is the ID for the EventBus subscription
	PendingPoolSubscriptionID = "pending-pool"
)

// PendingTransaction represents a pending transaction in the pool
type PendingTransaction struct {
	Hash      common.Hash
	From      common.Address
	To        *common.Address
	Value     string
	Nonce     uint64
	GasPrice  string
	Gas       uint64
	Data      []byte
	SeenAt    time.Time
	SeenIndex uint64 // Monotonically increasing index for tracking
}

// PendingPool manages pending transactions for filter polling
type PendingPool struct {
	// transactions stores pending tx hashes in order of arrival
	transactions []common.Hash

	// txDetails stores additional transaction details by hash
	txDetails map[common.Hash]*PendingTransaction

	// nextIndex is the next sequence number for new transactions
	nextIndex uint64

	// maxSize is the maximum pool size
	maxSize int

	// ttl is how long to keep transactions
	ttl time.Duration

	// mu protects all fields
	mu sync.RWMutex

	// ctx and cancel for cleanup goroutine
	ctx    context.Context
	cancel context.CancelFunc

	// cleanupDone signals when cleanup goroutine exits
	cleanupDone chan struct{}

	// eventBus is the reference to the EventBus for unsubscription
	eventBus *events.EventBus

	// subscription holds the EventBus subscription
	subscription *events.Subscription
}

// NewPendingPool creates a new pending transaction pool
func NewPendingPool(maxSize int, ttl time.Duration) *PendingPool {
	if maxSize <= 0 {
		maxSize = DefaultPendingPoolSize
	}
	if ttl <= 0 {
		ttl = DefaultPendingTxTTL
	}

	ctx, cancel := context.WithCancel(context.Background())

	pool := &PendingPool{
		transactions: make([]common.Hash, 0, maxSize),
		txDetails:    make(map[common.Hash]*PendingTransaction),
		nextIndex:    1,
		maxSize:      maxSize,
		ttl:          ttl,
		ctx:          ctx,
		cancel:       cancel,
		cleanupDone:  make(chan struct{}),
	}

	// Start cleanup goroutine
	go pool.cleanupLoop()

	return pool
}

// SubscribeToEventBus subscribes to pending transaction events from the EventBus
func (p *PendingPool) SubscribeToEventBus(bus *events.EventBus) {
	if bus == nil {
		return
	}

	p.eventBus = bus

	// Subscribe to transaction events
	p.subscription = bus.Subscribe(
		events.SubscriptionID(PendingPoolSubscriptionID),
		[]events.EventType{events.EventTypeTransaction},
		nil, // No filter, we'll filter in the handler
		256, // Buffer size
	)

	// Start processing events
	go p.processEvents()
}

// processEvents processes events from the EventBus subscription
func (p *PendingPool) processEvents() {
	if p.subscription == nil {
		return
	}

	for {
		select {
		case <-p.ctx.Done():
			return
		case event, ok := <-p.subscription.Channel:
			if !ok {
				return
			}

			// Process transaction events
			txEvent, ok := event.(*events.TransactionEvent)
			if !ok {
				continue
			}

			// Only process pending transactions (BlockNumber == 0)
			if txEvent.BlockNumber == 0 {
				p.AddTransaction(txEvent)
			} else {
				// Transaction was mined, remove from pool
				p.RemoveTransaction(txEvent.Hash)
			}
		}
	}
}

// AddTransaction adds a pending transaction to the pool
func (p *PendingPool) AddTransaction(txEvent *events.TransactionEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	hash := txEvent.Hash

	// Check if already exists
	if _, exists := p.txDetails[hash]; exists {
		return
	}

	// Check if pool is full
	if len(p.transactions) >= p.maxSize {
		// Remove oldest transaction
		if len(p.transactions) > 0 {
			oldestHash := p.transactions[0]
			delete(p.txDetails, oldestHash)
			p.transactions = p.transactions[1:]
		}
	}

	// Add transaction
	pendingTx := &PendingTransaction{
		Hash:      hash,
		From:      txEvent.From,
		To:        txEvent.To,
		Value:     txEvent.Value,
		SeenAt:    time.Now(),
		SeenIndex: p.nextIndex,
	}

	if txEvent.Tx != nil {
		pendingTx.Nonce = txEvent.Tx.Nonce()
		pendingTx.GasPrice = txEvent.Tx.GasPrice().String()
		pendingTx.Gas = txEvent.Tx.Gas()
		pendingTx.Data = txEvent.Tx.Data()
	}

	p.transactions = append(p.transactions, hash)
	p.txDetails[hash] = pendingTx
	p.nextIndex++
}

// RemoveTransaction removes a transaction from the pool (when mined)
func (p *PendingPool) RemoveTransaction(hash common.Hash) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.txDetails[hash]; !exists {
		return
	}

	delete(p.txDetails, hash)

	// Remove from slice (not efficient but maintains order)
	for i, h := range p.transactions {
		if h == hash {
			p.transactions = append(p.transactions[:i], p.transactions[i+1:]...)
			break
		}
	}
}

// GetTransactionsSince returns all pending transactions with SeenIndex > sinceIndex
func (p *PendingPool) GetTransactionsSince(sinceIndex uint64) ([]common.Hash, uint64) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []common.Hash
	var maxIndex uint64 = sinceIndex

	for _, hash := range p.transactions {
		if tx, exists := p.txDetails[hash]; exists {
			if tx.SeenIndex > sinceIndex {
				result = append(result, hash)
				if tx.SeenIndex > maxIndex {
					maxIndex = tx.SeenIndex
				}
			}
		}
	}

	return result, maxIndex
}

// GetAllTransactions returns all pending transaction hashes
func (p *PendingPool) GetAllTransactions() []common.Hash {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]common.Hash, len(p.transactions))
	copy(result, p.transactions)
	return result
}

// GetTransaction returns details of a specific pending transaction
func (p *PendingPool) GetTransaction(hash common.Hash) (*PendingTransaction, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	tx, exists := p.txDetails[hash]
	return tx, exists
}

// Size returns the current number of pending transactions
func (p *PendingPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.transactions)
}

// CurrentIndex returns the current sequence index
func (p *PendingPool) CurrentIndex() uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.nextIndex - 1
}

// Close stops the pool and releases resources
func (p *PendingPool) Close() {
	p.cancel()

	// Unsubscribe from EventBus
	if p.eventBus != nil && p.subscription != nil {
		p.eventBus.Unsubscribe(p.subscription.ID)
	}

	<-p.cleanupDone
}

// cleanupLoop periodically removes expired transactions
func (p *PendingPool) cleanupLoop() {
	defer close(p.cleanupDone)

	ticker := time.NewTicker(PendingPoolCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.cleanup()
		}
	}
}

// cleanup removes expired transactions
func (p *PendingPool) cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	var toRemove []common.Hash

	for hash, tx := range p.txDetails {
		if now.Sub(tx.SeenAt) > p.ttl {
			toRemove = append(toRemove, hash)
		}
	}

	for _, hash := range toRemove {
		delete(p.txDetails, hash)
	}

	// Rebuild transactions slice without expired entries
	if len(toRemove) > 0 {
		newTransactions := make([]common.Hash, 0, len(p.transactions))
		for _, hash := range p.transactions {
			if _, exists := p.txDetails[hash]; exists {
				newTransactions = append(newTransactions, hash)
			}
		}
		p.transactions = newTransactions
	}
}
