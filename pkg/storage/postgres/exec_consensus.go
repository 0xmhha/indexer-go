package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

type consensusRecord struct {
	policy chains.ConsensusPolicy
	point  chains.ConsensusPoint
}

func readConsensus(ctx context.Context, q querier) (consensusRecord, error) {
	var record consensusRecord
	var policy, state, hash, root, digest, policyDigest []byte
	var height int64
	err := q.QueryRow(ctx, "SELECT policy,height,hash,root,state,state_digest,(SELECT consensus_policy_digest FROM exec_control WHERE id) FROM exec_consensus WHERE id").Scan(&policy, &height, &hash, &root, &state, &digest, &policyDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		var pinned []byte
		if e := q.QueryRow(ctx, "SELECT consensus_policy_digest FROM exec_control WHERE id").Scan(&pinned); e != nil {
			return record, e
		}
		if len(pinned) > 0 {
			return record, port.ErrExecutionConflict
		}
	}
	if err != nil {
		return record, err
	}
	policyChecksum := sha256.Sum256(policy)
	if !bytes.Equal(policyDigest, policyChecksum[:]) {
		return record, port.ErrExecutionConflict
	}
	checksum := sha256.Sum256(state)
	if !bytes.Equal(digest, checksum[:]) {
		return record, port.ErrExecutionConflict
	}
	if err = json.Unmarshal(policy, &record.policy); err != nil {
		return record, err
	}
	if record.policy.Protocol == "" && record.policy != (chains.ConsensusPolicy{}) {
		return record, port.ErrExecutionConflict
	}
	record.point = chains.ConsensusPoint{Height: uint64(height), State: state}
	copy(record.point.Hash[:], hash)
	copy(record.point.Root[:], root)
	return record, nil
}

// ConfigureExecutionConsensus must finish before source recovery/admission.
// Once present, policy is checked even by lower-level publication calls which
// have no configuration or verifier instance in memory.
func (s *Store) ConfigureExecutionConsensus(ctx context.Context, policy chains.ConsensusPolicy) error {
	if err := s.rewindWriteAllowed(ctx); err != nil {
		return err
	}
	encoded, err := json.Marshal(policy)
	if err != nil || len(encoded) > 1<<20 {
		return port.ErrInvalidData
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+executionLockSQL+")"); err != nil {
			return err
		}
		c, err := scanActiveExecutionControl(tx.QueryRow(ctx, executionControlSQL+" FOR UPDATE"))
		if err != nil {
			return err
		}
		old, err := readConsensus(ctx, tx)
		if err == nil {
			existing, _ := json.Marshal(old.policy)
			if !bytes.Equal(existing, encoded) {
				return port.ErrExecutionConflict
			}
			if policy.Protocol != "" {
				if err = restoreConsensus(ctx, c.Chain, old); err != nil {
					return err
				}
				if old.point.Height != c.Visible.Height || old.point.Hash != c.Visible.Hash || old.point.Root != c.Root {
					return port.ErrExecutionConflict
				}
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		point := chains.ConsensusPoint{Height: c.Visible.Height, Hash: c.Visible.Hash, Root: c.Root, State: []byte{}}
		if policy.Protocol != "" {
			if c.Visible != c.Anchor || c.Rewinding {
				return port.ErrExecutionConflict
			}
			verifier, err := chains.NewConsensusVerifier(c.Chain, policy)
			if err != nil {
				return err
			}
			point, err = verifier.AnchorPoint()
			if err != nil {
				return err
			}
			if point.Height != c.Anchor.Height || point.Hash != c.Anchor.Hash || point.Root != c.AnchorRoot {
				return port.ErrExecutionConflict
			}
		} else if policy != (chains.ConsensusPolicy{}) {
			return port.ErrInvalidData
		}
		if err = executionDurable(ctx, tx); err != nil {
			return err
		}
		policyChecksum := sha256.Sum256(encoded)
		if _, err = tx.Exec(ctx, "UPDATE exec_control SET consensus_policy_digest=$1 WHERE id", policyChecksum[:]); err != nil {
			return err
		}
		checksum := sha256.Sum256(point.State)
		_, err = tx.Exec(ctx, "INSERT INTO exec_consensus(id,policy,height,hash,root,state,state_digest) VALUES(true,$1,$2,$3,$4,$5,$6)", encoded, int64(point.Height), point.Hash[:], point.Root[:], []byte(point.State), checksum[:])
		return err
	})
}
func verifyConsensusPublication(ctx context.Context, tx pgx.Tx, c port.ExecutionControl, p port.ExecutionPublication, duplicate bool) error {
	record, err := readConsensus(ctx, tx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	} // old, never opted-in databases
	if err != nil {
		return err
	}
	if record.policy.Protocol == "" {
		return nil
	}
	if err = restoreConsensus(ctx, c.Chain, record); err != nil {
		return err
	}
	if duplicate {
		if record.point.Height != c.Visible.Height || record.point.Hash != c.Visible.Hash || p.Ref.Height > record.point.Height {
			return port.ErrExecutionConflict
		}
		return nil // complete payload digest is compared with original durable receipt
	}
	if record.point.Height != c.Visible.Height || record.point.Hash != c.Visible.Hash || record.point.Root != c.Root {
		return port.ErrExecutionConflict
	}
	verifier, err := chains.NewConsensusVerifier(c.Chain, record.policy)
	if err != nil {
		return err
	}
	point, err := verifier.VerifyPoint(ctx, record.point.State, p.ConsensusHeader)
	if err != nil {
		return err
	}
	if point.Height != p.Ref.Height || point.Hash != p.Ref.Hash || point.Root != p.Root {
		return port.ErrExecutionConflict
	}
	checksum := sha256.Sum256(point.State)
	_, err = tx.Exec(ctx, "UPDATE exec_consensus SET height=$1,hash=$2,root=$3,state=$4,state_digest=$5 WHERE id", int64(point.Height), point.Hash[:], point.Root[:], []byte(point.State), checksum[:])
	return err
}
func consensusRewindAllowed(ctx context.Context, q querier, target execwire.Ref) error {
	record, err := readConsensus(ctx, q)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.policy.Protocol != "" && (target.Height < record.point.Height || target.Height == record.point.Height && target.Hash != record.point.Hash) {
		return port.ErrExecutionConflict
	}
	return nil
}

func restoreConsensus(ctx context.Context, chain execwire.Chain, record consensusRecord) error {
	verifier, err := chains.NewConsensusVerifier(chain, record.policy)
	if err != nil {
		return err
	}
	point, err := verifier.RestorePoint(ctx, record.point.State)
	if err != nil {
		return err
	}
	if point.Height != record.point.Height || point.Hash != record.point.Hash || point.Root != record.point.Root {
		return port.ErrExecutionConflict
	}
	return nil
}
func confirmConsensus(ctx context.Context, q querier, c port.ExecutionControl) error {
	record, err := readConsensus(ctx, q)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.policy.Protocol == "" {
		return nil
	}
	if record.point.Height != c.Visible.Height || record.point.Hash != c.Visible.Hash || record.point.Root != c.Root {
		return port.ErrExecutionConflict
	}
	return restoreConsensus(ctx, c.Chain, record)
}
