package model_test

import (
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/stretchr/testify/require"
)

func capability() model.BlobCapability {
	rev := strings.Repeat("1", 40)
	return model.BlobCapability{Version: 1, Profile: "fixture", Status: model.BlobUnsupported, Reason: "eip4844_disabled", Basis: "node_transaction_admission", Revision: rev, SourceURL: "https://example.test/repo/blob/" + rev + "/pool.rs"}
}
func TestBlobCapabilityContract(t *testing.T) {
	good := capability()
	require.NoError(t, good.Validate())
	for name, edit := range map[string]func(*model.BlobCapability){
		"supported-unproven": func(c *model.BlobCapability) { c.Status = "supported" },
		"version":            func(c *model.BlobCapability) { c.Version = 2 },
		"missing-profile":    func(c *model.BlobCapability) { c.Profile = "" },
		"bad-profile":        func(c *model.BlobCapability) { c.Profile = "foo\x00bar" },
		"basis":              func(c *model.BlobCapability) { c.Basis = "header_zero" },
		"bad-revision":       func(c *model.BlobCapability) { c.Revision = strings.Repeat("z", 40) },
		"short-revision":     func(c *model.BlobCapability) { c.Revision = "abc" },
		"credentials": func(c *model.BlobCapability) {
			c.SourceURL = strings.Replace(c.SourceURL, "https://", "https://user:secret@", 1)
		},
		"query":              func(c *model.BlobCapability) { c.SourceURL += "?token=secret" },
		"not-pinned":         func(c *model.BlobCapability) { c.SourceURL = strings.Replace(c.SourceURL, c.Revision, "main", 1) },
		"unknown-with-audit": func(c *model.BlobCapability) { c.Status = model.BlobUnknown; c.Reason = "not_audited" },
	} {
		t.Run(name, func(t *testing.T) { v := good; edit(&v); require.ErrorIs(t, v.Validate(), model.ErrCaptureInvalid) })
	}
	u := model.BlobCapability{Version: 1, Profile: "evm", Status: model.BlobUnknown, Reason: "not_audited"}
	require.NoError(t, u.Validate())
	d := model.ProjectionDescriptor{BlobCapability: &good, Plan: model.CapturePlan{Version: 1, Required: []model.DatasetRequirement{{Kind: model.DatasetBlobs, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}}}}}
	require.ErrorIs(t, d.ValidateBlobCapability(), model.ErrCaptureInvalid)
	d.BlobCapability = nil
	require.NoError(t, d.ValidateBlobCapability(), "legacy descriptors remain readable")
}
