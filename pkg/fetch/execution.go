package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/ethereum/go-ethereum/common"
)

// SetExecutionCanonical must receive the uncached source backed by the existing
// shared RPC pool. It is configured before ingest, never replaced by raw input.
func (f *Fetcher) SetExecutionCanonical(r source.CanonicalReader) { f.executionCanonical = r }

func (f *Fetcher) ValidateExecutionReorg(ctx context.Context, ref execwire.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.executionCanonical == nil {
		return port.ErrInvalidData
	}
	call, cancel := f.rpcCtx(ctx)
	defer cancel()
	hash, err := f.executionCanonical.HashAt(call, ref.Height)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %w", port.ErrExecutionCanonicalLag, err)
	}
	if execwire.Hash(hash) != ref.Hash {
		return port.ErrExecutionCanonicalLag
	}
	return nil
}

func (f *Fetcher) ConfirmExecution(ctx context.Context, payload []byte) (bool, error) {
	store, ok := f.storage.(port.ExecutionConfirmer)
	if !ok || f.executionCanonical == nil {
		return false, port.ErrInvalidData
	}
	return store.ConfirmExecution(ctx, payload, func(bound context.Context, ref execwire.Ref) error {
		call, cancel := f.rpcCtx(bound)
		defer cancel()
		hash, err := f.executionCanonical.HashAt(call, ref.Height)
		if err != nil {
			return err
		}
		if execwire.Hash(hash) != ref.Hash {
			return &ReorgError{Height: ref.Height}
		}
		return nil
	})
}

// PublishExecution applies legacy features exactly once after core/state parts.
// The store owns the last flush/fresh/commit boundary. It sends no source ACK;
// the IPC owner may ACK only after this method returns successfully.
func (f *Fetcher) PublishExecution(ctx context.Context, r execwire.Result, b *model.Block, rs []*model.Receipt, digest execwire.Hash) error {
	publisher, ok := f.storage.(port.ExecutionPublisher)
	if !ok || f.executionCanonical == nil || b == nil || digest == (execwire.Hash{}) {
		return port.ErrInvalidData
	}
	if r.Ref.Height != b.Number || r.Ref.Hash != execwire.Hash(b.Hash) || r.Parent != execwire.Hash(b.ParentHash) || r.StateRoot != execwire.Hash(b.StateRoot) {
		return port.ErrExecutionConflict
	}
	fb, err := fetchedFromModel(b, rs)
	if err != nil {
		return err
	}
	return f.write().do(ctx, "executionPublication", func(ctx context.Context) error {
		var pending []events.Event
		f.pendingEvents = &pending
		defer func() { f.pendingEvents = nil }()
		committed, err := publisher.CommitExecution(ctx, port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Digest: digest, Root: r.StateRoot}, func(bound context.Context) error {
			var native []feature.NativeBalanceChange
			for _, step := range r.Steps {
				for _, change := range step.Accounts {
					native = append(native, feature.NativeBalanceChange{Address: common.Address(change.Address), Before: new(big.Int).SetBytes(change.Before.Balance[:]), After: new(big.Int).SetBytes(change.After.Balance[:]), TxHash: common.Hash(step.TxHash), Ordinal: step.Ordinal})
				}
			}
			bound, bindErr := feature.WithNativeBalances(bound, b.Number, b.Hash, native)
			if bindErr != nil {
				return bindErr
			}
			// Prepare only on the new-publication callback: retries after a lost ACK
			// cannot require old node state or apply a newly enabled feature silently.
			limit := f.config.StateMaxBytes
			if limit <= 0 {
				limit = defaultStateMaxBytes
			}
			writers, err := f.features.PrepareWriters(bound, fb.featureBlock(), f.config.StatePrefetch, limit)
			if err != nil {
				return err
			}
			if err := f.processBlockMetadata(bound, fb); err != nil {
				return err
			}
			f.publish(f.blockEvent(fb))
			if err := writers.Apply(bound, fb.featureBlock(), func(ctx context.Context, unit feature.Unit) error {
				return f.recordExecutionWriter(ctx, unit, r, digest)
			}); err != nil {
				return err
			}
			if b.Number != r.Ref.Height || execwire.Hash(b.Hash) != r.Ref.Hash || execwire.Hash(b.ParentHash) != r.Parent || execwire.Hash(b.StateRoot) != r.StateRoot {
				return port.ErrExecutionConflict
			}
			f.publishBlockEvents(fb)
			if f.outbox != nil {
				if err := f.recordEvents(bound, pending); err != nil {
					return err
				}
			}
			if f.beforeCommitHook != nil {
				return f.beforeCommitHook(b.Number)
			}
			return nil
		}, func(bound context.Context) error {
			call, cancel := f.rpcCtx(bound)
			defer cancel()
			hash, err := f.executionCanonical.HashAt(call, r.Ref.Height)
			if err != nil {
				return err
			}
			if execwire.Hash(hash) != r.Ref.Hash {
				return &ReorgError{Height: r.Ref.Height}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if committed {
			f.pendingEvents = nil
			f.committed(pending, b.Number)
			f.metrics.RecordBlockProcessed(len(rs))
		}
		return nil
	})
}

const ExecutionFeaturePrefix = "/meta/execution/units/"

type ExecutionFeatureCheckpoint struct {
	Version    int
	Definition string
	Ref        execwire.Ref
	Digest     execwire.Hash
}

// recordExecutionWriter is called only after all hooks of this unit succeed.
// Its checkpoint and feature writes share the final publication's undo/commit.
func (f *Fetcher) recordExecutionWriter(ctx context.Context, unit feature.Unit, r execwire.Result, digest execwire.Hash) error {
	kv, ok := f.storage.(port.KVStore)
	if !ok {
		return port.ErrInvalidData
	}
	data, err := json.Marshal(ExecutionFeatureCheckpoint{Version: 1, Definition: unit.Definition, Ref: r.Ref, Digest: digest})
	if err != nil {
		return err
	}
	return kv.Put(ctx, []byte(ExecutionFeaturePrefix+unit.Name), data)
}

// RecoverExecution serializes feature rollback/outbox with the same writer
// used for publication. Store recovery owns the last fresh/generation/COMMIT.
func (f *Fetcher) RecoverExecution(ctx context.Context) error {
	store, ok := f.storage.(port.ExecutionRewindStore)
	if !ok || f.executionCanonical == nil {
		return port.ErrInvalidData
	}
	return f.write().do(ctx, "executionRewind", func(ctx context.Context) error {
		withdrawn := map[uint64][]events.Event{}
		reorg, err := store.ResumeExecutionRewind(ctx, f.rollbackHook(withdrawn), func(ctx context.Context, target execwire.Ref) error {
			call, cancel := f.rpcCtx(ctx)
			defer cancel()
			hash, err := f.executionCanonical.HashAt(call, target.Height)
			if err != nil {
				return err
			}
			if execwire.Hash(hash) != target.Hash {
				return &ReorgError{Height: target.Height}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if reorg != nil {
			f.publishReorg(reorg, withdrawn)
			f.metrics.RecordReorg(reorg.OldHead - reorg.ForkNumber)
		}
		return nil
	})
}

// ExecutionRecoveryTarget finds the newest common PUBLIC parent. Unpublished
// capture may be much farther ahead; it does not enlarge the undo window.
func (f *Fetcher) ExecutionRecoveryTarget(ctx context.Context) (execwire.Ref, execwire.Hash, error) {
	store, ok := f.storage.(port.ExecutionStore)
	if !ok || f.executionCanonical == nil {
		return execwire.Ref{}, execwire.Hash{}, port.ErrInvalidData
	}
	c, err := store.ExecutionStatus(ctx)
	if err != nil {
		return execwire.Ref{}, execwire.Hash{}, err
	}
	if !c.BaselineReady {
		return execwire.Ref{}, execwire.Hash{}, port.ErrExecutionCoverage
	}
	if c.Visible.Height < c.Anchor.Height {
		return execwire.Ref{}, execwire.Hash{}, port.ErrExecutionConflict
	}
	for h := c.Visible.Height; ; h-- {
		if c.Visible.Height-h > 128 {
			return execwire.Ref{}, execwire.Hash{}, ErrReorgTooDeep
		}
		b, err := f.storage.GetBlock(ctx, h)
		if errors.Is(err, port.ErrNotFound) {
			return execwire.Ref{}, execwire.Hash{}, fmt.Errorf("%w: recovery block %d", port.ErrExecutionCoverage, h)
		}
		if err != nil {
			return execwire.Ref{}, execwire.Hash{}, err
		}
		if h == c.Anchor.Height && execwire.Hash(b.Hash) != c.Anchor.Hash {
			return execwire.Ref{}, execwire.Hash{}, port.ErrExecutionConflict
		}
		call, cancel := f.rpcCtx(ctx)
		hash, err := f.executionCanonical.HashAt(call, h)
		cancel()
		if err != nil && !errors.Is(err, source.ErrNotFound) {
			return execwire.Ref{}, execwire.Hash{}, err
		}
		if err == nil && hash == b.Hash {
			if b.StateRoot == ([32]byte{}) {
				return execwire.Ref{}, execwire.Hash{}, port.ErrInvalidData
			}
			after, err := store.ExecutionStatus(ctx)
			if err != nil {
				return execwire.Ref{}, execwire.Hash{}, err
			}
			if after.Generation != c.Generation || after.Visible != c.Visible {
				return execwire.Ref{}, execwire.Hash{}, port.ErrExecutionStale
			}
			return execwire.Ref{Generation: c.Generation, Height: h, Hash: execwire.Hash(hash)}, execwire.Hash(b.StateRoot), nil
		}
		if h == c.Anchor.Height || h == 0 {
			return execwire.Ref{}, execwire.Hash{}, ErrReorgTooDeep
		}
	}
}
