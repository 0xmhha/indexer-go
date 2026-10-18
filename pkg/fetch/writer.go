package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0xmhha/indexer-go/pkg/source"
	"runtime/debug"
	"sync"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
	unitprogress "github.com/0xmhha/indexer-go/pkg/progress"
)

// The writer is the single goroutine that changes indexed state. Indexing a
// block, moving the cursor, rolling back after a reorg and backfilling a
// feature are commands sent to it; it runs them one at a time, in the order
// they arrive. Only this file opens storage block transactions (a test
// enforces it), so every state change is serialized in one place and
// recovery after a crash has a single entry point.

// ErrWriterClosed is returned for commands sent after the fetcher closed.
var ErrWriterClosed = errors.New("fetch: writer is closed")

// writerQueue is how many commands may wait for the writer.
const writerQueue = 64

type writerCmd struct {
	name  string
	ctx   context.Context
	run   func(ctx context.Context) error
	reply chan error
}

type writer struct {
	logger  *zap.Logger
	cmds    chan writerCmd
	stopped chan struct{}

	mu     sync.Mutex // guards closed and sending on cmds
	closed bool
}

// inWriterKey marks contexts of commands running on the writer, so a command
// that issues another command runs it inline instead of deadlocking.
type inWriterKey struct{}

func newWriter(logger *zap.Logger) *writer {
	w := &writer{
		logger:  logger,
		cmds:    make(chan writerCmd, writerQueue),
		stopped: make(chan struct{}),
	}
	go w.loop()
	return w
}

func (w *writer) loop() {
	defer close(w.stopped)
	for cmd := range w.cmds {
		cmd.reply <- w.exec(cmd)
	}
}

// exec runs a command, turning a panic into an error so one bad command does
// not stop the writer.
func (w *writer) exec(cmd writerCmd) (err error) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("writer command panicked", zap.String("command", cmd.name), zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
			err = fmt.Errorf("writer command %s panicked: %v", cmd.name, r)
		}
	}()
	return cmd.run(context.WithValue(cmd.ctx, inWriterKey{}, true))
}

// do runs fn on the writer and returns its error. It waits for the command
// to finish; fn should honour ctx to stop early.
func (w *writer) do(ctx context.Context, name string, fn func(ctx context.Context) error) error {
	if ctx.Value(inWriterKey{}) != nil {
		return fn(ctx) // already on the writer
	}
	reply := make(chan error, 1)
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return ErrWriterClosed
	}
	select {
	case w.cmds <- writerCmd{name: name, ctx: ctx, run: fn, reply: reply}:
	case <-ctx.Done():
		w.mu.Unlock()
		return ctx.Err()
	}
	w.mu.Unlock()
	return <-reply
}

// close stops accepting commands, lets queued ones finish and waits for the
// writer to exit.
func (w *writer) close() {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.cmds)
	}
	w.mu.Unlock()
	<-w.stopped
}

// write returns the fetcher's writer, starting it on first use.
func (f *Fetcher) write() *writer {
	f.writerOnce.Do(func() { f.writerInst = newWriter(f.logger) })
	return f.writerInst
}

// Close stops the writer after the commands already sent have run, then the
// event relay. The fetcher must not index after Close.
func (f *Fetcher) Close() {
	f.bgOnce.Do(func() {}) // background work never started: nothing to stop
	if f.bgCancel != nil {
		f.bgCancel()
	}
	f.bgWG.Wait()
	f.writerOnce.Do(func() {}) // never start a writer just to close it
	if f.writerInst != nil {
		f.writerInst.close()
	}
	f.stopRelay()
}

// ============================================================================
// Commands. Each opens at most one block transaction and runs only on the
// writer goroutine.
// ============================================================================

// indexBlock stores one fetched block with all derived indexes and advances
// the cursor in a single storage transaction. It is used by both the live
// loop and gap recovery.
//
//   - A block whose hash is already stored at its height is skipped, so
//     processing a block twice never changes storage.
//   - The cursor only moves forward, so filling a gap below it does not
//     rewind it.
//   - Events are recorded in the outbox in the same transaction and
//     delivered by the relay after it commits (outbox.go).
func (f *Fetcher) indexBlock(ctx context.Context, fb *fetchedBlock) error {
	if f.txr == nil {
		return errNoBlockTransactions
	}
	err := f.write().do(ctx, "indexBlock", func(ctx context.Context) error {
		return f.writeBlock(ctx, fb)
	})
	if err != nil {
		return err
	}
	if acknowledger, ok := f.src.(source.CommitAcknowledger); ok {
		started := f.pipelineMetrics.start()
		err := acknowledger.Acknowledge(ctx, fb.block)
		f.pipelineMetrics.finish(stageSourceAck, started, err != nil)
		if err != nil {
			return fmt.Errorf("block %d is durable but feed ACK failed: %w", fb.height(), err)
		}
	}
	return nil
}

func (f *Fetcher) writeBlock(ctx context.Context, fb *fetchedBlock) error {
	height := fb.height()

	storedHash, err := f.storedBlockHash(ctx, height)
	switch {
	case err == nil && storedHash == fb.block.Hash:
		f.logger.Debug("Block already indexed, skipping", zap.Uint64("height", height))
		return f.writeCursorOnlyValidated(ctx, fb)
	case err == nil:
		return fmt.Errorf("%w (height %d stored %s fetched %s): %w", ErrBlockConflict, height, storedHash.Hex(), fb.block.Hash.Hex(), &ReorgError{Height: height})
	case !errors.Is(err, port.ErrNotFound):
		return fmt.Errorf("check stored block %d: %w", height, err)
	}
	if err := f.checkParent(ctx, fb); err != nil {
		return err
	}
	if err := f.checkPreparedState(ctx, fb); err != nil {
		return err
	}

	txCtx, tx, err := f.txr.BeginBlock(ctx)
	if err != nil {
		return fmt.Errorf("begin block %d: %w", height, err)
	}
	tx.SetHeight(height) // record undo so a reorg can roll the block back
	defer tx.Rollback()  // no-op after Commit

	var pending []events.Event
	f.pendingEvents = &pending
	defer func() { f.pendingEvents = nil }()

	if err := f.applyBlock(txCtx, fb); err != nil {
		return err
	}
	if err := f.recordFeatureProgress(txCtx, fb, f.features, true, true); err != nil {
		return err
	}
	if err := f.advanceCursor(txCtx, height); err != nil {
		return err
	}
	if f.outbox != nil {
		if err := f.recordEvents(txCtx, pending); err != nil {
			return fmt.Errorf("record events of block %d: %w", height, err)
		}
	}
	if f.beforeCommitHook != nil {
		if err := f.beforeCommitHook(height); err != nil {
			return err
		}
	}
	if err := f.validateSourceCommit(txCtx, fb); err != nil {
		return err
	}
	commitStarted := f.pipelineMetrics.start()
	err = tx.Commit()
	f.pipelineMetrics.finish(stageCommit, commitStarted, err != nil)
	if err != nil {
		return fmt.Errorf("commit block %d: %w", height, err)
	}

	f.pendingEvents = nil
	f.committed(pending, height)

	f.metrics.RecordBlockProcessed(len(fb.receipts))
	f.logger.Info("Successfully indexed block",
		zap.Uint64("height", height),
		zap.String("hash", fb.block.Hash.Hex()),
		zap.Int("txs", len(fb.block.Transactions)),
		zap.Int("receipts", len(fb.receipts)),
	)
	return nil
}

func (f *Fetcher) validateSourceCommit(ctx context.Context, fb *fetchedBlock) (err error) {
	validator, ok := f.src.(source.CommitValidator)
	if !ok {
		return nil
	}
	started := f.pipelineMetrics.start()
	defer func() { f.pipelineMetrics.finish(stageSourceValidate, started, err != nil) }()
	durable, ok := f.storage.(port.BlockDurability)
	if !ok {
		return errors.New("fetch: validated source requires explicit block durability")
	}
	if err := durable.RequireBlockDurability(ctx); err != nil {
		return err
	}
	rctx, cancel := f.rpcCtx(ctx)
	defer cancel()
	if err := validator.ValidateCommit(rctx, fb.block); err != nil {
		return fmt.Errorf("validate source block %d before commit: %w", fb.height(), err)
	}
	return nil
}

// A committed block retried after a lost ACK still needs a fresh check before
// moving a cursor or delivering another source ACK. It retains its old undo.
func (f *Fetcher) writeCursorOnlyValidated(ctx context.Context, fb *fetchedBlock) error {
	if _, ok := f.src.(source.CommitValidator); !ok {
		return f.writeCursorOnly(ctx, fb.height())
	}
	txCtx, tx, err := f.txr.BeginBlock(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f.recordFeatureProgress(txCtx, fb, f.features, false, true); err != nil {
		return err
	}
	if err := f.advanceCursor(txCtx, fb.height()); err != nil {
		return err
	}
	if err := f.validateSourceCommit(txCtx, fb); err != nil {
		return err
	}
	return tx.Commit()
}

// writeCursorOnly moves the cursor for a skipped, already stored block in its
// own small transaction. It records no undo: the block's undo record from
// when it was indexed must stay.
func (f *Fetcher) writeCursorOnly(ctx context.Context, height uint64) error {
	txCtx, tx, err := f.txr.BeginBlock(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f.advanceCursor(txCtx, height); err != nil {
		return err
	}
	return tx.Commit()
}

// rollbackTo rolls the database back to height `to` on the writer. The
// reorganization's events are recorded in the outbox with each rolled-back
// block (rollbackHook), so they precede the events of the new branch and
// survive a crash during the rollback.
func (f *Fetcher) rollbackTo(ctx context.Context, rb port.Rollbacker, to uint64) error {
	return f.write().do(ctx, "rollback", func(ctx context.Context) error {
		withdrawn := map[uint64][]events.Event{}
		r, err := rb.RollbackTo(ctx, to, f.rollbackHook(withdrawn))
		if err != nil {
			return err
		}
		if r != nil {
			f.publishReorg(r, withdrawn)
		}
		return nil
	})
}

// backfillBlock runs a backfill pipeline for one stored block on the writer.
func (f *Fetcher) backfillBlock(ctx context.Context, p *feature.Pipeline, fb *fetchedBlock, progress func(context.Context, uint64) error) error {
	return f.write().do(ctx, "backfillBlock", func(ctx context.Context) error {
		return f.writeBackfillBlock(ctx, p, fb, progress)
	})
}

func (f *Fetcher) writeBackfillBlock(ctx context.Context, p *feature.Pipeline, fb *fetchedBlock, progress func(context.Context, uint64) error) error {
	h := fb.height()
	txCtx, tx, err := f.txr.BeginBlock(ctx)
	if err != nil {
		return fmt.Errorf("backfill: begin block %d: %w", h, err)
	}
	defer tx.Rollback() // no-op after Commit

	if err := p.HandleBlock(txCtx, &feature.Block{
		Model: fb.block, Receipts: fb.receipts, Geth: fb.geth, GethReceipts: fb.gethReceipts,
	}); err != nil {
		return fmt.Errorf("backfill: block %d: %w", h, err)
	}
	if err := f.recordFeatureProgress(txCtx, fb, p, true, false); err != nil {
		return err
	}
	if progress != nil {
		if err := progress(txCtx, h); err != nil {
			return fmt.Errorf("backfill: record progress at %d: %w", h, err)
		}
	}
	// The block's undo record does not cover what the backfill wrote, so it
	// can no longer be rolled back exactly; drop it so a reorg reaching this
	// block stops instead of leaving the backfilled data behind.
	if u, ok := f.storage.(undoDropper); ok {
		if err := u.DropUndo(txCtx, h); err != nil {
			return fmt.Errorf("backfill: drop undo of %d: %w", h, err)
		}
	}
	if f.beforeCommitHook != nil {
		if err := f.beforeCommitHook(h); err != nil {
			return err
		}
	}
	if err := f.validateSourceCommit(txCtx, fb); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("backfill: commit block %d: %w", h, err)
	}
	return nil
}

// Exec runs fn on the writer, ordered with block indexing. Use it for state
// changes outside block processing (for example recording feature state).
func (f *Fetcher) Exec(ctx context.Context, name string, fn func(ctx context.Context) error) error {
	return f.write().do(ctx, name, fn)
}

// writeSparse stores a range's blocks with declared logs and moves the
// cursor to the range's end, in one transaction. No undo is recorded:
// finalized blocks are not rolled back. A block already stored with the
// same hash is skipped, so indexing a range again changes nothing.
func (f *Fetcher) writeSparse(ctx context.Context, blocks []*fetchedBlock, end uint64) error {
	txCtx, tx, err := f.txr.BeginBlock(ctx)
	if err != nil {
		return fmt.Errorf("begin blocks up to %d: %w", end, err)
	}
	defer tx.Rollback()

	var pending []events.Event
	f.pendingEvents = &pending
	defer func() { f.pendingEvents = nil }()

	for _, fb := range blocks {
		stored, err := f.storedBlockHash(txCtx, fb.height())
		switch {
		case err == nil && stored == fb.block.Hash:
			continue
		case err == nil:
			return fmt.Errorf("%w (height %d stored %s fetched %s): a finalized block changed",
				ErrBlockConflict, fb.height(), stored.Hex(), fb.block.Hash.Hex())
		case !errors.Is(err, port.ErrNotFound):
			return fmt.Errorf("check stored block %d: %w", fb.height(), err)
		}
		if err := f.applyBlock(txCtx, fb); err != nil {
			return err
		}
	}
	if err := f.advanceCursor(txCtx, end); err != nil {
		return err
	}
	if f.outbox != nil {
		if err := f.recordEvents(txCtx, pending); err != nil {
			return fmt.Errorf("record events up to %d: %w", end, err)
		}
	}
	if f.beforeCommitHook != nil {
		if err := f.beforeCommitHook(end); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit blocks up to %d: %w", end, err)
	}
	f.pendingEvents = nil
	f.committed(pending, end)
	f.logger.Info("Indexed declared logs", zap.Uint64("to", end), zap.Int("blocks_with_logs", len(blocks)))
	return nil
}

// writeBackfillProgress records a backfill's progress at end in its own
// transaction (heights without blocks to process, backfillFromNode).
func (f *Fetcher) writeBackfillProgress(ctx context.Context, end uint64, progress func(ctx context.Context, height uint64) error) error {
	txCtx, tx, err := f.txr.BeginBlock(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := progress(txCtx, end); err != nil {
		return fmt.Errorf("backfill: record progress at %d: %w", end, err)
	}
	return tx.Commit()
}

// Metadata joins the same undo/outbox transaction as the actual handlers.
func (f *Fetcher) recordFeatureProgress(ctx context.Context, fb *fetchedBlock, p *feature.Pipeline, applied, head bool) (err error) {
	reporter, ok := f.src.(source.ProjectionReporter)
	if !ok {
		return nil
	}
	started := f.pipelineMetrics.start()
	defer func() { f.pipelineMetrics.finish(stageFeatureProgress, started, err != nil) }()
	kv, ok := f.storage.(port.KVStore)
	if !ok {
		return errors.New("facts progress requires transactional KV")
	}
	in, e := reporter.ProjectionInput(ctx, fb.block)
	if e != nil {
		return e
	}
	if p != nil {
		for _, u := range p.Units() {
			if e = unitprogress.Record(ctx, kv, u.Name, u.Definition, in, applied); e != nil {
				return e
			}
		}
	}
	if head {
		point := model.BlockPoint{Height: fb.height(), Hash: fb.block.Hash}
		if b, e := kv.Get(ctx, []byte(unitprogress.HeadKey)); e == nil {
			var old model.BlockPoint
			if e = unitprogress.Decode(b, &old); e != nil {
				return e
			}
			if old.Height > point.Height {
				return nil
			}
		} else if !errors.Is(e, port.ErrNotFound) {
			return e
		}
		b, e := json.Marshal(point)
		if e != nil {
			return e
		}
		return kv.Put(ctx, []byte(unitprogress.HeadKey), b)
	}
	return nil
}

// ConfigureProjection records the declared source scope before workers start.
// It makes no completion claim and joins the existing writer lifecycle.
func (f *Fetcher) ConfigureProjection(ctx context.Context, d model.ProjectionDescriptor) error {
	if d.ValidateStateChanges() != nil || d.ValidateTelemetry() != nil || d.ValidateCallTraces() != nil || d.ValidateBlobCapability() != nil || d.ValidateSourceLookup() != nil || d.ValidateSourceSearch() != nil {
		return port.ErrInvalidData
	}
	if d.StateProofs != nil && (d.StateProofs.Validate() != nil || d.StateProofs.FromHeight <= d.Anchor.Number) {
		return port.ErrInvalidData
	}
	if f.txr == nil || d.Version != 1 || d.Anchor.Validate() != nil || d.Plan.Validate() != nil || len(d.Workers) > 8 {
		return port.ErrInvalidData
	}
	if _, ok := f.src.(source.ProjectionReporter); !ok {
		return port.ErrInvalidData
	}
	b, e := json.Marshal(d)
	if e != nil {
		return e
	}
	return f.write().do(ctx, "sourceDescriptor", func(ctx context.Context) error {
		txCtx, tx, e := f.txr.BeginBlock(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		durable, ok := f.storage.(port.BlockDurability)
		if !ok {
			return port.ErrInvalidData
		}
		if e = durable.RequireBlockDurability(txCtx); e != nil {
			return e
		}
		kv, ok := f.storage.(port.KVStore)
		if !ok {
			return port.ErrInvalidData
		}
		if e = kv.Put(txCtx, []byte(unitprogress.DescriptorKey), b); e != nil {
			return e
		}
		return tx.Commit()
	})
}
