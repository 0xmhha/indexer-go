package rpcproxy

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func smallCacheConfig() *CacheConfig {
	return &CacheConfig{
		MaxSize:          5,
		DefaultTTL:       time.Second,
		ImmutableTTL:     time.Hour,
		BalanceTTL:       500 * time.Millisecond,
		TokenMetadataTTL: time.Hour,
	}
}

// ========== Cache ==========

func TestCache_SetAndGet(t *testing.T) {
	c := NewCache(smallCacheConfig())

	c.Set("key1", "value1", time.Minute)
	val, ok := c.Get("key1")

	require.True(t, ok)
	assert.Equal(t, "value1", val)
}

func TestCache_GetMiss(t *testing.T) {
	c := NewCache(smallCacheConfig())

	val, ok := c.Get("nonexistent")

	assert.False(t, ok)
	assert.Nil(t, val)
}

func TestCache_TTLExpiration(t *testing.T) {
	c := NewCache(smallCacheConfig())

	c.Set("short", "data", 50*time.Millisecond)

	// Should exist immediately
	val, ok := c.Get("short")
	require.True(t, ok)
	assert.Equal(t, "data", val)

	// Wait for expiration
	time.Sleep(80 * time.Millisecond)

	val, ok = c.Get("short")
	assert.False(t, ok)
	assert.Nil(t, val)
}

func TestCache_LRUEviction(t *testing.T) {
	c := NewCache(smallCacheConfig()) // maxSize=5

	// Fill cache
	for i := 0; i < 5; i++ {
		c.Set(string(rune('a'+i)), i, time.Minute)
	}
	assert.Equal(t, 5, c.Size())

	// Add one more — should evict oldest ("a")
	c.Set("f", 5, time.Minute)
	assert.Equal(t, 5, c.Size())

	_, ok := c.Get("a")
	assert.False(t, ok, "oldest entry 'a' should be evicted")

	val, ok := c.Get("f")
	require.True(t, ok)
	assert.Equal(t, 5, val)
}

func TestCache_LRUAccessOrder(t *testing.T) {
	c := NewCache(smallCacheConfig()) // maxSize=5

	for i := 0; i < 5; i++ {
		c.Set(string(rune('a'+i)), i, time.Minute)
	}

	// Access "a" to move it to front
	c.Get("a")

	// Add new entry — "b" should be evicted (oldest not-accessed)
	c.Set("f", 5, time.Minute)

	_, ok := c.Get("a")
	assert.True(t, ok, "'a' was recently accessed, should survive")

	_, ok = c.Get("b")
	assert.False(t, ok, "'b' should be evicted as least recently used")
}

func TestCache_UpdateExisting(t *testing.T) {
	c := NewCache(smallCacheConfig())

	c.Set("key", "old", time.Minute)
	c.Set("key", "new", time.Minute)

	assert.Equal(t, 1, c.Size())
	val, ok := c.Get("key")
	require.True(t, ok)
	assert.Equal(t, "new", val)
}

func TestCache_Delete(t *testing.T) {
	c := NewCache(smallCacheConfig())

	c.Set("key", "value", time.Minute)
	c.Delete("key")

	_, ok := c.Get("key")
	assert.False(t, ok)
	assert.Equal(t, 0, c.Size())
}

func TestCache_DeleteNonexistent(t *testing.T) {
	c := NewCache(smallCacheConfig())

	// Should not panic
	c.Delete("nonexistent")
	assert.Equal(t, 0, c.Size())
}

func TestCache_Clear(t *testing.T) {
	c := NewCache(smallCacheConfig())

	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)
	c.Clear()

	assert.Equal(t, 0, c.Size())
	_, ok := c.Get("a")
	assert.False(t, ok)
}

func TestCache_Stats(t *testing.T) {
	c := NewCache(smallCacheConfig())

	c.Set("a", 1, time.Minute)
	c.Get("a")          // hit
	c.Get("nonexistent") // miss
	c.Get("a")          // hit

	hits, misses, _, size := c.Stats()
	assert.Equal(t, int64(2), hits)
	assert.Equal(t, int64(1), misses)
	assert.Equal(t, 1, size)
}

func TestCache_HitRate(t *testing.T) {
	c := NewCache(smallCacheConfig())

	// No requests yet
	assert.Equal(t, 0.0, c.HitRate())

	c.Set("a", 1, time.Minute)
	c.Get("a")    // hit
	c.Get("miss") // miss

	assert.InDelta(t, 0.5, c.HitRate(), 0.001)
}

func TestCache_EvictionStats(t *testing.T) {
	c := NewCache(smallCacheConfig()) // maxSize=5

	// Fill and overflow
	for i := 0; i < 7; i++ {
		c.Set(string(rune('a'+i)), i, time.Minute)
	}

	_, _, evictions, _ := c.Stats()
	assert.Equal(t, int64(2), evictions)
}

func TestCache_SetWithDefaultTTL(t *testing.T) {
	c := NewCache(smallCacheConfig())

	c.SetWithDefaultTTL("key", "value")
	val, ok := c.Get("key")
	require.True(t, ok)
	assert.Equal(t, "value", val)
}

func TestCache_SetImmutable(t *testing.T) {
	c := NewCache(smallCacheConfig())

	c.SetImmutable("key", "immutable-data")
	val, ok := c.Get("key")
	require.True(t, ok)
	assert.Equal(t, "immutable-data", val)
}

func TestCache_SetBalance(t *testing.T) {
	c := NewCache(smallCacheConfig())

	c.SetBalance("key", "100")
	val, ok := c.Get("key")
	require.True(t, ok)
	assert.Equal(t, "100", val)
}

func TestCache_SetTokenMetadata(t *testing.T) {
	c := NewCache(smallCacheConfig())

	c.SetTokenMetadata("key", "USDC")
	val, ok := c.Get("key")
	require.True(t, ok)
	assert.Equal(t, "USDC", val)
}

func TestCache_GetOrSet_Hit(t *testing.T) {
	c := NewCache(smallCacheConfig())

	c.Set("key", "existing", time.Minute)

	called := false
	val, fromCache, err := c.GetOrSet("key", time.Minute, func() (interface{}, error) {
		called = true
		return "new", nil
	})

	require.NoError(t, err)
	assert.True(t, fromCache)
	assert.Equal(t, "existing", val)
	assert.False(t, called, "function should not be called on cache hit")
}

func TestCache_GetOrSet_Miss(t *testing.T) {
	c := NewCache(smallCacheConfig())

	val, fromCache, err := c.GetOrSet("key", time.Minute, func() (interface{}, error) {
		return "computed", nil
	})

	require.NoError(t, err)
	assert.False(t, fromCache)
	assert.Equal(t, "computed", val)

	// Should now be cached
	cached, ok := c.Get("key")
	require.True(t, ok)
	assert.Equal(t, "computed", cached)
}

func TestCache_GetOrSet_Error(t *testing.T) {
	c := NewCache(smallCacheConfig())

	expectedErr := errors.New("compute failed")
	val, fromCache, err := c.GetOrSet("key", time.Minute, func() (interface{}, error) {
		return nil, expectedErr
	})

	assert.ErrorIs(t, err, expectedErr)
	assert.False(t, fromCache)
	assert.Nil(t, val)

	// Should NOT be cached
	_, ok := c.Get("key")
	assert.False(t, ok)
}

func TestCache_NilConfig(t *testing.T) {
	c := NewCache(nil)
	require.NotNil(t, c)
	assert.Equal(t, 10000, c.maxSize) // DefaultCacheConfig
}

func TestCache_ConcurrentAccess(t *testing.T) {
	c := NewCache(&CacheConfig{
		MaxSize:          100,
		DefaultTTL:       time.Minute,
		ImmutableTTL:     time.Hour,
		BalanceTTL:       time.Second,
		TokenMetadataTTL: time.Hour,
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := string(rune('a' + i%26))
			c.Set(key, i, time.Minute)
			c.Get(key)
			c.Size()
			c.Stats()
		}(i)
	}
	wg.Wait()

	// Should not panic or deadlock
	assert.True(t, c.Size() > 0)
}

// ========== CacheKeyBuilder ==========

func TestCacheKeyBuilder(t *testing.T) {
	b := NewCacheKeyBuilder("proxy")

	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{"ContractCall", b.ContractCall("0xAddr", "transfer", "params"), "proxy:contract:0xAddr:transfer:params"},
		{"TransactionStatus", b.TransactionStatus("0xHash"), "proxy:txstatus:0xHash"},
		{"InternalTransactions", b.InternalTransactions("0xHash"), "proxy:internaltx:0xHash"},
		{"TokenMetadata", b.TokenMetadata("0xAddr", "name"), "proxy:token:0xAddr:name"},
		{"Balance", b.Balance("0xAddr", "latest"), "proxy:balance:0xAddr:latest"},
		{"Nonce", b.Nonce("0xAddr", "latest"), "proxy:nonce:0xAddr:latest"},
		{"Code", b.Code("0xAddr", "latest"), "proxy:code:0xAddr:latest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.key)
		})
	}
}

// ========== CacheEntry ==========

func TestCacheEntry_IsExpired(t *testing.T) {
	entry := &CacheEntry{
		ExpiresAt: time.Now().Add(-time.Second),
	}
	assert.True(t, entry.IsExpired())

	entry.ExpiresAt = time.Now().Add(time.Hour)
	assert.False(t, entry.IsExpired())
}
