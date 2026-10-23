package postgres

import (
	"context"
	"errors"
	"io"
	"math"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

const executionControlSQL = `SELECT chain_id,genesis,anchor_generation,anchor_height,anchor_hash,anchor_root,
 generation,visible_height,visible_hash,visible_root,partition_blocks,(rewinding OR EXISTS(SELECT 1 FROM exec_rewinds)),baseline_digest IS NOT NULL FROM exec_control WHERE id`
const executionLockSQL = `hashtextextended(current_schema() || ':execution',0)`

var _ port.ExecutionStore = (*Store)(nil)

func validExecutionConfig(c port.ExecutionConfig) error {
	if c.AnchorRoot == (execwire.Hash{}) || c.PartitionBlocks == 0 || c.PartitionBlocks > 1_000_000_000 {
		return port.ErrInvalidData
	}
	return execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: c.Chain, Ref: c.Anchor}})
}
func validExecutionWriter(w port.ExecutionWriter) bool {
	for _, name := range port.ExecutionWriters() {
		if w == name {
			return true
		}
	}
	return false
}
func executionDurable(ctx context.Context, tx pgx.Tx) error {
	var fsync string
	if err := tx.QueryRow(ctx, "SHOW fsync").Scan(&fsync); err != nil {
		return err
	}
	if fsync != "on" {
		return errors.New("execution: PostgreSQL fsync must be on")
	}
	_, err := tx.Exec(ctx, "SET LOCAL synchronous_commit = on; SET LOCAL indexer.undo = 'off'")
	return err
}
func scanExecutionControl(row pgx.Row) (port.ExecutionControl, error) {
	var c port.ExecutionControl
	var genesis, anchor, root, visible, visibleRoot []byte
	var ag, ah, generation, height, partition int64
	err := row.Scan(&c.Chain.ID, &genesis, &ag, &ah, &anchor, &root, &generation, &height, &visible, &visibleRoot, &partition, &c.Rewinding, &c.BaselineReady)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, port.ErrExecutionUninitialized
	}
	if err != nil {
		return c, err
	}
	copy(c.Chain.Genesis[:], genesis)
	c.Anchor.Generation, c.Anchor.Height = uint64(ag), uint64(ah)
	copy(c.Anchor.Hash[:], anchor)
	copy(c.AnchorRoot[:], root)
	c.Generation, c.Visible.Generation, c.Visible.Height = uint64(generation), uint64(generation), uint64(height)
	copy(c.Visible.Hash[:], visible)
	copy(c.Root[:], visibleRoot)
	c.PartitionBlocks = uint64(partition)
	return c, nil
}
func (s *Store) InitExecution(ctx context.Context, cfg port.ExecutionConfig) error {
	if s.executionReadBound(ctx) {
		return port.ErrReadOnly
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.write(); err != nil {
		return err
	}
	if s.boundTx(ctx) != nil || s.pool.Config().MaxConns != 4 {
		return port.ErrInvalidData
	}
	if cfg.PartitionBlocks == 0 {
		cfg.PartitionBlocks = 1_000_000
	}
	if err := validExecutionConfig(cfg); err != nil {
		return err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+executionLockSQL+")"); err != nil {
			return err
		}
		old, err := scanExecutionControl(tx.QueryRow(ctx, executionControlSQL))
		if err == nil {
			if old.ExecutionConfig != cfg {
				return port.ErrExecutionConflict
			}
			return nil
		}
		if !errors.Is(err, port.ErrExecutionUninitialized) {
			return err
		}
		if err := executionDurable(ctx, tx); err != nil {
			return err
		}
		bt := &blockTx{owner: s, tx: tx}
		bound := context.WithValue(ctx, blockTxKey{}, bt)
		block, err := s.GetBlock(bound, cfg.Anchor.Height)
		if errors.Is(err, port.ErrNotFound) {
			return port.ErrExecutionCoverage
		}
		if err != nil {
			return err
		}
		if execwire.Hash(block.Hash) != cfg.Anchor.Hash || execwire.Hash(block.StateRoot) != cfg.AnchorRoot {
			return port.ErrExecutionConflict
		}
		if h, err := s.GetLatestHeight(bound); err == nil && h != cfg.Anchor.Height {
			return port.ErrExecutionConflict
		} else if err != nil && !errors.Is(err, port.ErrNotFound) {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO exec_control
   (id,chain_id,genesis,anchor_generation,anchor_height,anchor_hash,anchor_root,generation,visible_height,visible_hash,visible_root,partition_blocks)
   VALUES(true,$1,$2,$3,$4,$5,$6,$3,$4,$5,$6,$7)`, cfg.Chain.ID, cfg.Chain.Genesis[:], int64(cfg.Anchor.Generation), int64(cfg.Anchor.Height), cfg.Anchor.Hash[:], cfg.AnchorRoot[:], int64(cfg.PartitionBlocks))
		if err != nil {
			return err
		}
		for _, writer := range port.ExecutionWriters() {
			if _, err := tx.Exec(ctx, `INSERT INTO exec_cursors(writer,generation,height,hash,parent,digest) VALUES($1,$2,$3,$4,$5,$6)`, string(writer), int64(cfg.Anchor.Generation), int64(cfg.Anchor.Height), cfg.Anchor.Hash[:], block.ParentHash.Bytes(), make([]byte, 32)); err != nil {
				return err
			}
		}
		if err := s.SetLatestHeight(bound, cfg.Anchor.Height); err != nil {
			return err
		}
		return bt.flush(ctx)
	})
	if err == nil {
		s.executionMode.Store(true)
	}
	return err
}
func (s *Store) ExecutionStatus(ctx context.Context) (port.ExecutionControl, error) {
	return scanExecutionControl(s.q(ctx).QueryRow(ctx, executionControlSQL))
}
func scanExecutionPart(row pgx.Row) (port.ExecutionPart, error) {
	var p port.ExecutionPart
	var writer string
	var generation, height int64
	var hash, parent, digest []byte
	if err := row.Scan(&writer, &generation, &height, &hash, &parent, &digest); err != nil {
		return p, err
	}
	p.Writer, p.Ref.Generation, p.Ref.Height = port.ExecutionWriter(writer), uint64(generation), uint64(height)
	copy(p.Ref.Hash[:], hash)
	copy(p.Parent[:], parent)
	copy(p.Digest[:], digest)
	return p, nil
}

const executionPartColumns = "writer,generation,height,hash,parent,digest"

func (s *Store) ExecutionCursor(ctx context.Context, writer port.ExecutionWriter) (port.ExecutionPart, error) {
	if !validExecutionWriter(writer) {
		return port.ExecutionPart{}, port.ErrInvalidData
	}
	p, err := scanExecutionPart(s.q(ctx).QueryRow(ctx, "SELECT "+executionPartColumns+" FROM exec_cursors WHERE writer=$1", string(writer)))
	return p, notFound(err)
}
func (s *Store) ExecutionParts(ctx context.Context, height uint64) ([]port.ExecutionPart, error) {
	c, err := s.ExecutionStatus(ctx)
	if err != nil {
		return nil, err
	}
	if height > math.MaxInt64 {
		return nil, port.ErrInvalidData
	}
	rows, err := s.q(ctx).Query(ctx, "SELECT "+executionPartColumns+" FROM exec_parts WHERE generation=$1 AND height=$2 ORDER BY writer", int64(c.Generation), int64(height))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var parts []port.ExecutionPart
	for rows.Next() {
		p, err := scanExecutionPart(rows)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	return parts, rows.Err()
}
