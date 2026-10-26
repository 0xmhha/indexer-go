package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var _ port.ExecutionConfirmer = (*Store)(nil)

func (s *Store) ConfirmExecution(ctx context.Context, payload []byte, fresh func(context.Context, execwire.Ref) error) (bool, error) {
	if s.executionReadBound(ctx) {
		return false, port.ErrReadOnly
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := s.write(); err != nil {
		return false, err
	}
	if fresh == nil || s.boundTx(ctx) != nil || s.pool.Config().MaxConns != 4 {
		return false, port.ErrInvalidData
	}
	r, err := execwire.UnmarshalResult(payload)
	if err != nil {
		return false, err
	}
	incoming := r.Ref
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	bt := &blockTx{owner: s, tx: tx}
	defer bt.Rollback()
	var locked bool
	if err = tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock_shared("+executionLockSQL+")").Scan(&locked); err != nil {
		return false, err
	}
	if !locked {
		return false, port.ErrExecutionRewinding
	}
	c, err := scanActiveExecutionControl(tx.QueryRow(ctx, executionControlSQL+" FOR UPDATE"))
	if err != nil {
		return false, err
	}
	if err = confirmConsensus(ctx, tx, c); err != nil {
		return false, err
	}
	if c.Rewinding {
		return false, port.ErrExecutionRewinding
	}
	if incoming.Generation != c.Generation {
		return false, port.ErrExecutionStale
	}
	if !c.BaselineReady {
		return false, port.ErrExecutionCoverage
	}
	if incoming.Height > c.Visible.Height {
		return false, nil
	}
	if err = executionDurable(ctx, tx); err != nil {
		return false, err
	}
	rows, err := tx.Query(ctx, "SELECT generation,hash,parent,digest,root,call_traces,blob_sidecars,state_proofs FROM exec_publications WHERE height=$1 LIMIT 2", int64(incoming.Height))
	if err != nil {
		return false, err
	}
	var generation int64
	var hash, parent, digest, root []byte
	var calls, blobs, proofs bool
	count := 0
	for rows.Next() {
		count++
		if err = rows.Scan(&generation, &hash, &parent, &digest, &root, &calls, &blobs, &proofs); err != nil {
			break
		}
	}
	rows.Close()
	if err != nil {
		return false, err
	}
	if err = rows.Err(); err != nil {
		return false, err
	}
	if count != 1 || generation < 1 || uint64(generation) > c.Generation || !bytes.Equal(hash, incoming.Hash[:]) || !bytes.Equal(parent, r.Parent[:]) || !bytes.Equal(root, r.StateRoot[:]) {
		return false, port.ErrExecutionConflict
	}
	// A surviving canonical prefix retains its original generation's proof.
	// Compare the complete normalized result, never only its height/hash.
	r.Ref.Generation = uint64(generation)
	normalized, err := execwire.MarshalResult(r)
	if err != nil {
		return false, err
	}
	expected := sha256.Sum256(normalized)
	if !bytes.Equal(digest, expected[:]) {
		return false, port.ErrExecutionConflict
	}
	if calls != (r.CallTraces != nil) {
		return false, port.ErrExecutionConflict
	}
	if err = validateCallReceipt(ctx, tx, port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Digest: execwire.Hash(expected), CallTraces: calls}, true); err != nil {
		return false, err
	}
	if blobs != (r.BlobBundle != nil) {
		return false, port.ErrExecutionConflict
	}
	blobRecord, e := readBlobReceipt(ctx, tx, port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Digest: execwire.Hash(expected), Blobs: blobs}, true)
	if e != nil {
		return false, e
	}
	if blobs && (blobRecord.Chain != c.Chain || blobRecord.Profile != r.BlobBundle.Profile || blobRecord.Provider != r.BlobBundle.Provider || blobRecord.InputDigest != execwire.Hash(sha256.Sum256(r.BlobBundle.Response))) {
		return false, port.ErrExecutionConflict
	}
	if proofs != (r.StateProofBundle != nil) {
		return false, port.ErrExecutionConflict
	}
	proofRecord, _, _, e := readProofReceipt(ctx, tx, port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Root: r.StateRoot, Digest: execwire.Hash(expected), StateProofs: proofs}, c.Chain)
	if e != nil {
		return false, e
	}
	if proofs && (proofRecord.Profile != r.StateProofBundle.Profile || proofRecord.Provider != r.StateProofBundle.Provider || proofRecord.InputDigest != execwire.Hash(sha256.Sum256(r.StateProofBundle.Response))) {
		return false, port.ErrExecutionConflict
	}
	bound := context.WithValue(ctx, blockTxKey{}, bt)
	if err = fresh(bound, incoming); err != nil {
		return false, err
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	after, err := scanActiveExecutionControl(tx.QueryRow(ctx, executionControlSQL))
	if err != nil {
		return false, err
	}
	if after.Rewinding || after.Generation != incoming.Generation {
		return false, port.ErrExecutionStale
	}
	if after.Visible != c.Visible || after.Root != c.Root {
		return false, port.ErrExecutionConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	bt.done = true
	return true, nil
}
