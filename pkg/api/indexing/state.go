package indexing

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/0xmhha/indexer-go/pkg/api"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/indexingstatus"
	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func init() {
	for _, route := range []struct{ path, kind string }{{"/indexing/state/current", "current"}, {"/indexing/state/blocks/{height}", "block"}, {"/indexing/state/events", "events"}, {"/indexing/state/inputs/{height}/{hash}/{generation}", "input"}} {
		api.RegisterRoute(api.Route{Method: http.MethodGet, Pattern: route.path, Handler: stateHandler(route.kind)})
	}
}
func stateHandler(kind string) func(port.QueryStore, *zap.Logger) http.Handler {
	return func(store port.QueryStore, log *zap.Logger) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			var result any
			var err error
			switch kind {
			case "current":
				result, err = indexingstatus.ReadState(r.Context(), store)
			case "block", "input":
				n, e := strconv.ParseUint(chi.URLParam(r, "height"), 10, 64)
				if e != nil {
					stateError(w, 400, "invalid_state_request")
					return
				}
				if kind == "block" {
					result, err = indexingstatus.ReadStateChanges(r.Context(), store, n)
				} else {
					raw := chi.URLParam(r, "hash")
					if len(raw) != 66 || raw[:2] != "0x" {
						stateError(w, 400, "invalid_state_request")
						return
					}
					hash, e := hex.DecodeString(raw[2:])
					if e != nil {
						stateError(w, 400, "invalid_state_request")
						return
					}
					generation, e := strconv.ParseUint(chi.URLParam(r, "generation"), 10, 64)
					if e != nil || generation == 0 {
						stateError(w, 400, "invalid_state_request")
						return
					}
					result, err = indexingstatus.ReadStateInput(r.Context(), store, n, generation, common.BytesToHash(hash))
				}
			case "events":
				after := uint64(0)
				limit := 50
				if raw := r.URL.Query().Get("after"); raw != "" {
					n, e := strconv.ParseUint(raw, 10, 64)
					if e != nil {
						stateError(w, 400, "invalid_state_request")
						return
					}
					after = n
				}
				if raw := r.URL.Query().Get("limit"); raw != "" {
					n, e := strconv.Atoi(raw)
					if e != nil || n < 1 || n > 256 {
						stateError(w, 400, "invalid_state_request")
						return
					}
					limit = n
				}
				result, err = indexingstatus.ReadStateEvents(r.Context(), store, after, limit)
			}
			if err != nil {
				status, code := 500, "state_projection_error"
				switch err {
				case port.ErrNotFound:
					status, code = 404, "state_projection_not_ready"
				case capture.ErrStale:
					status, code = 409, "state_projection_resync_required"
				case indexingstatus.ErrUnavailable:
					status, code = 503, "state_projection_unavailable"
				case port.ErrInvalidData:
					status, code = 400, "invalid_state_request"
				default:
					if log != nil {
						log.Error("Read state projection", zap.Error(err))
					}
				}
				stateError(w, status, code)
				return
			}
			_ = json.NewEncoder(w).Encode(result)
		})
	}
}
func stateError(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
