package stream

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Defaults of OutboxBusConfig.
const (
	DefaultBatch = 512
	DefaultPoll  = time.Second
)

// OutboxBusConfig configures an OutboxBus.
type OutboxBusConfig struct {
	// Batch is how many entries a consumer reads at a time.
	Batch int
	// Poll is how long a consumer waits for new entries when nobody calls
	// Notify; Notify wakes it at once.
	Poll time.Duration
	// Retain is how many entries stay in the outbox behind the slowest
	// group consuming from this bus (for consumers that ask for missed
	// events); 0 keeps every entry.
	Retain uint64
}

// OutboxBus is the Bus that reads the outbox directly: each group's
// position is its outbox cursor (port.Outbox.OutboxCursor), so a group
// resumes where it stopped in any process that opens the outbox.
//
// It also prunes the outbox: entries more than Retain behind the slowest
// group consuming from this bus are deleted. A group that is not consuming
// does not hold entries back; when it falls further behind than the
// retained entries, the entries it missed are gone and it continues after
// the gap (logged).
type OutboxBus struct {
	outbox port.Outbox
	cfg    OutboxBusConfig
	logger *zap.Logger

	mu        sync.Mutex
	consumers map[string]*consumerState // groups consuming now
	prunedTo  uint64                    // entries below were pruned by this bus
}

type consumerState struct {
	wake chan struct{}
	pos  uint64
}

var _ Bus = (*OutboxBus)(nil)

// NewOutboxBus returns a bus over outbox.
func NewOutboxBus(outbox port.Outbox, cfg OutboxBusConfig, logger *zap.Logger) *OutboxBus {
	if cfg.Batch <= 0 {
		cfg.Batch = DefaultBatch
	}
	if cfg.Poll <= 0 {
		cfg.Poll = DefaultPoll
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &OutboxBus{outbox: outbox, cfg: cfg, logger: logger, consumers: make(map[string]*consumerState)}
}

// Notify tells the consumers that entries were committed. It never blocks.
func (b *OutboxBus) Notify() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, c := range b.consumers {
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
}

// Join implements Bus.
func (b *OutboxBus) Join(ctx context.Context, group string, start Start) (uint64, error) {
	if group == "" {
		return 0, errors.New("stream: empty consumer group")
	}
	pos, ok, err := b.outbox.OutboxCursor(ctx, group)
	if err != nil {
		return 0, fmt.Errorf("stream: read position of %q: %w", group, err)
	}
	if ok {
		return pos, nil
	}
	switch start {
	case StartLatest:
		pos, err = b.outbox.LastOutboxSeq(ctx)
	case StartEarliest:
		var first []port.OutboxEntry
		first, err = b.outbox.ReadOutbox(ctx, 0, 1)
		if err == nil && len(first) > 0 {
			pos = first[0].Seq - 1
		} else if err == nil {
			pos, err = b.outbox.LastOutboxSeq(ctx)
		}
	default:
		return 0, fmt.Errorf("stream: unknown start %d", start)
	}
	if err != nil {
		return 0, fmt.Errorf("stream: start position of %q: %w", group, err)
	}
	if err := b.outbox.SetOutboxCursor(ctx, group, pos); err != nil {
		return 0, fmt.Errorf("stream: record position of %q: %w", group, err)
	}
	return pos, nil
}

// Consume implements Bus.
func (b *OutboxBus) Consume(ctx context.Context, group string, handle Handler) error {
	pos, err := b.Join(ctx, group, StartLatest)
	if err != nil {
		return err
	}
	c := &consumerState{wake: make(chan struct{}, 1), pos: pos}
	b.mu.Lock()
	if _, busy := b.consumers[group]; busy {
		b.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrGroupBusy, group)
	}
	b.consumers[group] = c
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.consumers, group)
		b.mu.Unlock()
	}()

	poll := time.NewTicker(b.cfg.Poll)
	defer poll.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch, err := b.outbox.ReadOutbox(ctx, pos, b.cfg.Batch)
		if err != nil {
			return fmt.Errorf("stream: read outbox after %d: %w", pos, err)
		}
		if len(batch) == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-c.wake:
			case <-poll.C:
			}
			continue
		}
		if batch[0].Seq != pos+1 {
			// Pruned or deleted (by a reindex) before this group read them.
			b.logger.Warn("Consumer group skips missing outbox entries",
				zap.String("group", group), zap.Uint64("after", pos), zap.Uint64("next", batch[0].Seq))
		}
		if err := handle(ctx, batch); err != nil {
			return err
		}
		pos = batch[len(batch)-1].Seq
		if err := b.outbox.SetOutboxCursor(ctx, group, pos); err != nil {
			return fmt.Errorf("stream: record position %d of %q: %w", pos, group, err)
		}
		b.moved(ctx, c, pos)
	}
}

// pruneStep is how many entries pass between prunes.
const pruneStep = 1024

// moved records a consumer's new position and prunes the entries more than
// Retain behind the slowest consumer.
func (b *OutboxBus) moved(ctx context.Context, c *consumerState, pos uint64) {
	b.mu.Lock()
	c.pos = pos
	slowest := pos
	for _, other := range b.consumers {
		slowest = min(slowest, other.pos)
	}
	prunedTo := b.prunedTo
	b.mu.Unlock()

	if b.cfg.Retain == 0 || slowest <= b.cfg.Retain {
		return
	}
	before := slowest - b.cfg.Retain + 1
	if before < prunedTo+pruneStep {
		return
	}
	if err := b.outbox.PruneOutbox(ctx, before); err != nil {
		b.logger.Warn("Outbox prune failed", zap.Uint64("before", before), zap.Error(err))
		return
	}
	b.mu.Lock()
	b.prunedTo = max(b.prunedTo, before)
	b.mu.Unlock()
}
