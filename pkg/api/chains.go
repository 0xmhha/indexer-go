package api

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/api/jsonrpc"
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

	mu       sync.Mutex
	handlers map[string]*chainHandlers
}

// chainHandlers are one chain's API handlers over one store.
type chainHandlers struct {
	store   port.QueryStore
	graphql *graphql.Handler
	sub     http.Handler
	rpc     http.Handler
}

func (s *Server) mountChainRoutes(chains ChainStores) {
	cr := &chainRoutes{
		chains:    chains,
		logger:    s.logger,
		keepAlive: s.config.EnableWebSocketKeepAlive,
		handlers:  map[string]*chainHandlers{},
	}
	s.router.Get("/chains", cr.list)
	if s.config.EnableGraphQL {
		s.router.Handle("/chains/{id}/graphql", cr.serve(func(h *chainHandlers) http.Handler { return h.graphql }))
		s.router.Get("/chains/{id}/graphql/ws", cr.serve(func(h *chainHandlers) http.Handler { return h.sub }))
		s.router.Get("/chains/{id}/playground", cr.serve(func(h *chainHandlers) http.Handler { return h.graphql.PlaygroundHandler() }))
	}
	if s.config.EnableJSONRPC {
		s.router.Post("/chains/{id}/rpc", cr.serve(func(h *chainHandlers) http.Handler { return h.rpc }))
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
	if h, ok := cr.handlers[id]; ok && h.store == store {
		return h, http.StatusOK
	}
	logger := cr.logger.With(zap.String("chain", id))
	gql, err := graphql.NewHandlerWithOptions(store, logger, nil)
	if err != nil {
		logger.Error("failed to create GraphQL handler", zap.Error(err))
		return nil, http.StatusInternalServerError
	}
	h := &chainHandlers{
		store:   store,
		graphql: gql,
		sub:     graphql.NewSubscriptionServer(bus, logger, cr.keepAlive).Handler(),
		rpc:     jsonrpc.NewServer(store, logger),
	}
	cr.handlers[id] = h
	return h, http.StatusOK
}

// list answers GET /chains with the registered chains.
func (cr *chainRoutes) list(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cr.chains.ListChains())
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
