package era

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/source"
)

// emptyBody is the RLP of a block body without transactions or uncles. A
// header decoded with it gives the block hash without decoding transactions.
var emptyBody = []byte{0xc2, 0xc0, 0xc0}

// Source serves the blocks of a directory of era1 files. The files must
// cover one contiguous range of heights.
type Source struct {
	profile     chains.BinaryProfile
	files       []*File // sorted by start height
	first, last uint64
}

var _ source.Ranged = (*Source)(nil)

// OpenDir opens every *.era1 file in dir. The profile decodes the blocks and
// must support binary decoding.
func OpenDir(dir string, profile chains.Profile) (*Source, error) {
	bp, ok := profile.(chains.BinaryProfile)
	if !ok {
		return nil, fmt.Errorf("era: chain profile %q cannot decode binary blocks", profile.ID())
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.era1"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("era: no .era1 files in %s", dir)
	}
	s := &Source{profile: bp}
	for _, p := range paths {
		f, err := Open(p)
		if err != nil {
			s.Close()
			return nil, err
		}
		s.files = append(s.files, f)
	}
	sort.Slice(s.files, func(i, j int) bool { return s.files[i].Start() < s.files[j].Start() })
	s.first = s.files[0].Start()
	next := s.first
	for _, f := range s.files {
		if f.Start() != next {
			s.Close()
			return nil, fmt.Errorf("era: files in %s are not contiguous: expected block %d, file starts at %d", dir, next, f.Start())
		}
		next = f.Start() + f.Count()
	}
	s.last = next - 1
	return s, nil
}

// Close closes the files.
func (s *Source) Close() error {
	var errs []error
	for _, f := range s.files {
		errs = append(errs, f.Close())
	}
	return errors.Join(errs...)
}

// Profile implements source.Source.
func (s *Source) Profile() chains.Profile { return s.profile }

// Range implements source.Ranged.
func (s *Source) Range() (first, last uint64) { return s.first, s.last }

// Head implements source.Source.
func (s *Source) Head(context.Context) (uint64, error) { return s.last, nil }

func (s *Source) raw(n uint64) (*RawBlock, error) {
	if n < s.first || n > s.last {
		return nil, fmt.Errorf("%w: block %d outside era1 range [%d, %d]", source.ErrNotFound, n, s.first, s.last)
	}
	i := sort.Search(len(s.files), func(i int) bool { return s.files[i].Start()+s.files[i].Count() > n })
	return s.files[i].Block(n)
}

// BlockWithReceipts implements source.Source.
func (s *Source) BlockWithReceipts(_ context.Context, n uint64) (*model.Block, []*model.Receipt, error) {
	rb, err := s.raw(n)
	if err != nil {
		return nil, nil, err
	}
	b, err := s.profile.DecodeBlockRLP(rb.Header, rb.Body)
	if err != nil {
		return nil, nil, err
	}
	if b.Number != n {
		return nil, nil, fmt.Errorf("era: index points block %d at block %d", n, b.Number)
	}
	rs, err := s.profile.DeriveReceipts(b, rb.Receipts)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", source.ErrInconsistentReceipts, err)
	}
	return b, rs, nil
}

// HashAt implements source.Source.
func (s *Source) HashAt(_ context.Context, n uint64) (common.Hash, error) {
	rb, err := s.raw(n)
	if err != nil {
		return common.Hash{}, err
	}
	b, err := s.profile.DecodeBlockRLP(rb.Header, emptyBody)
	if err != nil {
		return common.Hash{}, err
	}
	return b.Hash, nil
}
