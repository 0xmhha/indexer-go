package vmobserve

import (
	"context"
	"crypto/rand"
	"errors"
	"github.com/0xmhha/indexer-go/internal/nativeobserve"
	"io"
	"math"
	"os"
	"sync"
	"time"
)

type Job [16]byte
type Ownership uint8

const (
	Uncertain Ownership = iota
	NoResources
	Owned
)

// Launch describes positive ownership evidence. A nil/ambiguous handle does not
// prove no container was created. A real adapter must verify labels/cidfile.
type Launch struct {
	Job       Job
	Ownership Ownership
	Process   Process
}
type Receipt struct {
	Job      Job
	Released bool
}
type Process interface {
	Wait() error
	Kill() error
	Cleanup(context.Context) (Receipt, error)
}
type Runner interface {
	Start(context.Context, Job, io.Writer, io.Writer) (Launch, error)
}
type Config struct{ Timeout, CleanupTimeout time.Duration }
type Cause uint8

const (
	NoCause Cause = iota
	MissingHeartbeat
	InvalidTime
	PermissionDenied
	DaemonUnavailable
	TimedOut
	Cancelled
	Malformed
	OutputLimit
	ProbeFailed
	CleanupPending
)

type Sample struct {
	Job                   Job
	Sequence              uint64
	FreeBytes             nativeobserve.Number
	StartedAt, AcquiredAt time.Time
	Cause                 Cause
}
type HealthState uint8

const (
	Unknown HealthState = iota
	Healthy
	Degraded
)

type Health struct {
	State           HealthState
	Cause           Cause
	AsOf, Heartbeat time.Time
}
type View struct {
	Sample                  Sample
	Health                  Health
	Active, Pending, Closed bool
}

var (
	ErrConfig    = errors.New("vm observation: invalid runner or bounds")
	ErrLifecycle = errors.New("vm observation: closed or running")
	ErrDaemon    = errors.New("vm observation: daemon unavailable")
)

type attempt struct {
	job               Job
	sequence          uint64
	started, deadline time.Time
	cancel            context.CancelFunc
	done              chan struct{}
}

// Probe is one resource owner. All external calls occur outside mu. Pending is
// sticky: starting another Probe is not proof that a previous resource is gone.
type Probe struct {
	mu              sync.Mutex
	runner          Runner
	config          Config
	active          *attempt
	last            Sample
	heartbeat       time.Time
	sequence        uint64
	pending, closed bool
	runCancel       context.CancelFunc
	runDone         chan struct{}
	closeOnce       sync.Once
	closeDone       chan struct{}
}

func New(runner Runner, c Config) (*Probe, error) {
	if c.Timeout == 0 {
		c.Timeout = 5 * time.Second
	}
	if c.CleanupTimeout == 0 {
		c.CleanupTimeout = 2 * time.Second
	}
	if runner == nil || c.Timeout < time.Millisecond || c.Timeout > 30*time.Second || c.CleanupTimeout < time.Millisecond || c.CleanupTimeout > 5*time.Second {
		return nil, ErrConfig
	}
	return &Probe{runner: runner, config: c, closeDone: make(chan struct{})}, nil
}
func detachedContext(parent context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	deadline, has := parent.Deadline()
	if limit > 0 {
		bound := time.Now().Add(limit)
		if !has || bound.Before(deadline) {
			deadline, has = bound, true
		}
	}
	var ctx context.Context
	var cancel context.CancelFunc
	if has {
		ctx, cancel = context.WithDeadline(context.Background(), deadline)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	stop := context.AfterFunc(parent, cancel)
	if parent.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}
func failure(err error) Cause {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return TimedOut
	case errors.Is(err, context.Canceled):
		return Cancelled
	case errors.Is(err, os.ErrPermission):
		return PermissionDenied
	case errors.Is(err, ErrDaemon):
		return DaemonUnavailable
	default:
		return ProbeFailed
	}
}

// Sample returns admission, never measurement success. The owner stays occupied
// through actual Start/Wait/Kill/cleanup completion, including after cancellation.
func (p *Probe) Sample(parent context.Context) bool {
	p.mu.Lock()
	if p.closed || p.pending || p.active != nil || parent.Err() != nil {
		p.mu.Unlock()
		return false
	}
	if p.sequence == math.MaxUint64 {
		p.pending = true
		p.mu.Unlock()
		return false
	}
	p.sequence++
	ctx, cancel := detachedContext(parent, p.config.Timeout)
	a := &attempt{sequence: p.sequence, started: time.Now(), cancel: cancel, done: make(chan struct{})}
	a.deadline, _ = ctx.Deadline()
	p.active = a
	p.mu.Unlock()
	sample := Sample{Sequence: a.sequence, StartedAt: a.started}
	pending := false
	defer func() {
		acquired := time.Now()
		if sample.AcquiredAt.IsZero() {
			sample.AcquiredAt = acquired
		}
		p.mu.Lock()
		cancelErr := ctx.Err()
		if e := parent.Err(); e != nil {
			cancelErr = e
		}
		if p.closed || cancelErr != nil {
			sample.FreeBytes = nativeobserve.Number{}
			if sample.Cause == NoCause {
				sample.Cause = Cancelled
				if cancelErr != nil {
					sample.Cause = failure(cancelErr)
				}
				if !time.Now().Before(a.deadline) {
					sample.Cause = TimedOut
				}
			}
		}
		p.last, p.heartbeat, p.pending = sample, acquired, pending
		p.active = nil
		close(a.done)
		p.mu.Unlock()
		cancel()
	}()
	if _, e := rand.Read(a.job[:]); e != nil {
		sample.Cause = ProbeFailed
		return true
	}
	sample.Job = a.job
	if ctx.Err() != nil || parent.Err() != nil {
		sample.Cause = Cancelled
		if !time.Now().Before(a.deadline) {
			sample.Cause = TimedOut
		}
		return true
	}
	stdout := newCapture(MaxStdout, true, cancel)
	stderr := newCapture(MaxStderr, false, cancel)
	launch, startErr := p.runner.Start(ctx, a.job, stdout, stderr)
	if launch.Job != a.job || launch.Ownership != Owned && launch.Ownership != NoResources || launch.Ownership == Owned && launch.Process == nil || launch.Ownership == NoResources && launch.Process != nil {
		pending = true
		sample.Cause = CleanupPending
		stdout.seal()
		stderr.seal()
		return true
	}
	if launch.Ownership == NoResources {
		if startErr == nil {
			startErr = ctx.Err()
			if e := parent.Err(); e != nil {
				startErr = e
			}
		}
		sample.Cause = failure(startErr)
		_, outLimit := stdout.seal()
		_, errLimit := stderr.seal()
		if outLimit || errLimit {
			sample.Cause = OutputLimit
		}
		return true
	}
	proc := launch.Process
	joined := make(chan error, 1)
	go func() { joined <- proc.Wait() }()
	kill := startErr != nil || ctx.Err() != nil
	var waitErr error
	if !kill {
		select {
		case waitErr = <-joined:
		case <-ctx.Done():
			kill = true
		case <-parent.Done():
			cancel()
			kill = true
		}
	}
	var killErr error
	if kill {
		killErr = proc.Kill()
		waitErr = <-joined
	}
	sample.AcquiredAt = time.Now()
	data, outLimit := stdout.seal()
	_, errLimit := stderr.seal()
	// CLI Wait is separate from proof that its container/resource was released.
	cleanupCtx, stop := context.WithTimeout(context.Background(), p.config.CleanupTimeout)
	receipt, cleanupErr := proc.Cleanup(cleanupCtx)
	pending = cleanupErr != nil || cleanupCtx.Err() != nil || receipt.Job != a.job || !receipt.Released
	stop()
	switch {
	case outLimit || errLimit:
		sample.Cause = OutputLimit
	case startErr != nil:
		sample.Cause = failure(startErr)
	case ctx.Err() != nil:
		sample.Cause = failure(ctx.Err())
		if !time.Now().Before(a.deadline) {
			sample.Cause = TimedOut
		}
	case waitErr != nil:
		sample.Cause = failure(waitErr)
	case killErr != nil:
		sample.Cause = ProbeFailed
	case pending:
		sample.Cause = CleanupPending
	default:
		var e error
		sample.FreeBytes, e = ParseFreeKiB(data)
		if e != nil {
			sample.Cause = Malformed
		}
	}
	return true
}

// Run is optional and caller-owned. It never overlaps Sample or retries pending
// cleanup. An adapter ignoring cancellation still has to return before Run joins.
func (p *Probe) Run(parent context.Context, interval time.Duration) error {
	if interval < time.Second || interval > time.Hour {
		return ErrConfig
	}
	p.mu.Lock()
	if p.closed || p.runDone != nil {
		p.mu.Unlock()
		return ErrLifecycle
	}
	ctx, cancel := detachedContext(parent, 0)
	done := make(chan struct{})
	p.runCancel, p.runDone = cancel, done
	p.mu.Unlock()
	defer func() {
		cancel()
		p.mu.Lock()
		p.closed = true
		a := p.active
		p.mu.Unlock()
		if a != nil {
			a.cancel()
			<-a.done
		}
		p.mu.Lock()
		p.runCancel, p.runDone = nil, nil
		close(done)
		p.mu.Unlock()
	}()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if ctx.Err() != nil || parent.Err() != nil {
			return nil
		}
		p.Sample(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-parent.Done():
			return nil
		case <-tick.C:
		}
	}
}
func (p *Probe) Close() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		a, cancel, done := p.active, p.runCancel, p.runDone
		p.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if a != nil {
			a.cancel()
			<-a.done
		}
		if done != nil {
			<-done
		}
		close(p.closeDone)
	})
	<-p.closeDone
}

// Assess is pure with respect to the source. It never probes or refreshes data
// acquisition times, and reports health separately from measured free bytes.
func (p *Probe) Assess(at time.Time, maxAge time.Duration) View {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := View{Sample: p.last, Active: p.active != nil, Pending: p.pending, Closed: p.closed, Health: Health{AsOf: at, Heartbeat: p.heartbeat}}
	cause := NoCause
	switch {
	case at.IsZero() || maxAge <= 0 || maxAge > 24*time.Hour || p.heartbeat.After(at) || p.active != nil && p.active.started.After(at):
		cause = InvalidTime
	case p.pending:
		cause = CleanupPending
	case p.closed:
		cause = Cancelled
	case p.active != nil && !at.Before(p.active.deadline):
		cause = TimedOut
	case p.heartbeat.IsZero():
		cause = MissingHeartbeat
	case at.Sub(p.heartbeat) > maxAge:
		cause = MissingHeartbeat
	case p.last.AcquiredAt.After(at):
		cause = InvalidTime
	case at.Sub(p.last.AcquiredAt) > maxAge:
		cause = MissingHeartbeat
	case p.last.Cause != NoCause:
		cause = p.last.Cause
	case !p.last.FreeBytes.Present:
		cause = MissingHeartbeat
	}
	v.Health.Cause = cause
	if cause == NoCause {
		v.Health.State = Healthy
	} else {
		v.Sample.FreeBytes = nativeobserve.Number{}
		v.Health.State = Degraded
		if p.heartbeat.IsZero() || cause == InvalidTime || p.closed {
			v.Health.State = Unknown
		}
	}
	return v
}
