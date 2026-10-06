package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cockroachdb/pebble"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var _ port.FeatureStateStore = (*PebbleStorage)(nil)

const prefixFeatureState = "/meta/features/"

// FeatureStateKey returns the key of a feature's state.
func FeatureStateKey(name string) []byte { return []byte(prefixFeatureState + name) }

// FeatureStates implements FeatureStateStore.
func (s *PebbleStorage) FeatureStates(ctx context.Context) (map[string]port.FeatureState, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}
	prefix := []byte(prefixFeatureState)
	it, err := s.kv(ctx).NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: prefixUpperBound(prefix)})
	if err != nil {
		return nil, fmt.Errorf("open feature state iterator: %w", err)
	}
	defer it.Close()
	out := map[string]port.FeatureState{}
	for it.First(); it.Valid(); it.Next() {
		var st port.FeatureState
		if err := json.Unmarshal(it.Value(), &st); err != nil {
			return nil, fmt.Errorf("decode feature state %s: %w", it.Key(), err)
		}
		out[strings.TrimPrefix(string(it.Key()), prefixFeatureState)] = st
	}
	return out, it.Error()
}

// SetFeatureState implements FeatureStateStore.
func (s *PebbleStorage) SetFeatureState(ctx context.Context, name string, st port.FeatureState) error {
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
