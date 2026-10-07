package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// The outbox (refactoring plan R3-1) holds the events of indexed blocks
// under /outbox/<seq>, written in the block transaction. /meta/outbox/seq
// holds the last assigned sequence, so numbering continues after pruning,
// and /meta/outbox/cursor/<name> what each consumer group delivered. None of these
// keys is recorded in undo: a rollback keeps entries that may have been
// delivered and appends the reorganization's entries instead. A reindex
// deletes the entries but keeps the sequence and the cursors, so the
// events of the new indexing continue the numbering consumers have seen.

var _ port.Outbox = (*PebbleStorage)(nil)

const (
	prefixOutbox       = "/outbox/"
	prefixOutboxMeta   = "/meta/outbox/"
	keyOutboxSeq       = prefixOutboxMeta + "seq"
	prefixOutboxCursor = prefixOutboxMeta + "cursor/"
)

// OutboxKey returns the key of outbox entry seq.
func OutboxKey(seq uint64) []byte { return []byte(fmt.Sprintf("%s%020d", prefixOutbox, seq)) }

func init() {
	RegisterKeyspace("outbox", ChainData, prefixOutbox)
	RegisterKeyspace("outbox.sequence", Preserved, prefixOutboxMeta)
}

func outboxKey(key []byte) bool {
	return bytes.HasPrefix(key, []byte(prefixOutbox)) || bytes.HasPrefix(key, []byte(prefixOutboxMeta))
}

// outboxValue is the stored form of an entry.
type outboxValue struct {
	Type string
	Data []byte
}

// AppendOutbox implements port.Outbox.
func (s *PebbleStorage) AppendOutbox(ctx context.Context, entries []port.OutboxEntry) error {
	if len(entries) == 0 {
		return nil
	}
	b := s.boundBatch(ctx)
	if b == nil {
		return errors.New("storage: outbox entries must be appended in a block transaction")
	}
	last, err := s.readUint(b, []byte(keyOutboxSeq))
	if err != nil {
		return fmt.Errorf("read outbox sequence: %w", err)
	}
	for _, e := range entries {
		last++
		v, err := rlp.EncodeToBytes(&outboxValue{Type: e.Type, Data: e.Data})
		if err != nil {
			return err
		}
		if err := b.Set(OutboxKey(last), v, nil); err != nil {
			return err
		}
	}
	return b.Set([]byte(keyOutboxSeq), encodeUint64(last), nil)
}

// ReadOutbox implements port.Outbox.
func (s *PebbleStorage) ReadOutbox(ctx context.Context, after uint64, limit int) ([]port.OutboxEntry, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}
	if after == ^uint64(0) {
		return nil, nil
	}
	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: OutboxKey(after + 1),
		UpperBound: prefixUpperBound([]byte(prefixOutbox)),
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()
	var out []port.OutboxEntry
	for valid := iter.First(); valid && (limit <= 0 || len(out) < limit); valid = iter.Next() {
		seq, err := strconv.ParseUint(string(iter.Key()[len(prefixOutbox):]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("outbox key %q: %w", iter.Key(), err)
		}
		var v outboxValue
		if err := rlp.DecodeBytes(iter.Value(), &v); err != nil {
			return nil, fmt.Errorf("outbox entry %d: %w", seq, err)
		}
		out = append(out, port.OutboxEntry{Seq: seq, Type: v.Type, Data: v.Data})
	}
	return out, iter.Error()
}

// LastOutboxSeq implements port.Outbox.
func (s *PebbleStorage) LastOutboxSeq(ctx context.Context) (uint64, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}
	return s.readUint(s.kv(ctx), []byte(keyOutboxSeq))
}

// OutboxCursor implements port.Outbox.
func (s *PebbleStorage) OutboxCursor(ctx context.Context, name string) (uint64, bool, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, false, err
	}
	v, closer, err := s.kv(ctx).Get([]byte(prefixOutboxCursor + name))
	if errors.Is(err, pebble.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = closer.Close() }()
	if len(v) != 8 {
		return 0, false, fmt.Errorf("outbox cursor %q has %d bytes, want 8", name, len(v))
	}
	return binary.BigEndian.Uint64(v), true, nil
}

// SetOutboxCursor implements port.Outbox.
func (s *PebbleStorage) SetOutboxCursor(ctx context.Context, name string, seq uint64) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}
	// Not synced: a cursor lost in a crash only makes the relay deliver
	// again what consumers drop by sequence; the next block commit syncs it.
	return s.kv(ctx).Set([]byte(prefixOutboxCursor+name), encodeUint64(seq), pebble.NoSync)
}

// PruneOutbox implements port.Outbox.
func (s *PebbleStorage) PruneOutbox(ctx context.Context, before uint64) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}
	if before <= 1 {
		return nil
	}
	if s.boundBatch(ctx) != nil {
		return errors.New("storage: prune the outbox outside a block transaction")
	}
	return s.kv(ctx).DeleteRange([]byte(prefixOutbox), OutboxKey(before), pebble.Sync)
}

// readUint reads an 8-byte big-endian value, 0 if the key is absent.
func (s *PebbleStorage) readUint(r kvStore, key []byte) (uint64, error) {
	v, closer, err := r.Get(key)
	if errors.Is(err, pebble.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer func() { _ = closer.Close() }()
	if len(v) != 8 {
		return 0, fmt.Errorf("value of %s has %d bytes, want 8", key, len(v))
	}
	return binary.BigEndian.Uint64(v), nil
}

func encodeUint64(v uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return b[:]
}
