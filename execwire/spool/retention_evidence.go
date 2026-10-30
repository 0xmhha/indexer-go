package spool

import (
	"context"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
)

// WithRetentionEvidence holds source ownership for the callback. It does not
// recover, initialize or alter source data. A nonzero exact committed digest
// and matching admission are required; a Hello/base is not a committed ACK.
func WithRetentionEvidence(ctx context.Context, o Options, head execwire.Ref, root execwire.Hash, keep uint64, apply func(retention.Request, execwire.Hash) error) error {
	if apply == nil {
		return retention.ErrUnsafe
	}
	return withInspectedRetention(ctx, o, head, root, keep, func(r retention.Request, m manifest) error {
		if !admissionMatches(m.Admission, o.Admission, false) || m.Digest == (execwire.Hash{}) {
			return ErrConflict
		}
		if err := validateACK(execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: m.Chain, Ref: m.Committed, Digest: m.Digest}}); err != nil {
			return err
		}
		return apply(r, m.Digest)
	})
}
