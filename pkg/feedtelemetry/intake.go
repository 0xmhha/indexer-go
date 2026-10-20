package feedtelemetry

import (
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// PendingBlocks bounds the ledger of durably received, uncaptured blocks.
const PendingBlocks = 1024

// maxIntakeKinds is the width of the per-block dataset mask.
const maxIntakeKinds = 32

type pendingBlock struct {
	mask  uint32
	first time.Time
}

type intake struct {
	start, through uint64
	pending        map[model.FeedBlock]*pendingBlock
	truncated      bool
	frames         uint64
	bytes          int64
}

// RawIntake reports raw frames made durable by this run that capture has not
// committed. ReceivedThrough is unverified: every height from StartHeight up to
// it had all required datasets of one hash appended, or was captured. It is a
// lower bound when PendingTruncated is set, because blocks beyond the bound
// were not recorded.
type RawIntake struct {
	StartHeight      uint64          `json:"startHeight"`
	ReceivedThrough  uint64          `json:"receivedThrough"`
	PendingBlocks    int             `json:"pendingBlocks"`
	PendingTruncated bool            `json:"pendingTruncated"`
	OldestPending    *PendingReceipt `json:"oldestPending"`
	AppendedFrames   uint64          `json:"appendedFrames"`
	AppendedBytes    int64           `json:"appendedBytes"`
}

// PendingReceipt is the earliest original window receive time among pending
// blocks, including frames a previous run stored and this run re-delivered.
type PendingReceipt struct {
	Block         model.FeedBlock `json:"block"`
	FirstReceived time.Time       `json:"firstReceived"`
	AgeMS         float64         `json:"ageMs"`
}

// StartIntake begins a new intake session above the durable capture cursor.
// Earlier sessions' pending blocks are discarded because a new session
// re-delivers them; appended totals cover the whole run.
func (r *Recorder) StartIntake(through model.FeedBlock) {
	if r == nil || through.Chain != r.anchor.Chain || r.kinds == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := &intake{start: through.Number, through: through.Number, pending: map[model.FeedBlock]*pendingBlock{}}
	if old := r.intake; old != nil {
		next.frames, next.bytes = old.frames, old.bytes
	}
	r.intake = next
}

// Appended adds frames and bytes newly written to the window. Appends that
// returned a frame the window already held are not counted.
func (r *Recorder) Appended(frames uint64, bytes int64) {
	if r == nil || frames == 0 && bytes <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if in := r.intake; in != nil {
		in.frames += frames
		in.bytes += bytes
	}
}

// Received records one durably appended frame with its original window receive
// time, which predates this run when the window already held it.
func (r *Recorder) Received(block model.FeedBlock, kind model.DatasetKind, firstReceived time.Time) {
	if r == nil || block.Chain != r.anchor.Chain {
		return
	}
	bit, ok := r.kinds[kind]
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	in := r.intake
	if in == nil {
		return
	}
	if block.Number <= in.through {
		return
	}
	p := in.pending[block]
	if p == nil {
		if len(in.pending) >= PendingBlocks {
			in.truncated = true
			return
		}
		p = &pendingBlock{first: firstReceived}
		in.pending[block] = p
	}
	if firstReceived.Before(p.first) {
		p.first = firstReceived
	}
	p.mask |= 1 << bit
	r.advanceIntake()
}

// advanceIntake moves the contiguous cursor over complete pending heights.
func (r *Recorder) advanceIntake() {
	in, full := r.intake, uint32(1)<<len(r.kinds)-1
	for {
		next, found := in.through+1, false
		for b, p := range in.pending {
			if b.Number == next && p.mask == full {
				found = true
				break
			}
		}
		if !found {
			return
		}
		in.through = next
	}
}

// intakeCaptured drops blocks at or below a committed capture height; work for
// them is no longer pending, including retained forks at those heights.
func (r *Recorder) intakeCaptured(height uint64) {
	in := r.intake
	if in == nil {
		return
	}
	for b := range in.pending {
		if b.Number <= height {
			delete(in.pending, b)
		}
	}
	if height > in.through {
		in.through = height
		r.advanceIntake()
	}
}

func (r *Recorder) intakeSnapshot(now time.Time) *RawIntake {
	in := r.intake
	if in == nil {
		return nil
	}
	out := &RawIntake{StartHeight: in.start, ReceivedThrough: in.through, PendingBlocks: len(in.pending), PendingTruncated: in.truncated, AppendedFrames: in.frames, AppendedBytes: in.bytes}
	for b, p := range in.pending {
		if out.OldestPending == nil || p.first.Before(out.OldestPending.FirstReceived) {
			out.OldestPending = &PendingReceipt{Block: b, FirstReceived: p.first}
		}
	}
	if out.OldestPending != nil {
		// A wall clock behind the stored receive time yields no age, not zero.
		if age := now.Sub(out.OldestPending.FirstReceived); age >= 0 {
			v := ms(age)
			out.OldestPending.AgeMS = v
		} else {
			out.OldestPending = nil
		}
	}
	return out
}
