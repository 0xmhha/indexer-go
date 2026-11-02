// Package monitorpredicate contains exact arithmetic shared by offline and
// resource-scoped observation evaluators. It has no acquisition or authority.
package monitorpredicate

import "math/bits"

// ProductGE compares a*b >= c*d using the full 128-bit products.
func ProductGE(a, b, c, d uint64) bool {
	ah, al := bits.Mul64(a, b)
	bh, bl := bits.Mul64(c, d)
	return ah > bh || ah == bh && al >= bl
}
