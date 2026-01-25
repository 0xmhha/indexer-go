package watchlist

import (
	"encoding/binary"
	"hash"
	"hash/fnv"
	"math"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// BloomFilter implements a probabilistic data structure for fast address lookups.
// It provides O(k) time complexity for checking if an address might be in the set,
// where k is the number of hash functions.
type BloomFilter struct {
	bitset    []uint64
	size      uint64       // Number of bits
	hashCount uint         // Number of hash functions (k)
	count     uint64       // Number of items added
	mu        sync.RWMutex // Protects concurrent access
}

// BloomConfig holds configuration for creating a bloom filter
type BloomConfig struct {
	ExpectedItems     int     // Expected number of items
	FalsePositiveRate float64 // Desired false positive rate (e.g., 0.0001 for 0.01%)
}

// DefaultBloomConfig returns the default bloom filter configuration
// Configured for 100,000 addresses with 0.01% false positive rate
func DefaultBloomConfig() *BloomConfig {
	return &BloomConfig{
		ExpectedItems:     100000,
		FalsePositiveRate: 0.0001,
	}
}

// NewBloomFilter creates a new bloom filter with the given configuration
func NewBloomFilter(config *BloomConfig) *BloomFilter {
	if config == nil {
		config = DefaultBloomConfig()
	}

	// Calculate optimal size and hash count
	// m = -n * ln(p) / (ln(2)^2)
	// k = (m/n) * ln(2)
	n := float64(config.ExpectedItems)
	p := config.FalsePositiveRate

	// Optimal number of bits
	m := -n * math.Log(p) / (math.Ln2 * math.Ln2)

	// Optimal number of hash functions
	k := (m / n) * math.Ln2

	// Round up m to nearest multiple of 64 (for uint64 bitset)
	size := uint64(math.Ceil(m/64) * 64)

	return &BloomFilter{
		bitset:    make([]uint64, size/64),
		size:      size,
		hashCount: uint(math.Ceil(k)),
	}
}

// NewBloomFilterFromBytes creates a bloom filter from serialized bytes
func NewBloomFilterFromBytes(data []byte, hashCount uint) *BloomFilter {
	if len(data) == 0 {
		return NewBloomFilter(nil)
	}

	// Calculate size from data length
	size := uint64(len(data) * 8)
	bitset := make([]uint64, len(data)/8)

	// Deserialize bitset
	for i := 0; i < len(bitset); i++ {
		bitset[i] = binary.BigEndian.Uint64(data[i*8 : (i+1)*8])
	}

	return &BloomFilter{
		bitset:    bitset,
		size:      size,
		hashCount: hashCount,
	}
}

// Add adds an address to the bloom filter
func (bf *BloomFilter) Add(addr common.Address) {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	bf.addLocked(addr)
}

// addLocked adds an address without acquiring lock (caller must hold lock)
func (bf *BloomFilter) addLocked(addr common.Address) {
	for i := uint(0); i < bf.hashCount; i++ {
		idx := bf.hash(addr, i)
		bf.setBit(idx)
	}
	bf.count++
}

// AddBatch adds multiple addresses to the bloom filter
func (bf *BloomFilter) AddBatch(addrs []common.Address) {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	for _, addr := range addrs {
		bf.addLocked(addr)
	}
}

// MightContain checks if an address might be in the set
// Returns false if definitely not in set, true if possibly in set
func (bf *BloomFilter) MightContain(addr common.Address) bool {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	for i := uint(0); i < bf.hashCount; i++ {
		idx := bf.hash(addr, i)
		if !bf.getBit(idx) {
			return false
		}
	}
	return true
}

// MightContainAny checks if any of the addresses might be in the set
// Returns early on first potential match
func (bf *BloomFilter) MightContainAny(addrs []common.Address) bool {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	for _, addr := range addrs {
		found := true
		for i := uint(0); i < bf.hashCount; i++ {
			idx := bf.hash(addr, i)
			if !bf.getBit(idx) {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}

// Clear resets the bloom filter
func (bf *BloomFilter) Clear() {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	for i := range bf.bitset {
		bf.bitset[i] = 0
	}
	bf.count = 0
}

// Count returns the number of items added to the filter
func (bf *BloomFilter) Count() uint64 {
	bf.mu.RLock()
	defer bf.mu.RUnlock()
	return bf.count
}

// Size returns the size of the bitset in bits
func (bf *BloomFilter) Size() uint64 {
	return bf.size
}

// HashCount returns the number of hash functions used
func (bf *BloomFilter) HashCount() uint {
	return bf.hashCount
}

// FillRatio returns the ratio of set bits to total bits
func (bf *BloomFilter) FillRatio() float64 {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	var setBits uint64
	for _, word := range bf.bitset {
		setBits += uint64(popcount(word))
	}
	return float64(setBits) / float64(bf.size)
}

// EstimateFalsePositiveRate estimates the current false positive rate
func (bf *BloomFilter) EstimateFalsePositiveRate() float64 {
	fillRatio := bf.FillRatio()
	return math.Pow(fillRatio, float64(bf.hashCount))
}

// Bytes returns the serialized bloom filter bitset
func (bf *BloomFilter) Bytes() []byte {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	data := make([]byte, len(bf.bitset)*8)
	for i, word := range bf.bitset {
		binary.BigEndian.PutUint64(data[i*8:], word)
	}
	return data
}

// hash computes the i-th hash of an address
// Uses double hashing: h(i) = h1 + i*h2
func (bf *BloomFilter) hash(addr common.Address, i uint) uint64 {
	h1 := bf.fnvHash(addr.Bytes())
	h2 := bf.fnvHash(append(addr.Bytes(), byte(i)))
	return (h1 + uint64(i)*h2) % bf.size
}

// fnvHash computes FNV-1a hash of data
func (bf *BloomFilter) fnvHash(data []byte) uint64 {
	var h hash.Hash64 = fnv.New64a()
	h.Write(data)
	return h.Sum64()
}

// setBit sets the bit at index idx
func (bf *BloomFilter) setBit(idx uint64) {
	wordIdx := idx / 64
	bitIdx := idx % 64
	bf.bitset[wordIdx] |= 1 << bitIdx
}

// getBit gets the bit at index idx
func (bf *BloomFilter) getBit(idx uint64) bool {
	wordIdx := idx / 64
	bitIdx := idx % 64
	return bf.bitset[wordIdx]&(1<<bitIdx) != 0
}

// popcount counts the number of set bits in a uint64
func popcount(x uint64) int {
	// Using the Hamming weight algorithm
	x = x - ((x >> 1) & 0x5555555555555555)
	x = (x & 0x3333333333333333) + ((x >> 2) & 0x3333333333333333)
	x = (x + (x >> 4)) & 0x0f0f0f0f0f0f0f0f
	return int((x * 0x0101010101010101) >> 56)
}

// Merge merges another bloom filter into this one (union operation)
func (bf *BloomFilter) Merge(other *BloomFilter) error {
	if bf.size != other.size {
		return ErrBloomFilterSizeMismatch
	}

	bf.mu.Lock()
	defer bf.mu.Unlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	for i := range bf.bitset {
		bf.bitset[i] |= other.bitset[i]
	}
	return nil
}
