// Package progress records facts-based feature completion in the existing
// projection transaction. It never creates a writer, raw ACK or GC permission.
package progress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

const DescriptorKey = "/meta/indexing/source"
const HeadKey = "/meta/indexing/head"
const UnitPrefix = "/meta/indexing/units/"
const StatePrefix = "/meta/features/"
const MaxRanges = 128
const MaxValue = 128 << 10

func UnitKey(name string) []byte { return []byte(UnitPrefix + name) }
func ValidName(s string) bool {
	return len(s) > 0 && len(s) <= 256 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func Decode(b []byte, v any) error {
	if len(b) > MaxValue {
		return port.ErrInvalidData
	}
	if json.Unmarshal(b, v) != nil {
		return port.ErrInvalidData
	}
	return nil
}
func Validate(c model.UnitCheckpoint) error {
	if c.Version != 1 || c.Epoch == 0 || c.Chain.Validate() != nil || c.PlanDigest == (common.Hash{}) || c.Start == 0 || len(c.Ranges) == 0 || len(c.Ranges) > MaxRanges {
		return port.ErrInvalidData
	}
	for i, r := range c.Ranges {
		if r.From < c.Start || r.From > r.To || r.EndHash == (common.Hash{}) || (i > 0 && (c.Ranges[i-1].To == math.MaxUint64 || r.From <= c.Ranges[i-1].To+1)) {
			return port.ErrInvalidData
		}
	}
	return nil
}
func Record(ctx context.Context, kv port.KVStore, name, definition string, in model.ProjectionInput, applied bool) error {
	if !ValidName(name) || len(definition) > 4096 || (in.Identity.Block.Validate() != nil || in.Identity.Generation == 0 || in.Identity.PlanVersion == 0 || in.Identity.PlanDigest == (common.Hash{})) || in.Epoch == 0 || in.Anchor.Chain != in.Identity.Block.Chain || in.Anchor.Number == math.MaxUint64 || in.Identity.Block.Number <= in.Anchor.Number {
		return port.ErrInvalidData
	}
	c := model.UnitCheckpoint{Version: 1, Definition: definition, Epoch: in.Epoch, Chain: in.Identity.Block.Chain, PlanDigest: in.Identity.PlanDigest, Start: in.Anchor.Number + 1}
	old, e := kv.Get(ctx, UnitKey(name))
	if e == nil {
		if e = Decode(old, &c); e != nil {
			return e
		}
		if e = Validate(c); e != nil {
			return e
		}
		if c.Epoch > in.Epoch || c.Chain != in.Identity.Block.Chain || c.PlanDigest != in.Identity.PlanDigest || c.Start != in.Anchor.Number+1 {
			return port.ErrInvalidData
		}
		if c.Definition != definition {
			if !applied {
				return nil
			}
			c.Ranges = nil
			c.Definition = definition
		}
	} else if !errors.Is(e, port.ErrNotFound) {
		return e
	} else if !applied {
		return nil
	}
	n := in.Identity.Block.Number
	if applied {
		c.Ranges = append(c.Ranges, model.CompletedRange{From: n, To: n, EndHash: in.Identity.Block.Hash})
		sort.Slice(c.Ranges, func(i, j int) bool { return c.Ranges[i].From < c.Ranges[j].From })
		ranges := make([]model.CompletedRange, 0, len(c.Ranges))
		for _, r := range c.Ranges {
			if len(ranges) == 0 {
				ranges = append(ranges, r)
				continue
			}
			last := &ranges[len(ranges)-1]
			if r.From <= last.To || (last.To < math.MaxUint64 && r.From == last.To+1) {
				if r.To == last.To && r.EndHash != last.EndHash {
					return port.ErrInvalidData
				}
				if r.To > last.To {
					last.To = r.To
					last.EndHash = r.EndHash
				}
			} else {
				ranges = append(ranges, r)
			}
		}
		c.Ranges = ranges
	} else {
		// A duplicate must not pretend a newly enabled/backfilled feature ran.
		found := false
		for _, r := range c.Ranges {
			if n >= r.From && n <= r.To {
				found = true
			}
		}
		if !found {
			return nil
		}
	}
	c.Epoch = in.Epoch
	if e = Validate(c); e != nil {
		return fmt.Errorf("feature completion: %w", e)
	}
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	return kv.Put(ctx, UnitKey(name), b)
}

// ReadPage shares decoding between storage adapters. scan returns at most
// limit+1 ordered feature-state records; get and scan share one DB snapshot.
func ReadPage(get func([]byte) ([]byte, error), scan func(string, int) ([][2][]byte, error), after string, limit int) (port.IndexingPage, error) {
	out := port.IndexingPage{Units: []port.IndexingUnit{}}
	if limit < 1 || limit > 100 || (after != "" && !ValidName(after)) {
		return out, port.ErrInvalidData
	}
	if b, e := get([]byte(DescriptorKey)); e == nil {
		var d model.ProjectionDescriptor
		if e = Decode(b, &d); e != nil {
			return out, e
		}
		if d.Version != 1 || d.Anchor.Validate() != nil || d.Plan.Validate() != nil || len(d.Workers) > 8 {
			return out, port.ErrInvalidData
		}
		out.Descriptor = &d
		if d.StateProofs != nil && (d.StateProofs.Validate() != nil || d.StateProofs.FromHeight <= d.Anchor.Number) {
			return out, port.ErrInvalidData
		}
		if d.ValidateStateChanges() != nil || d.ValidateTelemetry() != nil || d.ValidateCallTraces() != nil || d.ValidateBlobCapability() != nil || d.ValidateSourceLookup() != nil || d.ValidateSourceSearch() != nil {
			return out, port.ErrInvalidData
		}
	} else if !errors.Is(e, port.ErrNotFound) {
		return out, e
	}
	if b, e := get([]byte(HeadKey)); e == nil {
		var h model.BlockPoint
		if e = Decode(b, &h); e != nil {
			return out, e
		}
		if h.Hash == (common.Hash{}) {
			return out, port.ErrInvalidData
		}
		out.Head = &h
	} else if !errors.Is(e, port.ErrNotFound) {
		return out, e
	}
	rows, e := scan(after, limit+1)
	if e != nil {
		return out, e
	}
	for i, pair := range rows {
		name := strings.TrimPrefix(string(pair[0]), StatePrefix)
		if !strings.HasPrefix(string(pair[0]), StatePrefix) || !ValidName(name) {
			return out, port.ErrInvalidData
		}
		if i == limit {
			out.Next = out.Units[len(out.Units)-1].Name
			break
		}
		u := port.IndexingUnit{Name: name}
		if e = Decode(pair[1], &u.State); e != nil {
			return out, e
		}
		if b, e := get(UnitKey(name)); e == nil {
			var c model.UnitCheckpoint
			if e = Decode(b, &c); e != nil {
				return out, e
			}
			if e = Validate(c); e != nil {
				return out, e
			}
			u.Checkpoint = &c
		} else if !errors.Is(e, port.ErrNotFound) {
			return out, e
		}
		out.Units = append(out.Units, u)
	}
	return out, nil
}
