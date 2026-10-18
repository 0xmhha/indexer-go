// Package chainfeed defines the protocol-independent contract for bounded
// peer/relay acquisition. Adapters live in independently linked modules;
// this package never starts a full node or creates an RPC rate limiter.
package chainfeed

import (
	"context"
	"errors"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

var ErrUnsupported = errors.New("chainfeed: unsupported capture requirement")

// Capability is data actually obtainable through an adapter and its declared
// supplemental sources. Capability declarations must be backed by protocol
// fixtures; they are not proof that any received block is complete.
type Capability struct {
	Dataset  model.DatasetKind
	Coverage model.DatasetCoverage
}

type Capabilities struct {
	Chain    model.FeedChain
	Datasets []Capability
}

// CheckPlan fails closed before acquisition starts. It does not silently
// downgrade a full-state/full-blob request to a tracked subset.
func (c Capabilities) CheckPlan(chain model.FeedChain, p model.CapturePlan) error {
	if err := chain.Validate(); err != nil {
		return err
	}
	if c.Chain != chain {
		return fmt.Errorf("%w: different chain/genesis", ErrUnsupported)
	}
	if err := p.Validate(); err != nil {
		return err
	}
	for _, available := range c.Datasets {
		if !available.Dataset.Valid() || available.Coverage.Validate() != nil {
			return fmt.Errorf("%w: invalid capability", ErrUnsupported)
		}
	}
	for _, required := range p.Required {
		found := false
		for _, available := range c.Datasets {
			if available.Dataset == required.Kind && available.Coverage.Covers(required.Coverage) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: %s/%s", ErrUnsupported, required.Kind, required.Coverage.Scope)
		}
	}
	return nil
}

// Envelope owns immutable raw bytes for one dataset. The consumer must
// persist and release it before requesting more than its byte budget allows.
// Structural/chain verification precedes durable append; completeness and
// canonical/finality checks are separate stages.
type Envelope struct {
	Block   model.FeedBlock
	Kind    model.DatasetKind
	Codec   string
	Payload []byte
}

type ReadLimits struct {
	MaxPayloadBytes int64
}

func (l ReadLimits) Validate() error {
	if l.MaxPayloadBytes <= 0 {
		return fmt.Errorf("%w: positive payload byte limit required", model.ErrCaptureInvalid)
	}
	return nil
}

// Validate checks an already bounded envelope before durable append. Wire
// decoders must additionally check the frame length before allocating bytes.
func (e Envelope) Validate(limits ReadLimits) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	if err := e.Block.Validate(); err != nil {
		return err
	}
	if !e.Kind.Valid() || e.Codec == "" || len(e.Payload) == 0 || int64(len(e.Payload)) > limits.MaxPayloadBytes {
		return fmt.Errorf("%w: envelope dataset, codec or payload size", model.ErrCaptureInvalid)
	}
	return nil
}

// Stream is pull-based so callers control in-flight memory and cancellation.
// Next must reject an oversized frame before allocating its declared length.
// EOF denotes an ended stream, never a verified empty dataset.
type Stream interface {
	Next(context.Context) (Envelope, error)
	Close() error
}

// Adapter implementations must use the application's shared RPC pool for
// supplemental public reads. They may not cache fresh canonical reads or
// advertise peer history/state outside the range they can actually serve.
type Adapter interface {
	Capabilities(context.Context) (Capabilities, error)
	Subscribe(context.Context, model.FeedBlock, ReadLimits) (Stream, error)
	FetchRange(context.Context, model.FeedChain, uint64, uint64, ReadLimits) (Stream, error)
	FetchDataset(context.Context, model.FeedBlock, model.DatasetRequirement, ReadLimits) (Envelope, error)
	Close() error
}
