// Package nodefeed reads execution data from a node-local ExEx over a Unix
// socket. Canonical checks remain fresh calls to the separately configured RPC.
package nodefeed

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	protocol "github.com/0xmhha/indexer-go/pkg/nodefeed"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

const Version = protocol.Version
const MaxFrame = protocol.MaxFrame

type Request = protocol.Request
type Response = protocol.Response
type Bundle = protocol.Bundle
type FeedError = protocol.FeedError

type Config struct {
	Consumer string
	Socket   string
	ChainID  uint64
	Timeout  time.Duration
}
type Source struct {
	cfg         Config
	profile     chains.BinaryProfile
	canonical   source.CanonicalReader
	session     string
	genesis     common.Hash
	handshakeMu sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	closed      bool
	active      sync.WaitGroup
}

// New checks both chain identity and genesis before using the local feed.
func New(ctx context.Context, cfg Config, canonical source.Source) (*Source, error) {
	if !filepath.IsAbs(cfg.Socket) || canonical == nil {
		return nil, errors.New("nodefeed: an absolute Unix socket and canonical source are required")
	}
	profile, ok := canonical.Profile().(chains.BinaryProfile)
	if !ok {
		return nil, errors.New("nodefeed: profile cannot decode consensus blocks")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	var nonce [16]byte
	if cfg.Consumer != "" {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", cfg.ChainID, cfg.Consumer)))
		copy(nonce[:], digest[:16])
	} else if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	root, cancel := context.WithCancel(context.Background())
	s := &Source{cfg: cfg, profile: profile, canonical: canonical, session: hex.EncodeToString(nonce[:]), ctx: root, cancel: cancel}
	genesis, err := s.HashAt(ctx, 0)
	if err == nil {
		s.genesis = genesis
		_, err = s.call(ctx, Request{Kind: "hello", Hash: genesis})
	}
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("nodefeed: handshake: %w", err)
	}
	return s, nil
}

func (s *Source) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.active.Wait()
}
func (s *Source) Profile() chains.Profile { return s.profile }
func (s *Source) HashAt(ctx context.Context, height uint64) (common.Hash, error) {
	// Never answer from a feed packet or cached execution notification.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return common.Hash{}, errors.New("nodefeed: closed")
	}
	s.active.Add(1)
	s.mu.Unlock()
	defer s.active.Done()
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	hash, err := s.canonical.HashAt(ctx, height)
	if err != nil && ctx.Err() != nil {
		return common.Hash{}, ctx.Err()
	}
	return hash, err
}
func (s *Source) Head(ctx context.Context) (uint64, error) {
	r, err := s.call(ctx, Request{Kind: "head"})
	if err != nil {
		return 0, err
	}
	if r.Height == nil {
		return 0, errors.New("nodefeed: missing head")
	}
	return *r.Height, nil
}
func (s *Source) Block(ctx context.Context, height uint64) (*model.Block, error) {
	b, _, err := s.BlockWithReceipts(ctx, height)
	return b, err
}
func (s *Source) BlockWithReceipts(ctx context.Context, height uint64) (*model.Block, []*model.Receipt, error) {
	var r Response
	var err error
	for {
		r, err = s.call(ctx, Request{Kind: "block", Height: height})
		var backpressure *FeedError
		if !errors.As(err, &backpressure) || backpressure.Code != "backpressure" {
			break
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-s.ctx.Done():
			timer.Stop()
			return nil, nil, errors.New("nodefeed: closed")
		case <-timer.C:
		}
	}
	if err != nil {
		return nil, nil, err
	}
	if r.Block == nil || r.Block.Height != height {
		return nil, nil, errors.New("nodefeed: missing or wrong block height")
	}
	b, err := s.profile.DecodeBlockRLP(r.Block.Header, r.Block.Body)
	if err != nil {
		return nil, nil, err
	}
	if b.Number != height || b.Hash != r.Block.Hash || b.ParentHash != r.Block.ParentHash {
		return nil, nil, errors.New("nodefeed: inconsistent block identity")
	}
	body, rest, bodyErr := rlp.SplitList(r.Block.Body)
	if bodyErr != nil || len(rest) != 0 {
		return nil, nil, errors.New("nodefeed: invalid block body")
	}
	_, _, remaining, bodyErr := rlp.Split(body)
	if bodyErr != nil {
		return nil, nil, bodyErr
	}
	txRoot, bodyErr := consensusListRoot(body[:len(body)-len(remaining)])
	if bodyErr != nil || txRoot != b.TxRoot {
		return nil, nil, errors.New("nodefeed: transaction root differs from verified header")
	}
	root, err := consensusListRoot(r.Block.Receipts)
	if err != nil {
		return nil, nil, err
	}
	if root != b.ReceiptRoot {
		return nil, nil, errors.New("nodefeed: receipt root differs from verified header")
	}
	rs, err := s.profile.DeriveReceipts(b, r.Block.Receipts)
	if len(rs) == 0 {
		rs = nil
	}
	return b, rs, err
}

// Acknowledge is called only after the storage transaction/cursor commit.
// Lost ACKs can delay pruning; they cannot undo a durable indexer commit.
func (s *Source) Acknowledge(ctx context.Context, b *model.Block) error {
	_, err := s.call(ctx, Request{Kind: "ack", Height: b.Number, Hash: b.Hash})
	var missing *FeedError
	if errors.As(err, &missing) && missing.Code == "ack_not_acquired" {
		acquired, _, acquireErr := s.BlockWithReceipts(ctx, b.Number)
		if acquireErr != nil {
			return acquireErr
		}
		if acquired.Hash != b.Hash {
			return errors.New("nodefeed: durable ACK block is no longer canonical")
		}
		_, err = s.call(ctx, Request{Kind: "ack", Height: b.Number, Hash: b.Hash})
	}
	return err
}
func (s *Source) BalanceAtHash(ctx context.Context, addr common.Address, hash common.Hash) (*big.Int, error) {
	values, failures, err := s.BalanceBatchAtHash(ctx, []common.Address{addr}, hash)
	if err != nil {
		return nil, err
	}
	return values[0], failures[0]
}
func (s *Source) BalanceBatchAtHash(ctx context.Context, addresses []common.Address, hash common.Hash) ([]*big.Int, []error, error) {
	if len(addresses) == 0 || len(addresses) > 64 {
		return nil, nil, errors.New("nodefeed: state batch outside 1..64")
	}
	r, err := s.call(ctx, Request{Kind: "balance", Hash: hash, Addresses: addresses})
	if err != nil {
		return nil, nil, err
	}
	if len(r.Balances) != len(addresses) {
		return nil, nil, errors.New("nodefeed: incomplete balance batch")
	}
	values := make([]*big.Int, len(addresses))
	for i, value := range r.Balances {
		if value == nil || value.ToInt().Sign() < 0 || value.ToInt().BitLen() > 256 {
			return nil, nil, errors.New("nodefeed: invalid balance")
		}
		values[i] = new(big.Int).Set(value.ToInt())
	}
	return values, make([]error, len(values)), nil
}
func (s *Source) BalanceAt(ctx context.Context, addr common.Address, height *big.Int) (*big.Int, error) {
	if height == nil || !height.IsUint64() {
		return nil, errors.New("nodefeed: explicit state height required")
	}
	hash, err := s.HashAt(ctx, height.Uint64())
	if err != nil {
		return nil, err
	}
	return s.BalanceAtHash(ctx, addr, hash)
}

func (s *Source) call(ctx context.Context, request Request) (Response, error) {
	response, err := s.exchange(ctx, request)
	var lost *FeedError
	if request.Kind != "hello" && errors.As(err, &lost) && lost.Code == "inactive_session" {
		s.handshakeMu.Lock()
		_, helloErr := s.exchange(ctx, Request{Kind: "hello", Hash: s.genesis})
		s.handshakeMu.Unlock()
		if helloErr != nil {
			return Response{}, helloErr
		}
		return s.exchange(ctx, request)
	}
	return response, err
}
func (s *Source) exchange(ctx context.Context, request Request) (result Response, err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Response{}, errors.New("nodefeed: closed")
	}
	s.active.Add(1)
	s.mu.Unlock()
	defer s.active.Done()
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	defer func() {
		if err != nil && ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	stopRoot := context.AfterFunc(s.ctx, cancel)
	defer stopRoot()
	request.Version, request.ChainID, request.Session = Version, s.cfg.ChainID, s.session
	raw, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", s.cfg.Socket)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = conn.Close() }()
	stopIO := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopIO()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return Response{}, err
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(raw)))
	if _, err = io.Copy(conn, bytes.NewReader(append(length[:], raw...))); err != nil {
		return Response{}, err
	}
	if _, err = io.ReadFull(conn, length[:]); err != nil {
		return Response{}, err
	}
	n := binary.BigEndian.Uint32(length[:])
	if n == 0 || n > MaxFrame {
		return Response{}, errors.New("nodefeed: response exceeds frame bound")
	}
	raw = make([]byte, n)
	if _, err = io.ReadFull(conn, raw); err != nil {
		return Response{}, err
	}
	var response Response
	if err = json.Unmarshal(raw, &response); err != nil {
		return Response{}, err
	}
	if response.Version != Version || response.ChainID != s.cfg.ChainID {
		return Response{}, errors.New("nodefeed: response protocol or chain mismatch")
	}
	if response.Error != nil {
		if response.Error.Code == "not_found" || strings.Contains(response.Error.Message, "block not found") {
			return Response{}, fmt.Errorf("%w: %s", source.ErrNotFound, response.Error.Message)
		}
		return Response{}, response.Error
	}
	return response, nil
}

type rawReceipts [][]byte

func (r rawReceipts) Len() int                           { return len(r) }
func (r rawReceipts) EncodeIndex(i int, w *bytes.Buffer) { _, _ = w.Write(r[i]) }
func consensusListRoot(raw []byte) (common.Hash, error) {
	content, rest, err := rlp.SplitList(raw)
	if err != nil || len(rest) != 0 {
		return common.Hash{}, errors.New("nodefeed: invalid consensus list")
	}
	var receipts rawReceipts
	for len(content) > 0 {
		kind, value, remaining, e := rlp.Split(content)
		if e != nil {
			return common.Hash{}, e
		}
		item := content[:len(content)-len(remaining)]
		if kind == rlp.String {
			item = value
		}
		receipts = append(receipts, item)
		content = remaining
	}
	return types.DeriveSha(receipts, trie.NewStackTrie(nil)), nil
}
