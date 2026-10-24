package nodestore

import (
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
)

// These are deterministic storage fixtures, not consensus-valid peer blocks.
// They use real path state, canonical RLP/freezer records and native DB reopen.
func fixture(t *testing.T, frozen bool) (Options, retention.Permit, common.Address) {
	t.Helper()
	o := Options{DataDir: filepath.Join(t.TempDir(), "node"), ReserveBytes: 1}
	path := filepath.Join(o.DataDir, "indexer-stablenet", "chaindata")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	disk, e := rawdb.Open(rawdb.OpenOptions{Directory: path, Cache: 16, Handles: 16})
	if e != nil {
		t.Fatal(e)
	}
	cold, e := rawdb.NewChainFreezer(filepath.Join(path, "ancient", "chain"), "fixture", false)
	if e != nil {
		t.Fatal(e)
	}
	disk = &offlineDB{KeyValueStore: disk, AncientStore: cold, ancient: filepath.Join(path, "ancient")}
	db := state.NewDatabaseWithConfig(disk, &triedb.Config{PathDB: pathdb.Defaults, Preimages: true})
	cfg := *params.TestWBFTChainConfig
	address := common.Address{19: 42}
	g := &core.Genesis{Config: &cfg, GasLimit: 30000000, Difficulty: big.NewInt(1), Alloc: types.GenesisAlloc{address: {Balance: big.NewInt(123)}}}
	genesis, e := g.Commit(disk, db.TrieDB())
	if e != nil {
		t.Fatal(e)
	}
	batch := disk.NewBatch()
	parent := genesis
	blocks := []*types.Block{genesis}
	for n := uint64(1); n <= 132; n++ {
		h := &types.Header{ParentHash: parent.Hash(), Number: new(big.Int).SetUint64(n), Root: genesis.Root(), Time: n, GasLimit: 30000000, Difficulty: big.NewInt(1), TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, UncleHash: types.EmptyUncleHash}
		b := types.NewBlockWithHeader(h)
		rawdb.WriteBlock(batch, b)
		rawdb.WriteCanonicalHash(batch, b.Hash(), n)
		rawdb.WriteTd(batch, b.Hash(), n, big.NewInt(int64(n+1)))
		rawdb.WriteReceipts(batch, b.Hash(), n, nil)
		blocks = append(blocks, b)
		parent = b
	}
	rawdb.WriteHeadBlockHash(batch, parent.Hash())
	rawdb.WriteHeadHeaderHash(batch, parent.Hash())
	rawdb.WriteHeadFastBlockHash(batch, parent.Hash())
	if e = batch.Write(); e != nil {
		t.Fatal(e)
	}
	if e = db.TrieDB().Close(); e != nil {
		t.Fatal(e)
	}
	if e = disk.Close(); e != nil {
		t.Fatal(e)
	}
	if frozen {
		kv, e := rawdb.Open(rawdb.OpenOptions{Directory: path, Cache: 16, Handles: 16})
		if e != nil {
			t.Fatal(e)
		}
		f, e := rawdb.NewChainFreezer(filepath.Join(path, "ancient", "chain"), "e3-test", false)
		if e != nil {
			t.Fatal(e)
		}
		_, e = f.ModifyAncients(func(w ethdb.AncientWriteOp) error {
			for n, b := range blocks[:4] {
				if e := w.AppendRaw(rawdb.ChainFreezerHashTable, uint64(n), b.Hash().Bytes()); e != nil {
					return e
				}
				if e := w.Append(rawdb.ChainFreezerHeaderTable, uint64(n), b.Header()); e != nil {
					return e
				}
				if e := w.Append(rawdb.ChainFreezerBodiesTable, uint64(n), b.Body()); e != nil {
					return e
				}
				if e := w.Append(rawdb.ChainFreezerReceiptTable, uint64(n), []*types.ReceiptForStorage{}); e != nil {
					return e
				}
				if e := w.Append(rawdb.ChainFreezerDifficultyTable, uint64(n), big.NewInt(int64(n+1))); e != nil {
					return e
				}
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
		if e = f.Close(); e != nil {
			t.Fatal(e)
		}
		for n, b := range blocks[1:4] {
			rawdb.DeleteBlockWithoutNumber(kv, b.Hash(), uint64(n+1))
			rawdb.DeleteCanonicalHash(kv, uint64(n+1))
		}
		if e = kv.Close(); e != nil {
			t.Fatal(e)
		}
	}
	chain := execwire.Chain{ID: cfg.ChainID.String(), Genesis: execwire.Hash(genesis.Hash())}
	ack := execwire.Ref{Generation: 2, Height: 132, Hash: execwire.Hash(parent.Hash())}
	r := retention.Request{Version: 1, Chain: chain, Anchor: execwire.Ref{Generation: 1, Hash: chain.Genesis}, ACK: ack, Tail: ack, Head: execwire.Ref{Height: 132, Hash: ack.Hash}, HeadRoot: execwire.Hash(parent.Root()), SourceDigest: execwire.Hash{5}, KeepBlocks: 129, Through: 3}
	return o, retention.Permit{Coverage: "full", BaselineDigest: execwire.Hash{102}, Version: 1, Token: execwire.Hash{99}, Request: r, Visible: ack, Root: r.HeadRoot}, address
}
func TestCompactPreservesGenesisHeadersPathStateAndProtectedBodies(t *testing.T) {
	for _, frozen := range []bool{false, true} {
		t.Run(map[bool]string{false: "hot", true: "frozen"}[frozen], func(t *testing.T) {
			o, p, address := fixture(t, frozen)
			if e := Compact(context.Background(), o, p); e != nil {
				t.Fatal("complete acknowledged offline node payloads must be compactable:", e)
			}
			path := filepath.Join(o.DataDir, "indexer-stablenet", "chaindata")
			disk, e := openDisk(path, true)
			if e != nil {
				t.Fatal(e)
			}
			for n := uint64(0); n <= 132; n++ {
				hash := rawdb.ReadCanonicalHash(disk, n)
				header := rawdb.ReadHeader(disk, hash, n)
				if header == nil || header.Number.Uint64() != n {
					t.Fatal("header/canonical ancestry lost", n)
				}
				body := rawdb.ReadBodyRLP(disk, hash, n)
				if n >= 1 && n <= 3 {
					if len(body) != 0 || len(rawdb.ReadReceiptsRLP(disk, hash, n)) != 0 {
						t.Fatal("expired body/receipt retained", n)
					}
				} else if len(body) == 0 {
					t.Fatal("protected payload lost", n)
				}
			}
			database := state.NewDatabaseWithConfig(disk, &triedb.Config{PathDB: pathdb.ReadOnly, Preimages: true})
			st, e := state.New(common.Hash(p.Request.HeadRoot), database, nil)
			if e != nil {
				t.Fatal(e)
			}
			if st.GetBalance(address).Uint64() != 123 {
				t.Fatal("live state changed")
			}
			if e = errors.Join(st.Error(), database.TrieDB().Close(), disk.Close()); e != nil {
				t.Fatal(e)
			}
			if !errors.Is(CheckStart(o.DataDir), ErrPending) {
				t.Fatal("producer started before DB release")
			}
			if _, e = os.Stat(filepath.Join(filepath.Dir(path), ".execution-retention-old")); !os.IsNotExist(e) {
				t.Fatal("old copy not collected", e)
			}
			pd, e := retention.Digest(p)
			if e != nil {
				t.Fatal(e)
			}
			receipt := retention.Receipt{Version: 1, PermitDigest: pd, Node: true, Spool: true}
			rd, e := retention.Digest(receipt)
			if e != nil {
				t.Fatal(e)
			}
			release := retention.Release{Version: 1, PermitDigest: pd, ReceiptDigest: rd}
			if e = Release(t.Context(), o, p, receipt, release); e != nil {
				t.Fatal(e)
			}
			if e = CheckStart(o.DataDir); e != nil {
				t.Fatal(e)
			}
			if limit, e := LookupWindow(o.DataDir); e != nil || limit != 1 {
				t.Fatal("native indexer may re-read deleted bodies", limit, e)
			}

		})
	}
}

func TestCompactEveryDurableBoundaryRecoversSamePermit(t *testing.T) {
	for _, phase := range []string{"intent", "copy", "candidate", "swap_intent", "old_rename", "new_rename", "cleanup_intent", "cleanup", "done"} {
		t.Run(phase, func(t *testing.T) {
			o, p, _ := fixture(t, true)
			failure := errors.New("interrupted node maintenance")
			fired := false
			e := compact(t.Context(), o, p, hooks{phase: func(at string) error {
				if at == phase {
					fired = true
					return failure
				}
				return nil
			}})
			if !errors.Is(e, failure) || !fired {
				t.Fatal("boundary not exercised", e)
			}
			if !errors.Is(CheckStart(o.DataDir), ErrPending) {
				t.Fatal("interruption reopened node")
			}
			wrong := p
			wrong.Token[0]++
			if e = Compact(t.Context(), o, wrong); !errors.Is(e, ErrUnsafe) {
				t.Fatal("different permit took over", e)
			}
			if e = Compact(t.Context(), o, p); e != nil {
				t.Fatal("same-permit recovery failed", e)
			}
			if e = Compact(t.Context(), o, p); e != nil {
				t.Fatal("completion retry failed", e)
			}
		})
	}
}
func TestCompactCapacityAndForeignLeasePreserveNode(t *testing.T) {
	o, p, _ := fixture(t, false)
	lease, e := AcquireRun(o.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	if e = Compact(t.Context(), o, p); !errors.Is(e, ErrPending) {
		t.Fatal("active node lease ignored", e)
	}
	if e = lease.Close(); e != nil {
		t.Fatal(e)
	}
	e = compact(t.Context(), o, p, hooks{free: func(string) (uint64, error) { return 0, nil }})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	path := filepath.Join(o.DataDir, "indexer-stablenet", "chaindata")
	disk, e := openDisk(path, true)
	if e != nil {
		t.Fatal(e)
	}
	if rawdb.ReadBlock(disk, rawdb.ReadCanonicalHash(disk, 1), 1) == nil {
		t.Fatal("capacity evicted source")
	}
	if e = disk.Close(); e != nil {
		t.Fatal(e)
	}
	if !errors.Is(CheckStart(o.DataDir), ErrPending) {
		t.Fatal("capacity halt expired")
	}
	if e = Compact(t.Context(), o, p); e != nil {
		t.Fatal("explicit capacity recovery failed", e)
	}
}
func TestCompactRejectsSymlinkAndUnknownJournalWithoutDeletion(t *testing.T) {
	o, p, _ := fixture(t, false)
	path := filepath.Join(o.DataDir, "indexer-stablenet", "chaindata")
	foreign := filepath.Join(t.TempDir(), "foreign")
	if e := os.WriteFile(foreign, []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(foreign, filepath.Join(path, "linked")); e != nil {
		t.Fatal(e)
	}
	if e := Compact(t.Context(), o, p); e == nil {
		t.Fatal("linked data copied/removed")
	}
	if data, e := os.ReadFile(foreign); e != nil || string(data) != "keep" {
		t.Fatal("foreign data changed", e)
	}
}
