package monitorpredicate

import (
	"math"
	"math/big"
	"testing"
)

func TestProductsAgainstIndependentBigIntegerOracle(t *testing.T) {
	values := []uint64{0, 1, 9999, 10000, 1 << 32, math.MaxUint64 / 2, math.MaxUint64 - 1, math.MaxUint64}
	for _, a := range values {
		for _, b := range values {
			for _, c := range values {
				for _, d := range values {
					left := new(big.Int).Mul(new(big.Int).SetUint64(a), new(big.Int).SetUint64(b))
					right := new(big.Int).Mul(new(big.Int).SetUint64(c), new(big.Int).SetUint64(d))
					if got := ProductGE(a, b, c, d); got != (left.Cmp(right) >= 0) {
						t.Fatalf("%d*%d >= %d*%d: %v", a, b, c, d, got)
					}
				}
			}
		}
	}
}
