package nodebackend

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/exporter"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/spool"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Config struct {
	StartMode    string   `json:"start_mode"`
	DataDir      string   `json:"data_dir"`
	SpoolDir     string   `json:"spool_dir"`
	Socket       string   `json:"socket"`
	GenesisFile  string   `json:"genesis_file"`
	ChainID      string   `json:"chain_id"`
	NetworkID    uint64   `json:"network_id"`
	GenesisHash  string   `json:"genesis_hash"`
	AnchorHeight uint64   `json:"anchor_height"`
	AnchorHash   string   `json:"anchor_hash"`
	AnchorRoot   string   `json:"anchor_root"`
	Peers        []string `json:"peers"`
	ListenAddr   string   `json:"listen_addr"`
	SpoolBytes   uint64   `json:"spool_bytes"`
	SpoolRecords int      `json:"spool_records"`
}
type ownedNode interface {
	Start() error
	Close() error
	Backend() exporter.Backend
}
type nodeFactory func(Config, *core.Genesis) (ownedNode, error)
type clientRunner func(context.Context, *exporter.Client) error

func validHash(value string) bool {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return false
	}
	b, err := hex.DecodeString(value[2:])
	return err == nil && common.BytesToHash(b) != (common.Hash{})
}
func settings(c Config, g *core.Genesis) (node.Config, ethconfig.Config, error) {
	n, e := node.DefaultConfig, ethconfig.Defaults
	invalid := func() (node.Config, ethconfig.Config, error) {
		return node.Config{}, ethconfig.Config{}, fmt.Errorf("executor: invalid explicit node identity, paths, checkpoint, or fork configuration")
	}
	if c.StartMode != "" && c.StartMode != "genesis" && c.StartMode != "checkpoint" || c.StartMode == "genesis" && c.AnchorHeight != 0 {
		return invalid()
	}
	if !filepath.IsAbs(c.DataDir) || !filepath.IsAbs(c.SpoolDir) || !filepath.IsAbs(c.Socket) || !filepath.IsAbs(c.GenesisFile) || c.NetworkID == 0 || c.AnchorHeight >= math.MaxInt64 || !validHash(c.GenesisHash) || !validHash(c.AnchorHash) || !validHash(c.AnchorRoot) || len(c.Peers) > 64 || c.SpoolRecords < 0 {
		return invalid()
	}
	a, b := filepath.Clean(c.DataDir), filepath.Clean(c.SpoolDir)
	if a == b || strings.HasPrefix(a, b+string(os.PathSeparator)) || strings.HasPrefix(b, a+string(os.PathSeparator)) {
		return invalid()
	}
	if g == nil || g.Config == nil || g.Config.ChainID == nil || g.Config.ChainID.Sign() <= 0 || g.Config.ChainID.String() != c.ChainID || !g.Config.AnzeonEnabled() || g.Config.Anzeon.WBFT == nil || g.Config.DAOForkSupport {
		return invalid()
	}
	if err := g.Config.CheckConfigForkOrder(); err != nil {
		return invalid()
	}
	genesis := g.ToBlock()
	if genesis.Hash() != common.HexToHash(c.GenesisHash) || c.AnchorHeight == 0 && (common.HexToHash(c.AnchorHash) != genesis.Hash() || common.HexToHash(c.AnchorRoot) != genesis.Root()) {
		return invalid()
	}
	if c.ListenAddr == "" {
		c.ListenAddr = "127.0.0.1:0"
	}
	if _, _, err := net.SplitHostPort(c.ListenAddr); err != nil {
		return invalid()
	}
	n.Name, n.DataDir, n.IPCPath, n.HTTPHost, n.WSHost = "indexer-stablenet", c.DataDir, "", "", ""
	n.P2P.ListenAddr, n.P2P.NoDiscovery, n.P2P.MaxPeers = c.ListenAddr, true, 8
	n.P2P.BootstrapNodes, n.P2P.BootstrapNodesV5, n.P2P.StaticNodes, n.P2P.TrustedNodes = nil, nil, nil, nil
	for _, raw := range c.Peers {
		peer, err := enode.ParseV4(raw)
		if err != nil {
			return invalid()
		}
		n.P2P.StaticNodes = append(n.P2P.StaticNodes, peer)
	}
	e.Genesis, e.NetworkId, e.SyncMode, e.StateScheme = g, c.NetworkID, downloader.FullSync, "path"
	// Keep history until E3 supplies verified ACK/reorg-aware retention. This
	// assembly never activates an automatic block/state deletion policy.
	e.Preimages = true
	e.StateHistory, e.TransactionHistory, e.EthDiscoveryURLs, e.SnapDiscoveryURLs = 0, 0, nil, nil
	return n, e, nil
}

func loadJSON(path string, target any, limit int64, strict bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return execwire.ErrLimit
	}
	dec := json.NewDecoder(io.LimitReader(f, limit+1))
	if strict {
		dec.DisallowUnknownFields()
	}
	if err = dec.Decode(target); err != nil {
		return err
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("executor: trailing JSON data")
	}
	return nil
}

// LoadConfig reads settings only. Neither this function nor settings starts a
// node, opens its data directory, dials a peer, or changes disk provisioning.
func LoadConfig(path string) (Config, error) {
	var c Config
	err := loadJSON(path, &c, 1<<20, true)
	return c, err
}

// Run owns the official node and spool. Peer import starts only after the
// authenticated exporter Hello/backlog gate invokes OnReady.
func Run(ctx context.Context, c Config) error {
	return run(ctx, c, openNode, func(ctx context.Context, c *exporter.Client) error { return c.Run(ctx) })
}

func run(ctx context.Context, c Config, open nodeFactory, send clientRunner) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	var genesis core.Genesis
	if err = loadJSON(c.GenesisFile, &genesis, 16<<20, false); err != nil {
		return err
	}
	if _, _, err = settings(c, &genesis); err != nil {
		return err
	}
	if open == nil || send == nil {
		return fmt.Errorf("executor: missing lifecycle dependency")
	}
	chain := execwire.Chain{ID: c.ChainID, Genesis: execwire.Hash(common.HexToHash(c.GenesisHash))}
	anchor := execwire.Ref{Generation: 1, Height: c.AnchorHeight, Hash: execwire.Hash(common.HexToHash(c.AnchorHash))}
	s, err := spool.Open(ctx, spool.Options{Path: c.SpoolDir, Chain: chain, Anchor: anchor, MaxBytes: c.SpoolBytes, MaxRecords: c.SpoolRecords})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	n, err := open(c, &genesis)
	if err != nil {
		return err
	}
	if n == nil {
		return fmt.Errorf("executor: nil node")
	}
	defer func() { err = errors.Join(err, n.Close()) }()
	backend := n.Backend()
	if backend == nil {
		return fmt.Errorf("executor: nil backend")
	}
	hash, err := backend.Canonical(ctx, c.AnchorHeight)
	if err != nil {
		return err
	}
	if hash != anchor.Hash {
		return fmt.Errorf("%w: original node anchor", spool.ErrConflict)
	}
	var once sync.Once
	var startErr error
	client := &exporter.Client{Chain: chain, Spool: s, Backend: backend, Socket: c.Socket, OnReady: func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		once.Do(func() { startErr = n.Start() })
		return startErr
	}}
	return send(ctx, client)
}

type nativeNode struct {
	stack   *node.Node
	backend *Backend
}

func (n *nativeNode) Start() error              { return n.stack.Start() }
func (n *nativeNode) Close() error              { return n.stack.Close() }
func (n *nativeNode) Backend() exporter.Backend { return n.backend }
func openNode(c Config, g *core.Genesis) (ownedNode, error) {
	ncfg, ecfg, err := settings(c, g)
	if err != nil {
		return nil, err
	}
	stack, err := node.New(&ncfg)
	if err != nil {
		return nil, err
	}
	service, err := eth.New(stack, &ecfg)
	if err != nil {
		_ = stack.Close()
		return nil, err
	}
	bc := service.BlockChain()
	anchor := bc.GetHeaderByNumber(c.AnchorHeight)
	if anchor == nil || anchor.Hash() != common.HexToHash(c.AnchorHash) || anchor.Root != common.HexToHash(c.AnchorRoot) {
		_ = stack.Close()
		return nil, fmt.Errorf("%w: original anchor/header/root; explicit prepared node checkpoint required", exporter.ErrCheckpointRecovery)
	}
	return &nativeNode{stack: stack, backend: &Backend{chain: bc}}, nil
}
