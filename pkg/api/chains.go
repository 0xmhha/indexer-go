package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sync"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/api/jsonrpc"
	apimiddleware "github.com/0xmhha/indexer-go/pkg/api/middleware"
	"github.com/0xmhha/indexer-go/pkg/api/rest"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/multichain"
)

// ChainStores gives the API the chains of multichain mode, each indexed
// into its own database (refactoring plan R2-8). multichain.Manager
// implements it.
type ChainStores interface {
	// ChainStore returns the store and event bus of a running chain.
	ChainStore(id string) (port.QueryStore, *events.EventBus, bool)
	// ListChains describes the registered chains.
	ListChains() []*multichain.ChainInfo
}

// chainRoutes serves every chain's API under /chains/{id}/: graphql,
// graphql/ws (subscriptions), playground and rpc. The handlers of a chain
// are built from its store when first requested and rebuilt when the chain
// restarts with a new store.
type chainRoutes struct {
	chains    ChainStores
	logger    *zap.Logger
	keepAlive bool
	direct    bool

	streamOutbox func(port.QueryStore) port.Outbox

	limits  graphql.Limits
	origins apimiddleware.Origins

	mu       sync.Mutex
	handlers map[string]*chainHandlers
}

// chainHandlers are one chain's API handlers over one store.
type chainHandlers struct {
	store   port.QueryStore
	graphql *graphql.Handler
	sub     http.Handler
	rpc     *jsonrpc.Server
	rest    http.Handler
	routes  map[string]http.Handler // registered routes by method and pattern
}

func (s *Server) mountChainRoutes(chains ChainStores) {
	cr := &chainRoutes{
		chains:    chains,
		logger:    s.logger,
		keepAlive: s.config.EnableWebSocketKeepAlive,
		direct:    s.config.DirectSubscriptions,
		handlers:  map[string]*chainHandlers{},
		limits:    s.graphqlLimits(),
		origins:   s.origins,

		streamOutbox: s.streamOutbox,
	}
	s.chainRoutes = cr
	s.router.Get("/chains", cr.list)
	if s.config.EnableGraphQL {
		s.router.Handle("/chains/{id}/graphql", cr.serve(func(h *chainHandlers) http.Handler { return h.graphql }))
		s.router.Get("/chains/{id}/graphql/ws", cr.serve(func(h *chainHandlers) http.Handler { return h.sub }))
		s.router.Get("/chains/{id}/playground", cr.serve(func(h *chainHandlers) http.Handler { return h.graphql.PlaygroundHandler() }))
	}
	if s.config.EnableJSONRPC {
		s.router.Post("/chains/{id}/rpc", cr.serve(func(h *chainHandlers) http.Handler { return h.rpc }))
	}
	if s.config.EnableREST {
		s.router.Handle("/chains/{id}/v1/*", cr.serve(func(h *chainHandlers) http.Handler { return h.rest }))
	}
	for _, r := range registeredRoutes() {
		if !chainRoutable(r) {
			s.logger.Warn("Route not mounted per chain: its {id} parameter clashes with the chain's",
				zap.String("method", r.Method), zap.String("pattern", r.Pattern))
			continue
		}
		key := r.Method + " " + r.Pattern
		s.router.Method(r.Method, "/chains/{id}"+r.Pattern, cr.serve(func(h *chainHandlers) http.Handler { return h.routes[key] }))
		s.logger.Info("Route registered per chain", zap.String("method", r.Method), zap.String("pattern", "/chains/{id}"+r.Pattern))
	}
	s.logger.Info("Per-chain API enabled", zap.String("path", "/chains/{id}/"))
}

// serve dispatches a request to the handler pick selects for the chain
// named in the path.
func (cr *chainRoutes) serve(pick func(*chainHandlers) http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		h, status := cr.lookup(id)
		if h == nil {
			msg := "chain not found"
			if status == http.StatusServiceUnavailable {
				msg = "chain is not running"
			}
			writeJSONError(w, status, msg)
			return
		}
		pick(h).ServeHTTP(w, r)
	}
}

// lookup returns the chain's handlers, or the HTTP status to answer with:
// 404 for an unknown chain, 503 for a chain that is not running.
func (cr *chainRoutes) lookup(id string) (*chainHandlers, int) {
	store, bus, ok := cr.chains.ChainStore(id)
	if !ok || store == nil {
		for _, info := range cr.chains.ListChains() {
			if info.ID == id {
				return nil, http.StatusServiceUnavailable
			}
		}
		return nil, http.StatusNotFound
	}
	cr.mu.Lock()
	defer cr.mu.Unlock()
	old, ok := cr.handlers[id]
	if ok && old.store == store {
		return old, http.StatusOK
	}
	if ok {
		old.rpc.Close() // the chain restarted with a new store
	}
	logger := cr.logger.With(zap.String("chain", id))
	outbox := cr.streamOutbox(store)
	opts := &graphql.HandlerOptions{Limits: cr.limits}
	if outbox != nil {
		opts.Stream = outbox
	}
	gql, err := graphql.NewHandlerWithOptions(store, logger, opts)
	if err != nil {
		logger.Error("failed to create GraphQL handler", zap.Error(err))
		return nil, http.StatusInternalServerError
	}
	sub := graphql.NewSubscriptionServer(bus, logger, cr.keepAlive)
	sub.SetDirect(cr.direct)
	sub.SetCheckOrigin(cr.origins.CheckWebSocketOrigin)
	if outbox != nil {
		sub.SetOutbox(outbox)
	}
	h := &chainHandlers{
		store:   store,
		graphql: gql,
		sub:     sub.Handler(),
		rpc:     jsonrpc.NewServer(store, logger),
		rest:    rest.NewHandler(gql),
		routes:  map[string]http.Handler{},
	}
	for _, r := range registeredRoutes() {
		if chainRoutable(r) {
			h.routes[r.Method+" "+r.Pattern] = r.Handler(store, logger)
		}
	}
	cr.handlers[id] = h
	return h, http.StatusOK
}

// close stops the background work of every chain's handlers.
func (cr *chainRoutes) close() {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	for id, h := range cr.handlers {
		h.rpc.Close()
		delete(cr.handlers, id)
	}
}

// list answers GET /chains with the registered chains.
// list describes the registered chains. Node URLs keep only their scheme
// and host: their user information, path and query often carry
// credentials (many providers put the API key in the path or the query).
func (cr *chainRoutes) list(w http.ResponseWriter, _ *http.Request) {
	infos := cr.chains.ListChains()
	out := make([]multichain.ChainInfo, len(infos))
	for i, info := range infos {
		out[i] = *info
		out[i].RPCEndpoint = redactURL(info.RPCEndpoint)
		out[i].WSEndpoint = redactURL(info.WSEndpoint)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// redactURL returns the scheme and host (with port) of a node URL, "" when
// it has none.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if raw == "" || err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
