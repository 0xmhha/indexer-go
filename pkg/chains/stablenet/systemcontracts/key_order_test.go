package systemcontracts

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestNumericKeysSortNumerically: for every system contract key built from
// numbers, byte order equals numeric order, so iterating a prefix returns
// entries in block, transaction and log order (refactoring plan R1-3).
func TestNumericKeysSortNumerically(t *testing.T) {
	addr := common.HexToAddress("0x00000000000000000000000000000000000000A1")
	keys := map[string]func(a, b, c uint64) []byte{
		"MintEvent":                MintEventKey,
		"BurnEvent":                BurnEventKey,
		"MemberChangeEvent":        func(a, b, _ uint64) []byte { return MemberChangeEventKey(addr, a, b) },
		"GasTipUpdateEvent":        func(a, b, _ uint64) []byte { return GasTipUpdateEventKey(a, b) },
		"EmergencyPauseEvent":      func(a, b, _ uint64) []byte { return EmergencyPauseEventKey(addr, a, b) },
		"MaxProposalsUpdateEvent":  func(a, b, _ uint64) []byte { return MaxProposalsUpdateEventKey(addr, a, b) },
		"ProposalExecutionSkipped": func(a, b, _ uint64) []byte { return ProposalExecutionSkippedEventKey(addr, a, b) },
		"AuthorizedAccountEvent":   func(a, b, _ uint64) []byte { return AuthorizedAccountEventKey(addr, a, b) },
		"ProposalStatusIndex":      func(a, _, _ uint64) []byte { return ProposalStatusIndexKey(addr, uint8(a), "p") },
	}
	rnd := rand.New(rand.NewSource(1))
	// Values around digit-count boundaries plus random ones.
	pick := func(max uint64) uint64 {
		edges := []uint64{0, 1, 2, 9, 10, 11, 99, 100, 999, 1000, 99999, 100000}
		if rnd.Intn(2) == 0 {
			if v := edges[rnd.Intn(len(edges))]; v <= max {
				return v
			}
		}
		return uint64(rnd.Int63n(int64(max) + 1))
	}
	cmp := func(x, y [3]uint64) int {
		for i := range x {
			if x[i] != y[i] {
				if x[i] < y[i] {
					return -1
				}
				return 1
			}
		}
		return 0
	}
	for name, key := range keys {
		maxA := uint64(1 << 40)
		if name == "ProposalStatusIndex" {
			maxA = 255
		}
		for i := 0; i < 2000; i++ {
			x := [3]uint64{pick(maxA), pick(99999), pick(99999)}
			y := [3]uint64{pick(maxA), pick(99999), pick(99999)}
			want := cmp(x, y)
			kx, ky := key(x[0], x[1], x[2]), key(y[0], y[1], y[2])
			if want == 0 {
				continue
			}
			if got := bytes.Compare(kx, ky); got != want && !bytes.Equal(kx, ky) {
				t.Fatalf("%s: %v vs %v: key order %d, numeric order %d (%s, %s)", name, x, y, got, want, kx, ky)
			}
		}
	}
}
