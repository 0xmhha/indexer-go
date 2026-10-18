package window

import (
	"context"
	"sort"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// Candidate returns required references only when their height has one raw
// block hash. It is an availability hint, never a canonicality assertion or a
// lease. Callers must verify payloads and check fresh canonicality at commit.
// Partial forks also make a height ambiguous. Read one bounded segment index
// at a time; do not retain a map of every block in the window.
func (s *Store) Candidate(ctx context.Context, number uint64, kinds []model.DatasetKind) ([]Reference, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.guard(); err != nil {
		return nil, err
	}
	if len(kinds) == 0 || len(kinds) > len(datasetKinds) {
		return nil, ErrConflict
	}
	wanted := make(map[model.DatasetKind]int, len(kinds))
	for i, k := range kinds {
		if _, duplicate := wanted[k]; duplicate || !k.Valid() {
			return nil, ErrConflict
		}
		wanted[k] = i
	}
	out := make([]Reference, len(kinds))
	var block model.FeedBlock
	found := false
	count := 0
	visit := func(ref Reference) error {
		if ref.Block.Number != number {
			return nil
		}
		if found && ref.Block != block {
			return ErrAmbiguous
		}
		block, found = ref.Block, true
		i, ok := wanted[ref.Kind]
		if !ok {
			return nil
		}
		if out[i].Kind != "" {
			if out[i] != ref {
				return ErrConflict
			}
			return nil
		}
		out[i] = ref
		count++
		return nil
	}
	if s.active != nil {
		for _, ref := range s.active.refs {
			if err := visit(ref); err != nil {
				return nil, err
			}
		}
	}
	for i := len(s.segments) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		seg := s.segments[i]
		if number < seg.MinHeight || number > seg.MaxHeight {
			continue
		}
		refs, err := s.indexRefs(seg)
		if err != nil {
			return nil, s.poison(err)
		}
		start := sort.Search(len(refs), func(i int) bool { return refs[i].Block.Number >= number })
		for _, ref := range refs[start:] {
			if ref.Block.Number != number {
				break
			}
			if err := visit(ref); err != nil {
				return nil, err
			}
		}
	}
	if count != len(kinds) {
		return nil, ErrNotFound
	}
	return out, nil
}
