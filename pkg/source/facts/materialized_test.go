package facts_test

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/source/facts"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestFactsMaterializedWriterBoundaries(t *testing.T) {
	original := dbFactory(t, "pebble")()
	defer func() { require.NoError(t, original.Close()) }()
	f := build(t, original, false)
	for _, backend := range []string{"pebble", "postgres"} {
		for _, failure := range []string{"none", "fresh", "large", "corrupt", "missing"} {
			t.Run(backend+"/"+failure, func(t *testing.T) {
				db := dbFactory(t, backend)()
				defer func() { require.NoError(t, db.Close()) }()
				fresh := &oracle{chain: f.chain}
				v := window.VolumeSpec{Root: t.TempDir(), ID: "facts-materialized", AllowDirectory: true}
				require.NoError(t, window.InitializeVolume(v))
				path := filepath.Join(v.Root, "objects")
				objects, e := window.CreateObjects(window.ObjectOptions{Volume: v, Path: path, Chain: f.feed, MaxBytes: 32 << 20})
				require.NoError(t, e)
				defer func() { require.NoError(t, objects.Close()) }()
				store, e := capture.NewWithObjects(db, f.feed, fresh, objects)
				require.NoError(t, e)
				require.NoError(t, store.Initialize(ctx, evmdata.BasePlan(), model.FeedBlock{Chain: f.feed, Hash: f.feed.GenesisHash}, nil))
				in := f.inputs[0]
				in.Manifest.Datasets = append([]model.DatasetEvidence(nil), in.Manifest.Datasets...)
				in.Payloads = nil
				for _, p := range f.inputs[0].Payloads {
					if failure == "large" && p.Kind == model.DatasetBodies {
						p.Bytes = bytes.Repeat([]byte{9}, (8<<20)+1)
						for i := range in.Manifest.Datasets {
							if in.Manifest.Datasets[i].Kind == p.Kind {
								in.Manifest.Datasets[i].Digest = sha256.Sum256(p.Bytes)
							}
						}
					}
					ref, e := objects.Put(ctx, sha256.Sum256(p.Bytes), int64(len(p.Bytes)), bytes.NewReader(p.Bytes))
					require.NoError(t, e)
					in.Objects = append(in.Objects, capture.ObjectPayload{Kind: p.Kind, Ref: ref})
				}
				require.NoError(t, store.Commit(ctx, in))
				src, e := facts.New(ctx, facts.Config{Store: store, Profile: f.profile, Canonical: fresh, Consumer: "materialized"})
				require.NoError(t, e)
				file := filepath.Join(path, in.Objects[0].Ref.ID.Hex()[2:]+".object")
				if failure == "missing" {
					require.NoError(t, os.Remove(file))
				}
				if failure == "corrupt" {
					data, e := os.ReadFile(file)
					require.NoError(t, e)
					data[len(data)-1] ^= 1
					require.NoError(t, os.WriteFile(file, data, 0600))
				}
				before := len(fresh.calls)
				b, rs, readErr := src.BlockWithReceipts(ctx, 1)
				require.Len(t, fresh.calls, before, "materialization is DB-only")
				switch failure {
				case "none", "fresh":
					require.NoError(t, readErr)
					require.Equal(t, f.chain.Block(1).Block.Hash(), b.Hash)
					require.Len(t, rs, 1)
				case "large":
					require.ErrorIs(t, readErr, facts.ErrCoverage)
					require.ErrorIs(t, readErr, capture.ErrProjectionInputLimit)
					require.ErrorIs(t, readErr, port.ErrInvalidData)
				case "corrupt":
					require.ErrorIs(t, readErr, window.ErrCorrupt)
					require.NotErrorIs(t, readErr, facts.ErrCoverage)
				case "missing":
					require.ErrorIs(t, readErr, os.ErrNotExist)
					require.NotErrorIs(t, readErr, facts.ErrCoverage)
				}
				if readErr != nil {
					require.Nil(t, b)
					require.Nil(t, rs)
				}
				seed(t, db, f)
				writer := makeFetcher(t, db, src, src, src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop()}))
				if failure == "fresh" {
					fresh.setWrong(true)
				}
				e = writer.FetchBlock(ctx, 1)
				writer.Close()
				if failure == "none" {
					require.NoError(t, e)
					_, e = store.ProjectionAck(ctx, "materialized", src.Epoch(), in.Manifest.Identity)
					require.NoError(t, e)
				} else {
					require.Error(t, e)
					if failure == "fresh" {
						require.ErrorIs(t, e, capture.ErrCanonical)
					}
					_, e = db.GetBlock(ctx, 1)
					require.ErrorIs(t, e, port.ErrNotFound)
					_, e = store.ProjectionAck(ctx, "materialized", src.Epoch(), in.Manifest.Identity)
					require.ErrorIs(t, e, port.ErrNotFound)
					seq, e := db.(port.Outbox).LastOutboxSeq(ctx)
					require.NoError(t, e)
					require.Zero(t, seq)
				}
			})
		}
	}
}

type factsDecodeAction struct {
	chains.BinaryProfile
	action func()
}

func (p factsDecodeAction) DecodeBlockRLP(h, b []byte) (*model.Block, error) {
	p.action()
	return p.BinaryProfile.DecodeBlockRLP(h, b)
}

func TestFactsMaterializedResetDuringDecode(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := dbFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			f := build(t, db, false)
			profile := factsDecodeAction{BinaryProfile: f.profile, action: func() {
				require.NoError(t, f.store.Reset(ctx, 1, f.inputs[0].Manifest.Identity.Block))
			}}
			src, e := facts.New(ctx, facts.Config{Store: f.store, Profile: profile, Canonical: f.fresh, Consumer: "decode-reset"})
			require.NoError(t, e)
			b, rs, e := src.BlockWithReceipts(ctx, 1)
			require.ErrorIs(t, e, capture.ErrStale)
			require.Nil(t, b)
			require.Nil(t, rs)
			_, e = f.store.ProjectionAck(ctx, "decode-reset", 1, f.inputs[0].Manifest.Identity)
			require.ErrorIs(t, e, port.ErrNotFound)
		})
	}
}
