package storage

import (
	"context"
	"errors"

	"github.com/cockroachdb/pebble"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

const capturePrefix = "/source/capture/"

func init() { RegisterKeyspace("capture", Preserved, capturePrefix) }

func (s *PebbleStorage) CaptureUpdate(ctx context.Context, chain model.FeedChain, fn func(port.CaptureWriter) error) error {
	if err := chain.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.boundBatch(ctx) != nil {
		return port.ErrInvalidData
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}
	// Sync cannot provide crash durability without the WAL.
	if s.config.DisableWAL {
		return errors.New("capture: WAL must be enabled")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b := s.db.NewIndexedBatch()
	defer func() { _ = b.Close() }()
	t := &captureKV{ctx: ctx, reader: b, batch: b, prefix: captureNamespace(chain), active: true}
	defer func() { t.active = false }()
	if err := fn(t); err != nil {
		return err
	}
	if t.err != nil {
		return t.err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return b.Commit(pebble.Sync)
}

func (s *PebbleStorage) CaptureView(ctx context.Context, chain model.FeedChain, fn func(port.CaptureReader) error) error {
	if err := chain.Validate(); err != nil {
		return err
	}
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	snap := s.db.NewSnapshot()
	defer func() { _ = snap.Close() }()
	t := &captureKV{ctx: ctx, reader: snap, prefix: captureNamespace(chain), active: true}
	defer func() { t.active = false }()
	if err := fn(t); err != nil {
		return err
	}
	return errors.Join(t.err, ctx.Err())
}

func captureNamespace(c model.FeedChain) []byte {
	return []byte(capturePrefix + c.ID + "/" + c.GenesisHash.Hex() + "/")
}

type captureKV struct {
	ctx    context.Context
	reader pebble.Reader
	batch  *pebble.Batch
	prefix []byte
	active bool
	used   int
	err    error
}

func (t *captureKV) check(k []byte) error {
	if !t.active {
		return port.ErrClosed
	}
	if t.err != nil {
		return t.err
	}
	if err := t.ctx.Err(); err != nil {
		t.err = err
		return err
	}
	if len(k) == 0 || len(k) > port.CaptureKeyLimit {
		t.err = port.ErrInvalidData
	}
	return t.err
}

func (t *captureKV) Get(k []byte) ([]byte, error) {
	if err := t.check(k); err != nil {
		return nil, err
	}
	v, c, err := t.reader.Get(append(append([]byte(nil), t.prefix...), k...))
	if errors.Is(err, pebble.ErrNotFound) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		t.err = err
		return nil, err
	}
	defer func() { _ = c.Close() }()
	if len(v) > port.CaptureValueLimit {
		t.err = port.ErrInvalidData
		return nil, t.err
	}
	return append([]byte{}, v...), nil
}

func (t *captureKV) Put(k, v []byte) error {
	if err := t.check(k); err != nil {
		return err
	}
	if t.batch == nil {
		t.err = port.ErrReadOnly
		return t.err
	}
	if len(v) > port.CaptureValueLimit || t.used+len(k)+len(v) > port.CaptureTransactionLimit {
		t.err = port.ErrInvalidData
		return t.err
	}
	t.used += len(k) + len(v)
	t.err = t.batch.Set(append(append([]byte(nil), t.prefix...), k...), v, nil)
	return t.err
}

var _ port.CaptureDatabase = (*PebbleStorage)(nil)
