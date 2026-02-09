package types

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
)

// --- BlockData tests ---

func TestBlockData_Fields(t *testing.T) {
	header := &types.Header{Number: big.NewInt(100)}
	block := types.NewBlockWithHeader(header)

	bd := &BlockData{
		Block:        block,
		Transactions: []*TransactionData{{BlockNumber: 100}},
		Receipts:     []*types.Receipt{{Status: 1}},
	}

	assert.Equal(t, block, bd.Block)
	assert.Len(t, bd.Transactions, 1)
	assert.Len(t, bd.Receipts, 1)
}

func TestBlockData_NilFields(t *testing.T) {
	bd := &BlockData{}
	assert.Nil(t, bd.Block)
	assert.Nil(t, bd.Transactions)
	assert.Nil(t, bd.Receipts)
}

// --- TransactionData tests ---

func TestTransactionData_Fields(t *testing.T) {
	to := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	txHash := common.HexToHash("0xabcdef")

	td := &TransactionData{
		Transaction: types.NewTx(&types.LegacyTx{Nonce: 1}),
		BlockNumber: 50,
		BlockHash:   common.HexToHash("0xblock"),
		TxIndex:     3,
		Receipt:     &types.Receipt{Status: 1, TxHash: txHash},
	}

	assert.Equal(t, uint64(50), td.BlockNumber)
	assert.Equal(t, uint64(3), td.TxIndex)
	assert.NotNil(t, td.Transaction)
	assert.NotNil(t, td.Receipt)
	_ = to
}

// --- IndexedBlock tests ---

func TestIndexedBlock_Fields(t *testing.T) {
	ib := &IndexedBlock{
		Number:     100,
		Hash:       common.HexToHash("0xblockhash"),
		ParentHash: common.HexToHash("0xparent"),
		Time:       1700000000,
		Miner:      common.HexToAddress("0xminer"),
		GasUsed:    21000,
		GasLimit:   30000000,
		BaseFee:    1000000000,
		TxCount:    5,
		Transactions: []common.Hash{
			common.HexToHash("0xtx1"),
			common.HexToHash("0xtx2"),
		},
	}

	assert.Equal(t, uint64(100), ib.Number)
	assert.Equal(t, uint64(1700000000), ib.Time)
	assert.Equal(t, 5, ib.TxCount)
	assert.Len(t, ib.Transactions, 2)
}

// --- IndexedTransaction tests ---

func TestIndexedTransaction_Fields(t *testing.T) {
	to := common.HexToAddress("0xto")
	it := &IndexedTransaction{
		Hash:        common.HexToHash("0xtxhash"),
		BlockNumber: 50,
		BlockHash:   common.HexToHash("0xblockhash"),
		TxIndex:     2,
		From:        common.HexToAddress("0xfrom"),
		To:          &to,
		Value:       "1000000000000000000",
		GasUsed:     21000,
		GasPrice:    "20000000000",
		Nonce:       5,
		TxType:      2,
		Status:      1,
	}

	assert.Equal(t, uint64(50), it.BlockNumber)
	assert.NotNil(t, it.To)
	assert.Equal(t, to, *it.To)
	assert.Equal(t, uint8(2), it.TxType)
	assert.Equal(t, uint64(1), it.Status)
}

func TestIndexedTransaction_NilTo(t *testing.T) {
	it := &IndexedTransaction{
		Hash: common.HexToHash("0xcontractcreation"),
		To:   nil, // contract creation
	}
	assert.Nil(t, it.To)
}

// --- IndexedReceipt tests ---

func TestIndexedReceipt_Fields(t *testing.T) {
	contractAddr := common.HexToAddress("0xcontract")
	ir := &IndexedReceipt{
		TxHash:          common.HexToHash("0xtx"),
		BlockNumber:     100,
		BlockHash:       common.HexToHash("0xblock"),
		TxIndex:         0,
		ContractAddress: &contractAddr,
		GasUsed:         50000,
		Status:          1,
		Logs:            []*types.Log{{BlockNumber: 100}},
	}

	assert.Equal(t, uint64(100), ir.BlockNumber)
	assert.NotNil(t, ir.ContractAddress)
	assert.Equal(t, contractAddr, *ir.ContractAddress)
	assert.Len(t, ir.Logs, 1)
}

// --- BlockFilter tests ---

func TestBlockFilter_AllNil(t *testing.T) {
	f := &BlockFilter{}
	assert.Nil(t, f.HeightMin)
	assert.Nil(t, f.HeightMax)
	assert.Nil(t, f.Miner)
	assert.Nil(t, f.GasUsed)
}

func TestBlockFilter_WithValues(t *testing.T) {
	min := uint64(10)
	max := uint64(100)
	miner := common.HexToAddress("0xminer")
	gas := uint64(21000)

	f := &BlockFilter{
		HeightMin: &min,
		HeightMax: &max,
		Miner:     &miner,
		GasUsed:   &gas,
	}

	assert.Equal(t, uint64(10), *f.HeightMin)
	assert.Equal(t, uint64(100), *f.HeightMax)
	assert.Equal(t, miner, *f.Miner)
}

// --- TransactionFilter tests ---

func TestTransactionFilter_Fields(t *testing.T) {
	from := common.HexToAddress("0xfrom")
	txType := uint8(2)
	status := uint64(1)

	f := &TransactionFilter{
		From:   &from,
		TxType: &txType,
		Status: &status,
	}

	assert.Equal(t, from, *f.From)
	assert.Equal(t, uint8(2), *f.TxType)
}

// --- PaginationOptions tests ---

func TestPaginationOptions_Defaults(t *testing.T) {
	p := &PaginationOptions{
		Limit:  10,
		Offset: 0,
	}
	assert.Equal(t, 10, p.Limit)
	assert.Equal(t, 0, p.Offset)
	assert.Nil(t, p.Cursor)
}

func TestPaginationOptions_WithCursor(t *testing.T) {
	cursor := "next_page_token"
	p := &PaginationOptions{
		Limit:  20,
		Cursor: &cursor,
	}
	assert.Equal(t, "next_page_token", *p.Cursor)
}
