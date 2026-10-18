package evmstate

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feedtelemetry"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

func ProjectionDescriptor(d model.SupplementStreamDescriptor) model.StateProjectionDescriptor {
	return model.StateProjectionDescriptor{Version: 1, Name: "tracked." + d.Name, Stream: d}
}

// AccountValues keeps consensus fields and exact selected slots. Code bytes
// remain in permanent proof inputs; codeHash is authenticated even if the
// selection did not request bytes. Balance is NOT a native USDC interpretation.
type AccountValues struct {
	Address     common.Address `json:"address"`
	Exists      bool           `json:"exists"`
	Nonce       uint64         `json:"nonce"`
	Balance     *hexutil.Big   `json:"balance"`
	StorageRoot common.Hash    `json:"storageRoot"`
	CodeHash    common.Hash    `json:"codeHash"`
	Storage     []Slot         `json:"storage"`
}
type Values struct {
	Accounts []AccountValues `json:"accounts"`
}
type AccountChange struct {
	Address common.Address `json:"address"`
	Before  AccountValues  `json:"before"`
	After   AccountValues  `json:"after"`
}
type Changes struct {
	Before    model.FeedBlock `json:"before"`
	After     model.FeedBlock `json:"after"`
	Selection common.Hash     `json:"selectionHash"`
	Accounts  []AccountChange `json:"accounts"`
}

func projectedValues(in []Account) Values {
	out := Values{Accounts: make([]AccountValues, len(in))}
	for i, a := range in {
		out.Accounts[i] = AccountValues{Address: a.Address, Exists: a.Exists, Nonce: a.Nonce, Balance: a.Balance, StorageRoot: a.StorageRoot, CodeHash: a.CodeHash, Storage: a.Storage}
	}
	return out
}
func Projector(profile chains.BinaryProfile) capture.StateProjector {
	return func(d model.StateProjectionDescriptor, before, after capture.SupplementCheckpoint) (out capture.StateProjectionValues, err error) {
		if d.Validate() != nil || profile == nil || before.Block.Chain != after.Block.Chain || before.Block.Number == math.MaxUint64 || before.Block.Number+1 != after.Block.Number {
			return out, ErrProof
		}
		a, e := verifiedCheckpoint(profile, d.Stream, before.Block, before.Data)
		if e != nil {
			return out, e
		}
		b, e := verifiedCheckpoint(profile, d.Stream, after.Block, after.Data)
		if e != nil {
			return out, e
		}
		av, bv := projectedValues(a), projectedValues(b)
		if len(av.Accounts) != len(bv.Accounts) {
			return out, ErrProof
		}
		changes := Changes{Before: before.Block, After: after.Block, Selection: d.Stream.SelectionHash, Accounts: []AccountChange{}}
		for i, v := range av.Accounts {
			if v.Address != bv.Accounts[i].Address {
				return out, ErrProof
			}
			x, e := json.Marshal(v)
			if e != nil {
				return out, e
			}
			y, e := json.Marshal(bv.Accounts[i])
			if e != nil {
				return out, e
			}
			if string(x) != string(y) {
				changes.Accounts = append(changes.Accounts, AccountChange{v.Address, v, bv.Accounts[i]})
			}
		}
		if out.Before, e = json.Marshal(av); e != nil {
			return out, e
		}
		if out.After, e = json.Marshal(bv); e != nil {
			return out, e
		}
		out.Changes, e = json.Marshal(changes)
		return out, e
	}
}

// RunProjection consumes the durable proof prefix, never RPC state or raw
// window bytes. Apply alone uses the existing Store's shared fresh reader.
// At most one block is active; the owner's task set cancels/joins this worker.
func RunProjection(ctx context.Context, store *capture.Store, profile chains.BinaryProfile, d model.StateProjectionDescriptor, poll time.Duration) (err error) {
	defer func() {
		if ctx.Err() != nil && !errors.Is(err, ctx.Err()) {
			err = errors.Join(err, ctx.Err())
		}
	}()
	if store == nil || profile == nil || d.Validate() != nil {
		return port.ErrInvalidData
	}
	if poll == 0 {
		poll = 50 * time.Millisecond
	}
	if poll < 20*time.Millisecond || poll > 5*time.Second {
		return port.ErrInvalidData
	}
	derive := Projector(profile)
	for restarts := 0; ctx.Err() == nil; restarts++ {
		if restarts > 64 {
			return capture.ErrRebuildRequired
		}
		c, e := store.InitializeStateProjection(ctx, d)
		if e == capture.ErrCanonical {
			st, readErr := store.State(ctx)
			if readErr != nil {
				return readErr
			}
			if e = waitProjectionEpoch(ctx, store, st.Generation, poll); e == nil {
				continue
			}
		}
		if e != nil {
			return e
		}
		next := d.Stream.FromHeight
		if c.Complete != nil {
			next = c.Complete.Block.Number
		} // retry/reverify current head, no duplicate event
		for ctx.Err() == nil {
			st, readErr := store.State(ctx)
			if readErr != nil {
				return readErr
			}
			if st.Generation != c.Epoch {
				e = capture.ErrStale
				break
			}
			if st.Through.Number >= next {
				id, e0 := store.CanonicalAt(ctx, c.Epoch, next)
				if e0 == capture.ErrStale {
					e = e0
					break
				}
				if e0 != nil {
					return e0
				}
				started := time.Now()
				span := feedtelemetry.From(ctx).Start(feedtelemetry.StateApply)
				e = store.ApplyStateProjection(ctx, c.Epoch, d, id, derive)
				// Not-ready polling is not a completed operation or a processing error.
				if e != capture.ErrSupplementIncomplete {
					span.End(e)
				} else {
					span.Discard()
				}
				if e == nil {
					r := feedtelemetry.From(ctx)
					r.QueueAge(feedtelemetry.Proved, feedtelemetry.StateQueue, id, started)
					r.Complete(feedtelemetry.Projected, id)
				}
				if e == nil {
					if next == math.MaxUint64 {
						return model.ErrCaptureInvalid
					}
					next++
					continue
				}
				if e == capture.ErrStale || e == capture.ErrCanonical {
					break
				}
				if e != capture.ErrSupplementIncomplete {
					return e
				}
			}
			timer := time.NewTimer(poll)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e == capture.ErrCanonical {
			if e := waitProjectionEpoch(ctx, store, c.Epoch, poll); e != nil {
				return e
			}
		}
	}
	return ctx.Err()
}

func waitProjectionEpoch(ctx context.Context, store *capture.Store, epoch uint64, poll time.Duration) error {
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return capture.ErrCanonical
		case <-tick.C:
			st, e := store.State(ctx)
			if e != nil {
				return e
			}
			if st.Generation != epoch {
				return nil
			}
		}
	}
}
