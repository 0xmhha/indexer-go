package vmobserve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type processFuncs struct {
	wait    func() error
	kill    func() error
	cleanup func(context.Context) (Receipt, error)
}

func (p processFuncs) Wait() error                                  { return p.wait() }
func (p processFuncs) Kill() error                                  { return p.kill() }
func (p processFuncs) Cleanup(ctx context.Context) (Receipt, error) { return p.cleanup(ctx) }
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("owned lifecycle did not reach expected state")
	}
}
func requireHeld(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("owner returned before real work joined")
	default:
	}
}
func probeFor(t *testing.T, r Runner, c Config) *Probe {
	t.Helper()
	p, e := New(r, c)
	require.NoError(t, e)
	t.Cleanup(p.Close)
	return p
}
func asyncSample(p *Probe, ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() { p.Sample(ctx); close(done) }()
	return done
}

func TestProbeCancellationJoinsWaitThenIndependentCleanup(t *testing.T) {
	entered, killed, release, cleaning, cleanupRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	type key struct{}
	p := probeFor(t, runnerFunc(func(ctx context.Context, j Job, out, stderr io.Writer) (Launch, error) {
		calls.Add(1)
		require.Nil(t, ctx.Value(key{}))
		_, _ = io.WriteString(out, report)
		proc := processFuncs{wait: func() error { close(entered); <-release; return nil }, kill: func() error { close(killed); return nil }, cleanup: func(c context.Context) (Receipt, error) {
			require.Nil(t, c.Value(key{}))
			require.NoError(t, c.Err(), "cleanup has independent cancellation budget")
			close(cleaning)
			<-cleanupRelease
			return Receipt{j, true}, nil
		}}
		return Launch{j, Owned, proc}, nil
	}), Config{})
	var releaseOnce, cleanupOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); cleanupOnce.Do(func() { close(cleanupRelease) }) })
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), key{}, true))
	defer cancel()
	done := asyncSample(p, ctx)
	await(t, entered)
	require.False(t, p.Sample(t.Context()))
	cancel()
	await(t, killed)
	requireHeld(t, done)
	require.False(t, p.Sample(t.Context()))
	closing := make(chan struct{})
	go func() { p.Close(); close(closing) }()
	requireHeld(t, closing)
	releaseOnce.Do(func() { close(release) })
	await(t, cleaning)
	requireHeld(t, done)
	requireHeld(t, closing)
	require.False(t, p.Sample(t.Context()))
	cleanupOnce.Do(func() { close(cleanupRelease) })
	await(t, done)
	await(t, closing)
	v := p.Assess(time.Now(), time.Second)
	require.True(t, v.Closed)
	require.False(t, v.Pending)
	require.False(t, v.Sample.FreeBytes.Present)
	require.EqualValues(t, 1, calls.Load())
}
func TestProbeDeadlineFencesLateValidOutput(t *testing.T) {
	entered, killed, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var writer io.Writer
	p := probeFor(t, runnerFunc(func(ctx context.Context, j Job, out, stderr io.Writer) (Launch, error) {
		writer = out
		return Launch{j, Owned, processFuncs{wait: func() error { close(entered); <-release; _, _ = io.WriteString(out, report); return nil }, kill: func() error { close(killed); return nil }, cleanup: func(context.Context) (Receipt, error) { return Receipt{j, true}, nil }}}, nil
	}), Config{Timeout: 20 * time.Millisecond})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	done := asyncSample(p, t.Context())
	await(t, entered)
	await(t, killed)
	requireHeld(t, done)
	require.False(t, p.Sample(t.Context()))
	once.Do(func() { close(release) })
	await(t, done)
	v := p.Assess(time.Now(), time.Second)
	require.Equal(t, TimedOut, v.Health.Cause)
	require.False(t, v.Sample.FreeBytes.Present)
	_, e := io.WriteString(writer, report)
	require.ErrorIs(t, e, errCapture)
}
func TestProbeCleanupFailurePermanentlyFencesAdmission(t *testing.T) {
	for _, tc := range []string{"error", "wrong_nonce", "not_released", "ambiguous_start", "foreign_launch", "contradictory_launch"} {
		t.Run(tc, func(t *testing.T) {
			var starts, processCalls atomic.Int32
			p := probeFor(t, runnerFunc(func(ctx context.Context, j Job, out, stderr io.Writer) (Launch, error) {
				starts.Add(1)
				_, _ = io.WriteString(out, report)
				proc := processFuncs{wait: func() error { processCalls.Add(1); return nil }, kill: func() error { processCalls.Add(1); return nil }, cleanup: func(context.Context) (Receipt, error) {
					processCalls.Add(1)
					switch tc {
					case "error":
						return Receipt{}, errors.New("private endpoint must not leak")
					case "wrong_nonce":
						return Receipt{Job{99}, true}, nil
					default:
						return Receipt{j, false}, nil
					}
				}}
				switch tc {
				case "ambiguous_start":
					return Launch{Job: j}, errors.New("uncertain start")
				case "foreign_launch":
					return Launch{Job: Job{99}, Ownership: Owned, Process: proc}, nil
				case "contradictory_launch":
					return Launch{Job: j, Ownership: NoResources, Process: proc}, nil
				default:
					return Launch{j, Owned, proc}, nil
				}
			}), Config{})
			require.True(t, p.Sample(t.Context()))
			v := p.Assess(time.Now(), time.Second)
			require.True(t, v.Pending)
			require.Equal(t, CleanupPending, v.Health.Cause)
			require.False(t, v.Sample.FreeBytes.Present)
			for range 4 {
				require.False(t, p.Sample(t.Context()))
			}
			require.EqualValues(t, 1, starts.Load())
			if strings.Contains(tc, "launch") || tc == "ambiguous_start" {
				require.Zero(t, processCalls.Load(), "unverified capability was used")
			}
		})
	}
}
func TestProbeOutputCapsAndRedaction(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			var killed atomic.Int32
			p := probeFor(t, runnerFunc(func(ctx context.Context, j Job, out, stderr io.Writer) (Launch, error) {
				if stream == "stdout" {
					_, _ = io.WriteString(out, strings.Repeat("s", MaxStdout+1))
				} else {
					_, _ = io.WriteString(stderr, strings.Repeat("credential", MaxStderr))
				}
				return Launch{j, Owned, processFuncs{wait: func() error { return nil }, kill: func() error { killed.Add(1); return nil }, cleanup: func(context.Context) (Receipt, error) { return Receipt{j, true}, nil }}}, nil
			}), Config{})
			require.True(t, p.Sample(t.Context()))
			v := p.Assess(time.Now(), time.Second)
			require.Equal(t, OutputLimit, v.Sample.Cause)
			require.False(t, v.Sample.FreeBytes.Present)
			require.False(t, v.Pending)
			require.EqualValues(t, 1, killed.Load())
			data, e := json.Marshal(v)
			require.NoError(t, e)
			require.NotContains(t, string(data), "credential")
		})
	}
	// No stderr byte is retained, even below the limit.
	c := newCapture(MaxStderr, false, func() {})
	_, e := io.WriteString(c, "postgres://user:private@host/db")
	require.NoError(t, e)
	data, bad := c.seal()
	require.Empty(t, data)
	require.False(t, bad)
}
func TestProbeFailuresNeverBecomeKnownZeroOrHealthy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		cause Cause
	}{{"permission", fmt.Errorf("secret DSN: %w", os.ErrPermission), PermissionDenied}, {"daemon", fmt.Errorf("secret endpoint: %w", ErrDaemon), DaemonUnavailable}, {"driver", errors.New("secret password"), ProbeFailed}} {
		t.Run(tc.name, func(t *testing.T) {
			p := probeFor(t, runnerFunc(func(_ context.Context, j Job, out, stderr io.Writer) (Launch, error) {
				_, _ = io.WriteString(stderr, tc.err.Error())
				return Launch{Job: j, Ownership: NoResources}, tc.err
			}), Config{})
			require.True(t, p.Sample(t.Context()))
			v := p.Assess(time.Now(), time.Second)
			require.Equal(t, tc.cause, v.Health.Cause)
			require.Equal(t, Degraded, v.Health.State)
			require.False(t, v.Sample.FreeBytes.Present)
			require.False(t, v.Pending)
			encoded, e := json.Marshal(v)
			require.NoError(t, e)
			require.NotContains(t, string(encoded), "secret")
			require.True(t, p.Sample(t.Context()), "positive no-resource failure may retry only on a new explicit call")
		})
	}
}
func TestProbeHealthKeepsOriginalDataTime(t *testing.T) {
	p := probeFor(t, runnerFunc(func(_ context.Context, j Job, out, stderr io.Writer) (Launch, error) {
		_, _ = io.WriteString(out, report)
		return Launch{j, Owned, &immediate{job: j}}, nil
	}), Config{})
	require.True(t, p.Sample(t.Context()))
	v := p.Assess(time.Now(), time.Second)
	original := v.Sample.AcquiredAt
	require.Equal(t, Healthy, v.Health.State)
	stale := p.Assess(original.Add(2*time.Second), time.Second)
	require.Equal(t, MissingHeartbeat, stale.Health.Cause)
	require.False(t, stale.Sample.FreeBytes.Present)
	require.Equal(t, original, stale.Sample.AcquiredAt)
	bad := p.Assess(original.Add(-time.Second), time.Second)
	require.Equal(t, InvalidTime, bad.Health.Cause)
	require.Equal(t, Unknown, bad.Health.State)
	require.False(t, bad.Sample.FreeBytes.Present)
	v.Sample.FreeBytes.Value = 99
	require.EqualValues(t, 2048, p.Assess(time.Now(), time.Second).Sample.FreeBytes.Value)
}
func TestProbeLateStartAndConcurrentClose(t *testing.T) {
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var waits, kills, cleanups atomic.Int32
	p := probeFor(t, runnerFunc(func(ctx context.Context, j Job, out, stderr io.Writer) (Launch, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		_, _ = io.WriteString(out, report)
		return Launch{j, Owned, processFuncs{wait: func() error { waits.Add(1); return nil }, kill: func() error { kills.Add(1); return nil }, cleanup: func(context.Context) (Receipt, error) { cleanups.Add(1); return Receipt{j, true}, nil }}}, nil
	}), Config{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	done := asyncSample(p, t.Context())
	await(t, entered)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(p.Close)
	}
	closed := make(chan struct{})
	go func() { wg.Wait(); close(closed) }()
	await(t, cancelled)
	requireHeld(t, closed)
	require.False(t, p.Sample(t.Context()))
	once.Do(func() { close(release) })
	await(t, closed)
	await(t, done)
	require.EqualValues(t, 1, waits.Load())
	require.EqualValues(t, 1, kills.Load())
	require.EqualValues(t, 1, cleanups.Load())
	require.False(t, p.Assess(time.Now(), time.Second).Sample.FreeBytes.Present)
}
func TestProbePeriodicRunAndNeverStartedClose(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var starts atomic.Int32
	p := probeFor(t, runnerFunc(func(ctx context.Context, j Job, out, stderr io.Writer) (Launch, error) {
		starts.Add(1)
		return Launch{j, Owned, processFuncs{wait: func() error { close(entered); <-release; return nil }, kill: func() error { close(release); return nil }, cleanup: func(context.Context) (Receipt, error) { return Receipt{j, true}, nil }}}, nil
	}), Config{})
	done := make(chan error, 1)
	go func() { done <- p.Run(t.Context(), time.Second) }()
	await(t, entered)
	require.ErrorIs(t, p.Run(t.Context(), time.Second), ErrLifecycle)
	require.False(t, p.Sample(t.Context()))
	p.Close()
	require.NoError(t, <-done)
	require.EqualValues(t, 1, starts.Load())
	require.True(t, p.Assess(time.Now(), time.Second).Closed)
	q := probeFor(t, runnerFunc(func(context.Context, Job, io.Writer, io.Writer) (Launch, error) {
		t.Fatal("never-started Close performed IO")
		return Launch{}, nil
	}), Config{})
	q.Close()
	q.Close()
	require.False(t, q.Sample(t.Context()))
	require.ErrorIs(t, q.Run(t.Context(), time.Second), ErrLifecycle)
}
func TestProbeSequenceAndConfigBounds(t *testing.T) {
	_, e := New(nil, Config{})
	require.ErrorIs(t, e, ErrConfig)
	runner := runnerFunc(func(context.Context, Job, io.Writer, io.Writer) (Launch, error) {
		t.Fatal("counter overflow started a probe")
		return Launch{}, nil
	})
	for _, c := range []Config{{Timeout: -time.Second}, {Timeout: 31 * time.Second}, {CleanupTimeout: 6 * time.Second}} {
		_, e = New(runner, c)
		require.ErrorIs(t, e, ErrConfig)
	}
	p := probeFor(t, runner, Config{})
	p.mu.Lock()
	p.sequence = math.MaxUint64
	p.mu.Unlock()
	require.False(t, p.Sample(t.Context()))
	require.True(t, p.Assess(time.Now(), time.Second).Pending)
	require.ErrorIs(t, p.Run(t.Context(), 0), ErrConfig)
}

// A legal delayed cancellation callback must not let a result publish after the
// parent's Err already changed. This models the scheduling gap of AfterFunc.
type delayedCancellation struct{ done chan struct{} }

func (d delayedCancellation) Deadline() (time.Time, bool) { return time.Time{}, false }
func (d delayedCancellation) Done() <-chan struct{}       { return d.done }
func (d delayedCancellation) Err() error {
	select {
	case <-d.done:
		return context.Canceled
	default:
		return nil
	}
}
func (d delayedCancellation) Value(any) any                { return nil }
func (d delayedCancellation) AfterFunc(func()) func() bool { return func() bool { return true } }
func TestProbeParentCancellationFencesCompletionBeforeCallback(t *testing.T) {
	parent := delayedCancellation{make(chan struct{})}
	p := probeFor(t, runnerFunc(func(_ context.Context, j Job, out, stderr io.Writer) (Launch, error) {
		_, _ = io.WriteString(out, report)
		return Launch{j, Owned, processFuncs{wait: func() error { return nil }, kill: func() error { return nil }, cleanup: func(context.Context) (Receipt, error) { close(parent.done); return Receipt{j, true}, nil }}}, nil
	}), Config{})
	require.True(t, p.Sample(parent))
	v := p.Assess(time.Now(), time.Second)
	require.False(t, v.Sample.FreeBytes.Present, "late successful output crossed parent cancellation fence")
	require.Equal(t, Cancelled, v.Sample.Cause)
}

func TestProbeCleanupTimeoutFencesNewAttempts(t *testing.T) {
	entered, expired, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	p := probeFor(t, runnerFunc(func(_ context.Context, j Job, out, stderr io.Writer) (Launch, error) {
		_, _ = io.WriteString(out, report)
		return Launch{j, Owned, processFuncs{wait: func() error { return nil }, kill: func() error { return nil }, cleanup: func(c context.Context) (Receipt, error) {
			close(entered)
			<-c.Done()
			close(expired)
			<-release
			return Receipt{j, true}, nil
		}}}, nil
	}), Config{CleanupTimeout: 20 * time.Millisecond})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	done := asyncSample(p, t.Context())
	await(t, entered)
	await(t, expired)
	requireHeld(t, done)
	require.False(t, p.Sample(t.Context()))
	once.Do(func() { close(release) })
	await(t, done)
	v := p.Assess(time.Now(), time.Second)
	require.True(t, v.Pending)
	require.Equal(t, CleanupPending, v.Health.Cause)
	require.False(t, v.Sample.FreeBytes.Present)
	require.False(t, p.Sample(t.Context()))
}
func TestProbePartialStartFailureStillReleasesOwnedWork(t *testing.T) {
	var killed, waited, cleaned atomic.Int32
	p := probeFor(t, runnerFunc(func(_ context.Context, j Job, out, stderr io.Writer) (Launch, error) {
		_, _ = io.WriteString(out, report)
		return Launch{j, Owned, processFuncs{wait: func() error { waited.Add(1); return nil }, kill: func() error { killed.Add(1); return nil }, cleanup: func(context.Context) (Receipt, error) { cleaned.Add(1); return Receipt{j, true}, nil }}}, os.ErrPermission
	}), Config{})
	require.True(t, p.Sample(t.Context()))
	v := p.Assess(time.Now(), time.Second)
	require.False(t, v.Pending)
	require.False(t, v.Sample.FreeBytes.Present)
	require.Equal(t, PermissionDenied, v.Sample.Cause)
	require.EqualValues(t, 1, killed.Load())
	require.EqualValues(t, 1, waited.Load())
	require.EqualValues(t, 1, cleaned.Load())
}
func TestProbeRunCancellationJoinsExternallyAdmittedWork(t *testing.T) {
	entered, killed, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	p := probeFor(t, runnerFunc(func(_ context.Context, j Job, out, stderr io.Writer) (Launch, error) {
		return Launch{j, Owned, processFuncs{wait: func() error { close(entered); <-release; return nil }, kill: func() error { close(killed); return nil }, cleanup: func(context.Context) (Receipt, error) { return Receipt{j, true}, nil }}}, nil
	}), Config{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	sampled := asyncSample(p, t.Context())
	await(t, entered)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = p.Run(ctx, time.Second); close(done) }()
	require.Eventually(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.runDone != nil }, time.Second, time.Millisecond)
	cancel()
	await(t, killed)
	requireHeld(t, done)
	require.False(t, p.Sample(t.Context()))
	once.Do(func() { close(release) })
	await(t, sampled)
	await(t, done)
	require.True(t, p.Assess(time.Now(), time.Second).Closed)
}
func TestProbeHealthyObserverCanMeasureKnownFullDisk(t *testing.T) {
	p := probeFor(t, runnerFunc(func(_ context.Context, j Job, out, stderr io.Writer) (Launch, error) {
		_, _ = io.WriteString(out, header+"overlay 10 10 0 100% /\n")
		return Launch{j, Owned, &immediate{job: j}}, nil
	}), Config{})
	require.True(t, p.Sample(t.Context()))
	v := p.Assess(time.Now(), time.Second)
	require.Equal(t, Healthy, v.Health.State)
	require.True(t, v.Sample.FreeBytes.Present)
	require.Zero(t, v.Sample.FreeBytes.Value)
}

func TestCaptureConcurrentWritesAndSealRemainBounded(t *testing.T) {
	var cancellations atomic.Int32
	c := newCapture(64, true, func() { cancellations.Add(1) })
	_, e := c.Write([]byte(strings.Repeat("x", 32)))
	require.NoError(t, e)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			<-start
			for range 128 {
				_, _ = c.Write([]byte("x"))
			}
		})
	}
	close(start)
	data, _ := c.seal()
	wg.Wait()
	require.LessOrEqual(t, len(data), 64)
	require.GreaterOrEqual(t, len(data), 32)
	_, e = c.Write([]byte("late"))
	require.ErrorIs(t, e, errCapture)
	data, _ = c.seal()
	require.Empty(t, data)
}
