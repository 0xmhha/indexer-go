// Package indexingstatus combines two read-only DB snapshots. Completion is
// reported against the explicit capture generation, never a fresh network tip.
package indexingstatus

import (
	"context"
	"errors"

	"github.com/0xmhha/indexer-go/internal/feedtelemetry"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

var ErrUnavailable = errors.New("indexing status is unavailable")

type Dataset struct {
	Kind           model.DatasetKind `json:"dataset"`
	Coverage       string            `json:"coverage"`
	Representation string            `json:"representation"`
	SelectionHash  string            `json:"selectionHash,omitempty"`
}
type SourceStatus struct {
	SourceSearch   *model.SourceSearchDescriptor `json:"sourceSearch,omitempty"`
	SourceLookup   *model.SourceLookupDescriptor `json:"sourceLookup,omitempty"`
	BlobCapability *model.BlobCapability         `json:"blobCapability"`
	PlanDigest     common.Hash                   `json:"planDigest"`
	Chain          model.FeedChain               `json:"chain"`
	Generation     uint64                        `json:"generation"`
	Anchor         model.FeedBlock               `json:"anchor"`
	Through        *model.BlockPoint             `json:"factsThrough"`
	Finalized      *model.BlockPoint             `json:"finalized"`
	PlanVersion    uint32                        `json:"planVersion"`
	Datasets       []Dataset                     `json:"datasets"`
}
type Unit struct {
	Name       string           `json:"name"`
	Active     bool             `json:"active"`
	Definition string           `json:"definition"`
	Gap        *port.BlockRange `json:"gap"`
	capture.UnitStatus
}
type CallTraceStatus struct {
	capture.SupplementStatus
	Verification string `json:"verification"`
	Coverage     string `json:"coverage"`
}

type Status struct {
	CallTraces          *CallTraceStatus               `json:"callTraces,omitempty"`
	Pipeline            *PipelineStatus                `json:"pipeline,omitempty"`
	Version             int                            `json:"version"`
	Mode                string                         `json:"mode"`
	Source              *SourceStatus                  `json:"source"`
	Projection          *model.BlockPoint              `json:"projectionHead"`
	ProjectionCanonical bool                           `json:"projectionMatchesCapture"`
	Units               []Unit                         `json:"units"`
	Workers             []capture.WorkerStatus         `json:"workers"`
	StateProofs         *capture.SupplementStatus      `json:"stateProofs,omitempty"`
	StateChanges        *capture.StateProjectionStatus `json:"stateChanges,omitempty"`
	Next                string                         `json:"next"`
}

func Read(ctx context.Context, store any, after string, limit int) (out Status, err error) {
	out = Status{Version: 1, Mode: "unreported", Units: []Unit{}, Workers: []capture.WorkerStatus{}}
	reader, ok := store.(port.IndexingReader)
	if !ok {
		return out, ErrUnavailable
	}
	page, e := reader.ReadIndexingPage(ctx, after, limit)
	if e != nil {
		return out, e
	}
	out.Next = page.Next
	if page.Descriptor == nil {
		for _, u := range page.Units {
			out.Units = append(out.Units, Unit{Name: u.Name, Active: u.State.Active, Definition: u.State.Definition, Gap: u.State.Gap, UnitStatus: capture.UnitStatus{Status: "unreported"}})
		}
		return out, nil
	}
	db, ok := store.(port.CaptureDatabase)
	if !ok {
		return out, ErrUnavailable
	}
	cap, e := capture.ReadStatus(ctx, db, *page.Descriptor, page)
	if e != nil {
		return out, e
	}
	out.Mode = "captured_facts"
	st := cap.State
	src := &SourceStatus{Chain: st.Anchor.Chain, Generation: st.Generation, Anchor: st.Anchor, PlanVersion: st.Plan.Version, Datasets: coverage(st.Plan)}
	if page.Descriptor.SourceSearch != nil {
		copy := *page.Descriptor.SourceSearch
		src.SourceSearch = &copy
	}
	if page.Descriptor.SourceLookup != nil {
		copy := *page.Descriptor.SourceLookup
		src.SourceLookup = &copy
	}
	if page.Descriptor.BlobCapability != nil {
		copy := *page.Descriptor.BlobCapability
		src.BlobCapability = &copy
	}
	if st.Through.Number > st.Anchor.Number {
		src.Through = &model.BlockPoint{Height: st.Through.Number, Hash: st.Through.Hash}
	}
	if st.Finalized != nil {
		src.Finalized = &model.BlockPoint{Height: st.Finalized.Number, Hash: st.Finalized.Hash}
	}
	src.PlanDigest, _ = st.Plan.Digest()
	out.Source = src
	out.Pipeline = readPipeline(cap, *page.Descriptor, feedtelemetry.Read(page.Descriptor.Telemetry, st.Anchor, src.PlanDigest))
	out.Projection = page.Head
	out.ProjectionCanonical = cap.HeadCanonical
	out.Workers = cap.Workers
	if cap.CallTraces != nil {
		out.CallTraces = &CallTraceStatus{SupplementStatus: *cap.CallTraces, Verification: "provider_reported_root_consistency", Coverage: "reported_call_tree"}
	}
	out.StateProofs = cap.StateProofs
	out.StateChanges = cap.StateChanges
	for _, u := range page.Units {
		out.Units = append(out.Units, Unit{Name: u.Name, Active: u.State.Active, Definition: u.State.Definition, Gap: u.State.Gap, UnitStatus: cap.Units[u.Name]})
	}
	return out, nil
}
func coverage(p model.CapturePlan) []Dataset {
	requirements := map[model.DatasetKind]model.DatasetCoverage{}
	for _, r := range p.Required {
		requirements[r.Kind] = r.Coverage
	}
	out := []Dataset{}
	for _, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetTransactions, model.DatasetReceipts, model.DatasetLogs, model.DatasetAccounts, model.DatasetCode, model.DatasetStorage, model.DatasetTraces, model.DatasetBlobs, model.DatasetStateInputs} {
		d := Dataset{Kind: k, Coverage: "unavailable", Representation: "none"}
		if c, ok := requirements[k]; ok {
			d.Coverage = c.Scope
			d.Representation = "encoded"
			if c.Scope == model.CoverageTracked {
				d.SelectionHash = c.SelectionHash.Hex()
			}
		} else if k == model.DatasetTransactions && requirements[model.DatasetBodies].Scope == model.CoverageComplete || k == model.DatasetLogs && requirements[model.DatasetReceipts].Scope == model.CoverageComplete {
			d.Coverage = model.CoverageComplete
			d.Representation = "derived_from_committed_inputs"
		}
		out = append(out, d)
	}
	return out
}

type WorkerResult struct {
	Epoch  uint64               `json:"epoch"`
	Output capture.WorkerOutput `json:"output"`
}

func ReadOutput(ctx context.Context, store any, name string, height uint64) (out WorkerResult, err error) {
	r, ok := store.(port.IndexingReader)
	if !ok {
		return out, ErrUnavailable
	}
	db, ok := store.(port.CaptureDatabase)
	if !ok {
		return out, ErrUnavailable
	}
	page, e := r.ReadIndexingPage(ctx, "", 1)
	if e != nil {
		return out, e
	}
	if page.Descriptor == nil {
		return out, ErrUnavailable
	}
	out.Output, out.Epoch, err = capture.ReadWorkerOutput(ctx, db, *page.Descriptor, name, height)
	return out, err
}
