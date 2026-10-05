package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cockroachdb/pebble"
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

var _ FeatureStateStore = (*PebbleStorage)(nil)

const prefixFeatureState = "/meta/features/"

// FeatureStateKey returns the key of a feature's state.
func FeatureStateKey(name string) []byte { return []byte(prefixFeatureState + name) }

// FeatureStates implements FeatureStateStore.
func (s *PebbleStorage) FeatureStates(ctx context.Context) (map[string]FeatureState, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}
	prefix := []byte(prefixFeatureState)
	it, err := s.kv(ctx).NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: prefixUpperBound(prefix)})
	if err != nil {
		return nil, fmt.Errorf("open feature state iterator: %w", err)
	}
	defer it.Close()
	out := map[string]FeatureState{}
	for it.First(); it.Valid(); it.Next() {
		var st FeatureState
		if err := json.Unmarshal(it.Value(), &st); err != nil {
			return nil, fmt.Errorf("decode feature state %s: %w", it.Key(), err)
		}
		out[strings.TrimPrefix(string(it.Key()), prefixFeatureState)] = st
	}
	return out, it.Error()
}

// SetFeatureState implements FeatureStateStore.
func (s *PebbleStorage) SetFeatureState(ctx context.Context, name string, st FeatureState) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.kv(ctx).Set(FeatureStateKey(name), b, pebble.Sync)
}
