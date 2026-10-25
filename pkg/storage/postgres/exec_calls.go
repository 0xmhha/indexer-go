package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"

	"github.com/0xmhha/indexer-go/execwire"
	nativecalls "github.com/0xmhha/indexer-go/pkg/chainfeed/evmtrace/native"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

var _ port.ExecutionCallWriter = (*Store)(nil)
var _ port.ExecutionCallReader = (*Store)(nil)

func (s *Store) ApplyExecutionCalls(ctx context.Context, r execwire.Result, b *model.Block, rs []*model.Receipt) error {
	scope, _ := ctx.Value(executionPartKey{}).(*executionPartScope)
	if scope == nil || !scope.active.Load() || s.boundTx(ctx) == nil {
		return port.ErrClosed
	}
	if scope.part.Writer != port.ExecutionTransactions || scope.part.Ref != r.Ref || scope.part.Parent != r.Parent {
		return port.ErrExecutionConflict
	}
	if r.CallTraces == nil {
		return nil
	}
	raw, e := execwire.MarshalResult(r)
	if e != nil {
		return e
	}
	if execwire.Hash(sha256.Sum256(raw)) != scope.part.Digest {
		return port.ErrExecutionConflict
	}
	frames, e := nativecalls.Verify(r.CallTraces.Profile, r, b, rs)
	if e != nil {
		return e
	}
	trace, e := json.Marshal(r.CallTraces)
	if e != nil {
		return e
	}
	traceDigest := sha256.Sum256(trace)
	var height int64
	e = s.q(ctx).QueryRow(ctx, `INSERT INTO exec_call_blocks(height,generation,hash,parent,payload_digest,trace_digest,profile,frame_count) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(height) DO NOTHING RETURNING height`, int64(r.Ref.Height), int64(r.Ref.Generation), r.Ref.Hash[:], r.Parent[:], scope.part.Digest[:], traceDigest[:], r.CallTraces.Profile, len(frames)).Scan(&height)
	if errors.Is(e, pgx.ErrNoRows) {
		var digest []byte
		if e = s.q(ctx).QueryRow(ctx, "SELECT trace_digest FROM exec_call_blocks WHERE height=$1", int64(r.Ref.Height)).Scan(&digest); e != nil {
			return e
		}
		if !bytes.Equal(digest, traceDigest[:]) {
			return port.ErrExecutionConflict
		}
		return validateCallReceipt(ctx, s.q(ctx), port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Digest: scope.part.Digest, CallTraces: true}, true)
	}
	if e != nil {
		return e
	}
	for _, f := range frames {
		data, e := json.Marshal(f)
		if e != nil {
			return e
		}
		digest := sha256.Sum256(data)
		if e = s.execLater(ctx, `INSERT INTO exec_call_frames(height,ordinal,tx_hash,internal,data,row_digest) VALUES($1,$2,$3,$4,$5,$6)`, int64(r.Ref.Height), int32(f.Ordinal), f.TransactionHash[:], f.Internal, data, digest[:]); e != nil {
			return e
		}
	}
	return nil
}

// This gate runs before fresh, including on a duplicate publication. Parts and
// a published collected marker cannot silently coexist with missing trace rows.
func validateCallReceipt(ctx context.Context, q querier, p port.ExecutionPublication, verifyBytes bool) error {
	var generation int64
	var hash, parent, digest []byte
	var count int
	e := q.QueryRow(ctx, "SELECT generation,hash,parent,payload_digest,frame_count FROM exec_call_blocks WHERE height=$1", int64(p.Ref.Height)).Scan(&generation, &hash, &parent, &digest, &count)
	if errors.Is(e, pgx.ErrNoRows) {
		if p.CallTraces {
			return execwire.ErrIncomplete
		}
		return nil
	}
	if e != nil {
		return e
	}
	if !p.CallTraces || uint64(generation) != p.Ref.Generation || !bytes.Equal(hash, p.Ref.Hash[:]) || !bytes.Equal(parent, p.Parent[:]) || !bytes.Equal(digest, p.Digest[:]) {
		return port.ErrExecutionConflict
	}
	var rows, first, last int
	if e = q.QueryRow(ctx, "SELECT count(*),COALESCE(min(ordinal),0),COALESCE(max(ordinal),-1) FROM exec_call_frames WHERE height=$1", int64(p.Ref.Height)).Scan(&rows, &first, &last); e != nil {
		return e
	}
	if rows != count || first != 0 || last != count-1 {
		return execwire.ErrIncomplete
	}
	// Publication/ACK recovery checks every retained row before fresh. API
	// pages check metadata here and authenticate only their requested rows.
	if verifyBytes {
		r, e := q.Query(ctx, "SELECT data,row_digest FROM exec_call_frames WHERE height=$1 ORDER BY ordinal", int64(p.Ref.Height))
		if e != nil {
			return e
		}
		defer r.Close()
		size := 0
		for r.Next() {
			var data, digest []byte
			if e = r.Scan(&data, &digest); e != nil {
				return e
			}
			size += len(data)
			if len(data) > 256<<10 || size > execwire.MaxMetadataBytes+execwire.MaxCallFrames*(execwire.MaxCallDepth*6+256) {
				return execwire.ErrLimit
			}
			actual := sha256.Sum256(data)
			if !bytes.Equal(actual[:], digest) {
				return port.ErrExecutionConflict
			}
		}
		if e = r.Err(); e != nil {
			return e
		}
	}
	return nil
}

func (s *Store) ExecutionCalls(ctx context.Context, in port.ExecutionCallQuery) (out port.ExecutionCallPage, err error) {
	if in.Height > math.MaxInt64 || in.Limit == 0 || in.Limit > 256 || in.After > execwire.MaxCallFrames || (in.Epoch == 0) != (in.Hash == (execwire.Hash{})) || in.After > 0 && in.Epoch == 0 || in.Transaction != nil && *in.Transaction == (execwire.Hash{}) {
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
		if in.Epoch != 0 && in.Epoch != c.Generation {
			return port.ErrExecutionStale
		}
		q := s.q(ctx)
		height := in.Height
		if in.Transaction != nil {
			var h int64
			if e = q.QueryRow(ctx, "SELECT block_number FROM transactions WHERE hash=$1 AND block_number<=$2", in.Transaction[:], int64(c.Visible.Height)).Scan(&h); e != nil {
				return notFound(e)
			}
			height = uint64(h)
		}
		if height > c.Visible.Height {
			return port.ErrNotFound
		}
		var generation int64
		var hash, digest, parent []byte
		var collected bool
		if e = q.QueryRow(ctx, "SELECT generation,hash,parent,digest,call_traces FROM exec_publications WHERE height=$1", int64(height)).Scan(&generation, &hash, &parent, &digest, &collected); e != nil {
			return notFound(e)
		}
		if !collected {
			return port.ErrNotFound
		}
		if generation < 1 || uint64(generation) > c.Generation {
			return port.ErrExecutionConflict
		}
		out.Epoch = c.Generation
		out.Ref = execwire.Ref{Generation: uint64(generation), Height: height}
		copy(out.Ref.Hash[:], hash)
		copy(out.Digest[:], digest)
		if in.Epoch != 0 && (in.Epoch != c.Generation || in.Hash != out.Ref.Hash) {
			return port.ErrExecutionStale
		}
		p := port.ExecutionPublication{Ref: out.Ref, Digest: out.Digest, CallTraces: true}
		copy(p.Parent[:], parent)
		if e = validateCallReceipt(ctx, q, p, false); e != nil {
			return e
		}
		if e = q.QueryRow(ctx, "SELECT profile FROM exec_call_blocks WHERE height=$1", int64(height)).Scan(&out.Profile); e != nil {
			return e
		}
		var tx []byte
		if in.Transaction != nil {
			tx = in.Transaction[:]
		}
		const filter = `height=$1 AND ($2::bytea IS NULL OR tx_hash=$2) AND (NOT $3::boolean OR internal)`
		if e = q.QueryRow(ctx, "SELECT count(*) FROM exec_call_frames WHERE "+filter, int64(height), tx, in.InternalOnly).Scan(&out.Total); e != nil {
			return e
		}
		rows, e := q.Query(ctx, "SELECT ordinal,tx_hash,internal,data,row_digest FROM exec_call_frames WHERE "+filter+" AND ordinal >= $4 ORDER BY ordinal LIMIT $5", int64(height), tx, in.InternalOnly, int32(in.After), int32(in.Limit+1))
		if e != nil {
			return e
		}
		defer rows.Close()
		out.Frames = make([]port.ExecutionCall, 0, in.Limit)
		out.Verification = nativecalls.Verification
		for rows.Next() {
			var ordinal int32
			var hash, data, digest []byte
			var internal bool
			if e = rows.Scan(&ordinal, &hash, &internal, &data, &digest); e != nil {
				return e
			}
			if len(out.Frames) == int(in.Limit) {
				next := uint32(ordinal)
				out.Next = &next
				break
			}
			actual := sha256.Sum256(data)
			if !bytes.Equal(digest, actual[:]) {
				return port.ErrExecutionConflict
			}
			var f port.ExecutionCall
			if e = json.Unmarshal(data, &f); e != nil {
				return e
			}
			canonical, e := json.Marshal(f)
			if e != nil || !bytes.Equal(canonical, data) || f.Ordinal != uint32(ordinal) || !bytes.Equal(f.TransactionHash[:], hash) || f.Internal != internal {
				return port.ErrExecutionConflict
			}
			out.Frames = append(out.Frames, f)
		}
		return rows.Err()
	})
	return out, err
}
