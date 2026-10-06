package storage

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

func TestEncodeDecodeUint64(t *testing.T) {
	tests := []struct {
		name  string
		value uint64
	}{
		{"zero", 0},
		{"one", 1},
		{"small", 100},
		{"medium", 1000000},
		{"large", 18446744073709551615},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeUint64(tt.value)
			if len(encoded) != 8 {
				t.Errorf("EncodeUint64() length = %d, want 8", len(encoded))
			}

			decoded, err := DecodeUint64(encoded)
			if err != nil {
				t.Errorf("DecodeUint64() error = %v", err)
			}
			if decoded != tt.value {
				t.Errorf("DecodeUint64() = %d, want %d", decoded, tt.value)
			}
		})
	}
}

func TestDecodeUint64_InvalidData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"too short", []byte{1, 2, 3}},
		{"too long", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeUint64(tt.data)
			if err == nil {
				t.Error("DecodeUint64() should return error for invalid data")
			}
		})
	}
}

func TestEncodeDecodeTxLocation(t *testing.T) {
	tests := []struct {
		name string
		loc  *port.TxLocation
	}{
		{
			"genesis tx",
			&port.TxLocation{
				BlockHeight: 0,
				TxIndex:     0,
				BlockHash:   common.Hash{},
			},
		},
		{
			"regular tx",
			&port.TxLocation{
				BlockHeight: 1000,
				TxIndex:     5,
				BlockHash:   common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
			},
		},
		{
			"large values",
			&port.TxLocation{
				BlockHeight: 18446744073709551615,
				TxIndex:     18446744073709551615,
				BlockHash:   common.HexToHash("0xfedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeTxLocation(tt.loc)
			if err != nil {
				t.Errorf("EncodeTxLocation() error = %v", err)
			}
			if len(encoded) == 0 {
				t.Error("EncodeTxLocation() returned empty data")
			}

			decoded, err := DecodeTxLocation(encoded)
			if err != nil {
				t.Errorf("DecodeTxLocation() error = %v", err)
			}

			if decoded.BlockHeight != tt.loc.BlockHeight {
				t.Errorf("BlockHeight = %d, want %d", decoded.BlockHeight, tt.loc.BlockHeight)
			}
			if decoded.TxIndex != tt.loc.TxIndex {
				t.Errorf("TxIndex = %d, want %d", decoded.TxIndex, tt.loc.TxIndex)
			}
			if decoded.BlockHash != tt.loc.BlockHash {
				t.Errorf("BlockHash = %s, want %s", decoded.BlockHash.Hex(), tt.loc.BlockHash.Hex())
			}
		})
	}
}

func TestEncodeTxLocation_Nil(t *testing.T) {
	_, err := EncodeTxLocation(nil)
	if err == nil {
		t.Error("EncodeTxLocation(nil) should return error")
	}
}

func TestDecodeTxLocation_InvalidData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"invalid RLP", []byte{0xff, 0xff, 0xff}},
		{"garbage", []byte("not valid")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeTxLocation(tt.data)
			if err == nil {
				t.Error("DecodeTxLocation() should return error for invalid data")
			}
		})
	}
}
