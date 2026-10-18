package capture_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestProjectionPreflightSnapshotAndFailures(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := openFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			o := newOracle()
			s := service(t, db, o)
			initialize(t, s)
			in := input(1, 1, common.Hash{2}, chain.GenesisHash)
			require.NoError(t, s.Commit(ctx, in))
			digest, err := plan.Digest()
			require.NoError(t, err)
			for _, name := range []string{"valid", "hash", "plan", "epoch", "bootstrap", "future", "missing", "manifest", "oversize", "joinedIO"} {
				t.Run(name, func(t *testing.T) {
					counted := &projectionReadDB{CaptureDatabase: db}
					block, expected, epoch := in.Manifest.Identity.Block, digest, uint64(1)
					var want error
					switch name {
					case "hash":
						block.Hash = common.Hash{99}
						want = capture.ErrCanonical
					case "plan":
						expected = common.Hash{99}
						want = capture.ErrConflict
					case "epoch":
						epoch++
						want = capture.ErrStale
					case "bootstrap":
						block.Number = 0
						want = capture.ErrGap
					case "future":
						block.Number = 2
						want = capture.ErrGap
					case "missing", "manifest", "oversize", "joinedIO":
						counted.fault = func(key []byte) ([]byte, error, bool) {
							if strings.HasPrefix(string(key), "facts/") {
								switch name {
								case "missing":
									return nil, port.ErrNotFound, true
								case "manifest":
									return []byte(`{}`), nil, true
								case "oversize":
									return bytes.Repeat([]byte{1}, port.CaptureBatchValueLimit+1), nil, true
								case "joinedIO":
									return nil, errors.Join(port.ErrNotFound, io.ErrUnexpectedEOF), true
								}
							}
							return nil, nil, false
						}
						switch name {
						case "missing":
							want = port.ErrNotFound
						case "manifest":
							want = capture.ErrConflict
						case "oversize":
							want = port.ErrInvalidData
						case "joinedIO":
							want = io.ErrUnexpectedEOF
						}
					}
					reader, err := capture.New(counted, chain, o)
					require.NoError(t, err)
					before := o.calls
					st, err := reader.ValidateProjectionBlock(ctx, epoch, expected, block)
					if want == nil {
						require.NoError(t, err)
						require.Equal(t, in.Manifest.Identity.Block, st.Through)
						if backend == "postgres" {
							require.Len(t, counted.batches, 2)
							require.Len(t, counted.batches[0], 2)
							require.Len(t, counted.batches[1], 1)
						}
					} else {
						require.ErrorIs(t, err, want)
					}
					require.Equal(t, 1, counted.views)
					require.Equal(t, before, o.calls, "preflight performs no RPC")
				})
			}
		})
	}
}
