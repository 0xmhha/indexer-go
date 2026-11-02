package nativeobserve

import (
	"testing"
	"time"
)

func TestACKHeaderWithoutNativeEpochIsUnknownNotConflicting(t *testing.T) {
	b := positionBinding(SourceACKHeight)
	o := sample(b, clock())
	o.Position = Position{Hash: Hash{9}, Epochs: Epochs{Receiver: 1}}
	o.Coverage = Unverified
	a := catalog(t, b).Qualify(o, clock(), time.Minute)
	requireCode(t, a, PositionIncomplete)
	for _, c := range a.Codes {
		if c == InvalidPosition {
			t.Fatal("unavailable native/read epochs became an alleged conflict")
		}
	}
}
