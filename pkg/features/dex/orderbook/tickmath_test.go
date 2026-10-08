package orderbook

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSqrtRatioAtTick(t *testing.T) {
	// TickMath's published bounds and values.
	assert.Equal(t, minSqrtRatio.String(), SqrtRatioAtTick(MinTick).String())
	assert.Equal(t, maxSqrtRatio.String(), SqrtRatioAtTick(MaxTick).String())
	assert.Equal(t, q96.String(), SqrtRatioAtTick(0).String())
	assert.Equal(t, "79232123823359799118286999568", SqrtRatioAtTick(1).String())
	assert.Equal(t, "79224201403219477170569942574", SqrtRatioAtTick(-1).String())
	for tick := int32(-50); tick < 50; tick++ {
		assert.Equal(t, -1, SqrtRatioAtTick(tick).Cmp(SqrtRatioAtTick(tick+1)), "increasing at %d", tick)
	}
}
