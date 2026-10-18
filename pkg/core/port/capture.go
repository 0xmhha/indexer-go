package port

import (
	"context"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

const (
	CaptureKeyLimit         = 256
	CaptureValueLimit       = 16 << 20
	CaptureTransactionLimit = 128 << 20
)

// CaptureReader reads a chain's isolated source-facts namespace. Returned
// bytes are owned by the caller; absent keys return ErrNotFound.
type CaptureReader interface{ Get([]byte) ([]byte, error) }

const (
	CaptureBatchKeyLimit   = 32
	CaptureBatchValueLimit = 1 << 20
	CaptureBatchBytesLimit = 4 << 20
)

// CaptureBatchReader optionally reads bounded immutable-node batches in the
// same snapshot/transaction as Get. Missing keys are omitted; existing empty
// values remain present. Results are caller-owned, unordered and deduplicated.
// Limits apply before key deduplication and to the returned key+value bytes.
// Any invalid input, oversized value/result or I/O error fails the whole call
// and poisons the enclosing capture callback, like an invalid Get/Put.
type CaptureBatchReader interface {
	GetMany(keys [][]byte) (map[string][]byte, error)
}

// CaptureWriter belongs to one synchronous callback, used by one goroutine.
// It must not escape the callback or call another storage transaction.
type CaptureWriter interface {
	CaptureReader
	Put(key, value []byte) error
}

// CaptureDatabase separates immutable source facts from projection KV/undo
// and its user-facing outbox. Updates serialize across handles/processes for
// the same chain, read their own writes and commit durably and atomically.
// A callback error, cancelled context or failed write must roll back all
// changes, even when the callback ignores a writer error. Success is returned
// only after durable commit. View is a consistent read-only snapshot.
type CaptureDatabase interface {
	CaptureUpdate(context.Context, model.FeedChain, func(CaptureWriter) error) error
	CaptureView(context.Context, model.FeedChain, func(CaptureReader) error) error
}

// CaptureAppender optionally serializes append-only source publication while
// allowing projection validation/receipts in the SAME generation. It must
// exclude CaptureUpdate (reset, deletion, policy changes) through commit.
// The trusted callback may only advance the source state/next canonical pointer
// and insert immutable facts/events/object bindings. Retained fork pointers
// ABOVE Through may be replaced; already published prefix inputs/pointers,
// generation, plan, anchor, finality, consumers and deletion fences must stay
// unchanged. Existing immutable keys may be repeated with identical bytes only.
// All durability,
// cancellation, callback scope and error poisoning rules still apply.
type CaptureAppender interface {
	CaptureAppend(context.Context, model.FeedChain, func(CaptureWriter) error) error
}

// CaptureReceipter optionally inserts immutable projection/* receipts while
// excluding generation changes through commit. It may coexist with source
// appends and projection validation; all reads of published inputs remain
// stable. Only identical receipt retries are allowed, never state/cursor/facts
// changes. The caller must validate the current epoch and exact input before
// writing and invoke this ONLY after its durable projection commit.
type CaptureReceipter interface {
	CaptureReceipt(context.Context, model.FeedChain, func(CaptureWriter) error) error
}

// CaptureProjectionViewer optionally pins a published prefix/generation in a
// bound projection transaction while allowing append-only publication. Its
// trusted callback may read state once and immutable inputs at/below that
// state's Through; it must not inspect mutable workers or a future height.
// Through may advance between callbacks, but the epoch and published inputs
// cannot change until projection commit/rollback. Unbound calls remain full
// read-only snapshots. Generic CaptureView keeps its stronger snapshot fence.
type CaptureProjectionViewer interface {
	CaptureProjectionView(context.Context, model.FeedChain, func(CaptureReader) error) error
}
