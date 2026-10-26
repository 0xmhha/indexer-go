package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/proofdata"
	"github.com/jackc/pgx/v5"
)

var _ port.ExecutionProofWriter = (*Store)(nil)

func (s *Store) ApplyExecutionProofs(ctx context.Context, chain execwire.Chain, r execwire.Result) error {
	scope, _ := ctx.Value(executionPartKey{}).(*executionPartScope)
	if scope == nil || !scope.active.Load() || s.boundTx(ctx) == nil {
		return port.ErrClosed
	}
	if scope.part.Writer != port.ExecutionAccounts || scope.part.Ref != r.Ref || scope.part.Parent != r.Parent || scope.chain != chain {
		return port.ErrExecutionConflict
	}
	if r.StateProofBundle == nil {
		return nil
	}
	raw, e := execwire.MarshalResult(r)
	if e != nil {
		return e
	}
	if execwire.Hash(sha256.Sum256(raw)) != scope.part.Digest {
		return port.ErrExecutionConflict
	}
	record, _, _, e := proofdata.Decode(ctx, chain, r)
	if e != nil {
		return e
	}
	record.Digest = scope.part.Digest
	meta, e := json.Marshal(record)
	if e != nil {
		return e
	}
	digest := sha256.Sum256(meta)
	var height int64
	e = s.q(ctx).QueryRow(ctx, `INSERT INTO exec_state_proofs(height,generation,hash,payload_digest,metadata,metadata_digest,response) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(height) DO NOTHING RETURNING height`, int64(r.Ref.Height), int64(r.Ref.Generation), r.Ref.Hash[:], record.Digest[:], meta, digest[:], record.Response).Scan(&height)
	if errors.Is(e, pgx.ErrNoRows) {
		existing, _, _, err := readProofReceipt(ctx, s.q(ctx), port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Root: r.StateRoot, Digest: record.Digest, StateProofs: true}, chain)
		if err != nil {
			return err
		}
		old, _ := json.Marshal(existing)
		if !bytes.Equal(old, meta) || !bytes.Equal(existing.Response, record.Response) {
			return port.ErrExecutionConflict
		}
		return nil
	}
	return e
}

// Every checksum/proof operation precedes the final NEW uncached fresh check.
func readProofReceipt(ctx context.Context, q querier, p port.ExecutionPublication, chain execwire.Chain) (*port.ExecutionProofRecord, stateproof.Bundle, []stateproof.Account, error) {
	var empty stateproof.Bundle
	var gen int64
	var hash, payload, meta, digest, response []byte
	e := q.QueryRow(ctx, `SELECT generation,hash,payload_digest,metadata,metadata_digest,response FROM exec_state_proofs WHERE height=$1`, int64(p.Ref.Height)).Scan(&gen, &hash, &payload, &meta, &digest, &response)
	if errors.Is(e, pgx.ErrNoRows) {
		if p.StateProofs {
			return nil, empty, nil, execwire.ErrIncomplete
		}
		return nil, empty, nil, nil
	}
	if e != nil {
		return nil, empty, nil, e
	}
	if !p.StateProofs || gen < 1 || uint64(gen) != p.Ref.Generation || !bytes.Equal(hash, p.Ref.Hash[:]) || !bytes.Equal(payload, p.Digest[:]) || len(meta) == 0 || len(meta) > 65536 || len(response) > execwire.MaxStateProofBytes {
		return nil, empty, nil, port.ErrExecutionConflict
	}
	h := sha256.Sum256(meta)
	if !bytes.Equal(h[:], digest) {
		return nil, empty, nil, port.ErrExecutionConflict
	}
	var record port.ExecutionProofRecord
	if e = json.Unmarshal(meta, &record); e != nil {
		return nil, empty, nil, e
	}
	canonical, e := json.Marshal(record)
	if e != nil || !bytes.Equal(canonical, meta) || record.Chain != chain || record.Ref != p.Ref || record.Parent != p.Parent || record.Root != p.Root || record.Digest != p.Digest {
		return nil, empty, nil, port.ErrExecutionConflict
	}
	record.Response = response
	bundle, accounts, e := proofdata.VerifyRecord(ctx, record)
	if e != nil {
		return nil, empty, nil, e
	}
	return &record, bundle, accounts, nil
}
