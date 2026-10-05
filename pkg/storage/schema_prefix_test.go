package storage

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestInternalTxToIndexKeyPrefix(t *testing.T) {
	to := common.HexToAddress("0xTO12345678901234567890123456789012345678")

	prefix := InternalTxToIndexKeyPrefix(to)
	assert.NotNil(t, prefix)
	assert.True(t, len(prefix) > 0)
}

func TestERC721FromIndexKeyPrefix(t *testing.T) {
	from := common.HexToAddress("0xFROM123456789012345678901234567890123456")

	prefix := ERC721FromIndexKeyPrefix(from)
	assert.NotNil(t, prefix)
	assert.True(t, len(prefix) > 0)
}

func TestERC721ToIndexKeyPrefix(t *testing.T) {
	to := common.HexToAddress("0xTO12345678901234567890123456789012345678")

	prefix := ERC721ToIndexKeyPrefix(to)
	assert.NotNil(t, prefix)
	assert.True(t, len(prefix) > 0)
}

func TestLogKeyPrefix(t *testing.T) {
	prefix := LogKeyPrefix()
	assert.NotNil(t, prefix)
	assert.True(t, len(prefix) > 0)
}

func TestLogBlockKeyPrefix(t *testing.T) {
	prefix := LogBlockKeyPrefix(12345)
	assert.NotNil(t, prefix)
	assert.True(t, len(prefix) > 0)
}

func TestLogBlockRangeIndexKeyPrefix(t *testing.T) {
	prefix := LogBlockRangeIndexKeyPrefix(12345)
	assert.NotNil(t, prefix)
	assert.True(t, len(prefix) > 0)
}

func TestContractVerificationKeyPrefix(t *testing.T) {
	prefix := ContractVerificationKeyPrefix()
	assert.NotNil(t, prefix)
	assert.True(t, len(prefix) > 0)
}
