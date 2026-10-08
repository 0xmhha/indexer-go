package testchain

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

// DefaultClientVersion makes adapter detection pick the generic EVM adapter.
const DefaultClientVersion = "Geth/v1.16.5-stable/linux-amd64/go1.24.9"

// Server serves a Chain over JSON-RPC (HTTP).
type Server struct {
	chain         *Chain
	srv           *httptest.Server
	clientVersion string

	mu         sync.Mutex
	calls      map[string]int
	unknown    map[string]int
	blockLoads map[uint64]int // eth_getBlockByNumber with an explicit number
	disabled   map[string]bool
}

// NewServer starts an HTTP JSON-RPC server for chain. Close it when done.
func NewServer(chain *Chain) *Server {
	s := &Server{
		chain:         chain,
		clientVersion: DefaultClientVersion,
		calls:         map[string]int{},
		unknown:       map[string]int{},
		blockLoads:    map[uint64]int{},
		disabled:      map[string]bool{},
	}
	if chain.StableNet() {
		s.clientVersion = StableNetClientVersion
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serveHTTP))
	return s
}

// URL is the RPC endpoint.
func (s *Server) URL() string { return s.srv.URL }

// Close stops the server.
func (s *Server) Close() { s.srv.Close() }

// SetClientVersion changes the web3_clientVersion answer (adapter detection).
func (s *Server) SetClientVersion(v string) { s.clientVersion = v }

// DisableMethod makes the server answer method as "not available", like a
// node that does not implement it.
func (s *Server) DisableMethod(method string) {
	s.mu.Lock()
	s.disabled[method] = true
	s.mu.Unlock()
}

// EnableMethod answers a method DisableMethod turned off again.
func (s *Server) EnableMethod(method string) {
	s.mu.Lock()
	delete(s.disabled, method)
	s.mu.Unlock()
}

// UnknownMethods lists methods that were called but are not implemented.
// Tests can assert it is empty to notice new RPC dependencies.
func (s *Server) UnknownMethods() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.unknown))
	for m := range s.unknown {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// BlockLoads returns how many times each block was requested by number
// (block tags such as "latest" are not counted). Reprocessing shows up as
// counts above one.
func (s *Server) BlockLoads() map[uint64]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[uint64]int, len(s.blockLoads))
	for k, v := range s.blockLoads {
		out[k] = v
	}
	return out
}

// ResetBlockLoads clears the per-block request counters.
func (s *Server) ResetBlockLoads() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blockLoads = map[uint64]int{}
}

// Calls returns how many times each method was called.
func (s *Server) Calls() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.calls))
	for k, v := range s.calls {
		out[k] = v
	}
	return out
}

type rpcRequest struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	var body bytes.Buffer
	if _, err := body.ReadFrom(r.Body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	raw := bytes.TrimSpace(body.Bytes())
	if len(raw) > 0 && raw[0] == '[' {
		var reqs []rpcRequest
		if err := json.Unmarshal(raw, &reqs); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resps := make([]rpcResponse, len(reqs))
		for i, req := range reqs {
			resps[i] = s.handle(req)
		}
		_ = json.NewEncoder(w).Encode(resps)
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(s.handle(req))
}

func (s *Server) handle(req rpcRequest) rpcResponse {
	s.mu.Lock()
	s.calls[req.Method]++
	disabled := s.disabled[req.Method]
	s.mu.Unlock()

	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	if disabled {
		resp.Error = errNotFound
		return resp
	}
	result, err := s.dispatch(req)
	if err != nil {
		resp.Error = err
		return resp
	}
	if result == nil {
		// JSON-RPC null result must still be present.
		resp.Result = json.RawMessage("null")
	} else {
		resp.Result = result
	}
	return resp
}

var errNotFound = &rpcError{Code: -32601, Message: "the method does not exist/is not available"}

func (s *Server) dispatch(req rpcRequest) (any, *rpcError) {
	c := s.chain
	c.mu.RLock()
	defer c.mu.RUnlock()

	switch req.Method {
	case "web3_clientVersion":
		return s.clientVersion, nil
	case "eth_chainId":
		return (*hexutil.Big)(c.chainID), nil
	case "net_version":
		return c.chainID.String(), nil
	case "eth_syncing":
		return false, nil
	case "eth_blockNumber":
		return hexutil.Uint64(c.head), nil
	case "eth_gasPrice":
		return (*hexutil.Big)(big.NewInt(1_000_000_000)), nil

	case "eth_getBlockByNumber":
		if c.finalizedError && isFinalityTag(req, 0) {
			return nil, &rpcError{Code: -32000, Message: "finalized block not found"}
		}
		n, ok := s.blockNumberParam(req, 0)
		if !ok {
			return nil, nil
		}
		if raw, ok := c.raw[n]; ok {
			return raw.Block, nil
		}
		b := c.blockAt(n)
		if b == nil {
			return nil, nil
		}
		if isHexNumber(req, 0) {
			s.mu.Lock()
			s.blockLoads[n]++
			s.mu.Unlock()
		}
		return marshalBlock(b, boolParam(req, 1)), nil
	case "eth_getBlockByHash":
		for _, raw := range c.raw {
			if rawHash(raw) == hashParam(req, 0) {
				return raw.Block, nil
			}
		}
		b := c.byHash[hashParam(req, 0)]
		if b == nil || b.Block.NumberU64() > c.head {
			return nil, nil
		}
		return marshalBlock(b, boolParam(req, 1)), nil

	case "eth_getLogs":
		return s.getLogs(req)

	case "eth_getBlockReceipts":
		if raw, ok := s.rawBlockParam(req, 0); ok {
			return raw.Receipts, nil
		}
		b := s.blockParamNumberOrHash(req, 0)
		if b == nil {
			return nil, nil
		}
		out := make([]map[string]any, len(b.Receipts))
		for i := range b.Receipts {
			out[i] = marshalReceipt(b, i)
		}
		return out, nil
	case "eth_getTransactionReceipt":
		loc, ok := c.txIndex[hashParam(req, 0)]
		if !ok || loc.block.Block.NumberU64() > c.head {
			return nil, nil
		}
		return marshalReceipt(loc.block, loc.index), nil
	case "eth_getTransactionByHash":
		loc, ok := c.txIndex[hashParam(req, 0)]
		if !ok || loc.block.Block.NumberU64() > c.head {
			return nil, nil
		}
		return marshalTx(loc.block, loc.index), nil

	case "eth_getBalance":
		var addr common.Address
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params[0], &addr)
		}
		n, ok := s.blockNumberParam(req, 1)
		if !ok {
			n = c.head
		}
		return (*hexutil.Big)(c.balanceAt(addr, n)), nil
	case "eth_getCode":
		var addr common.Address
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params[0], &addr)
		}
		return hexutil.Bytes(c.code[addr]), nil

	case "eth_call":
		var call struct {
			To    *common.Address `json:"to"`
			Data  hexutil.Bytes   `json:"data"`
			Input hexutil.Bytes   `json:"input"` // go-ethereum clients send the call data here
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params[0], &call)
		}
		if len(call.Input) > 0 {
			call.Data = call.Input
		}
		if call.To == nil || len(call.Data) < 4 {
			return nil, &rpcError{Code: 3, Message: "execution reverted"}
		}
		mock := c.contracts[*call.To]
		if out, ok := mock[hex.EncodeToString(call.Data)]; ok {
			return hexutil.Bytes(out), nil
		}
		if out, ok := mock[hex.EncodeToString(call.Data[:4])]; ok {
			return hexutil.Bytes(out), nil
		}
		return nil, &rpcError{Code: 3, Message: "execution reverted"}
	}

	// Capability probes (debug tracing, anvil cheat codes) are answered as
	// "not available", which is what a plain production node does.
	if strings.HasPrefix(req.Method, "debug_") || strings.HasPrefix(req.Method, "anvil_") {
		return nil, errNotFound
	}
	s.mu.Lock()
	s.unknown[req.Method]++
	s.mu.Unlock()
	return nil, errNotFound
}

// getLogs answers eth_getLogs for a block range (blockHash filters are not
// supported): logs of visible blocks whose address is one of the filter's
// and whose topics match position by position (null matches any topic, a
// list any of its topics).
func (s *Server) getLogs(req rpcRequest) (any, *rpcError) {
	c := s.chain
	var f struct {
		FromBlock string            `json:"fromBlock"`
		ToBlock   string            `json:"toBlock"`
		Address   json.RawMessage   `json:"address"`
		Topics    []json.RawMessage `json:"topics"`
	}
	if len(req.Params) == 0 || json.Unmarshal(req.Params[0], &f) != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid filter"}
	}
	bound := func(tag string, def uint64) (uint64, bool) {
		switch tag {
		case "", "latest", "pending", "safe", "finalized":
			return def, true
		case "earliest":
			return 0, true
		}
		n, err := hexutil.DecodeUint64(tag)
		return n, err == nil
	}
	from, ok1 := bound(f.FromBlock, c.head)
	to, ok2 := bound(f.ToBlock, c.head)
	if !ok1 || !ok2 {
		return nil, &rpcError{Code: -32602, Message: "invalid block range"}
	}
	var addresses []common.Address
	if len(f.Address) > 0 && string(f.Address) != "null" {
		var one common.Address
		if json.Unmarshal(f.Address, &one) == nil {
			addresses = []common.Address{one}
		} else if json.Unmarshal(f.Address, &addresses) != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid address"}
		}
	}
	topics := make([][]common.Hash, len(f.Topics))
	for i, raw := range f.Topics {
		if string(raw) == "null" {
			continue
		}
		var one common.Hash
		if json.Unmarshal(raw, &one) == nil {
			topics[i] = []common.Hash{one}
		} else if json.Unmarshal(raw, &topics[i]) != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid topics"}
		}
	}
	matches := func(l *types.Log) bool {
		if len(addresses) > 0 && !slices.Contains(addresses, l.Address) {
			return false
		}
		for i, want := range topics {
			if len(want) == 0 {
				continue
			}
			if i >= len(l.Topics) || !slices.Contains(want, l.Topics[i]) {
				return false
			}
		}
		return true
	}
	out := []*types.Log{}
	for n := from; n <= min(to, c.head); n++ {
		b := c.blockAt(n)
		if b == nil {
			break
		}
		for _, r := range b.Receipts {
			for _, l := range r.Logs {
				if matches(l) {
					out = append(out, l)
				}
			}
		}
	}
	return out, nil
}

// blockNumberParam resolves a block tag or hex number. ok is false when the
// requested block is not visible.
func (s *Server) blockNumberParam(req rpcRequest, i int) (uint64, bool) {
	c := s.chain
	if len(req.Params) <= i {
		return c.head, true
	}
	var tag string
	if err := json.Unmarshal(req.Params[i], &tag); err != nil {
		return 0, false
	}
	switch tag {
	case "latest", "pending":
		return c.head, true
	case "safe", "finalized":
		switch {
		case c.noFinalized:
			return 0, false
		case c.finalized != nil && *c.finalized < c.head:
			return *c.finalized, true
		}
		return c.head, true
	case "earliest":
		return 0, true
	}
	n, err := hexutil.DecodeUint64(tag)
	if err != nil {
		return 0, false
	}
	return n, n <= c.head
}

// rawBlockParam returns the recorded block a block number or hash
// parameter names.
func (s *Server) rawBlockParam(req rpcRequest, i int) (RawBlock, bool) {
	c := s.chain
	if len(c.raw) == 0 || len(req.Params) <= i {
		return RawBlock{}, false
	}
	var arg string
	if json.Unmarshal(req.Params[i], &arg) != nil {
		return RawBlock{}, false
	}
	if n, err := hexutil.DecodeUint64(arg); err == nil {
		raw, ok := c.raw[n]
		return raw, ok
	}
	for _, raw := range c.raw {
		if rawHash(raw) == common.HexToHash(arg) {
			return raw, true
		}
	}
	return RawBlock{}, false
}

func (s *Server) blockParamNumberOrHash(req rpcRequest, i int) *Block {
	c := s.chain
	if len(req.Params) <= i {
		return nil
	}
	var str string
	if err := json.Unmarshal(req.Params[i], &str); err != nil {
		var obj struct {
			BlockHash   *common.Hash    `json:"blockHash"`
			BlockNumber *hexutil.Uint64 `json:"blockNumber"`
		}
		if err := json.Unmarshal(req.Params[i], &obj); err != nil {
			return nil
		}
		if obj.BlockHash != nil {
			b := c.byHash[*obj.BlockHash]
			if b != nil && b.Block.NumberU64() <= c.head {
				return b
			}
			return nil
		}
		if obj.BlockNumber != nil {
			return c.blockAt(uint64(*obj.BlockNumber))
		}
		return nil
	}
	if strings.HasPrefix(str, "0x") && len(str) == 66 {
		b := c.byHash[common.HexToHash(str)]
		if b != nil && b.Block.NumberU64() <= c.head {
			return b
		}
		return nil
	}
	n, ok := s.blockNumberParam(req, i)
	if !ok {
		return nil
	}
	return c.blockAt(n)
}

func isFinalityTag(req rpcRequest, i int) bool {
	if len(req.Params) <= i {
		return false
	}
	var tag string
	return json.Unmarshal(req.Params[i], &tag) == nil && (tag == "finalized" || tag == "safe")
}

func isHexNumber(req rpcRequest, i int) bool {
	var tag string
	if len(req.Params) <= i || json.Unmarshal(req.Params[i], &tag) != nil {
		return false
	}
	return strings.HasPrefix(tag, "0x")
}

func hashParam(req rpcRequest, i int) common.Hash {
	var h common.Hash
	if len(req.Params) > i {
		_ = json.Unmarshal(req.Params[i], &h)
	}
	return h
}

func boolParam(req rpcRequest, i int) bool {
	var v bool
	if len(req.Params) > i {
		_ = json.Unmarshal(req.Params[i], &v)
	}
	return v
}

// --- JSON encoding -----------------------------------------------------------

func toMap(v json.Marshaler) map[string]any {
	raw, err := v.MarshalJSON()
	if err != nil {
		panic(fmt.Sprintf("testchain: marshal: %v", err))
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		panic(fmt.Sprintf("testchain: unmarshal: %v", err))
	}
	return m
}

func marshalBlock(b *Block, fullTx bool) map[string]any {
	m := toMap(b.Block.Header())
	m["hash"] = b.Block.Hash()
	m["uncles"] = []common.Hash{}
	m["size"] = hexutil.Uint64(b.Block.Size())
	txs := make([]any, len(b.Block.Transactions()))
	for i, tx := range b.Block.Transactions() {
		if fullTx {
			txs[i] = marshalTx(b, i)
		} else {
			txs[i] = tx.Hash()
		}
	}
	m["transactions"] = txs
	return m
}

func marshalTx(b *Block, i int) map[string]any {
	tx := b.Block.Transactions()[i]
	m := toMap(tx)
	from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		panic(fmt.Sprintf("testchain: sender: %v", err))
	}
	m["from"] = from
	m["blockHash"] = b.Block.Hash()
	m["blockNumber"] = (*hexutil.Big)(b.Block.Number())
	m["transactionIndex"] = hexutil.Uint64(i)
	if _, ok := m["gasPrice"]; !ok {
		m["gasPrice"] = (*hexutil.Big)(tx.GasPrice())
	}
	return m
}

func marshalReceipt(b *Block, i int) map[string]any {
	r := b.Receipts[i]
	tx := b.Block.Transactions()[i]
	m := toMap(r)
	from, _ := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
	m["from"] = from
	if tx.To() != nil {
		m["to"] = *tx.To()
	} else {
		m["to"] = nil
	}
	if r.ContractAddress == (common.Address{}) {
		m["contractAddress"] = nil
	}
	return m
}
