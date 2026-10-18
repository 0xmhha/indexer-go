package chainfeed

import "context"

const MaxBatchEnvelopes = 3

// BatchStream optionally exposes an already bounded block's datasets together.
// Each batch owns 1..3 immutable envelopes of the SAME block, with distinct
// kinds and each within Subscribe's ReadLimits. Empty success is invalid.
// Implementations bound bytes before allocation; ordinary Streams remain valid.
type BatchStream interface {
	Stream
	NextBatch(context.Context) ([]Envelope, error)
}
