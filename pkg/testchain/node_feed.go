package testchain

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/pkg/nodefeed"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rlp"
)

// NodeFeedServer is an offline protocol double. It is not an ExEx/full node.
type NodeFeedServer struct {
	chain       *Chain
	listener    net.Listener
	dir         string
	mu          sync.Mutex
	closed      bool
	conns       map[net.Conn]bool
	session     string
	frameLength uint32
	requests    []nodefeed.Request
	hook        func(nodefeed.Request, *nodefeed.Response)
	wg          sync.WaitGroup
}

func NewNodeFeedServer(chain *Chain) (*NodeFeedServer, error) {
	dir, err := os.MkdirTemp("/tmp", "inf-")
	if err != nil {
		return nil, err
	}
	l, err := net.Listen("unix", filepath.Join(dir, "feed.sock"))
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	s := &NodeFeedServer{chain: chain, dir: dir, listener: l, conns: make(map[net.Conn]bool)}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}
func (s *NodeFeedServer) Socket() string { return filepath.Join(s.dir, "feed.sock") }
func (s *NodeFeedServer) SetHook(hook func(nodefeed.Request, *nodefeed.Response)) {
	s.mu.Lock()
	s.hook = hook
	s.mu.Unlock()
}
func (s *NodeFeedServer) ResetSession()           { s.mu.Lock(); s.session = ""; s.mu.Unlock() }
func (s *NodeFeedServer) SetFrameLength(n uint32) { s.mu.Lock(); s.frameLength = n; s.mu.Unlock() }
func (s *NodeFeedServer) Requests() []nodefeed.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]nodefeed.Request(nil), s.requests...)
}
func (s *NodeFeedServer) Close() {
	s.mu.Lock()
	s.closed = true
	_ = s.listener.Close()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	_ = os.RemoveAll(s.dir)
}
func (s *NodeFeedServer) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.conns[conn] = true
		s.wg.Add(1)
		s.mu.Unlock()
		go s.handle(conn)
	}
}
func (s *NodeFeedServer) handle(conn net.Conn) {
	defer s.wg.Done()
	defer func() { _ = conn.Close(); s.mu.Lock(); delete(s.conns, conn); s.mu.Unlock() }()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	var length [4]byte
	if _, err := io.ReadFull(conn, length[:]); err != nil {
		return
	}
	n := binary.BigEndian.Uint32(length[:])
	if n == 0 || n > 64<<10 {
		return
	}
	raw := make([]byte, n)
	if _, err := io.ReadFull(conn, raw); err != nil {
		return
	}
	var req nodefeed.Request
	if json.Unmarshal(raw, &req) != nil {
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	hook := s.hook
	frameLength := s.frameLength
	validSession := req.Session == s.session
	if req.Kind == "hello" && req.Hash == s.chain.Block(0).Block.Hash() {
		s.session = req.Session
		validSession = true
	}
	s.mu.Unlock()
	head := s.chain.Head()
	response := nodefeed.Response{Version: nodefeed.Version, ChainID: s.chain.chainID.Uint64()}
	switch req.Kind {
	case "hello", "head":
		response.Height = &head
	case "block":
		b, err := s.Bundle(req.Height)
		if err != nil {
			response.Error = &nodefeed.FeedError{Code: "not_found", Message: err.Error()}
		} else {
			response.Block = b
		}
	case "balance":
		var at uint64
		found := false
		for height := uint64(0); height <= head; height++ {
			if s.chain.Block(height).Block.Hash() == req.Hash {
				at, found = height, true
				break
			}
		}
		if !found {
			response.Error = &nodefeed.FeedError{Code: "state_unavailable", Message: "missing hash-bound state"}
			break
		}
		for _, addr := range req.Addresses {
			value := s.chain.BalanceAt(addr, at)
			response.Balances = append(response.Balances, (*hexutil.Big)(value))
		}
	case "ack":
	default:
		response.Error = &nodefeed.FeedError{Code: "unknown", Message: req.Kind}
	}
	if !validSession {
		response.Error = &nodefeed.FeedError{Code: "inactive_session", Message: "inactive session: hello required"}
	}
	if req.Kind == "hello" && req.Hash != s.chain.Block(0).Block.Hash() {
		response.Error = &nodefeed.FeedError{Code: "genesis", Message: "genesis mismatch"}
	}
	if hook != nil {
		hook(req, &response)
	}
	raw, err := json.Marshal(response)
	if err != nil {
		return
	}
	if frameLength != 0 && req.Kind != "hello" {
		binary.BigEndian.PutUint32(length[:], frameLength)
		_, _ = conn.Write(length[:])
		return
	}
	binary.BigEndian.PutUint32(length[:], uint32(len(raw)))
	_, _ = conn.Write(append(length[:], raw...))
}
func (s *NodeFeedServer) Bundle(height uint64) (*nodefeed.Bundle, error) {
	if height > s.chain.Head() {
		return nil, fmt.Errorf("block not found: %d", height)
	}
	b := s.chain.Block(height)
	header, err := rlp.EncodeToBytes(b.Block.Header())
	if err != nil {
		return nil, err
	}
	body, err := rlp.EncodeToBytes(b.Block.Body())
	if err != nil {
		return nil, err
	}
	receipts, err := rlp.EncodeToBytes(b.Receipts)
	if err != nil {
		return nil, err
	}
	return &nodefeed.Bundle{Height: height, Hash: b.Block.Hash(), ParentHash: b.Block.ParentHash(), Header: header, Body: body, Receipts: receipts}, nil
}

// ExportNodeFeedFixture freezes blocks and balance oracles for the Rust
// producer's offline fixture executable, allowing cross-language checks.
func (s *NodeFeedServer) ExportNodeFeedFixture() ([]byte, error) {
	var fixture struct {
		ChainID uint64                                          `json:"chain_id"`
		Blocks  []*nodefeed.Bundle                              `json:"blocks"`
		States  map[common.Hash]map[common.Address]*hexutil.Big `json:"states"`
	}
	fixture.ChainID = s.chain.chainID.Uint64()
	fixture.States = make(map[common.Hash]map[common.Address]*hexutil.Big)
	for n := uint64(0); n <= s.chain.Head(); n++ {
		b, err := s.Bundle(n)
		if err != nil {
			return nil, err
		}
		fixture.Blocks = append(fixture.Blocks, b)
		state := make(map[common.Address]*hexutil.Big)
		for _, addr := range s.chain.Accounts() {
			state[addr] = (*hexutil.Big)(s.chain.BalanceAt(addr, n))
		}
		fixture.States[b.Hash] = state
	}
	return json.Marshal(fixture)
}
