package opsguard

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrResumeRefused means a resume condition does not hold; the latch stays.
var ErrResumeRefused = errors.New("opsguard: resume refused")

// ResumeReport describes a resume check.
type ResumeReport struct {
	Latch     *Latch
	FreeBytes []int64
	Postgres  *int64
	Cleared   bool
}

// ResumeOptions replaces the probes and the wait between samples, for tests.
type ResumeOptions struct {
	Free  func(string) (uint64, error)
	Tree  func(context.Context, string, int) (int64, error)
	Sleep func(context.Context, time.Duration) error
}

// Resume removes the halt latch only if every resume condition holds now:
// ResumeSamples filesystem samples, ResumeInterval apart, each above
// resume_free_bytes, and a data directory below 80% of its budget. Without
// a capacity section only the latch is checked. A raw window halt, an
// integrity halt or an unreadable latch is never cleared here: raw usage does
// not fall while collection is off, and the other causes need the operator's
// own verification. The storage identity is checked again when the window
// opens at the next start.
func Resume(ctx context.Context, cfg Config, opts ResumeOptions) (ResumeReport, error) {
	cfg = cfg.WithDefaults()
	if opts.Free == nil {
		opts.Free = FreeBytes
	}
	if opts.Tree == nil {
		opts.Tree = TreeBytes
	}
	if opts.Sleep == nil {
		opts.Sleep = sleep
	}
	l, err := ReadLatch(cfg.HaltFile)
	report := ResumeReport{Latch: l}
	if err != nil || l == nil {
		return report, err
	}
	if l.Reason == ReasonIntegrity || l.Source == SourceRaw {
		return report, fmt.Errorf("%w: a %s %s halt needs the operator's verification; remove %s by hand after it", ErrResumeRefused, l.Reason, l.Source, cfg.HaltFile)
	}
	if c := cfg.Capacity; c != nil {
		for i := 0; i < c.ResumeSamples; i++ {
			if i > 0 {
				if err := opts.Sleep(ctx, c.ResumeInterval); err != nil {
					return report, err
				}
			}
			free, err := opts.Free(c.FilesystemPath)
			if err != nil {
				return report, fmt.Errorf("%w: filesystem sample: %v", ErrResumeRefused, err)
			}
			v := int64(min(free, uint64(1<<62)))
			report.FreeBytes = append(report.FreeBytes, v)
			if v <= c.ResumeFreeBytes {
				return report, fmt.Errorf("%w: free %d bytes not above resume_free_bytes %d", ErrResumeRefused, v, c.ResumeFreeBytes)
			}
		}
		if c.PostgresDataDir != "" {
			used, err := opts.Tree(ctx, c.PostgresDataDir, c.PostgresMaxEntries)
			if err != nil {
				return report, fmt.Errorf("%w: data directory sample: %v", ErrResumeRefused, err)
			}
			report.Postgres = &used
			if float64(used) >= warnRatio*float64(c.PostgresBudgetBytes) {
				return report, fmt.Errorf("%w: data directory %d bytes not below 80%% of postgres_budget_bytes %d", ErrResumeRefused, used, c.PostgresBudgetBytes)
			}
		}
	}
	if err := clearLatch(cfg.HaltFile); err != nil {
		return report, err
	}
	report.Cleared = true
	return report, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
