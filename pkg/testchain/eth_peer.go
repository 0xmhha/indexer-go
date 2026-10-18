package testchain

import (
	"errors"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rlp"
)

// EthPeerServer is a loopback RLPx/ETH68 protocol oracle. It does not run an
// execution or consensus node. Packets are independently encoded from Chain.
type EthPeerServer struct {
	Chain    *Chain
	Server   *p2p.Server
	Fork     [4]byte
	mu       sync.Mutex
	hook     func(uint64, *EthPacket)
	requests int
}
type EthPacket struct {
	ID    uint64
	Items []rlp.RawValue
}
type ethStatus struct {
	Version       uint32
	Network       uint64
	TD            *big.Int
	Head, Genesis common.Hash
	Fork          struct {
		Hash [4]byte
		Next uint64
	}
}

func NewEthPeerServer(chain *Chain) (*EthPeerServer, error) {
	s := &EthPeerServer{Chain: chain, Fork: [4]byte{1, 2, 3, 4}}
	s.Server = &p2p.Server{Config: p2p.Config{PrivateKey: NewAccount(9000).Key, Name: "indexer-test-eth68", NoDiscovery: true, NoDial: true, ListenAddr: "127.0.0.1:0", MaxPeers: 8, Protocols: []p2p.Protocol{{Name: "eth", Version: 68, Length: 17, Run: s.run}}}}
	if err := s.Server.Start(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *EthPeerServer) Node() *enode.Node                  { return s.Server.Self() }
func (s *EthPeerServer) Close()                             { s.Server.Stop() }
func (s *EthPeerServer) SetHook(h func(uint64, *EthPacket)) { s.mu.Lock(); s.hook = h; s.mu.Unlock() }
func (s *EthPeerServer) Requests() int                      { s.mu.Lock(); defer s.mu.Unlock(); return s.requests }
func (s *EthPeerServer) run(_ *p2p.Peer, rw p2p.MsgReadWriter) error {
	msg, err := rw.ReadMsg()
	if err != nil {
		return err
	}
	var remote ethStatus
	if msg.Code != 0 || msg.Size > 4096 {
		return errors.New("fixture status size/code")
	}
	if err := msg.Decode(&remote); err != nil {
		return err
	}
	st := ethStatus{Version: 68, Network: s.Chain.ChainID().Uint64(), TD: new(big.Int), Head: s.Chain.Block(s.Chain.Head()).Block.Hash(), Genesis: s.Chain.Block(0).Block.Hash()}
	st.Fork.Hash = s.Fork
	if err := p2p.Send(rw, 0, st); err != nil {
		return err
	}
	for {
		msg, err := rw.ReadMsg()
		if err != nil {
			return err
		}
		if msg.Size > 10<<20 {
			return errors.New("fixture size")
		}
		var id uint64
		var response uint64
		var out []rlp.RawValue
		switch msg.Code {
		case 3:
			var request struct {
				ID    uint64
				Query struct {
					Origin       rlp.RawValue
					Amount, Skip uint64
					Reverse      bool
				}
			}
			if err := msg.Decode(&request); err != nil {
				return err
			}
			id = request.ID
			response = 4
			var n uint64
			_, value, _, err := rlp.Split(request.Query.Origin)
			if err != nil {
				return err
			}
			if len(value) == 32 {
				var hash common.Hash
				if err := rlp.DecodeBytes(request.Query.Origin, &hash); err != nil {
					return err
				}
				n = s.find(hash)
			} else {
				if err := rlp.DecodeBytes(request.Query.Origin, &n); err != nil {
					return err
				}
			}
			if n <= s.Chain.Head() && request.Query.Amount > 0 {
				raw, err := rlp.EncodeToBytes(s.Chain.Block(n).Block.Header())
				if err != nil {
					return err
				}
				out = append(out, raw)
			}
		case 5, 15:
			var request struct {
				ID     uint64
				Hashes []common.Hash
			}
			if err := msg.Decode(&request); err != nil {
				return err
			}
			id = request.ID
			response = 6
			if msg.Code == 15 {
				response = 16
			}
			for _, hash := range request.Hashes {
				n := s.find(hash)
				if n > s.Chain.Head() {
					continue
				}
				b := s.Chain.Block(n)
				var raw []byte
				var err error
				if msg.Code == 5 {
					raw, err = rlp.EncodeToBytes(b.Block.Body())
				} else {
					raw, err = rlp.EncodeToBytes(b.Receipts)
				}
				if err != nil {
					return err
				}
				out = append(out, raw)
			}
		default:
			if err := msg.Discard(); err != nil {
				return err
			}
			continue
		}
		packet := EthPacket{ID: id, Items: out}
		s.mu.Lock()
		s.requests++
		hook := s.hook
		s.mu.Unlock()
		if hook != nil {
			hook(response, &packet)
		}
		if err := p2p.Send(rw, response, packet); err != nil {
			return err
		}
	}
}
func (s *EthPeerServer) find(hash common.Hash) uint64 {
	for n := uint64(0); n <= s.Chain.Head(); n++ {
		if s.Chain.Block(n).Block.Hash() == hash {
			return n
		}
	}
	return ^uint64(0)
}
