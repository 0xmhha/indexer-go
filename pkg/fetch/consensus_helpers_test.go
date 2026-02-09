package fetch

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestContainsAddress_Found(t *testing.T) {
	addrs := []common.Address{
		common.HexToAddress("0xaaa"),
		common.HexToAddress("0xbbb"),
		common.HexToAddress("0xccc"),
	}
	if !containsAddress(addrs, common.HexToAddress("0xbbb")) {
		t.Error("expected address to be found")
	}
}

func TestContainsAddress_NotFound(t *testing.T) {
	addrs := []common.Address{
		common.HexToAddress("0xaaa"),
		common.HexToAddress("0xbbb"),
	}
	if containsAddress(addrs, common.HexToAddress("0xddd")) {
		t.Error("expected address not found")
	}
}

func TestContainsAddress_EmptySlice(t *testing.T) {
	if containsAddress(nil, common.HexToAddress("0xaaa")) {
		t.Error("expected false for nil slice")
	}
	if containsAddress([]common.Address{}, common.HexToAddress("0xaaa")) {
		t.Error("expected false for empty slice")
	}
}

func TestCountBitsInBitmap_Empty(t *testing.T) {
	if countBitsInBitmap(nil) != 0 {
		t.Error("expected 0 for nil bitmap")
	}
	if countBitsInBitmap([]byte{}) != 0 {
		t.Error("expected 0 for empty bitmap")
	}
}

func TestCountBitsInBitmap_AllZero(t *testing.T) {
	if countBitsInBitmap([]byte{0x00, 0x00}) != 0 {
		t.Error("expected 0 for all-zero bitmap")
	}
}

func TestCountBitsInBitmap_AllOnes(t *testing.T) {
	// 0xFF = 8 bits set
	if countBitsInBitmap([]byte{0xFF}) != 8 {
		t.Errorf("expected 8 for 0xFF, got %d", countBitsInBitmap([]byte{0xFF}))
	}
	// Two bytes all ones = 16 bits
	if countBitsInBitmap([]byte{0xFF, 0xFF}) != 16 {
		t.Errorf("expected 16 for 0xFFFF, got %d", countBitsInBitmap([]byte{0xFF, 0xFF}))
	}
}

func TestCountBitsInBitmap_Mixed(t *testing.T) {
	// 0b10101010 = 4 bits set
	if countBitsInBitmap([]byte{0xAA}) != 4 {
		t.Errorf("expected 4 for 0xAA, got %d", countBitsInBitmap([]byte{0xAA}))
	}
	// 0b00000001 = 1 bit, 0b10000000 = 1 bit
	if countBitsInBitmap([]byte{0x01, 0x80}) != 2 {
		t.Errorf("expected 2, got %d", countBitsInBitmap([]byte{0x01, 0x80}))
	}
}

func TestCountBitsInBitmap_SingleBits(t *testing.T) {
	tests := []struct {
		input    byte
		expected int
	}{
		{0x01, 1}, // 00000001
		{0x02, 1}, // 00000010
		{0x04, 1}, // 00000100
		{0x08, 1}, // 00001000
		{0x10, 1}, // 00010000
		{0x20, 1}, // 00100000
		{0x40, 1}, // 01000000
		{0x80, 1}, // 10000000
		{0x03, 2}, // 00000011
		{0x07, 3}, // 00000111
		{0x0F, 4}, // 00001111
		{0x1F, 5}, // 00011111
		{0x3F, 6}, // 00111111
		{0x7F, 7}, // 01111111
	}

	for _, tt := range tests {
		got := countBitsInBitmap([]byte{tt.input})
		if got != tt.expected {
			t.Errorf("countBitsInBitmap(0x%02X): expected %d, got %d", tt.input, tt.expected, got)
		}
	}
}
