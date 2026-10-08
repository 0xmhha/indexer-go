package orderbook

import "math/big"

// Uniswap V3 tick bounds and their square root prices (TickMath).
const (
	MinTick = -887272
	MaxTick = 887272
)

var (
	q96            = new(big.Int).Lsh(big.NewInt(1), 96)
	q192           = new(big.Int).Lsh(big.NewInt(1), 192)
	maxUint256     = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	minSqrtRatio   = big.NewInt(4295128739)
	maxSqrtRatio   = mustBig("1461446703485210103287273052203988822378723970342")
	sqrtRatioMults = func() []*big.Int {
		hex := []string{
			"fff97272373d413259a46990580e213a", "fff2e50f5f656932ef12357cf3c7fdcc", "ffe5caca7e10e4e61c3624eaa0941cd0",
			"ffcb9843d60f6159c9db58835c926644", "ff973b41fa98c081472e6896dfb254c0", "ff2ea16466c96a3843ec78b326b52861",
			"fe5dee046a99a2a811c461f1969c3053", "fcbe86c7900a88aedcffc83b479aa3a4", "f987a7253ac413176f2b074cf7815e54",
			"f3392b0822b70005940c7a398e4b70f3", "e7159475a2c29b7443b29c7fa6e889d9", "d097f3bdfd2022b8845ad8f792aa5825",
			"a9f746462d870fdf8a65dc1f90e061e5", "70d869a156d2a1b890bb3df62baf32f7", "31be135f97d08fd981231505542fcfa6",
			"9aa508b5b7a84e1c677de54f3e99bc9", "5d6af8dedb81196699c329225ee604", "2216e584f5fa1ea926041bedfe98",
			"48a170391f7dc42444e8fa2",
		}
		out := make([]*big.Int, len(hex))
		for i, h := range hex {
			out[i], _ = new(big.Int).SetString(h, 16)
		}
		return out
	}()
	ratioOdd, _  = new(big.Int).SetString("fffcb933bd6fad37aa2d162d1a594001", 16)
	ratioEven, _ = new(big.Int).SetString("100000000000000000000000000000000", 16)
)

func mustBig(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("orderbook: bad constant " + s)
	}
	return v
}

// SqrtRatioAtTick is TickMath.getSqrtRatioAtTick: sqrt(1.0001^tick) as a
// Q64.96, exactly as a pool computes it. A tick outside the bounds is
// clamped to them.
func SqrtRatioAtTick(tick int32) *big.Int {
	t := int64(max(MinTick, min(MaxTick, tick)))
	abs := t
	if abs < 0 {
		abs = -abs
	}
	ratio := new(big.Int).Set(ratioEven)
	if abs&1 != 0 {
		ratio.Set(ratioOdd)
	}
	for i, m := range sqrtRatioMults {
		if abs&(2<<i) != 0 {
			ratio.Mul(ratio, m).Rsh(ratio, 128)
		}
	}
	if t > 0 {
		ratio.Div(maxUint256, ratio)
	}
	rem := new(big.Int).And(ratio, big.NewInt(1<<32-1))
	ratio.Rsh(ratio, 32)
	if rem.Sign() != 0 {
		ratio.Add(ratio, big.NewInt(1))
	}
	return ratio
}
