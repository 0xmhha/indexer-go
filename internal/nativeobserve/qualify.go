package nativeobserve

import (
	"math"
	"strconv"
	"time"
)

func (a *Assessment) add(code Code) {
	for _, c := range a.Codes {
		if c == code {
			return
		}
	}
	a.Codes = append(a.Codes, code)
}

// ParseNumber accepts only nonnegative decimal integers. Empty means missing,
// not zero. Fixed codes never expose driver errors or the rejected input.
func ParseNumber(s string) (Number, Assessment) {
	if s == "" {
		return Number{}, Assessment{Codes: []Code{MissingValue}}
	}
	if len(s) > 20 {
		return Number{}, Assessment{Codes: []Code{InvalidNumber}}
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return Number{}, Assessment{Codes: []Code{InvalidNumber}}
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return Number{}, Assessment{Codes: []Code{InvalidNumber}}
	}
	return Number{true, n}, Assessment{}
}

func measurement(m Metric) (ResourceKind, Definition, Unit) {
	switch m {
	case DatabaseBytes:
		return Database, DatabaseLogical, Bytes
	case WALBytes:
		return Cluster, ClusterWALLogical, Bytes
	case PGDataBytes:
		return PGData, PGDataAllocated, Bytes
	case SchemaBytes:
		return Schema, SchemaLogical, Bytes
	case RawBytes:
		return RawWindow, RawRetained, Bytes
	case NativeBytes:
		return NativeWindow, NativeRetained, Bytes
	case SourceBytes:
		return SourceSpool, SourceRetained, Bytes
	case FreeBytes:
		return Filesystem, FilesystemAvailable, Bytes
	case RawDurableHeight:
		return NativePipeline, RawDurable, Blocks
	case NativeExecutedHeight:
		return NativePipeline, NativeExecuted, Blocks
	case SQLVisibleHeight:
		return NativePipeline, SQLVisible, Blocks
	case SourceACKHeight:
		return NativePipeline, DurableSourceACK, Blocks
	case CaptureReceivedHeight:
		return LegacyCapture, CaptureReceived, Blocks
	default:
		return 0, 0, 0
	}
}

func budgeted(m Metric) bool { return m >= DatabaseBytes && m <= SourceBytes }

func validateBinding(b Binding, a *Assessment) {
	kind, definition, unit := measurement(b.Metric)
	if kind == 0 || b.Scope.ID == (ResourceID{}) || b.Source.Token == (SourceToken{}) ||
		b.Source.Incarnation == 0 || b.Source.Run == (RunID{}) || b.Series == (Digest{}) || b.Policy == (Digest{}) {
		a.add(InvalidBinding)
	}
	if b.Scope.Kind != kind || b.Definition != definition || b.Unit != unit {
		a.add(ScopeMismatch)
	}
	chainBound := kind == RawWindow || kind == NativeWindow || kind == SourceSpool || kind == NativePipeline || kind == LegacyCapture
	if chainBound {
		if b.Chain.ID == 0 || b.Chain.AnchorHeight > math.MaxInt64 || b.Chain.Genesis == (Hash{}) || b.Chain.AnchorHash == (Hash{}) ||
			b.Chain.AnchorHeight == 0 && b.Chain.AnchorHash != b.Chain.Genesis {
			a.add(InvalidBinding)
		}
	} else if b.Chain != (Chain{}) {
		// Whole-database/cluster/filesystem usage is not a chain allocation.
		a.add(ScopeMismatch)
	}
	if !validEpochs(b) {
		a.add(InvalidPosition)
	}
	if budgeted(b.Metric) {
		if b.Budget.Scope != b.Scope || b.Budget.Definition != b.Definition {
			a.add(ScopeMismatch)
		}
		if b.Budget.ID == (Digest{}) {
			a.add(InvalidBinding)
		}
		if b.Budget.Limit.Present && b.Budget.Limit.Value == 0 || !b.Budget.Limit.Present && b.Budget.Limit.Value != 0 {
			a.add(InvalidNumber)
		}
	} else if b.Budget != (Budget{}) {
		a.add(ScopeMismatch)
	}
}

// NewCatalog copies its bounded input and rejects ambiguity instead of deduping
// declarations by a database name or treating global bytes as schema bytes.
func NewCatalog(bindings []Binding) (*Catalog, Assessment) {
	var a Assessment
	if len(bindings) == 0 {
		a.add(InvalidBinding)
	}
	if len(bindings) > MaxKeys {
		a.add(LimitExceeded)
		return nil, a
	}
	resources := make(map[ResourceID]Binding)
	keys := make(map[struct {
		ID     ResourceID
		Metric Metric
	}]Binding)
	for _, b := range bindings {
		validateBinding(b, &a)
		if before, ok := resources[b.Scope.ID]; ok {
			if before.Scope != b.Scope || before.Source != b.Source || before.Chain != b.Chain ||
				before.Series != b.Series || before.Policy != b.Policy || before.Budget != b.Budget {
				a.add(ResourceConflict)
			}
		} else {
			resources[b.Scope.ID] = b
		}
		key := struct {
			ID     ResourceID
			Metric Metric
		}{b.Scope.ID, b.Metric}
		if before, ok := keys[key]; ok {
			a.add(DuplicateKey)
			if before != b {
				a.add(ResourceConflict)
			}
		}
		keys[key] = b
	}
	if len(resources) > MaxResources {
		a.add(LimitExceeded)
	}
	if !a.Usable() {
		return nil, a
	}
	return &Catalog{bindings: append([]Binding(nil), bindings...)}, a
}

func (c *Catalog) Qualify(o Observation, at time.Time, maxAge time.Duration) Assessment {
	var a Assessment
	validateBinding(o.Binding, &a)
	bound := false
	if c != nil {
		for _, b := range c.bindings {
			if b == o.Binding {
				bound = true
				break
			}
		}
	}
	if !bound {
		a.add(UnboundSample)
	}
	qualifyValue(o, at, maxAge, false, &a)
	return a
}

func qualifyValue(o Observation, at time.Time, maxAge time.Duration, historical bool, a *Assessment) {
	if o.Quality > Unsupported || o.Cause > SourceUnavailable ||
		o.Quality != Known && o.Quality != Stale && o.Number.Present || o.Quality == Known && o.Cause != NoCause ||
		o.Quality == Failed && o.Cause == NoCause {
		a.add(InvalidQuality)
	}
	if !o.Number.Present {
		a.add(MissingValue)
	}
	if !o.Number.Present && o.Number.Value != 0 {
		a.add(InvalidNumber)
	}
	switch o.Quality {
	case Unknown:
		a.add(MissingValue)
	case Failed:
		a.add(SampleFailed)
	case Stale:
		a.add(SampleStale)
	case Unsupported:
		a.add(SampleUnsupported)
	}
	if o.Binding.Metric == SchemaBytes {
		a.add(SampleUnsupported)
	}
	if o.Coverage != Exact {
		a.add(InexactValue)
	}
	if o.Sequence == 0 {
		a.add(InvalidSequence)
	}
	if at.IsZero() || o.StartedAt.IsZero() || o.AcquiredAt.IsZero() {
		a.add(MissingTime)
	}
	if maxAge <= 0 || maxAge > 24*time.Hour ||
		!o.StartedAt.IsZero() && !o.AcquiredAt.IsZero() && o.StartedAt.After(o.AcquiredAt) ||
		!at.IsZero() && (o.StartedAt.After(at) || o.AcquiredAt.After(at) || o.EventAt.After(at)) ||
		!o.EventAt.IsZero() && !o.AcquiredAt.IsZero() && o.EventAt.After(o.AcquiredAt) {
		a.add(InvalidTime)
	} else if !historical && !at.IsZero() && !o.AcquiredAt.IsZero() && at.Sub(o.AcquiredAt) > maxAge {
		a.add(SampleStale)
	}
	if budgeted(o.Binding.Metric) {
		if !o.Binding.Budget.Limit.Present {
			a.add(BudgetMissing)
		}
		if o.Number.Present && o.Binding.Budget.Limit.Present && o.Number.Value > o.Binding.Budget.Limit.Value {
			a.add(BudgetExceeded)
		}
	}
	if o.Binding.Unit == Blocks && o.Number.Present && o.Number.Value > math.MaxInt64 {
		a.add(InvalidNumber)
	}
	if o.Binding.Unit == Blocks && o.Number.Present && (o.Number.Value < o.Binding.Chain.AnchorHeight ||
		o.Number.Value == o.Binding.Chain.AnchorHeight && o.Position.Hash != (Hash{}) && o.Position.Hash != o.Binding.Chain.AnchorHash) {
		a.add(InvalidPosition)
	}
	if o.Number.Present || o.Position != (Position{}) {
		qualifyPosition(o.Binding, o.Position, a)
	}
}

func validEpochs(b Binding) bool {
	p := b.Epochs
	if uint64(p.Receiver) > math.MaxInt64 || uint64(p.Capture) > math.MaxInt64 ||
		uint64(p.Raw) > math.MaxInt64 || uint64(p.Native) > math.MaxInt64 || uint64(p.Read) > math.MaxInt64 {
		return false
	}
	if b.Scope.Kind == LegacyCapture {
		return p.Capture > 0 && p.Receiver == 0 && p.Raw == 0 && p.Native == 0 && p.Read == 0
	}
	if b.Scope.Kind != NativePipeline && b.Scope.Kind != RawWindow && b.Scope.Kind != NativeWindow && b.Scope.Kind != SourceSpool {
		return p == (Epochs{})
	}
	if p.Receiver == 0 || p.Capture != 0 {
		return false
	}
	switch b.Metric {
	case RawDurableHeight, RawBytes:
		return p.Raw > 0 && p.Native == 0 && p.Read == 0
	case NativeExecutedHeight, NativeBytes:
		return p.Raw == 0 && p.Native > 0 && p.Read == 0
	case SQLVisibleHeight, SourceACKHeight, SourceBytes:
		return p.Raw == 0 && p.Native > 0 && p.Read > 0
	default:
		return false
	}
}

func qualifyPosition(b Binding, p Position, a *Assessment) {
	if b.Unit != Blocks {
		if p != (Position{}) {
			a.add(InvalidPosition)
		}
		return
	}
	if p.Hash == (Hash{}) {
		a.add(PositionIncomplete)
	}
	actual := []uint64{uint64(p.Receiver), uint64(p.Capture), uint64(p.Raw), uint64(p.Native), uint64(p.Read)}
	expected := []uint64{uint64(b.Epochs.Receiver), uint64(b.Epochs.Capture), uint64(b.Epochs.Raw), uint64(b.Epochs.Native), uint64(b.Epochs.Read)}
	for n, v := range actual {
		if v == 0 && expected[n] != 0 {
			a.add(PositionIncomplete)
		} else if v != expected[n] {
			a.add(InvalidPosition)
		}
	}
}
