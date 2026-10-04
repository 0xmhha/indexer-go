// Package chains defines chain profiles: the place where everything specific
// to one blockchain lives (node detection, decoding RPC responses into the
// neutral model including chain-specific transaction types, and the features
// a chain enables by default). The indexer core only sees pkg/core/model.
package chains

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// NodeInfo is what detection knows about a node.
type NodeInfo struct {
	ClientVersion string // web3_clientVersion
	ChainID       uint64 // eth_chainId
}

// Profile describes one chain family.
type Profile interface {
	// ID is the profile name used in configuration ("evm", "stablenet").
	ID() string
	// Detect reports whether the node belongs to this profile.
	Detect(info NodeInfo) bool
	// DecodeBlock converts an eth_getBlockByNumber/ByHash result with full
	// transactions into the model. It recovers senders and verifies every
	// transaction hash it can compute.
	DecodeBlock(raw json.RawMessage) (*model.Block, error)
	// DecodeReceipts converts an eth_getBlockReceipts result into the model.
	DecodeReceipts(raw json.RawMessage) ([]*model.Receipt, error)
	// Features lists features this chain enables by default.
	Features() []string
}

type registration struct {
	profile  Profile
	priority int
}

var (
	mu       sync.RWMutex
	registry = map[string]registration{}
)

// Register adds a profile. Higher priority profiles are tried first during
// detection, so specific chains (StableNet) must outrank the generic EVM
// profile. Registering the same id twice panics: it is a wiring bug.
func Register(p Profile, priority int) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[p.ID()]; dup {
		panic(fmt.Sprintf("chains: profile %q registered twice", p.ID()))
	}
	registry[p.ID()] = registration{profile: p, priority: priority}
}

// Lookup returns the profile registered under id.
func Lookup(id string) (Profile, bool) {
	mu.RLock()
	defer mu.RUnlock()
	r, ok := registry[id]
	return r.profile, ok
}

// Detect returns the highest-priority profile that accepts the node.
func Detect(info NodeInfo) (Profile, error) {
	for _, p := range ordered() {
		if p.Detect(info) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("chains: no profile accepts node %q (chain id %d)", info.ClientVersion, info.ChainID)
}

// IDs lists registered profile ids in detection order.
func IDs() []string {
	ps := ordered()
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.ID()
	}
	return out
}

func ordered() []Profile {
	mu.RLock()
	defer mu.RUnlock()
	regs := make([]registration, 0, len(registry))
	for _, r := range registry {
		regs = append(regs, r)
	}
	sort.Slice(regs, func(i, j int) bool {
		if regs[i].priority != regs[j].priority {
			return regs[i].priority > regs[j].priority
		}
		return regs[i].profile.ID() < regs[j].profile.ID()
	})
	out := make([]Profile, len(regs))
	for i, r := range regs {
		out[i] = r.profile
	}
	return out
}
