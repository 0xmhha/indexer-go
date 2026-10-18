// Package projection contains independent, block-scoped facts workers. Their
// immutable outputs, cursor and outbox live in the capture namespace, never
// in the existing feature writer's globally ordered undo/outbox.
package projection

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmlookup"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/source/facts"
	"github.com/ethereum/go-ethereum/common"
)

const Summary = "execution.summary.v1"
const summaryDefinition = "evm-commitments-summary-v1"

func Supported(name string) bool {
	return name == Summary || name == model.SourceLookupWorker || name == model.SourceSearchWorker
}

type SummaryOutput struct {
	Block             model.FeedBlock `json:"block"`
	Transactions      int             `json:"transactions"`
	Receipts          int             `json:"receipts"`
	Successful        int             `json:"successful"`
	Failed            int             `json:"failed"`
	Logs              int             `json:"logs"`
	ContractCreations int             `json:"contractCreations"`
	GasUsed           uint64          `json:"gasUsed"`
}

func Run(ctx context.Context, name string, store *capture.Store, profile chains.BinaryProfile, fresh source.CanonicalReader) error {
	if !Supported(name) || store == nil || profile == nil || fresh == nil {
		return model.ErrCaptureInvalid
	}
	if name == model.SourceSearchWorker {
		return evmlookup.RunSearch(ctx, store, profile)
	}
	if name == model.SourceLookupWorker {
		return evmlookup.Run(ctx, store, profile)
	}
	for restarts := 0; ctx.Err() == nil; restarts++ {
		if restarts > 64 {
			return capture.ErrRebuildRequired
		}
		cursor, e := store.InitializeWorker(ctx, name, summaryDefinition)
		if e != nil {
			return e
		}
		src, e := facts.New(ctx, facts.Config{Store: store, Profile: profile, Canonical: fresh, Consumer: name})
		if e == capture.ErrStale {
			continue
		}
		if e != nil {
			return e
		}
		if src.Epoch() != cursor.Epoch {
			continue
		}
		e = runEpoch(ctx, name, store, src, cursor)
		if e == capture.ErrStale || e == capture.ErrCanonical {
			// Canonical mismatch is resolved only by the capture supervisor. Do not
			// skip ahead, forge a new epoch or spin fresh RPCs on an obsolete fork.
			deadline := time.NewTimer(2 * time.Minute)
			tick := time.NewTicker(50 * time.Millisecond)
			changed := false
		wait:
			for {
				select {
				case <-ctx.Done():
					e = ctx.Err()
					break wait
				case <-deadline.C:
					break wait
				case <-tick.C:
					st, err := store.State(ctx)
					if err != nil {
						e = err
						break wait
					}
					if st.Generation != src.Epoch() {
						changed = true
						break wait
					}
				}
			}
			deadline.Stop()
			tick.Stop()
			if changed {
				continue
			}
		}
		return e
	}
	return ctx.Err()
}
func runEpoch(ctx context.Context, name string, store *capture.Store, src *facts.Source, cursor capture.WorkerCursor) error {
	if cursor.Through.Number == math.MaxUint64 {
		return model.ErrCaptureInvalid
	}
	next := cursor.Through.Number + 1
	for ctx.Err() == nil {
		head, e := src.Head(ctx)
		if e != nil {
			return e
		}
		if next > head {
			if e = pause(ctx); e != nil {
				return e
			}
			continue
		}
		block, rs, e := src.BlockWithReceipts(ctx, next)
		if e != nil {
			return e
		}
		in, e := src.ProjectionInput(ctx, block)
		if e != nil {
			return e
		}
		value := SummaryOutput{Block: in.Identity.Block, Transactions: len(block.Transactions), Receipts: len(rs), GasUsed: block.GasUsed}
		for _, r := range rs {
			if r.Status == 1 {
				value.Successful++
			} else {
				value.Failed++
			}
			value.Logs += len(r.Logs)
			if r.ContractAddress != nil && *r.ContractAddress != (common.Address{}) {
				value.ContractCreations++
			}
		}
		data, e := json.Marshal(value)
		if e != nil {
			return e
		}
		if e = store.CommitWorker(ctx, name, summaryDefinition, src.Epoch(), in.Identity, data); e != nil {
			return e
		}
		if next == math.MaxUint64 {
			return model.ErrCaptureInvalid
		}
		next++
	}
	return ctx.Err()
}
func pause(ctx context.Context) error {
	t := time.NewTimer(50 * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
