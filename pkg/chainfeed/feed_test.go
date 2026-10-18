package chainfeed_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

func TestCapabilitiesFailClosedForMissingOrPartialDatasets(t *testing.T) {
	chain := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	full := model.DatasetCoverage{Scope: model.CoverageComplete}
	tracked := model.DatasetCoverage{Scope: model.CoverageTracked, SelectionHash: common.Hash{2}}
	plan := model.CapturePlan{Version: 1, Required: []model.DatasetRequirement{{Kind: model.DatasetAccounts, Coverage: full}}}
	caps := chainfeed.Capabilities{Chain: chain}
	require.ErrorIs(t, caps.CheckPlan(chain, plan), chainfeed.ErrUnsupported)
	caps.Datasets = []chainfeed.Capability{{Dataset: model.DatasetAccounts, Coverage: tracked}}
	require.ErrorIs(t, caps.CheckPlan(chain, plan), chainfeed.ErrUnsupported)
	caps.Datasets[0].Coverage = full
	require.NoError(t, caps.CheckPlan(chain, plan))
	plan.Required[0].Coverage = tracked
	require.NoError(t, caps.CheckPlan(chain, plan))
	caps.Chain.GenesisHash = common.Hash{3}
	require.ErrorIs(t, caps.CheckPlan(chain, plan), chainfeed.ErrUnsupported)
	caps.Chain = chain
	caps.Datasets[0].Dataset = model.DatasetHeaders
	require.ErrorIs(t, caps.CheckPlan(chain, plan), chainfeed.ErrUnsupported)
	caps.Datasets[0].Dataset = "sql_table_from_input"
	require.ErrorIs(t, caps.CheckPlan(chain, plan), chainfeed.ErrUnsupported)
}

func TestEnvelopeRequiresBoundedIdentifiedInput(t *testing.T) {
	block := model.FeedBlock{Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, Number: 1, Hash: common.Hash{2}}
	e := chainfeed.Envelope{Block: block, Kind: model.DatasetReceipts, Codec: "fixture.v1", Payload: []byte{1, 2}}
	require.Error(t, e.Validate(chainfeed.ReadLimits{}))
	require.Error(t, e.Validate(chainfeed.ReadLimits{MaxPayloadBytes: 1}))
	require.NoError(t, e.Validate(chainfeed.ReadLimits{MaxPayloadBytes: 2}))
	e.Payload = nil
	require.Error(t, e.Validate(chainfeed.ReadLimits{MaxPayloadBytes: 2}), "empty bytes are not proof of an empty dataset")
}
