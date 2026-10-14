package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A slow block accounts for more than 1% of a 60-block load. Its delay
// must appear in ingest p99, just as it does in the subscribers' samples.
func TestSLOStatsKeepSlowBlockAtP99(t *testing.T) {
	samples := make([]time.Duration, 60)
	for i := range samples {
		samples[i] = 10 * time.Millisecond
	}
	samples[7] = 300 * time.Millisecond
	stats := statsOf(samples)
	require.Equal(t, 10*time.Millisecond, stats.p50)
	require.Equal(t, 300*time.Millisecond, stats.p99)
	require.Equal(t, stats.p99, stats.max)

	// Exactly 100 samples use the 99th observation, not the maximum.
	samples = make([]time.Duration, 100)
	for i := range samples {
		samples[i] = time.Duration(i+1) * time.Millisecond
	}
	stats = statsOf(samples)
	require.Equal(t, 99*time.Millisecond, stats.p99)
	require.Equal(t, 100*time.Millisecond, stats.max)
	require.Equal(t, time.Second, statsOf([]time.Duration{time.Second}).p99)
}
