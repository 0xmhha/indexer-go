package vmobserve

import (
	"context"
	"github.com/stretchr/testify/require"
	"io"
	"testing"
	"time"
)

type runnerFunc func(context.Context, Job, io.Writer, io.Writer) (Launch, error)

func (f runnerFunc) Start(c context.Context, j Job, o, e io.Writer) (Launch, error) {
	return f(c, j, o, e)
}

type immediate struct {
	job     Job
	cleaned bool
}

func (p *immediate) Wait() error { return nil }
func (p *immediate) Kill() error { return nil }
func (p *immediate) Cleanup(context.Context) (Receipt, error) {
	p.cleaned = true
	return Receipt{p.job, true}, nil
}
func TestProbeRequiresHeartbeatAndOwnedCleanup(t *testing.T) {
	var process *immediate
	p, e := New(runnerFunc(func(_ context.Context, j Job, out, stderr io.Writer) (Launch, error) {
		process = &immediate{job: j}
		_, _ = io.WriteString(out, report)
		return Launch{j, Owned, process}, nil
	}), Config{Timeout: time.Second, CleanupTimeout: time.Second})
	require.NoError(t, e)
	defer p.Close()
	view := p.Assess(time.Now(), time.Second)
	require.Equal(t, Unknown, view.Health.State)
	require.Equal(t, MissingHeartbeat, view.Health.Cause)
	require.False(t, view.Sample.FreeBytes.Present)
	require.True(t, p.Sample(t.Context()))
	require.NotNil(t, process)
	require.True(t, process.cleaned)
	view = p.Assess(time.Now(), time.Second)
	require.Equal(t, Healthy, view.Health.State)
	require.EqualValues(t, 2048, view.Sample.FreeBytes.Value)
}
