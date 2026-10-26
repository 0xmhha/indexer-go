package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/blobdata"
	"github.com/jackc/pgx/v5"
	"io"
)

var _ port.ExecutionBlobWriter = (*Store)(nil)
var _ port.ExecutionBlobReader = (*Store)(nil)

func (s *Store) ApplyExecutionBlobs(ctx context.Context, chain execwire.Chain, r execwire.Result, b *model.Block) error {
	scope, _ := ctx.Value(executionPartKey{}).(*executionPartScope)
	if scope == nil || !scope.active.Load() || s.boundTx(ctx) == nil {
		return port.ErrClosed
	}
	if scope.part.Writer != port.ExecutionTransactions || scope.part.Ref != r.Ref || scope.part.Parent != r.Parent || scope.chain != chain {
		return port.ErrExecutionConflict
	}
	if r.BlobBundle == nil {
		return nil
	}
	raw, e := execwire.MarshalResult(r)
	if e != nil {
		return e
	}
	if execwire.Hash(sha256.Sum256(raw)) != scope.part.Digest {
		return port.ErrExecutionConflict
	}
	record, e := blobdata.Decode(ctx, r.BlobBundle.Profile, chain, r, b)
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
	e = s.q(ctx).QueryRow(ctx, `INSERT INTO exec_blob_blocks(height,generation,hash,parent,payload_digest,metadata,metadata_digest) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(height) DO NOTHING RETURNING height`, int64(r.Ref.Height), int64(r.Ref.Generation), r.Ref.Hash[:], r.Parent[:], record.Digest[:], meta, digest[:]).Scan(&height)
	if errors.Is(e, pgx.ErrNoRows) {
		existing, e := readBlobReceipt(ctx, s.q(ctx), port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Digest: record.Digest, Blobs: true}, true)
		if e != nil {
			return e
		}
		if !sameBlobMetadata(*existing, *record) {
			return port.ErrExecutionConflict
		}
		return nil
	}
	if e != nil {
		return e
	}
	for _, entry := range record.Entries {
		if e = s.execLater(ctx, `INSERT INTO exec_blob_entries(height,ordinal,blob,commitment,proof) VALUES($1,$2,$3,$4,$5)`, int64(r.Ref.Height), int32(entry.Index), entry.Blob, entry.Commitment, entry.Proof); e != nil {
			return e
		}
	}
	return nil
}
func sameBlobMetadata(a, b port.ExecutionBlobRecord) bool {
	x, e1 := json.Marshal(a)
	y, e2 := json.Marshal(b)
	return e1 == nil && e2 == nil && bytes.Equal(x, y)
}

// All receipt/proof work precedes the existing final fresh read.
func readBlobReceipt(ctx context.Context, q querier, p port.ExecutionPublication, full bool) (*port.ExecutionBlobRecord, error) {
	var gen int64
	var hash, parent, payload, meta, digest []byte
	e := q.QueryRow(ctx, `SELECT generation,hash,parent,payload_digest,metadata,metadata_digest FROM exec_blob_blocks WHERE height=$1`, int64(p.Ref.Height)).Scan(&gen, &hash, &parent, &payload, &meta, &digest)
	if errors.Is(e, pgx.ErrNoRows) {
		if p.Blobs {
			return nil, execwire.ErrIncomplete
		}
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if !p.Blobs || gen < 1 || uint64(gen) != p.Ref.Generation || !bytes.Equal(hash, p.Ref.Hash[:]) || !bytes.Equal(parent, p.Parent[:]) || !bytes.Equal(payload, p.Digest[:]) || len(meta) > 65536 {
		return nil, port.ErrExecutionConflict
	}
	h := sha256.Sum256(meta)
	if !bytes.Equal(h[:], digest) {
		return nil, port.ErrExecutionConflict
	}
	var r port.ExecutionBlobRecord
	if e = json.Unmarshal(meta, &r); e != nil {
		return nil, e
	}
	canonical, e := json.Marshal(r)
	if e != nil || !bytes.Equal(canonical, meta) || r.Ref != p.Ref || r.Parent != p.Parent || r.Digest != p.Digest || r.InputDigest == (execwire.Hash{}) || r.References == nil || len(r.References) > 32 {
		return nil, port.ErrExecutionConflict
	}
	if e = execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: r.Chain, Ref: r.Ref}}); e != nil {
		return nil, e
	}
	if e = execwire.ValidateBlobBundle(&execwire.BlobBundle{Version: 1, Format: execwire.FullBlobFormat, Profile: r.Profile, Provider: r.Provider, Response: []byte{0xc0}}); e != nil {
		return nil, e
	}
	for i, ref := range r.References {
		if ref.Index != uint32(i) || ref.VersionedHash[0] != 1 {
			return nil, port.ErrExecutionConflict
		}
	}
	var count, first, last int
	if e = q.QueryRow(ctx, `SELECT count(*),COALESCE(min(ordinal),0),COALESCE(max(ordinal),-1) FROM exec_blob_entries WHERE height=$1`, int64(p.Ref.Height)).Scan(&count, &first, &last); e != nil {
		return nil, e
	}
	if count != len(r.References) || first != 0 || last != count-1 {
		return nil, execwire.ErrIncomplete
	}
	if full {
		rows, e := q.Query(ctx, `SELECT ordinal,blob,commitment,proof FROM exec_blob_entries WHERE height=$1 ORDER BY ordinal`, int64(p.Ref.Height))
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		r.Entries = make([]port.ExecutionBlob, 0, count)
		for rows.Next() {
			var entry port.ExecutionBlob
			if e = rows.Scan(&entry.Index, &entry.Blob, &entry.Commitment, &entry.Proof); e != nil {
				return nil, e
			}
			r.Entries = append(r.Entries, entry)
		}
		if e = rows.Err(); e != nil {
			return nil, e
		}
		if e = blobdata.VerifyAll(ctx, r); e != nil {
			return nil, e
		}
	}
	return &r, nil
}
