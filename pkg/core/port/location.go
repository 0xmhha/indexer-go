package port

import (
	"github.com/ethereum/go-ethereum/common"
)

// TxLocation represents the location of a transaction in the blockchain
type TxLocation struct {
	BlockHeight uint64
	TxIndex     uint64
	BlockHash   common.Hash
}
