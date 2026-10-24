package collector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/exporter"
	"github.com/0xmhha/indexer-go/execwire/spool"
)

type Config struct {
	Native             Options
	Anchor             execwire.Ref
	SpoolDir, Socket   string
	MaxBytes           uint64
	MaxRecords, Window int
	Poll, Retry        time.Duration
}

// Run replays into a durable bounded queue independently of delivery. It owns
// the source spool and the read-only adapter, never the separately operated node.
func Run(ctx context.Context, c Config) (err error) {
	if c.Native.Timeout < 0 || c.Native.Timeout > time.Hour || c.Poll < 0 || c.Retry < 0 || c.Window < 0 || c.Window > 64 || c.MaxRecords < 0 || c.MaxRecords > 1<<20 || c.MaxBytes != 0 && c.MaxBytes <= 64<<10 {
		return execwire.ErrLimit
	}
	if c.Native.Chain.ID != "5042" && c.Native.Chain.ID != "5042002" || !filepath.IsAbs(c.SpoolDir) || !filepath.IsAbs(c.Native.DataDir) || !filepath.IsAbs(c.Socket) {
		return ErrNative
	}
	if err := execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: c.Native.Chain, Ref: c.Anchor}}); err != nil {
		return err
	}
	// Resolve existing parents before comparing paths. A symlink must not hide
	// a source spool inside native storage (or native storage inside the spool).
	native, err := filepath.EvalSymlinks(c.Native.DataDir)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(c.SpoolDir))
	if err != nil {
		return err
	}
	source := filepath.Join(parent, filepath.Base(c.SpoolDir))
	within := func(a, b string) bool {
		return a == b || strings.HasPrefix(a, strings.TrimSuffix(b, string(filepath.Separator))+string(filepath.Separator))
	}
	if within(source, native) || within(native, source) {
		return fmt.Errorf("%w: spool and native directories must be separate", ErrNative)
	}
	s, err := spool.Open(ctx, spool.Options{Path: c.SpoolDir, Chain: c.Native.Chain, Anchor: c.Anchor, MaxBytes: c.MaxBytes, MaxRecords: c.MaxRecords})
	if err != nil {
		return err
	}
	defer func() {
		if e := s.Close(); e != nil {
			err = errors.Join(err, e)
		}
	}()
	if c.Native.ScratchDir == "" {
		c.Native.ScratchDir = filepath.Dir(c.SpoolDir)
	}
	b, err := Start(ctx, c.Native)
	if err != nil {
		return err
	}
	defer func() {
		if e := b.Close(); e != nil {
			err = errors.Join(err, e)
		}
	}()
	hash, err := b.Canonical(ctx, c.Anchor.Height)
	if err != nil {
		return err
	}
	if hash != c.Anchor.Hash {
		return exporter.ErrCheckpointRecovery
	}
	client := &exporter.Client{Chain: c.Native.Chain, Spool: s, Backend: b, Socket: c.Socket, Window: c.Window, Poll: c.Poll, Retry: c.Retry, Timeout: c.Native.Timeout}
	return client.Run(ctx)
}
