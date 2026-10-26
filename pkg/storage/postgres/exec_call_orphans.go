package postgres

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

func archiveExecutionCalls(ctx context.Context, tx pgx.Tx, marker port.ExecutionRewind) error {
	// Authenticate the entire published set before preserving it. Part-only
	// rows are discarded by rewind and never presented as historical coverage.
	for height := marker.Target.Height + 1; height <= marker.From.Height; height++ {
		var generation int64
		var hash, parent, digest []byte
		var collected bool
		if err := tx.QueryRow(ctx, "SELECT generation,hash,parent,digest,call_traces FROM exec_publications WHERE height=$1", int64(height)).Scan(&generation, &hash, &parent, &digest, &collected); err != nil {
			return err
		}
		p := port.ExecutionPublication{Ref: execwire.Ref{Generation: uint64(generation), Height: height}, CallTraces: collected}
		copy(p.Ref.Hash[:], hash)
		copy(p.Parent[:], parent)
		copy(p.Digest[:], digest)
		if err := validateCallReceipt(ctx, tx, p, true); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO exec_call_orphan_blocks(epoch,height,generation,hash,parent,payload_digest,collected,trace_digest,profile,frame_count)
 SELECT $1,p.height,p.generation,p.hash,p.parent,p.digest,p.call_traces,b.trace_digest,b.profile,b.frame_count
 FROM exec_publications p LEFT JOIN exec_call_blocks b ON b.height=p.height
 WHERE p.height>$2 AND p.height<=$3`, int64(marker.From.Generation), int64(marker.Target.Height), int64(marker.From.Height)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO exec_call_orphan_frames(epoch,height,ordinal,tx_hash,internal,data,row_digest)
 SELECT $1,f.height,f.ordinal,f.tx_hash,f.internal,f.data,f.row_digest FROM exec_call_frames f
 JOIN exec_publications p ON p.height=f.height AND p.call_traces
 WHERE f.height>$2 AND f.height<=$3`, int64(marker.From.Generation), int64(marker.Target.Height), int64(marker.From.Height))
	return err
}
