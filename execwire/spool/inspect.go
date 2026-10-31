package spool

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/metadata"
	"github.com/0xmhha/indexer-go/execwire/retention"
)

// MetadataObservation is diagnostic only: Committed is the durable source ACK,
// Base is the retained generation floor, not a scan of available payloads.
type MetadataObservation struct {
	Chain              execwire.Chain
	Anchor, Committed  execwire.Ref
	Base               execwire.Ref
	TransitionPending  bool
	MaintenancePending bool
	Maintenance        *retention.Permit
	Completion         *retention.Permit
	Scope              string
	Admission          *execwire.Hash
	Digest             execwire.Hash
}

func InspectMetadata(ctx context.Context, path string) (out MetadataObservation, err error) {
	r := metadata.New(ctx)
	if err = r.Directory(path); err != nil {
		return out, err
	}
	b, err := r.File(filepath.Join(path, stateName))
	if err != nil {
		return out, err
	}
	var m manifest
	if metadata.Sealed(b, &m) != nil {
		return out, ErrCorrupt
	}
	m, err = validateManifest(m)
	if err != nil {
		return out, err
	}
	out = MetadataObservation{Chain: m.Chain, Anchor: m.Anchor, Committed: m.Committed, Base: m.Anchor, Scope: "separate metadata samples; no payload scan, atomicity, lease, freshness or ACK/GC/start authorization"}
	out.Admission, out.Digest = m.Admission, m.Digest
	if m.Base != nil {
		out.Base = *m.Base
	}
	b, err = r.File(filepath.Join(path, transitionName))
	if err == nil {
		var j transition
		if metadata.Sealed(b, &j) != nil || validateTransition(m, m.Chain, j) != nil || validateAnchor(Options{Chain: j.Chain, Anchor: j.From}) != nil {
			return out, ErrCorrupt
		}
		out.TransitionPending = true
	} else if !os.IsNotExist(err) {
		return out, err
	}
	b, err = r.File(filepath.Join(path, gcIntentName))
	if err == nil {
		var g gcIntent
		if metadata.Sealed(b, &g) != nil || validateGCIntent(g) != nil || g.Permit.Request.Chain != m.Chain || g.Permit.Request.Anchor != m.Anchor || len(g.Files) > maxGCFiles {
			return out, ErrCorrupt
		}
		out.MaintenancePending = true
		out.Maintenance = &g.Permit
	} else if !os.IsNotExist(err) {
		return out, err
	}
	b, err = r.File(filepath.Join(path, gcReceiptName))
	if err == nil {
		var done gcCompletion
		if metadata.Sealed(b, &done) != nil || done.Receipt.Match(done.Permit) != nil || done.Permit.Request.Chain != m.Chain || done.Permit.Request.Anchor != m.Anchor {
			return out, ErrCorrupt
		}
		out.Completion = &done.Permit
	} else if !os.IsNotExist(err) {
		return out, err
	}

	err = r.Verify()
	return out, errors.Join(err, ctx.Err())
}
