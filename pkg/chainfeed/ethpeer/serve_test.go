package ethpeer

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/0xmhha/indexer-go/pkg/window"
)

func TestWindowPeerServesOnlyPublishedReferences(t *testing.T) {
	ctx := context.Background()
	chain := testchain.NewChain(1, nil)
	b := chain.AddBlock()
	id := model.FeedChain{ID: "1", GenesisHash: chain.Block(0).Block.Hash()}
	v := window.VolumeSpec{Root: t.TempDir(), ID: "serve-fixture", AllowDirectory: true}
	require.NoError(t, window.InitializeVolume(v))
	w, err := window.Create(window.Options{Volume: v, Path: filepath.Join(v.Root, "raw"), Chain: id, MaxBytes: 1 << 20, SegmentBytes: 64 << 10, MaxPayloadBytes: 32 << 10, MaxSegmentRecords: 8})
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	c := &Client{cfg: Config{Chain: id, ServeBlocks: 1, Timeout: 5 * time.Second}, cache: map[common.Hash]*cached{}, byHeight: map[uint64]common.Hash{}}
	var refs []window.Reference
	for i, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		var value []byte
		switch i {
		case 0:
			value, err = rlp.EncodeToBytes(b.Block.Header())
		case 1:
			value, err = rlp.EncodeToBytes(b.Block.Body())
		case 2:
			value, err = rlp.EncodeToBytes(b.Receipts)
		}
		require.NoError(t, err)
		r, err := w.Append(ctx, chainfeed.Envelope{Block: model.FeedBlock{Chain: id, Number: 1, Hash: b.Block.Hash()}, Kind: k, Codec: evmdata.Codec(k), Payload: value})
		require.NoError(t, err)
		refs = append(refs, r)
	}
	value, err := c.local(ctx, b.Block.Hash(), nil, model.DatasetBodies)
	require.NoError(t, err)
	require.Empty(t, value, "raw append alone never publishes to peers")
	require.NoError(t, c.ObserveCaptured(w, refs))
	for _, code := range []uint64{3, 5, 15} {
		var request any
		if code == 3 {
			origin, _ := rlp.EncodeToBytes(uint64(1))
			request = headerRequest{51, query{Origin: origin, Amount: 1}}
		} else {
			request = hashRequest{51, []common.Hash{b.Block.Hash()}}
		}
		encoded, err := rlp.EncodeToBytes(request)
		require.NoError(t, err)
		left, right := p2p.MsgPipe()
		done := make(chan error, 1)
		pc := &connection{ctx: ctx, peer: p2p.NewPeer(enode.ID{}, "fixture", nil), rw: left}
		go func() {
			done <- c.serve(pc, p2p.Msg{Code: code, Size: uint32(len(encoded)), Payload: bytes.NewReader(encoded)})
		}()
		msg, err := right.ReadMsg()
		require.NoError(t, err)
		var response packet
		require.NoError(t, msg.Decode(&response))
		require.EqualValues(t, 51, response.ID)
		require.Len(t, response.Items, 1)
		require.NoError(t, <-done)
		require.NoError(t, left.Close())
		require.NoError(t, right.Close())
	}
	require.Zero(t, w.Stats().ReadReservedBytes)
}
