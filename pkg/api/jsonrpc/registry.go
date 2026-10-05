package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/storage"
)

// Methods registered by packages outside the API, such as chain-specific
// code under pkg/chains/<chain>/, served after the built-in methods.

// MethodDeps is what a registered method receives.
type MethodDeps struct {
	Storage storage.Storage
	Logger  *zap.Logger
}

// MethodFunc serves one JSON-RPC method.
type MethodFunc func(ctx context.Context, d MethodDeps, params json.RawMessage) (interface{}, *Error)

var (
	methodsMu sync.RWMutex
	methods   = map[string]MethodFunc{}
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
