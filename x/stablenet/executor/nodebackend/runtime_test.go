package nodebackend

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/exporter"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

func runtimeConfig(t *testing.T) (Config, *core.Genesis) {
	t.Helper()
	dir := t.TempDir()
	chain := *params.TestWBFTChainConfig
	g := &core.Genesis{Config: &chain, GasLimit: 30000000, Difficulty: big.NewInt(1), Alloc: types.GenesisAlloc{}}
	block := g.ToBlock()
	path := filepath.Join(dir, "genesis.json")
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return Config{DataDir: filepath.Join(dir, "node"), SpoolDir: filepath.Join(dir, "spool"), Socket: filepath.Join(dir, "feed.sock"), GenesisFile: path, ChainID: chain.ChainID.String(), NetworkID: 1, GenesisHash: block.Hash().Hex(), AnchorHash: block.Hash().Hex(), AnchorRoot: block.Root().Hex(), Peers: []string{"enode://bad"}, ListenAddr: "127.0.0.1:0"}, g
}
func TestNodeSettingsRequireExactIdentityAndKeepServicesDisabled(t *testing.T) {
	c, g := runtimeConfig(t)
	if _, _, err := settings(c, g); err == nil {
		t.Fatal("invalid enode accepted")
	}
	c.Peers = nil // A prepared offline node is permitted; no discovery is enabled.
	n, e, err := settings(c, g)
	if err != nil {
		t.Fatal(err)
	}
	if n.HTTPHost != "" || n.WSHost != "" || n.IPCPath != "" || !n.P2P.NoDiscovery || n.DataDir != c.DataDir || e.StateScheme != "path" || e.StateHistory != 0 || e.TransactionHistory != 0 || e.NetworkId != c.NetworkID {
		t.Fatalf("unsafe node settings: %+v %+v", n, e)
	}
	for _, change := range []func(*Config){func(c *Config) { c.ChainID = "9" }, func(c *Config) { c.GenesisHash = common.Hash{9}.Hex() }, func(c *Config) { c.DataDir = "relative" }, func(c *Config) { c.SpoolDir = c.DataDir }, func(c *Config) { c.NetworkID = 0 }, func(c *Config) { c.AnchorRoot = "0x00" }} {
		bad := c
		change(&bad)
		if _, _, err = settings(bad, g); err == nil {
			t.Fatalf("invalid config accepted: %+v", bad)
		}
	}
}

type lifecycleBackend struct {
	exporter.Backend
	anchor execwire.Ref
}

func (b lifecycleBackend) Canonical(context.Context, uint64) (execwire.Hash, error) {
	return b.anchor.Hash, nil
}

type lifecycleNode struct {
	started, closed int
	backend         exporter.Backend
	err             error
}

func (n *lifecycleNode) Start() error              { n.started++; return n.err }
func (n *lifecycleNode) Close() error              { n.closed++; return nil }
func (n *lifecycleNode) Backend() exporter.Backend { return n.backend }
func TestNodeRuntimeDefersStartUntilReadyAndClosesOnFailure(t *testing.T) {
	c, g := runtimeConfig(t)
	c.Peers = nil
	n := &lifecycleNode{backend: lifecycleBackend{anchor: execwire.Ref{Generation: 1, Hash: execwire.Hash(g.ToBlock().Hash())}}}
	want := errors.New("owned exporter finished")
	factory := func(Config, *core.Genesis) (ownedNode, error) { return n, nil }
	err := run(t.Context(), c, factory, func(ctx context.Context, client *exporter.Client) error {
		if n.started != 0 {
			t.Fatal("node started before Hello")
		}
		if err := client.OnReady(ctx); err != nil {
			return err
		}
		if n.started != 1 {
			t.Fatal("node did not start")
		}
		return want
	})
	if !errors.Is(err, want) || n.closed != 1 {
		t.Fatalf("lifecycle: %v start=%d close=%d", err, n.started, n.closed)
	}
}
func TestNodeRuntimeCanceledBeforeFactoryDoesNotOpenNode(t *testing.T) {
	c, _ := runtimeConfig(t)
	c.Peers = nil
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	err := run(ctx, c, func(Config, *core.Genesis) (ownedNode, error) { called = true; return nil, nil }, nil)
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("cancellation: %v factory=%v", err, called)
	}
}
