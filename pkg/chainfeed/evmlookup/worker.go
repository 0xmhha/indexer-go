// Package evmlookup derives optional source search indexes from permanent,
// profile-verified execution inputs. It never acquires node datasets.
package evmlookup

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/0xmhha/indexer-go/internal/feedtelemetry"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

func Run(ctx context.Context, s *capture.Store, p chains.BinaryProfile) error {
	return run(ctx, s, p, false)
}
func RunSearch(ctx context.Context, s *capture.Store, p chains.BinaryProfile) error {
	return run(ctx, s, p, true)
}
func run(ctx context.Context, s *capture.Store, p chains.BinaryProfile, search bool) (err error) {
	if s == nil || p == nil {
		return port.ErrInvalidData
	}
	defer func() {
		if ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err)
		}
	}()
	d := model.SourceLookupDescriptor{Version: 1, Profile: p.ID()}
	name, definition := model.SourceLookupWorker, d.Definition()
	if search {
		name = model.SourceSearchWorker
		definition = (model.SourceSearchDescriptor{Version: 1, Profile: p.ID()}).Definition()
	}
	if d.Validate() != nil {
		return port.ErrInvalidData
	}
	for restarts := 0; ctx.Err() == nil; restarts++ {
		if restarts > 64 {
			return capture.ErrRebuildRequired
		}
		c, e := s.InitializeWorker(ctx, name, definition)
		if e != nil {
			return e
		}
		if search {
			_, e = s.SearchSnapshot(ctx, model.SourceSearchDescriptor{Version: 1, Profile: p.ID()}, c.Epoch)
		} else {
			_, e = s.LookupSnapshot(ctx, d, c.Epoch)
		}
		if errors.Is(e, capture.ErrStale) {
			continue
		}
		if e != nil {
			return e
		}

		e = runEpoch(ctx, s, p, d, c, search)
		if errors.Is(e, capture.ErrStale) || errors.Is(e, capture.ErrCanonical) {
			deadline := time.Now().Add(2 * time.Minute)
			for ctx.Err() == nil && time.Now().Before(deadline) {
				st, err := s.State(ctx)
				if err != nil {
					return err
				}
				if st.Generation != c.Epoch {
					break
				}
				if err = wait(ctx); err != nil {
					return err
				}
			}
			st, err := s.State(ctx)
			if err != nil {
				return err
			}
			if st.Generation != c.Epoch {
				continue
			}
		}
		return e
	}
	return ctx.Err()
}
func runEpoch(ctx context.Context, s *capture.Store, p chains.BinaryProfile, d model.SourceLookupDescriptor, c capture.WorkerCursor, search bool) error {
	if c.Through.Number == math.MaxUint64 {
		return port.ErrInvalidData
	}
	for next := c.Through.Number + 1; ctx.Err() == nil; next++ {
		for {
			st, e := s.State(ctx)
			if e != nil {
				return e
			}
			if st.Generation != c.Epoch {
				return capture.ErrStale
			}
			if st.Through.Number >= next {
				break
			}
			if e = wait(ctx); e != nil {
				return e
			}
		}
		id, e := s.CanonicalAt(ctx, c.Epoch, next)
		if e != nil {
			return e
		}
		verifyStage, commitStage := feedtelemetry.LookupVerify, feedtelemetry.LookupCommit
		if search {
			verifyStage, commitStage = feedtelemetry.SearchVerify, feedtelemetry.SearchCommit
		}
		verifySpan := feedtelemetry.From(ctx).Start(verifyStage)
		b, rs, e := evmdata.ReadCaptured(ctx, s, p, c.Epoch, id)
		verifySpan.End(e)
		if e != nil {
			return e
		}
		var data capture.SearchData
		hashes := make([]common.Hash, len(b.Transactions))
		for i, tx := range b.Transactions {
			hashes[i] = tx.Hash
			if search {
				if tx.Opaque {
					return model.ErrCaptureInvalid
				}
				data.Transactions = append(data.Transactions, capture.SearchTransaction{Hash: tx.Hash, From: tx.From, To: tx.To})
			}
		}
		if search {
			for i, r := range rs {
				for _, l := range r.Logs {
					data.Logs = append(data.Logs, capture.SearchLog{Address: l.Address, Topics: l.Topics, Transaction: uint32(i)})
				}
			}
		}
		commitSpan := feedtelemetry.From(ctx).Start(commitStage)
		if search {
			e = s.CommitSearch(ctx, model.SourceSearchDescriptor{Version: 1, Profile: p.ID()}, c.Epoch, id, data)
		} else {
			e = s.CommitLookup(ctx, d, c.Epoch, id, hashes)
		}
		commitSpan.End(e)
		if e != nil {
			return e
		}
		if next == math.MaxUint64 {
			return port.ErrInvalidData
		}
	}
	return ctx.Err()
}
func wait(ctx context.Context) error {
	t := time.NewTimer(50 * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
