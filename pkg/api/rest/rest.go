// Package rest serves the paths clients poll most as plain HTTP GET
// resources (refactoring plan R4-4): latest blocks and transactions, an
// address's balance, overview, tokens and transactions, and network
// statistics.
//
// Each path runs one fixed GraphQL document through the served schema, so
// a response is exactly the GraphQL response of that document ({"data":
// ...}, with "errors" when a resolver failed) and clients can switch from
// the GraphQL query without changing how they read it. Unlike a GraphQL
// request it is a cacheable GET: responses carry an ETag (a request with a
// matching If-None-Match gets 304 without a body) and Cache-Control.
//
// The handler also keeps successful responses for as long as Cache-Control
// lets clients keep them (refactoring plan R4-5): clients polling the same
// path share one execution per second, and requests arriving together for
// the same uncached response share one execution (singleflight).
package rest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/graphql-go/graphql"
	"golang.org/x/sync/singleflight"
)

// Executor runs a GraphQL document against the served schema
// (graphql.Handler).
type Executor interface {
	Execute(ctx context.Context, document string, variables map[string]interface{}) *graphql.Result
}

// MaxLimit is the largest page a request may ask for (the stores' cap).
const MaxLimit = 100

// cacheControl is sent with successful responses: the data changes with
// every block, so caches may reuse a response for a second. DefaultCacheTTL
// keeps them in the handler as long.
const (
	cacheControl    = "public, max-age=1"
	DefaultCacheTTL = time.Second
)

// maxCached bounds the responses the handler keeps (a page of 100 rows is
// some tens of KB).
const maxCached = 1024

// executeTimeout bounds an execution several requests share: it runs apart
// from the context of the request that started it.
const executeTimeout = 30 * time.Second

// kind is how a parameter is read and checked.
type kind int

const (
	kindNumber  kind = iota // decimal unsigned integer, passed as a string
	kindInt                 // integer, passed as an Int
	kindLimit               // page size 1..MaxLimit
	kindOffset              // page offset >= 0
	kindCursor              // page cursor from a previous response's pageInfo.endCursor
	kindAddress             // hex address
	kindName                // short identifier (letters and digits)
)

// param is a query-string parameter, or the {address} path segment.
type param struct {
	name     string
	kind     kind
	required bool
}

// endpoint is one REST path.
type endpoint struct {
	document string
	params   []param
}

// rangeParams page the lists read by block range (blocks, transactions):
// by offset from the newest. cursorParams page the lists the stores keep
// (an address's transactions): by offset or after a previous page's
// pageInfo.endCursor.
var (
	rangeParams  = []param{{name: "limit", kind: kindLimit}, {name: "offset", kind: kindOffset}}
	cursorParams = append(rangeParams, param{name: "after", kind: kindCursor})
)

// endpoints by path; "addresses/{address}/<name>" paths are under
// "address/<name>". The field sets are those indexer-frontend asks for.
var endpoints = map[string]endpoint{
	"blocks": {
		document: `query($limit: Int, $offset: Int, $numberFrom: String, $numberTo: String, $miner: String) {
  blocks(pagination: {limit: $limit, offset: $offset}, filter: {numberFrom: $numberFrom, numberTo: $numberTo, miner: $miner}) {
    nodes { number hash timestamp miner gasUsed gasLimit size transactionCount }
    totalCount
    pageInfo { hasNextPage }
  }
}`,
		params: append([]param{{name: "numberFrom", kind: kindNumber}, {name: "numberTo", kind: kindNumber}, {name: "miner", kind: kindAddress}}, rangeParams...),
	},
	"transactions": {
		document: `query($limit: Int, $offset: Int, $blockNumberFrom: String, $blockNumberTo: String, $from: String, $to: String, $type: Int) {
  transactions(pagination: {limit: $limit, offset: $offset}, filter: {blockNumberFrom: $blockNumberFrom, blockNumberTo: $blockNumberTo, from: $from, to: $to, type: $type}) {
    nodes { hash blockNumber from to contractAddress value gas gasPrice type }
    totalCount
    pageInfo { hasNextPage }
  }
}`,
		params: append([]param{
			{name: "blockNumberFrom", kind: kindNumber}, {name: "blockNumberTo", kind: kindNumber},
			{name: "from", kind: kindAddress}, {name: "to", kind: kindAddress}, {name: "type", kind: kindInt},
		}, rangeParams...),
	},
	"address/balance": {
		document: `query($address: String!, $blockNumber: String) {
  addressBalance(address: $address, blockNumber: $blockNumber)
}`,
		params: []param{{name: "blockNumber", kind: kindNumber}},
	},
	"address/overview": {
		document: `query($address: String!) {
  addressOverview(address: $address) {
    address isContract balance transactionCount sentCount receivedCount
    internalTxCount erc20TokenCount erc721TokenCount firstSeen lastSeen
  }
}`,
	},
	"address/tokens": {
		document: `query($address: String!, $tokenType: String) {
  tokenBalances(address: $address, tokenType: $tokenType) {
    address tokenType balance tokenId name symbol decimals metadata
  }
}`,
		params: []param{{name: "tokenType", kind: kindName}},
	},
	"address/transactions": {
		document: `query($address: String!, $limit: Int, $offset: Int, $after: String) {
  transactionsByAddress(address: $address, pagination: {limit: $limit, offset: $offset, after: $after}) {
    nodes {
      hash blockNumber blockTimestamp from to contractAddress value gas gasPrice type
      receipt { status contractAddress }
    }
    totalCount
    pageInfo { hasNextPage endCursor }
  }
}`,
		params: cursorParams,
	},
	"stats/miners": {
		document: `query($limit: Int, $fromBlock: String, $toBlock: String) {
  topMiners(limit: $limit, fromBlock: $fromBlock, toBlock: $toBlock) {
    address blockCount percentage totalRewards lastBlockNumber lastBlockTime
  }
}`,
		params: []param{{name: "limit", kind: kindLimit}, {name: "fromBlock", kind: kindNumber}, {name: "toBlock", kind: kindNumber}},
	},
	"stats/network": {
		document: `query($fromTime: String!, $toTime: String!) {
  networkMetrics(fromTime: $fromTime, toTime: $toTime) {
    tps blockTime totalBlocks totalTransactions averageBlockSize timePeriod
  }
}`,
		params: []param{{name: "fromTime", kind: kindNumber, required: true}, {name: "toTime", kind: kindNumber, required: true}},
	},
}

// Paths returns the served paths, for documentation and tests.
func Paths() []string {
	out := make([]string, 0, len(endpoints))
	for p := range endpoints {
		if rest, ok := strings.CutPrefix(p, "address/"); ok {
			p = "addresses/{address}/" + rest
		}
		out = append(out, p)
	}
	return out
}

// Handler serves the REST paths. Mount it under a pattern ending in "/*"
// (such as /v1/*): it reads its path from the pattern's wildcard.
type Handler struct {
	exec  Executor
	ttl   time.Duration
	group singleflight.Group

	mu     sync.Mutex
	cached map[string]response
}

// response is an executed request: its body, and for a successful one its
// ETag and when it stops being served from the handler.
type response struct {
	body    []byte
	status  int
	etag    string
	expires time.Time
}

// NewHandler returns the REST API over exec, keeping responses for
// DefaultCacheTTL.
func NewHandler(exec Executor) *Handler { return NewHandlerWithCache(exec, DefaultCacheTTL) }

// NewHandlerWithCache returns the REST API over exec keeping successful
// responses for ttl (0: not kept; requests arriving together still share
// an execution).
func NewHandlerWithCache(exec Executor, ttl time.Duration) *Handler {
	return &Handler{exec: exec, ttl: ttl, cached: map[string]response{}}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	path := strings.Trim(chi.URLParam(r, "*"), "/")
	var address string
	if rest, ok := strings.CutPrefix(path, "addresses/"); ok {
		if a, sub, ok := strings.Cut(rest, "/"); ok {
			address, path = a, "address/"+sub
		}
	}
	ep, ok := endpoints[path]
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	vars, err := variables(ep, r, address)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := h.respond(r.Context(), path, ep, vars)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if resp.etag == "" {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(resp.status)
		_, _ = w.Write(resp.body)
		return
	}
	w.Header().Set("ETag", resp.etag)
	w.Header().Set("Cache-Control", cacheControl)
	if matches(r.Header.Get("If-None-Match"), resp.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(resp.body)
	}
}

// respond returns the response of a request: kept, or executed once for
// the requests asking for it together.
func (h *Handler) respond(ctx context.Context, path string, ep endpoint, vars map[string]interface{}) (response, error) {
	key, err := json.Marshal(vars) // map keys are sorted
	if err != nil {
		return response{}, fmt.Errorf("failed to encode the request")
	}
	k := path + "\x00" + string(key)
	if resp, ok := h.lookup(k); ok {
		return resp, nil
	}
	ch := h.group.DoChan(k, func() (interface{}, error) {
		if resp, ok := h.lookup(k); ok {
			return resp, nil
		}
		ectx, cancel := context.WithTimeout(context.WithoutCancel(ctx), executeTimeout)
		defer cancel()
		resp, err := execute(ectx, h.exec, ep, vars)
		if err == nil && resp.etag != "" {
			h.keep(k, resp)
		}
		return resp, err
	})
	select {
	case <-ctx.Done():
		return response{}, ctx.Err()
	case r := <-ch:
		if r.Err != nil {
			return response{}, r.Err
		}
		return r.Val.(response), nil
	}
}

// execute runs an endpoint's document. A response with errors gets no ETag:
// it is neither cached nor kept.
func execute(ctx context.Context, exec Executor, ep endpoint, vars map[string]interface{}) (response, error) {
	res := exec.Execute(ctx, ep.document, vars)
	body, err := json.Marshal(res)
	if err != nil {
		return response{}, fmt.Errorf("failed to encode the response")
	}
	if len(res.Errors) > 0 {
		status := http.StatusOK
		if failed(res) {
			status = http.StatusInternalServerError
		}
		return response{body: body, status: status}, nil
	}
	sum := sha256.Sum256(body)
	return response{body: body, status: http.StatusOK, etag: `"` + hex.EncodeToString(sum[:16]) + `"`}, nil
}

// lookup returns a kept response that has not expired.
func (h *Handler) lookup(key string) (response, bool) {
	if h.ttl <= 0 {
		return response{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	resp, ok := h.cached[key]
	if !ok || time.Now().After(resp.expires) {
		return response{}, false
	}
	return resp, true
}

// keep keeps a successful response for the handler's TTL. When maxCached
// responses are kept, expired ones are dropped first; if none has expired
// the response is not kept.
func (h *Handler) keep(key string, resp response) {
	if h.ttl <= 0 {
		return
	}
	now := time.Now()
	resp.expires = now.Add(h.ttl)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.cached) >= maxCached {
		for k, r := range h.cached {
			if now.After(r.expires) {
				delete(h.cached, k)
			}
		}
		if len(h.cached) >= maxCached {
			return
		}
	}
	h.cached[key] = resp
}

// failed reports whether a result with errors has no data: the requested
// field itself failed rather than a part of it.
func failed(res *graphql.Result) bool {
	data, _ := res.Data.(map[string]interface{})
	for _, v := range data {
		if v != nil {
			return false
		}
	}
	return true
}

// matches reports whether an If-None-Match header names etag.
func matches(header, etag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(t)
		if t == "*" || strings.TrimPrefix(t, "W/") == etag {
			return true
		}
	}
	return false
}

// variables reads and checks an endpoint's parameters.
func variables(ep endpoint, r *http.Request, address string) (map[string]interface{}, error) {
	vars := map[string]interface{}{}
	if strings.Contains(ep.document, "$address") {
		if !common.IsHexAddress(address) {
			return nil, fmt.Errorf("invalid address %q", address)
		}
		vars["address"] = address
	}
	q := r.URL.Query()
	for _, p := range ep.params {
		raw := q.Get(p.name)
		if raw == "" {
			if p.required {
				return nil, fmt.Errorf("%s is required", p.name)
			}
			continue
		}
		v, err := read(p.kind, raw)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", p.name, err)
		}
		vars[p.name] = v
	}
	return vars, nil
}

func read(k kind, raw string) (interface{}, error) {
	switch k {
	case kindNumber:
		if _, err := strconv.ParseUint(raw, 10, 64); err != nil {
			return nil, fmt.Errorf("want a decimal number")
		}
		return raw, nil
	case kindInt:
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("want an integer")
		}
		return n, nil
	case kindLimit:
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > MaxLimit {
			return nil, fmt.Errorf("want 1 to %d", MaxLimit)
		}
		return n, nil
	case kindOffset:
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("want 0 or more")
		}
		return n, nil
	case kindAddress:
		if !common.IsHexAddress(raw) {
			return nil, fmt.Errorf("want a hex address")
		}
		return raw, nil
	case kindName:
		if len(raw) > 32 || strings.IndexFunc(raw, func(c rune) bool {
			return (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9')
		}) >= 0 {
			return nil, fmt.Errorf("want letters and digits")
		}
		return raw, nil
	default: // kindCursor
		if len(raw) > 512 {
			return nil, fmt.Errorf("too long")
		}
		return raw, nil
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
