package evmblob_test

import (
	"context"
	"io"
	"sync/atomic"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob"
	"github.com/0xmhha/indexer-go/pkg/chains/arc"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type noIO struct{ calls atomic.Int32 }

func (n *noIO) CaptureUpdate(context.Context, model.FeedChain, func(port.CaptureWriter) error) error {
	n.calls.Add(1)
	return port.ErrInvalidData
}
func (n *noIO) CaptureView(context.Context, model.FeedChain, func(port.CaptureReader) error) error {
	n.calls.Add(1)
	return port.ErrInvalidData
}
func (n *noIO) HashAt(context.Context, uint64) (common.Hash, error) {
	n.calls.Add(1)
	return common.Hash{}, port.ErrInvalidData
}
func (n *noIO) Open(context.Context, model.FeedBlock, []evmblob.Reference) (io.ReadCloser, error) {
	n.calls.Add(1)
	return nil, port.ErrInvalidData
}
func TestBlobUnsupportedProfileRejectsBeforeIO(t *testing.T) {
	io := &noIO{}
	chain := model.FeedChain{ID: "5042002", GenesisHash: common.Hash{1}}
	store, e := capture.New(io, chain, io)
	require.NoError(t, e)
	_, e = evmblob.New(store, arc.New(), io)
	require.ErrorIs(t, e, evmblob.ErrUnsupported)
	reader, e := capture.NewReader(io, chain, nil)
	require.NoError(t, e)
	d, e := evmblob.Descriptor("blobs-v1", 1, evmblob.Definition{Provider: "fixture", Profile: "arc", Format: evmblob.Format})
	require.NoError(t, e)
	id := model.CaptureIdentity{Block: model.FeedBlock{Chain: chain, Number: 1, Hash: common.Hash{2}}, Generation: 1}
	_, e = evmblob.ReadSnapshot(t.Context(), reader, arc.New(), 1, id, d)
	require.ErrorIs(t, e, evmblob.ErrUnsupported)
	require.Zero(t, io.calls.Load(), "no DB, provider or canonical RPC for unsupported policy")
}
