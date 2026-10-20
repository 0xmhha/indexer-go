package opsguard

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Journal kinds of PostgreSQL's own size statistics. They explain a data
// directory sample (Dpg) and never halt: a statistics query can fail for
// lack of a privilege while the data directory is fine.
const (
	KindPostgresDatabase = "postgres_database"
	KindPostgresWAL      = "postgres_wal"
	// statsTimeout bounds one statistics query.
	statsTimeout = 30 * time.Second
)

// PostgresStats reads PostgreSQL's own size statistics: the database (Ddb)
// and the server's WAL (Dwal).
type PostgresStats interface {
	DatabaseBytes(ctx context.Context) (int64, error)
	WALBytes(ctx context.Context) (int64, error)
}

type statsSource struct{ s PostgresStats }

// AttachPostgresStats samples s at postgres_stats_interval until the returned
// detach runs; detach must run before s closes, and may run more than once.
// While several are attached the latest is sampled: the chains of
// multichain mode share one database.
func (m *Monitor) AttachPostgresStats(s PostgresStats) (detach func()) {
	src := &statsSource{s: s}
	m.mu.Lock()
	m.stats = append(m.stats, src)
	m.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			for i, x := range m.stats {
				if x == src {
					m.stats = append(m.stats[:i], m.stats[i+1:]...)
					break
				}
			}
		})
	}
}

// sampleStats journals one sample of each statistic of the latest attached
// source, if any.
func (m *Monitor) sampleStats(ctx context.Context) {
	m.mu.Lock()
	var s PostgresStats
	if n := len(m.stats); n > 0 {
		s = m.stats[n-1].s
	}
	m.mu.Unlock()
	if s == nil {
		return
	}
	for _, q := range []struct {
		kind string
		read func(context.Context) (int64, error)
	}{{KindPostgresDatabase, s.DatabaseBytes}, {KindPostgresWAL, s.WALBytes}} {
		qctx, cancel := context.WithTimeout(ctx, statsTimeout)
		v, err := q.read(qctx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		m.observeStat(m.now(), q.kind, v, err)
	}
}

func (m *Monitor) observeStat(at time.Time, kind string, v int64, err error) {
	rec := Record{At: at, Kind: kind, Level: LevelNormal}
	if err != nil {
		rec.Level, rec.Error = LevelUnknown, err.Error()
	} else {
		rec.Bytes = &v
	}
	if m.transition(kind, rec.Level) {
		if err != nil {
			m.logger.Warn("PostgreSQL statistic unavailable; it does not stop indexing", zap.String("value", kind), zap.Error(err))
		} else {
			m.logger.Info("PostgreSQL statistic available again", zap.String("value", kind))
		}
	}
	m.record(rec)
}
