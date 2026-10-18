// Package ethpeer is a bounded ETH/68 history peer, not an execution node.
// RLPx transport is supplied by the existing go-ethereum dependency. No state
// trie, EVM, discovery crawl, transaction pool or consensus engine is started.
package ethpeer

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/window"
)

const Version = 68
const MaxMessage = 10 << 20

var ErrNoData = errors.New("ethpeer: requested input unavailable")
var ErrHistoryGap = errors.New("ethpeer: missing input below peer head")
var ErrClosed = errors.New("ethpeer: closed")
var ErrProtocol = errors.New("ethpeer: incompatible or malformed peer")

type ForkID struct {
	Hash [4]byte
	Next uint64
}
type Config struct {
	Chain        model.FeedChain
	NetworkID    uint64
	Checkpoint   model.FeedBlock   // externally verified bootstrap; never a finalized claim
	ForkID       ForkID            // explicitly audited chain fork ID; exact match is conservative
	Key          *ecdsa.PrivateKey // network identity only; caller persists it if needed
	Peers        []*enode.Node     // explicit authenticated RLPx identities, no default public peers
	ListenAddr   string
	Timeout      time.Duration
	PollInterval time.Duration
	MaxPeers     int
	ServeBlocks  int // bounded reference cache, raw bytes stay in window
}
type status struct {
	Version uint32
	Network uint64
	TD      *big.Int
	Head    common.Hash
	Genesis common.Hash
	Fork    ForkID
}
type query struct {
	Origin  rlp.RawValue
	Amount  uint64
	Skip    uint64
	Reverse bool
}
type headerRequest struct {
	ID    uint64
	Query query
}
type hashRequest struct {
	ID     uint64
	Hashes []common.Hash
}
type packet struct {
	ID    uint64
	Items []rlp.RawValue
}
type response struct {
	raw []byte
	err error
}
type connection struct {
	owner    *Client
	peer     *p2p.Peer
	rw       p2p.MsgReadWriter
	ctx      context.Context
	cancel   context.CancelFunc
	token    chan struct{}
	mu       sync.Mutex
	pending  chan response
	id       uint64
	expected uint64
	failure  error
	head     common.Hash
	tip      uint64
	knownTip bool
}
type cached struct {
	block model.FeedBlock
	refs  map[model.DatasetKind]window.Reference
}
type Client struct {
	cfg        Config
	srv        *p2p.Server
	ctx        context.Context
	cancel     context.CancelFunc
	closeOnce  sync.Once
	mu         sync.Mutex
	peers      map[enode.ID]*connection
	allowed    map[enode.ID]bool
	wake       chan struct{}
	seq        atomic.Uint64
	subscribed atomic.Bool
	raw        *window.Store
	cache      map[common.Hash]*cached
	order      []common.Hash
	byHeight   map[uint64]common.Hash
	localHead  model.FeedBlock
}

func New(cfg Config) (*Client, error) {
	if cfg.Chain.Validate() != nil || cfg.Checkpoint.Validate() != nil || cfg.Checkpoint.Chain != cfg.Chain || cfg.Key == nil || cfg.ForkID.Hash == ([4]byte{}) || cfg.NetworkID == 0 || len(cfg.Peers) == 0 || len(cfg.Peers) > 8 {
		return nil, model.ErrCaptureInvalid
	}
	if cfg.MaxPeers == 0 {
		cfg.MaxPeers = 4
	}
	if cfg.MaxPeers < 1 || cfg.MaxPeers > 8 || len(cfg.Peers) > cfg.MaxPeers {
		return nil, model.ErrCaptureInvalid
	}
	if cfg.ServeBlocks == 0 {
		cfg.ServeBlocks = 1024
	}
	if cfg.ServeBlocks < 1 || cfg.ServeBlocks > 8192 {
		return nil, model.ErrCaptureInvalid
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 100 * time.Millisecond
	}
	if cfg.Timeout < time.Millisecond || cfg.Timeout > time.Minute || cfg.PollInterval < time.Millisecond {
		return nil, model.ErrCaptureInvalid
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:0"
	}
	cfg.Peers = append([]*enode.Node(nil), cfg.Peers...)
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{cfg: cfg, ctx: ctx, cancel: cancel, peers: map[enode.ID]*connection{}, allowed: map[enode.ID]bool{}, wake: make(chan struct{}, 1), cache: map[common.Hash]*cached{}, byHeight: map[uint64]common.Hash{}}
	for _, n := range cfg.Peers {
		if n == nil || c.allowed[n.ID()] {
			cancel()
			return nil, model.ErrCaptureInvalid
		}
		c.allowed[n.ID()] = true
	}
	c.srv = &p2p.Server{Config: p2p.Config{PrivateKey: cfg.Key, Name: "indexer-window/eth68", MaxPeers: cfg.MaxPeers, MaxPendingPeers: cfg.MaxPeers, NoDiscovery: true, DialRatio: 1, StaticNodes: cfg.Peers, ListenAddr: cfg.ListenAddr, Protocols: []p2p.Protocol{{Name: "eth", Version: Version, Length: 17, Run: c.run}}}}
	if err := c.srv.Start(); err != nil {
		cancel()
		return nil, err
	}
	return c, nil
}
func (c *Client) Self() *enode.Node { return c.srv.Self() }
func (c *Client) Close() error      { c.closeOnce.Do(func() { c.cancel(); c.srv.Stop() }); return nil }
func (c *Client) Capabilities(ctx context.Context) (chainfeed.Capabilities, error) {
	if err := ctx.Err(); err != nil {
		return chainfeed.Capabilities{}, err
	}
	return evmdata.Capabilities(c.cfg.Chain), nil
}
func (c *Client) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Client) run(peer *p2p.Peer, rw p2p.MsgReadWriter) (runErr error) {
	if !c.allowed[peer.ID()] {
		return ErrProtocol
	}
	ctx, cancel := context.WithTimeout(c.ctx, c.cfg.Timeout)
	stop := context.AfterFunc(ctx, func() { peer.Disconnect(p2p.DiscReadTimeout) })
	c.mu.Lock()
	head := c.cfg.Checkpoint
	if c.localHead.Hash != (common.Hash{}) {
		head = c.localHead
	}
	c.mu.Unlock()
	err := p2p.Send(rw, 0, status{Version: Version, Network: c.cfg.NetworkID, TD: new(big.Int), Head: head.Hash, Genesis: c.cfg.Chain.GenesisHash, Fork: c.cfg.ForkID})
	var remote status
	if err == nil {
		var msg p2p.Msg
		msg, err = rw.ReadMsg()
		if err == nil {
			if msg.Code != 0 || msg.Size > 4096 {
				err = ErrProtocol
			} else {
				err = msg.Decode(&remote)
			}
			if err == nil && (remote.Version != Version || remote.Network != c.cfg.NetworkID || remote.Genesis != c.cfg.Chain.GenesisHash || remote.Fork != c.cfg.ForkID || remote.Head == (common.Hash{})) {
				err = ErrProtocol
			}
		}
	}
	stop()
	cancel()
	if err != nil {
		return err
	}
	pcCtx, pcCancel := context.WithCancel(c.ctx)
	pc := &connection{owner: c, peer: peer, rw: rw, ctx: pcCtx, cancel: pcCancel, token: make(chan struct{}, 1)}
	pc.head = remote.Head
	pc.token <- struct{}{}
	c.mu.Lock()
	c.peers[peer.ID()] = pc
	c.mu.Unlock()
	c.signal()
	defer func() {
		pc.mu.Lock()
		pc.failure = runErr
		pc.mu.Unlock()
		pcCancel()
		c.mu.Lock()
		delete(c.peers, peer.ID())
		c.mu.Unlock()
		c.signal()
	}()
	for {
		msg, err := rw.ReadMsg()
		if err != nil {
			return err
		}
		if msg.Size > MaxMessage {
			return ErrProtocol
		}
		switch msg.Code {
		case 4, 6, 16:
			data, err := io.ReadAll(io.LimitReader(msg.Payload, int64(msg.Size)+1))
			if err != nil {
				return err
			}
			if len(data) != int(msg.Size) {
				return ErrProtocol
			}
			parts, err := evmdata.Items(data, 2)
			if err != nil || len(parts) != 2 {
				return ErrProtocol
			}
			var id uint64
			if err := rlp.DecodeBytes(parts[0], &id); err != nil {
				return err
			}
			pc.mu.Lock()
			ch := pc.pending
			valid := ch != nil && id == pc.id && msg.Code == pc.expected
			if valid {
				pc.pending = nil
			}
			pc.mu.Unlock()
			if !valid {
				return ErrProtocol
			}
			ch <- response{raw: bytes.Clone(parts[1])}
		case 3, 5, 15, 9:
			if err := c.serve(pc, msg); err != nil {
				return err
			}
		case 1, 2, 7, 8:
			// Announcements/tx gossip are hints only. No unverified payload
			// is propagated and there is no transaction pool or execution.
			if err := msg.Discard(); err != nil {
				return err
			}
		default:
			return ErrProtocol
		}
	}
}

func (pc *connection) call(ctx context.Context, code, want uint64, makeRequest func(uint64) any) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, pc.owner.cfg.Timeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-pc.ctx.Done():
		return nil, pc.ended()
	case <-pc.token:
	}
	defer func() { pc.token <- struct{}{} }()
	id := pc.owner.seq.Add(1)
	if id == 0 {
		return nil, ErrProtocol
	}
	ch := make(chan response, 1)
	pc.mu.Lock()
	pc.id = id
	pc.expected = want
	pc.pending = ch
	pc.mu.Unlock()
	defer func() {
		pc.mu.Lock()
		if pc.pending == ch {
			pc.pending = nil
		}
		pc.mu.Unlock()
	}()
	stop := context.AfterFunc(ctx, func() { pc.peer.Disconnect(p2p.DiscReadTimeout) })
	defer stop()
	if err := p2p.Send(pc.rw, code, makeRequest(id)); err != nil {
		pc.peer.Disconnect(p2p.DiscNetworkError)
		return nil, ErrClosed
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-pc.ctx.Done():
		return nil, pc.ended()
	case r := <-ch:
		items, err := evmdata.Items(r.raw, 1)
		if err != nil {
			return nil, err
		}
		if len(items) != 1 {
			return nil, ErrNoData
		}
		return bytes.Clone(items[0]), r.err
	}
}

func (pc *connection) ended() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if errors.Is(pc.failure, ErrProtocol) {
		return ErrProtocol
	}
	return ErrClosed
}

func (c *Client) peer(ctx context.Context) (*connection, error) {
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.ctx.Err() != nil {
			return nil, ErrClosed
		}
		c.mu.Lock()
		var pc *connection
		for _, p := range c.peers {
			pc = p
			break
		}
		c.mu.Unlock()
		if pc != nil {
			return pc, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.ctx.Done():
			return nil, ErrClosed
		case <-c.wake:
		}
	}
}
func (c *Client) fetch(ctx context.Context, n uint64, limits chainfeed.ReadLimits) ([]chainfeed.Envelope, error) {
	pc, err := c.peer(ctx)
	if err != nil {
		return nil, err
	}
	pc.mu.Lock()
	known := pc.knownTip
	head := pc.head
	pc.mu.Unlock()
	if !known {
		origin, _ := rlp.EncodeToBytes(head)
		h, err := pc.call(ctx, 3, 4, func(id uint64) any { return headerRequest{id, query{Origin: origin, Amount: 1}} })
		if err != nil {
			return nil, err
		}
		fields, err := evmdata.Items(h, 64)
		if err != nil || len(fields) < 15 || crypto.Keccak256Hash(h) != head {
			return nil, ErrProtocol
		}
		var height uint64
		if err := rlp.DecodeBytes(fields[8], &height); err != nil {
			return nil, ErrProtocol
		}
		pc.mu.Lock()
		if height > pc.tip {
			pc.tip = height
		}
		pc.knownTip = true
		pc.mu.Unlock()
	}
	origin, _ := rlp.EncodeToBytes(n)
	header, err := pc.call(ctx, 3, 4, func(id uint64) any { return headerRequest{id, query{Origin: origin, Amount: 1}} })
	if err != nil {
		if errors.Is(err, ErrNoData) {
			pc.mu.Lock()
			old := n <= pc.tip
			pc.mu.Unlock()
			if old {
				return nil, ErrHistoryGap
			}
		}
		return nil, err
	}
	if len(header) > 64<<10 {
		return nil, ErrProtocol
	}
	fields, err := evmdata.Items(header, 64)
	if err != nil || len(fields) < 15 {
		return nil, ErrProtocol
	}
	var height uint64
	if err := rlp.DecodeBytes(fields[8], &height); err != nil || height != n {
		return nil, ErrProtocol
	}
	hash := crypto.Keccak256Hash(header)
	pc.mu.Lock()
	if n > pc.tip {
		pc.tip = n
	}
	pc.mu.Unlock()
	body, err := pc.call(ctx, 5, 6, func(id uint64) any { return hashRequest{id, []common.Hash{hash}} })
	if err != nil {
		if errors.Is(err, ErrNoData) {
			return nil, ErrHistoryGap
		}
		return nil, err
	}
	receipts, err := pc.call(ctx, 15, 16, func(id uint64) any { return hashRequest{id, []common.Hash{hash}} })
	if err != nil {
		if errors.Is(err, ErrNoData) {
			return nil, ErrHistoryGap
		}
		return nil, err
	}
	values := [][]byte{header, body, receipts}
	kinds := []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts}
	var out []chainfeed.Envelope
	for i, k := range kinds {
		e := chainfeed.Envelope{Block: model.FeedBlock{Chain: c.cfg.Chain, Number: n, Hash: hash}, Kind: k, Codec: evmdata.Codec(k), Payload: values[i]}
		if err := e.Validate(limits); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

type stream struct {
	client    *Client
	ctx       context.Context
	cancel    context.CancelFunc
	next, end uint64
	finite    bool
	limits    chainfeed.ReadLimits
	pending   []chainfeed.Envelope
}

func (c *Client) Subscribe(ctx context.Context, after model.FeedBlock, limits chainfeed.ReadLimits) (chainfeed.Stream, error) {
	if after.Validate() != nil || after.Chain != c.cfg.Chain || after.Number == math.MaxUint64 || limits.Validate() != nil || limits.MaxPayloadBytes > 8<<20 {
		return nil, model.ErrCaptureInvalid
	}
	if !c.subscribed.CompareAndSwap(false, true) {
		return nil, errors.New("ethpeer: one live stream per adapter")
	}
	s := c.newStream(ctx, after.Number+1, 0, false, limits)
	return s, nil
}
func (c *Client) newStream(ctx context.Context, from, to uint64, finite bool, limits chainfeed.ReadLimits) *stream {
	x, cancel := context.WithCancel(ctx)
	return &stream{client: c, ctx: x, cancel: cancel, next: from, end: to, finite: finite, limits: limits}
}
func (c *Client) FetchRange(ctx context.Context, chain model.FeedChain, from, to uint64, limits chainfeed.ReadLimits) (chainfeed.Stream, error) {
	if chain != c.cfg.Chain || from > to || to == math.MaxUint64 || to-from > 4095 || limits.Validate() != nil || limits.MaxPayloadBytes > 8<<20 {
		return nil, model.ErrCaptureInvalid
	}
	return c.newStream(ctx, from, to, true, limits), nil
}
func (c *Client) FetchDataset(ctx context.Context, b model.FeedBlock, r model.DatasetRequirement, limits chainfeed.ReadLimits) (chainfeed.Envelope, error) {
	if limits.Validate() != nil || limits.MaxPayloadBytes > 8<<20 {
		return chainfeed.Envelope{}, model.ErrCaptureInvalid
	}
	if b.Validate() != nil || b.Chain != c.cfg.Chain || r.Coverage.Scope != model.CoverageComplete || r.Coverage.Validate() != nil || evmdata.Codec(r.Kind) == "" {
		return chainfeed.Envelope{}, chainfeed.ErrUnsupported
	}
	es, err := c.fetch(ctx, b.Number, limits)
	if err != nil {
		return chainfeed.Envelope{}, err
	}
	for _, e := range es {
		if e.Kind == r.Kind && e.Block == b {
			return e, nil
		}
	}
	return chainfeed.Envelope{}, ErrProtocol
}
func (s *stream) Close() error { s.cancel(); return nil }

// fetch already bounds and verifies all three execution datasets together.
// Expose their existing owned bytes without more network requests or copies.
func (s *stream) NextBatch(ctx context.Context) ([]chainfeed.Envelope, error) {
	first, err := s.Next(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]chainfeed.Envelope, 1, 1+len(s.pending))
	out[0] = first
	out = append(out, s.pending...)
	clear(s.pending)
	s.pending = nil
	return out, nil
}

func (s *stream) Next(ctx context.Context) (chainfeed.Envelope, error) {
	if err := ctx.Err(); err != nil {
		return chainfeed.Envelope{}, err
	}
	if s.client.ctx.Err() != nil {
		return chainfeed.Envelope{}, ErrClosed
	}
	if s.ctx.Err() != nil {
		return chainfeed.Envelope{}, s.ctx.Err()
	}
	if len(s.pending) == 0 {
		if s.finite && s.next > s.end {
			return chainfeed.Envelope{}, io.EOF
		}
		if s.next == math.MaxUint64 {
			return chainfeed.Envelope{}, model.ErrCaptureInvalid
		}
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		stop := context.AfterFunc(s.ctx, cancel)
		defer stop()
		for {
			values, err := s.client.fetch(ctx, s.next, s.limits)
			if err == nil {
				s.pending = values
				s.next++
				break
			}
			retry := errors.Is(err, ErrNoData) || errors.Is(err, ErrClosed) || (errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil)
			if !retry {
				return chainfeed.Envelope{}, err
			}
			if s.finite {
				return chainfeed.Envelope{}, err
			}
			t := time.NewTimer(s.client.cfg.PollInterval)
			select {
			case <-ctx.Done():
				t.Stop()
				return chainfeed.Envelope{}, ctx.Err()
			case <-s.client.ctx.Done():
				t.Stop()
				return chainfeed.Envelope{}, ErrClosed
			case <-t.C:
			}
		}
	}
	e := s.pending[0]
	s.pending[0] = chainfeed.Envelope{}
	s.pending = s.pending[1:]
	return e, nil
}

// ObserveCaptured is called by the trusted pipeline AFTER verified facts commit.
// A bounded recent-reference index owns no raw payload cache. Pending raw inputs
// must never be published to other peers through this composition API.
func (c *Client) ObserveCaptured(raw *window.Store, refs []window.Reference) error {
	if len(refs) != 3 {
		return model.ErrCaptureIncomplete
	}
	for _, ref := range refs {
		if ref.Block != refs[0].Block {
			return model.ErrCaptureInvalid
		}
		if err := c.observe(raw, ref); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.localHead = refs[0].Block
	c.mu.Unlock()
	return nil
}
func (c *Client) observe(raw *window.Store, ref window.Reference) error {
	if raw == nil || ref.Block.Chain != c.cfg.Chain || evmdata.Codec(ref.Kind) == "" || ref.Codec != evmdata.Codec(ref.Kind) {
		return model.ErrCaptureInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.raw != nil && c.raw != raw {
		return ErrProtocol
	}
	c.raw = raw
	v := c.cache[ref.Block.Hash]
	if v == nil {
		if len(c.order) >= c.cfg.ServeBlocks {
			old := c.order[0]
			c.order = c.order[1:]
			delete(c.byHeight, c.cache[old].block.Number)
			delete(c.cache, old)
		}
		v = &cached{block: ref.Block, refs: map[model.DatasetKind]window.Reference{}}
		c.cache[ref.Block.Hash] = v
		c.order = append(c.order, ref.Block.Hash)
		c.byHeight[ref.Block.Number] = ref.Block.Hash
	}
	if v.block != ref.Block {
		return ErrProtocol
	}
	v.refs[ref.Kind] = ref
	return nil
}
func (c *Client) local(ctx context.Context, hash common.Hash, number *uint64, k model.DatasetKind) (rlp.RawValue, error) {
	c.mu.Lock()
	if number != nil {
		hash = c.byHeight[*number]
	}
	entry := c.cache[hash]
	raw := c.raw
	var ref window.Reference
	if entry != nil {
		ref = entry.refs[k]
	}
	c.mu.Unlock()
	if ref.StoreID == (common.Hash{}) || raw == nil {
		return nil, nil
	}
	l, err := raw.Acquire(ctx, ref)
	if errors.Is(err, window.ErrNotFound) || errors.Is(err, window.ErrBusy) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e, err := l.Envelope()
	value := bytes.Clone(e.Payload)
	err = errors.Join(err, l.Close())
	return value, err
}
func (c *Client) serve(pc *connection, msg p2p.Msg) error {
	if msg.Size > 64<<10 {
		return ErrProtocol
	}
	data, err := io.ReadAll(msg.Payload)
	if err != nil {
		return err
	}
	parts, err := evmdata.Items(data, 2)
	if err != nil || len(parts) != 2 {
		return ErrProtocol
	}
	var id uint64
	if err := rlp.DecodeBytes(parts[0], &id); err != nil {
		return err
	}
	result := packet{ID: id}
	code := uint64(10)
	ctx, cancel := context.WithTimeout(pc.ctx, c.cfg.Timeout)
	defer cancel()
	if msg.Code == 3 {
		var q query
		if err := rlp.DecodeBytes(parts[1], &q); err != nil {
			return err
		}
		if q.Amount > 128 || q.Skip != 0 || q.Reverse {
			return ErrProtocol
		}
		var hash common.Hash
		var number uint64
		var np *uint64
		_, value, _, err := rlp.Split(q.Origin)
		if err != nil {
			return err
		}
		if len(value) == 32 {
			if err := rlp.DecodeBytes(q.Origin, &hash); err != nil {
				return err
			}
		} else {
			if err := rlp.DecodeBytes(q.Origin, &number); err != nil {
				return err
			}
			np = &number
		}
		// A bounded recent index may provide fewer headers than requested.
		if q.Amount > 0 {
			v, err := c.local(ctx, hash, np, model.DatasetHeaders)
			if err != nil {
				return err
			}
			if len(v) > 0 {
				result.Items = append(result.Items, v)
			}
		}
		code = 4
	} else if msg.Code == 5 || msg.Code == 15 {
		items, err := evmdata.Items(parts[1], 128)
		if err != nil {
			return err
		}
		kind := model.DatasetBodies
		code = 6
		if msg.Code == 15 {
			kind = model.DatasetReceipts
			code = 16
		}
		bytesSent := 0
		for _, item := range items {
			var hash common.Hash
			if err := rlp.DecodeBytes(item, &hash); err != nil {
				return err
			}
			v, err := c.local(ctx, hash, nil, kind)
			if err != nil {
				return err
			}
			if len(v) > 0 {
				if bytesSent > 0 && bytesSent+len(v) > 2<<20 {
					break
				}
				result.Items = append(result.Items, v)
				bytesSent += len(v)
			}
		}
	} else if msg.Code == 9 {
		if _, err := evmdata.Items(parts[1], 128); err != nil {
			return err
		}
	}
	stop := context.AfterFunc(ctx, func() { pc.peer.Disconnect(p2p.DiscReadTimeout) })
	defer stop()
	if err := p2p.Send(pc.rw, code, result); err != nil {
		return fmt.Errorf("ethpeer: reply: %w", err)
	}
	return nil
}
