package arcingress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"time"
)

type nativeWriter interface {
	Import(context.Context, importMeta, []byte, []byte, execHash, execHash) (importReceipt, error)
}
type execHash = execwire.Hash
type rawControl struct {
	Version     uint8
	Instance    execHash
	Chain       model.FeedChain
	Anchor      nativeRef
	RawEpoch    uint64
	Received    nativeRef
	NativeEpoch uint64 `json:",omitempty"`
	Generation  uint64 `json:",omitempty"`
}
type capturedBlock struct {
	envelopes         []chainfeed.Envelope
	block             *model.Block
	encoded, receipts []byte
}

func verifyBatch(chain model.FeedChain, parent nativeRef, batch []chainfeed.Envelope) (capturedBlock, error) {
	b, err := decodeBatch(chain, batch)
	if err != nil {
		return capturedBlock{}, err
	}
	if b.block.Number != parent.Height+1 {
		return capturedBlock{}, ErrNative
	}
	if b.block.ParentHash != common.Hash(parent.Hash) {
		return capturedBlock{}, errors.Join(ErrNative, errRawFork)
	}
	return b, nil
}
func decodeBatch(chain model.FeedChain, batch []chainfeed.Envelope) (capturedBlock, error) {
	var empty capturedBlock
	if len(batch) != 3 {
		return empty, ErrNative
	}
	data := map[model.DatasetKind][]byte{}
	id := batch[0].Block
	for _, e := range batch {
		if e.Validate(chainfeed.ReadLimits{MaxPayloadBytes: 8 << 20}) != nil || e.Block != id || e.Block.Chain != chain || e.Codec != evmdata.Codec(e.Kind) || data[e.Kind] != nil {
			return empty, ErrNative
		}
		data[e.Kind] = e.Payload
	}
	b, _, err := evmdata.Decode(arc.New(), id, data[model.DatasetHeaders], data[model.DatasetBodies], data[model.DatasetReceipts])
	if err != nil {
		return empty, err
	}
	parts, err := evmdata.Items(data[model.DatasetBodies], 3)
	if err != nil {
		return empty, err
	}
	full := append([]rlp.RawValue{data[model.DatasetHeaders]}, parts...)
	encoded, err := rlp.EncodeToBytes(full)
	if err != nil || len(encoded) > maxPart {
		return empty, ErrNative
	}
	return capturedBlock{envelopes: batch, block: b, encoded: encoded, receipts: data[model.DatasetReceipts]}, nil
}
func (p *program) capture(ctx context.Context, stream chainfeed.BatchStream, wake chan<- struct{}) error {
	for {
		release, err := p.active(ctx)
		if err != nil {
			return err
		}
		batch, err := stream.NextBatch(ctx)
		if err == nil {
			err = p.persist(ctx, batch)
		}
		release()
		if err != nil {
			return err
		}
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}
func (p *program) persist(ctx context.Context, batch []chainfeed.Envelope) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	b, err := verifyBatch(p.control.Chain, p.control.Received, batch)
	if err != nil {
		return err
	}
	missing := 0
	for _, e := range batch {
		ref, err := p.raw.Lookup(ctx, e.Block, e.Kind)
		if errors.Is(err, window.ErrNotFound) {
			missing++
			continue
		}
		if err != nil {
			return err
		}
		if ref.Codec != e.Codec || ref.PayloadBytes != int64(len(e.Payload)) {
			return ErrNative
		}
	}
	if p.raw.Stats().Records+missing > p.cfg.MaxRawBlocks*3 {
		return window.ErrCapacity
	}
	if _, err = p.raw.AppendBatch(ctx, batch); err != nil {
		return err
	}
	next := p.control
	next.Received = nativeRef{Height: b.block.Number, Hash: execwire.Hash(b.block.Hash), Root: execwire.Hash(b.block.StateRoot)}
	oldJSON, _ := json.Marshal(p.control)
	newJSON, _ := json.Marshal(next)
	if err = p.raw.CompareOwner(ctx, oldJSON, newJSON); err != nil {
		return err
	}
	p.control = next
	return nil
}
func (p *program) load(ctx context.Context, id model.FeedBlock, parent nativeRef) (capturedBlock, []window.Reference, error) {
	batch, refs, err := p.readBatch(ctx, id)
	if err != nil {
		return capturedBlock{}, nil, err
	}
	b, err := verifyBatch(id.Chain, parent, batch)
	return b, refs, err
}
func (p *program) readBatch(ctx context.Context, id model.FeedBlock) ([]chainfeed.Envelope, []window.Reference, error) {
	var batch []chainfeed.Envelope
	var refs []window.Reference
	for _, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		ref, err := p.raw.Lookup(ctx, id, k)
		if err != nil {
			return nil, nil, err
		}
		lease, err := p.raw.Acquire(ctx, ref)
		if err != nil {
			return nil, nil, err
		}
		e, err := lease.Envelope()
		if err == nil {
			e.Payload = bytes.Clone(e.Payload)
		}
		err = errors.Join(err, lease.Close())
		if err != nil {
			return nil, nil, err
		}
		batch = append(batch, e)
		refs = append(refs, ref)
	}
	return batch, refs, nil
}
func (p *program) consume(ctx context.Context, w nativeWriter, wake <-chan struct{}, observe func([]window.Reference) error) error {
	return p.consumeUntil(ctx, w, wake, observe, nil)
}
func (p *program) consumeUntil(ctx context.Context, w nativeWriter, wake <-chan struct{}, observe func([]window.Reference) error, stop <-chan struct{}) error {
	var lagSince time.Time
	for {
		select {
		case <-stop:
			return port.ErrExecutionRewinding
		default:
		}
		p.mu.Lock()
		h, r, chain := p.head, p.control.Received, p.control.Chain
		p.mu.Unlock()
		if h.Tip.Height >= r.Height {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-stop:
				return port.ErrExecutionRewinding
			case <-wake:
				continue
			}
		}
		release, err := p.active(ctx)
		if err != nil {
			return err
		}
		select {
		case <-stop:
			release()
			return port.ErrExecutionRewinding
		default:
		}
		attempted := time.Now()
		err = p.importNext(ctx, w, h, chain, observe)
		release()
		if errors.Is(err, port.ErrExecutionCanonicalLag) {
			if lagSince.IsZero() {
				lagSince = attempted
			}
			if time.Since(lagSince) >= p.cfg.Timeout {
				return err
			}
			timer := time.NewTimer(min(100*time.Millisecond, p.cfg.Timeout))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-stop:
				timer.Stop()
				return port.ErrExecutionRewinding
			case <-timer.C:
			}
			continue
		}
		if err != nil {
			return err
		}
		lagSince = time.Time{}
	}
}
func (p *program) importNext(ctx context.Context, w nativeWriter, h nativeHead, chain model.FeedChain, observe func([]window.Reference) error) error {
	if err := p.stable(ctx); err != nil {
		return err
	}
	// An admission read never substitutes E1's separate final NEW fresh check.
	call, finish := context.WithTimeout(ctx, p.cfg.Timeout)
	hash, err := p.deps.Canonical.HashAt(call, h.Tip.Height+1)
	finish()
	if fence := p.stable(ctx); fence != nil {
		return fence
	}
	if err != nil {
		if errors.Is(err, source.ErrNotFound) || errors.Is(err, context.DeadlineExceeded) {
			return errors.Join(port.ErrExecutionCanonicalLag, err)
		}
		return err
	}
	if hash == (common.Hash{}) {
		return ErrNative
	}
	id := model.FeedBlock{Chain: chain, Number: h.Tip.Height + 1, Hash: hash}
	b, refs, err := p.load(ctx, id, h.Tip)
	if err != nil {
		if errors.Is(err, window.ErrNotFound) {
			return errors.Join(errRawMismatch, err)
		}
		return err
	}
	m := p.meta(h, id)
	r, err := w.Import(ctx, m, b.encoded, b.receipts, execwire.Hash(b.block.StateRoot), execwire.Hash(b.block.ReceiptRoot))
	if err != nil {
		return err
	}
	// Validate even injected writers: receipt is completion, never E1 ACK.
	_, digest, err := encodeInput(m, b.encoded, b.receipts)
	if err != nil {
		return err
	}
	if err = validateCompletion(p.identity, h, m, digest, execwire.Hash(b.block.StateRoot), execwire.Hash(b.block.ReceiptRoot), r); err != nil {
		return err
	}
	if err = p.stable(ctx); err != nil {
		return err
	}
	p.mu.Lock()
	p.head = nativeHead{Version: 1, Instance: r.Identity.Instance, ImportEpoch: r.ImportEpoch, ReadEpoch: r.ReadEpoch, RequestID: r.RequestID, Tip: nativeRef{Height: r.Height, Hash: r.Hash, Root: r.Root}}
	p.mu.Unlock()
	return observe(refs)
}
func (p *program) stable(ctx context.Context) error {
	_, err := p.currentControl(ctx)
	return err
}
func (p *program) currentControl(ctx context.Context) (port.ExecutionControl, error) {
	if err := ctx.Err(); err != nil {
		return port.ExecutionControl{}, err
	}
	c, err := p.deps.Store.ExecutionStatus(ctx)
	if err != nil {
		return c, err
	}
	if c.Maintenance {
		return c, port.ErrExecutionMaintenance
	}
	if c.Rewinding || c.ReorgPending || c.Generation != p.deps.Control.Generation {
		return c, port.ErrExecutionRewinding
	}
	if !c.BaselineReady || c.Chain != p.deps.Control.Chain || c.Anchor != p.deps.Control.Anchor || c.AnchorRoot != p.deps.Control.AnchorRoot {
		return c, port.ErrExecutionConflict
	}
	return c, nil
}
func (p *program) meta(h nativeHead, id model.FeedBlock) importMeta {
	return importMeta{Version: 1, RequestID: h.RequestID + 1, ChainID: 5042002, Genesis: execwire.Hash(id.Chain.GenesisHash), ArcRevision: ArcRevision, RethRevision: RethRevision, ImportEpoch: h.ImportEpoch, Height: id.Number, Hash: execwire.Hash(id.Hash), Parent: h.Tip.Hash, ParentRoot: h.Tip.Root}
}
