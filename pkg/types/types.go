package types

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// BlockData represents a complete block with all related data
type BlockData struct {
	Block        *types.Block
	Transactions []*TransactionData
	Receipts     []*types.Receipt
}

// TransactionData represents a transaction with its metadata
type TransactionData struct {
	Transaction *types.Transaction
	BlockNumber uint64
	BlockHash   common.Hash
	TxIndex     uint64
	Receipt     *types.Receipt
}

// IndexedBlock represents a block stored in the database
type IndexedBlock struct {
	Number       uint64
	Hash         common.Hash
	ParentHash   common.Hash
	Time         uint64
	Miner        common.Address
	GasUsed      uint64
	GasLimit     uint64
	BaseFee      uint64
	TxCount      int
	Transactions []common.Hash
}

// IndexedTransaction represents a transaction stored in the database
type IndexedTransaction struct {
	Hash        common.Hash
	BlockNumber uint64
	BlockHash   common.Hash
	TxIndex     uint64
	From        common.Address
	To          *common.Address
	Value       string
	GasUsed     uint64
	GasPrice    string
	Nonce       uint64
	TxType      uint8
	Status      uint64
}

// IndexedReceipt represents a receipt stored in the database
type IndexedReceipt struct {
	TxHash          common.Hash
	BlockNumber     uint64
	BlockHash       common.Hash
	TxIndex         uint64
	ContractAddress *common.Address
	GasUsed         uint64
	Status          uint64
	Logs            []*types.Log
}

// BlockFilter represents filtering criteria for block queries
type BlockFilter struct {
	HeightMin *uint64
	HeightMax *uint64
	Miner     *common.Address
	GasUsed   *uint64
}

// TransactionFilter represents filtering criteria for transaction queries
type TransactionFilter struct {
	BlockHeightMin *uint64
	BlockHeightMax *uint64
	From           *common.Address
	To             *common.Address
	TxType         *uint8
	Status         *uint64
}

// PaginationOptions represents pagination parameters
type PaginationOptions struct {
	Limit  int
	Offset int
	Cursor *string
}
