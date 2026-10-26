package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob/proof"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/0xmhha/indexer-go/pkg/execution/blobdata"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func blobExecutionFixture(t *testing.T, name string, collected bool) (*Store, execwire.Result, []byte) {
	t.Helper()
	var f struct {
		Chain                 execwire.Chain
		ParentHeader, Payload []byte
		Baseline              port.ExecutionBaseline
	}
	raw, e := os.ReadFile("../../../testdata/execution/" + name + ".json")
	require.NoError(t, e)
	require.NoError(t, json.Unmarshal(raw, &f))
	profile := evm.New("evm")
	if collected {
		result, e := execwire.UnmarshalResult(f.Payload)
		require.NoError(t, e)
		var response []byte
		if name == "evm-blobs" {
			v := proof.Response{Version: 1, Block: blobdata.Block(f.Chain, result.Ref), Entries: make([]proof.Entry, 3)}
			for i := range v.Entries {
				v.Entries[i].Index = uint64(i)
				v.Entries[i].Commitment[0] = 0xc0
				v.Entries[i].Proof[0] = 0xc0
			}
			response, e = rlp.EncodeToBytes(v)
			require.NoError(t, e)
		}
		f.Payload, e = execution.AttachBlobInput(t.Context(), profile, f.Chain, f.Payload, "retained-sidecars", response)
		require.NoError(t, e)
	}
	r, e := execwire.UnmarshalResult(f.Payload)
	require.NoError(t, e)
	s := newTestStore(t)
	parent, e := profile.DecodeHeaderRLP(f.ParentHeader)
	require.NoError(t, e)
	require.NoError(t, s.SetBlock(t.Context(), parent))
	require.NoError(t, s.InitExecution(t.Context(), port.ExecutionConfig{Chain: f.Chain, Anchor: f.Baseline.Ref, AnchorRoot: f.Baseline.Root, PartitionBlocks: 2}))
	require.NoError(t, s.SeedExecution(t.Context(), f.Baseline))
	prepared, e := execution.Prepare(profile, f.Chain, f.Payload)
	require.NoError(t, e)
	backend := execution.StoreBackend{Store: s}
	for _, w := range port.ExecutionWriters() {
		require.NoError(t, backend.Part(t.Context(), w, prepared))
	}
	return s, r, f.Payload
}
func blobPublication(r execwire.Result, raw []byte) port.ExecutionPublication {
	return port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Root: r.StateRoot, Digest: execwire.Hash(sha256.Sum256(raw)), CallTraces: r.CallTraces != nil, Blobs: r.BlobBundle != nil}
}
func TestExecutionBlobsDurabilityVisibilityAndExactACK(t *testing.T) {
	s := newTestStore(t)
	_, ok := any(s).(port.ExecutionBlobWriter)
	require.True(t, ok, "same transactions part must retain exact full blob input before publication/ACK")
	_, ok = any(s).(port.ExecutionBlobReader)
	require.True(t, ok, "published blobs must be readable without RPC or raw fallback")
	s, r, raw := blobExecutionFixture(t, "evm-blobs", true)
	query := port.ExecutionBlobQuery{Height: r.Ref.Height, Limit: 1}
	_, e := s.ExecutionBlobs(t.Context(), query)
	require.ErrorIs(t, e, port.ErrNotFound)
	pub := blobPublication(r, raw)
	failure := errors.New("fresh unavailable")
	_, e = s.CommitExecution(t.Context(), pub, func(context.Context) error { return nil }, func(context.Context) error { return failure })
	require.ErrorIs(t, e, failure)
	_, e = s.ExecutionBlobs(t.Context(), query)
	require.ErrorIs(t, e, port.ErrNotFound)
	fresh := 0
	_, e = s.CommitExecution(t.Context(), pub, func(context.Context) error { return nil }, func(context.Context) error { fresh++; return nil })
	require.NoError(t, e)
	require.Equal(t, 1, fresh)
	page, e := s.ExecutionBlobs(t.Context(), query)
	require.NoError(t, e)
	require.EqualValues(t, 3, page.Total)
	require.Len(t, page.Entries, 1)
	require.EqualValues(t, 1, *page.Next)
	query.After = *page.Next
	_, e = s.ExecutionBlobs(t.Context(), query)
	require.ErrorIs(t, e, port.ErrInvalidData)
	query.Epoch = page.Epoch
	query.Hash = page.Ref.Hash
	page, e = s.ExecutionBlobs(t.Context(), query)
	require.NoError(t, e)
	require.EqualValues(t, 1, page.Entries[0].Index)
	tx := page.References[0].TransactionHash
	page, e = s.ExecutionBlobs(t.Context(), port.ExecutionBlobQuery{Transaction: &tx, Limit: 4})
	require.NoError(t, e)
	require.EqualValues(t, 3, page.Total)
	require.Len(t, page.Entries, 3)
	ok, e = s.ConfirmExecution(t.Context(), raw, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, e)
	require.True(t, ok)
	cctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, e = s.ExecutionBlobs(cctx, port.ExecutionBlobQuery{Height: 1, Limit: 1})
	require.ErrorIs(t, e, context.Canceled)
	var meta []byte
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT metadata FROM exec_blob_blocks WHERE height=$1", int64(1)).Scan(&meta))
	_, e = s.pool.Exec(t.Context(), "UPDATE exec_blob_blocks SET metadata=$1 WHERE height=$2", []byte("damaged metadata"), int64(1))
	require.NoError(t, e)
	_, e = s.ExecutionBlobs(t.Context(), port.ExecutionBlobQuery{Height: 1, Limit: 1})
	require.Error(t, e)
	_, e = s.ConfirmExecution(t.Context(), raw, func(context.Context, execwire.Ref) error { t.Fatal("damaged metadata reached fresh"); return nil })
	require.Error(t, e)
	_, e = s.pool.Exec(t.Context(), "UPDATE exec_blob_blocks SET metadata=$1 WHERE height=$2", meta, int64(1))
	require.NoError(t, e)
	// Invalid proof bytes of the correct SQL length cannot pass even if other
	// database checksums are valid. Fresh must not run after this failure.
	bad := make([]byte, 48)
	bad[0] = 0x80
	_, e = s.pool.Exec(t.Context(), "UPDATE exec_blob_entries SET proof=$1 WHERE height=$2 AND ordinal=$3", bad, int64(r.Ref.Height), int32(0))
	require.NoError(t, e)
	_, e = s.ExecutionBlobs(t.Context(), port.ExecutionBlobQuery{Height: 1, Limit: 1})
	require.Error(t, e)
	fresh = 0
	_, e = s.ConfirmExecution(t.Context(), raw, func(context.Context, execwire.Ref) error { fresh++; return nil })
	require.Error(t, e)
	require.Zero(t, fresh)
	good := make([]byte, 48)
	good[0] = 0xc0
	_, e = s.pool.Exec(t.Context(), "UPDATE exec_blob_entries SET proof=$1 WHERE height=$2 AND ordinal=$3", good, int64(r.Ref.Height), int32(0))
	require.NoError(t, e)
	_, e = s.BeginExecutionRewind(t.Context(), r.Ref, r.StateRoot)
	require.NoError(t, e)
	_, e = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, e)
	r.Ref.Generation++
	raw, e = execwire.MarshalResult(r)
	require.NoError(t, e)
	ok, e = s.ConfirmExecution(t.Context(), raw, func(_ context.Context, ref execwire.Ref) error { require.Equal(t, r.Ref, ref); return nil })
	require.NoError(t, e)
	require.True(t, ok)
	page, e = s.ExecutionBlobs(t.Context(), port.ExecutionBlobQuery{Height: 1, Limit: 4})
	require.NoError(t, e)
	require.EqualValues(t, 2, page.Epoch)
	require.EqualValues(t, 1, page.Ref.Generation)
	require.Equal(t, pub.Digest, page.Digest)
	// Missing rows are unavailable, including for a page not requesting that row.
	_, e = s.pool.Exec(t.Context(), "DELETE FROM exec_blob_entries WHERE height=$1 AND ordinal=$2", int64(1), int32(2))
	require.NoError(t, e)
	_, e = s.ExecutionBlobs(t.Context(), port.ExecutionBlobQuery{Height: 1, Limit: 1})
	require.Error(t, e)
	_, e = s.ConfirmExecution(t.Context(), raw, func(context.Context, execwire.Ref) error { t.Fatal("missing blob invoked fresh"); return nil })
	require.Error(t, e)
	c, e := s.ExecutionStatus(t.Context())
	require.NoError(t, e)
	anchor := c.Anchor
	anchor.Generation = c.Generation
	_, e = s.BeginExecutionRewind(t.Context(), anchor, c.AnchorRoot)
	require.NoError(t, e)
	_, e = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, e)
	_, e = s.ExecutionBlobs(t.Context(), port.ExecutionBlobQuery{Height: 1, Limit: 1, Epoch: page.Epoch, Hash: page.Ref.Hash})
	require.ErrorIs(t, e, port.ErrExecutionStale)
	var count int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM exec_blob_blocks").Scan(&count))
	require.Zero(t, count)
}
func TestExecutionBlobsEmptyDiffersFromNotCollected(t *testing.T) {
	for _, collected := range []bool{false, true} {
		t.Run(map[bool]string{false: "not collected", true: "collected no references"}[collected], func(t *testing.T) {
			s, r, raw := blobExecutionFixture(t, "evm-storage", collected)
			_, e := s.CommitExecution(t.Context(), blobPublication(r, raw), func(context.Context) error { return nil }, func(context.Context) error { return nil })
			require.NoError(t, e)
			page, e := s.ExecutionBlobs(t.Context(), port.ExecutionBlobQuery{Height: 1, Limit: 4})
			if !collected {
				require.ErrorIs(t, e, port.ErrNotFound)
				return
			}
			require.NoError(t, e)
			require.Equal(t, "no_blob_references", page.Availability)
			require.Empty(t, page.Entries)
			require.NotNil(t, page.Entries)
		})
	}
}
