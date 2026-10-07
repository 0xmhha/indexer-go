package consensus

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// TestConsensusEventCodecs: the WBFT feature's events are recorded in the
// outbox with their block, so each type encodes and decodes unchanged.
func TestConsensusEventCodecs(t *testing.T) {
	at := time.Unix(1_700_000_000, 0).UTC()
	epoch := uint64(3)
	v := []common.Address{common.HexToAddress("0x1"), common.HexToAddress("0x2")}
	for _, ev := range []events.Event{
		&ConsensusBlockEvent{BlockNumber: 9, BlockHash: common.HexToHash("0xb"), Round: 2, Proposer: v[0],
			ValidatorCount: 4, ParticipationRate: 0.75, EpochNumber: &epoch, EpochValidators: v, CreatedAt: at},
		&ConsensusForkEvent{ForkBlockNumber: 9, Chain1Weight: "10", Resolved: true, WinningChain: 2, DetectedAt: at, CreatedAt: at},
		&ConsensusValidatorChangeEvent{BlockNumber: 9, ChangeType: "added", AddedValidators: v, ValidatorSet: v, CreatedAt: at},
		&ConsensusErrorEvent{BlockNumber: 9, ErrorType: "round_change", MissedValidators: v, ParticipationRate: 0.5, CreatedAt: at},
	} {
		data, err := events.MarshalEvent(ev)
		require.NoError(t, err, "%s", ev.Type())
		got, err := events.UnmarshalEvent(ev.Type(), data)
		require.NoError(t, err, "%s", ev.Type())
		require.Equal(t, ev, got)
	}
}
