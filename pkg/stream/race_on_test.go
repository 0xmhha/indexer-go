//go:build race

package stream

// raceEnabled reports a build with the race detector, which runs code
// several times slower: latency bounds of load tests do not apply to it.
const raceEnabled = true
