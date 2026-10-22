package capture_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// Only the processor is run; intake notifications are deliberately absent.
// Durable availability must work on restart and with a coalesced/full queue.
type candidateAdapter struct{ chainfeed.Adapter }

func (candidateAdapter) Capabilities(context.Context) (chainfeed.Capabilities, error) {
	return chainfeed.Capabilities{Chain: chain, Datasets: []chainfeed.Capability{
		{Dataset: plan.Required[0].Kind, Coverage: plan.Required[0].Coverage},
		{Dataset: plan.Required[1].Kind, Coverage: plan.Required[1].Coverage},
	}}, nil
}

type candidateAssembler struct{ beforeCommit func() }

func (a candidateAssembler) Assemble(_ context.Context, _ *window.Store, state capture.State, refs []window.Reference) (capture.Input, error) {
	if a.beforeCommit != nil {
		a.beforeCommit()
	}
	return input(refs[0].Block.Number, state.Generation, refs[0].Block.Hash, state.Through.Hash), nil
}

type candidateViews struct {
	port.CaptureDatabase
	views atomic.Int32
}

func (d *candidateViews) CaptureView(ctx context.Context, c model.FeedChain, fn func(port.CaptureReader) error) error {
	err := d.CaptureDatabase.CaptureView(ctx, c, fn)
	d.views.Add(1)
	return err
}

func candidateWindow(t *testing.T) *window.Store {
	t.Helper()
	root := t.TempDir()
	opts := window.Options{Volume: window.VolumeSpec{Root: root, ID: "candidate", AllowDirectory: true}, Path: filepath.Join(root, "raw"), Chain: chain, MaxBytes: 2 << 20, SegmentBytes: 32 << 10, MaxPayloadBytes: 2048, MaxSegmentRecords: 1, MaxSegments: 16}
	require.NoError(t, window.InitializeVolume(opts.Volume))
	raw, err := window.Create(opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	return raw
}

func appendCandidate(t *testing.T, raw *window.Store, in capture.Input) {
	t.Helper()
	for _, p := range in.Payloads {
		_, err := raw.Append(ctx, chainfeed.Envelope{Block: in.Manifest.Identity.Block, Kind: p.Kind, Codec: "fixture.v1", Payload: p.Bytes})
		require.NoError(t, err)
	}
}

func oracleCalls(o *oracle) int { o.mu.Lock(); defer o.mu.Unlock(); return o.calls }

type candidateCanonical struct {
	*oracle
	missing atomic.Bool
}

func (o *candidateCanonical) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	h, err := o.oracle.HashAt(ctx, n)
	if n > 0 && o.missing.Load() {
		return common.Hash{}, fmt.Errorf("%w: %d", source.ErrNotFound, n)
	}
	return h, err
}

func TestCanonicalCandidateUsesCommitFreshWithoutFuturePolling(t *testing.T) {
	db := openFactory(t, "pebble")()
	t.Cleanup(func() { require.NoError(t, db.Close()) }) // after the process goroutine is joined
	views := &candidateViews{CaptureDatabase: db}
	o := newOracle()
	facts := service(t, views, o)
	initialize(t, facts)
	bootstrap := oracleCalls(o)
	raw := candidateWindow(t)
	in := input(1, 1, common.Hash{2}, chain.GenesisHash)
	partial := in
	partial.Payloads = in.Payloads[:1]
	appendCandidate(t, raw, partial)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, process, err := capture.PipelineTasks(candidateAdapter{}, candidateAssembler{}, capture.PipelineOptions{ResolveCanonical: true, ResolveInterval: time.Minute, PollInterval: time.Millisecond, OnCaptured: func(model.CaptureIdentity) { cancel() }})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- process(ctx, raw, facts) }()
	t.Cleanup(func() { cancel(); require.ErrorIs(t, <-done, context.Canceled) })
	require.Eventually(t, func() bool { return views.views.Load() >= 4 }, time.Second, time.Millisecond)
	require.Equal(t, bootstrap, oracleCalls(o), "partial/future inputs must not spend RPC budget or cache a negative hash")
	partial.Payloads = in.Payloads[1:]
	appendCandidate(t, raw, partial)
	require.Eventually(t, func() bool { return ctx.Err() != nil }, time.Second, time.Millisecond)
	require.Equal(t, context.Canceled, ctx.Err(), "raw completion must not wait for the minute-long fork resolver")
	require.Equal(t, bootstrap+1, oracleCalls(o), "one mandatory fresh inside the capture transaction")
	state, err := facts.State(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, state.Through.Number)
}

func TestCanonicalCandidateResolvesRetainedForks(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		for _, mode := range []string{"ambiguous", "fresh-rejected", "future-rejected"} {
			t.Run(backend+"/"+mode, func(t *testing.T) {
				db := openFactory(t, backend)()
				t.Cleanup(func() { require.NoError(t, db.Close()) }) // after the process goroutine is joined
				o := newOracle()
				fresh := &candidateCanonical{oracle: o}
				facts, err := capture.New(db, chain, fresh)
				require.NoError(t, err)
				initialize(t, facts)
				raw := candidateWindow(t)
				old := input(1, 1, common.Hash{2}, chain.GenesisHash)
				newInput := input(1, 1, common.Hash{99}, chain.GenesisHash)
				appendCandidate(t, raw, old)
				if mode == "ambiguous" {
					o.fork(1, common.Hash{99})
					appendCandidate(t, raw, newInput)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				var once atomic.Bool
				assembler := candidateAssembler{beforeCommit: func() {
					if mode != "ambiguous" && once.CompareAndSwap(false, true) {
						o.fork(1, common.Hash{99}) // changes AFTER assembly, before Commit
						fresh.missing.Store(mode == "future-rejected")
					}
				}}
				_, process, err := capture.PipelineTasks(candidateAdapter{}, assembler, capture.PipelineOptions{ResolveCanonical: true, ResolveInterval: 20 * time.Millisecond, PollInterval: time.Millisecond, OnCaptured: func(model.CaptureIdentity) { cancel() }})
				require.NoError(t, err)
				done := make(chan error, 1)
				go func() { done <- process(ctx, raw, facts) }()
				t.Cleanup(func() { cancel(); require.ErrorIs(t, <-done, context.Canceled) })
				if mode != "ambiguous" {
					require.Eventually(t, func() bool { return oracleCalls(o) >= 4 }, time.Second, time.Millisecond)
					state, e := facts.State(context.Background())
					require.NoError(t, e)
					require.Zero(t, state.Through.Number, "rejected hint cannot advance source facts")
					_, e = facts.Read(context.Background(), old.Manifest.Identity)
					require.ErrorIs(t, e, port.ErrNotFound)
					appendCandidate(t, raw, newInput)
					fresh.missing.Store(false)
				}
				require.Eventually(t, func() bool { return ctx.Err() != nil }, time.Second, time.Millisecond)
				require.Equal(t, context.Canceled, ctx.Err())
				state, err := facts.State(context.Background())
				require.NoError(t, err)
				require.Equal(t, newInput.Manifest.Identity.Block, state.Through)
				events, err := facts.Events(context.Background(), 0, 10)
				require.NoError(t, err)
				require.Len(t, events, 1, "only the fresh accepted candidate publishes FactsReady")
			})
		}
	}
}

type candidateCommitFailure struct{ port.CaptureDatabase }

func (d candidateCommitFailure) CaptureUpdate(ctx context.Context, c model.FeedChain, fn func(port.CaptureWriter) error) error {
	err := d.CaptureDatabase.CaptureUpdate(ctx, c, fn)
	if err == capture.ErrCanonical {
		return errors.Join(err, os.ErrPermission)
	}
	return err
}

func TestCanonicalCandidateNeverHidesCommitFailure(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	o := newOracle()
	facts := service(t, candidateCommitFailure{db}, o)
	initialize(t, facts)
	raw := candidateWindow(t)
	appendCandidate(t, raw, input(1, 1, common.Hash{2}, chain.GenesisHash))
	_, process, err := capture.PipelineTasks(candidateAdapter{}, candidateAssembler{beforeCommit: func() { o.fork(1, common.Hash{99}) }}, capture.PipelineOptions{ResolveCanonical: true})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = process(ctx, raw, facts)
	require.ErrorIs(t, err, capture.ErrCanonical)
	require.ErrorIs(t, err, os.ErrPermission, "canonical selection must not swallow independent commit failures")
	require.Equal(t, 3, oracleCalls(o), "no resolver retry after the joined failure")
}
