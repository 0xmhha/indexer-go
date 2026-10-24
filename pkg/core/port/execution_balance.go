package port

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// ExecutionBalanceChange is one ordered, verified account journal entry.
type ExecutionBalanceChange struct {
	Address       common.Address
	Before, After *big.Int
	TxHash        common.Hash
	Ordinal       uint32
}

// ExecutionBalanceJournal verifies feature input against durable account parts
// inside the publication transaction. It writes no second balance projection.
type ExecutionBalanceJournal interface {
	ValidateExecutionBalances(context.Context, uint64, common.Hash, []ExecutionBalanceChange) error
}
