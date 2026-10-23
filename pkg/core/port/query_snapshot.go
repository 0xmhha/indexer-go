package port

import "context"

// QuerySnapshotter binds a complete read operation to a consistent public
// generation. Mutations and an entire WebSocket lifetime must not use it.
type QuerySnapshotter interface {
	ReadSnapshot(context.Context, func(context.Context) error) error
}

func WithQuerySnapshot(ctx context.Context, store any, read func(context.Context) error) error {
	if s, ok := store.(QuerySnapshotter); ok {
		return s.ReadSnapshot(ctx, read)
	}
	return read(ctx)
}

// ExecutionSnapshotPolicy lets API response caches avoid reusing a result from
// an older public generation. Activation is fixed before API process startup.
type ExecutionSnapshotPolicy interface{ ExecutionSnapshotsEnabled() bool }
