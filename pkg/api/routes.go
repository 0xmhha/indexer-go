package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Routes let packages outside the API, such as a project's own handlers
// (refactoring plan R6-2, pkg/sdk), serve HTTP endpoints of their own next
// to the GraphQL API, for responses GraphQL cannot give (status codes,
// fixed JSON shapes). They register in init; every single-chain server,
// including one of declared data only, mounts them, and a multi-chain
// server mounts them under each chain, /chains/{id}/<pattern>, over that
// chain's storage. A pattern with an {id} parameter of its own clashes with
// the chain's and is not mounted per chain (logged).

// Route is one HTTP endpoint.
type Route struct {
	Method string
	// Pattern is a chi route pattern such as "/receipts/{merchant}/{id}";
	// chi.URLParam reads its parameters.
	Pattern string
	// Handler builds the endpoint over the server's storage.
	Handler func(store port.QueryStore, logger *zap.Logger) http.Handler
}

var (
	routeMu sync.Mutex
	routes  = map[string]Route{}
)

// RegisterRoute adds an endpoint. Registering a method and pattern twice
// panics: it is a wiring bug.
func RegisterRoute(r Route) {
	routeMu.Lock()
	defer routeMu.Unlock()
	key := r.Method + " " + r.Pattern
	if _, dup := routes[key]; dup {
		panic(fmt.Sprintf("api: route %s registered twice", key))
	}
	routes[key] = r
}

// registeredRoutes returns the routes in method and pattern order.
func registeredRoutes() []Route {
	routeMu.Lock()
	defer routeMu.Unlock()
	keys := make([]string, 0, len(routes))
	for k := range routes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Route, len(keys))
	for i, k := range keys {
		out[i] = routes[k]
	}
	return out
}

// chainRoutable reports whether a route can be mounted under
// /chains/{id}: its pattern must not use the chain's {id} parameter.
func chainRoutable(r Route) bool {
	return !strings.Contains(r.Pattern, "{id}") && !strings.Contains(r.Pattern, "{id:")
}

// mountRoutes mounts the registered routes over the server's storage.
func (s *Server) mountRoutes() {
	for _, r := range registeredRoutes() {
		s.router.Method(r.Method, r.Pattern, r.Handler(s.storage, s.logger))
		s.logger.Info("Route registered", zap.String("method", r.Method), zap.String("pattern", r.Pattern))
	}
}
