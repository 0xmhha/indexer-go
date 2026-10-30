package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/jackc/pgx/v5"
)

type rawDatasetProof struct {
	codec              string
	size               uint64
	payload, reference execwire.Hash
}

func (s *Store) validateRawDeletion(ctx context.Context, p retention.Permit, e ingress.RawDeletion) error {
	o, err := readIngressOwner(ctx, s.q(ctx))
	if err != nil {
		return err
	}
	if o.Binding == nil || o.BindingDigest != e.OwnerDigest || o.Binding.RawStoreID != execwire.Hash(e.Record.StoreID) {
		return port.ErrExecutionConflict
	}
	var ready bool
	err = s.q(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM exec_publications p JOIN blocks b ON b.number=p.height AND b.hash=p.hash AND b.parent_hash=p.parent
 WHERE p.generation=$1 AND p.height=$2 AND p.hash=$3 AND p.digest=$4 AND p.root=$5
 AND (SELECT count(*) FROM exec_parts q WHERE q.generation=p.generation AND q.height=p.height AND q.hash=p.hash AND q.parent=p.parent AND q.digest=p.digest)=7)
 AND (SELECT count(*) FROM exec_cursors q JOIN exec_publications p
 ON q.generation=p.generation AND q.height=p.height AND q.hash=p.hash AND q.parent=p.parent AND q.digest=p.digest
 WHERE p.generation=$1 AND p.height=$2 AND p.hash=$3 AND p.digest=$4)=7
 AND NOT EXISTS(SELECT 1 FROM exec_parts WHERE height>$2)`, int64(p.Visible.Generation), int64(p.Visible.Height), p.Visible.Hash[:], e.ACKDigest[:], p.Root[:]).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return port.ErrExecutionCoverage
	}
	verified := map[model.FeedBlock]map[model.DatasetKind]rawDatasetProof{}
	for _, ref := range e.References {
		proofs, ok := verified[ref.Block]
		if !ok {
			part := port.ExecutionPart{Writer: port.ExecutionBlocks, Ref: execwire.Ref{Height: ref.Block.Number, Hash: execwire.Hash(ref.Block.Hash)}}
			var generation int64
			var parent, digest []byte
			err = s.q(ctx).QueryRow(ctx, `SELECT p.generation,p.parent,p.digest FROM exec_publications p
 JOIN blocks b ON b.number=p.height AND b.hash=p.hash AND b.parent_hash=p.parent
 WHERE p.height=$1 AND p.hash=$2 AND
 (SELECT count(*) FROM exec_parts q WHERE q.generation=p.generation AND q.height=p.height AND q.hash=p.hash AND q.parent=p.parent AND q.digest=p.digest)=7`, int64(ref.Block.Number), ref.Block.Hash[:]).Scan(&generation, &parent, &digest)
			if errors.Is(err, pgx.ErrNoRows) {
				return window.ErrRetain
			}
			if err != nil {
				return err
			}
			if generation < 1 || len(parent) != 32 || len(digest) != 32 {
				return port.ErrExecutionConflict
			}
			part.Ref.Generation = uint64(generation)
			copy(part.Parent[:], parent)
			copy(part.Digest[:], digest)
			a, err := readIngressArchive(ctx, s.q(ctx), part, o.BindingDigest)
			if err != nil {
				return err
			}
			if a.NativeEpoch > e.NativeEpoch || a.NativeReadEpoch > e.NativeReadEpoch || a.ObservedRawEpoch > e.RawEpoch {
				return port.ErrExecutionConflict
			}
			proofs = map[model.DatasetKind]rawDatasetProof{}
			for _, d := range a.Datasets {
				proofs[d.Kind] = rawDatasetProof{codec: d.Codec, size: d.PayloadBytes, payload: d.Digest, reference: execwire.Hash(sha256.Sum256(d.Reference))}
			}
			verified[ref.Block] = proofs
		}
		d, ok := proofs[ref.Kind]
		reference, err := json.Marshal(ref)
		if err != nil {
			return err
		}
		if !ok || d.codec != ref.Codec || d.size != uint64(ref.PayloadBytes) || d.payload != execwire.Hash(ref.Digest) || d.reference != execwire.Hash(sha256.Sum256(reference)) {
			return port.ErrExecutionConflict
		}
	}
	return nil
}
