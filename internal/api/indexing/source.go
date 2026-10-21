package indexing

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/0xmhha/indexer-go/internal/indexingstatus"
	"github.com/0xmhha/indexer-go/pkg/api"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func init() {
	api.RegisterRoute(api.Route{Method: http.MethodGet, Pattern: "/indexing/source/*", Handler: SourceHandler})
}
func sourceHash(raw string) (h common.Hash, ok bool) {
	if len(raw) != 66 || !strings.HasPrefix(raw, "0x") {
		return h, false
	}
	b, e := hex.DecodeString(raw[2:])
	h = common.BytesToHash(b)
	return h, e == nil && h != (common.Hash{})
}

// One admission pool covers all source routes, including malformed/failed
// requests. The handler has no RPC, writer, raw ACK or GC dependencies.
func SourceHandler(store port.QueryStore, log *zap.Logger) http.Handler {
	slots := make(chan struct{}, 2)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fail := func() { stateError(w, 400, "invalid_source_request") }
		tail := chi.URLParam(r, "*")
		if tail == "" {
			tail = strings.TrimPrefix(r.URL.Path, "/indexing/source/")
		}
		path := strings.Split(tail, "/")
		if len(path) == 1 && (path[0] == "logs" || path[0] == "transactions") {
			q, e := parseSourceSearch(r, path[0])
			if e != nil {
				fail()
				return
			}
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			default:
				stateError(w, 429, "source_query_busy")
				return
			}
			out, e := indexingstatus.ReadSearch(r.Context(), store, q.filter, q.cursor, q.limit)
			if e != nil {
				sourceQueryError(w, log, e)
				return
			}
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		q := indexingstatus.SourceRequest{Limit: 50}
		switch {
		case len(path) == 3 && path[0] == "blocks" && path[1] != "by-hash":
			n, e := strconv.ParseUint(path[1], 10, 64)
			if e != nil {
				fail()
				return
			}
			q.Height = &n
			q.Kind = capture.RowKind(path[2])
		case len(path) == 4 && path[0] == "blocks" && path[1] == "by-hash":
			h, ok := sourceHash(path[2])
			if !ok {
				fail()
				return
			}
			q.Hash = h
			q.Kind = capture.RowKind(path[3])
		case len(path) == 3 && path[0] == "transactions":
			h, ok := sourceHash(path[1])
			if !ok {
				fail()
				return
			}
			q.Hash = h
			q.Transaction = true
			q.Kind = capture.RowKind(path[2])
		default:
			stateError(w, 404, "source_route_not_found")
			return
		}
		if capture.RowCodec(q.Kind) == "" || q.Transaction && q.Kind != capture.RowTransaction && q.Kind != capture.RowReceipt && q.Kind != capture.RowLog {
			fail()
			return
		}
		query := r.URL.Query()
		if query.Has("after") {
			n, e := strconv.ParseUint(query.Get("after"), 10, 32)
			if e != nil {
				fail()
				return
			}
			q.After = uint32(n)
		}
		if query.Has("limit") {
			n, e := strconv.Atoi(query.Get("limit"))
			if e != nil || n < 1 || n > 256 {
				fail()
				return
			}
			q.Limit = n
		}
		if q.After > 0 || query.Has("epoch") || query.Has("hash") || query.Has("definition") {
			epoch, e := strconv.ParseUint(query.Get("epoch"), 10, 64)
			h, ok := sourceHash(query.Get("hash"))
			def := query.Get("definition")
			if e != nil || epoch == 0 || !ok || len(def) == 0 || len(def) > 128 {
				fail()
				return
			}
			q.Expected = &indexingstatus.SourceScope{Epoch: epoch, Hash: h, Definition: def}
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			stateError(w, 429, "source_query_busy")
			return
		}
		result, e := indexingstatus.ReadSource(r.Context(), store, q)
		if e != nil {
			sourceQueryError(w, log, e)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}

func sourceQueryError(w http.ResponseWriter, log *zap.Logger, e error) {
	code, status := "source_query_error", 500
	switch {
	case errors.Is(e, capture.ErrStale):
		code, status = "source_resync_required", 409
	case errors.Is(e, capture.ErrConflict): // Corruption may wrap a missing DB key; never report absence.
	case errors.Is(e, port.ErrInvalidData):
		code, status = "invalid_source_request", 400
	case errors.Is(e, port.ErrNotFound):
		code, status = "source_not_ready", 404
	case errors.Is(e, indexingstatus.ErrUnavailable):
		code, status = "source_unavailable", 503
	default:
		if log != nil {
			log.Error("Read source facts", zap.Error(e))
		}
	}
	stateError(w, status, code)
}
