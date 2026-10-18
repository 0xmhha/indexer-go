package capture

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/window"
)

// RawConsumer returns success only AFTER its own durable transaction. Bytes
// are read-only and valid until return; spawned work must be joined first.
// Redelivery is possible after crash and must be idempotent by identity.
type RawConsumer interface {
	Consume(context.Context, model.CaptureIdentity, []chainfeed.Envelope) error
}

var errRawReadBusy = errors.New("capture: raw read budget busy")

func (s *Store) RawAcknowledged(ctx context.Context, id model.CaptureIdentity, consumer string) (bool, error) {
	var ack bool
	err := s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		st, err := loadState(tx)
		if err != nil {
			return err
		}
		known := false
		for _, c := range st.RawConsumers {
			known = known || c == consumer
		}
		if !known {
			return port.ErrInvalidData
		}
		got, err := canonicalID(tx, st, id.Block)
		if err != nil {
			return err
		}
		if got != id {
			return ErrStale
		}
		v, err := tx.Get([]byte(ackKey(id, consumer)))
		if errors.Is(err, port.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(v) != 1 || v[0] != 1 {
			return ErrConflict
		}
		ack = true
		return nil
	})
	return ack, err
}

// RawConsumerTask resumes using durable per-identity ACKs, not source fsync or
// another consumer's progress. Lease release precedes ACK; failure never ACKs.
func RawConsumerTask(name string, consumer RawConsumer, poll time.Duration) Task {
	return func(ctx context.Context, raw *window.Store, facts *Store) error {
		if name == "" || consumer == nil || poll < time.Millisecond {
			return port.ErrInvalidData
		}
		st, err := facts.State(ctx)
		if err != nil {
			return err
		}
		n := st.Anchor.Number
		generation := st.Generation
		for {
			st, err := facts.State(ctx)
			if err != nil {
				return err
			}
			if st.Generation != generation {
				return ErrStale
			}
			if n == math.MaxUint64 {
				return ErrGap
			}
			if n >= st.Through.Number {
				if err := pause(ctx, poll, nil); err != nil {
					return err
				}
				continue
			}
			id, err := facts.CanonicalAt(ctx, st.Generation, n+1)
			if err != nil {
				return err
			}
			acked, err := facts.RawAcknowledged(ctx, id, name)
			if err != nil {
				return err
			}
			if !acked {
				err := consumeRaw(ctx, raw, st.Plan, id, consumer)
				if errors.Is(err, errRawReadBusy) {
					if err := pause(ctx, poll, nil); err != nil {
						return err
					}
					continue
				}
				if err != nil {
					return err
				}
				if err := waitDeletion(ctx, true, func() error { return facts.AckRaw(ctx, id, name) }); err != nil {
					return err
				}
			}
			n++
		}
	}
}

func consumeRaw(ctx context.Context, raw *window.Store, plan model.CapturePlan, id model.CaptureIdentity, consumer RawConsumer) (err error) {
	var leases []*window.Lease
	defer func() {
		for _, l := range leases {
			err = errors.Join(err, l.Close())
		}
	}()
	var envelopes []chainfeed.Envelope
	for _, req := range plan.Required {
		ref, err := raw.Lookup(ctx, id.Block, req.Kind)
		if err != nil {
			return err
		}
		l, err := raw.Acquire(ctx, ref)
		if err != nil {
			if err == window.ErrBusy {
				return errRawReadBusy
			}
			return err
		}
		leases = append(leases, l)
		e, err := l.Envelope()
		if err != nil {
			return err
		}
		envelopes = append(envelopes, e)
	}
	return consumer.Consume(ctx, id, envelopes)
}
