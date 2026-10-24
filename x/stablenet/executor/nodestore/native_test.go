package nodestore

import (
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
	"github.com/holiman/uint256"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, goleak.IgnoreTopFunction("github.com/ethereum/go-ethereum/core.(*txSenderCacher).cache"))
}

func TestCompactedNativeBlockChainImportsAndReorgsWithoutNodeStart(t *testing.T) {
	o := Options{DataDir: filepath.Join(t.TempDir(), "node"), ReserveBytes: 1}
	path := filepath.Join(o.DataDir, "indexer-stablenet", "chaindata")
	if e := os.MkdirAll(path, 0700); e != nil {
		t.Fatal(e)
	}
	address := common.Address{19: 42}
	cfg := *params.AllEthashProtocolChanges
	genesis := &core.Genesis{Config: &cfg, Difficulty: big.NewInt(131072), GasLimit: 30000000, Alloc: types.GenesisAlloc{address: {Balance: big.NewInt(123)}}}
	engine := ethash.NewFaker()
	mem, blocks, _ := core.GenerateChainWithGenesis(genesis, engine, 133, nil)
	defer func() { _ = mem.Close() }()
	disk, e := rawdb.Open(rawdb.OpenOptions{Directory: path, AncientsDirectory: filepath.Join(path, "ancient"), Cache: 16, Handles: 32})
	if e != nil {
		t.Fatal(e)
	}
	cache := &core.CacheConfig{StateScheme: rawdb.PathScheme, StateHistory: 0, Preimages: true, TrieCleanLimit: 16, TrieDirtyLimit: 16}
	bc, e := core.NewBlockChain(disk, cache, genesis, nil, engine, vm.Config{}, nil, nil)
	if e != nil {
		_ = disk.Close()
		t.Fatal(e)
	}
	if _, e = bc.InsertChain(blocks[:132]); e != nil {
		bc.Stop()
		_ = disk.Close()
		t.Fatal(e)
	}
	bc.Stop()
	if e = disk.Close(); e != nil {
		t.Fatal(e)
	}
	b := blocks[131]
	chain := execwire.Chain{ID: cfg.ChainID.String(), Genesis: execwire.Hash(genesis.ToBlock().Hash())}
	ack := execwire.Ref{Generation: 1, Height: 132, Hash: execwire.Hash(b.Hash())}
	r := retention.Request{Version: 1, Chain: chain, Anchor: execwire.Ref{Generation: 1, Hash: chain.Genesis}, ACK: ack, Tail: ack, Head: execwire.Ref{Height: 132, Hash: ack.Hash}, HeadRoot: execwire.Hash(b.Root()), SourceDigest: execwire.Hash{6}, KeepBlocks: 129, Through: 3}
	p := retention.Permit{Coverage: "full", BaselineDigest: execwire.Hash{102}, Version: 1, Token: execwire.Hash{100}, Request: r, Visible: ack, Root: r.HeadRoot}
	if e = Compact(t.Context(), o, p); e != nil {
		t.Fatal(e)
	}
	disk, e = rawdb.Open(rawdb.OpenOptions{Directory: path, AncientsDirectory: filepath.Join(path, "ancient"), Cache: 16, Handles: 32})
	if e != nil {
		t.Fatal(e)
	}
	limit := uint64(1)
	bc, e = core.NewBlockChain(disk, cache, genesis, nil, engine, vm.Config{}, nil, &limit)
	if e != nil {
		_ = disk.Close()
		t.Fatal(e)
	}
	defer func() {
		bc.Stop()
		if e := disk.Close(); e != nil {
			t.Error(e)
		}
	}()
	if bc.CurrentBlock().Hash() != b.Hash() || bc.GetBlockByNumber(1) != nil || bc.GetHeaderByNumber(1) == nil || bc.Genesis().Hash() != genesis.ToBlock().Hash() {
		t.Fatal("native restart changed head/history/ancestry")
	}
	if _, e = bc.InsertChain(blocks[132:]); e != nil {
		t.Fatal("next native import failed", e)
	}
	branch, _ := core.GenerateChain(&cfg, blocks[129], engine, mem, 4, func(_ int, b *core.BlockGen) { b.SetExtra([]byte("replacement")) })
	if _, e = bc.InsertChain(branch); e != nil {
		t.Fatal("short native reorg failed", e)
	}
	if bc.CurrentBlock().Hash() != branch[3].Hash() {
		t.Fatal("short reorg did not replace canonical head")
	}
	// Exercise the native future freezer, not just immediate reopen/import.
	freezer, ok := disk.(interface{ Freeze(uint64) error })
	if !ok {
		t.Fatal("native freezer unavailable")
	}
	if e = freezer.Freeze(1); e != nil {
		t.Fatal(e)
	}
	frozen, e := disk.Ancients()
	if e != nil || frozen < bc.CurrentBlock().Number.Uint64()-1 {
		t.Fatal("native freezer stalled at an expired hot-body gap", frozen, e)
	}
	account, e := bc.State()
	if e != nil {
		t.Fatal(e)
	}
	if account.GetBalance(address).Uint64() != 123 || account.Error() != nil {
		t.Fatal("native state changed", account.Error())
	}
}

func TestStateHistoryUsesStateIDsAndPreservesUnchangedBlockParent(t *testing.T) {
	o := Options{DataDir: filepath.Join(t.TempDir(), "node"), ReserveBytes: 1}
	path := filepath.Join(o.DataDir, "indexer-stablenet", "chaindata")
	if e := os.MkdirAll(path, 0700); e != nil {
		t.Fatal(e)
	}
	hot, e := rawdb.Open(rawdb.OpenOptions{Directory: path, Cache: 16, Handles: 32})
	if e != nil {
		t.Fatal(e)
	}
	cold, e := rawdb.NewChainFreezer(filepath.Join(path, "ancient", "chain"), "state-id-test", false)
	if e != nil {
		t.Fatal(e)
	}
	disk := &offlineDB{KeyValueStore: hot, AncientStore: cold, ancient: filepath.Join(path, "ancient")}
	conf := *pathdb.Defaults
	conf.StateHistory = 0
	db := state.NewDatabaseWithConfig(disk, &triedb.Config{PathDB: &conf, Preimages: true})
	cfg := *params.TestWBFTChainConfig
	addr := common.Address{19: 42}
	g := &core.Genesis{Config: &cfg, Difficulty: big.NewInt(1), GasLimit: 30000000, Alloc: types.GenesisAlloc{addr: {Balance: big.NewInt(123)}}}
	parent, e := g.Commit(disk, db.TrieDB())
	if e != nil {
		t.Fatal(e)
	}
	genesis := parent
	var roots [4]common.Hash
	for n := uint64(1); n <= 136; n++ {
		root := parent.Root()
		if n%2 == 0 {
			st, e := state.New(root, db, nil)
			if e != nil {
				t.Fatal(e)
			}
			st.SetBalance(addr, uint256.NewInt(123+n))
			root, e = st.Commit(n, false)
			if e != nil {
				t.Fatal(e)
			}
			if e = db.TrieDB().Commit(root, false); e != nil {
				t.Fatal(e)
			}
			if n <= 8 {
				roots[n/2-1] = root
			}
		}
		h := &types.Header{ParentHash: parent.Hash(), Number: new(big.Int).SetUint64(n), Root: root, Time: n, GasLimit: 30000000, Difficulty: big.NewInt(1), TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, UncleHash: types.EmptyUncleHash}
		b := types.NewBlockWithHeader(h)
		batch := disk.NewBatch()
		rawdb.WriteBlock(batch, b)
		rawdb.WriteCanonicalHash(batch, b.Hash(), n)
		rawdb.WriteTd(batch, b.Hash(), n, big.NewInt(int64(n+1)))
		rawdb.WriteReceipts(batch, b.Hash(), n, nil)
		if e = batch.Write(); e != nil {
			t.Fatal(e)
		}
		parent = b
	}
	batch := disk.NewBatch()
	rawdb.WriteHeadBlockHash(batch, parent.Hash())
	rawdb.WriteHeadHeaderHash(batch, parent.Hash())
	rawdb.WriteHeadFastBlockHash(batch, parent.Hash())
	if e = batch.Write(); e != nil {
		t.Fatal(e)
	}
	boundaryBefore := rawdb.ReadStateID(disk, roots[2])
	if boundaryBefore == nil {
		t.Fatal("native boundary ID absent before GC")
	}
	expectedBoundary := *boundaryBefore
	if e = errors.Join(db.TrieDB().Close(), disk.Close()); e != nil {
		t.Fatal(e)
	}
	chain := execwire.Chain{ID: cfg.ChainID.String(), Genesis: execwire.Hash(genesis.Hash())}
	ack := execwire.Ref{Generation: 1, Height: 136, Hash: execwire.Hash(parent.Hash())}
	r := retention.Request{Version: 1, Chain: chain, Anchor: execwire.Ref{Generation: 1, Hash: chain.Genesis}, ACK: ack, Tail: ack, Head: execwire.Ref{Height: 136, Hash: ack.Hash}, HeadRoot: execwire.Hash(parent.Root()), SourceDigest: execwire.Hash{6}, KeepBlocks: 129, Through: 7}
	p := retention.Permit{Coverage: "full", BaselineDigest: execwire.Hash{102}, Version: 1, Token: execwire.Hash{101}, Request: r, Visible: ack, Root: r.HeadRoot}
	if e = Compact(t.Context(), o, p); e != nil {
		t.Fatal(e)
	}
	disk2, e := openDisk(path, false)
	if e != nil {
		t.Fatal(e)
	}
	if rawdb.ReadStateID(disk2, roots[0]) != nil || rawdb.ReadStateID(disk2, roots[1]) != nil {
		t.Fatal("expired state IDs not pruned")
	}
	boundary := rawdb.ReadStateID(disk2, roots[2])
	if boundary == nil || *boundary != expectedBoundary {
		t.Fatal("unchanged block7 needs the exact native block6 state ID", boundary)
	}
	freezer, e := rawdb.NewStateFreezer(filepath.Join(path, "ancient"), true)
	if e != nil {
		t.Fatal(e)
	}
	tail, e := freezer.Tail()
	if e != nil || tail != expectedBoundary-1 {
		t.Fatal("state IDs treated as block heights", tail, expectedBoundary, e)
	}
	if e = freezer.Close(); e != nil {
		t.Fatal(e)
	}
	database := state.NewDatabaseWithConfig(disk2, &triedb.Config{PathDB: &conf, Preimages: true})
	recoverable, e := database.TrieDB().Recoverable(roots[2])
	if e != nil || !recoverable {
		t.Fatal("retained boundary cannot recover", recoverable, e)
	}
	if e = database.TrieDB().Recover(roots[2]); e != nil {
		t.Fatal(e)
	}
	st, e := state.New(roots[2], database, nil)
	if e != nil {
		t.Fatal(e)
	}
	if st.GetBalance(addr).Uint64() != 129 || st.Error() != nil {
		t.Fatal("recovered parent state differs", st.Error())
	}
	if e = errors.Join(database.TrieDB().Close(), disk2.Close()); e != nil {
		t.Fatal(e)
	}
}
