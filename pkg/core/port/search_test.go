package port

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectQueryType(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{"block number", "12345", "blockNumber"},
		{"block number zero", "0", "blockNumber"},
		{"hex block number", "0x3", "blockNumber"},
		{"block hash with 0x", "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef", "hash"},
		{"block hash without 0x", "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef", "hash"},
		{"address with 0x", "0x1234567890123456789012345678901234567890", "address"},
		{"address without 0x", "1234567890123456789012345678901234567890", "address"},
		{"short query", "abc", ""},
		{"not hex", "0x" + "zz34567890123456789012345678901234567890", ""},
		{"empty after trim", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectQueryType(tt.query)
			assert.Equal(t, tt.expected, result)
		})
	}
}
