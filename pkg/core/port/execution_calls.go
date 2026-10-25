package port

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// ExecutionCall preserves attempted calls. Reverted/ancestor-reverted and
// delegate-context values must never be interpreted as native transfers.
type ExecutionCall struct {
	execwire.CallFrame
	Ordinal          uint32
	Path             []uint32
	Internal         bool
	AncestorReverted bool
	Reverted         bool
}

type ExecutionCallWriter interface {
	ApplyExecutionCalls(context.Context, execwire.Result, *model.Block, []*model.Receipt) error
}

type ExecutionCallQuery struct {
	Height       uint64
	Transaction  *execwire.Hash
	InternalOnly bool
	After        uint32
	Limit        uint32
	// Subsequent pages must pin both values; first pages may supply them.
	Epoch uint64
	Hash  execwire.Hash
}
type ExecutionCallPage struct {
	Epoch        uint64
	Ref          execwire.Ref
	Digest       execwire.Hash
	Profile      string
	Verification string
	Total        uint32
	Frames       []ExecutionCall
	Next         *uint32
}
type ExecutionCallReader interface {
	ExecutionCalls(context.Context, ExecutionCallQuery) (ExecutionCallPage, error)
}
