package evmtrace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
)

type corruptTraceReader struct {
	port.CaptureReader
	edit func([]byte) []byte
}

func (r corruptTraceReader) Get(key []byte) ([]byte, error) {
	raw, e := r.CaptureReader.Get(key)
	if e != nil {
		return nil, e
	}
	var in capture.Supplement
	if json.Unmarshal(raw, &in) == nil && in.Codec == Codec {
		in.Data = r.edit(in.Data)
		in.Digest = sha256.Sum256(in.Data)
		return json.Marshal(in)
	}
	return raw, nil
}

type corruptTraceDB struct {
	port.CaptureDatabase
	edit func([]byte) []byte
}

func (d corruptTraceDB) CaptureView(ctx context.Context, c model.FeedChain, fn func(port.CaptureReader) error) error {
	return d.CaptureDatabase.CaptureView(ctx, c, func(r port.CaptureReader) error { return fn(corruptTraceReader{CaptureReader: r, edit: d.edit}) })
}
func TestCallTraceStoredRootReverified(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := traceDatabase(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			f := nativeFixture(t)
			fresh := &traceFresh{fixture: f}
			store, id, d := initializeTrace(t, db, f, fresh)
			wire := &traceWire{fixture: f}
			client, _ := traceClient(t, wire)
			supplier, e := New(store, evm.New("evm"), client)
			require.NoError(t, e)
			_, e = supplier.Acquire(t.Context(), 1, id, d)
			require.NoError(t, e)
			// The storage digest is deliberately recomputed, so the decoder's root
			// consistency check, not just a checksum, must reject this forged value.
			corrupted := corruptTraceDB{CaptureDatabase: db, edit: func(raw []byte) []byte {
				var b Bundle
				require.NoError(t, json.Unmarshal(raw, &b))
				var roots []map[string]any
				require.NoError(t, json.Unmarshal(b.Response, &roots))
				roots[0]["result"].(map[string]any)["value"] = "0x8"
				response, e := json.Marshal(roots)
				b.Response = response
				require.NoError(t, e)
				out, e := json.Marshal(b)
				require.NoError(t, e)
				return out
			}}
			reader, e := capture.NewReader(corrupted, id.Block.Chain, nil)
			require.NoError(t, e)
			_, e = ReadSnapshot(t.Context(), reader, evm.New("evm"), 1, id, d)
			require.ErrorIs(t, e, ErrTrace)
		})
	}
}
