package port

import "context"

// OutboxEntry is one event of a chain's change stream (refactoring plan
// R3-1): its sequence number, its event type and its encoded value.
type OutboxEntry struct {
	Seq  uint64
	Type string
	Data []byte
}

// Outbox records the events of indexed blocks in the block transaction that
// stores the block (transactional outbox), so an event exists exactly when
// its block's writes do. A relay reads the entries in sequence order and
// delivers them.
//
// Sequence numbers start at 1 and increase by one per entry in commit
// order. A failed or rolled-back transaction does not use numbers. Entries
// are not part of a block's undo: rolling a block back keeps its entries
// (they may have been delivered) and records the reorganization as new
// entries instead.
type Outbox interface {
	// AppendOutbox numbers entries after the last committed one and adds
	// them to the block transaction bound to ctx (the given Seq is
	// ignored). Outside a block transaction it fails.
	AppendOutbox(ctx context.Context, entries []OutboxEntry) error
	// ReadOutbox returns up to limit entries with Seq > after, in order.
	// Pruned entries are not returned.
	ReadOutbox(ctx context.Context, after uint64, limit int) ([]OutboxEntry, error)
	// LastOutboxSeq returns the sequence of the last committed entry, 0 if
	// there is none.
	LastOutboxSeq(ctx context.Context) (uint64, error)
	// OutboxCursor returns the last sequence a named consumer (a consumer
	// group of the change stream) recorded as delivered; ok is false if it
	// recorded none.
	OutboxCursor(ctx context.Context, name string) (seq uint64, ok bool, err error)
	// SetOutboxCursor records seq as delivered by the named consumer.
	SetOutboxCursor(ctx context.Context, name string, seq uint64) error
	// PruneOutbox deletes the entries with Seq < before. Numbering
	// continues after the last committed entry.
	PruneOutbox(ctx context.Context, before uint64) error
}

// UndoHook runs inside the transaction that rolls back one block (ctx is
// bound to it), so what it writes, such as the reorganization's outbox
// entries, commits with the rollback. It runs before the block's changes
// are undone, so it reads the records the block wrote (and must write no
// key that undo restores). first is true for the newest block, the first
// one rolled back.
type UndoHook func(ctx context.Context, r *Reorg, b *OrphanedBlock, first bool) error
