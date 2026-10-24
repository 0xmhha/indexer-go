package nodebackend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/exporter"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/nodestore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
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

func TestNodeSettingsStartModesAndPreimages(t *testing.T) {
	c, g := runtimeConfig(t)
	c.Peers = nil
	c.StartMode = "genesis"
	_, e, err := settings(c, g)
	if err != nil || !e.Preimages {
		t.Fatal("genesis mode must preserve state preimages", err)
	}
	c.AnchorHeight = 1
	if _, _, err = settings(c, g); err == nil {
		t.Fatal("nonzero genesis anchor accepted")
	}
	c.StartMode = "unknown"
	if _, _, err = settings(c, g); err == nil {
		t.Fatal("unknown mode accepted")
	}
}
func TestOfflineCheckpointMissingPathDoesNotCreateNodeData(t *testing.T) {
	c, _ := runtimeConfig(t)
	path := filepath.Join(t.TempDir(), "missing")
	if _, err := InspectCheckpoint(t.Context(), c, path, ""); err == nil {
		t.Fatal("missing database accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("inspection created data")
	}
}

func TestOfflineCheckpointReadsPreparedPathDBWithoutNodeStart(t *testing.T) {
	c, g := runtimeConfig(t)
	c.Peers = nil
	g.Alloc = types.GenesisAlloc{common.Address{19: 1}: {Balance: big.NewInt(1)}}
	path := filepath.Join(t.TempDir(), "chaindata")
	disk, err := rawdb.Open(rawdb.OpenOptions{Directory: path, Cache: 16, Handles: 16})
	if err != nil {
		t.Fatal(err)
	}
	db := state.NewDatabaseWithConfig(disk, &triedb.Config{PathDB: pathdb.Defaults, Preimages: true})
	b, err := g.Commit(disk, db.TrieDB())
	if err != nil {
		t.Fatal(err)
	}
	c.GenesisHash = b.Hash().Hex()
	c.AnchorHash = b.Hash().Hex()
	c.AnchorRoot = b.Root().Hex()
	if err = errors.Join(db.TrieDB().Close(), disk.Close()); err != nil {
		t.Fatal(err)
	}
	report, err := InspectCheckpoint(t.Context(), c, path, "")
	if err != nil || !report.StateAvailable || !report.HeaderAvailable {
		t.Fatal(report, err)
	}
	var out bytes.Buffer
	if err = ExportCheckpoint(t.Context(), c, path, "", &out, nil); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 {
		t.Fatal("no snapshot")
	}
	c.AnchorRoot = common.Hash{99}.Hex()
	if _, err = InspectCheckpoint(t.Context(), c, path, ""); err == nil {
		t.Fatal("wrong exact root accepted")
	}
}

func TestOfflineExportGenesisDoesNotCreateNodeOrSpool(t *testing.T) {
	c, _ := runtimeConfig(t)
	c.Peers = nil
	var out bytes.Buffer
	if err := ExportGenesis(t.Context(), c, &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 {
		t.Fatal("empty export")
	}
	for _, path := range []string{c.DataDir, c.SpoolDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("export created live data path", path, err)
		}
	}
}

func TestNodeRuntimeMaintenanceFencePreventsFactoryAndStart(t *testing.T) {
	c, _ := runtimeConfig(t)
	c.Peers = nil
	if err := os.Mkdir(c.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.DataDir, ".execution-retention"), []byte("preserve even corrupt evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	err := run(t.Context(), c, func(Config, *core.Genesis) (ownedNode, error) { called = true; return nil, nil }, func(context.Context, *exporter.Client) error { return nil })
	if !errors.Is(err, nodestore.ErrPending) || called {
		t.Fatal("maintenance started a node", called, err)
	}
	if _, err = os.Stat(c.SpoolDir); !os.IsNotExist(err) {
		t.Fatal("fenced startup initialized source")
	}
}
