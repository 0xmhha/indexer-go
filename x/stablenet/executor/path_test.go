package executor

import (
	"context"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
)

func TestReplayPathDatabaseUsesRetainedParentWithoutWriting(t *testing.T) {
	path := *pathdb.Defaults
	path.StateHistory = 256
	disk, in := evmFixtureDatabase(t, []byte{0x60, 0x03, 0x60, 0x00, 0x55, 0x00}, []evmTxSpec{{to: &account}}, &triedb.Config{PathDB: &path})
	before := dump(t, disk)
	out, err := Replay(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if common.Hash(out.StateRoot) != in.Block.Root() {
		t.Fatal("path-mode parent did not reproduce state")
	}
	if !reflect.DeepEqual(before, dump(t, disk)) {
		t.Fatal("path-mode replay changed disk state")
	}
}

type rootRequests struct {
	state.Database
	roots []common.Hash
}

func (d *rootRequests) OpenTrie(root common.Hash) (state.Trie, error) {
	d.roots = append(d.roots, root)
	return d.Database.OpenTrie(root)
}

func TestReplayUnavailableParentFailsWithoutLatestFallback(t *testing.T) {
	disk, in := evmFixture(t)
	parent := types.CopyHeader(in.Parent)
	parent.Root = common.Hash{99}
	in.Parent = parent
	h := in.Block.Header()
	h.ParentHash = parent.Hash()
	in.Block = types.NewBlockWithHeader(h).WithBody(in.Block.Transactions(), in.Block.Uncles())
	db := &rootRequests{Database: in.Database}
	in.Database = db
	before := dump(t, disk)
	out, err := Replay(context.Background(), in)
	if err == nil || out.Ref.Generation != 0 {
		t.Fatal("missing parent accepted")
	}
	if len(db.roots) != 1 || db.roots[0] != parent.Root {
		t.Fatal("replay tried another state instead of the declared parent")
	}
	if !reflect.DeepEqual(before, dump(t, disk)) {
		t.Fatal("failed parent lookup wrote node state")
	}
}
