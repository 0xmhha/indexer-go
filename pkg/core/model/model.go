// Package model defines the chain-neutral data model of the indexer.
//
// Blocks, transactions, receipts and logs are represented with the indexer's
// own types instead of go-ethereum's, so that a chain profile (pkg/chains) can
// describe transaction types that upstream go-ethereum does not know, such as
// StableNet fee delegation (type 0x16), without remapping them. Values such as
// hashes and the transaction type are kept exactly as the chain defines them.
//
// Chain-specific fields live in Extensions, keyed by typed keys that each
// profile declares; feature code reads them through the profile's accessors.
package model

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Block is a chain-neutral block.
type Block struct {
	Hash             common.Hash // as reported by the chain
	ParentHash       common.Hash
	UncleHash        common.Hash
	Miner            common.Address
	StateRoot        common.Hash
	TxRoot           common.Hash
	ReceiptRoot      common.Hash
	Bloom            []byte
	Difficulty       *big.Int
	Number           uint64
	GasLimit         uint64
	GasUsed          uint64
	Time             uint64
	Extra            []byte // raw header extra data (e.g. consensus data)
	MixDigest        common.Hash
	Nonce            uint64
	BaseFee          *big.Int
	WithdrawalsRoot  *common.Hash
	BlobGasUsed      *uint64
	ExcessBlobGas    *uint64
	ParentBeaconRoot *common.Hash
	RequestsHash     *common.Hash
	Size             uint64

	Transactions []*Transaction
	Uncles       []common.Hash // uncle block hashes, in order
	Withdrawals  []Withdrawal  // nil when the block has no withdrawals list
	Ext          Extensions
}

// Withdrawal is an EIP-4895 validator withdrawal.
type Withdrawal struct {
	Index     uint64
	Validator uint64
	Address   common.Address
	Amount    uint64 // in gwei
}

// Transaction is a chain-neutral transaction.
type Transaction struct {
	Hash    common.Hash // canonical hash on its chain
	Type    uint8       // the chain's type number, never remapped
	ChainID *big.Int    // nil for unprotected legacy transactions
	Nonce   uint64
	From    common.Address  // sender, recovered and checked by the profile
	To      *common.Address // nil for contract creation
	Value   *big.Int
	Gas     uint64

	// GasPrice is the legacy/EIP-2930 gas price. For fee-market types it is
	// the fee cap, matching go-ethereum's Transaction.GasPrice.
	GasPrice  *big.Int
	GasTipCap *big.Int
	GasFeeCap *big.Int

	Input      []byte
	AccessList []AccessTuple
	AuthList   []SetCodeAuthorization // EIP-7702
	BlobHashes []common.Hash
	BlobFeeCap *big.Int

	Signature Signature

	// Raw is the canonical encoding (type byte || payload for typed
	// transactions). Empty when the profile could not encode the type.
	Raw []byte

	BlockHash   common.Hash
	BlockNumber uint64
	Index       uint

	// Opaque is true when no profile understood the transaction type. Only
	// fields readable from the RPC response are filled in.
	Opaque bool

	Ext Extensions
}

// Signature holds a sender signature.
type Signature struct {
	V, R, S *big.Int
}

// AccessTuple is one access list entry.
type AccessTuple struct {
	Address     common.Address
	StorageKeys []common.Hash
}

// SetCodeAuthorization is an EIP-7702 authorization.
type SetCodeAuthorization struct {
	ChainID *big.Int
	Address common.Address
	Nonce   uint64
	V       uint8
	R, S    *big.Int
}

// Receipt is a chain-neutral transaction receipt.
type Receipt struct {
	Type              uint8
	Status            uint64
	CumulativeGasUsed uint64
	GasUsed           uint64
	EffectiveGasPrice *big.Int
	BlobGasUsed       uint64
	BlobGasPrice      *big.Int
	ContractAddress   *common.Address
	Bloom             []byte
	Logs              []*Log

	TxHash      common.Hash
	TxIndex     uint
	BlockHash   common.Hash
	BlockNumber uint64

	Ext Extensions
}

// Log is a chain-neutral event log.
type Log struct {
	Address     common.Address
	Topics      []common.Hash
	Data        []byte
	BlockNumber uint64
	BlockHash   common.Hash
	TxHash      common.Hash
	TxIndex     uint
	Index       uint
	Removed     bool
}
