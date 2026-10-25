package executor

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

// CaptureTxStart runs after upstream constructs the EVM. Detach this one-shot
// observer before interpreting opcodes; the upstream transaction-level defer
// still calls CaptureTxEnd on its local logger. State recording uses tries.
type cancelLogger struct {
	ctx     context.Context
	cleanup func()
}

func (t *cancelLogger) CaptureTxStart(env *vm.EVM, _ uint64, _ []types.SetCodeAuthorization) {
	env.Config.Tracer = nil
	t.arm(env)
}
func (t *cancelLogger) arm(env *vm.EVM) {
	if t.ctx.Err() != nil {
		env.Cancel()
		return
	}
	if t.ctx.Done() == nil {
		return
	}
	done := make(chan struct{})
	stop := context.AfterFunc(t.ctx, func() { env.Cancel(); close(done) })
	t.cleanup = func() {
		if !stop() {
			<-done
		}
	}
}
func (t *cancelLogger) close() {
	if t.cleanup != nil {
		t.cleanup()
		t.cleanup = nil
	}
}
func (t *cancelLogger) CaptureTxEnd(uint64)                                                       { t.close() }
func (*cancelLogger) CaptureStart(common.Address, common.Address, bool, []byte, uint64, *big.Int) {}
func (*cancelLogger) CaptureEnd([]byte, uint64, error)                                            {}
func (*cancelLogger) CaptureEnter(vm.OpCode, common.Address, common.Address, []byte, uint64, *big.Int) {
}
func (*cancelLogger) CaptureExit([]byte, uint64, error) {}
func (*cancelLogger) CaptureState(uint64, vm.OpCode, uint64, uint64, *vm.ScopeContext, []byte, int, error) {
}
func (*cancelLogger) CaptureFault(uint64, vm.OpCode, uint64, uint64, *vm.ScopeContext, int, error) {}
