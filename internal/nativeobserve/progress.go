package nativeobserve

import "time"

// Progress copies an existing owner result. Supplied epochs/hash are preserved,
// never filled from a newer head or the observer's declared binding.
func (t *Tap) Progress(metric Metric, height uint64, hash Hash, epochs Epochs, event, at time.Time, coverage Coverage) bool {
	return t.Offer(metric, Observation{Number: Number{true, height}, Quality: Known, Coverage: coverage,
		StartedAt: at, AcquiredAt: at, EventAt: event, Position: Position{Hash: hash, Epochs: epochs}})
}

// ACKClock separates observation time from progress. First sight (including a
// restart seed), reorg and generation changes have no proven progress time.
// It is owner-local; callers serialize calls as they already serialize ACKs.
type ACKClock struct {
	seen       bool
	height     uint64
	hash       Hash
	generation ReceiverGeneration
	event      time.Time
}

func (c *ACKClock) Observe(height uint64, hash Hash, generation ReceiverGeneration, at time.Time) time.Time {
	event := time.Time{}
	if c.seen && c.generation == generation {
		if c.height == height && c.hash == hash {
			event = c.event
		} else if height > c.height {
			event = at
		}
	}
	c.seen, c.height, c.hash, c.generation, c.event = true, height, hash, generation, event
	return event
}
