package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	checkpoint "github.com/0xmhha/indexer-go/pkg/execution/checkpoint"
	"github.com/jackc/pgx/v5"
)

type ExecutionBootstrapInfo struct {
	Coverage, Mode string
	Digest         []byte
}

func (s *Store) ExecutionBootstrapStatus(ctx context.Context) (v ExecutionBootstrapInfo, err error) {
	err = s.q(ctx).QueryRow(ctx, "SELECT coverage,start_mode,bootstrap_digest FROM exec_control WHERE id").Scan(&v.Coverage, &v.Mode, &v.Digest)
	return v, notFound(err)
}

// BootstrapExecution is an offline-only operation. The schema must be empty and
// no application may be running against it. It never resets existing data.
// All staged records roll back on proof/root/EOF/cancellation/fresh failure.
func (s *Store) BootstrapExecution(ctx context.Context, r io.Reader, m format.Manifest, p chains.HeaderCodec, fresh func(context.Context, execwire.Ref) error) (out checkpoint.Report, err error) {
	if err = s.rewindWriteAllowed(ctx); err != nil {
		return out, err
	}
	if fresh == nil || r == nil || p == nil {
		return out, port.ErrInvalidData
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+executionLockSQL+")"); e != nil {
			return e
		}
		if e := emptyExecutionBootstrap(ctx, tx); e != nil {
			return e
		}
		if e := executionDurable(ctx, tx); e != nil {
			return e
		}
		bt := &blockTx{owner: s, tx: tx}
		bound := context.WithValue(ctx, blockTxKey{}, bt)
		cfg := port.ExecutionConfig{Chain: m.Chain, Anchor: m.Ref, AnchorRoot: m.Root, PartitionBlocks: 1_000_000}
		control := port.ExecutionControl{ExecutionConfig: cfg, Generation: 1, Visible: m.Ref, Root: m.Root}
		blockTime := uint64(0)
		visit := func(v format.Record) error {
			switch v.Kind {
			case "manifest":
				b, e := p.DecodeHeaderRLP(v.Manifest.Header)
				if e != nil {
					return e
				}
				blockTime = b.Time
				// A checkpoint authenticates its header and state, not historical body or receipts.
				if e = s.SetBlock(bound, b); e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, `INSERT INTO exec_control(id,chain_id,genesis,anchor_generation,anchor_height,anchor_hash,anchor_root,generation,visible_height,visible_hash,visible_root,partition_blocks,coverage,start_mode) VALUES(true,$1,$2,1,$3,$4,$5,1,$3,$4,$5,$6,$7,$8)`, m.Chain.ID, m.Chain.Genesis[:], int64(m.Ref.Height), m.Ref.Hash[:], m.Root[:], int64(cfg.PartitionBlocks), m.Coverage, m.Mode); e != nil {
					return e
				}
				for _, w := range port.ExecutionWriters() {
					if _, e = tx.Exec(ctx, `INSERT INTO exec_cursors(writer,generation,height,hash,parent,digest) VALUES($1,1,$2,$3,$4,$5)`, string(w), int64(m.Ref.Height), m.Ref.Hash[:], b.ParentHash.Bytes(), make([]byte, 32)); e != nil {
						return e
					}
				}
				return s.SetLatestHeight(bound, m.Ref.Height)
			case "account":
				a := execwire.AccountChange{Address: v.Address, Before: *v.Account, After: *v.Account, Baseline: true}
				step := execwire.Step{Cause: execwire.Baseline, TxIndex: -1, Accounts: []execwire.AccountChange{a}}
				if m.Mode == "genesis" {
					step.Cause = execwire.Genesis
				}
				if len(v.Code) > 0 {
					step.Code = []execwire.Code{{Hash: v.Account.CodeHash, Bytes: v.Code}}
				}
				result := execwire.Result{Ref: m.Ref, StateRoot: m.Root, Time: blockTime, Steps: []execwire.Step{step}}
				if e := s.writeExecutionAccounts(bound, control, result, true); e != nil {
					return e
				}
				if e := s.writeExecutionStorage(bound, control, result, true); e != nil {
					return e
				}
				if e := s.writeExecutionContracts(bound, control, result, true, nil); e != nil {
					return e
				}
				return bt.flush(ctx)
			case "storage":
				if e := s.ensureExecutionPartition(bound, "state_storage_changes", m.Ref.Height, cfg.PartitionBlocks); e != nil {
					return e
				}
				cause := execwire.Baseline
				if m.Mode == "genesis" {
					cause = execwire.Genesis
				}
				if _, e := tx.Exec(ctx, `INSERT INTO state_storage_changes(height,ordinal,generation,block_hash,block_time,tx_index,tx_hash,cause,address,slot,value_before,value_after,baseline) VALUES($1,0,1,$2,$3,-1,NULL,$4,$5,$6,$7,$7,true)`, int64(m.Ref.Height), m.Ref.Hash[:], time.Unix(int64(blockTime), 0).UTC(), int16(cause), v.Address[:], v.Slot[:], v.Value[:]); e != nil {
					return e
				}
				_, e := tx.Exec(ctx, `INSERT INTO state_storage(address,slot,value,last_height,last_ordinal) VALUES($1,$2,$3,$4,0)`, v.Address[:], v.Slot[:], v.Value[:], int64(m.Ref.Height))
				return e
			}
			return port.ErrInvalidData
		}
		var e error
		out, e = checkpoint.Verify(ctx, r, m, p, visit)
		if e != nil {
			return e
		}
		// The baseline digest also fences old SeedExecution callers from replacing it.
		baseline := sha256.Sum256(append([]byte("stream-bootstrap-v1:"), out.Digest[:]...))
		if e = bt.flush(ctx); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE exec_control SET baseline_digest=$1,bootstrap_digest=$2 WHERE id", baseline[:], out.Digest[:]); e != nil {
			return e
		}
		if e = fresh(ctx, m.Ref); e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		c, e := scanExecutionControl(tx.QueryRow(ctx, executionControlSQL))
		if e != nil {
			return e
		}
		if c.Generation != 1 || c.Rewinding || c.Visible != m.Ref || c.Root != m.Root {
			return port.ErrExecutionStale
		}
		return nil
	})
	if err == nil {
		s.executionMode.Store(true)
	}
	return out, err
}
func emptyExecutionBootstrap(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relkind IN ('r','p') AND c.relname NOT IN ('schema_migrations','undo_by_height') ORDER BY c.relname`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, name := range tables {
		// Identifiers come from the schema catalog and are always quoted.
		table := pgx.Identifier{name}.Sanitize()
		if _, err = tx.Exec(ctx, "LOCK TABLE "+table+" IN ACCESS EXCLUSIVE MODE"); err != nil {
			return err
		}
		query := "SELECT EXISTS(SELECT 1 FROM " + table + ")"
		if name == "meta" {
			query = "SELECT EXISTS(SELECT 1 FROM meta WHERE name<>'transaction_count' OR value<>decode('0000000000000000','hex'))"
		}
		var used bool
		if err = tx.QueryRow(ctx, query).Scan(&used); err != nil {
			return err
		}
		if used {
			return fmt.Errorf("%w: bootstrap requires an empty schema (%s)", port.ErrExecutionConflict, name)
		}
	}
	return nil
}
func (s *Store) executionFullCoverage(ctx context.Context, height uint64) (bool, error) {
	var full bool
	err := s.q(ctx).QueryRow(ctx, `SELECT COALESCE((SELECT coverage='full' AND baseline_digest IS NOT NULL AND anchor_height<=$1 FROM exec_control WHERE id),false)`, int64(height)).Scan(&full)
	return full, err
}
