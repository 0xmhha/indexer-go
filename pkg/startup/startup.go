// Package startup runs the preparation steps that linked-in packages
// register for a single-chain store. The engine runs them once the store is
// open and the node is reachable, before ingest starts; a chain package
// registers its step in init, so the engine needs no chain-specific code.
package startup

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Env is what a step receives.
type Env struct {
	Config *config.Config
	// Storage is the opened store; a step takes the optional ports it needs
	// by type assertion.
	Storage port.Reader
	Logger  *zap.Logger
}

// Step prepares the store for one start.
type Step func(ctx context.Context, env Env) error

var (
	mu    sync.RWMutex
	steps = map[string]Step{}
)

// Register adds a step under name. Registering a name twice panics: it is a
// wiring bug.
func Register(name string, s Step) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := steps[name]; dup {
		panic(fmt.Sprintf("startup: step %q registered twice", name))
	}
	steps[name] = s
}

// Run runs every registered step in name order. A failed step does not stop
// the others; onError receives it with the step's name.
func Run(ctx context.Context, env Env, onError func(name string, err error)) {
	mu.RLock()
	names := make([]string, 0, len(steps))
	for n := range steps {
		names = append(names, n)
	}
	mu.RUnlock()
	slices.Sort(names)
	for _, n := range names {
		mu.RLock()
		s := steps[n]
		mu.RUnlock()
		if err := s(ctx, env); err != nil {
			onError(n, err)
		}
	}
}
