package arcingress

import (
	"context"
	"encoding/json"
)

func (p *program) validRawControl(c rawControl) bool {
	if c.Version == 1 {
		return c.RawEpoch == 1 && c.NativeEpoch == 0 && c.Generation == 0 && p.head.ImportEpoch == 1 && p.deps.Control.Generation == 1
	}
	return c.Version == 2 && positive(c.RawEpoch) && c.NativeEpoch == p.head.ImportEpoch && c.Generation == p.deps.Control.Generation
}
func (p *program) migrateControl(ctx context.Context, ready nativeReady) error {
	if err := p.stable(ctx); err != nil {
		return err
	}
	if ready.Identity != p.identity || ready.Head != p.head || validateReady(ready, p.deps.Control, p.bootstrap) != nil {
		return ErrNative
	}
	if err := p.reconcile(ctx); err != nil {
		return err
	}
	if p.control.Version == 2 {
		return nil
	}
	if p.control.Version != 1 || p.head.ImportEpoch != 1 || p.deps.Control.Generation != 1 {
		return ErrNative
	}
	if err := p.stable(ctx); err != nil {
		return err
	}
	next := p.control
	next.Version = 2
	next.NativeEpoch = p.head.ImportEpoch
	next.Generation = p.deps.Control.Generation
	before, _ := json.Marshal(p.control)
	after, _ := json.Marshal(next)
	if err := p.raw.CompareOwner(ctx, before, after); err != nil {
		return err
	}
	p.control = next
	return p.stable(ctx)
}
