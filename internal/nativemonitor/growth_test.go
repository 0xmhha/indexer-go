package nativemonitor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func growthFiles(t *testing.T) (string, string, string, []string) {
	t.Helper()
	policy, current, args := files(t)
	previous := filepath.Join(filepath.Dir(current), "previous.json")
	p, s, at := fixture()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var policyJSON map[string]any
	if err = json.Unmarshal(data, &policyJSON); err != nil {
		t.Fatal(err)
	}
	id := sha256.Sum256([]byte("owned fixture DB/schema measurement incarnation"))
	series := hex.EncodeToString(id[:])
	policyJSON["indexer_growth"] = map[string]any{"series_id": series, "warn_within_seconds": 3600, "stop_within_seconds": 600, "min_interval_seconds": 60, "max_interval_seconds": 3600}
	write(t, policy, policyJSON)
	for _, sample := range []struct {
		path string
		age  time.Duration
		used uint64
	}{{current, 0, 200}, {previous, time.Minute, 100}} {
		when := at.Add(-sample.age)
		s.Metrics.ObservedAt = &when
		s.Metrics.Storage[1].Used = u(sample.used)
		data, err = json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		var snapshotJSON map[string]any
		if err = json.Unmarshal(data, &snapshotJSON); err != nil {
			t.Fatal(err)
		}
		metrics := snapshotJSON["metrics"].(map[string]any)
		metrics["storage"].([]any)[1].(map[string]any)["series_id"] = series
		write(t, sample.path, snapshotJSON)
	}
	return policy, current, previous, append(args, "--previous-snapshot", previous)
}

func TestIndexerGrowthCommandDetectsExhaustionBeforeCapacityWarning(t *testing.T) {
	_, _, _, args := growthFiles(t)
	var out bytes.Buffer
	code := Run(t.Context(), args, &out)
	var r Report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if code != 1 || r.AlertLevel != "critical" || !has(r, "growth.indexer.forecast", "stop_horizon_reached") {
		t.Fatalf("480s remaining must be critical under600s horizon despite20%% usage: exit%d %+v", code, r)
	}
	if r.OperationalReadiness != "not_authorized" || r.LiveFreshness != "not_performed" || r.Actuation != "none" {
		t.Fatal("growth observation granted operating authority")
	}
}

// Keep the explicit fixture bytes available for later no-mutation checks.
func growthInput(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
