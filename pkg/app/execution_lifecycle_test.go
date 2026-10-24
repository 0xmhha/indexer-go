package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type executionServiceProbe struct {
	runs, stops atomic.Int32
	err         error
}

func (p *executionServiceProbe) Run(context.Context) error { p.runs.Add(1); return p.err }
func (p *executionServiceProbe) Stop()                     { p.stops.Add(1) }
func TestExecutionAppSelectsOnlyExecutionServiceAndStopsIt(t *testing.T) {
	want := errors.New("execution service done")
	p := &executionServiceProbe{err: want}
	a := &App{config: config.NewConfig(), logger: zap.NewNop(), executionFeed: p}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, a.run(ctx), want)
	require.Equal(t, int32(1), p.runs.Load())
	a.Shutdown()
	require.Equal(t, int32(1), p.stops.Load())
}
