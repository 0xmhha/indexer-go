// Package feature is the registry of optional indexing features (refactoring
// plan 5.3 and 5.4). A feature has a name, the features it requires and a
// Register function that attaches its block handlers. Features register
// themselves when their package is linked in; at startup the enabled set is
// resolved into a deterministic order and dependencies are checked.
package feature

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
)

// Feature is one optional part of indexing.
type Feature interface {
	// Name is unique, for example "stablenet.wbft".
	Name() string
	// Requires lists features that must be enabled with this one.
	Requires() []string
	// Register attaches the feature's handlers.
	Register(r Registrar) error
}

// Block is one block as handlers see it. Model and Receipts are the
// chain-neutral model: use them for every hash and identity. Geth and
// GethReceipts are a go-ethereum view of the same block for code not yet
// moved to the model; the view recomputes hashes with go-ethereum rules, so
// never record a hash taken from it. Transactions and receipts are aligned
// by index in both views.
type Block struct {
	Model    *model.Block
	Receipts []*model.Receipt

	Geth         *types.Block
	GethReceipts types.Receipts

	txs []TxWithReceipt // Transactions, computed once
}

// BlockHandler processes one block. It runs inside the block's storage
// transaction (ctx carries it): an error aborts the whole block.
type BlockHandler interface {
	HandleBlock(ctx context.Context, b *Block) error
}

// BlockHandlerFunc adapts a function to BlockHandler.
type BlockHandlerFunc func(ctx context.Context, b *Block) error

// HandleBlock implements BlockHandler.
func (f BlockHandlerFunc) HandleBlock(ctx context.Context, b *Block) error { return f(ctx, b) }

// Deps are the services a feature may use.
type Deps struct {
	// Storage is the indexer's storage. A feature takes the ports it uses
	// (pkg/core/port) by type assertion and fails to register when one is
	// missing.
	Storage port.Reader
	Logger  *zap.Logger
	// Profile is the chain profile of the node, or nil if it is unknown.
	Profile chains.Profile
	// Publish sends an event to subscribers. Events published while a block
	// is processed are delivered only after the block commits.
	Publish func(events.Event) bool
	// BalanceAt reads an account's native balance from the node at a block
	// (nil means latest), with the indexer's RPC timeout.
	BalanceAt func(ctx context.Context, addr common.Address, block *big.Int) (*big.Int, error)
	// BlockAt reads a block from the node (nil when unavailable).
	BlockAt func(ctx context.Context, number uint64) (*model.Block, error)
	// Contracts reads contract code and calls contracts on the node at its
	// latest state (nil when unavailable).
	Contracts ContractReader
}

// ContractReader reads contracts on the node. A nil block number means the
// latest state.
type ContractReader interface {
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber interface{}) ([]byte, error)
	CodeAt(ctx context.Context, contract common.Address, blockNumber interface{}) ([]byte, error)
}

// DefaultOn is implemented by features that are enabled on every chain
// unless configured off (the explorer defaults of refactoring plan 5.3).
type DefaultOn interface {
	DefaultOn() bool
}

// Defaults returns the registered features that are on by default.
func Defaults() []string {
	mu.RLock()
	defer mu.RUnlock()
	var out []string
	for n, f := range features {
		if d, ok := f.(DefaultOn); ok && d.DefaultOn() {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// TxWithReceipt is one transaction of a block with its receipt, in both
// views.
type TxWithReceipt struct {
	Index       int
	Tx          *model.Transaction
	Receipt     *model.Receipt
	GethTx      *types.Transaction
	GethReceipt *types.Receipt
}

// Transactions pairs each transaction with its receipt by the chain's
// transaction hash. Transactions without a receipt are left out.
func (b *Block) Transactions() []TxWithReceipt {
	if b.txs == nil {
		b.txs = b.pairTransactions()
	}
	return b.txs
}

func (b *Block) pairTransactions() []TxWithReceipt {
	byHash := make(map[common.Hash]int, len(b.Receipts))
	for i, r := range b.Receipts {
		byHash[r.TxHash] = i
	}
	var gethTxs types.Transactions
	if b.Geth != nil {
		gethTxs = b.Geth.Transactions()
	}
	out := make([]TxWithReceipt, 0, len(b.Model.Transactions))
	for i, tx := range b.Model.Transactions {
		ri, ok := byHash[tx.Hash]
		if !ok || i >= len(gethTxs) || ri >= len(b.GethReceipts) {
			continue
		}
		out = append(out, TxWithReceipt{
			Index: i, Tx: tx, Receipt: b.Receipts[ri],
			GethTx: gethTxs[i], GethReceipt: b.GethReceipts[ri],
		})
	}
	return out
}

// DelegatedFeePayer returns the account paying gas for tx when it is not the
// sender, as the chain profile decoded it (chains.FeeDelegationOf).
func DelegatedFeePayer(tx *model.Transaction) (common.Address, bool) {
	if fd, ok := chains.FeeDelegationOf(tx); ok {
		return fd.Payer, true
	}
	return common.Address{}, false
}

// Registrar is what a feature attaches to.
type Registrar interface {
	Deps() Deps
	// OnBlock adds a handler that runs for every indexed block.
	OnBlock(h BlockHandler)
}

var (
	mu       sync.RWMutex
	features = map[string]Feature{}
)

// Register adds f to the registry. It panics on a duplicate or empty name.
func Register(f Feature) {
	mu.Lock()
	defer mu.Unlock()
	name := f.Name()
	if name == "" {
		panic("feature: empty name")
	}
	if _, ok := features[name]; ok {
		panic(fmt.Sprintf("feature: %q registered twice", name))
	}
	features[name] = f
}

// Names returns the registered feature names in order.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(features))
	for n := range features {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Resolve returns the enabled features in execution order: every feature
// after the features it requires, otherwise by name. It fails if an enabled
// name is not registered, a requirement is not enabled, or requirements form
// a cycle.
func Resolve(enabled []string) ([]Feature, error) {
	mu.RLock()
	defer mu.RUnlock()

	set := map[string]Feature{}
	for _, n := range enabled {
		f, ok := features[n]
		if !ok {
			return nil, fmt.Errorf("feature: %q is enabled but not registered (registered: %s)", n, strings.Join(namesLocked(), ", "))
		}
		set[n] = f
	}

	// Kahn's algorithm; the ready queue is kept sorted so the order is
	// deterministic.
	pending := map[string]int{}
	dependents := map[string][]string{}
	for n, f := range set {
		for _, r := range f.Requires() {
			if _, ok := set[r]; !ok {
				return nil, fmt.Errorf("feature: %q requires %q, which is not enabled", n, r)
			}
			pending[n]++
			dependents[r] = append(dependents[r], n)
		}
	}
	var ready []string
	for n := range set {
		if pending[n] == 0 {
			ready = append(ready, n)
		}
	}
	sort.Strings(ready)

	out := make([]Feature, 0, len(set))
	for len(ready) > 0 {
		n := ready[0]
		ready = ready[1:]
		out = append(out, set[n])
		for _, d := range dependents[n] {
			pending[d]--
			if pending[d] == 0 {
				ready = append(ready, d)
				sort.Strings(ready)
			}
		}
	}
	if len(out) != len(set) {
		var cyclic []string
		for n := range set {
			if pending[n] > 0 {
				cyclic = append(cyclic, n)
			}
		}
		sort.Strings(cyclic)
		return nil, fmt.Errorf("feature: requirement cycle among %s", strings.Join(cyclic, ", "))
	}
	return out, nil
}

func namesLocked() []string {
	out := make([]string, 0, len(features))
	for n := range features {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Pipeline is the set of handlers of the enabled features, in order.
type Pipeline struct {
	deps     Deps
	handlers []namedHandler
}

type namedHandler struct {
	feature string
	h       BlockHandler
}

// Build resolves enabled and lets each feature register its handlers.
func Build(enabled []string, deps Deps) (*Pipeline, error) {
	fs, err := Resolve(enabled)
	if err != nil {
		return nil, err
	}
	p := &Pipeline{deps: deps}
	for _, f := range fs {
		r := &registrar{p: p, feature: f.Name()}
		if err := f.Register(r); err != nil {
			return nil, fmt.Errorf("feature %q: %w", f.Name(), err)
		}
	}
	return p, nil
}

// Features returns the names of the features with handlers, in order.
func (p *Pipeline) Features() []string {
	var out []string
	for _, h := range p.handlers {
		if len(out) == 0 || out[len(out)-1] != h.feature {
			out = append(out, h.feature)
		}
	}
	return out
}

// HandleBlock runs every handler in order and stops at the first error.
func (p *Pipeline) HandleBlock(ctx context.Context, b *Block) error {
	if p == nil {
		return nil
	}
	for _, h := range p.handlers {
		if err := h.h.HandleBlock(ctx, b); err != nil {
			return fmt.Errorf("feature %q: %w", h.feature, err)
		}
	}
	return nil
}

type registrar struct {
	p       *Pipeline
	feature string
}

func (r *registrar) Deps() Deps { return r.p.deps }

func (r *registrar) OnBlock(h BlockHandler) {
	r.p.handlers = append(r.p.handlers, namedHandler{feature: r.feature, h: h})
}

// Enabled returns the features to enable: the registered defaults (for
// example a chain profile's Features) with overrides applied. Defaults that
// are not registered are skipped, because some default features are still
// built into the fetcher until they move to this registry. An override must
// name a registered feature.
func Enabled(defaults []string, overrides map[string]bool) ([]string, error) {
	mu.RLock()
	defer mu.RUnlock()
	on := map[string]bool{}
	for _, n := range defaults {
		if _, ok := features[n]; ok {
			on[n] = true
		}
	}
	for n, enable := range overrides {
		if _, ok := features[n]; !ok {
			return nil, fmt.Errorf("feature: %q is configured but not registered (registered: %s)", n, strings.Join(namesLocked(), ", "))
		}
		on[n] = enable
	}
	out := make([]string, 0, len(on))
	for n, enable := range on {
		if enable {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Blocks returns a reader of earlier blocks for chain rules that need them
// (for example the epoch a StableNet block belongs to): the index first,
// the node for blocks before the index starts.
func (d Deps) Blocks() chains.AccountingEnv { return depsBlocks{d} }

type depsBlocks struct{ d Deps }

func (b depsBlocks) Block(ctx context.Context, number uint64) (*model.Block, error) {
	blk, err := b.d.Storage.GetBlock(ctx, number)
	if err == nil || !errors.Is(err, port.ErrNotFound) || b.d.BlockAt == nil {
		return blk, err
	}
	return b.d.BlockAt(ctx, number)
}
