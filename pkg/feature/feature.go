// Package feature is the registry of optional indexing features (refactoring
// plan 5.3 and 5.4). A feature has a name, the features it requires and a
// Register function that attaches its block handlers. Features register
// themselves when their package is linked in; at startup the enabled set is
// resolved into a deterministic order and dependencies are checked.
package feature

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/storage"
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
	Storage storage.Storage
	Logger  *zap.Logger
	// Profile is the chain profile of the node, or nil if it is unknown.
	Profile chains.Profile
	// Publish sends an event to subscribers. Events published while a block
	// is processed are delivered only after the block commits.
	Publish func(events.Event) bool
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
