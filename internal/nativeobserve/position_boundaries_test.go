package nativeobserve

import (
	"math"
	"testing"
	"time"
)

func TestUnknownPositionDoesNotInventInvalidEvidence(t *testing.T) {
	b := positionBinding(SourceACKHeight)
	o := sample(b, clock())
	o.Quality, o.Cause, o.Number = Failed, TimedOut, Number{}
	a := catalog(t, b).Qualify(o, clock(), time.Minute)
	requireCode(t, a, SampleFailed)
	for _, code := range a.Codes {
		if code == InvalidPosition {
			t.Fatal("missing position misclassified as a known conflict")
		}
	}
}

func TestNativeHeightOverflowIsNotQualified(t *testing.T) {
	b := positionBinding(RawDurableHeight)
	o := sample(b, clock())
	o.Position, o.Number.Value = Position{Hash{9}, b.Epochs}, math.MaxUint64
	requireCode(t, catalog(t, b).Qualify(o, clock(), time.Minute), InvalidNumber)
	b.Chain.AnchorHeight = math.MaxUint64
	_, a := NewCatalog([]Binding{b})
	requireCode(t, a, InvalidBinding)
}

func TestKnownAnchorConflictsRemainVisibleWithMissingTime(t *testing.T) {
	b := positionBinding(SQLVisibleHeight)
	b.Chain.AnchorHeight, b.Chain.AnchorHash = 100, Hash{7}
	c := catalog(t, b)
	for _, height := range []uint64{99, 100} {
		o := sample(b, clock())
		o.Number.Value, o.Position = height, Position{Hash{9}, b.Epochs}
		o.AcquiredAt = time.Time{}
		a := c.Qualify(o, clock(), time.Minute)
		requireCode(t, a, MissingTime)
		requireCode(t, a, InvalidPosition)
	}
}
