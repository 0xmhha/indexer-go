package opsguard

import "time"

const (
	// forecastWindow keeps the samples of the long-term average.
	forecastWindow = time.Hour
	// burstWindow is where the steepest recent change is searched.
	burstWindow = 15 * time.Minute
	// burstGap is the shortest interval a recent change is measured over.
	burstGap = time.Minute
	// averageSpan is the shortest history the long-term average needs.
	averageSpan = 5 * time.Minute
	// maxSeries bounds the retained samples.
	maxSeries = 512
)

type sample struct {
	at time.Time
	v  int64
}

// series holds valid samples of one value for the forecast.
type series struct{ s []sample }

// add appends a sample. A clock that does not move forward discards the
// history: rates over it would be meaningless.
func (x *series) add(at time.Time, v int64) {
	if n := len(x.s); n > 0 && !at.After(x.s[n-1].at) {
		x.s = x.s[:0]
	}
	x.s = append(x.s, sample{at: at, v: v})
	cut := 0
	for cut < len(x.s) && (at.Sub(x.s[cut].at) > forecastWindow || len(x.s)-cut > maxSeries) {
		cut++
	}
	x.s = append(x.s[:0], x.s[cut:]...)
}

// rate is the bytes per second toward exhaustion: growth for sign 1, decline
// for sign -1. It is the larger of the steepest change over at least a minute
// in the last 15 minutes and the average over the retained hour. It is unknown
// without enough history or without any change toward exhaustion.
func (x *series) rate(sign int64) (float64, bool) {
	n := len(x.s)
	if n < 2 {
		return 0, false
	}
	last := x.s[n-1].at
	best := 0.0
	for i := n - 1; i > 0 && last.Sub(x.s[i].at) <= burstWindow; i-- {
		for j := i - 1; j >= 0; j-- {
			dt := x.s[i].at.Sub(x.s[j].at)
			if dt < burstGap {
				continue
			}
			if r := float64(sign*(x.s[i].v-x.s[j].v)) / dt.Seconds(); r > best {
				best = r
			}
			break
		}
	}
	if span := last.Sub(x.s[0].at); span >= averageSpan {
		if r := float64(sign*(x.s[n-1].v-x.s[0].v)) / span.Seconds(); r > best {
			best = r
		}
	}
	return best, best > 0
}

// forecast is the time until remaining bytes are used at the series' rate.
// Nothing remaining is zero; an unknown rate is unknown, never infinite.
func (x *series) forecast(remaining int64, sign int64) (time.Duration, bool) {
	if remaining <= 0 {
		return 0, true
	}
	r, ok := x.rate(sign)
	if !ok {
		return 0, false
	}
	seconds := float64(remaining) / r
	if seconds > float64(100*365*24*time.Hour/time.Second) {
		return 0, false
	}
	return time.Duration(seconds * float64(time.Second)), true
}
