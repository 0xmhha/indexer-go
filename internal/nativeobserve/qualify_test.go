package nativeobserve

import (
	"slices"
	"testing"
	"time"
)

func databaseBinding() Binding {
	scope := Scope{Database, ResourceID{1}}
	return Binding{Scope: scope, Metric: DatabaseBytes, Unit: Bytes, Definition: DatabaseLogical,
		Source: Source{SourceToken{1}, 1, RunID{1}}, Series: Digest{1}, Policy: Digest{1},
		Budget: Budget{scope, DatabaseLogical, Digest{1}, Number{true, 1000}}}
}

func sample(b Binding, at time.Time) Observation {
	o := Observation{Binding: b, Number: Number{true, 0}, Quality: Known, Coverage: Exact,
		Sequence: 1, StartedAt: at.Add(-time.Second), AcquiredAt: at}
	if b.Unit == Blocks {
		o.Number.Value = 1
	}
	return o
}

func requireCode(t *testing.T, a Assessment, code Code) {
	t.Helper()
	if !slices.Contains(a.Codes, code) {
		t.Fatalf("want %s, got %+v", code, a)
	}
}

func TestDatabaseCannotBindToPerChainSchemaBudgets(t *testing.T) {
	a := databaseBinding()
	a.Budget.Scope = Scope{Schema, ResourceID{2}}
	b := a
	b.Chain = Chain{ID: 2, Genesis: Hash{2}, AnchorHash: Hash{2}}
	b.Budget.Scope.ID = ResourceID{3}
	c, result := NewCatalog([]Binding{a, b})
	requireCode(t, result, ScopeMismatch)
	if c != nil {
		t.Fatal("invalid global-to-schema binding admitted")
	}
}

func TestEachComponentRetainsItsOwnFreshness(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	db := databaseBinding()
	wal := db
	wal.Scope = Scope{Cluster, ResourceID{2}}
	wal.Metric, wal.Definition = WALBytes, ClusterWALLogical
	wal.Budget.Scope, wal.Budget.Definition = wal.Scope, wal.Definition
	c, result := NewCatalog([]Binding{db, wal})
	if !result.Usable() {
		t.Fatal(result)
	}
	if got := c.Qualify(sample(db, now), now, time.Minute); !got.Usable() {
		t.Fatal(got)
	}
	requireCode(t, c.Qualify(sample(wal, now.Add(-2*time.Minute)), now, time.Minute), SampleStale)
}
