package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

type callRangeCursor struct {
	Fingerprint [32]byte
	Height      uint64
	Ordinal     uint32
}

func (s *Store) ExecutionCallRange(ctx context.Context, in port.ExecutionCallRangeQuery) (out port.ExecutionCallRangePage, err error) {
	if err = in.Validate(); err != nil {
		return out, err
	}
	var cursor *callRangeCursor
	if in.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(in.Cursor)
		if e != nil {
			return out, port.ErrInvalidData
		}
		cursor = &callRangeCursor{}
		if e = json.Unmarshal(raw, cursor); e != nil || cursor.Height < in.FromBlock || cursor.Height > in.ToBlock || cursor.Ordinal >= execwire.MaxCallFrames {
			return out, port.ErrInvalidData
		}
	}
	err = s.ReadSnapshot(ctx, func(ctx context.Context) error {
		c, e := s.ExecutionStatus(ctx)
		if e != nil {
			return e
		}
		if c.Rewinding {
			return port.ErrExecutionRewinding
		}
		out = port.ExecutionCallRangePage{Selection: in.Selection, Epoch: c.Generation, Complete: true, Missing: []port.ExecutionCallMissingRange{}, Coverage: []port.ExecutionCallCoverage{}, Calls: []port.ExecutionCallRangeItem{}}
		if in.Selection == "orphan" {
			out.Epoch = in.OrphanEpoch
		}
		for h := in.FromBlock; h <= in.ToBlock; h++ {
			coverage := port.ExecutionCallCoverage{Height: h, Epoch: out.Epoch}
			var generation int64
			var hash, digest, parent []byte
			var count *int32
			var profile *string
			if in.Selection == "canonical" && h <= c.Visible.Height {
				e = s.q(ctx).QueryRow(ctx, `SELECT p.generation,p.hash,p.parent,p.digest,p.call_traces,b.profile,b.frame_count FROM exec_publications p LEFT JOIN exec_call_blocks b ON b.height=p.height WHERE p.height=$1`, int64(h)).Scan(&generation, &hash, &parent, &digest, &coverage.Collected, &profile, &count)
			} else if in.Selection == "orphan" {
				e = s.q(ctx).QueryRow(ctx, `SELECT generation,hash,parent,payload_digest,collected,profile,frame_count FROM exec_call_orphan_blocks WHERE epoch=$1 AND height=$2`, int64(in.OrphanEpoch), int64(h)).Scan(&generation, &hash, &parent, &digest, &coverage.Collected, &profile, &count)
			} else {
				e = pgx.ErrNoRows
			}
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			if e == nil {
				coverage.Ref = execwire.Ref{Generation: uint64(generation), Height: h}
				copy(coverage.Ref.Hash[:], hash)
				copy(coverage.Digest[:], digest)
				if coverage.Collected {
					if profile == nil || count == nil {
						return port.ErrExecutionConflict
					}
					coverage.Profile = *profile
					if in.Selection == "canonical" {
						p := port.ExecutionPublication{Ref: coverage.Ref, Digest: coverage.Digest, CallTraces: true}
						copy(p.Parent[:], parent)
						if e = validateCallReceipt(ctx, s.q(ctx), p, false); e != nil {
							return e
						}
					} else {
						var n, first, last int32
						if e = s.q(ctx).QueryRow(ctx, "SELECT count(*),COALESCE(min(ordinal),0),COALESCE(max(ordinal),-1) FROM exec_call_orphan_frames WHERE epoch=$1 AND height=$2", int64(in.OrphanEpoch), int64(h)).Scan(&n, &first, &last); e != nil {
							return e
						}
						if n != *count || first != 0 || last != n-1 {
							return port.ErrExecutionConflict
						}
					}
				}
			}
			out.Coverage = append(out.Coverage, coverage)
			if !coverage.Collected {
				out.Complete = false
				n := len(out.Missing)
				if n > 0 && out.Missing[n-1].ToBlock+1 == h {
					out.Missing[n-1].ToBlock = h
				} else {
					out.Missing = append(out.Missing, port.ExecutionCallMissingRange{FromBlock: h, ToBlock: h})
				}
			}
		}
		// Bind cursors to every filter and the complete coverage snapshot. This
		// fences visibility/collection changes as well as epochs and forks.
		binding := in
		binding.Cursor = ""
		raw, e := json.Marshal(struct {
			Query    port.ExecutionCallRangeQuery
			Epoch    uint64
			Coverage []port.ExecutionCallCoverage
		}{binding, c.Generation, out.Coverage})
		if e != nil {
			return e
		}
		fingerprint := sha256.Sum256(raw)
		if cursor != nil && cursor.Fingerprint != fingerprint {
			return port.ErrExecutionStale
		}
		if !out.Complete && !in.Partial {
			return &port.ExecutionCallCoverageError{Missing: out.Missing}
		}
		address, _ := in.Address.MarshalText()
		for _, coverage := range out.Coverage {
			if !coverage.Collected || cursor != nil && coverage.Height < cursor.Height {
				continue
			}
			after := uint32(0)
			if cursor != nil && coverage.Height == cursor.Height {
				after = cursor.Ordinal
			}
			// The only varying identifier is selected from two internal constants.
			table := "exec_call_frames"
			epochFilter := ""
			args := []any{int64(coverage.Height), int32(after), string(address), int32(in.Limit - uint32(len(out.Calls)) + 1)}
			if in.Selection == "orphan" {
				table = "exec_call_orphan_frames"
				epochFilter = " AND epoch=$5"
				args = append(args, int64(in.OrphanEpoch))
			}
			rows, e := s.q(ctx).Query(ctx, `SELECT ordinal,tx_hash,internal,data,row_digest FROM `+pgx.Identifier{table}.Sanitize()+` WHERE height=$1 AND ordinal>=$2 AND (convert_from(data,'UTF8')::jsonb->>'From'=$3 OR convert_from(data,'UTF8')::jsonb->>'To'=$3)`+epochFilter+` ORDER BY ordinal LIMIT $4`, args...)
			if e != nil {
				return e
			}
			for rows.Next() {
				var ordinal int32
				var hash, data, digest []byte
				var internal bool
				if e = rows.Scan(&ordinal, &hash, &internal, &data, &digest); e != nil {
					rows.Close()
					return e
				}
				actual := sha256.Sum256(data)
				if !bytes.Equal(actual[:], digest) {
					rows.Close()
					return port.ErrExecutionConflict
				}
				var f port.ExecutionCall
				if e = json.Unmarshal(data, &f); e != nil {
					rows.Close()
					return e
				}
				canonical, e := json.Marshal(f)
				if e != nil || !bytes.Equal(canonical, data) || f.Ordinal != uint32(ordinal) || !bytes.Equal(f.TransactionHash[:], hash) || f.Internal != internal || f.From != in.Address && (f.To == nil || *f.To != in.Address) {
					rows.Close()
					return port.ErrExecutionConflict
				}
				if len(out.Calls) == int(in.Limit) {
					raw, e := json.Marshal(callRangeCursor{Fingerprint: fingerprint, Height: coverage.Height, Ordinal: uint32(ordinal)})
					rows.Close()
					if e != nil {
						return e
					}
					out.Next = base64.RawURLEncoding.EncodeToString(raw)
					return nil
				}
				out.Calls = append(out.Calls, port.ExecutionCallRangeItem{Coverage: coverage, Call: f})
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
		}
		return nil
	})
	return out, err
}
