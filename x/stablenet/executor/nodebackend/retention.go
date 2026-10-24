package nodebackend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/nodestore"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/spool"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
)

func retentionOptions(c Config) (spool.Options, nodestore.Options, error) {
	var g core.Genesis
	if err := loadJSON(c.GenesisFile, &g, 16<<20, false); err != nil {
		return spool.Options{}, nodestore.Options{}, err
	}
	if _, _, err := settings(c, &g); err != nil {
		return spool.Options{}, nodestore.Options{}, err
	}
	chain := execwire.Chain{ID: c.ChainID, Genesis: execwire.Hash(common.HexToHash(c.GenesisHash))}
	anchor := execwire.Ref{Generation: 1, Height: c.AnchorHeight, Hash: execwire.Hash(common.HexToHash(c.AnchorHash))}
	return spool.Options{Path: c.SpoolDir, Chain: chain, Anchor: anchor, MaxBytes: c.SpoolBytes, MaxRecords: c.SpoolRecords}, nodestore.Options{DataDir: c.DataDir, ReserveBytes: c.RetentionReserveBytes}, nil
}

// PlanRetention observes existing stores only. Owners must be stopped; these
// separately acquired observations do not authorize GC and are rechecked on apply.
func PlanRetention(ctx context.Context, c Config, keep uint64) (retention.Request, error) {
	source, node, e := retentionOptions(c)
	if e != nil {
		return retention.Request{}, e
	}
	st, e := nodestore.Inspect(ctx, node, source.Chain, source.Anchor)
	if e != nil {
		return retention.Request{}, e
	}
	if st.AnchorRoot != execwire.Hash(common.HexToHash(c.AnchorRoot)) {
		return retention.Request{}, retention.ErrUnsafe
	}
	return spool.InspectRetention(ctx, source, st.Head, st.Root, keep)
}
func applyRetention(ctx context.Context, c Config, p retention.Permit) (receipt retention.Receipt, err error) {
	source, node, err := retentionOptions(c)
	if err != nil {
		return receipt, err
	}
	if p.Validate() != nil || source.Chain != p.Request.Chain || source.Anchor != p.Request.Anchor {
		return receipt, retention.ErrUnsafe
	}
	store, err := spool.OpenMaintenance(ctx, source, p)
	if err != nil {
		return receipt, err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	return store.CollectRetention(ctx, p, func(ctx context.Context) error { return nodestore.Compact(ctx, node, p) })
}
func ReleaseRetention(ctx context.Context, c Config, p retention.Permit, release retention.Release) (err error) {
	source, node, err := retentionOptions(c)
	if err != nil {
		return err
	}

	store, err := spool.OpenMaintenance(ctx, source, p)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	r, err := store.RetentionCompletion(ctx, p)
	if err != nil {
		return err
	}
	if release.Match(p, r) != nil {
		return retention.ErrUnsafe
	}
	// The source process lock remains held while releasing the node fence.
	if err = nodestore.Release(ctx, node, p, r, release); err != nil {
		return err
	}
	return store.ReleaseRetention(ctx, p, release)
}

type RetentionCLI struct {
	Action, Config, Permit, Release, Output string
	Supervisor                              *os.File
	Keep                                    uint64
}

func RunRetention(ctx context.Context, o RetentionCLI, out interface{ Write([]byte) (int, error) }) error {
	if o.Action != "plan" && o.Action != "apply" && o.Action != "release" && o.Action != "inspect" && o.Action != "resume-check" {
		return fmt.Errorf("action must be plan, apply, release, inspect or resume-check")
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
		source, node, e := retentionOptions(c)
		if e != nil {
			return e
		}
		p, phase, ne := nodestore.Status(node)
		s, se := spool.InspectMetadata(ctx, source.Path)
		if ne != nil && !errors.Is(ne, os.ErrNotExist) {
			return ne
		}
		if se != nil {
			return se
		}
		return emit(struct {
			Permit retention.Permit
			Phase  string
			Source spool.MetadataObservation
			Scope  string
		}{p, phase, s, "separate read-only observations; no ACK/GC/start authorization"})
	}
	if o.Action == "resume-check" {
		if c.Capacity == nil {
			return fmt.Errorf("resume-check needs explicit producer capacity thresholds")
		}
		source, _, err := retentionOptions(c)
		if err != nil {
			return err
		}
		if err = spool.ResumeCapacity(ctx, source); err != nil {
			return err
		}
		if e = checkNodeCapacity(ctx, c, true); e != nil {
			return e
		}
		if e = nodestore.CheckStart(c.DataDir); e != nil {
			return e
		}
		s, e := spool.InspectMetadata(ctx, c.SpoolDir)
		if e != nil {
			return e
		}
		if s.TransitionPending || s.MaintenancePending {
			return spool.ErrRecovery
		}
		return emit(struct{ Status, Scope string }{"capacity_ready_observation", "no services started; normal Hello/backlog/fresh gates still required"})
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
			return fmt.Errorf("apply requires the live inherited DB supervisor pipe; use execution-retention collect")
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
