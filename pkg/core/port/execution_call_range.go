package port

import (
	"context"
	"fmt"
	"math"

	"github.com/0xmhha/indexer-go/execwire"
)

const MaxExecutionCallRange = 128

type ExecutionCallRangeQuery struct {
	Address     execwire.Address
	FromBlock   uint64
	ToBlock     uint64
	Limit       uint32
	Selection   string // canonical or orphan; never combined
	OrphanEpoch uint64 // original public epoch, required for orphan selection
	Partial     bool
	Cursor      string
}

func (q ExecutionCallRangeQuery) Validate() error {
	if q.ToBlock > math.MaxInt64 || q.FromBlock > q.ToBlock || q.ToBlock-q.FromBlock >= MaxExecutionCallRange || q.Limit == 0 || q.Limit > 256 || len(q.Cursor) > 2048 || q.OrphanEpoch > math.MaxInt64 {
		return ErrInvalidData
	}
	if q.Selection != "canonical" && q.Selection != "orphan" || (q.Selection == "orphan") != (q.OrphanEpoch > 0) {
		return ErrInvalidData
	}
	return nil
}

type ExecutionCallMissingRange struct {
	FromBlock uint64 `json:"fromBlock"`
	ToBlock   uint64 `json:"toBlock"`
}
type ExecutionCallCoverage struct {
	Height    uint64        `json:"height"`
	Collected bool          `json:"collected"`
	Epoch     uint64        `json:"epoch"`
	Ref       execwire.Ref  `json:"ref"`
	Digest    execwire.Hash `json:"digest"`
	Profile   string        `json:"profile"`
}
type ExecutionCallRangeItem struct {
	Coverage ExecutionCallCoverage `json:"coverage"`
	Call     ExecutionCall         `json:"call"`
}
type ExecutionCallRangePage struct {
	Selection string                      `json:"selection"`
	Epoch     uint64                      `json:"epoch"`
	Complete  bool                        `json:"complete"`
	Missing   []ExecutionCallMissingRange `json:"missing"`
	Coverage  []ExecutionCallCoverage     `json:"coverage"`
	Calls     []ExecutionCallRangeItem    `json:"calls"`
	Next      string                      `json:"next"`
}
type ExecutionCallCoverageError struct{ Missing []ExecutionCallMissingRange }

func (e *ExecutionCallCoverageError) Error() string {
	return fmt.Sprintf("execution calls not collected in %d range(s)", len(e.Missing))
}
func (e *ExecutionCallCoverageError) Unwrap() error { return ErrExecutionCoverage }

type ExecutionCallRangeReader interface {
	ExecutionCallRange(context.Context, ExecutionCallRangeQuery) (ExecutionCallRangePage, error)
}
