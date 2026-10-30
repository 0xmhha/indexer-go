package app

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"testing"
)

type nativeRecoveryStore struct {
	port.ExecutionStore
	port.ExecutionRewindStore
	c      port.ExecutionControl
	marker port.ExecutionRewind
}

func (s *nativeRecoveryStore) ExecutionStatus(context.Context) (port.ExecutionControl, error) {
	return s.c, nil
}
func (s *nativeRecoveryStore) ExecutionRewindStatus(context.Context) (port.ExecutionRewind, error) {
	return s.marker, nil
}
func (s *nativeRecoveryStore) BeginExecutionReorgCheck(context.Context) (port.ExecutionReorgCheck, error) {
	return port.ExecutionReorgCheck{Generation: s.c.Generation, From: s.c.Visible, Root: s.c.Root}, nil
}
func (s *nativeRecoveryStore) ExecutionReorgCheckStatus(ctx context.Context) (port.ExecutionReorgCheck, error) {
	return s.BeginExecutionReorgCheck(ctx)
}
func (s *nativeRecoveryStore) BeginExecutionRewind(_ context.Context, target execwire.Ref, root execwire.Hash) (port.ExecutionRewind, error) {
	s.marker = port.ExecutionRewind{RequestGeneration: s.c.Generation, From: s.c.Visible, FromRoot: s.c.Root, Target: target, Root: root}
	s.marker.Target.Generation++
	s.c.Generation++
	s.c.Visible.Generation = s.c.Generation
	s.c.ReorgPending = false
	s.c.Rewinding = true
	return s.marker, nil
}

type nativeRecoveryProgram struct {
	recover func(context.Context, port.ExecutionRewind) error
	resume  func(context.Context, port.ExecutionControl) error
}

func (p *nativeRecoveryProgram) Check(context.Context) error { return nil }
func (p *nativeRecoveryProgram) Run(context.Context) error   { return nil }
func (p *nativeRecoveryProgram) Stop()                       {}
func (p *nativeRecoveryProgram) Recover(ctx context.Context, m port.ExecutionRewind) error {
	return p.recover(ctx, m)
}
func (p *nativeRecoveryProgram) Recovered(ctx context.Context, c port.ExecutionControl) error {
	return p.resume(ctx, c)
}
func TestExecutionNativeRecoveryPrecedesSQLFreshAndResume(t *testing.T) {
	for _, fail := range []string{"none", "target", "native", "fresh", "validate"} {
		t.Run(fail, func(t *testing.T) {
			store := &nativeRecoveryStore{c: port.ExecutionControl{Generation: 7, Visible: execwire.Ref{Generation: 7, Height: 3, Hash: execwire.Hash{3}}, Root: execwire.Hash{4}, BaselineReady: true, ReorgPending: true}}
			order := []string{}
			failure := errors.New("owned failure")
			target := execwire.Ref{Generation: 7, Height: 2, Hash: execwire.Hash{2}}
			r := executionRecovery{store: store, target: func(context.Context) (execwire.Ref, execwire.Hash, error) {
				order = append(order, "target")
				require.True(t, store.c.ReorgPending)
				require.Equal(t, uint64(7), store.c.Generation)
				if fail == "target" {
					return execwire.Ref{}, execwire.Hash{}, failure
				}
				return target, execwire.Hash{5}, nil
			}, sql: func(context.Context) error {
				order = append(order, "fresh-sql")
				require.True(t, store.c.Rewinding)
				if fail == "fresh" {
					return failure
				}
				store.c.Rewinding = false
				store.c.Visible = store.marker.Target
				store.c.Root = store.marker.Root
				return nil
			}, validate: func(context.Context, port.ExecutionControl) error {
				order = append(order, "validate")
				if fail == "validate" {
					return failure
				}
				return nil
			}}
			p := &nativeRecoveryProgram{recover: func(_ context.Context, m port.ExecutionRewind) error {
				order = append(order, "native")
				require.True(t, store.c.Rewinding)
				require.False(t, store.c.ReorgPending)
				require.Equal(t, uint64(8), m.Target.Generation)
				if fail == "native" {
					return failure
				}
				return nil
			}, resume: func(_ context.Context, c port.ExecutionControl) error {
				order = append(order, "resume")
				require.False(t, c.Rewinding)
				require.False(t, c.ReorgPending)
				require.Equal(t, store.marker.Target, c.Visible)
				return nil
			}}
			err := r.Run(t.Context(), p)
			switch fail {
			case "none":
				require.NoError(t, err)
				require.Equal(t, []string{"target", "native", "fresh-sql", "validate", "resume"}, order)
			case "target":
				require.ErrorIs(t, err, failure)
				require.Equal(t, []string{"target"}, order)
				require.True(t, store.c.ReorgPending)
			case "native":
				require.ErrorIs(t, err, failure)
				require.Equal(t, []string{"target", "native"}, order)
				require.True(t, store.c.Rewinding)
			case "validate":
				require.ErrorIs(t, err, failure)
				require.Equal(t, []string{"target", "native", "fresh-sql", "validate"}, order)
			case "fresh":
				require.ErrorIs(t, err, failure)
				require.Equal(t, []string{"target", "native", "fresh-sql"}, order)
				require.True(t, store.c.Rewinding)
			}
		})
	}
}
