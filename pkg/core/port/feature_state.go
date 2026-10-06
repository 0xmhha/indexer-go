package port

import (
	"context"
)

// FeatureState records which blocks an indexing feature has processed
// (feature registry design, section 8).
type FeatureState struct {
	// Active means the feature has processed every indexed block and keeps
	// processing new ones.
	Active bool `json:"active"`
	// Through is the last block the feature's data is complete for when it
	// is not active (disabled, or backfill in progress).
	Through uint64 `json:"through"`
	// Gap, on an active feature, is a range of earlier blocks it has not
	// processed yet: an order-independent feature enabled on an indexed
	// database processes new blocks at once and fills the gap in the
	// background (online backfill).
	Gap *BlockRange `json:"gap,omitempty"`
}

// BlockRange is an inclusive range of block heights.
type BlockRange struct {
	From uint64 `json:"from"`
	To   uint64 `json:"to"`
}

// FeatureStateStore keeps feature states.
type FeatureStateStore interface {
	FeatureStates(ctx context.Context) (map[string]FeatureState, error)
	SetFeatureState(ctx context.Context, name string, st FeatureState) error
}
