package model_test

import (
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

func captureFixture(t *testing.T) (model.CapturePlan, model.CaptureManifest) {
	t.Helper()
	p := model.CapturePlan{Version: 1, Required: []model.DatasetRequirement{
		{Kind: model.DatasetReceipts, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}},
		{Kind: model.DatasetAccounts, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}},
		{Kind: model.DatasetBlobs, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}},
	}}
	digest, err := p.Digest()
	require.NoError(t, err)
	m := model.CaptureManifest{Identity: model.CaptureIdentity{
		Block:      model.FeedBlock{Chain: model.FeedChain{ID: "5042002", GenesisHash: common.Hash{1}}, Number: 10, Hash: common.Hash{2}},
		Generation: 1, PlanVersion: p.Version, PlanDigest: digest,
	}}
	for _, r := range p.Required {
		m.Datasets = append(m.Datasets, model.DatasetEvidence{
			Kind: r.Kind, Coverage: r.Coverage, Status: model.DatasetPersisted,
			Digest: common.Hash{3}, Codec: "fixture.v1", Source: "verified-fixture", Verification: model.VerificationCommitment,
		})
	}
	return p, m
}

func TestCaptureNeverCompletesMissingOrUnstoredInputs(t *testing.T) {
	for _, state := range []model.DatasetStatus{
		"", model.DatasetMissing, model.DatasetFetching, model.DatasetCaptured, model.DatasetVerified,
		model.DatasetUnsupported, model.DatasetFailed, model.DatasetPruned, "unknown",
	} {
		t.Run(string(state), func(t *testing.T) {
			p, m := captureFixture(t)
			m.Datasets[1].Status = state
			require.ErrorIs(t, p.ValidateComplete(m), model.ErrCaptureIncomplete)
		})
	}
	t.Run("absent", func(t *testing.T) {
		p, m := captureFixture(t)
		m.Datasets = m.Datasets[:1]
		require.ErrorIs(t, p.ValidateComplete(m), model.ErrCaptureIncomplete)
	})
}

func TestCaptureRequiresEvidenceForEmptyAndInapplicable(t *testing.T) {
	p, m := captureFixture(t)
	require.NoError(t, p.ValidateComplete(m))
	m.Datasets[2].Status = model.DatasetVerifiedEmpty
	require.NoError(t, p.ValidateComplete(m))
	m.Datasets[2].Digest = common.Hash{}
	require.ErrorIs(t, p.ValidateComplete(m), model.ErrCaptureIncomplete)
	m.Datasets[2].Status = model.DatasetNotApplicable
	require.ErrorIs(t, p.ValidateComplete(m), model.ErrCaptureIncomplete)
	m.Datasets[2].Verification = model.VerificationForkRules
	m.Datasets[2].Reason = "fixture fork has no blobs"
	require.NoError(t, p.ValidateComplete(m))
	m.Datasets[2].Source = ""
	require.ErrorIs(t, p.ValidateComplete(m), model.ErrCaptureIncomplete)
}

func TestCapturePlanBindsExactRequirements(t *testing.T) {
	p, m := captureFixture(t)
	original := slices.Clone(p.Required)
	digest, err := p.Digest()
	require.NoError(t, err)
	assert.Equal(t, original, p.Required, "digest must not mutate a shared plan")
	slices.Reverse(p.Required)
	reversed, err := p.Digest()
	require.NoError(t, err)
	assert.Equal(t, digest, reversed)
	require.NoError(t, p.ValidateComplete(m))
	p.Required = append(p.Required, model.DatasetRequirement{Kind: model.DatasetTraces, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}})
	require.ErrorIs(t, p.ValidateComplete(m), model.ErrCaptureInvalid, "same version with different content must not reuse a manifest")
	p.Required = append(p.Required, p.Required[0])
	require.ErrorIs(t, p.Validate(), model.ErrCaptureInvalid)
	p.Required = []model.DatasetRequirement{{Kind: `receipts; DROP TABLE blocks; --`}}
	require.ErrorIs(t, p.Validate(), model.ErrCaptureInvalid, "dataset names are enums, never arbitrary SQL identifiers")
}

func TestCaptureCoverageCannotOverclaimState(t *testing.T) {
	full := model.DatasetCoverage{Scope: model.CoverageComplete}
	a := model.DatasetCoverage{Scope: model.CoverageTracked, SelectionHash: common.Hash{1}}
	b := model.DatasetCoverage{Scope: model.CoverageTracked, SelectionHash: common.Hash{2}}
	assert.True(t, full.Covers(a))
	assert.True(t, a.Covers(a))
	assert.False(t, a.Covers(full))
	assert.False(t, a.Covers(b))
	assert.False(t, model.DatasetCoverage{Scope: model.CoverageTracked}.Covers(a))
	p, m := captureFixture(t)
	m.Datasets[1].Coverage = a
	require.ErrorIs(t, p.ValidateComplete(m), model.ErrCaptureIncomplete)
	p.Required[1].Coverage = b
	digest, err := p.Digest()
	require.NoError(t, err)
	m.Identity.PlanDigest = digest
	require.ErrorIs(t, p.ValidateComplete(m), model.ErrCaptureIncomplete)
	m.Datasets[1].Coverage = b
	require.NoError(t, p.ValidateComplete(m))
}

func TestCaptureRejectsMalformedIdentityOrEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*model.CaptureManifest){
		"zero generation":   func(m *model.CaptureManifest) { m.Identity.Generation = 0 },
		"wrong version":     func(m *model.CaptureManifest) { m.Identity.PlanVersion++ },
		"wrong plan":        func(m *model.CaptureManifest) { m.Identity.PlanDigest = common.Hash{9} },
		"zero block hash":   func(m *model.CaptureManifest) { m.Identity.Block.Hash = common.Hash{} },
		"wrong genesis":     func(m *model.CaptureManifest) { m.Identity.Block.Number = 0 },
		"duplicate dataset": func(m *model.CaptureManifest) { m.Datasets[1] = m.Datasets[0] },
		"no source":         func(m *model.CaptureManifest) { m.Datasets[0].Source = "" },
		"no codec":          func(m *model.CaptureManifest) { m.Datasets[0].Codec = "" },
		"no verification":   func(m *model.CaptureManifest) { m.Datasets[0].Verification = "" },
	} {
		t.Run(name, func(t *testing.T) {
			p, m := captureFixture(t)
			mutate(&m)
			require.Error(t, p.ValidateComplete(m))
		})
	}
	for _, id := range []string{"", "01", "-1", "+1", " 1", "0x01", "1e3", "1;--",
		"115792089237316195423570985008687907853269984665640564039457584007913129639936"} {
		require.Error(t, (model.FeedChain{ID: id, GenesisHash: common.Hash{1}}).Validate(), id)
	}
	genesis := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	require.NoError(t, (model.FeedBlock{Chain: genesis, Hash: genesis.GenesisHash}).Validate())
	require.NoError(t, (model.FeedChain{ID: "0", GenesisHash: common.Hash{1}}).Validate())
}
