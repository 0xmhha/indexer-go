package postgres

import (
	"context"
	"errors"
	"math"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/jackc/pgx/v5"
)

type executionContractValue struct {
	exists, active                bool
	hash                          execwire.Hash
	created, activated, destroyed *int64
	txHash, creator               []byte
}

func executionCodeActive(a execwire.Account) (bool, error) {
	if !a.Exists {
		return false, nil
	}
	if a.CodeHash == (execwire.Hash{}) {
		return false, port.ErrExecutionCoverage
	}
	return a.CodeHash != execwire.Hash(types.EmptyCodeHash), nil
}
func validExecutionCode(code execwire.Code) error {
	if len(code.Bytes) == 0 || len(code.Bytes) > 1<<20 || execwire.Hash(crypto.Keccak256Hash(code.Bytes)) != code.Hash {
		return port.ErrInvalidData
	}
	return nil
}
func (s *Store) ExecutionCodeAt(ctx context.Context, addr execwire.Address, height uint64) ([]byte, error) {
	if height > math.MaxInt64 {
		return nil, port.ErrInvalidData
	}
	var active bool
	var hash []byte
	err := s.q(ctx).QueryRow(ctx, "SELECT active_after,code_hash_after FROM state_contract_changes WHERE address=$1 AND height<=$2 ORDER BY height DESC,ordinal DESC LIMIT 1", addr[:], int64(height)).Scan(&active, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, port.ErrExecutionCoverage
	}
	if err != nil {
		return nil, err
	}
	if !active {
		return []byte{}, nil
	}
	var code []byte
	err = s.q(ctx).QueryRow(ctx, "SELECT code FROM state_code WHERE code_hash=$1", hash).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, port.ErrExecutionCoverage
	}
	if err != nil {
		return nil, err
	}
	if len(hash) != 32 || !bytesEqualHash(code, hash) {
		return nil, port.ErrExecutionConflict
	}
	return code, nil
}
func bytesEqualHash(code, hash []byte) bool {
	h := crypto.Keccak256Hash(code)
	return len(hash) == 32 && string(h[:]) == string(hash)
}

var executionContractChangeColumns = []string{"height", "ordinal", "generation", "block_hash", "block_time", "tx_index", "tx_hash", "cause", "address", "existed_before", "exists_after", "active_before", "active_after", "code_hash_before", "code_hash_after", "created_height", "created_tx_hash", "creator", "code_activated_height", "destroyed_height", "baseline"}

type executionCreationKey struct {
	address execwire.Address
	index   uint32
}

func executionCreationProofs(r execwire.Result, proofs []port.ExecutionCreation) (map[executionCreationKey]port.ExecutionCreation, error) {
	if len(proofs) > int(r.TxCount) {
		return nil, port.ErrInvalidData
	}
	steps := map[uint32]execwire.Step{}
	for _, step := range r.Steps {
		if step.Cause == execwire.Transaction {
			steps[uint32(step.TxIndex)] = step
		}
	}
	out := map[executionCreationKey]port.ExecutionCreation{}
	seen := map[uint32]bool{}
	for _, proof := range proofs {
		step, ok := steps[proof.TxIndex]
		if !ok || step.TxHash != proof.TxHash || seen[proof.TxIndex] {
			return nil, port.ErrExecutionConflict
		}
		seen[proof.TxIndex] = true
		out[executionCreationKey{proof.Address, proof.TxIndex}] = proof
	}
	return out, nil
}

func (s *Store) writeExecutionContracts(ctx context.Context, c port.ExecutionControl, r execwire.Result, baseline bool, creations []port.ExecutionCreation) error {
	bt := s.boundTx(ctx)
	if bt == nil {
		return port.ErrClosed
	}
	proofs, err := executionCreationProofs(r, creations)
	if err != nil {
		return err
	}
	keys := make([][]byte, 0)
	seen := map[execwire.Address]bool{}
	needed := map[execwire.Hash]bool{}
	for _, step := range r.Steps {
		for _, code := range step.Code {
			if err := validExecutionCode(code); err != nil {
				return err
			}
			tag, err := s.q(ctx).Exec(ctx, `INSERT INTO state_code(code_hash,code,first_height) VALUES($1,$2,$3) ON CONFLICT(code_hash) DO UPDATE SET code=state_code.code WHERE state_code.code=EXCLUDED.code`, code.Hash[:], code.Bytes, int64(r.Ref.Height))
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return port.ErrExecutionConflict
			}
		}
		for _, a := range step.Accounts {
			if !seen[a.Address] {
				seen[a.Address] = true
				keys = append(keys, a.Address[:])
			}
			for _, v := range []execwire.Account{a.Before, a.After} {
				active, err := executionCodeActive(v)
				if err != nil {
					return err
				}
				if active {
					needed[v.CodeHash] = true
				}
			}
		}
	}
	hashes := make([][]byte, 0, len(needed))
	for h := range needed {
		hashes = append(hashes, h[:])
	}
	rows, err := s.q(ctx).Query(ctx, "SELECT code_hash,code FROM state_code WHERE code_hash=ANY($1::bytea[])", hashes)
	if err != nil {
		return err
	}
	for rows.Next() {
		var h, code []byte
		if err := rows.Scan(&h, &code); err != nil {
			rows.Close()
			return err
		}
		if !bytesEqualHash(code, h) {
			rows.Close()
			return port.ErrExecutionConflict
		}
		var hash execwire.Hash
		copy(hash[:], h)
		delete(needed, hash)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(needed) > 0 {
		return port.ErrExecutionCoverage
	}
	current := map[execwire.Address]executionContractValue{}
	rows, err = s.q(ctx).Query(ctx, "SELECT address,account_exists,active,code_hash,created_height,created_tx_hash,creator,code_activated_height,destroyed_height FROM state_contracts WHERE address=ANY($1::bytea[])", keys)
	if err != nil {
		return err
	}
	for rows.Next() {
		var addr, hash []byte
		var v executionContractValue
		if err := rows.Scan(&addr, &v.exists, &v.active, &hash, &v.created, &v.txHash, &v.creator, &v.activated, &v.destroyed); err != nil {
			rows.Close()
			return err
		}
		var a execwire.Address
		copy(a[:], addr)
		copy(v.hash[:], hash)
		current[a] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var history [][]any
	type latest struct {
		value    executionContractValue
		position executionPosition
	}
	last := map[execwire.Address]latest{}
	used := map[executionCreationKey]bool{}
	for _, step := range r.Steps {
		for _, a := range step.Accounts {
			old, known := current[a.Address]
			if baseline {
				if known {
					return port.ErrExecutionConflict
				}
			} else {
				if !known && a.Before.Exists {
					return port.ErrExecutionCoverage
				}
				if old.exists != a.Before.Exists || old.hash != a.Before.CodeHash {
					return port.ErrExecutionConflict
				}
			}
			active, err := executionCodeActive(a.After)
			if err != nil {
				return err
			}
			key := executionCreationKey{a.Address, uint32(step.TxIndex)}
			proof, hasProof := proofs[key]
			if !baseline && known && old.exists == a.After.Exists && old.hash == a.After.CodeHash && !hasProof {
				continue
			}
			v := old
			v.exists, v.active, v.hash = a.After.Exists, active, a.After.CodeHash
			height := int64(r.Ref.Height)
			if !baseline {
				if active && (!old.active || old.hash != v.hash) {
					v.activated = &height
					v.destroyed = nil
				}
				if old.active && !active || old.exists && !a.After.Exists && old.created != nil {
					v.destroyed = &height
				}
			}
			if hasProof {
				beforeActive, err := executionCodeActive(a.Before)
				if err != nil {
					return err
				}
				if beforeActive || a.Before.Nonce != 0 || step.Cause != execwire.Transaction {
					return port.ErrExecutionConflict
				}
				v.created = &height
				v.txHash = proof.TxHash[:]
				v.creator = proof.Creator[:]
				if !a.After.Exists {
					v.destroyed = &height
				}
				used[key] = true
			}
			beforeActive, err := executionCodeActive(a.Before)
			if err != nil {
				return err
			}
			history = append(history, append(executionChangeMeta(r, step, a.Address), a.Before.Exists, a.After.Exists, beforeActive, active, a.Before.CodeHash[:], a.After.CodeHash[:], v.created, v.txHash, v.creator, v.activated, v.destroyed, baseline))
			current[a.Address] = v
			last[a.Address] = latest{v, executionPosition{r.Ref.Height, step.Ordinal}}
		}
	}
	for key := range proofs {
		if !used[key] {
			return port.ErrExecutionConflict
		}
	}
	if len(history) > 0 {
		if err := s.ensureExecutionPartition(ctx, "state_contract_changes", r.Ref.Height, c.PartitionBlocks); err != nil {
			return err
		}
		if err := bt.flush(ctx); err != nil {
			return err
		}
		if _, err := bt.tx.CopyFrom(ctx, pgx.Identifier{"state_contract_changes"}, executionContractChangeColumns, pgx.CopyFromRows(history)); err != nil {
			return err
		}
	}
	for addr, v := range last {
		a := v.value
		bt.queue(`INSERT INTO state_contracts(address,account_exists,active,code_hash,first_height,last_height,last_ordinal,created_height,created_tx_hash,creator,code_activated_height,destroyed_height)
   VALUES($1,$2,$3,$4,$5,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(address) DO UPDATE SET account_exists=EXCLUDED.account_exists,active=EXCLUDED.active,code_hash=EXCLUDED.code_hash,last_height=EXCLUDED.last_height,last_ordinal=EXCLUDED.last_ordinal,created_height=EXCLUDED.created_height,created_tx_hash=EXCLUDED.created_tx_hash,creator=EXCLUDED.creator,code_activated_height=EXCLUDED.code_activated_height,destroyed_height=EXCLUDED.destroyed_height`, addr[:], a.exists, a.active, a.hash[:], int64(v.position.height), int32(v.position.ordinal), a.created, a.txHash, a.creator, a.activated, a.destroyed)
	}
	return nil
}
