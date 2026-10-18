package evm

import "github.com/0xmhha/indexer-go/pkg/core/model"

// WithBlobCapability records a code-owned audited policy. It does not change
// header/transaction decoding or claim execution/fork-rule validation.
func WithBlobCapability(c model.BlobCapability) Option {
	return func(p *Profile) { p.blobCapability = &c }
}
func (p *Profile) BlobCapability() model.BlobCapability {
	if p.blobCapability != nil {
		return *p.blobCapability
	}
	return model.BlobCapability{Version: 1, Profile: p.ID(), Status: model.BlobUnknown, Reason: "not_audited"}
}
