package execution

import (
	"context"
	"net"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// SubmitResult transfers one immutable result and succeeds only on an exact
// durable ACK. It owns the connection, never deletes input or requests a rewind.
func SubmitResult(ctx context.Context, raw net.Conn, profile chains.BinaryProfile, chain execwire.Chain, payload []byte) (execwire.Frame, error) {
	if raw == nil {
		return execwire.Frame{}, port.ErrInvalidData
	}
	defer func() { _ = raw.Close() }()
	if profile == nil {
		return execwire.Frame{}, port.ErrInvalidData
	}
	p, err := PrepareContext(ctx, profile, chain, payload, false)
	if err != nil {
		return execwire.Frame{}, err
	}
	return submitPrepared(ctx, raw, chain, payload, p)
}

func submitPrepared(ctx context.Context, raw net.Conn, chain execwire.Chain, payload []byte, p *Prepared) (execwire.Frame, error) {
	ref := p.Ref()
	if ref.Height == 0 {
		return execwire.Frame{}, port.ErrExecutionSequence
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	c := execwire.NewConn(raw)
	anchor := execwire.Ref{Generation: ref.Generation, Height: ref.Height - 1, Hash: p.result.Parent}
	header := execwire.Header{Version: execwire.Version, Chain: chain, Ref: anchor}
	if err := c.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: header}); err != nil {
		return execwire.Frame{}, err
	}
	hello, err := c.Read(ctx)
	if err != nil {
		return execwire.Frame{}, err
	}
	if hello.Kind != execwire.Hello || hello.Header.Chain != chain || hello.Header.Ref.Generation != ref.Generation {
		return execwire.Frame{}, port.ErrExecutionStale
	}
	visible := hello.Header.Ref
	if visible.Height < anchor.Height || visible.Height == anchor.Height && visible.Hash != anchor.Hash || visible.Height == ref.Height && visible.Hash != ref.Hash {
		return execwire.Frame{}, port.ErrExecutionSequence
	}
	frames, err := execwire.Split(chain, ref, payload, execwire.MaxFrameBytes)
	if err != nil {
		return execwire.Frame{}, err
	}
	for _, f := range frames {
		if err := c.Write(ctx, f); err != nil {
			return execwire.Frame{}, err
		}
	}
	ack, err := c.Read(ctx)
	if err != nil {
		return execwire.Frame{}, err
	}
	if ack.Kind != execwire.Committed || ack.Header.Chain != chain || ack.Header.Ref != ref || ack.Header.Digest != p.Digest() {
		return execwire.Frame{}, port.ErrExecutionConflict
	}
	return ack, nil
}
