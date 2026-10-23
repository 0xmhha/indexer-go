package executor

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
)

type cancelDatabase struct {
	state.Database
	cancel context.CancelFunc
	once   sync.Once
}

func (d *cancelDatabase) OpenStorageTrie(root common.Hash, addr common.Address, storage common.Hash, account state.Trie) (state.Trie, error) {
	d.once.Do(d.cancel)
	return d.Database.OpenStorageTrie(root, addr, storage, account)
}

func TestReplayCancellationDuringEVMReturnsNoResult(t *testing.T) {
	disk, in := evmFixture(t)
	before := dump(t, disk)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in.Database = &cancelDatabase{Database: in.Database, cancel: cancel}
	result, err := Replay(ctx, in)
	if !errors.Is(err, context.Canceled) || result.Ref.Generation != 0 {
		t.Fatalf("cancelled EVM result published: %v", err)
	}
	if !reflect.DeepEqual(before, dump(t, disk)) {
		t.Fatal("cancelled EVM wrote backing DB")
	}
}

func TestCancelLoggerJoinsCallbackAndDetaches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	logger := &cancelLogger{ctx: ctx}
	env := &vm.EVM{Config: vm.Config{Tracer: logger}}
	logger.CaptureTxStart(env, 0, nil)
	if env.Config.Tracer != nil {
		t.Fatal("one-shot observer left opcode tracing enabled")
	}
	cancel()
	logger.CaptureTxEnd(0)
	if !env.Cancelled() {
		t.Fatal("context did not cancel bound EVM")
	}
	logger.close()
	ctx, cancel = context.WithCancel(context.Background())
	logger = &cancelLogger{ctx: ctx}
	next := &vm.EVM{}
	logger.CaptureTxStart(next, 0, nil)
	logger.close()
	cancel()
	if next.Cancelled() {
		t.Fatal("stopped callback cancelled a completed EVM")
	}
}

func TestCancelLoggerInterruptsRunningEVMAndJoins(t *testing.T) {
	_, in := evmFixture(t)
	s, err := state.New(in.Parent.Root, in.Database, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A real SLOAD/JUMP loop; the storage read deterministically triggers cancel.
	s.SetCode(account, []byte{0x5b, 0x60, 0x00, 0x54, 0x50, 0x60, 0x00, 0x56})
	s.SetState(account, common.Hash{}, common.Hash{1})
	root, err := s.Commit(0, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Database.TrieDB().Commit(root, false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db := &cancelDatabase{Database: in.Database, cancel: cancel}
	running, err := state.New(root, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	logger := &cancelLogger{ctx: ctx}
	env := vm.NewEVM(core.NewEVMBlockContext(in.Block.Header(), in.Chain, nil), vm.TxContext{}, running, in.Chain.Config(), vm.Config{Tracer: logger})
	logger.CaptureTxStart(env, 1_000_000_000, nil)
	done := make(chan struct{})
	go func() {
		_, _, _ = env.Call(vm.AccountRef(common.Address{}), account, nil, 1_000_000_000, uint256.NewInt(0))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		env.Cancel()
		<-done
		logger.close()
		t.Fatal("context callback did not interrupt EVM loop")
	}
	logger.CaptureTxEnd(0)
	if !env.Cancelled() || ctx.Err() != context.Canceled {
		t.Fatal("loop ended without exercising context cancellation")
	}
}
