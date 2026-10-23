package postgres

import (
	"context"
	"math"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jackc/pgx/v5"
)

type executionPosition struct {
	height  uint64
	ordinal uint32
}

func (p executionPosition) before(q executionPosition) bool {
	return p.height < q.height || p.height == q.height && p.ordinal < q.ordinal
}

type executionSlotKey struct {
	address execwire.Address
	slot    execwire.Word
}
type executionSlotValue struct {
	value    execwire.Word
	position executionPosition
}
type executionStorageRoot struct {
	exists bool
	root   execwire.Hash
}

func (s *Store) ExecutionStorageAt(ctx context.Context, addr execwire.Address, slot execwire.Word, height uint64) (execwire.Word, error) {
	var value execwire.Word
	if height > math.MaxInt64 {
		return value, port.ErrInvalidData
	}
	var data []byte
	var vh, ch *int64
	var vo, co *int32
	err := s.q(ctx).QueryRow(ctx, `WITH v AS(SELECT value_after,height,ordinal FROM state_storage_changes WHERE address=$1 AND slot=$2 AND height<=$3 ORDER BY height DESC,ordinal DESC LIMIT 1),
 c AS(SELECT height,ordinal FROM state_storage_clears WHERE address=$1 AND height<=$3 ORDER BY height DESC,ordinal DESC LIMIT 1)
 SELECT (SELECT value_after FROM v),(SELECT height FROM v),(SELECT ordinal FROM v),(SELECT height FROM c),(SELECT ordinal FROM c)`, addr[:], slot[:], int64(height)).Scan(&data, &vh, &vo, &ch, &co)
	if err != nil {
		return value, err
	}
	if ch != nil && (vh == nil || executionPosition{uint64(*vh), uint32(*vo)}.before(executionPosition{uint64(*ch), uint32(*co)})) {
		return value, nil
	}
	if data == nil {
		return value, port.ErrExecutionCoverage
	}
	if len(data) != 32 {
		return value, port.ErrInvalidData
	}
	copy(value[:], data)
	return value, nil
}

var executionStorageChangeColumns = []string{"height", "ordinal", "generation", "block_hash", "block_time", "tx_index", "tx_hash", "cause", "address", "slot", "value_before", "value_after", "baseline"}
var executionStorageClearColumns = []string{"height", "ordinal", "generation", "block_hash", "block_time", "tx_index", "tx_hash", "cause", "address", "root_before", "root_after", "existed_before", "exists_after", "explicit_clear", "baseline"}

func executionChangeMeta(r execwire.Result, step execwire.Step, addr execwire.Address) []any {
	var hash []byte
	if step.TxIndex >= 0 {
		hash = step.TxHash[:]
	}
	return []any{int64(r.Ref.Height), int32(step.Ordinal), int64(r.Ref.Generation), r.Ref.Hash[:], time.Unix(int64(r.Time), 0).UTC(), step.TxIndex, hash, int16(step.Cause), addr[:]}
}
func (s *Store) writeExecutionStorage(ctx context.Context, c port.ExecutionControl, r execwire.Result, baseline bool) error {
	bt := s.boundTx(ctx)
	if bt == nil {
		return port.ErrClosed
	}
	addresses := make([][]byte, 0)
	seen := map[execwire.Address]bool{}
	keys := make([]executionSlotKey, 0)
	keySeen := map[executionSlotKey]bool{}
	for _, step := range r.Steps {
		for _, a := range step.Accounts {
			if !seen[a.Address] {
				seen[a.Address] = true
				addresses = append(addresses, a.Address[:])
			}
		}
		for _, slot := range step.Storage {
			k := executionSlotKey{slot.Address, slot.Slot}
			if !keySeen[k] {
				keySeen[k] = true
				keys = append(keys, k)
			}
			if !seen[slot.Address] {
				seen[slot.Address] = true
				addresses = append(addresses, slot.Address[:])
			}
		}
	}
	roots := map[execwire.Address]executionStorageRoot{}
	rows, err := s.q(ctx).Query(ctx, "SELECT address,exists_now,root FROM state_storage_roots WHERE address=ANY($1::bytea[])", addresses)
	if err != nil {
		return err
	}
	for rows.Next() {
		var addr, root []byte
		var exists bool
		if err := rows.Scan(&addr, &exists, &root); err != nil {
			rows.Close()
			return err
		}
		var a execwire.Address
		var h execwire.Hash
		copy(a[:], addr)
		copy(h[:], root)
		roots[a] = executionStorageRoot{exists, h}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	clears := map[execwire.Address]executionPosition{}
	rows, err = s.q(ctx).Query(ctx, "SELECT DISTINCT ON(address) address,height,ordinal FROM state_storage_clears WHERE address=ANY($1::bytea[]) ORDER BY address,height DESC,ordinal DESC", addresses)
	if err != nil {
		return err
	}
	for rows.Next() {
		var addr []byte
		var h int64
		var o int32
		if err := rows.Scan(&addr, &h, &o); err != nil {
			rows.Close()
			return err
		}
		var a execwire.Address
		copy(a[:], addr)
		clears[a] = executionPosition{uint64(h), uint32(o)}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	aa, ss := make([][]byte, 0, len(keys)), make([][]byte, 0, len(keys))
	for _, k := range keys {
		aa = append(aa, k.address[:])
		ss = append(ss, k.slot[:])
	}
	values := map[executionSlotKey]executionSlotValue{}
	rows, err = s.q(ctx).Query(ctx, `SELECT s.address,s.slot,s.value,s.last_height,s.last_ordinal FROM state_storage s JOIN unnest($1::bytea[],$2::bytea[]) AS k(address,slot) ON s.address=k.address AND s.slot=k.slot`, aa, ss)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a, k, v []byte
		var h int64
		var o int32
		if err := rows.Scan(&a, &k, &v, &h, &o); err != nil {
			rows.Close()
			return err
		}
		var key executionSlotKey
		var word execwire.Word
		copy(key.address[:], a)
		copy(key.slot[:], k)
		copy(word[:], v)
		values[key] = executionSlotValue{word, executionPosition{uint64(h), uint32(o)}}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var history, clearRows [][]any
	lastSlots := map[executionSlotKey]executionSlotValue{}
	type rootValue struct {
		value    executionStorageRoot
		position executionPosition
	}
	lastRoots := map[execwire.Address]rootValue{}
	for _, step := range r.Steps {
		pos := executionPosition{r.Ref.Height, step.Ordinal}
		proofs := map[execwire.Address]execwire.AccountChange{}
		newClears := map[execwire.Address]bool{}
		for _, a := range step.Accounts {
			before := executionStorageRoot{a.Before.Exists, a.Before.StorageRoot}
			old, known := roots[a.Address]
			if baseline {
				if known {
					return port.ErrExecutionConflict
				}
			} else {
				if !known && before.exists {
					return port.ErrExecutionCoverage
				}
				if old != before {
					return port.ErrExecutionConflict
				}
			}
			proofs[a.Address] = a
			empty := execwire.Hash(types.EmptyRootHash)
			if baseline {
				newClears[a.Address] = !a.After.Exists || a.After.StorageRoot == empty
			} else {
				newClears[a.Address] = a.StorageCleared || !a.Before.Exists || !a.After.Exists || a.After.StorageRoot == empty && a.Before.StorageRoot != empty
			}
		}
		// Compare every Before against the PHASE-BEFORE state, before this step's
		// clear is installed. Same-step After writes then survive that clear.
		for _, slot := range step.Storage {
			a, covered := proofs[slot.Address]
			if !covered {
				return port.ErrExecutionCoverage
			}
			k := executionSlotKey{slot.Address, slot.Slot}
			old, known := values[k]
			clear, hasClear := clears[slot.Address]
			if baseline {
				if known {
					return port.ErrExecutionConflict
				}
				if (!a.After.Exists || a.After.StorageRoot == execwire.Hash(types.EmptyRootHash)) && slot.After != (execwire.Word{}) {
					return port.ErrExecutionConflict
				}
			} else {
				if hasClear && (!known || old.position.before(clear)) {
					old.value = execwire.Word{}
					known = true
				}
				if !known && (!a.Before.Exists || a.Before.StorageRoot == execwire.Hash(types.EmptyRootHash)) {
					known = true
				}
				if !known {
					return port.ErrExecutionCoverage
				}
				if old.value != slot.Before {
					return port.ErrExecutionConflict
				}
			}
			history = append(history, append(executionChangeMeta(r, step, slot.Address), slot.Slot[:], slot.Before[:], slot.After[:], baseline))
		}
		for addr, clear := range newClears {
			if clear {
				a := proofs[addr]
				clears[addr] = pos
				clearRows = append(clearRows, append(executionChangeMeta(r, step, addr), a.Before.StorageRoot[:], a.After.StorageRoot[:], a.Before.Exists, a.After.Exists, a.StorageCleared, baseline))
			}
		}
		for _, slot := range step.Storage {
			k := executionSlotKey{slot.Address, slot.Slot}
			v := executionSlotValue{slot.After, pos}
			values[k] = v
			lastSlots[k] = v
		}
		for addr, a := range proofs {
			v := executionStorageRoot{a.After.Exists, a.After.StorageRoot}
			roots[addr] = v
			lastRoots[addr] = rootValue{v, pos}
		}
	}
	if len(history) > 0 {
		if err := s.ensureExecutionPartition(ctx, "state_storage_changes", r.Ref.Height, c.PartitionBlocks); err != nil {
			return err
		}
		if err := bt.flush(ctx); err != nil {
			return err
		}
		if _, err := bt.tx.CopyFrom(ctx, pgx.Identifier{"state_storage_changes"}, executionStorageChangeColumns, pgx.CopyFromRows(history)); err != nil {
			return err
		}
	}
	if len(clearRows) > 0 {
		if err := s.ensureExecutionPartition(ctx, "state_storage_clears", r.Ref.Height, c.PartitionBlocks); err != nil {
			return err
		}
		if err := bt.flush(ctx); err != nil {
			return err
		}
		if _, err := bt.tx.CopyFrom(ctx, pgx.Identifier{"state_storage_clears"}, executionStorageClearColumns, pgx.CopyFromRows(clearRows)); err != nil {
			return err
		}
	}
	for key, v := range lastSlots {
		bt.queue(`INSERT INTO state_storage(address,slot,value,last_height,last_ordinal) VALUES($1,$2,$3,$4,$5) ON CONFLICT(address,slot) DO UPDATE SET value=EXCLUDED.value,last_height=EXCLUDED.last_height,last_ordinal=EXCLUDED.last_ordinal`, key.address[:], key.slot[:], v.value[:], int64(v.position.height), int32(v.position.ordinal))
	}
	for addr, v := range lastRoots {
		bt.queue(`INSERT INTO state_storage_roots(address,exists_now,root,last_height,last_ordinal) VALUES($1,$2,$3,$4,$5) ON CONFLICT(address) DO UPDATE SET exists_now=EXCLUDED.exists_now,root=EXCLUDED.root,last_height=EXCLUDED.last_height,last_ordinal=EXCLUDED.last_ordinal`, addr[:], v.value.exists, v.value.root[:], int64(v.position.height), int32(v.position.ordinal))
	}
	return nil
}
