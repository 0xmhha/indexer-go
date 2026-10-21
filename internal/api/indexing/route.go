// Package indexing exposes committed source and feature/worker coverage.
package indexing

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/0xmhha/indexer-go/internal/indexingstatus"
	"github.com/0xmhha/indexer-go/internal/progress"
	"github.com/0xmhha/indexer-go/pkg/api"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

const Path = "/indexing/status"

func init() {
	api.RegisterRoute(api.Route{Method: http.MethodGet, Pattern: Path, Handler: Handler})
	api.RegisterRoute(api.Route{Method: http.MethodGet, Pattern: "/indexing/workers/{worker}/blocks/{height}", Handler: OutputHandler})
}
func Handler(store port.QueryStore, log *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		after := r.URL.Query().Get("after")
		if after != "" && !progress.ValidName(after) {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid after"})
			return
		}
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 || n > 100 {
				w.WriteHeader(400)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid limit"})
				return
			}
			limit = n
		}
		result, e := indexingstatus.Read(r.Context(), store, after, limit)
		if e != nil {
			status, code := http.StatusInternalServerError, "indexing_status_error"
			if errors.Is(e, indexingstatus.ErrUnavailable) {
				status, code = http.StatusServiceUnavailable, "indexing_status_unavailable"
			} else if log != nil {
				log.Error("Read indexing status", zap.Error(e))
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}

func OutputHandler(store port.QueryStore, log *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		name := chi.URLParam(r, "worker")
		n, e := strconv.ParseUint(chi.URLParam(r, "height"), 10, 64)
		if e != nil || !progress.ValidName(name) {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_worker_request"})
			return
		}
		result, e := indexingstatus.ReadOutput(r.Context(), store, name, n)
		if e != nil {
			status, code := 500, "worker_output_error"
			switch {
			case errors.Is(e, port.ErrNotFound):
				status, code = 404, "worker_output_not_ready"
			case errors.Is(e, capture.ErrStale):
				status, code = 409, "worker_resync_required"
			case errors.Is(e, indexingstatus.ErrUnavailable):
				status, code = 503, "worker_output_unavailable"
			default:
				if log != nil {
					log.Error("Read worker output", zap.Error(e))
				}
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}
