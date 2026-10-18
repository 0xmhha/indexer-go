package evmstate

import (
	"context"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/stretchr/testify/require"
)

func TestTrackedProofRuntimeResume(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := fixture(t)
			db := openProofDB(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			store, id, fresh := captured(t, db, f.root)
			client, wire := proofRPC(t, f, fresh)
			fresh.client = client
			cfg := Config{Name: "test-tracked", StartHeight: 1}
			for _, target := range f.selection.Targets {
				a := AccountConfig{Address: target.Address.Hex(), Code: target.Code}
				for _, k := range target.Slots {
					a.Slots = append(a.Slots, k.Hex())
				}
				cfg.Accounts = append(cfg.Accounts, a)
			}
			d, e := cfg.Descriptor(model.FeedBlock{Chain: id.Block.Chain, Hash: id.Block.Chain.GenesisHash})
			require.NoError(t, e)
			worker, e := New(store, evm.New("evm"), client)
			require.NoError(t, e)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- worker.Run(ctx, d, 20*time.Millisecond) }()
			require.Eventually(t, func() bool {
				s, e := store.SupplementStreamStatus(t.Context(), d)
				return e == nil && s.Complete != nil
			}, 5*time.Second, 10*time.Millisecond)
			cancel()
			require.ErrorIs(t, <-done, context.Canceled)
			require.EqualValues(t, 2, wire.calls.Load(), "proof batch and commit fresh share one pool; cursor adds no second fresh request")
			wire.mode.Store(2)
			worker, e = New(store, evm.New("evm"), client)
			require.NoError(t, e)
			ctx, cancel = context.WithCancel(t.Context())
			go func() { done <- worker.Run(ctx, d, 20*time.Millisecond) }()
			require.Eventually(t, func() bool { return worker.Metrics().Verify.Count >= 1 }, 5*time.Second, 10*time.Millisecond)
			cancel()
			require.ErrorIs(t, <-done, context.Canceled)
			require.EqualValues(t, 2, wire.calls.Load())
		})
	}
}
