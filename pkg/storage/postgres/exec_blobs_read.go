package postgres

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob/proof"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/blobdata"
	"math"
	"slices"
)

func (s *Store) ExecutionBlobs(ctx context.Context, in port.ExecutionBlobQuery) (out port.ExecutionBlobPage, err error) {
	if in.Height > math.MaxInt64 || in.Limit == 0 || in.Limit > 4 || in.After > 32 || (in.Epoch == 0) != (in.Hash == (execwire.Hash{})) || in.After > 0 && in.Epoch == 0 || in.Transaction != nil && *in.Transaction == (execwire.Hash{}) {
		return out, port.ErrInvalidData
	}
	err = s.ReadSnapshot(ctx, func(ctx context.Context) error {
		c, e := s.ExecutionStatus(ctx)
		if e != nil {
			return e
		}
		if c.Rewinding {
			return port.ErrExecutionRewinding
		}
		if c.Maintenance {
			return port.ErrExecutionMaintenance
		}
		if in.Epoch != 0 && in.Epoch != c.Generation {
			return port.ErrExecutionStale
		}
		q := s.q(ctx)
		height := in.Height
		if in.Transaction != nil {
			var h int64
			if e = q.QueryRow(ctx, `SELECT block_number FROM transactions WHERE hash=$1 AND block_number<=$2`, in.Transaction[:], int64(c.Visible.Height)).Scan(&h); e != nil {
				return notFound(e)
			}
			height = uint64(h)
		}
		if height > c.Visible.Height {
			return port.ErrNotFound
		}
		var generation int64
		var hash, parent, digest []byte
		var collected bool
		if e = q.QueryRow(ctx, `SELECT generation,hash,parent,digest,blob_sidecars FROM exec_publications WHERE height=$1`, int64(height)).Scan(&generation, &hash, &parent, &digest, &collected); e != nil {
			return notFound(e)
		}
		if !collected {
			return port.ErrNotFound
		}
		if generation < 1 || uint64(generation) > c.Generation {
			return port.ErrExecutionConflict
		}
		p := port.ExecutionPublication{Ref: execwire.Ref{Generation: uint64(generation), Height: height}, Blobs: true}
		copy(p.Ref.Hash[:], hash)
		copy(p.Parent[:], parent)
		copy(p.Digest[:], digest)
		if in.Epoch != 0 && in.Hash != p.Ref.Hash {
			return port.ErrExecutionStale
		}
		r, e := readBlobReceipt(ctx, q, p, false)
		if e != nil {
			return e
		}
		if r.Chain != c.Chain {
			return port.ErrExecutionConflict
		}
		b, e := s.GetBlock(ctx, height)
		if e != nil {
			return e
		}
		if execwire.Hash(b.Hash) != p.Ref.Hash {
			return port.ErrExecutionConflict
		}
		refs, e := proof.References(b)
		if e != nil {
			return e
		}
		if !slices.Equal(refs, blobdata.References(r.References)) {
			return port.ErrExecutionConflict
		}
		out = port.ExecutionBlobPage{Epoch: c.Generation, Ref: p.Ref, Digest: p.Digest, Profile: r.Profile, Provider: r.Provider, Availability: "retained", Verification: "kzg_requested_page_and_execution_references", References: []port.ExecutionBlobReference{}, Entries: []port.ExecutionBlob{}}
		if len(r.References) == 0 {
			out.Availability = "no_blob_references"
			out.Verification = "execution_transaction_references"
		}
		var selected []int32
		for _, ref := range r.References {
			if in.Transaction != nil && ref.TransactionHash != *in.Transaction {
				continue
			}
			out.Total++
			if ref.Index < in.After {
				continue
			}
			if len(selected) == int(in.Limit) {
				if out.Next == nil {
					n := ref.Index
					out.Next = &n
				}
				continue
			}
			selected = append(selected, int32(ref.Index))
			out.References = append(out.References, ref)
		}
		if len(selected) == 0 {
			return nil
		}
		rows, e := q.Query(ctx, `SELECT ordinal,blob,commitment,proof FROM exec_blob_entries WHERE height=$1 AND ordinal=ANY($2::integer[]) ORDER BY ordinal`, int64(height), selected)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var entry port.ExecutionBlob
			if e = rows.Scan(&entry.Index, &entry.Blob, &entry.Commitment, &entry.Proof); e != nil {
				return e
			}
			i := len(out.Entries)
			if i >= len(out.References) {
				return port.ErrExecutionConflict
			}
			if e = blobdata.VerifyEntry(ctx, entry, out.References[i]); e != nil {
				return e
			}
			out.Entries = append(out.Entries, entry)
		}
		if e = rows.Err(); e != nil {
			return e
		}
		if len(out.Entries) != len(selected) {
			return execwire.ErrIncomplete
		}
		return nil
	})
	return out, err
}
