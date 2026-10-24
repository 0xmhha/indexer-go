package nodebackend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	snapshot "github.com/0xmhha/indexer-go/x/stablenet/executor/checkpoint"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
)

// Manifest returns explicit expected identity, never a remotely selected head.
func Manifest(c Config, mode, coverage string) format.Manifest {
	return format.Manifest{Version: 1, Mode: mode, Coverage: coverage, Chain: execwire.Chain{ID: c.ChainID, Genesis: execwire.Hash(common.HexToHash(c.GenesisHash))}, Ref: execwire.Ref{Generation: 1, Height: c.AnchorHeight, Hash: execwire.Hash(common.HexToHash(c.AnchorHash))}, Root: execwire.Hash(common.HexToHash(c.AnchorRoot))}
}
func ExportGenesis(ctx context.Context, c Config, w io.Writer) error {
	var g core.Genesis
	if err := loadJSON(c.GenesisFile, &g, 16<<20, false); err != nil {
		return err
	}
	if _, _, err := settings(c, &g); err != nil {
		return err
	}
	return snapshot.Genesis(ctx, w, &g, Manifest(c, "genesis", "full"))
}

type NodeCheckpointReport struct {
	Chain                           execwire.Chain
	Ref                             execwire.Ref
	Root                            execwire.Hash
	HeaderAvailable, StateAvailable bool
	Scope                           string
}

// readCheckpoint opens existing chaindata and path trie READ ONLY. Paths are
// explicit because freezer placement is an operator decision. No node.New/eth.New.
func readCheckpoint(ctx context.Context, c Config, path, ancients string) (disk ethdb.Database, db state.Database, report NodeCheckpointReport, err error) {
	report = NodeCheckpointReport{Chain: Manifest(c, "checkpoint", "full").Chain, Ref: Manifest(c, "checkpoint", "full").Ref, Root: Manifest(c, "checkpoint", "full").Root, Scope: "exact configured checkpoint; no peer, consensus or retention proof"}
	if err = ctx.Err(); err != nil {
		return
	}
	if !filepath.IsAbs(path) || ancients != "" && !filepath.IsAbs(ancients) || !validHash(c.GenesisHash) || !validHash(c.AnchorHash) || !validHash(c.AnchorRoot) {
		err = execwire.ErrProtocol
		return
	}
	info, e := os.Stat(path)
	if e != nil {
		err = e
		return
	}
	if !info.IsDir() {
		err = execwire.ErrProtocol
		return
	}
	disk, err = rawdb.Open(rawdb.OpenOptions{Directory: path, AncientsDirectory: ancients, ReadOnly: true, Cache: 16, Handles: 16, Namespace: "execution-checkpoint"})
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, disk.Close())
			disk = nil
		}
	}()
	genesis := rawdb.ReadCanonicalHash(disk, 0)
	cfg := rawdb.ReadChainConfig(disk, genesis)
	if genesis != common.Hash(report.Chain.Genesis) || cfg == nil || cfg.ChainID == nil || cfg.ChainID.String() != c.ChainID {
		err = execwire.ErrProtocol
		return
	}
	h := rawdb.ReadHeader(disk, common.Hash(report.Ref.Hash), report.Ref.Height)
	if h == nil || rawdb.ReadCanonicalHash(disk, report.Ref.Height) != h.Hash() || h.Root != common.Hash(report.Root) {
		err = fmt.Errorf("checkpoint: missing or mismatched exact canonical header")
		return
	}
	report.HeaderAvailable = true
	scheme := rawdb.ReadStateScheme(disk)
	if scheme != rawdb.PathScheme && (scheme != "" || h.Root != types.EmptyRootHash) {
		err = fmt.Errorf("checkpoint: expected prepared path state database")
		return
	}
	db = state.NewDatabaseWithConfig(disk, &triedb.Config{PathDB: pathdb.ReadOnly, Preimages: true})
	if _, e = db.OpenTrie(h.Root); e != nil {
		err = errors.Join(fmt.Errorf("checkpoint: exact state root unavailable: %w", e), db.TrieDB().Close())
		db = nil
		return
	}
	report.StateAvailable = true
	return
}
func InspectCheckpoint(ctx context.Context, c Config, path, ancients string) (report NodeCheckpointReport, err error) {
	disk, db, report, err := readCheckpoint(ctx, c, path, ancients)
	if err != nil {
		return report, err
	}
	return report, errors.Join(ctx.Err(), db.TrieDB().Close(), disk.Close())
}
func ExportCheckpoint(ctx context.Context, c Config, path, ancients string, w io.Writer, targets []snapshot.Target) (err error) {
	disk, db, report, err := readCheckpoint(ctx, c, path, ancients)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.TrieDB().Close(), disk.Close()) }()
	coverage := "full"
	if targets != nil {
		coverage = "partial"
	}
	m := Manifest(c, "checkpoint", coverage)
	h := rawdb.ReadHeader(disk, common.Hash(report.Ref.Hash), report.Ref.Height)
	m.Header, err = rlp.EncodeToBytes(h)
	if err != nil {
		return err
	}
	return snapshot.Export(ctx, w, m, db, targets)
}
