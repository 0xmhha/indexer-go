package executor

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"math/big"
)

// The library resets the EVM before the direct system Call. Arm cancellation
// at CaptureStart after that reset; never turn a system operation into a tx.
type preludeLogger struct {
	cancelLogger
	env *vm.EVM
}

func (l *preludeLogger) CaptureStart(common.Address, common.Address, bool, []byte, uint64, *big.Int) {
	l.arm(l.env)
}
