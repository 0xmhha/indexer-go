package execution

import (
	"context"
	"errors"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"net"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Receiver is assembled before ingest. Its connection session is private;
// the runtime listener must authenticate the Unix peer before invoking it.
type Receiver struct {
	RequiredStateProofs *stateproof.Selection
	RequireBlobSidecars bool
	Chain               execwire.Chain
	Profile             chains.BinaryProfile
	Store               port.ExecutionStore
	OpenController      func(context.Context) (*Controller, error)
	Confirm             func(context.Context, []byte) (bool, error)
	Reorg               func(context.Context, *Controller) error
	ReorgOnError        func(error) bool
	ValidateReorg       func(context.Context, execwire.Ref) error
	Window              int
	Timeout             time.Duration
	OnSessionError      func(error)
	OnController        func(*Controller) func()
	// OnReady runs after the owned private listener is published, before accept.
	OnReady      func() error
	RetrySession func(error) bool
}

type receiverACK struct {
	ref    execwire.Ref
	digest execwire.Hash
	future *Future
}

func (r *Receiver) serveConn(parent context.Context, raw net.Conn) error {
	conn := execwire.NewConn(raw)
	defer func() { _ = conn.Close() }()
	if r.Profile == nil || r.Store == nil || r.OpenController == nil || r.Confirm == nil {
		return port.ErrInvalidData
	}
	window := r.Window
	if window == 0 {
		window = 8
	}
	if window < 1 || window > 64 {
		return execwire.ErrLimit
	}
	timeout := r.Timeout
	if timeout == 0 {
		timeout = time.Minute
	}
	if timeout <= 0 || timeout > time.Hour {
		return execwire.ErrLimit
	}
	read := func(ctx context.Context) (execwire.Frame, error) {
		call, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return conn.Read(call)
	}
	hello, err := read(parent)
	if err != nil {
		return err
	}
	if hello.Kind != execwire.Hello || hello.Header.Chain != r.Chain {
		return port.ErrExecutionConflict
	}
	controller, err := r.OpenController(parent)
	if err != nil {
		return err
	}
	defer func() { _ = controller.Close() }()
	if r.OnController != nil {
		if release := r.OnController(controller); release != nil {
			defer release()
		}
	}
	status, err := r.Store.ExecutionStatus(parent)
	if err != nil {
		return err
	}
	if status.Maintenance {
		return port.ErrExecutionMaintenance
	}
	if status.Chain != r.Chain || status.Rewinding || status.ReorgPending || !status.BaselineReady {
		return port.ErrExecutionCoverage
	}
	if hello.Header.Ref.Generation > status.Generation {
		return port.ErrExecutionStale
	}
	if err = conn.Write(parent, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: r.Chain, Ref: status.Visible}}); err != nil {
		return err
	}
	assembler, err := execwire.NewAssembler(execwire.Limits{Blocks: window, Bytes: 256 << 20, TTL: timeout})
	if err != nil {
		return err
	}
	defer assembler.Reset()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = conn.Close(); close(closed) })
	defer func() {
		cancel()
		if !stop() {
			<-closed
		}
	}()
	acks := make(chan receiverACK, window)
	ackDone := make(chan error, 1)
	go func() {
		ackErr := func() error {
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case ack := <-acks:
					if ack.future != nil {
						if err := ack.future.Wait(ctx); err != nil {
							return err
						}
					}
					call, finish := context.WithTimeout(ctx, timeout)
					err := conn.Write(call, execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: r.Chain, Ref: ack.ref, Digest: ack.digest}})
					finish()
					if err != nil {
						return err
					}
				}
			}
		}()
		ackDone <- ackErr
		cancel()
	}()
	var requestedRewind *execwire.Ref
	readErr := func() error {
		for {
			frame, err := read(ctx)
			if err != nil {
				return err
			}
			if frame.Header.Chain != r.Chain || frame.Header.Ref.Generation != status.Generation {
				return port.ErrExecutionStale
			}
			if frame.Kind == execwire.Rewind {
				if r.Reorg == nil || r.ValidateReorg == nil {
					return port.ErrInvalidData
				}
				ref := frame.Header.Ref
				requestedRewind = &ref
				return port.ErrExecutionRewinding
			}
			if assembler.Expire(time.Now()) > 0 {
				return execwire.ErrIncomplete
			}
			payload, complete, err := assembler.Add(time.Now(), frame)
			if err != nil {
				return err
			}
			if !complete {
				continue
			}
			prepared, err := PrepareWithRequirements(ctx, r.Profile, r.Chain, payload, Requirements{BlobSidecars: r.RequireBlobSidecars, StateProofs: r.RequiredStateProofs})
			if err != nil {
				return err
			}
			if prepared.Ref() != frame.Header.Ref || prepared.Digest() != frame.Header.Digest {
				return port.ErrExecutionConflict
			}
			ack := receiverACK{ref: prepared.Ref(), digest: prepared.Digest()}
			confirmed := false
			if prepared.Ref().Height <= status.Visible.Height {
				confirmed, err = r.Confirm(ctx, payload)
				if err != nil {
					return err
				}
				if !confirmed {
					return port.ErrExecutionConflict
				}
			}
			if !confirmed {
				ack.future, err = controller.Submit(ctx, prepared)
				if errors.Is(err, port.ErrExecutionSequence) {
					confirmed, err = r.Confirm(ctx, payload)
					if err == nil && !confirmed {
						err = port.ErrExecutionSequence
					}
				}
				if err != nil {
					return err
				}
			}
			select {
			case acks <- ack:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}()
	cancel()
	_ = conn.Close()
	ackErr := <-ackDone
	result := readErr
	if ackErr != nil && !errors.Is(ackErr, context.Canceled) && !errors.Is(ackErr, context.DeadlineExceeded) {
		result = ackErr
	}
	if requestedRewind != nil {
		if err = r.ValidateReorg(parent, *requestedRewind); err != nil {
			return err
		}
		if err = r.Reorg(parent, controller); err != nil {
			return err
		}
		return port.ErrExecutionRewinding
	}
	if r.Reorg != nil && r.ReorgOnError != nil && r.ReorgOnError(result) {
		if err = r.Reorg(parent, controller); err != nil {
			return err
		}
		return port.ErrExecutionRewinding
	}
	return result
}
