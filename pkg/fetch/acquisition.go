package fetch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

const (
	defaultQueueBlocks   = 256
	defaultQueueMaxBytes = 256 << 20
)

type acquiredBlock struct {
	result *jobResult
	bytes  int64
	queued time.Time
}

// indexAcquired uses two independent bounds: a small acquisition/reorder
// window and a larger ordered processing queue. Acquisition permits are
// returned on handoff, not on commit. Only acquisition reads prunable initial
// balances and contract metadata; processing retains canonical checks and
// the existing atomic writer. No RPC client or rate limiter is added here.
// A nil end follows the live target without waiting for a chunk to commit.
func (f *Fetcher) indexAcquired(ctx context.Context, start uint64, end *uint64) error {
	if !f.config.StatePrefetch {
		return fmt.Errorf("acquisition_queue requires state_prefetch")
	}
	if end != nil && *end < start {
		return nil
	}
	capacity, maxBytes := f.config.QueueBlocks, f.config.QueueMaxBytes
	if capacity == 0 {
		capacity = defaultQueueBlocks
	}
	if maxBytes == 0 {
		maxBytes = defaultQueueMaxBytes
	}
	if capacity < 1 || capacity > 65536 || maxBytes < 1 {
		return fmt.Errorf("invalid acquisition queue limits")
	}
	var committed *uint64
	if height, err := f.storage.GetLatestHeight(ctx); err == nil {
		committed = &height
	} else if !errors.Is(err, port.ErrNotFound) {
		return fmt.Errorf("read committed cursor: %w", err)
	}
	workers := max(1, f.workers())
	if end != nil && *end-start < uint64(workers) {
		workers = int(*end - start + 1)
	}
	window := 2 * workers
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	acquiring := make(chan struct{}, window)
	queueSlots := semaphore.NewWeighted(int64(capacity))
	queueBytes := semaphore.NewWeighted(maxBytes)
	heights := make(chan uint64)
	results := make(chan *jobResult, window)
	ready := make(chan acquiredBlock, capacity)
	var held, stateBytes, queued, bytes atomic.Int64
	var workersWG, allWG sync.WaitGroup
	f.pipelineMetrics.queueLimits(capacity, maxBytes, committed)
	allWG.Add(1)
	go func() {
		defer allWG.Done()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				target, observed := f.Progress()
				f.logger.Info("Acquisition queue", zap.Uint64("target_height", target), zap.Bool("target_observed", observed), zap.Any("pipeline", f.GetPipelineMetrics()))
			}
		}
	}()

	allWG.Add(1)
	go func() {
		defer allWG.Done()
		defer close(heights)
		for h := start; end == nil || h <= *end; h++ {
			if end == nil && f.awaitAcquisitionTarget(ctx, h) != nil {
				return
			}
			select {
			case acquiring <- struct{}{}:
			case <-ctx.Done():
				return
			}
			held.Add(1)
			f.pipelineMetrics.addInFlight(1)
			select {
			case heights <- h:
			case <-ctx.Done():
				return
			}
			if h == ^uint64(0) {
				return
			}
		}
	}()
	for range workers {
		workersWG.Add(1)
		go func() {
			defer workersWG.Done()
			for h := range heights {
				began := f.pipelineMetrics.start()
				r := f.fetchJob(ctx, h)
				f.pipelineMetrics.finish(stageAcquire, began, r.err != nil)
				if r.block != nil {
					size := r.block.prepared.Bytes()
					stateBytes.Add(size)
					f.pipelineMetrics.addStateBytes(size)
				}
				select {
				case results <- r:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	allWG.Add(1)
	go func() {
		defer allWG.Done()
		workersWG.Wait()
		close(results)
	}()
	allWG.Add(1)
	go func() {
		defer allWG.Done()
		defer close(ready)
		pending := make(map[uint64]*jobResult, window)
		next := start
		for r := range results {
			pending[r.height] = r
			for r, ok := pending[next]; ok; r, ok = pending[next] {
				size := int64(0)
				if r.err == nil {
					f.pipelineMetrics.acquired(next)
					size, r.err = acquiredPayloadBytes(r.block)
					if r.err == nil && size > maxBytes {
						r.err = fmt.Errorf("block %d payload %d exceeds queue_max_bytes %d", next, size, maxBytes)
					}
				}
				// Admit bytes in height order: out-of-order large blocks must
				// never consume the budget needed by the next writer block.
				wait := f.pipelineMetrics.start()
				if err := queueSlots.Acquire(ctx, 1); err != nil {
					return
				}
				if r.err != nil {
					size = 0
				}
				if err := queueBytes.Acquire(ctx, size); err != nil {
					queueSlots.Release(1)
					return
				}
				f.pipelineMetrics.finish(stageHandoff, wait, false)
				queued.Add(1)
				bytes.Add(size)
				f.pipelineMetrics.addQueue(1, size)
				select {
				case ready <- acquiredBlock{result: r, bytes: size, queued: time.Now()}:
				case <-ctx.Done():
					return
				}
				<-acquiring // processing owns this block now
				delete(pending, next)
				if r.err != nil || next == ^uint64(0) {
					return // error follows all lower blocks through the queue
				}
				next++
			}
		}
	}()
	defer func() {
		cancel()
		allWG.Wait() // all producers honour cancellation; no send after close
		f.pipelineMetrics.addInFlight(-held.Swap(0))
		f.pipelineMetrics.addStateBytes(-stateBytes.Swap(0))
		f.pipelineMetrics.addQueue(-queued.Swap(0), -bytes.Swap(0))
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case block, ok := <-ready:
			if !ok {
				return ctx.Err()
			}
			r := block.result
			if r.err != nil {
				return fmt.Errorf("failed to fetch block %d: %w", r.height, r.err)
			}
			f.pipelineMetrics.finish(stageQueueWait, block.queued, false)
			f.offerBlock(r.block)
			began := f.pipelineMetrics.start()
			err := f.indexBlock(ctx, r.block)
			f.pipelineMetrics.finish(stageWrite, began, err != nil)
			if err != nil {
				return err
			}
			f.pipelineMetrics.committed(r.height)
			held.Add(-1)
			stateBytes.Add(-r.block.prepared.Bytes())
			queued.Add(-1)
			bytes.Add(-block.bytes)
			f.pipelineMetrics.addInFlight(-1)
			f.pipelineMetrics.addStateBytes(-r.block.prepared.Bytes())
			f.pipelineMetrics.addQueue(-1, -block.bytes)
			queueBytes.Release(block.bytes)
			queueSlots.Release(1)
			if (r.height-start+1)%100 == 0 || end != nil && r.height == *end {
				f.logger.Info("Acquisition progress", zap.Uint64("height", r.height), zap.Any("pipeline", f.GetPipelineMetrics()))
			}
		}
	}
}

// Account serialized model payload and prepared state, not the Go heap. The
// bounded acquisition window additionally owns up to 2*workers decoded blocks;
// the processing budget includes its active writer block. Heap RSS is separate.
func acquiredPayloadBytes(b *fetchedBlock) (int64, error) {
	encoded, err := model.EncodeBlock(b.block)
	if err != nil {
		return 0, err
	}
	size := int64(len(encoded)) + b.prepared.Bytes()
	for _, r := range b.receipts {
		encoded, err = model.EncodeReceipt(r)
		if err != nil {
			return 0, err
		}
		size += int64(len(encoded))
	}
	return size, nil
}

func (f *Fetcher) awaitAcquisitionTarget(ctx context.Context, height uint64) error {
	age := f.pollInterval()
	for ctx.Err() == nil {
		if target, ok := f.Progress(); ok && height <= target {
			return nil
		}
		// Share cached/in-flight head observations with followTarget instead
		// of adding a head RPC for each block or each writer commit.
		target, ok, err := f.readTargetHead(ctx, true, age)
		age = f.pollInterval()
		if err == nil && ok {
			f.noteTarget(target)
			if height <= target {
				return nil
			}
		}
		if err != nil {
			f.logger.Warn("Acquisition head read failed", zap.Error(err))
			if err := sleepCtx(ctx, f.config.RetryDelay); err != nil {
				return err
			}
		} else {
			fresh, err := f.waitHeadSignal(ctx)
			if err != nil {
				return err
			}
			if fresh {
				age = 0
			}
		}
	}
	return ctx.Err()
}

func (f *Fetcher) runAcquired(ctx context.Context) error {
	next := f.GetNextHeight(ctx)
	for ctx.Err() == nil {
		// After errors/reorgs restart from the committed cursor, discarding
		// the entire speculative queue and its hash-bound state inputs.
		err := f.indexAcquired(ctx, next, nil)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var reorg *ReorgError
		if errors.As(err, &reorg) {
			fork, rollbackErr := f.HandleReorg(ctx, reorg.Height)
			if errors.Is(rollbackErr, ErrReorgTooDeep) {
				return rollbackErr
			}
			if rollbackErr == nil {
				next = fork + 1
				continue
			}
			err = rollbackErr
		}
		f.logger.Error("Acquisition pipeline failed", zap.Error(err))
		if err := sleepCtx(ctx, f.config.RetryDelay); err != nil {
			return err
		}
		next = f.GetNextHeight(ctx)
	}
	return ctx.Err()
}
