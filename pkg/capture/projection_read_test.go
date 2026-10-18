package capture_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type projectionReadDB struct {
	port.CaptureDatabase
	views     int
	batches   [][][]byte
	fault     func([]byte) ([]byte, error, bool)
	onPayload func()
}
type projectionReadReader struct {
	port.CaptureReader
	db *projectionReadDB
}

func (r projectionReadReader) Get(key []byte) ([]byte, error) {
	if r.db.fault != nil {
		if b, e, intercept := r.db.fault(key); intercept {
			return b, e
		}
	}
	if strings.HasSuffix(string(key), "/bodies") && r.db.onPayload != nil {
		r.db.onPayload()
	}
	return r.CaptureReader.Get(key)
}

type projectionReadBatch struct {
	projectionReadReader
	batch port.CaptureBatchReader
}

func (r projectionReadBatch) GetMany(keys [][]byte) (map[string][]byte, error) {
	r.db.batches = append(r.db.batches, keys)
	return r.batch.GetMany(keys)
}
func (d *projectionReadDB) CaptureView(ctx context.Context, chain model.FeedChain, fn func(port.CaptureReader) error) error {
	d.views++
	return d.CaptureDatabase.CaptureView(ctx, chain, func(r port.CaptureReader) error {
		wrapped := projectionReadReader{r, d}
		if batch, ok := r.(port.CaptureBatchReader); ok && d.fault == nil {
			return fn(projectionReadBatch{wrapped, batch})
		}
		return fn(wrapped)
	})
}
func projectionPayloadMap(in capture.Input) map[model.DatasetKind][]byte {
	out := map[model.DatasetKind][]byte{}
	for _, p := range in.Payloads {
		out[p.Kind] = p.Bytes
	}
	return out
}

func TestProjectionInputOneSnapshotAndLargePayload(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := openFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			o := newOracle()
			s := service(t, db, o)
			initialize(t, s)
			in := input(1, 1, common.Hash{2}, chain.GenesisHash)
			in.Payloads[0].Bytes = bytes.Repeat([]byte{7}, 2<<20)
			in.Manifest.Datasets[0].Digest = sha256.Sum256(in.Payloads[0].Bytes)
			require.NoError(t, s.Commit(ctx, in))
			counted := &projectionReadDB{CaptureDatabase: db}
			r, e := capture.NewReader(counted, chain, nil)
			require.NoError(t, e)
			before := o.calls
			got, e := r.ReadProjectionInput(ctx, 1, in.Manifest.Identity)
			require.NoError(t, e)
			require.Equal(t, 1, counted.views)
			require.Equal(t, projectionPayloadMap(in), projectionPayloadMap(got))
			require.Equal(t, in.ParentHash, got.ParentHash)
			require.Equal(t, before, o.calls)
			if backend == "postgres" {
				require.Len(t, counted.batches, 1)
				require.Len(t, counted.batches[0], 3)
				for _, key := range counted.batches[0] {
					require.NotContains(t, []string{"bodies", "receipts"}, string(key[strings.LastIndex(string(key), "/")+1:]), "large payload must not enter the 1MiB metadata batch")
				}
			}
			got.Payloads[0].Bytes[0] ^= 1
			again, e := r.ReadProjectionInput(ctx, 1, in.Manifest.Identity)
			require.NoError(t, e)
			require.Equal(t, projectionPayloadMap(in), projectionPayloadMap(again), "caller owns returned bytes")
			for _, cause := range []error{port.ErrNotFound, errors.Join(port.ErrNotFound, errInjected)} {
				counted.fault = func(key []byte) ([]byte, error, bool) {
					if strings.HasSuffix(string(key), "/bodies") {
						return nil, cause, true
					}
					return nil, nil, false
				}
				got, e = r.ReadProjectionInput(ctx, 1, in.Manifest.Identity)
				require.ErrorIs(t, e, cause)
				require.Equal(t, capture.Input{}, got)
			}
			counted.fault = func(key []byte) ([]byte, error, bool) {
				if strings.HasSuffix(string(key), "/bodies") {
					return []byte("corrupt"), nil, true
				}
				return nil, nil, false
			}
			got, e = r.ReadProjectionInput(ctx, 1, in.Manifest.Identity)
			require.Error(t, e)
			require.Equal(t, capture.Input{}, got)
			// Joined metadata errors are not silently converted to missing records.
			counted.fault = func(key []byte) ([]byte, error, bool) {
				if bytes.HasPrefix(key, []byte("canonical/")) {
					return nil, errors.Join(port.ErrNotFound, errInjected), true
				}
				return nil, nil, false
			}
			_, e = r.ReadProjectionInput(ctx, 99, in.Manifest.Identity)
			require.ErrorIs(t, e, errInjected)
			counted.fault = nil
			counted.fault = func(key []byte) ([]byte, error, bool) {
				if string(key) == "state" {
					return make([]byte, port.CaptureBatchValueLimit+1), nil, true
				}
				return nil, nil, false
			}
			got, e = r.ReadProjectionInput(ctx, 1, in.Manifest.Identity)
			require.ErrorIs(t, e, port.ErrInvalidData)
			require.Equal(t, capture.Input{}, got)
			counted.fault = nil
			require.NoError(t, s.Reset(ctx, 1, model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}))
			_, e = r.ReadProjectionInput(ctx, 1, in.Manifest.Identity)
			require.ErrorIs(t, e, capture.ErrStale)
			_, e = r.ReadProjectionInput(ctx, 2, in.Manifest.Identity)
			require.ErrorIs(t, e, capture.ErrGap)
			_, e = r.Read(ctx, in.Manifest.Identity)
			require.NoError(t, e, "historical fork read remains supported")
		})
	}
}

func TestProjectionInputSnapshotAndRetainedGeneration(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := openFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			s := service(t, db, newOracle())
			initialize(t, s)
			in := input(1, 1, common.Hash{2}, chain.GenesisHash)
			require.NoError(t, s.Commit(ctx, in))
			once := sync.Once{}
			counted := &projectionReadDB{CaptureDatabase: db, onPayload: func() { once.Do(func() { require.NoError(t, s.Reset(ctx, 1, in.Manifest.Identity.Block)) }) }}
			r, e := capture.NewReader(counted, chain, nil)
			require.NoError(t, e)
			got, e := r.ReadProjectionInput(ctx, 1, in.Manifest.Identity)
			require.NoError(t, e)
			require.Equal(t, in.Manifest, got.Manifest, "one snapshot remains internally consistent across reset")
			require.ErrorIs(t, r.ValidateProjection(ctx, 1, in.Manifest.Identity), capture.ErrStale, "post-decode validation must still detect the new epoch")
			got, e = r.ReadProjectionInput(ctx, 2, in.Manifest.Identity)
			require.NoError(t, e)
			require.EqualValues(t, 1, got.Manifest.Identity.Generation, "retained canonical inputs keep their original generation")
		})
	}
}

func TestProjectionInputPermanentObjectsAndLimits(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := openFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			area, opts := objectArea(t)
			defer func() { require.NoError(t, area.Close()) }()
			s, e := capture.NewWithObjects(db, chain, newOracle(), area)
			require.NoError(t, e)
			initialize(t, s)
			original := input(1, 1, common.Hash{2}, chain.GenesisHash)
			in := external(t, area, original)
			require.NoError(t, s.Commit(ctx, in))
			counted := &projectionReadDB{CaptureDatabase: db}
			r, e := capture.NewReader(counted, chain, area)
			require.NoError(t, e)
			got, e := r.ReadProjectionInput(ctx, 1, in.Manifest.Identity)
			require.NoError(t, e)
			require.Equal(t, 1, counted.views)
			require.Empty(t, got.Objects)
			require.Equal(t, projectionPayloadMap(original), projectionPayloadMap(got))
			plain, e := capture.NewReader(db, chain, nil)
			require.NoError(t, e)
			_, e = plain.ReadProjectionInput(ctx, 1, in.Manifest.Identity)
			require.ErrorIs(t, e, capture.ErrObjectsUnavailable)
			for _, binding := range []string{"missing", "retired"} {
				counted.fault = func(key []byte) ([]byte, error, bool) {
					if bytes.HasPrefix(key, []byte("objects/")) {
						if binding == "missing" {
							return nil, port.ErrNotFound, true
						}
						value, e := json.Marshal(map[string]any{"ref": in.Objects[0].Ref, "retired": true})
						return value, e, true
					}
					return nil, nil, false
				}
				got, e = r.ReadProjectionInput(ctx, 1, in.Manifest.Identity)
				require.Error(t, e)
				if binding == "retired" {
					require.ErrorIs(t, e, capture.ErrObjectRetired)
				}
				require.Equal(t, capture.Input{}, got)
			}
			large := input(2, 1, common.Hash{3}, in.Manifest.Identity.Block.Hash)
			large.Payloads[0].Bytes = bytes.Repeat([]byte{9}, (8<<20)+1)
			large.Manifest.Datasets[0].Digest = sha256.Sum256(large.Payloads[0].Bytes)
			large = external(t, area, large)
			require.NoError(t, s.Commit(ctx, large))
			got, e = s.ReadProjectionInput(ctx, 1, large.Manifest.Identity)
			require.ErrorIs(t, e, port.ErrInvalidData)
			require.ErrorIs(t, e, capture.ErrProjectionInputLimit)
			require.Equal(t, capture.Input{}, got)
			require.NoError(t, s.CopyPayload(ctx, large.Manifest.Identity, large.Objects[0].Kind, io.Discard), "streaming API retains its larger object support")
			counted.fault = nil
			path := filepath.Join(opts.Path, in.Objects[0].Ref.ID.Hex()[2:]+".object")
			contents, e := os.ReadFile(path)
			require.NoError(t, e)
			contents[len(contents)-1] ^= 1
			require.NoError(t, os.WriteFile(path, contents, 0600))
			got, e = r.ReadProjectionInput(ctx, 1, in.Manifest.Identity)
			require.ErrorIs(t, e, window.ErrCorrupt)
			require.Equal(t, capture.Input{}, got)
			require.NoError(t, os.Remove(path))
			got, e = r.ReadProjectionInput(ctx, 1, in.Manifest.Identity)
			require.ErrorIs(t, e, os.ErrNotExist)
			require.Equal(t, capture.Input{}, got)
		})
	}
}

func TestProjectionInputEpochRace(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	s := service(t, db, newOracle())
	initialize(t, s)
	in := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, in))
	done := make(chan error, 1)
	go func() { _, e := s.ReadProjectionInput(ctx, 1, in.Manifest.Identity); done <- e }()
	require.NoError(t, s.Reset(ctx, 1, in.Manifest.Identity.Block))
	if e := <-done; e != nil {
		require.ErrorIs(t, e, capture.ErrStale)
	}
	require.ErrorIs(t, s.ValidateProjection(ctx, 1, in.Manifest.Identity), capture.ErrStale)
}

func TestProjectionInputAggregateObjectBudget(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	area, _ := objectArea(t)
	defer func() { require.NoError(t, area.Close()) }()
	s, e := capture.NewWithObjects(db, chain, newOracle(), area)
	require.NoError(t, e)
	data := bytes.Repeat([]byte{11}, 8<<20)
	digest := sha256.Sum256(data)
	ref, e := area.Put(ctx, digest, int64(len(data)), bytes.NewReader(data))
	require.NoError(t, e)
	p := model.CapturePlan{Version: 1}
	for _, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetTransactions, model.DatasetReceipts, model.DatasetLogs, model.DatasetAccounts, model.DatasetCode, model.DatasetStorage, model.DatasetTraces} {
		p.Required = append(p.Required, model.DatasetRequirement{Kind: k, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}})
	}
	require.NoError(t, s.Initialize(ctx, p, model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}, nil))
	pd, e := p.Digest()
	require.NoError(t, e)
	in := capture.Input{Manifest: model.CaptureManifest{Identity: model.CaptureIdentity{Block: model.FeedBlock{Chain: chain, Number: 1, Hash: common.Hash{2}}, Generation: 1, PlanVersion: 1, PlanDigest: pd}}, ParentHash: chain.GenesisHash}
	for _, r := range p.Required {
		in.Manifest.Datasets = append(in.Manifest.Datasets, model.DatasetEvidence{Kind: r.Kind, Coverage: r.Coverage, Status: model.DatasetPersisted, Verification: model.VerificationHashBound, Codec: "fixture.v1", Source: "fixture", Digest: digest})
		in.Objects = append(in.Objects, capture.ObjectPayload{Kind: r.Kind, Ref: ref})
	}
	require.NoError(t, s.Commit(ctx, in))
	got, e := s.ReadProjectionInput(ctx, 1, in.Manifest.Identity)
	require.ErrorIs(t, e, port.ErrInvalidData)
	require.Equal(t, capture.Input{}, got, "9 valid 8MiB references exceed the materialization budget")
}
