package api

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"go.uber.org/zap"
	"net/http"
)

func (s *Server) readRoute(r Route) http.Handler { return readRouteOver(r, s.storage, s.logger) }
func readRouteOver(route Route, store port.QueryStore, logger *zap.Logger) http.Handler {
	handler := route.Handler(store, logger)
	if route.Streaming || route.Method != http.MethodGet && route.Method != http.MethodHead {
		return handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := port.WithQuerySnapshot(r.Context(), store, func(ctx context.Context) error { handler.ServeHTTP(w, r.WithContext(ctx)); return nil })
		if err != nil {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "query snapshot unavailable; retry", http.StatusServiceUnavailable)
		}
	})
}
