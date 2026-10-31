package window

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// Layout describes window configuration and an optional Go embedding clock.
// Parsing/validation stays pure: no clock calls, volume initialization, marker
// migration, paths, RPC or feed activation.
type Layout struct {
	// Now is an embedding clock forwarded to Store options. It is deliberately
	// absent from config decoding/encoding. Nil keeps time.Now; callers that
	// supply it must make concurrent calls safe, as with Options.Now.
	Now     func() time.Time        `yaml:"-" json:"-"`
	Volumes map[string]VolumeSpec   `yaml:"volumes"`
	Windows map[string]WindowConfig `yaml:"windows"`
}
type WindowConfig struct {
	Volume            string        `yaml:"volume"`
	Path              string        `yaml:"path"`
	MaxBytes          int64         `yaml:"max_bytes"`
	MinFreeBytes      int64         `yaml:"min_free_bytes"`
	SegmentBytes      int64         `yaml:"segment_bytes"`
	MaxPayloadBytes   int64         `yaml:"max_payload_bytes"`
	MaxSegmentRecords int           `yaml:"max_segment_records"`
	MaxSegments       int           `yaml:"max_segments"`
	ReadBudgetBytes   int64         `yaml:"read_budget_bytes"`
	MaxReaders        int           `yaml:"max_readers"`
	Retention         time.Duration `yaml:"retention"`
}

func (l Layout) Configured() bool { return len(l.Volumes) > 0 || len(l.Windows) > 0 }

func (l *Layout) UnmarshalYAML(n *yaml.Node) error {
	type plain Layout
	data, err := yaml.Marshal(n)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var v plain
	if err := dec.Decode(&v); err != nil {
		return err
	}
	*l = Layout(v)
	return nil
}

// ReadLayout loads the public standalone layout format. The application uses
// the same type under window_storage, so strict field validation is shared.
func ReadLayout(r io.Reader) (Layout, error) {
	var l Layout
	data, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil {
		return l, err
	}
	if len(data) > 1<<20 {
		return l, ErrCapacity
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&l); err != nil {
		return l, err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return l, errors.New("window: exactly one layout document required")
	}
	return l, l.Validate()
}

func (l Layout) Validate() error {
	if !l.Configured() {
		return nil
	}
	if len(l.Volumes) == 0 || len(l.Volumes) > 64 || len(l.Windows) == 0 || len(l.Windows) > maxClaims {
		return errors.New("window: volumes/windows required within limits")
	}
	roots := map[string]string{}
	ids := map[string]bool{}
	for name, v := range l.Volumes {
		if !validVolumeID(name) || !validVolumeID(v.ID) || !filepath.IsAbs(v.Root) || v.MaxBytes <= VolumeOverheadBytes || v.MaxBytes > 1<<60 || v.MinFreeBytes < 0 {
			return fmt.Errorf("window: invalid volume %q", name)
		}
		root := filepath.Clean(v.Root)
		if ids[v.ID] {
			return fmt.Errorf("window: duplicate volume identity %q", v.ID)
		}
		ids[v.ID] = true
		for r := range roots {
			if pathOverlaps(r, root) {
				return errors.New("window: overlapping volume roots")
			}
		}
		roots[root] = name
	}
	allocated := map[string]int64{}
	paths := map[string][]string{}
	dummy := model.FeedChain{ID: "0", GenesisHash: common.Hash{1}}
	for name, w := range l.Windows {
		if !validVolumeID(name) {
			return fmt.Errorf("window: invalid reservation name %q", name)
		}
		v, ok := l.Volumes[w.Volume]
		if !ok {
			return fmt.Errorf("window: %s: unknown volume %q", name, w.Volume)
		}
		if _, err := w.options(name, v, dummy).Normalize(); err != nil {
			return fmt.Errorf("window: %s: %w", name, err)
		}
		if w.MaxBytes > v.MaxBytes-VolumeOverheadBytes-allocated[w.Volume] {
			return fmt.Errorf("window: volume %s quotas exceed budget", w.Volume)
		}
		allocated[w.Volume] += w.MaxBytes
		path := filepath.Clean(w.Path)
		for _, p := range paths[w.Volume] {
			if pathOverlaps(p, path) {
				return errors.New("window: overlapping window paths")
			}
		}
		paths[w.Volume] = append(paths[w.Volume], path)
	}
	return nil
}

func (w WindowConfig) options(name string, v VolumeSpec, chain model.FeedChain) Options {
	return Options{Volume: v, Reservation: name, Path: w.Path, Chain: chain, MaxBytes: w.MaxBytes, MinFreeBytes: w.MinFreeBytes, SegmentBytes: w.SegmentBytes, MaxPayloadBytes: w.MaxPayloadBytes, MaxSegmentRecords: w.MaxSegmentRecords, MaxSegments: w.MaxSegments, ReadBudgetBytes: w.ReadBudgetBytes, MaxReaders: w.MaxReaders, Retention: w.Retention}
}
func (l Layout) OptionsFor(instance string, verifiedChain model.FeedChain) (Options, error) {
	if err := l.Validate(); err != nil {
		return Options{}, err
	}
	w, ok := l.Windows[instance]
	if !ok {
		return Options{}, fmt.Errorf("window: unknown instance %q", instance)
	}
	options := w.options(instance, l.Volumes[w.Volume], verifiedChain)
	options.Now = l.Now
	return options.Normalize()
}
