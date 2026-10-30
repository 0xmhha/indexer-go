// Package ingress registers opt-in execution-input owners. It borrows the
// application's prepared Store and canonical reader; it creates neither.
package ingress

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"
)

type NativeConfig struct {
	Executable string `yaml:"executable"`
	DataDir    string `yaml:"data_dir"`
}
type CollectorConfig struct {
	Executable string `yaml:"executable"`
	SpoolDir   string `yaml:"spool_dir"`
	MaxBytes   uint64 `yaml:"max_bytes"`
	MaxRecords int    `yaml:"max_records"`
}
type Config struct {
	Module           string                 `yaml:"module"`
	Window           string                 `yaml:"window"`
	InitializeVolume bool                   `yaml:"initialize_volume"`
	CreateWindow     bool                   `yaml:"create_window"`
	Native           NativeConfig           `yaml:"native"`
	Collector        CollectorConfig        `yaml:"collector"`
	Peer             chainfeed.PeerSettings `yaml:"peer"`
	MaxRawBlocks     int                    `yaml:"max_raw_blocks"`
	MaxActiveBlocks  int                    `yaml:"max_active_blocks"`
	MaxActiveBytes   int64                  `yaml:"max_active_bytes"`
	Timeout          time.Duration          `yaml:"timeout"`
}
type StatusReader interface {
	ExecutionStatus(context.Context) (port.ExecutionControl, error)
}
type Dependencies struct {
	Store       StatusReader
	Canonical   source.CanonicalReader
	Profile     chains.BinaryProfile
	Control     port.ExecutionControl
	Layout      window.Layout
	Socket      string
	StateProofs *stateproof.Selection
	FenceReorg  func(context.Context, execwire.Ref) error
}
type Program interface {
	// Check validates prepared state before API exposure without starting peers.
	Check(context.Context) error
	Run(context.Context) error
	// Stop is idempotent, cancels/joins owned work, and also releases prepared
	// resources if Run never started. Run after Stop must open no resources.
	Stop()
}

// RecoveryProgram joins its DB children and changes native/raw lineage while
// the existing receiver-owned SQL rewind marker still fences visibility.
type RecoveryProgram interface {
	Program
	Recover(context.Context, port.ExecutionRewind) error
	Recovered(context.Context, port.ExecutionControl) error
}
type Module struct {
	Profile  string
	Validate func(Config) error
	Recovery bool
	// Preflight is pure; it may inspect declared bounds, never open a resource.
	Preflight func(Config, window.Options) error
	Prepare   func(context.Context, Config, Dependencies) (Program, error)
}

var modules = struct {
	sync.RWMutex
	values map[string]Module
}{values: map[string]Module{}}

func Register(name string, m Module) {
	modules.Lock()
	defer modules.Unlock()
	if name == "" || m.Profile == "" || m.Validate == nil || m.Prepare == nil {
		panic("ingress: invalid module")
	}
	if _, ok := modules.values[name]; ok {
		panic("ingress: duplicate module")
	}
	modules.values[name] = m
}
func (c *Config) UnmarshalYAML(n *yaml.Node) error {
	type plain Config
	data, err := yaml.Marshal(n)
	if err != nil {
		return err
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	var decoded plain
	if err = d.Decode(&decoded); err != nil {
		return err
	}
	*c = Config(decoded)
	return nil
}

func (c Config) Normalize() Config {
	if c.Collector.MaxBytes == 0 {
		c.Collector.MaxBytes = 256 << 20
	}
	if c.Collector.MaxRecords == 0 {
		c.Collector.MaxRecords = 4096
	}
	if c.MaxRawBlocks == 0 {
		c.MaxRawBlocks = 172800
	}
	if c.MaxActiveBlocks == 0 {
		c.MaxActiveBlocks = 8
	}
	if c.MaxActiveBytes == 0 {
		c.MaxActiveBytes = 256 << 20
	}
	if c.Timeout == 0 {
		c.Timeout = time.Minute
	}
	c.Peer.Peers = append([]string(nil), c.Peer.Peers...)
	return c
}
func supported(name string) (Module, bool) {
	modules.RLock()
	defer modules.RUnlock()
	m, ok := modules.values[name]
	return m, ok
}
func validWindowName(name string) bool {
	if len(name) == 0 || len(name) > 64 || name == "." || name == ".." {
		return false
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.ContainsRune("._-", c):
		default:
			return false
		}
	}
	return true
}

// Overlap is lexical only. Prepared modules must also reject filesystem aliases
// after resolving their existing parents, before opening a child or transport.
func Overlap(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == string(filepath.Separator) || b == string(filepath.Separator) {
		return true
	}
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
}

// Preflight resolves only declared paths and limits. The dummy identity is
// solely for Layout's pure shape checks; it is never persisted or sent to peers.
// Prepare recomputes options with the actual prepared chain identity.
func (c Config) Preflight(layout window.Layout, socket string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	opts, err := layout.OptionsFor(c.Window, model.FeedChain{ID: "0", GenesisHash: common.Hash{1}})
	if err != nil {
		return err
	}
	if !filepath.IsAbs(socket) || filepath.Clean(opts.Volume.Root) == string(filepath.Separator) || Overlap(c.Native.DataDir, opts.Path) || Overlap(c.Native.Executable, opts.Path) || Overlap(filepath.Dir(socket), opts.Path) || Overlap(filepath.Dir(socket), c.Native.DataDir) {
		return fmt.Errorf("ingress: native, raw window and private socket directories must be separate")
	}
	m, _ := supported(c.Module)
	if m.Preflight != nil {
		return m.Preflight(c.Normalize(), opts)
	}
	return nil
}
func (c Config) Validate() error {
	c = c.Normalize()
	m, ok := supported(c.Module)
	if !ok {
		return fmt.Errorf("ingress: unsupported module %q", c.Module)
	}
	if !validWindowName(c.Window) || !filepath.IsAbs(c.Native.Executable) || !filepath.IsAbs(c.Native.DataDir) || filepath.Clean(c.Native.DataDir) == string(filepath.Separator) || filepath.Clean(c.Native.Executable) == string(filepath.Separator) || Overlap(c.Native.DataDir, c.Native.Executable) {
		return fmt.Errorf("ingress: explicit window and separate absolute native executable/data paths required")
	}
	if c.MaxRawBlocks < 1 || c.MaxRawBlocks > 1<<20 || c.MaxActiveBlocks < 1 || c.MaxActiveBlocks > 64 || c.MaxActiveBytes < 1 || c.MaxActiveBytes > 2<<30 || c.Timeout < time.Millisecond || c.Timeout > time.Hour {
		return fmt.Errorf("ingress: count, bytes or timeout outside supported bounds")
	}
	return m.Validate(c)
}

// Prepare borrows exactly the supplied prepared Store/reader. It does not
// construct pools, invoke canonical RPC or start Program.Run. The App owns
// recovery and Program.Check before exposing its API.
func Prepare(ctx context.Context, c Config, d Dependencies) (Program, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	m, _ := supported(c.Module)
	if d.Store == nil || d.Canonical == nil || d.Profile == nil || d.Profile.ID() != m.Profile {
		return nil, port.ErrInvalidData
	}
	current, err := d.Store.ExecutionStatus(ctx)
	if err != nil {
		return nil, err
	}
	if current != d.Control {
		return nil, port.ErrExecutionStale
	}
	if !current.BaselineReady {
		return nil, port.ErrExecutionCoverage
	}
	if current.Maintenance {
		return nil, port.ErrExecutionMaintenance
	}
	if (current.Rewinding || current.ReorgPending) && !m.Recovery {
		return nil, port.ErrExecutionRewinding
	}
	if current.Root == (execwire.Hash{}) || current.AnchorRoot == (execwire.Hash{}) || current.Anchor.Generation == 0 || current.Anchor.Height > current.Visible.Height || current.Visible.Generation != current.Generation {
		return nil, port.ErrExecutionConflict
	}
	if err = execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: current.Chain, Ref: current.Visible}}); err != nil {
		return nil, err
	}
	p, err := m.Prepare(ctx, c.Normalize(), d)
	if err == nil && m.Recovery {
		if _, ok := p.(RecoveryProgram); !ok {
			err = port.ErrInvalidData
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		if p != nil {
			p.Stop()
		}
		return nil, err
	}
	if p == nil {
		return nil, port.ErrInvalidData
	}
	return p, nil
}
