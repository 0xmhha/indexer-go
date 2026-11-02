package nativeobserve

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func clock() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }

func catalog(t *testing.T, bindings ...Binding) *Catalog {
	t.Helper()
	c, a := NewCatalog(bindings)
	if !a.Usable() || c == nil {
		t.Fatalf("valid catalog: %+v", a)
	}
	return c
}

func TestNumberMissingZeroNegativeAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		in    string
		value uint64
		code  Code
	}{
		{"", 0, MissingValue}, {"0", 0, ""}, {"18446744073709551615", math.MaxUint64, ""},
		{"-1", 0, InvalidNumber}, {"+1", 0, InvalidNumber}, {"1.0", 0, InvalidNumber},
		{"1e3", 0, InvalidNumber}, {" 1", 0, InvalidNumber}, {"18446744073709551616", 0, InvalidNumber},
		{"999999999999999999999", 0, InvalidNumber},
	} {
		t.Run(tc.in, func(t *testing.T) {
			n, a := ParseNumber(tc.in)
			if tc.code != "" {
				requireCode(t, a, tc.code)
				if n.Present {
					t.Fatal("invalid input became known")
				}
				return
			}
			if !a.Usable() || !n.Present || n.Value != tc.value {
				t.Fatalf("number=%+v result=%+v", n, a)
			}
		})
	}
}

func TestCatalogCapsConflictsAndImmutableInput(t *testing.T) {
	b := databaseBinding()
	input := []Binding{b}
	c := catalog(t, input...)
	input[0].Budget.Limit.Value = 7
	o := sample(b, clock())
	if !c.Qualify(o, clock(), time.Minute).Usable() {
		t.Fatal("catalog retained input slice alias")
	}
	if o != sample(b, clock()) {
		t.Fatal("qualification mutated input")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Binding)
		code   Code
	}{
		{"same-key", func(*Binding) {}, DuplicateKey},
		{"budget", func(b *Binding) { b.Budget.Limit.Value++ }, ResourceConflict},
		{"definition", func(b *Binding) { b.Definition = PGDataAllocated }, ScopeMismatch},
		{"source", func(b *Binding) { b.Source.Incarnation++ }, ResourceConflict},
		{"resource-kind", func(b *Binding) { b.Scope.Kind = Schema }, ResourceConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			other := b
			tc.mutate(&other)
			invalid, a := NewCatalog([]Binding{b, other})
			requireCode(t, a, tc.code)
			if invalid != nil {
				t.Fatal("conflict admitted")
			}
		})
	}
	bindings := make([]Binding, MaxResources)
	for i := range bindings {
		bindings[i] = b
		bindings[i].Scope.ID = ResourceID{byte(i + 1)}
		bindings[i].Budget.Scope = bindings[i].Scope
	}
	_ = catalog(t, bindings...)
	extra := b
	extra.Scope.ID = ResourceID{255}
	extra.Budget.Scope = extra.Scope
	_, a := NewCatalog(append(bindings, extra))
	requireCode(t, a, LimitExceeded)
	_, a = NewCatalog(make([]Binding, MaxKeys+1))
	requireCode(t, a, LimitExceeded)
	_, a = NewCatalog(nil)
	requireCode(t, a, InvalidBinding)
}

func TestIndependentKnownErrorsAndComponentTime(t *testing.T) {
	b := databaseBinding()
	c := catalog(t, b)
	for _, tc := range []struct {
		name   string
		mutate func(*Observation)
		code   Code
	}{
		{"missing", func(o *Observation) { o.Number = Number{} }, MissingValue},
		{"hidden-value", func(o *Observation) { o.Number = Number{false, 8} }, InvalidNumber},
		{"future-even-unknown", func(o *Observation) {
			o.Number = Number{}
			o.Quality = Unknown
			o.AcquiredAt = clock().Add(time.Second)
		}, InvalidTime},
		{"reversed-acquisition", func(o *Observation) { o.StartedAt = clock().Add(time.Second) }, InvalidTime},
		{"event-in-future", func(o *Observation) { o.EventAt = clock().Add(time.Second) }, InvalidTime},
		{"missing-time", func(o *Observation) { o.AcquiredAt = time.Time{} }, MissingTime},
		{"stale", func(o *Observation) {
			o.AcquiredAt = clock().Add(-time.Minute - time.Nanosecond)
			o.StartedAt = o.AcquiredAt
		}, SampleStale},
		{"known-error", func(o *Observation) { o.Number = Number{}; o.Quality = Failed; o.Cause = PermissionDenied }, SampleFailed},
		{"bad-quality", func(o *Observation) { o.Quality = 255 }, InvalidQuality},
		{"bad-cause", func(o *Observation) { o.Cause = 255 }, InvalidQuality},
		{"lower-bound", func(o *Observation) { o.Coverage = LowerBound }, InexactValue},
		{"truncated", func(o *Observation) { o.Coverage = Truncated }, InexactValue},
		{"unverified", func(o *Observation) { o.Coverage = Unverified }, InexactValue},
		{"exceeded", func(o *Observation) { o.Number.Value = 1001 }, BudgetExceeded},
		{"sequence", func(o *Observation) { o.Sequence = 0 }, InvalidSequence},
		{"source-replaced", func(o *Observation) { o.Binding.Source.Token[0]++ }, UnboundSample},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := sample(b, clock())
			tc.mutate(&o)
			before := o
			requireCode(t, c.Qualify(o, clock(), time.Minute), tc.code)
			if o != before {
				t.Fatal("sample mutated")
			}
		})
	}
	boundary := sample(b, clock().Add(-time.Minute))
	if !c.Qualify(boundary, clock(), time.Minute).Usable() {
		t.Fatal("inclusive max age rejected")
	}
	// A missing budget cannot conceal a source failure or a known future time.
	b.Budget.Limit = Number{}
	c = catalog(t, b)
	o := sample(b, clock().Add(time.Second))
	o.Number = Number{}
	o.Quality = Failed
	o.Cause = TimedOut
	a := c.Qualify(o, clock(), time.Minute)
	for _, code := range []Code{BudgetMissing, SampleFailed, InvalidTime} {
		requireCode(t, a, code)
	}
	var absent *Catalog
	requireCode(t, absent.Qualify(o, clock(), time.Minute), UnboundSample)
	for _, maxAge := range []time.Duration{0, -time.Second, 24*time.Hour + time.Nanosecond} {
		requireCode(t, c.Qualify(o, clock(), maxAge), InvalidTime)
	}
}

func positionBinding(metric Metric) Binding {
	b := databaseBinding()
	k, d, u := measurement(metric)
	b.Scope.Kind, b.Metric, b.Unit, b.Definition, b.Budget = k, metric, u, d, Budget{}
	b.Chain = Chain{ID: 5042002, Genesis: Hash{1}, AnchorHash: Hash{1}}
	switch metric {
	case RawDurableHeight, RawBytes:
		b.Epochs = Epochs{Receiver: 1, Raw: 2}
	case NativeExecutedHeight, NativeBytes:
		b.Epochs = Epochs{Receiver: 1, Native: 3}
	case SQLVisibleHeight, SourceACKHeight, SourceBytes:
		b.Epochs = Epochs{Receiver: 1, Native: 3, Read: 4}
	case CaptureReceivedHeight:
		b.Epochs = Epochs{Capture: 5}
	}
	if budgeted(metric) {
		b.Budget = Budget{b.Scope, b.Definition, Digest{1}, Number{true, 1000}}
	}
	return b
}

func TestPositionDomainsAndSeededACKDoNotInventProgress(t *testing.T) {
	for _, m := range []Metric{RawDurableHeight, NativeExecutedHeight, SQLVisibleHeight, SourceACKHeight, CaptureReceivedHeight} {
		b := positionBinding(m)
		c := catalog(t, b)
		o := sample(b, clock())
		o.Position = Position{Hash{9}, b.Epochs}
		if !c.Qualify(o, clock(), time.Minute).Usable() {
			t.Fatalf("valid position %v rejected", m)
		}
		if !o.EventAt.IsZero() {
			t.Fatal("seeded ACK gained a progress time")
		}
		o.Position.Capture = 1
		if m == CaptureReceivedHeight {
			o.Position.Receiver = 1
		}
		requireCode(t, c.Qualify(o, clock(), time.Minute), InvalidPosition)
		o.Position = Position{Hash{9}, b.Epochs}
		o.Coverage = LowerBound
		requireCode(t, c.Qualify(o, clock(), time.Minute), InexactValue)
	}
	// Types cannot be assigned interchangeably without an explicit conversion.
	if reflect.TypeFor[ReceiverGeneration]().AssignableTo(reflect.TypeFor[CaptureGeneration]()) || reflect.TypeFor[RawEpoch]().AssignableTo(reflect.TypeFor[NativeEpoch]()) || reflect.TypeFor[NativeEpoch]().AssignableTo(reflect.TypeFor[ReadEpoch]()) {
		t.Fatal("position domain types collapsed")
	}
	b := positionBinding(RawDurableHeight)
	b.Epochs.Capture = 1
	_, a := NewCatalog([]Binding{b})
	requireCode(t, a, InvalidPosition)
}

func TestMeasurementScopesUnitsAndUnsupportedSchema(t *testing.T) {
	for m := DatabaseBytes; m <= CaptureReceivedHeight; m++ {
		b := positionBinding(m)
		if m < RawDurableHeight && m != RawBytes && m != NativeBytes && m != SourceBytes {
			b.Chain, b.Epochs = Chain{}, Epochs{}
		}
		c := catalog(t, b)
		o := sample(b, clock())
		if b.Unit == Blocks {
			o.Position = Position{Hash{9}, b.Epochs}
		}
		a := c.Qualify(o, clock(), time.Minute)
		if m == SchemaBytes {
			requireCode(t, a, SampleUnsupported)
		} else if !a.Usable() {
			t.Fatalf("metric %d: %+v", m, a)
		}
		b.Unit = 255
		_, a = NewCatalog([]Binding{b})
		requireCode(t, a, ScopeMismatch)
	}
	b := databaseBinding()
	b.Budget.Limit = Number{true, 0}
	b.Series = Digest{}
	_, a := NewCatalog([]Binding{b})
	requireCode(t, a, InvalidNumber)
	requireCode(t, a, InvalidBinding)
}
