package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// parseBody reads a single JSON-RPC message or a batch.
func parseBody[T any](body []byte) ([]T, bool, error) {
	body = bytes.TrimSpace(body)
	if len(body) > 0 && body[0] == '[' {
		var out []T
		err := json.Unmarshal(body, &out)
		return out, true, err
	}
	var one T
	err := json.Unmarshal(body, &one)
	return []T{one}, false, err
}

// ---------------------------------------------------------------------------
// Recorder
// ---------------------------------------------------------------------------

// Recorder is an HTTP JSON-RPC proxy: it forwards each request to the
// upstream node and records the calls and their answers.
type Recorder struct {
	upstream string
	client   *http.Client
	w        *Writer
}

// NewRecorder returns a proxy to upstream that records into w.
func NewRecorder(upstream string, w *Writer) *Recorder {
	return &Recorder{upstream: upstream, client: &http.Client{}, w: w}
}

func (rc *Recorder) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, rc.upstream, bytes.NewReader(body))
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := rc.client.Do(req)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	if resp.StatusCode == http.StatusOK {
		rc.record(body, out)
	}
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(resp.StatusCode)
	_, _ = rw.Write(out)
}

// record pairs requests with responses by id and stores them.
func (rc *Recorder) record(reqBody, respBody []byte) {
	reqs, _, err := parseBody[rpcRequest](reqBody)
	if err != nil {
		return
	}
	resps, _, err := parseBody[rpcResponse](respBody)
	if err != nil {
		return
	}
	byID := make(map[string]rpcResponse, len(resps))
	for _, r := range resps {
		byID[string(r.ID)] = r
	}
	for _, q := range reqs {
		r, ok := byID[string(q.ID)]
		if !ok || q.Method == "" {
			continue
		}
		_ = rc.w.Record(q.Method, q.Params, r.Result, r.Error)
	}
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

// Server answers JSON-RPC requests from an archive: a frozen node whose head
// is the archive's last block.
type Server struct {
	a *Archive

	mu          sync.Mutex
	unrecorded  map[string]int
	hashOnce    sync.Once
	byBlockHash map[common.Hash]uint64
	txs         map[common.Hash]txRef
	indexErr    error
}

type txRef struct {
	height uint64
	index  int
}

// NewServer serves archive a.
func NewServer(a *Archive) *Server {
	return &Server{a: a, unrecorded: map[string]int{}}
}

// Unrecorded counts requests the archive could not answer, by method. A
// replay that needs no unrecorded call reproduces the recorded run.
func (s *Server) Unrecorded() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.unrecorded))
	for k, v := range s.unrecorded {
		out[k] = v
	}
	return out
}

func (s *Server) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	reqs, batch, err := parseBody[rpcRequest](body)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	resps := make([]rpcResponse, len(reqs))
	for i, q := range reqs {
		resps[i] = s.answer(q)
	}
	rw.Header().Set("Content-Type", "application/json")
	if batch {
		_ = json.NewEncoder(rw).Encode(resps)
		return
	}
	_ = json.NewEncoder(rw).Encode(resps[0])
}

var null = json.RawMessage("null")

func (s *Server) answer(q rpcRequest) rpcResponse {
	resp := rpcResponse{JSONRPC: "2.0", ID: q.ID}
	result, rpcErr, ok := s.resolve(q.Method, q.Params)
	switch {
	case !ok:
		s.mu.Lock()
		s.unrecorded[q.Method]++
		s.mu.Unlock()
		resp.Error = json.RawMessage(fmt.Sprintf(`{"code":-32000,"message":"replay: %s not recorded"}`, q.Method))
	case len(rpcErr) > 0:
		resp.Error = rpcErr
	case len(result) == 0:
		resp.Result = null
	default:
		resp.Result = result
	}
	return resp
}

// resolve answers one call: recorded calls as recorded; the head and block
// tags from the archive's range; lookups by hash from the recorded blocks.
func (s *Server) resolve(method string, params json.RawMessage) (json.RawMessage, json.RawMessage, bool) {
	m := s.a.Manifest()
	switch method {
	case "eth_blockNumber":
		return json.RawMessage(fmt.Sprintf("%q", hexutil.EncodeUint64(m.Last))), nil, true
	case "eth_getBlockByNumber", "eth_getBlockReceipts":
		if p, ok := s.resolveTag(params, m); ok {
			params = p
		}
		if h, ok := blockHeight(method, params); ok && h > m.Last {
			return null, nil, true // not mined yet, as a node answers
		}
	case "eth_getBlockByHash":
		return s.byHash(params)
	case "eth_getTransactionReceipt", "eth_getTransactionByHash":
		return s.byTxHash(method, params)
	}
	c, ok, err := s.a.lookup(method, params)
	if err != nil || !ok {
		return nil, nil, false
	}
	return c.Result, c.Error, true
}

// resolveTag replaces a block tag (latest, finalized, ...) with the
// archive's last height.
func (s *Server) resolveTag(params json.RawMessage, m Manifest) (json.RawMessage, bool) {
	var ps []json.RawMessage
	if json.Unmarshal(params, &ps) != nil || len(ps) == 0 {
		return nil, false
	}
	var tag string
	if json.Unmarshal(ps[0], &tag) != nil {
		return nil, false
	}
	var h uint64
	switch tag {
	case "latest", "pending", "safe", "finalized":
		h = m.Last
	case "earliest":
		h = m.First
	default:
		return nil, false
	}
	ps[0] = json.RawMessage(fmt.Sprintf("%q", hexutil.EncodeUint64(h)))
	out, err := json.Marshal(ps)
	return out, err == nil
}

// buildIndexes maps block and transaction hashes to heights by reading every
// recorded full block once.
func (s *Server) buildIndexes() {
	s.byBlockHash = map[common.Hash]uint64{}
	s.txs = map[common.Hash]txRef{}
	for _, start := range s.a.Segments() {
		calls, err := s.a.segment(start)
		if err != nil {
			s.indexErr = err
			return
		}
		for _, c := range calls {
			h, ok := blockHeight(c.Method, c.Params)
			if !ok || c.Method != "eth_getBlockByNumber" || len(c.Result) == 0 || string(c.Result) == "null" {
				continue
			}
			var b struct {
				Hash         common.Hash       `json:"hash"`
				Transactions []json.RawMessage `json:"transactions"`
			}
			if json.Unmarshal(c.Result, &b) != nil {
				continue
			}
			s.byBlockHash[b.Hash] = h
			for i, raw := range b.Transactions {
				var tx struct {
					Hash common.Hash `json:"hash"`
				}
				if json.Unmarshal(raw, &tx) == nil && tx.Hash != (common.Hash{}) {
					s.txs[tx.Hash] = txRef{height: h, index: i}
				}
			}
		}
	}
}

func hashParam(params json.RawMessage) (common.Hash, []json.RawMessage, bool) {
	var ps []json.RawMessage
	if json.Unmarshal(params, &ps) != nil || len(ps) == 0 {
		return common.Hash{}, nil, false
	}
	var h common.Hash
	if json.Unmarshal(ps[0], &h) != nil {
		return common.Hash{}, nil, false
	}
	return h, ps, true
}

func (s *Server) byHash(params json.RawMessage) (json.RawMessage, json.RawMessage, bool) {
	s.hashOnce.Do(s.buildIndexes)
	hash, ps, ok := hashParam(params)
	if !ok || s.indexErr != nil {
		return nil, nil, false
	}
	h, ok := s.byBlockHash[hash]
	if !ok {
		return null, nil, true
	}
	full := json.RawMessage("false")
	if len(ps) > 1 {
		full = ps[1]
	}
	p, _ := json.Marshal([]json.RawMessage{json.RawMessage(fmt.Sprintf("%q", hexutil.EncodeUint64(h))), full})
	c, ok, err := s.a.lookup("eth_getBlockByNumber", p)
	if err != nil || !ok {
		return nil, nil, false
	}
	return c.Result, c.Error, true
}

func (s *Server) byTxHash(method string, params json.RawMessage) (json.RawMessage, json.RawMessage, bool) {
	if c, ok, err := s.a.lookup(method, params); err == nil && ok {
		return c.Result, c.Error, true
	}
	s.hashOnce.Do(s.buildIndexes)
	hash, _, ok := hashParam(params)
	if !ok || s.indexErr != nil {
		return nil, nil, false
	}
	ref, ok := s.txs[hash]
	if !ok {
		return null, nil, true
	}
	num := json.RawMessage(fmt.Sprintf("%q", hexutil.EncodeUint64(ref.height)))
	var p []byte
	var from string
	if method == "eth_getTransactionReceipt" {
		p, _ = json.Marshal([]json.RawMessage{num})
		from = "eth_getBlockReceipts"
	} else {
		p, _ = json.Marshal([]json.RawMessage{num, json.RawMessage("true")})
		from = "eth_getBlockByNumber"
	}
	c, ok, err := s.a.lookup(from, p)
	if err != nil || !ok {
		return nil, nil, false
	}
	var items []json.RawMessage
	if from == "eth_getBlockReceipts" {
		if json.Unmarshal(c.Result, &items) != nil || ref.index >= len(items) {
			return nil, nil, false
		}
		return items[ref.index], nil, true
	}
	var b struct {
		Transactions []json.RawMessage `json:"transactions"`
	}
	if json.Unmarshal(c.Result, &b) != nil || ref.index >= len(b.Transactions) {
		return nil, nil, false
	}
	return b.Transactions[ref.index], nil, true
}

// ---------------------------------------------------------------------------
// Serving on a local port
// ---------------------------------------------------------------------------

// Endpoint is a handler served on a local port.
type Endpoint struct {
	URL string
	srv *http.Server
}

// Serve starts h on 127.0.0.1 with a free port.
func Serve(h http.Handler) (*Endpoint, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return &Endpoint{URL: "http://" + ln.Addr().String(), srv: srv}, nil
}

// Close stops the server.
func (e *Endpoint) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := e.srv.Shutdown(ctx)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// ParseEndpoint reports whether an RPC endpoint names a replay archive
// ("replay:///path/to/archive" or "replay:path") and returns its directory.
func ParseEndpoint(endpoint string) (string, bool) {
	if !strings.HasPrefix(endpoint, "replay:") {
		return "", false
	}
	dir := strings.TrimPrefix(endpoint, "replay:")
	dir = strings.TrimPrefix(dir, "//")
	return dir, dir != ""
}
