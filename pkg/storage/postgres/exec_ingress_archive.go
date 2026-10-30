package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ApplyExecutionIngress(ctx context.Context, a port.ExecutionIngressArchive) error {
	scope, _ := ctx.Value(executionPartKey{}).(*executionPartScope)
	if scope == nil || !scope.active.Load() || s.boundTx(ctx) == nil {
		return port.ErrClosed
	}
	if scope.part.Writer != port.ExecutionBlocks || a.Ref != scope.part.Ref || a.Parent != scope.part.Parent || a.Digest != scope.part.Digest {
		return port.ErrExecutionConflict
	}
	digest, err := a.MetadataDigest()
	if err != nil {
		return err
	}
	owner, err := readIngressOwner(ctx, s.q(ctx))
	if err != nil {
		return err
	}
	if owner.Binding == nil || owner.BindingDigest != a.OwnerDigest {
		return port.ErrExecutionConflict
	}
	metadata, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = s.q(ctx).Exec(ctx, `INSERT INTO exec_ingress_payloads(owner_digest,generation,height,hash,parent,result_digest,metadata,metadata_digest,header,body,receipts)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`, a.OwnerDigest[:], int64(a.Ref.Generation), int64(a.Ref.Height), a.Ref.Hash[:], a.Parent[:], a.Digest[:], metadata, digest[:], a.Datasets[0].Payload, a.Datasets[1].Payload, a.Datasets[2].Payload)
	if err != nil {
		return err
	}
	stored, err := readIngressArchive(ctx, s.q(ctx), scope.part, owner.BindingDigest)
	if err != nil {
		return err
	}
	storedDigest, err := stored.MetadataDigest()
	if err != nil || storedDigest != digest {
		return port.ErrExecutionConflict
	}
	return nil
}

func readIngressArchive(ctx context.Context, q querier, p port.ExecutionPart, owner execwire.Hash) (a port.ExecutionIngressArchive, err error) {
	var metadata, digest, header, body, receipts []byte
	err = q.QueryRow(ctx, `SELECT metadata,metadata_digest,header,body,receipts FROM exec_ingress_payloads
 WHERE owner_digest=$1 AND generation=$2 AND height=$3 AND hash=$4 AND parent=$5 AND result_digest=$6`, owner[:], int64(p.Ref.Generation), int64(p.Ref.Height), p.Ref.Hash[:], p.Parent[:], p.Digest[:]).Scan(&metadata, &digest, &header, &body, &receipts)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, port.ErrExecutionCoverage
	}
	if err != nil {
		return a, err
	}
	if ingressCanonical(metadata, &a) != nil || len(a.Datasets) != 3 {
		return a, port.ErrExecutionConflict
	}
	for i, payload := range [][]byte{header, body, receipts} {
		a.Datasets[i].Payload = payload
	}
	d, e := a.MetadataDigest()
	if e != nil || !bytes.Equal(digest, d[:]) || a.OwnerDigest != owner || a.Ref != p.Ref || a.Parent != p.Parent || a.Digest != p.Digest {
		return a, port.ErrExecutionConflict
	}
	return a, nil
}

func requireIngressPart(ctx context.Context, q querier, p port.ExecutionPart) error {
	o, err := readIngressOwner(ctx, q)
	if errors.Is(err, port.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if o.Binding == nil {
		return port.ErrExecutionCoverage
	}
	if p.Writer == port.ExecutionBlocks {
		_, err = readIngressArchive(ctx, q, p, o.BindingDigest)
	}
	return err
}

// Private coverage observation for the existing retention coordinator. It
// grants no deletion permit. Public API admission retains its own read fence.
func (s *Store) ExecutionIngressArchive(ctx context.Context, p port.ExecutionPart) (port.ExecutionIngressArchive, error) {
	var empty port.ExecutionIngressArchive
	if p.Writer != port.ExecutionBlocks || p.Ref.Generation == 0 || p.Ref.Generation > math.MaxInt64 || p.Ref.Height == 0 || p.Ref.Height > math.MaxInt64 || p.Ref.Hash == (execwire.Hash{}) || p.Parent == (execwire.Hash{}) || p.Digest == (execwire.Hash{}) {
		return empty, port.ErrInvalidData
	}
	c, err := s.ExecutionStatus(ctx)
	if err != nil {
		return empty, err
	}
	if c.Rewinding || c.ReorgPending {
		return empty, port.ErrExecutionRewinding
	}
	if p.Ref.Height <= c.Anchor.Height || p.Ref.Height > c.Visible.Height {
		return empty, port.ErrExecutionCoverage
	}
	o, err := readIngressOwner(ctx, s.q(ctx))
	if err != nil {
		return empty, err
	}
	if o.Binding == nil {
		return empty, port.ErrExecutionCoverage
	}
	var covered bool
	err = s.q(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM exec_publications p
 JOIN blocks b ON b.number=p.height AND b.hash=p.hash AND b.parent_hash=p.parent
 WHERE p.generation=$1 AND p.height=$2 AND p.hash=$3 AND p.parent=$4 AND p.digest=$5
 AND (SELECT count(*) FROM exec_parts q WHERE q.generation=p.generation AND q.height=p.height
 AND q.hash=p.hash AND q.parent=p.parent AND q.digest=p.digest)=7)`, int64(p.Ref.Generation), int64(p.Ref.Height), p.Ref.Hash[:], p.Parent[:], p.Digest[:]).Scan(&covered)
	if err != nil {
		return empty, err
	}
	if !covered {
		return empty, port.ErrExecutionCoverage
	}
	return readIngressArchive(ctx, s.q(ctx), p, o.BindingDigest)
}
