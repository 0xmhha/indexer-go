package chain

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// FeeDelegationMeta holds the fee payer fields of a StableNet fee delegation
// transaction (type 0x16), which go-ethereum's types cannot represent. It is
// shared by the RPC client that extracts it and the fetcher that stores it.
type FeeDelegationMeta struct {
	TxHash       common.Hash
	BlockNumber  uint64
	OriginalType uint8
	FeePayer     common.Address
	FeePayerV    *big.Int
	FeePayerR    *big.Int
	FeePayerS    *big.Int
}
