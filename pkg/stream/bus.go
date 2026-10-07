package stream

import (
	"context"
	"errors"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Bus carries a chain's change stream to consumer groups (refactoring plan
// R3-2). Every node that serves the stream consumes it as its own group, so
// every node receives every entry: groups do not share entries or
// progress.
//
// A group receives the entries after its position in sequence order and
// moves its position when its handler accepts a batch. Delivery is at least
// once: a batch whose handler fails, or that was being handled when the
// consumer died, is delivered again to the group's next consumer. Entries
// carry their sequence, so consumers drop the ones they have seen.
//
// OutboxBus reads the outbox directly (the first implementation the plan
// decided on); an external bus (NATS JetStream, Kafka) implements the same
// port, checked by streamtest.Run.
type Bus interface {
	// Join creates group's position if it has none, at start, and returns
	// the group's position: entries after it are delivered next. A group's
	// position is kept until it is moved by Consume; start applies only to
	// a group that has none.
	Join(ctx context.Context, group string, start Start) (uint64, error)
	// Consume delivers the entries after group's position to handle, in
	// order and in batches, until ctx ends or handle fails, and returns
	// ctx.Err() or the error. A group without a position joins at
	// StartLatest. One consumer of a group runs at a time per bus;
	// another returns ErrGroupBusy.
	Consume(ctx context.Context, group string, handle Handler) error
}

// Handler handles a batch of entries. When it returns nil the group's
// position moves past the batch; otherwise the batch is delivered again.
type Handler func(ctx context.Context, batch []port.OutboxEntry) error

// Start is where a new group's position starts.
type Start int

const (
	// StartLatest starts after the last committed entry: the group
	// receives the entries committed after it joined.
	StartLatest Start = iota
	// StartEarliest starts before the oldest retained entry.
	StartEarliest
)

// ErrGroupBusy is returned by Consume when the group is consumed already.
var ErrGroupBusy = errors.New("stream: consumer group is consumed already")
