package nodestore

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
)

// offlineDB deliberately uses no chainFreezer background writer. Both native
// stores are locked; no eth.New, downloader, blockchain repair or peer runs.
type offlineDB struct {
	ethdb.KeyValueStore
	ethdb.AncientStore
	ancient string
}

func (d *offlineDB) AncientDatadir() (string, error) { return d.ancient, nil }
func (d *offlineDB) Close() error {
	return errors.Join(d.AncientStore.Close(), d.KeyValueStore.Close())
}

func openDisk(path string, readonly bool) (ethdb.Database, error) {
	hot, e := rawdb.Open(rawdb.OpenOptions{Directory: path, ReadOnly: readonly, Cache: 16, Handles: 32, Namespace: "execution-retention"})
	if e != nil {
		return nil, e
	}
	chain := filepath.Join(path, "ancient", rawdb.ChainFreezerName)
	if _, e = os.Lstat(chain); errors.Is(e, os.ErrNotExist) {
		return hot, nil
	} else if e != nil {
		_ = hot.Close()
		return nil, e
	}
	cold, e := rawdb.NewChainFreezer(chain, "execution-retention", readonly)
	if e != nil {
		_ = hot.Close()
		return nil, e
	}
	return &offlineDB{KeyValueStore: hot, AncientStore: cold, ancient: filepath.Join(path, "ancient")}, nil
}

// State is an exact offline observation, never a consensus/finality proof.
type State struct {
	Head       execwire.Ref
	Root       execwire.Hash
	AnchorRoot execwire.Hash
}

func checkDisk(ctx context.Context, disk ethdb.Database, r retention.Request) (State, error) {
	if e := ctx.Err(); e != nil {
		return State{}, e
	}
	if rawdb.ReadCanonicalHash(disk, 0) != common.Hash(r.Chain.Genesis) {
		return State{}, ErrUnsafe
	}
	cfg := rawdb.ReadChainConfig(disk, common.Hash(r.Chain.Genesis))
	if cfg == nil || cfg.ChainID == nil || cfg.ChainID.String() != r.Chain.ID {
		return State{}, ErrUnsafe
	}
	anchor := rawdb.ReadHeader(disk, common.Hash(r.Anchor.Hash), r.Anchor.Height)
	if anchor == nil || rawdb.ReadCanonicalHash(disk, r.Anchor.Height) != anchor.Hash() {
		return State{}, ErrUnsafe
	}
	head := rawdb.ReadHeadBlock(disk)
	if head == nil || rawdb.ReadCanonicalHash(disk, head.NumberU64()) != head.Hash() {
		return State{}, ErrUnsafe
	}
	if count, e := disk.Ancients(); e == nil && count > head.NumberU64()+1 {
		return State{}, ErrUnsafe
	}
	st := State{Head: execwire.Ref{Height: head.NumberU64(), Hash: execwire.Hash(head.Hash())}, Root: execwire.Hash(head.Root()), AnchorRoot: execwire.Hash(anchor.Root)}
	if r.Head.Hash != (execwire.Hash{}) && (st.Head != r.Head || st.Root != r.HeadRoot) {
		return State{}, ErrUnsafe
	}
	if r.ACK.Hash != (execwire.Hash{}) {
		if rawdb.ReadCanonicalHash(disk, r.ACK.Height) != common.Hash(r.ACK.Hash) || rawdb.ReadCanonicalHash(disk, r.Tail.Height) != common.Hash(r.Tail.Hash) {
			return State{}, ErrUnsafe
		}
	}
	scheme := rawdb.ReadStateScheme(disk)
	if scheme != rawdb.PathScheme && (scheme != "" || head.Root() != types.EmptyRootHash) {
		return State{}, ErrUnsafe
	}
	db := state.NewDatabaseWithConfig(disk, &triedb.Config{PathDB: pathdb.ReadOnly, Preimages: true})
	defer func() { _ = db.TrieDB().Close() }()
	if _, e := db.OpenTrie(head.Root()); e != nil {
		return State{}, fmt.Errorf("%w: exact current state unavailable", ErrUnsafe)
	}
	if r.Tail.Hash != (execwire.Hash{}) {
		parent := rawdb.ReadHeader(disk, common.Hash(r.Tail.Hash), r.Tail.Height)
		if parent == nil {
			return State{}, ErrUnsafe
		}
		if _, e := db.OpenTrie(parent.Root); e != nil {
			return State{}, fmt.Errorf("%w: next replay parent unavailable", ErrUnsafe)
		}
	}
	return st, nil
}

func pruneHot(ctx context.Context, disk ethdb.Database, r retention.Request) error {
	batch := disk.NewBatch()
	for n := r.Anchor.Height + 1; n <= r.Through; n++ {
		if e := ctx.Err(); e != nil {
			return e
		}
		hash := rawdb.ReadCanonicalHash(disk, n)
		header := rawdb.ReadHeader(disk, hash, n)
		if hash == (common.Hash{}) || header == nil {
			return ErrUnsafe
		}
		body := rawdb.ReadBody(disk, hash, n)
		if body != nil {
			for _, tx := range body.Transactions {
				rawdb.DeleteTxLookupEntry(batch, tx.Hash())
			}
		}
		// Sidechain payloads inside this expired height range are no longer needed
		// for the 128-block supported rewind. Headers and number mappings stay.
		for _, h := range rawdb.ReadAllHashes(disk, n) {
			b := rawdb.ReadBody(disk, h, n)
			if b != nil {
				for _, tx := range b.Transactions {
					if block := rawdb.ReadTxLookupEntry(disk, tx.Hash()); block != nil && *block == n {
						rawdb.DeleteTxLookupEntry(batch, tx.Hash())
					}
				}
			}
			rawdb.DeleteBody(batch, h, n)
			rawdb.DeleteReceipts(batch, h, n)
		}
		rawdb.DeleteBody(batch, hash, n)
		rawdb.DeleteReceipts(batch, hash, n)
		if batch.ValueSize() > 1<<20 {
			if e := batch.Write(); e != nil {
				return e
			}
			batch.Reset()
		}
	}
	return batch.Write()
}

var chainTables = []string{rawdb.ChainFreezerHeaderTable, rawdb.ChainFreezerHashTable, rawdb.ChainFreezerBodiesTable, rawdb.ChainFreezerReceiptTable, rawdb.ChainFreezerDifficultyTable}

// Rebuild, rather than tail-truncate, the chain freezer: preserve genesis,
// every header/hash/TD and all table positions, empty only expired payloads.
// The entire operation occurs in the disposable candidate DB; a crash never
// exposes an in-place half-migrated freezer to the running node.
func pruneCold(ctx context.Context, path string, r retention.Request) error {
	source := filepath.Join(path, "ancient", rawdb.ChainFreezerName)
	if _, e := os.Stat(source); e != nil {
		return fmt.Errorf("%w: prepared standard chain freezer required", ErrUnsafe)
	}
	old, e := rawdb.NewChainFreezer(source, "execution-retention-source", true)
	if e != nil {
		return e
	}
	defer func() { _ = old.Close() }()
	tail, e := old.Tail()
	if e != nil {
		return e
	}
	if tail != 0 {
		return fmt.Errorf("%w: tail-deleted chain freezer", ErrUnsafe)
	}
	count, e := old.Ancients()
	if e != nil {
		return e
	}
	hot, e := rawdb.Open(rawdb.OpenOptions{Directory: path, ReadOnly: true, Cache: 16, Handles: 32})
	if e != nil {
		return e
	}
	defer func() { _ = hot.Close() }()
	target := max(count, r.Through+1)
	dest := source + ".retention-new"
	next, e := rawdb.NewChainFreezer(dest, "execution-retention-copy", false)
	if e != nil {
		return e
	}
	ok := false
	defer func() {
		if !ok {
			_ = next.Close()
		}
	}()
	var previous common.Hash
	for n := uint64(0); n < target; n++ {
		if e = ctx.Err(); e != nil {
			return e
		}
		values := make(map[string][]byte, len(chainTables))
		if n < count {
			for _, kind := range chainTables {
				values[kind], e = old.Ancient(kind, n)
				if e != nil {
					return e
				}
			}
		} else {
			hash := rawdb.ReadCanonicalHash(hot, n)
			values[rawdb.ChainFreezerHashTable] = hash.Bytes()
			values[rawdb.ChainFreezerHeaderTable] = rawdb.ReadHeaderRLP(hot, hash, n)
			values[rawdb.ChainFreezerDifficultyTable] = rawdb.ReadTdRLP(hot, hash, n)
			values[rawdb.ChainFreezerBodiesTable] = rawdb.ReadBodyRLP(hot, hash, n)
			values[rawdb.ChainFreezerReceiptTable] = rawdb.ReadReceiptsRLP(hot, hash, n)
		}
		var header types.Header
		if e = rlp.DecodeBytes(values[rawdb.ChainFreezerHeaderTable], &header); e != nil {
			return e
		}
		if header.Number == nil || header.Number.Uint64() != n || header.Hash() != common.BytesToHash(values[rawdb.ChainFreezerHashTable]) || n > 0 && header.ParentHash != previous || len(values[rawdb.ChainFreezerDifficultyTable]) == 0 {
			return ErrUnsafe
		}
		previous = header.Hash()
		_, e = next.ModifyAncients(func(w ethdb.AncientWriteOp) error {
			for _, kind := range chainTables {
				blob := values[kind]
				if n > r.Anchor.Height && n <= r.Through && (kind == rawdb.ChainFreezerBodiesTable || kind == rawdb.ChainFreezerReceiptTable) {
					blob = nil
				}
				if e = w.AppendRaw(kind, n, blob); e != nil {
					return e
				}
			}
			return nil
		})
		if e != nil {
			return e
		}
	}
	if e = next.Sync(); e != nil {
		return e
	}
	if e = next.Close(); e != nil {
		return e
	}
	ok = true
	if e = old.Close(); e != nil {
		return e
	}
	if e = removeTree(source); e != nil {
		return e
	}
	if e = os.Rename(dest, source); e != nil {
		return e
	}
	return syncDir(filepath.Dir(source))
}

// State history v0 is pinned to go-stablenet 4d17eea. IDs count state changes,
// NOT block heights; unchanged-state blocks need no entry. Unknown layouts
// fail closed. Keep the exact cutoff state (parent of first retained block),
// current/persistent dependencies and every later history.
func pruneStateHistory(ctx context.Context, disk ethdb.Database, path string, r retention.Request) error {
	ancient := filepath.Join(path, "ancient")
	if _, e := os.Stat(filepath.Join(ancient, rawdb.StateFreezerName)); errors.Is(e, os.ErrNotExist) {
		return nil
	}
	freezer, e := rawdb.NewStateFreezer(ancient, false)
	if e != nil {
		return e
	}
	defer func() { _ = freezer.Close() }()
	tail, e := freezer.Tail()
	if e != nil {
		return e
	}
	head, e := freezer.Ancients()
	if e != nil {
		return e
	}
	boundary := rawdb.ReadHeader(disk, rawdb.ReadCanonicalHash(disk, r.Through), r.Through)
	if boundary == nil {
		return ErrUnsafe
	}
	persistent := rawdb.ReadPersistentStateID(disk)
	target := tail
	batch := disk.NewBatch()
	var previous uint64
	var priorRoot common.Hash
	for id := tail + 1; id <= head; id++ {
		if e = ctx.Err(); e != nil {
			return e
		}
		blob := rawdb.ReadStateHistoryMeta(freezer, id)
		if len(blob) < 73 || blob[0] != 0 || (len(blob)-73)%20 != 0 {
			return fmt.Errorf("%w: state history metadata layout", ErrUnsafe)
		}
		parent := common.BytesToHash(blob[1:33])
		root := common.BytesToHash(blob[33:65])
		block := binary.BigEndian.Uint64(blob[65:73])
		if id > tail+1 && (block <= previous || parent != priorRoot) {
			return ErrUnsafe
		}
		header := rawdb.ReadHeader(disk, rawdb.ReadCanonicalHash(disk, block), block)
		if header == nil || header.Root != root {
			return ErrUnsafe
		}
		previous, priorRoot = block, root
		if block >= r.Through || root == boundary.Root || root == common.Hash(r.HeadRoot) || id >= persistent {
			break
		}
		mapped := rawdb.ReadStateID(disk, root)
		if mapped == nil || *mapped != id {
			return ErrUnsafe
		}
		rawdb.DeleteStateID(batch, root)
		target = id
	}
	if target == tail {
		return nil
	}
	if e = batch.Write(); e != nil {
		return e
	}
	if _, e = freezer.TruncateTail(target); e != nil {
		return e
	}
	return freezer.Sync()
}
