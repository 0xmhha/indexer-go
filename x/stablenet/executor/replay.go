package executor

import (
	"context"
	"fmt"
	"math"
	"math/big"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

// Replay executes an already imported block from its parent state. It owns the
// recording StateDB, never commits it, and compares against immutable original
// roots even when the consensus engine rewrites its private header copy.
func Replay(ctx context.Context, in ReplayInput) (execwire.Result, error) {
	if err := ctx.Err(); err != nil {
		return execwire.Result{}, err
	}
	if in.Chain == nil || in.Database == nil || in.Parent == nil || in.Parent.Number == nil || in.Block == nil || in.Block.Number() == nil || in.Generation == 0 || in.Generation > math.MaxInt64 {
		return execwire.Result{}, fmt.Errorf("executor: invalid replay input")
	}
	h := in.Block.Header()
	if len(in.Block.Transactions()) > 65536 || len(in.Receipts) > 65536 {
		return execwire.Result{}, execwire.ErrLimit
	}
	config := in.Chain.Config()
	if config == nil || in.Chain.Engine() == nil || h.Number.Sign() <= 0 || h.Number.BitLen() > 63 || in.Parent.Number.Sign() < 0 || new(big.Int).Add(in.Parent.Number, big.NewInt(1)).Cmp(h.Number) != 0 || h.ParentHash != in.Parent.Hash() {
		return execwire.Result{}, fmt.Errorf("executor: invalid parent or configuration")
	}
	if h.ParentBeaconRoot != nil || config.DAOForkSupport && config.DAOForkBlock != nil && config.DAOForkBlock.Cmp(h.Number) == 0 {
		return execwire.Result{}, ErrUnsupportedPrelude
	}
	if len(in.Block.Withdrawals()) > 0 && !config.IsShanghai(h.Number, h.Time) {
		return execwire.Result{}, fmt.Errorf("executor: withdrawals before Shanghai")
	}
	// Preserve these values before Finalize. WBFT assigns header.Root itself.
	expectedState, expectedReceipts := h.Root, h.ReceiptHash
	if types.DeriveSha(in.Block.Transactions(), trie.NewStackTrie(nil)) != h.TxHash || types.CalcUncleHash(in.Block.Uncles()) != h.UncleHash || len(in.Receipts) != len(in.Block.Transactions()) || types.DeriveSha(in.Receipts, trie.NewStackTrie(nil)) != expectedReceipts {
		return execwire.Result{}, fmt.Errorf("%w: body or stored receipts", ErrRootMismatch)
	}
	r, err := newRecorder(in.Database, in.Parent.Root)
	if err != nil {
		return execwire.Result{}, err
	}
	gp := new(core.GasPool).AddGas(h.GasLimit)
	var used uint64
	receipts := make(types.Receipts, 0, len(in.Receipts))
	steps := make([]execwire.Step, 0, len(in.Receipts)+1)
	for i, tx := range in.Block.Transactions() {
		if err := ctx.Err(); err != nil {
			return execwire.Result{}, err
		}
		if err := r.beginStep(execwire.Transaction, int32(i), tx.Hash()); err != nil {
			return execwire.Result{}, err
		}
		r.state.SetTxContext(tx.Hash(), i)
		cfg := vm.Config{}
		logger := &cancelLogger{ctx: ctx}
		if ctx.Done() != nil {
			cfg.Tracer = logger
		}
		receipt, err := core.ApplyTransaction(config, in.Chain, nil, gp, r.state, h, tx, &used, cfg)
		logger.close()
		if err := ctx.Err(); err != nil {
			return execwire.Result{}, err
		}
		if err != nil {
			return execwire.Result{}, fmt.Errorf("executor: transaction %d: %w", i, err)
		}
		step, err := r.finishStep(config.IsEIP158(h.Number))
		if err != nil {
			return execwire.Result{}, err
		}
		steps = append(steps, step)
		receipts = append(receipts, receipt)
	}
	if used != h.GasUsed || types.DeriveSha(receipts, trie.NewStackTrie(nil)) != expectedReceipts {
		return execwire.Result{}, fmt.Errorf("%w: replay receipts or gas", ErrRootMismatch)
	}
	if err := ctx.Err(); err != nil {
		return execwire.Result{}, err
	}
	if err := r.beginStep(execwire.Finalize, -1, common.Hash{}); err != nil {
		return execwire.Result{}, err
	}
	if err := in.Chain.Engine().Finalize(in.Chain, h, r.state, in.Block.Transactions(), in.Block.Uncles(), in.Block.Withdrawals()); err != nil {
		return execwire.Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return execwire.Result{}, err
	}
	step, err := r.finishStep(config.IsEIP158(h.Number))
	if err != nil {
		return execwire.Result{}, err
	}
	steps = append(steps, step)
	if r.main.Hash() != expectedState {
		return execwire.Result{}, fmt.Errorf("%w: replay state", ErrRootMismatch)
	}
	blockRLP, err := rlp.EncodeToBytes(in.Block)
	if err != nil {
		return execwire.Result{}, err
	}
	receiptRLP, err := rlp.EncodeToBytes(in.Receipts)
	if err != nil {
		return execwire.Result{}, err
	}
	out := execwire.Result{Ref: execwire.Ref{Generation: in.Generation, Height: in.Block.NumberU64(), Hash: execwire.Hash(in.Block.Hash())}, Parent: execwire.Hash(in.Block.ParentHash()), Time: in.Block.Time(), StateRoot: execwire.Hash(expectedState), ReceiptsRoot: execwire.Hash(expectedReceipts), TxCount: uint32(len(in.Receipts)), BlockRLP: blockRLP, ReceiptsRLP: receiptRLP, Steps: steps}
	if _, err := execwire.MarshalResult(out); err != nil {
		return execwire.Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return execwire.Result{}, err
	}
	return out, nil
}
