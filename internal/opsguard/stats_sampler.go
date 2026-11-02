package opsguard

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/internal/nativeobserve"
)

const ObservationQueryLimit = 2 * time.Second
const ObservationRoundLimit = 5 * time.Second

var ErrStatsRunning = errors.New("opsguard: statistics sampler already running")

type statsSource struct {
	provider PostgresStats
	tap      *nativeobserve.Tap
}
type statsRound struct {
	source *statsSource
	cancel context.CancelFunc
	done   chan struct{}
}

// StatsSampler owns one sequential DB/WAL round, shared by capacity and observer
// consumers. Providers use the existing general pool; no API context is borrowed.
type StatsSampler struct {
	mu                     sync.Mutex
	sources                []*statsSource
	active                 *statsRound
	closed                 bool
	runnerDone             chan struct{}
	stop                   chan struct{}
	queryLimit, roundLimit time.Duration
	now                    func() time.Time
	journal                func(time.Time, string, int64, error)
}

// NewObservationStatsSampler provides observation-only acquisition. Actual App
// configuration/activation belongs to MON-02-D; no worker starts here.
func NewObservationStatsSampler() *StatsSampler {
	return &StatsSampler{queryLimit: ObservationQueryLimit, roundLimit: ObservationRoundLimit, now: time.Now}
}

func (s *StatsSampler) Attach(provider PostgresStats, tap *nativeobserve.Tap) (func(), error) {
	if provider == nil {
		return nil, errors.New("opsguard: missing statistics provider")
	}
	src := &statsSource{provider, tap}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("opsguard: statistics sampler closed")
	}
	if len(s.sources) >= nativeobserve.MaxResources {
		s.mu.Unlock()
		return nil, errors.New("opsguard: statistics source limit")
	}
	if s.active != nil {
		s.active.cancel()
	}
	s.sources = append(s.sources, src)
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			for n, v := range s.sources {
				if v == src {
					s.sources = append(s.sources[:n], s.sources[n+1:]...)
					break
				}
			}
			src.tap.Retire()
			var done chan struct{}
			if r := s.active; r != nil && r.source == src {
				r.cancel()
				done = r.done
			}
			s.mu.Unlock()
			if done != nil {
				<-done
			}
		})
	}, nil
}

// Close fences new admission, cancels and joins the actual provider, including
// providers that return late. It does not pretend an uncooperative read joined.
func (s *StatsSampler) Close() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		if s.stop != nil {
			close(s.stop)
		}
	}
	for _, src := range s.sources {
		src.tap.Retire()
	}
	var done chan struct{}
	if s.active != nil {
		s.active.cancel()
		done = s.active.done
	}
	runnerDone := s.runnerDone
	s.mu.Unlock()
	if done != nil {
		<-done
	}
	if runnerDone != nil {
		<-runnerDone
	}
}

func (s *StatsSampler) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("opsguard: invalid statistics interval")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	if s.runnerDone != nil {
		s.mu.Unlock()
		return ErrStatsRunning
	}
	if s.stop == nil {
		s.stop = make(chan struct{})
	}
	done := make(chan struct{})
	s.runnerDone = done
	stop := s.stop
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.runnerDone = nil; close(done); s.mu.Unlock() }()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return nil
		}
		s.Sample(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-stop:
			return nil
		case <-tick.C:
		}
	}
}

// Sample returns false rather than adding an overlapping query or retry. Context
// values are stripped (API reservations/transactions), cancellation is retained.
func (s *StatsSampler) Sample(parent context.Context) bool {
	s.mu.Lock()
	if s.closed || s.active != nil || len(s.sources) == 0 || parent.Err() != nil {
		s.mu.Unlock()
		return false
	}
	src := s.sources[len(s.sources)-1]
	base := context.Background()
	var ctx context.Context
	var cancel context.CancelFunc
	deadline, hasDeadline := parent.Deadline()
	if s.roundLimit > 0 {
		capDeadline := time.Now().Add(s.roundLimit)
		if !hasDeadline || capDeadline.Before(deadline) {
			deadline, hasDeadline = capDeadline, true
		}
	}
	if hasDeadline {
		ctx, cancel = context.WithDeadline(base, deadline)
	} else {
		ctx, cancel = context.WithCancel(base)
	}
	stop := context.AfterFunc(parent, cancel)
	if parent.Err() != nil {
		cancel()
	}
	r := &statsRound{src, cancel, make(chan struct{})}
	s.active = r
	s.mu.Unlock()
	defer func() {
		stop()
		cancel()
		s.mu.Lock()
		s.active = nil
		close(r.done)
		s.mu.Unlock()
	}()
	for _, q := range []struct {
		kind   string
		metric nativeobserve.Metric
		read   func(context.Context) (int64, error)
	}{
		{KindPostgresDatabase, nativeobserve.DatabaseBytes, src.provider.DatabaseBytes},
		{KindPostgresWAL, nativeobserve.WALBytes, src.provider.WALBytes},
	} {
		if ctx.Err() != nil {
			break
		}
		start := s.now()
		qctx, qcancel := context.WithTimeout(ctx, s.queryLimit)
		v, err := q.read(qctx)
		if qctx.Err() != nil {
			err = qctx.Err()
		}
		end := s.now()
		qcancel()
		s.mu.Lock()
		valid := !s.closed && s.active == r && len(s.sources) > 0 && s.sources[len(s.sources)-1] == src && ctx.Err() == nil && parent.Err() == nil
		s.mu.Unlock()
		if !valid {
			break
		}
		o := nativeobserve.Observation{Quality: nativeobserve.Known, Coverage: nativeobserve.Exact, Number: nativeobserve.Number{Present: true}, StartedAt: start, AcquiredAt: end}
		if err != nil || v < 0 {
			o.Quality = nativeobserve.Failed
			o.Number.Present = false
			o.Cause = nativeobserve.SourceUnavailable
			if errors.Is(err, context.DeadlineExceeded) {
				o.Cause = nativeobserve.TimedOut
			}
			var state interface{ SQLState() string }
			if errors.As(err, &state) && state.SQLState() == "42501" {
				o.Cause = nativeobserve.PermissionDenied
			}
			if v < 0 && err == nil {
				err = errors.New("opsguard: negative statistics value")
			}
		} else {
			o.Number.Value = uint64(v)
		}
		src.tap.Offer(q.metric, o)
		if s.journal != nil {
			s.journal(end, q.kind, v, err)
		}
	}
	return true
}
