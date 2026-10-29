package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/x/arc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

const prepareIngressModule = "test.app-native-prepare"

type ingressPreparedProbe struct {
	deps    ingress.Dependencies
	check   func(context.Context) error
	stopped atomic.Bool
	runs    atomic.Int32
}

func (p *ingressPreparedProbe) Check(ctx context.Context) error { return p.check(ctx) }
func (p *ingressPreparedProbe) Run(context.Context) error       { p.runs.Add(1); return nil }
func (p *ingressPreparedProbe) Stop()                           { p.stopped.Store(true) }

type ingressCanonicalProbe struct {
	program *ingressPreparedProbe
	check   func(context.Context) error
	calls   int
}

func (p *ingressCanonicalProbe) HashAt(context.Context, uint64) (common.Hash, error) {
	p.calls++
	return common.Hash{}, nil
}

type ingressStateProbe struct {
	control port.ExecutionControl
	reads   int
}

func (s *ingressStateProbe) ExecutionStatus(context.Context) (port.ExecutionControl, error) {
	s.reads++
	return s.control, nil
}
func init() {
	ingress.Register(prepareIngressModule, ingress.Module{Profile: "arc", Validate: func(ingress.Config) error { return nil }, Prepare: func(_ context.Context, _ ingress.Config, d ingress.Dependencies) (ingress.Program, error) {
		probe := d.Canonical.(*ingressCanonicalProbe)
		p := &ingressPreparedProbe{deps: d, check: probe.check}
		probe.program = p
		return p, nil
	}})
}
func preparedIngressFixture() (*config.Config, *ingressStateProbe, *ingressCanonicalProbe) {
	cfg := ingressAppConfig()
	cfg.Source.Execution.Ingress.Module = prepareIngressModule
	c := port.ExecutionControl{ExecutionConfig: port.ExecutionConfig{Chain: execwire.Chain{ID: "1337", Genesis: execwire.Hash{1}}, Anchor: execwire.Ref{Generation: 1, Hash: execwire.Hash{1}}, AnchorRoot: execwire.Hash{2}}, Generation: 1, Visible: execwire.Ref{Generation: 1, Hash: execwire.Hash{1}}, Root: execwire.Hash{2}, BaselineReady: true}
	cfg.Source.Execution.AnchorHash = common.Hash(c.Anchor.Hash).Hex()
	cfg.Source.Execution.AnchorRoot = common.Hash(c.AnchorRoot).Hex()
	return cfg, &ingressStateProbe{control: c}, &ingressCanonicalProbe{check: func(context.Context) error { return nil }}
}
func TestExecutionIngressPreparationBorrowsAppAndChecksAfterRecoveryBeforeRun(t *testing.T) {
	cfg, store, canonical := preparedIngressFixture()
	recovered, checked := false, false
	canonical.check = func(context.Context) error {
		if !recovered {
			return errors.New("native checked before SQL recovery")
		}
		checked = true
		return nil
	}
	p, err := prepareExecutionIngress(t.Context(), cfg, store, canonical, arc.New(), func(context.Context) error { recovered = true; return nil })
	require.NoError(t, err)
	require.NotNil(t, p, "App must prepare the configured ingress before API exposure")
	defer p.Stop()
	require.True(t, checked)
	require.True(t, recovered)
	probe := p.(*ingressPreparedProbe)
	require.Same(t, store, probe.deps.Store)
	require.Same(t, canonical, probe.deps.Canonical)
	require.Equal(t, cfg.Source.Execution.Socket, probe.deps.Socket)
	require.Zero(t, probe.runs.Load())
	require.Zero(t, canonical.calls)
}
func TestExecutionIngressPreparationFailureClosesBeforeAPIExposure(t *testing.T) {
	for _, atCheck := range []bool{false, true} {
		cfg, store, canonical := preparedIngressFixture()
		want := errors.New("prepared validation failed")
		recover := func(context.Context) error {
			if !atCheck {
				return want
			}
			return nil
		}
		if atCheck {
			canonical.check = func(context.Context) error { return want }
		}
		p, err := prepareExecutionIngress(t.Context(), cfg, store, canonical, arc.New(), recover)
		require.ErrorIs(t, err, want)
		require.Nil(t, p)
		require.NotNil(t, canonical.program)
		require.True(t, canonical.program.stopped.Load())
		require.Zero(t, canonical.program.runs.Load())
	}
}
func TestExecutionIngressDefaultOffPreservesRecoveryWithoutProbingNewDependencies(t *testing.T) {
	cfg, store, canonical := preparedIngressFixture()
	cfg.Source.Execution.Ingress = nil
	recovered := false
	p, err := prepareExecutionIngress(t.Context(), cfg, store, canonical, arc.New(), func(context.Context) error { recovered = true; return nil })
	require.NoError(t, err)
	require.Nil(t, p)
	require.True(t, recovered)
	require.Zero(t, store.reads)
	require.Zero(t, canonical.calls)
}
