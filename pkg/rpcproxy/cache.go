package rpcproxy

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
)

// CacheEntry represents a single cache entry
type CacheEntry struct {
	Key       string
	Value     interface{}
	ExpiresAt time.Time
	element   *list.Element
	// used is set by Get and cleared when eviction passes the entry over
	used atomic.Bool
}

// IsExpired returns true if the entry has expired
func (e *CacheEntry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

// Cache is a thread-safe cache with TTL support and approximate LRU
// eviction (CLOCK): Get takes the lock shared and only marks the entry as
// used, so readers do not wait for each other; eviction moves a used entry
// to the front once instead of evicting it.
type Cache struct {
	mu        sync.RWMutex
	maxSize   int
	items     map[string]*CacheEntry
	lru       *list.List
	config    *CacheConfig
	hits      atomic.Int64
	misses    atomic.Int64
	evictions int64
	stop      chan struct{}
	stopOnce  sync.Once
}

// NewCache creates a new LRU cache with TTL support
func NewCache(config *CacheConfig) *Cache {
	if config == nil {
		config = DefaultCacheConfig()
	}

	c := &Cache{
		maxSize: config.MaxSize,
		items:   make(map[string]*CacheEntry),
		lru:     list.New(),
		config:  config,
		stop:    make(chan struct{}),
	}

	// Start background cleanup goroutine
	go c.cleanupLoop()

	return c
}

// Get retrieves a value from the cache. Expired entries are left for Set
// and the cleanup to remove.
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	entry, exists := c.items[key]
	var value interface{}
	if exists && !entry.IsExpired() {
		entry.used.Store(true)
		value = entry.Value
	} else {
		exists = false
	}
	c.mu.RUnlock()

	if !exists {
		c.misses.Add(1)
		return nil, false
	}
	c.hits.Add(1)
	return value, true
}

// Set stores a value in the cache with the specified TTL
func (c *Cache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if key already exists
	if entry, exists := c.items[key]; exists {
		// Update existing entry
		entry.Value = value
		entry.ExpiresAt = time.Now().Add(ttl)
		entry.used.Store(false)
		c.lru.MoveToFront(entry.element)
		return
	}

	// Evict if necessary
	for c.lru.Len() >= c.maxSize {
		c.evictOldest()
	}

	// Create new entry
	entry := &CacheEntry{
		Key:       key,
		Value:     value,
		ExpiresAt: time.Now().Add(ttl),
	}

	// Add to LRU list
	entry.element = c.lru.PushFront(entry)
	c.items[key] = entry
}

// SetWithDefaultTTL stores a value with the default TTL
func (c *Cache) SetWithDefaultTTL(key string, value interface{}) {
	c.Set(key, value, c.config.DefaultTTL)
}

// SetImmutable stores an immutable value (long TTL)
func (c *Cache) SetImmutable(key string, value interface{}) {
	c.Set(key, value, c.config.ImmutableTTL)
}

// SetBalance stores a balance value (short TTL)
func (c *Cache) SetBalance(key string, value interface{}) {
	c.Set(key, value, c.config.BalanceTTL)
}

// SetTokenMetadata stores token metadata (long TTL)
func (c *Cache) SetTokenMetadata(key string, value interface{}) {
	c.Set(key, value, c.config.TokenMetadataTTL)
}

// Delete removes a value from the cache
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, exists := c.items[key]; exists {
		c.removeEntry(entry)
	}
}

// Clear removes all entries from the cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*CacheEntry)
	c.lru.Init()
}

// Size returns the current number of entries in the cache
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Stats returns cache statistics
func (c *Cache) Stats() (hits, misses, evictions int64, size int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hits.Load(), c.misses.Load(), c.evictions, len(c.items)
}

// HitRate returns the cache hit rate (0.0 to 1.0)
func (c *Cache) HitRate() float64 {
	hits, misses := c.hits.Load(), c.misses.Load()
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// removeEntry removes an entry from the cache (must be called with lock held)
func (c *Cache) removeEntry(entry *CacheEntry) {
	c.lru.Remove(entry.element)
	delete(c.items, entry.Key)
}

// evictOldest removes the least recently used entry (must be called with
// lock held): entries used since eviction last passed them get a second
// chance at the front. Every entry is passed over at most once, so it ends.
func (c *Cache) evictOldest() {
	for range c.lru.Len() {
		oldest := c.lru.Back()
		entry, ok := oldest.Value.(*CacheEntry)
		if !ok {
			return
		}
		if entry.used.Swap(false) {
			c.lru.MoveToFront(oldest)
			continue
		}
		c.removeEntry(entry)
		c.evictions++
		return
	}
	if oldest := c.lru.Back(); oldest != nil {
		if entry, ok := oldest.Value.(*CacheEntry); ok {
			c.removeEntry(entry)
			c.evictions++
		}
	}
}

// cleanupLoop periodically removes expired entries
func (c *Cache) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.cleanup()
		}
	}
}

// Close stops the background cleanup.
func (c *Cache) Close() {
	c.stopOnce.Do(func() { close(c.stop) })
}

// cleanup removes all expired entries
func (c *Cache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for _, entry := range c.items {
		if now.After(entry.ExpiresAt) {
			c.removeEntry(entry)
		}
	}
}

// GetOrSet atomically gets a value or sets it using the provided function
func (c *Cache) GetOrSet(key string, ttl time.Duration, fn func() (interface{}, error)) (interface{}, bool, error) {
	// Try to get from cache first
	if value, ok := c.Get(key); ok {
		return value, true, nil
	}

	// Execute function to get value
	value, err := fn()
	if err != nil {
		return nil, false, err
	}

	// Store in cache
	c.Set(key, value, ttl)
	return value, false, nil
}

// CacheKeyBuilder helps build consistent cache keys
type CacheKeyBuilder struct {
	prefix string
}

// NewCacheKeyBuilder creates a new cache key builder
func NewCacheKeyBuilder(prefix string) *CacheKeyBuilder {
	return &CacheKeyBuilder{prefix: prefix}
}

// ContractCall builds a cache key for contract calls
func (b *CacheKeyBuilder) ContractCall(address string, method string, params string) string {
	return b.prefix + ":contract:" + address + ":" + method + ":" + params
}

// TransactionStatus builds a cache key for transaction status
func (b *CacheKeyBuilder) TransactionStatus(txHash string) string {
	return b.prefix + ":txstatus:" + txHash
}

// InternalTransactions builds a cache key for internal transactions
func (b *CacheKeyBuilder) InternalTransactions(txHash string) string {
	return b.prefix + ":internaltx:" + txHash
}

// TokenMetadata builds a cache key for token metadata
func (b *CacheKeyBuilder) TokenMetadata(address string, field string) string {
	return b.prefix + ":token:" + address + ":" + field
}

// Balance builds a cache key for balance
func (b *CacheKeyBuilder) Balance(address string, block string) string {
	return b.prefix + ":balance:" + address + ":" + block
}

// Nonce builds a cache key for nonce
func (b *CacheKeyBuilder) Nonce(address string, block string) string {
	return b.prefix + ":nonce:" + address + ":" + block
}

// Code builds a cache key for code
func (b *CacheKeyBuilder) Code(address string, block string) string {
	return b.prefix + ":code:" + address + ":" + block
}
