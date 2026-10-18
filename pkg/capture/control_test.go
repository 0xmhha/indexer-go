package capture_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/window"
)

type finalityFixture struct {
	block model.FeedBlock
	err   error
	name  string
}

type shorterCanonical struct{ *oracle }

func (o shorterCanonical) HashAt(c context.Context, n uint64) (common.Hash, error) {
	if n >= 2 {
		return common.Hash{}, source.ErrNotFound
	}
	return o.oracle.HashAt(c, n)
}

func (p finalityFixture) ID() string {
	if p.name != "" {
		return p.name
	}
	return "fixture.finalized.v1"
}
func (p finalityFixture) Checkpoint(context.Context, model.FeedChain) (model.FeedBlock, error) {
	return p.block, p.err
}

type rawWork func(context.Context, model.CaptureIdentity, []chainfeed.Envelope) error

func (f rawWork) Consume(ctx context.Context, id model.CaptureIdentity, e []chainfeed.Envelope) error {
	return f(ctx, id, e)
}

func TestControlGCRequiresACKAgeAndLease(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := openFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			o := newOracle()
			s := service(t, db, o)
			initialize(t, s, "audit")
			var clock atomic.Int64
			clock.Store(time.Now().UnixNano())
			opts := rawOptions(t.TempDir())
			opts.Now = func() time.Time { return time.Unix(0, clock.Load()) }
			require.NoError(t, window.InitializeVolume(opts.Volume))
			raw, err := window.Create(opts)
			require.NoError(t, err)
			defer func() { require.NoError(t, raw.Close()) }()
			a := input(1, 1, common.Hash{2}, chain.GenesisHash)
			var refs []window.Reference
			for _, p := range a.Payloads {
				r, err := raw.Append(ctx, chainfeed.Envelope{Block: a.Manifest.Identity.Block, Kind: p.Kind, Codec: "fixture.v1", Payload: p.Bytes})
				require.NoError(t, err)
				refs = append(refs, r)
			}
			require.NoError(t, s.Commit(ctx, a))
			policy := finalityFixture{block: a.Manifest.Identity.Block}
			status, changed, err := capture.ControlStep(ctx, raw, s, finalityFixture{err: source.ErrFinalityUnavailable}, true, 128)
			require.NoError(t, err)
			require.False(t, changed)
			require.ErrorIs(t, status.Blocked, source.ErrFinalityUnavailable)
			state, err := s.State(ctx)
			require.NoError(t, err)
			require.Nil(t, state.Finalized)
			status, _, err = capture.ControlStep(ctx, raw, s, policy, true, 128)
			require.NoError(t, err)
			require.Zero(t, status.Collected)
			clock.Add(int64(25 * time.Hour))
			status, _, err = capture.ControlStep(ctx, raw, s, policy, true, 128)
			require.NoError(t, err)
			require.Zero(t, status.Collected)
			require.Error(t, status.Blocked)
			lease, err := raw.Acquire(ctx, refs[0])
			require.NoError(t, err)
			entered, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			consumer := rawWork(func(c context.Context, id model.CaptureIdentity, env []chainfeed.Envelope) error {
				if calls.Add(1) == 1 {
					close(entered)
				}
				if id != a.Manifest.Identity || len(env) != 2 {
					return errors.New("wrong raw delivery")
				}
				select {
				case <-release:
					return nil
				case <-c.Done():
					return c.Err()
				}
			})
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- capture.RawConsumerTask("audit", consumer, time.Millisecond)(c, raw, s) }()
			select {
			case <-entered:
			case <-c.Done():
				t.Fatal(c.Err())
			}
			acked, err := s.RawAcknowledged(ctx, a.Manifest.Identity, "audit")
			require.NoError(t, err)
			require.False(t, acked)
			close(release)
			require.Eventually(t, func() bool {
				ack, err := s.RawAcknowledged(ctx, a.Manifest.Identity, "audit")
				return err == nil && ack
			}, 5*time.Second, time.Millisecond)
			cancel()
			require.ErrorIs(t, <-done, context.Canceled)
			status, _, err = capture.ControlStep(ctx, raw, s, policy, true, 128)
			require.NoError(t, err)
			require.Zero(t, status.Collected, "active lease still pins acknowledged input")
			require.NoError(t, lease.Close())
			status, _, err = capture.ControlStep(ctx, raw, s, policy, true, 128)
			require.NoError(t, err)
			require.Equal(t, 1, status.Collected)
			require.Zero(t, raw.Stats().Records)
			stored, err := s.Read(ctx, a.Manifest.Identity)
			require.NoError(t, err)
			require.Equal(t, a.Payloads, stored.Payloads)
			_, _, err = capture.ControlStep(ctx, raw, s, finalityFixture{block: a.Manifest.Identity.Block, name: "different-authority"}, true, 128)
			require.ErrorIs(t, err, capture.ErrConflict)
			require.EqualValues(t, 1, calls.Load())
			require.Zero(t, raw.Stats().Readers)
		})
	}
}

func TestControlRejectsFinalityContradictionsAndDeepReorg(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := openFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			o := newOracle()
			s := service(t, db, o)
			initialize(t, s)
			prev := chain.GenesisHash
			for n := uint64(1); n <= 3; n++ {
				hash := o.hashes[n]
				require.NoError(t, s.Commit(ctx, input(n, 1, hash, prev)))
				prev = hash
			}
			o.fork(3, common.Hash{33})
			o.fork(2, common.Hash{22})
			_, _, err := s.CommonAncestor(ctx, 1)
			require.ErrorIs(t, err, capture.ErrRebuildRequired)
			a, changed, err := s.CommonAncestor(ctx, 2)
			require.NoError(t, err)
			require.True(t, changed)
			require.EqualValues(t, 1, a.Number)
			o.fork(2, common.Hash{3})
			o.fork(3, common.Hash{4})
			short, err := capture.New(db, chain, shorterCanonical{o})
			require.NoError(t, err)
			ancestor, changed, err := short.CommonAncestor(ctx, 128)
			require.NoError(t, err)
			require.True(t, changed)
			require.EqualValues(t, 1, ancestor.Number, "temporarily shorter head has a verifiable common prefix")
			policy := finalityFixture{block: a}
			_, _, err = capture.ControlStep(ctx, nil, s, policy, false, 128)
			require.NoError(t, err)
			_, _, err = capture.ControlStep(ctx, nil, s, finalityFixture{block: model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}}, false, 128)
			require.ErrorIs(t, err, capture.ErrFinalityConflict)
			o.fork(1, common.Hash{11})
			_, _, err = capture.ControlStep(ctx, nil, s, policy, false, 128)
			require.ErrorIs(t, err, capture.ErrFinalityConflict)
			state, err := s.State(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, state.Generation)
			require.EqualValues(t, 3, state.Through.Number)
		})
	}
}

func TestRawConsumerFailureNeverACKs(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	s := service(t, db, newOracle())
	initialize(t, s, "audit")
	raw, _, _ := recoveryRaw(t, t.TempDir())
	defer func() { require.NoError(t, raw.Close()) }()
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	want := errors.New("consumer durable write failed")
	err := capture.RawConsumerTask("audit", rawWork(func(context.Context, model.CaptureIdentity, []chainfeed.Envelope) error { return want }), time.Millisecond)(ctx, raw, s)
	require.ErrorIs(t, err, want)
	acked, err := s.RawAcknowledged(ctx, a.Manifest.Identity, "audit")
	require.NoError(t, err)
	require.False(t, acked)
	require.Zero(t, raw.Stats().Readers)
}

func TestRuntimeChecksPolicyBeforePendingDeletionRecovery(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := openFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			o := newOracle()
			s := service(t, db, o)
			initialize(t, s)
			raw, opts, refs := recoveryRaw(t, t.TempDir())
			a := input(1, 1, common.Hash{2}, chain.GenesisHash)
			require.NoError(t, s.Commit(ctx, a))
			_, _, err := capture.ControlStep(ctx, nil, s, finalityFixture{block: a.Manifest.Identity.Block}, false, 128)
			require.NoError(t, err)
			require.ErrorIs(t, s.WithDeletePermit(ctx, refs, func() error { return errInjected }), errInjected)
			require.NoError(t, raw.Close())
			before, err := s.PendingDeletion(ctx)
			require.NoError(t, err)
			called := false
			err = capture.RunFeedSession(ctx, db, o, capture.SessionOptions{Window: opts, Plan: plan, Anchor: model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}}, func(context.Context) (chainfeed.Adapter, capture.Assembler, error) {
				called = true
				return nil, nil, errInjected
			}, capture.RuntimeOptions{Collect: true, Finality: finalityFixture{block: a.Manifest.Identity.Block, name: "changed-authority"}})
			require.ErrorIs(t, err, capture.ErrConflict)
			require.False(t, called, "no peer/assembler or raw Open before policy match")
			after, err := s.PendingDeletion(ctx)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
