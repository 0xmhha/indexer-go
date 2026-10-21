package token

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

func TestReadTransfer(t *testing.T) {
	topic := common.HexToHash(port.ERC20TransferTopic)
	token, native := common.HexToAddress("0x01"), common.HexToAddress("0x02")
	from, to := common.BytesToHash(common.HexToAddress("0xa").Bytes()), common.BytesToHash(common.HexToAddress("0xb").Bytes())
	value := common.LeftPadBytes(big.NewInt(5).Bytes(), 32)

	tr, ok := ReadTransfer(&types.Log{Address: token, Topics: []common.Hash{topic, from, to}, Data: value}, &native)
	assert.True(t, ok)
	assert.Equal(t, Transfer{Token: token, From: common.HexToAddress("0xa"), To: common.HexToAddress("0xb"), Value: big.NewInt(5)}, tr)
	tr, ok = ReadTransfer(&types.Log{Address: token, Topics: []common.Hash{topic, from, to, common.BigToHash(big.NewInt(7))}}, &native)
	assert.True(t, ok)
	assert.True(t, tr.ERC721)
	assert.Equal(t, "7", tr.TokenID.String())

	for name, log := range map[string]*types.Log{
		"native coin":   {Address: native, Topics: []common.Hash{topic, from, to}, Data: value},
		"short data":    {Address: token, Topics: []common.Hash{topic, from, to}},
		"two topics":    {Address: token, Topics: []common.Hash{topic, from}, Data: value},
		"one topic":     {Address: token, Topics: []common.Hash{topic}, Data: value},
		"another event": {Address: token, Topics: []common.Hash{from, from, to}, Data: value},
		"no topics":     {Address: token},
	} {
		_, ok := ReadTransfer(log, &native)
		assert.False(t, ok, name)
	}
	_, ok = ReadTransfer(&types.Log{Address: native, Topics: []common.Hash{topic, from, to}, Data: value}, nil)
	assert.True(t, ok, "a chain without a native coin contract")
}
