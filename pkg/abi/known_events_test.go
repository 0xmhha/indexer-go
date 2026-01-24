package abi

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeKnownEvent_ERC20Transfer(t *testing.T) {
	// ERC20 Transfer: Transfer(address indexed from, address indexed to, uint256 value)
	// Topics: [eventSig, from, to], Data: [value]
	transferSig := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")

	// Encode value (1 ETH = 1e18)
	value := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	valueBytes := common.LeftPadBytes(value.Bytes(), 32)

	log := &types.Log{
		Topics: []common.Hash{
			transferSig,
			common.BytesToHash(from.Bytes()), // from is padded to 32 bytes
			common.BytesToHash(to.Bytes()),   // to is padded to 32 bytes
		},
		Data: valueBytes,
	}

	decoded := DecodeKnownEvent(log)
	require.NotNil(t, decoded)

	assert.Equal(t, "Transfer", decoded.EventName)
	assert.Equal(t, "Transfer(address,address,uint256)", decoded.EventSignature)
	assert.Len(t, decoded.Params, 3)

	// Check from
	assert.Equal(t, "from", decoded.Params[0].Name)
	assert.Equal(t, "address", decoded.Params[0].Type)
	assert.Equal(t, from.Hex(), decoded.Params[0].Value)
	assert.True(t, decoded.Params[0].Indexed)

	// Check to
	assert.Equal(t, "to", decoded.Params[1].Name)
	assert.Equal(t, "address", decoded.Params[1].Type)
	assert.Equal(t, to.Hex(), decoded.Params[1].Value)
	assert.True(t, decoded.Params[1].Indexed)

	// Check value
	assert.Equal(t, "value", decoded.Params[2].Name)
	assert.Equal(t, "uint256", decoded.Params[2].Type)
	assert.Equal(t, "1000000000000000000", decoded.Params[2].Value)
	assert.False(t, decoded.Params[2].Indexed)
}

func TestDecodeKnownEvent_ERC721Transfer(t *testing.T) {
	// ERC721 Transfer: Transfer(address indexed from, address indexed to, uint256 indexed tokenId)
	// Topics: [eventSig, from, to, tokenId], Data: []
	transferSig := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	tokenId := big.NewInt(12345)

	log := &types.Log{
		Topics: []common.Hash{
			transferSig,
			common.BytesToHash(from.Bytes()),
			common.BytesToHash(to.Bytes()),
			common.BytesToHash(common.LeftPadBytes(tokenId.Bytes(), 32)),
		},
		Data: []byte{}, // ERC721 Transfer has no data (tokenId is indexed)
	}

	decoded := DecodeKnownEvent(log)
	require.NotNil(t, decoded)

	assert.Equal(t, "Transfer", decoded.EventName)
	assert.Len(t, decoded.Params, 3)

	// Check tokenId (not value for ERC721)
	assert.Equal(t, "tokenId", decoded.Params[2].Name)
	assert.Equal(t, "uint256", decoded.Params[2].Type)
	assert.Equal(t, "12345", decoded.Params[2].Value)
	assert.True(t, decoded.Params[2].Indexed)
}

func TestDecodeKnownEvent_ERC20Approval(t *testing.T) {
	// ERC20 Approval: Approval(address indexed owner, address indexed spender, uint256 value)
	approvalSig := common.HexToHash("0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925")
	owner := common.HexToAddress("0x1111111111111111111111111111111111111111")
	spender := common.HexToAddress("0x2222222222222222222222222222222222222222")

	// Max approval value
	maxValue := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	valueBytes := common.LeftPadBytes(maxValue.Bytes(), 32)

	log := &types.Log{
		Topics: []common.Hash{
			approvalSig,
			common.BytesToHash(owner.Bytes()),
			common.BytesToHash(spender.Bytes()),
		},
		Data: valueBytes,
	}

	decoded := DecodeKnownEvent(log)
	require.NotNil(t, decoded)

	assert.Equal(t, "Approval", decoded.EventName)
	assert.Equal(t, "Approval(address,address,uint256)", decoded.EventSignature)
	assert.Len(t, decoded.Params, 3)

	assert.Equal(t, "owner", decoded.Params[0].Name)
	assert.Equal(t, "spender", decoded.Params[1].Name)
	assert.Equal(t, "value", decoded.Params[2].Name)
}

func TestDecodeKnownEvent_ApprovalForAll(t *testing.T) {
	// ApprovalForAll: ApprovalForAll(address indexed owner, address indexed operator, bool approved)
	sig := common.HexToHash("0x17307eab39ab6107e8899845ad3d59bd9653f200f220920489ca2b5937696c31")
	owner := common.HexToAddress("0x1111111111111111111111111111111111111111")
	operator := common.HexToAddress("0x2222222222222222222222222222222222222222")

	// approved = true (last byte = 1)
	approvedBytes := make([]byte, 32)
	approvedBytes[31] = 1

	log := &types.Log{
		Topics: []common.Hash{
			sig,
			common.BytesToHash(owner.Bytes()),
			common.BytesToHash(operator.Bytes()),
		},
		Data: approvedBytes,
	}

	decoded := DecodeKnownEvent(log)
	require.NotNil(t, decoded)

	assert.Equal(t, "ApprovalForAll", decoded.EventName)
	assert.Len(t, decoded.Params, 3)

	assert.Equal(t, "approved", decoded.Params[2].Name)
	assert.Equal(t, "bool", decoded.Params[2].Type)
	assert.Equal(t, "true", decoded.Params[2].Value)
}

func TestDecodeKnownEvent_UnknownEvent(t *testing.T) {
	// Unknown event signature
	unknownSig := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	log := &types.Log{
		Topics: []common.Hash{unknownSig},
		Data:   []byte{},
	}

	decoded := DecodeKnownEvent(log)
	assert.Nil(t, decoded)
}

func TestDecodeKnownEvent_NoTopics(t *testing.T) {
	log := &types.Log{
		Topics: []common.Hash{},
		Data:   []byte{},
	}

	decoded := DecodeKnownEvent(log)
	assert.Nil(t, decoded)
}

func TestIsERC20Transfer(t *testing.T) {
	transferSig := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

	// ERC20 Transfer: 3 topics
	erc20Log := &types.Log{
		Topics: []common.Hash{
			transferSig,
			common.HexToHash("0x0000000000000000000000001111111111111111111111111111111111111111"),
			common.HexToHash("0x0000000000000000000000002222222222222222222222222222222222222222"),
		},
	}
	assert.True(t, IsERC20Transfer(erc20Log))
	assert.False(t, IsERC721Transfer(erc20Log))

	// ERC721 Transfer: 4 topics
	erc721Log := &types.Log{
		Topics: []common.Hash{
			transferSig,
			common.HexToHash("0x0000000000000000000000001111111111111111111111111111111111111111"),
			common.HexToHash("0x0000000000000000000000002222222222222222222222222222222222222222"),
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
		},
	}
	assert.True(t, IsERC721Transfer(erc721Log))
	assert.False(t, IsERC20Transfer(erc721Log))
}

func TestDecodeTopicValue(t *testing.T) {
	tests := []struct {
		name     string
		topic    common.Hash
		typeName string
		expected string
	}{
		{
			name:     "address",
			topic:    common.HexToHash("0x0000000000000000000000001111111111111111111111111111111111111111"),
			typeName: "address",
			expected: "0x1111111111111111111111111111111111111111",
		},
		{
			name:     "uint256",
			topic:    common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000003e8"),
			typeName: "uint256",
			expected: "1000",
		},
		{
			name:     "bool_true",
			topic:    common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
			typeName: "bool",
			expected: "true",
		},
		{
			name:     "bool_false",
			topic:    common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
			typeName: "bool",
			expected: "false",
		},
		{
			name:     "bytes32",
			topic:    common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
			typeName: "bytes32",
			expected: "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := decodeTopicValue(tt.topic, tt.typeName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetAllKnownEventSignatures(t *testing.T) {
	sigs := GetAllKnownEventSignatures()
	assert.NotEmpty(t, sigs)

	// Check that Transfer and Approval are included
	hasTransfer := false
	hasApproval := false
	for _, sig := range sigs {
		if sig == "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef" {
			hasTransfer = true
		}
		if sig == "0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925" {
			hasApproval = true
		}
	}
	assert.True(t, hasTransfer, "Transfer event should be in known events")
	assert.True(t, hasApproval, "Approval event should be in known events")
}
