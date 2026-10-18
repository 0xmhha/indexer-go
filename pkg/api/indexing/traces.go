package indexing

import (
	"encoding/hex"
	"encoding/json"
	"github.com/0xmhha/indexer-go/pkg/api"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/indexingstatus"
	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"net/http"
	"strconv"
)

func init() {
	api.RegisterRoute(api.Route{Method: http.MethodGet, Pattern: "/indexing/traces/blocks/{height}", Handler: TraceHandler})
}
func TraceHandler(store port.QueryStore, log *zap.Logger) http.Handler {
	slots := make(chan struct{}, 2)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		n, e := strconv.ParseUint(chi.URLParam(r, "height"), 10, 64)
		if e != nil {
			stateError(w, 400, "invalid_trace_request")
			return
		}
		var after uint64
		limit := 50
		if raw := r.URL.Query().Get("after"); raw != "" {
			after, e = strconv.ParseUint(raw, 10, 32)
			if e != nil {
				stateError(w, 400, "invalid_trace_request")
				return
			}
		}
		if raw := r.URL.Query().Get("limit"); raw != "" {
			limit, e = strconv.Atoi(raw)
			if e != nil || limit < 1 || limit > 256 {
				stateError(w, 400, "invalid_trace_request")
				return
			}
		}
		expected := []indexingstatus.TraceScope{}
		q := r.URL.Query()
		if after > 0 || q.Has("epoch") || q.Has("hash") || q.Has("selection") {
			epoch, e := strconv.ParseUint(q.Get("epoch"), 10, 64)
			if e != nil || epoch == 0 {
				stateError(w, 400, "invalid_trace_request")
				return
			}
			parseHash := func(v string) (common.Hash, bool) {
				if len(v) != 66 || v[:2] != "0x" {
					return common.Hash{}, false
				}
				b, e := hex.DecodeString(v[2:])
				h := common.BytesToHash(b)
				return h, e == nil && h != (common.Hash{})
			}
			hash, ok := parseHash(q.Get("hash"))
			selection, okSelection := parseHash(q.Get("selection"))
			if !ok || !okSelection {
				stateError(w, 400, "invalid_trace_request")
				return
			}
			expected = append(expected, indexingstatus.TraceScope{Epoch: epoch, Hash: hash, Selection: selection})
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			stateError(w, 429, "call_trace_busy")
			return
		}
		out, e := indexingstatus.ReadTraces(r.Context(), store, n, uint32(after), limit, expected...)
		if e != nil {
			status, code := 500, "call_trace_error"
			switch e {
			case port.ErrNotFound:
				status, code = 404, "call_trace_not_ready"
			case capture.ErrStale:
				status, code = 409, "call_trace_resync_required"
			case indexingstatus.ErrUnavailable:
				status, code = 503, "call_trace_unavailable"
			case port.ErrInvalidData:
				status, code = 400, "invalid_trace_request"
			default:
				if log != nil {
					log.Error("Read call trace", zap.Error(e))
				}
			}
			stateError(w, status, code)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}
