package chains

import "github.com/0xmhha/indexer-go/pkg/core/model"

// BlobCapabilityProfile optionally declares a pinned audit of node policy.
// It must not infer blob support from Cancun header fields or an EVM label.
type BlobCapabilityProfile interface{ BlobCapability() model.BlobCapability }

func BlobCapabilityOf(p Profile) (model.BlobCapability, error) {
	if p == nil {
		return model.BlobCapability{}, model.ErrCaptureInvalid
	}
	out := model.BlobCapability{Version: 1, Profile: p.ID(), Status: model.BlobUnknown, Reason: "not_audited"}
	if capable, ok := p.(BlobCapabilityProfile); ok {
		out = capable.BlobCapability()
	}
	if e := out.Validate(); e != nil {
		return out, e
	}
	// An embedding project profile may override ID while inheriting the base
	// node policy. Bind the reported policy to the selected outer identity.
	out.Profile = p.ID()
	if e := out.Validate(); e != nil {
		return out, e
	}
	return out, nil
}
