package nativeops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	NativeExecutable, NativeDataDir, SpoolDir string
	Chain                                     execwire.Chain
	Anchor                                    execwire.Ref
	AnchorRoot                                execwire.Hash
	SpoolBytes, ReserveBytes                  uint64
	SpoolRecords                              int
}

func LoadConfig(path string) (Config, error) {
	var c Config
	if !filepath.IsAbs(path) {
		return c, retention.ErrUnsafe
	}
	if e := nativestore.PrivateFile(path); e != nil {
		return c, e
	}
	// Same private artifact parser/bounds used for E3 permits; config has no DSN.
	if e := retention.ReadArtifact(path, &c); e != nil {
		return c, e
	}
	_, _, e := options(c)
	return c, e
}
func options(c Config) (spool.Options, nativestore.Options, error) {
	if (c.Chain.ID != "5042" && c.Chain.ID != "5042002") || c.Chain.Genesis == (execwire.Hash{}) || c.AnchorRoot == (execwire.Hash{}) || c.Anchor.Generation == 0 || !filepath.IsAbs(c.NativeExecutable) || !filepath.IsAbs(c.NativeDataDir) || !filepath.IsAbs(c.SpoolDir) {
		return spool.Options{}, nativestore.Options{}, retention.ErrUnsafe
	}
	native, e := filepath.EvalSymlinks(c.NativeDataDir)
	if errors.Is(e, os.ErrNotExist) {
		parent, pe := filepath.EvalSymlinks(filepath.Dir(c.NativeDataDir))
		if pe != nil {
			return spool.Options{}, nativestore.Options{}, pe
		}
		native = filepath.Join(parent, filepath.Base(c.NativeDataDir))
		pending, phase, pe := nativestore.Status(nativestore.Options{DataDir: native})
		if pe != nil || phase != "swap" || pending.Request.Chain != c.Chain || pending.Request.Anchor != c.Anchor {
			return spool.Options{}, nativestore.Options{}, retention.ErrUnsafe
		}
	} else if e != nil {
		return spool.Options{}, nativestore.Options{}, e
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(c.SpoolDir))
	if e != nil {
		return spool.Options{}, nativestore.Options{}, e
	}
	source := filepath.Join(parent, filepath.Base(c.SpoolDir))
	within := func(a, b string) bool {
		rel, e := filepath.Rel(a, b)
		return e == nil && (rel == "." || rel != ".." && len(rel) > 0 && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
	}
	if native != filepath.Clean(c.NativeDataDir) || source != filepath.Clean(c.SpoolDir) || within(native, source) || within(source, native) {
		return spool.Options{}, nativestore.Options{}, retention.ErrUnsafe
	}
	return spool.Options{Path: source, Chain: c.Chain, Anchor: c.Anchor, MaxBytes: c.SpoolBytes, MaxRecords: c.SpoolRecords}, nativestore.Options{DataDir: native, ReserveBytes: c.ReserveBytes, Adapter: Native{Executable: c.NativeExecutable, Chain: c.Chain, AnchorRoot: c.AnchorRoot}}, nil
}
func PlanRetention(ctx context.Context, c Config, keep uint64) (retention.Request, error) {
	source, node, e := options(c)
	if e != nil {
		return retention.Request{}, e
	}
	lease, e := nativestore.AcquireRun(node.DataDir)
	if e != nil {
		return retention.Request{}, e
	}
	defer func() { _ = lease.Close() }()
	native := node.Adapter.(Native)
	observed, e := native.Inspect(ctx, node.DataDir, retention.Request{Version: 1, Chain: source.Chain, Anchor: source.Anchor})
	if e != nil {
		return retention.Request{}, e
	}
	if observed.AnchorRoot != c.AnchorRoot {
		return retention.Request{}, retention.ErrUnsafe
	}
	return spool.InspectRetention(ctx, source, observed.Head, observed.Root, keep)
}
func applyRetention(ctx context.Context, c Config, p retention.Permit) (r retention.Receipt, err error) {
	source, node, err := options(c)
	if err != nil {
		return r, err
	}
	if p.Validate() != nil || p.Request.Chain != source.Chain || p.Request.Anchor != source.Anchor {
		return r, retention.ErrUnsafe
	}
	s, err := spool.OpenMaintenance(ctx, source, p)
	if err != nil {
		return r, err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	return s.CollectRetention(ctx, p, func(ctx context.Context) error { return nativestore.Compact(ctx, node, p) })
}
func ReleaseRetention(ctx context.Context, c Config, p retention.Permit, release retention.Release) (err error) {
	source, node, err := options(c)
	if err != nil {
		return err
	}
	s, err := spool.OpenMaintenance(ctx, source, p)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	r, err := s.RetentionCompletion(ctx, p)
	if err != nil {
		return err
	}
	if release.Match(p, r) != nil {
		return retention.ErrUnsafe
	}
	if err = nativestore.Release(ctx, node, p, r, release); err != nil {
		return err
	}
	return s.ReleaseRetention(ctx, p, release)
}

type CLI struct {
	Action, Config, Permit, Release, Output string
	Keep                                    uint64
	Supervisor                              *os.File
}

func RunRetention(ctx context.Context, o CLI, out io.Writer) error {
	if o.Action != "plan" && o.Action != "inspect" && o.Action != "apply" && o.Action != "release" {
		return fmt.Errorf("action must be plan, inspect, apply or release")
	}
	c, e := LoadConfig(o.Config)
	if e != nil {
		return e
	}
	emit := func(v any) error {
		if o.Output != "" {
			return retention.WriteArtifact(o.Output, v)
		}
		return json.NewEncoder(out).Encode(v)
	}
	if o.Action == "plan" {
		r, e := PlanRetention(ctx, c, o.Keep)
		if e != nil {
			return e
		}
		return emit(r)
	}
	if o.Action == "inspect" {
		source, node, e := options(c)
		if e != nil {
			return e
		}
		p, phase, ne := nativestore.Status(node)
		if ne != nil && !errors.Is(ne, os.ErrNotExist) {
			return ne
		}
		s, e := spool.InspectMetadata(ctx, source.Path)
		if e != nil {
			return e
		}
		return emit(struct {
			Permit retention.Permit
			Phase  string
			Source spool.MetadataObservation
			Scope  string
		}{p, phase, s, "read-only observations; no ACK/deletion/start authorization"})
	}
	var p retention.Permit
	if e = retention.ReadArtifact(o.Permit, &p); e != nil {
		return e
	}
	if e = p.Validate(); e != nil {
		return e
	}
	if o.Action == "apply" {
		if o.Supervisor == nil {
			return fmt.Errorf("apply requires live inherited DB supervisor; use execution-retention collect")
		}
		guarded, hello, stop, e := retention.Supervise(ctx, o.Supervisor, p)
		if e != nil {
			return e
		}
		defer stop()
		r, e := applyRetention(guarded, c, p)
		if e != nil {
			return e
		}
		return emit(retention.SupervisedReply{Version: 1, Challenge: hello.Challenge, Receipt: r})
	}
	var release retention.Release
	if e = retention.ReadArtifact(o.Release, &release); e != nil {
		return e
	}
	if e = ReleaseRetention(ctx, c, p, release); e != nil {
		return e
	}
	return emit(struct{ Status string }{"producer_fences_released"})
}
