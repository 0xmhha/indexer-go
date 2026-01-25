package watchlist

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestNewBloomFilter(t *testing.T) {
	bf := NewBloomFilter(nil) // Use default config

	if bf == nil {
		t.Fatal("expected bloom filter to not be nil")
	}

	if bf.Count() != 0 {
		t.Errorf("expected count 0, got %d", bf.Count())
	}

	if bf.HashCount() == 0 {
		t.Error("expected non-zero hash count")
	}

	if bf.Size() == 0 {
		t.Error("expected non-zero size")
	}
}

func TestNewBloomFilterWithConfig(t *testing.T) {
	config := &BloomConfig{
		ExpectedItems:     1000,
		FalsePositiveRate: 0.01,
	}

	bf := NewBloomFilter(config)

	if bf == nil {
		t.Fatal("expected bloom filter to not be nil")
	}

	// Size should be calculated based on expected items and FPR
	if bf.Size() == 0 {
		t.Error("expected non-zero size")
	}
}

func TestBloomFilterAddAndMightContain(t *testing.T) {
	bf := NewBloomFilter(&BloomConfig{
		ExpectedItems:     100,
		FalsePositiveRate: 0.001,
	})

	// Create test addresses
	addr1 := common.HexToAddress("0x1234567890123456789012345678901234567890")
	addr2 := common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	addr3 := common.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	// Initially no addresses should match
	if bf.MightContain(addr1) {
		t.Error("filter should not contain addr1 initially")
	}

	// Add addr1
	bf.Add(addr1)

	// addr1 should be found
	if !bf.MightContain(addr1) {
		t.Error("filter should contain addr1 after adding")
	}

	// addr2 should probably not be found (bloom filter can have false positives)
	// We just verify the logic works, not guarantee no false positives

	// Add addr2
	bf.Add(addr2)

	// Both should be found
	if !bf.MightContain(addr1) {
		t.Error("filter should contain addr1")
	}
	if !bf.MightContain(addr2) {
		t.Error("filter should contain addr2")
	}

	// Count should be 2
	if bf.Count() != 2 {
		t.Errorf("expected count 2, got %d", bf.Count())
	}

	// addr3 might or might not be found (probabilistic)
	_ = bf.MightContain(addr3)
}

func TestBloomFilterAddBatch(t *testing.T) {
	bf := NewBloomFilter(&BloomConfig{
		ExpectedItems:     100,
		FalsePositiveRate: 0.001,
	})

	addresses := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	}

	bf.AddBatch(addresses)

	if bf.Count() != 3 {
		t.Errorf("expected count 3, got %d", bf.Count())
	}

	for i, addr := range addresses {
		if !bf.MightContain(addr) {
			t.Errorf("filter should contain address %d", i)
		}
	}
}

func TestBloomFilterMightContainAny(t *testing.T) {
	bf := NewBloomFilter(&BloomConfig{
		ExpectedItems:     100,
		FalsePositiveRate: 0.001,
	})

	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	addr3 := common.HexToAddress("0x3333333333333333333333333333333333333333")

	bf.Add(addr1)

	// Should find match when one address is in the filter
	if !bf.MightContainAny([]common.Address{addr1, addr2, addr3}) {
		t.Error("MightContainAny should return true when addr1 is in the list")
	}

	// Empty filter case
	bf2 := NewBloomFilter(&BloomConfig{
		ExpectedItems:     100,
		FalsePositiveRate: 0.001,
	})

	// Should not find match in empty filter (but may have false positives)
	// This test just verifies the function doesn't panic
	_ = bf2.MightContainAny([]common.Address{addr2, addr3})
}

func TestBloomFilterClear(t *testing.T) {
	bf := NewBloomFilter(&BloomConfig{
		ExpectedItems:     100,
		FalsePositiveRate: 0.001,
	})

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	bf.Add(addr)

	if bf.Count() != 1 {
		t.Errorf("expected count 1, got %d", bf.Count())
	}

	bf.Clear()

	if bf.Count() != 0 {
		t.Errorf("expected count 0 after clear, got %d", bf.Count())
	}

	// After clearing, the address should not be found
	// (though false positives are possible in theory)
	if bf.FillRatio() != 0 {
		t.Error("fill ratio should be 0 after clear")
	}
}

func TestBloomFilterFillRatio(t *testing.T) {
	bf := NewBloomFilter(&BloomConfig{
		ExpectedItems:     100,
		FalsePositiveRate: 0.001,
	})

	initialRatio := bf.FillRatio()
	if initialRatio != 0 {
		t.Errorf("expected initial fill ratio 0, got %f", initialRatio)
	}

	// Add some addresses
	for i := 0; i < 10; i++ {
		addr := common.BigToAddress(common.Big0)
		addr[0] = byte(i)
		bf.Add(addr)
	}

	newRatio := bf.FillRatio()
	if newRatio <= initialRatio {
		t.Errorf("fill ratio should increase after adding items")
	}

	if newRatio > 1.0 {
		t.Errorf("fill ratio should not exceed 1.0, got %f", newRatio)
	}
}

func TestBloomFilterBytes(t *testing.T) {
	bf := NewBloomFilter(&BloomConfig{
		ExpectedItems:     100,
		FalsePositiveRate: 0.01,
	})

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	bf.Add(addr)

	// Serialize
	data := bf.Bytes()
	if len(data) == 0 {
		t.Error("serialized data should not be empty")
	}

	// Deserialize
	bf2 := NewBloomFilterFromBytes(data, bf.HashCount())
	if bf2 == nil {
		t.Fatal("expected bloom filter from bytes to not be nil")
	}

	// Original address should still be found
	if !bf2.MightContain(addr) {
		t.Error("deserialized filter should contain the original address")
	}
}

func TestBloomFilterFromEmptyBytes(t *testing.T) {
	bf := NewBloomFilterFromBytes(nil, 5)

	if bf == nil {
		t.Fatal("expected bloom filter to not be nil")
	}

	// Should return a default filter
	if bf.Size() == 0 {
		t.Error("size should not be 0")
	}
}

func TestBloomFilterMerge(t *testing.T) {
	config := &BloomConfig{
		ExpectedItems:     100,
		FalsePositiveRate: 0.01,
	}

	bf1 := NewBloomFilter(config)
	bf2 := NewBloomFilter(config)

	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")

	bf1.Add(addr1)
	bf2.Add(addr2)

	// Merge bf2 into bf1
	if err := bf1.Merge(bf2); err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// Both addresses should be found in bf1
	if !bf1.MightContain(addr1) {
		t.Error("merged filter should contain addr1")
	}
	if !bf1.MightContain(addr2) {
		t.Error("merged filter should contain addr2")
	}
}

func TestBloomFilterMergeSizeMismatch(t *testing.T) {
	config1 := &BloomConfig{
		ExpectedItems:     100,
		FalsePositiveRate: 0.01,
	}
	config2 := &BloomConfig{
		ExpectedItems:     1000,
		FalsePositiveRate: 0.01,
	}

	bf1 := NewBloomFilter(config1)
	bf2 := NewBloomFilter(config2)

	err := bf1.Merge(bf2)
	if err != ErrBloomFilterSizeMismatch {
		t.Errorf("expected ErrBloomFilterSizeMismatch, got %v", err)
	}
}

func TestBloomFilterEstimateFalsePositiveRate(t *testing.T) {
	bf := NewBloomFilter(&BloomConfig{
		ExpectedItems:     1000,
		FalsePositiveRate: 0.01,
	})

	// Empty filter should have 0 FPR
	initialFPR := bf.EstimateFalsePositiveRate()
	if initialFPR != 0 {
		t.Errorf("expected initial FPR 0, got %f", initialFPR)
	}

	// Add items
	for i := 0; i < 100; i++ {
		addr := common.BigToAddress(common.Big0)
		addr[0] = byte(i)
		bf.Add(addr)
	}

	// FPR should increase
	newFPR := bf.EstimateFalsePositiveRate()
	if newFPR <= initialFPR {
		t.Error("FPR should increase after adding items")
	}
	if newFPR > 1.0 {
		t.Errorf("FPR should not exceed 1.0, got %f", newFPR)
	}
}

func TestPopcount(t *testing.T) {
	tests := []struct {
		input    uint64
		expected int
	}{
		{0, 0},
		{1, 1},
		{0xFF, 8},
		{0xFFFFFFFFFFFFFFFF, 64},
		{0xAAAAAAAAAAAAAAAA, 32}, // alternating bits
	}

	for _, tt := range tests {
		result := popcount(tt.input)
		if result != tt.expected {
			t.Errorf("popcount(%d) = %d, expected %d", tt.input, result, tt.expected)
		}
	}
}

// Benchmark tests
func BenchmarkBloomFilterAdd(b *testing.B) {
	bf := NewBloomFilter(&BloomConfig{
		ExpectedItems:     100000,
		FalsePositiveRate: 0.0001,
	})

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.Add(addr)
	}
}

func BenchmarkBloomFilterMightContain(b *testing.B) {
	bf := NewBloomFilter(&BloomConfig{
		ExpectedItems:     100000,
		FalsePositiveRate: 0.0001,
	})

	// Pre-populate
	for i := 0; i < 1000; i++ {
		addr := common.BigToAddress(common.Big0)
		addr[0] = byte(i % 256)
		addr[1] = byte(i / 256)
		bf.Add(addr)
	}

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.MightContain(addr)
	}
}
