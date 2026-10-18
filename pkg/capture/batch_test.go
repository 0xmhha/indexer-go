package capture

import (
	"context"
	"io"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

type ordinaryStream struct {
	e   chainfeed.Envelope
	err error
}

func (s ordinaryStream) Next(context.Context) (chainfeed.Envelope, error) { return s.e, s.err }
func (s ordinaryStream) Close() error                                     { return nil }

type groupedStream struct {
	ordinaryStream
	inputs []chainfeed.Envelope
}

func (s groupedStream) NextBatch(context.Context) ([]chainfeed.Envelope, error) {
	return s.inputs, s.err
}

func TestNextEnvelopesCompatibilityAndValidation(t *testing.T) {
	ctx := context.Background()
	e := chainfeed.Envelope{Block: model.FeedBlock{Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, Number: 1, Hash: common.Hash{2}}, Kind: model.DatasetHeaders, Codec: "fixture.v1", Payload: []byte("header")}
	inputs, err := nextEnvelopes(ctx, ordinaryStream{e: e}, e.Block.Chain, 16)
	require.NoError(t, err)
	require.Equal(t, []chainfeed.Envelope{e}, inputs)
	_, err = nextEnvelopes(ctx, ordinaryStream{err: io.EOF}, e.Block.Chain, 16)
	require.ErrorIs(t, err, io.EOF)
	for _, name := range []string{"valid", "empty", "tooMany", "duplicate", "block", "chain", "size"} {
		t.Run(name, func(t *testing.T) {
			second := e
			second.Kind = model.DatasetBodies
			batch := []chainfeed.Envelope{e, second}
			switch name {
			case "empty":
				batch = nil
			case "tooMany":
				batch = append(batch, e, second)
			case "duplicate":
				batch[1] = e
			case "block":
				batch[1].Block.Number++
			case "chain":
				batch[1].Block.Chain.ID = "2"
			case "size":
				batch[1].Payload = make([]byte, 17)
			}
			got, err := nextEnvelopes(ctx, groupedStream{inputs: batch}, e.Block.Chain, 16)
			if name == "valid" {
				require.NoError(t, err)
				require.Equal(t, batch, got)
			} else {
				require.Error(t, err)
				require.Empty(t, got)
			}
		})
	}
}
