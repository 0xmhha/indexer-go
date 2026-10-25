package native

import (
	"bytes"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var ErrTrace = errors.New("execution: invalid native call trace")

const Verification = "locally_replayed_evm_hooks_root_consistency"

// Verify authenticates roots against the already profile-verified block
// and receipts. It does not re-execute or claim a child-list commitment.
func Verify(profile string, r execwire.Result, b *model.Block, rs []*model.Receipt) ([]port.ExecutionCall, error) {
	if r.CallTraces == nil {
		return nil, nil
	}
	if execwire.ValidateCallTraces(r) != nil || r.CallTraces.Profile != profile || b == nil || len(rs) != len(b.Transactions) || len(rs) != int(r.TxCount) {
		return nil, ErrTrace
	}
	if r.Ref.Height != b.Number || r.Ref.Hash != execwire.Hash(b.Hash) {
		return nil, ErrTrace
	}
	frames := r.CallTraces.Frames
	out := make([]port.ExecutionCall, 0, len(frames))
	children := make([]uint32, len(frames))
	for i, f := range frames {
		if f.Parent == -1 {
			tx, receipt := b.Transactions[f.TransactionIndex], rs[f.TransactionIndex]
			if tx == nil || receipt == nil || tx.Opaque || tx.Value == nil || tx.Value.Sign() < 0 || tx.Value.BitLen() > 256 || receipt.Status > 1 || receipt.TxHash != tx.Hash || receipt.BlockHash != b.Hash || receipt.BlockNumber != b.Number || receipt.TxIndex != uint(f.TransactionIndex) {
				return nil, ErrTrace
			}
			var value execwire.Word
			tx.Value.FillBytes(value[:])
			if f.TransactionHash != execwire.Hash(tx.Hash) || f.From != execwire.Address(tx.From) || f.Value == nil || *f.Value != value || f.Gas != tx.Gas || f.GasUsed != receipt.GasUsed || !bytes.Equal(f.Input, tx.Input) || (f.Error == "") != (receipt.Status == model.ReceiptStatusSuccessful) {
				return nil, ErrTrace
			}
			if tx.To != nil {
				if f.Type != "CALL" || f.To == nil || *f.To != execwire.Address(*tx.To) {
					return nil, ErrTrace
				}
			} else {
				if f.Type != "CREATE" {
					return nil, ErrTrace
				}
				if receipt.Status == model.ReceiptStatusSuccessful {
					if receipt.ContractAddress == nil || f.To == nil || *f.To != execwire.Address(*receipt.ContractAddress) {
						return nil, ErrTrace
					}
				} else if f.To != nil {
					return nil, ErrTrace
				}
			}
		}
		v := port.ExecutionCall{CallFrame: f, Ordinal: uint32(i), Path: []uint32{}, Internal: f.Parent >= 0, Reverted: f.Error != ""}
		if f.Parent >= 0 {
			p := out[f.Parent]
			v.Path = append(append([]uint32{}, p.Path...), children[f.Parent])
			children[f.Parent]++
			v.AncestorReverted = p.Reverted
			v.Reverted = v.Reverted || v.AncestorReverted
		}
		out = append(out, v)
	}
	return out, nil
}
