package capture_test

import (
	"context"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type schedulingOracle struct {
	*oracle
	roles []bool
}

func (o *schedulingOracle) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	o.roles = append(o.roles, port.IsRPCMaintenance(ctx))
	return o.oracle.HashAt(ctx, n)
}

func TestOnlyControlReadsHaveMaintenanceHint(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := openFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			o := &schedulingOracle{oracle: newOracle()}
			s, err := capture.New(db, chain, o)
			require.NoError(t, err)
			initialize(t, s)
			o.roles = nil
			require.NoError(t, s.Commit(ctx, input(1, 1, common.Hash{2}, chain.GenesisHash)))
			_, changed, err := capture.ControlStep(ctx, nil, s, nil, false, 128)
			require.NoError(t, err)
			require.False(t, changed)
			require.False(t, port.IsRPCMaintenance(ctx), "hint does not escape the control call")
			require.NoError(t, s.Commit(ctx, input(2, 1, common.Hash{3}, common.Hash{2})))
			require.Equal(t, []bool{false, true, false}, o.roles, "both commits still make independent foreground fresh reads")
			o.roles = nil
			st, err := s.State(ctx)
			require.NoError(t, err)
			_, changed, err = capture.ControlStep(ctx, nil, s, finalityFixture{block: st.Through}, false, 128)
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, []bool{true, true, false}, o.roles, "finality transaction fresh is also foreground")
		})
	}
}
