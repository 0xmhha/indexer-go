package executor

import (
	"context"
	"crypto/sha256"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/ethereum/go-ethereum/common"
	"reflect"
	"testing"
)

func beaconReplayFixture(t *testing.T) ReplayInput {
	t.Helper()
	var c [48]byte
	c[0] = 0xc0
	h := sha256.Sum256(c[:])
	h[0] = 1
	_, in := evmFixtureWith(t, []byte{0}, []evmTxSpec{{to: &account, blobHashes: []common.Hash{h}}})
	return in
}
func TestReplayBeaconPreludeRecordsStateWithoutUserTrace(t *testing.T) {
	in := beaconReplayFixture(t)
	before := dump(t, in.Database.DiskDB())
	out, e := Replay(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	if len(out.Steps) != 3 || out.Steps[0].Cause != execwire.PreBlock || out.Steps[0].TxIndex != -1 || out.Steps[0].TxHash != (execwire.Hash{}) || len(out.Steps[0].Storage) != 2 {
		t.Fatal("system state must have a distinct complete pre-block phase")
	}
	if out.CallTraces == nil || len(out.CallTraces.Frames) != 1 {
		t.Fatal("system operation must not be invented as a signed user call")
	}
	if !reflect.DeepEqual(before, dump(t, in.Database.DiskDB())) {
		t.Fatal("prelude replay modified original native state")
	}
	cfg := in.Chain.Config()
	future := uint64(1000)
	cfg.CancunTime = &future
	if _, e = Replay(t.Context(), in); !errors.Is(e, ErrUnsupportedPrelude) {
		t.Fatal("beacon header outside declared fork accepted", e)
	}
}
func TestReplayBeaconPreludeCancellationReturnsNoResult(t *testing.T) {
	in := beaconReplayFixture(t)
	before := dump(t, in.Database.DiskDB())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	in.Database = &cancelDatabase{Database: in.Database, cancel: cancel}
	out, e := Replay(ctx, in)
	if !errors.Is(e, context.Canceled) || out.Ref.Generation != 0 {
		t.Fatal("canceled system operation escaped as a replay result", e)
	}
	if !reflect.DeepEqual(before, dump(t, in.Database.DiskDB())) {
		t.Fatal("canceled prelude wrote backing state")
	}
}
