package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Methods registered by packages outside the API, such as chain-specific
// code under x/<chain>/, served after the built-in methods.

// MethodDeps is what a registered method receives.
type MethodDeps struct {
	Storage port.QueryStore
	Logger  *zap.Logger
}

// MethodFunc serves one JSON-RPC method.
type MethodFunc func(ctx context.Context, d MethodDeps, params json.RawMessage) (interface{}, *Error)

var (
	methodsMu       sync.RWMutex
	methods         = map[string]MethodFunc{}
	mutationMethods = map[string]bool{}
)

// RegisterMethod adds a method. Registering a name twice panics: it is a
// wiring bug.
func RegisterMethod(name string, fn MethodFunc) {
	methodsMu.Lock()
	defer methodsMu.Unlock()
	if _, dup := methods[name]; dup {
		panic(fmt.Sprintf("jsonrpc: method %q registered twice", name))
	}
	methods[name] = fn
}

func registeredMethod(name string) (MethodFunc, bool) {
	methodsMu.RLock()
	defer methodsMu.RUnlock()
	fn, ok := methods[name]
	return fn, ok
}

// RegisterMutationMethod opts a custom method out of read snapshots. Existing
// registered methods are queries; writes must declare this boundary explicitly.
func RegisterMutationMethod(name string, fn MethodFunc) {
	methodsMu.Lock()
	defer methodsMu.Unlock()
	if _, dup := methods[name]; dup {
		panic(fmt.Sprintf("jsonrpc: method %q registered twice", name))
	}
	methods[name] = fn
	mutationMethods[name] = true
}
func registeredMutation(name string) bool {
	methodsMu.RLock()
	defer methodsMu.RUnlock()
	return mutationMethods[name]
}
