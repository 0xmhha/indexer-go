package executor

import (
	"context"
	"fmt"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"math/big"
)

type callCollector struct {
	frames []execwire.CallFrame
	stack  []int
	bytes  int
	err    error
}
type callLogger struct {
	cancelLogger
	calls *callCollector
	index uint32
	hash  execwire.Hash
	env   *vm.EVM
}

func (t *callLogger) CaptureTxStart(env *vm.EVM, _ uint64, _ []types.SetCodeAuthorization) {
	t.env = env
	t.arm(env)
}
func (t *callLogger) fail(err error) {
	t.calls.err = err
	if t.env != nil {
		t.env.Cancel()
	}
}
func (t *callLogger) push(kind string, from, to common.Address, input []byte, gas uint64, value *big.Int) {
	c := t.calls
	if c.err != nil {
		return
	}
	if len(c.frames) >= execwire.MaxCallFrames || len(c.stack) > execwire.MaxCallDepth || len(input) > execwire.MaxCallFieldBytes || c.bytes+len(input) > execwire.MaxCallBytes {
		t.fail(execwire.ErrLimit)
		return
	}
	switch kind {
	case "CALL", "CALLCODE", "DELEGATECALL", "STATICCALL", "CREATE", "CREATE2", "SELFDESTRUCT":
	default:
		t.fail(execwire.ErrProtocol)
		return
	}
	target := execwire.Address(to)
	f := execwire.CallFrame{TransactionIndex: t.index, TransactionHash: t.hash, Parent: -1, Type: kind, From: execwire.Address(from), To: &target, Gas: gas, Input: append([]byte{}, input...), Output: []byte{}}
	if value != nil {
		if value.Sign() < 0 || value.BitLen() > 256 {
			t.fail(execwire.ErrProtocol)
			return
		}
		v := execwire.Word{}
		value.FillBytes(v[:])
		f.Value = &v
	}
	if kind == "STATICCALL" {
		f.Value = nil
	}
	if len(c.stack) > 0 {
		f.Parent = int32(c.stack[len(c.stack)-1])
	}
	c.bytes += len(input)
	c.stack = append(c.stack, len(c.frames))
	c.frames = append(c.frames, f)
}
func (t *callLogger) finish(output []byte, gas uint64, err error) {
	c := t.calls
	if c.err != nil {
		return
	}
	if len(c.stack) == 0 {
		t.fail(execwire.ErrProtocol)
		return
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	if len(output) > execwire.MaxCallFieldBytes || len(message) > execwire.MaxCallErrorBytes || c.bytes+len(output)+len(message) > execwire.MaxCallBytes {
		t.fail(execwire.ErrLimit)
		return
	}
	n := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	f := &c.frames[n]
	f.Output = append([]byte{}, output...)
	f.GasUsed = gas
	f.Error = message
	if err != nil && (f.Type == "CREATE" || f.Type == "CREATE2") {
		f.To = nil
	}
	c.bytes += len(output) + len(message)
}
func (t *callLogger) CaptureStart(from, to common.Address, create bool, input []byte, gas uint64, value *big.Int) {
	kind := "CALL"
	if create {
		kind = "CREATE"
	}
	t.push(kind, from, to, input, gas, value)
}
func (t *callLogger) CaptureEnd(output []byte, gas uint64, err error) { t.finish(output, gas, err) }
func (t *callLogger) CaptureEnter(op vm.OpCode, from, to common.Address, input []byte, gas uint64, value *big.Int) {
	t.push(op.String(), from, to, input, gas, value)
}
func (t *callLogger) CaptureExit(output []byte, gas uint64, err error) { t.finish(output, gas, err) }

func (c *callCollector) end(root int, tx *types.Transaction, receipt *types.Receipt) error {
	if c.err != nil {
		return c.err
	}
	if len(c.stack) != 0 || len(c.frames) <= root {
		return fmt.Errorf("executor: incomplete call observer")
	}
	f := &c.frames[root]
	if f.Parent != -1 || (f.Error == "") != (receipt.Status == types.ReceiptStatusSuccessful) {
		return fmt.Errorf("executor: call root status mismatch")
	}
	f.Gas = tx.Gas()
	f.GasUsed = receipt.GasUsed
	return nil
}
func newCallLogger(ctx context.Context, c *callCollector, index uint32, hash common.Hash) *callLogger {
	return &callLogger{cancelLogger: cancelLogger{ctx: ctx}, calls: c, index: index, hash: execwire.Hash(hash)}
}
