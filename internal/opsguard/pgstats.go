package opsguard

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/nativeobserve"
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

// AttachPostgresStats shares the monitor's single sampler. Detach cancels and
// joins any read using this source before the Store may close. Latest attach
// wins; removing it falls back to the earlier live source.
func (m *Monitor) AttachPostgresStats(s PostgresStats) func() {
	detach, err := m.statsSampler.Attach(s, nil)
	if err != nil {
		return func() {}
	}
	return detach
}

// StatsSampler exposes the existing owner for opt-in observation integration;
// consumers must share it instead of constructing another poller.
func (m *Monitor) StatsSampler() *StatsSampler { return m.statsSampler }

// WithObservationStats opts into stricter acquisition bounds, without changing
// legacy defaults or introducing a configuration key.
func WithObservationStats() Option { return func(m *Monitor) { m.observedStats = true } }

// AttachObservedPostgresStats attaches a typed nonblocking tap to that same owner.
func (m *Monitor) AttachObservedPostgresStats(s PostgresStats, tap *nativeobserve.Tap) (func(), error) {
	return m.statsSampler.Attach(s, tap)
}
func (m *Monitor) sampleStats(ctx context.Context) { m.statsSampler.Sample(ctx) }

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
