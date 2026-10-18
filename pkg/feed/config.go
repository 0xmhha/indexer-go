// Package feed composes registered chain modules with the capture lifecycle.
// Registration selects an audited module; configuration never starts a node.
package feed

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmtrace"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"
)

type TelemetryConfig struct {
	// ReceiveIntervalTarget is advisory. It never changes RPC/GC/canonical policy.
	ReceiveIntervalTarget time.Duration `yaml:"receive_interval_target"`
}

type Config struct {
	CallTraces        *evmtrace.Config `yaml:"call_traces,omitempty"`
	Telemetry         *TelemetryConfig `yaml:"telemetry,omitempty"`
	StateProofs       *evmstate.Config `yaml:"state_proofs,omitempty"`
	Projections       []string         `yaml:"projections,omitempty"`
	Module            string           `yaml:"module"`
	Window            string           `yaml:"window"`
	InitializeVolume  bool             `yaml:"initialize_volume"`
	CreateWindow      bool             `yaml:"create_window"`
	Consumer          string           `yaml:"consumer"`
	ChainID           string           `yaml:"chain_id"`
	GenesisHash       string           `yaml:"genesis_hash"`
	AnchorHeight      uint64           `yaml:"anchor_height"`
	AnchorHash        string           `yaml:"anchor_hash"`
	NetworkID         uint64           `yaml:"network_id"`
	ForkHash          string           `yaml:"fork_hash"`
	ForkNext          uint64           `yaml:"fork_next"`
	KeyFile           string           `yaml:"key_file"`
	Peers             []string         `yaml:"peers"`
	ListenAddr        string           `yaml:"listen_addr"`
	FinalityAuthority string           `yaml:"finality_authority"`
	Collect           bool             `yaml:"collect"`
	ControlInterval   time.Duration    `yaml:"control_interval"`
}

func (c *Config) UnmarshalYAML(n *yaml.Node) error {
	type plain Config
	b, err := yaml.Marshal(n)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var v plain
	if err := dec.Decode(&v); err != nil {
		return err
	}
	*c = Config(v)
	return nil
}

type Fresh interface {
	source.CanonicalReader
	source.FinalizedReader
}
type Program struct {
	BlobCapability model.BlobCapability // set from the validated profile by Open
	Chain          model.FeedChain
	Anchor         model.FeedBlock
	Plan           model.CapturePlan
	Profile        chains.BinaryProfile
	Policy         capture.FinalityPolicy
	Run            func(context.Context, port.CaptureDatabase, capture.SessionConfig) error
}
type Module struct {
	// BlobCapability is code-owned audit metadata, never a live provider probe.
	BlobCapability *model.BlobCapability
	// CallTraceProfile enables the explicit provider-reported debug format,
	// not a guarantee that a public endpoint exposes debug methods.
	CallTraceProfile    string
	SupportsStateProofs bool
	Validate            func(Config) error // pure: no RPC, files or goroutines
	Open                func(context.Context, Config, Fresh) (Program, error)
}

var registry = struct {
	sync.RWMutex
	modules map[string]Module
}{modules: map[string]Module{}}

func Register(name string, m Module) {
	registry.Lock()
	defer registry.Unlock()
	if name == "" || m.Validate == nil || m.Open == nil {
		panic("feed: invalid module")
	}
	if _, ok := registry.modules[name]; ok {
		panic("feed: duplicate module")
	}
	if m.BlobCapability != nil {
		copy := *m.BlobCapability
		m.BlobCapability = &copy
	}
	registry.modules[name] = m
}
func (c Config) Validate() error {
	if c.Telemetry != nil && (c.Telemetry.ReceiveIntervalTarget < 0 || c.Telemetry.ReceiveIntervalTarget > time.Minute) {
		return fmt.Errorf("feed: invalid telemetry receive interval target")
	}
	if len(c.Projections) > 8 {
		return fmt.Errorf("feed: at most 8 independent projections")
	}
	seen := map[string]bool{}
	for _, name := range c.Projections {
		if name == "" || len(name) > 128 || seen[name] {
			return fmt.Errorf("feed: invalid or duplicate projection")
		}
		seen[name] = true
	}
	if c.Window == "" || c.Consumer == "" || len(c.Consumer) > 128 || !filepath.IsAbs(c.KeyFile) || c.NetworkID == 0 || len(c.Peers) == 0 || len(c.Peers) > 8 || (c.Collect && c.FinalityAuthority == "") || c.ControlInterval < 0 || c.ControlInterval > time.Hour {
		return fmt.Errorf("feed: incomplete or invalid settings")
	}
	for _, r := range c.Consumer {
		if r < 33 || r > 126 {
			return fmt.Errorf("feed: invalid consumer")
		}
	}
	if c.ControlInterval > 0 && c.ControlInterval < time.Second {
		return fmt.Errorf("feed: control_interval must be at least 1s")
	}
	if _, err := c.Identity(); err != nil {
		return err
	}
	if len(c.ForkHash) != 8 {
		return fmt.Errorf("feed: fork_hash must be 8 hex characters")
	}
	fork, err := hex.DecodeString(c.ForkHash)
	if err != nil || len(fork) != 4 || bytes.Equal(fork, make([]byte, 4)) {
		return fmt.Errorf("feed: fork_hash must contain exactly four nonzero hex bytes")
	}
	registry.RLock()
	m, ok := registry.modules[c.Module]
	registry.RUnlock()
	if !ok {
		return fmt.Errorf("feed: unsupported module %q", c.Module)
	}
	if m.BlobCapability != nil && m.BlobCapability.Validate() != nil {
		return fmt.Errorf("feed: invalid module blob capability")
	}
	if c.StateProofs != nil {
		if !m.SupportsStateProofs {
			return fmt.Errorf("feed: module has no audited MPT state proof supplier")
		}
		anchor, _ := c.Identity()
		if _, e := c.StateProofs.Descriptor(anchor); e != nil {
			return e
		}
	}
	if c.CallTraces != nil {
		if m.CallTraceProfile == "" {
			return fmt.Errorf("feed: module has no audited call trace decoding profile")
		}
		anchor, _ := c.Identity()
		d, e := c.CallTraces.Descriptor(anchor, m.CallTraceProfile)
		if e != nil {
			return e
		}
		if c.StateProofs != nil && d.Name == c.StateProofs.Name {
			return fmt.Errorf("feed: trace and proof streams require distinct names")
		}
	}
	return m.Validate(c)
}
func (c Config) Identity() (model.FeedBlock, error) {
	hash := func(v string) (common.Hash, error) {
		if len(vString(v)) != 64 {
			return common.Hash{}, fmt.Errorf("feed: exact nonzero 32-byte hash required")
		}
		b, err := hex.DecodeString(vString(v))
		if err != nil || len(b) != 32 || bytes.Equal(b, make([]byte, 32)) {
			return common.Hash{}, fmt.Errorf("feed: exact nonzero 32-byte hash required")
		}
		return common.BytesToHash(b), nil
	}
	g, err := hash(c.GenesisHash)
	if err != nil {
		return model.FeedBlock{}, err
	}
	h, err := hash(c.AnchorHash)
	if err != nil {
		return model.FeedBlock{}, err
	}
	b := model.FeedBlock{Chain: model.FeedChain{ID: c.ChainID, GenesisHash: g}, Number: c.AnchorHeight, Hash: h}
	return b, b.Validate()
}
func vString(v string) string {
	if len(v) >= 2 && v[:2] == "0x" {
		return v[2:]
	}
	return v
}
func Open(ctx context.Context, c Config, fresh Fresh) (Program, error) {
	if err := c.Validate(); err != nil {
		return Program{}, err
	}
	if fresh == nil {
		return Program{}, port.ErrInvalidData
	}
	registry.RLock()
	m := registry.modules[c.Module]
	registry.RUnlock()
	p, err := m.Open(ctx, c, fresh)
	if err != nil {
		return Program{}, err
	}
	anchor, _ := c.Identity()
	if p.Run == nil || p.Profile == nil || p.Anchor != anchor || p.Chain != anchor.Chain || p.Plan.Validate() != nil || (c.Collect && p.Policy == nil) {
		return Program{}, port.ErrInvalidData
	}
	if c.CallTraces != nil && p.Profile.ID() != m.CallTraceProfile {
		return Program{}, port.ErrInvalidData
	}
	capability, e := chains.BlobCapabilityOf(p.Profile)
	if e != nil || m.BlobCapability != nil && capability != *m.BlobCapability {
		return Program{}, port.ErrInvalidData
	}
	p.BlobCapability = capability
	return p, nil
}
