package stablenet

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// wbftDifficulty marks headers produced by WBFT consensus.
var wbftDifficulty = big.NewInt(1)

// Positions in the WBFT extra-data list: [vanity, randaoReveal, prevRound,
// prevPreparedSeal, prevCommittedSeal, round, preparedSeal, committedSeal,
// gasTip, epochInfo].
const (
	extraFields        = 10
	extraRound         = 5
	extraPreparedSeal  = 6
	extraCommittedSeal = 7
)

var (
	rlpZeroUint  = []byte{0x80} // uint 0
	rlpEmptyList = []byte{0xc0} // absent (nil) seal
)

// HeaderHash computes a StableNet block hash. WBFT blocks (difficulty 1) hash
// the header with the current round's seals removed and the round set to 0,
// so the hash identifies the block independently of who signed it and in
// which round. Other headers use the plain Ethereum rule.
func HeaderHash(h *types.Header) common.Hash {
	if h.Difficulty == nil || h.Difficulty.Cmp(wbftDifficulty) != 0 {
		return h.Hash()
	}
	filtered, err := filteredExtra(h.Extra)
	if err != nil {
		// Not a WBFT extra: fall back to the plain rule; a mismatch with the
		// reported hash is then reported by the profile.
		return h.Hash()
	}
	cp := types.CopyHeader(h)
	cp.Extra = filtered
	return cp.Hash()
}

// filteredExtra re-encodes the extra data with preparedSeal and committedSeal
// cleared and round reset, keeping every other element byte for byte.
func filteredExtra(extra []byte) ([]byte, error) {
	var elems []rlp.RawValue
	if err := rlp.DecodeBytes(extra, &elems); err != nil {
		return nil, fmt.Errorf("decode WBFT extra: %w", err)
	}
	if len(elems) != extraFields {
		return nil, fmt.Errorf("WBFT extra has %d elements, want %d", len(elems), extraFields)
	}
	elems[extraRound] = rlpZeroUint
	elems[extraPreparedSeal] = rlpEmptyList
	elems[extraCommittedSeal] = rlpEmptyList
	return rlp.EncodeToBytes(elems)
}
