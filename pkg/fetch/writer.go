package fetch

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
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
	return f.write().do(ctx, "indexBlock", func(ctx context.Context) error {
		return f.writeBlock(ctx, fb)
	})
}

func (f *Fetcher) writeBlock(ctx context.Context, fb *fetchedBlock) error {
	height := fb.height()

	storedHash, err := f.storedBlockHash(ctx, height)
	switch {
	case err == nil && storedHash == fb.block.Hash:
		f.logger.Debug("Block already indexed, skipping", zap.Uint64("height", height))
		return f.writeCursorOnly(ctx, height)
	case err == nil:
		return fmt.Errorf("%w (height %d stored %s fetched %s): %w", ErrBlockConflict, height, storedHash.Hex(), fb.block.Hash.Hex(), &ReorgError{Height: height})
	case !errors.Is(err, port.ErrNotFound):
		return fmt.Errorf("check stored block %d: %w", height, err)
	}
	if err := f.checkParent(ctx, fb); err != nil {
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
	if err := tx.Commit(); err != nil {
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
