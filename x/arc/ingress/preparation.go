package arcingress

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"github.com/ethereum/go-ethereum/common"
	"os"
	"path/filepath"
)

// Check starts no peer/child. The writer independently rechecks live SDK roots.
func (p *program) Check(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped || p.started {
		return port.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkPaths(p.cfg, p.opts, p.deps.Socket); err != nil {
		return fmt.Errorf("native paths: %w", err)
	}
	for _, name := range []string{"db", "rocksdb", "static_files"} {
		info, err := os.Lstat(filepath.Join(p.cfg.Native.DataDir, name))
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return ErrNative
		}
	}
	if err := nativestore.CheckStart(p.cfg.Native.DataDir); err != nil {
		return err
	}
	for _, name := range []string{".arc-native-import.pending", ".arc-native-bootstrap.pending"} {
		if _, err := os.Lstat(filepath.Join(p.cfg.Native.DataDir, name)); !errors.Is(err, os.ErrNotExist) {
			return ErrNative
		}
	}
	bootBytes, err := privateBytes(filepath.Join(p.cfg.Native.DataDir, ".arc-native-bootstrap.json"))
	if err != nil {
		return err
	}
	var boot bootstrapReceipt
	if strictJSON(bootBytes, &boot) != nil {
		return fmt.Errorf("native bootstrap JSON: %w", ErrNative)
	}
	c := p.deps.Control
	if c.Generation != 1 {
		return fmt.Errorf("native generation: %w", ErrNative)
	}
	if boot.Version != 1 || boot.ArcRevision != ArcRevision || boot.RethRevision != RethRevision || boot.Anchor.Version != 1 || boot.Anchor.Coverage != "full" || boot.Anchor.Chain != c.Chain || boot.Anchor.Ref != c.Anchor || boot.Anchor.Root != c.AnchorRoot || boot.Floor != c.Anchor.Height || boot.Next != c.Anchor.Height+1 || boot.StateDigest == (execwire.Hash{}) {
		return fmt.Errorf("native bootstrap identity: %w", ErrNative)
	}
	p.bootstrap = execwire.Hash(sha256.Sum256(bootBytes))
	var ready nativeReady
	ready.Version = 1
	err = readArtifact(filepath.Join(p.cfg.Native.DataDir, identityName), &ready.Identity)
	if errors.Is(err, os.ErrNotExist) {
		for _, name := range []string{headName, ".arc-native-import-receipts"} {
			if _, e := os.Lstat(filepath.Join(p.cfg.Native.DataDir, name)); !errors.Is(e, os.ErrNotExist) {
				return ErrNative
			}
		}
		if _, e := os.Lstat(p.opts.Path); !errors.Is(e, os.ErrNotExist) || !p.cfg.CreateWindow || c.Visible != c.Anchor {
			return ErrNative
		}
		return nil
	}
	if err != nil {
		return err
	}
	if err = readArtifact(filepath.Join(p.cfg.Native.DataDir, headName), &ready.Head); err != nil {
		return err
	}
	if err = validateReady(ready, c, p.bootstrap); err != nil {
		return err
	}
	p.identity, p.head = ready.Identity, ready.Head
	if p.raw == nil {
		if _, err = os.Lstat(p.opts.Path); errors.Is(err, os.ErrNotExist) {
			if !p.cfg.CreateWindow || ready.Head.Tip != ready.Identity.Anchor {
				return ErrNative
			}
			return nil
		}
		p.raw, err = window.OpenOwnedChecked(p.opts, ownerDomain, func(window.DeletionRecord) error { return window.ErrUnsafeGC })
		if err != nil {
			return err
		}
	}
	release, err := p.active(ctx)
	if err != nil {
		return err
	}
	defer release()
	return p.reconcile(ctx)
}
func (p *program) readControl(ctx context.Context) error {
	o, err := p.raw.ReadOwner(ctx)
	if err != nil {
		return err
	}
	var c rawControl
	if strictJSON(o.State, &c) != nil || c.Version != 1 || c.Instance != p.identity.Instance || c.Chain != (model.FeedChain{ID: p.deps.Control.Chain.ID, GenesisHash: common.Hash(p.deps.Control.Chain.Genesis)}) || c.Anchor != p.identity.Anchor || c.RawEpoch != 1 || c.Received.Height < p.head.Tip.Height || c.Received.Hash == (execwire.Hash{}) || c.Received.Root == (execwire.Hash{}) {
		return ErrNative
	}
	if c.Received.Height == c.Anchor.Height && c.Received != c.Anchor {
		return ErrNative
	}
	p.control = c
	return nil
}
func (p *program) reconcile(ctx context.Context) error {
	if err := p.readControl(ctx); err != nil {
		return err
	}
	h := p.head
	if p.control.Received.Height > p.control.Anchor.Height {
		id := model.FeedBlock{Chain: p.control.Chain, Number: p.control.Received.Height, Hash: common.Hash(p.control.Received.Hash)}
		batch, _, err := p.readBatch(ctx, id)
		if err != nil {
			return err
		}
		decoded, err := decodeBatch(id.Chain, batch)
		if err != nil {
			return err
		}
		if decoded.block.StateRoot != common.Hash(p.control.Received.Root) {
			return ErrNative
		}
	}
	if h.Tip == p.identity.Anchor {
		return nil
	}
	var r importReceipt
	if err := readArtifact(completionPath(p.cfg.Native.DataDir, h.ImportEpoch, h.Tip.Height), &r); err != nil {
		return err
	}
	if r.Hash != h.Tip.Hash || r.Root != h.Tip.Root || r.ReadEpoch != h.ReadEpoch || r.RequestID != h.RequestID {
		return ErrNative
	}
	previous := nativeHead{Version: 1, Instance: h.Instance, ImportEpoch: h.ImportEpoch, ReadEpoch: h.ReadEpoch - 1, RequestID: h.RequestID - 1, Tip: nativeRef{h.Tip.Height - 1, r.Parent, r.ParentRoot}}
	id := model.FeedBlock{Chain: p.control.Chain, Number: h.Tip.Height, Hash: common.Hash(h.Tip.Hash)}
	b, _, err := p.load(ctx, id, previous.Tip)
	if err != nil {
		return err
	}
	m := p.meta(previous, id)
	_, digest, err := encodeInput(m, b.encoded, b.receipts)
	if err != nil {
		return err
	}
	return validateCompletion(p.identity, previous, m, digest, execwire.Hash(b.block.StateRoot), execwire.Hash(b.block.ReceiptRoot), r)
}
