package nativemonitor

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func files(t *testing.T) (string, string, []string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, s, at := fixture()
	policy, snapshot := filepath.Join(dir, "policy.json"), filepath.Join(dir, "snapshot.json")
	write(t, policy, p)
	write(t, snapshot, s)
	return policy, snapshot, []string{"--policy", policy, "--snapshot", snapshot, "--at", at.Format(time.RFC3339Nano)}
}

func TestMonitorFilesStatusesPrivacyAndInputPreservation(t *testing.T) {
	policy, snapshot, args := files(t)
	before := map[string][]byte{}
	info := map[string]os.FileInfo{}
	for _, path := range []string{policy, snapshot} {
		var err error
		before[path], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		info[path], err = os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if code := Run(t.Context(), args, &out); code != 2 {
		t.Fatalf("unexpected exit: %d %s", code, &out)
	}
	for _, secret := range []string{policy, snapshot, "fixture-native", "original_anchor", "observed_at"} {
		if strings.Contains(out.String(), secret) {
			t.Fatal("input echoed", secret)
		}
	}
	for _, path := range []string{policy, snapshot} {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, before[path]) {
			t.Fatal("input bytes modified")
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(info[path], after) {
			t.Fatal("input inode replaced")
		}
	}
	entries, err := os.ReadDir(filepath.Dir(policy))
	if err != nil || len(entries) != 2 {
		t.Fatal("monitor created state")
	}
	_, s, _ := fixture()
	s.Diagnostics.Report.Status = "FAIL"
	s.Diagnostics.Report.Findings = []Finding{{Field: "private/password", Status: "FAIL", Code: "postgres://private-token"}}
	write(t, snapshot, s)
	out.Reset()
	if code := Run(t.Context(), args, &out); code != 1 || strings.Contains(out.String(), "private") {
		t.Fatalf("failed report leaked or was hidden: %d %s", code, &out)
	}
	for _, a := range [][]string{nil, {"--policy", "HOLD", "--snapshot", "HOLD", "--at", "HOLD"}} {
		if Run(t.Context(), a, &out) != 2 {
			t.Fatal("unknown must HOLD")
		}
	}
}

func TestMonitorStrictInputAndUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"unknown", "duplicate", "scalar-null", "trailing", "oversize", "public", "hardlink", "leaf-link", "parent-link", "directory", "missing", "zero-version", "invalid-clock", "duplicate-flag", "positional", "dangling"} {
		t.Run(kind, func(t *testing.T) {
			policy, snapshot, args := files(t)
			data, err := os.ReadFile(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "unknown":
				data = append([]byte(`{"extra":"opaque",`), data[1:]...)
			case "duplicate":
				data = append([]byte(`{"Version":1,`), data[1:]...)
			case "scalar-null":
				data = []byte(`{"version":null}`)
			case "trailing":
				data = append(data, []byte(` {}`)...)
			case "oversize":
				data = bytes.Repeat([]byte(" "), (1<<20)+1)
			case "public":
				if err = os.Chmod(snapshot, 0644); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err = os.Link(snapshot, snapshot+".linked"); err != nil {
					t.Fatal(err)
				}
			case "leaf-link":
				link := snapshot + ".linked"
				if err = os.Symlink(snapshot, link); err != nil {
					t.Fatal(err)
				}
				args[3] = link
			case "parent-link":
				link := filepath.Join(filepath.Dir(policy), "link")
				if err = os.Symlink(filepath.Dir(policy), link); err != nil {
					t.Fatal(err)
				}
				args[3] = filepath.Join(link, "snapshot.json")
			case "directory":
				args[3] = filepath.Dir(snapshot)
			case "missing":
				args[3] += ".absent"
			case "zero-version":
				data = []byte(`{}`)
			case "invalid-clock":
				args[5] = "secret-invalid-clock"
			case "duplicate-flag":
				args = []string{"--policy", policy, "-policy", policy}
			case "positional":
				args = append(args, "secret-positional")
			case "dangling":
				args = []string{"--policy"}
			}
			if kind == "unknown" || kind == "duplicate" || kind == "scalar-null" || kind == "trailing" || kind == "oversize" || kind == "zero-version" {
				if err = os.WriteFile(snapshot, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			if code := Run(t.Context(), args, &out); code != 1 || strings.Contains(out.String(), "secret") {
				t.Fatalf("unsafe input accepted or leaked: %d %s", code, &out)
			}
		})
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestMonitorCancellationAndFailedOutput(t *testing.T) {
	_, _, args := files(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out bytes.Buffer
	if Run(ctx, args, &out) != 1 {
		t.Fatal("cancellation ignored")
	}
	if Run(t.Context(), args, shortWriter{}) != 1 {
		t.Fatal("short output ignored")
	}
	if Run(t.Context(), []string{"--help"}, shortWriter{}) != 1 {
		t.Fatal("short help ignored")
	}
	if Run(t.Context(), []string{"--help"}, &out) != 0 {
		t.Fatal("help failed")
	}
}

func TestMonitoringDraftsRemainUnknownWithoutInventedDefaults(t *testing.T) {
	policy, snapshot, args := files(t)
	for _, item := range []struct{ source, target string }{{"policy.draft.json", policy}, {"snapshot.draft.json", snapshot}} {
		data, err := os.ReadFile(filepath.Join("..", "..", "deployments", "native", "monitoring", item.source))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(item.target, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	args[5] = "HOLD"
	var out bytes.Buffer
	if code := Run(t.Context(), args, &out); code != 2 {
		t.Fatalf("draft must HOLD: %d %s", code, &out)
	}
	var r Report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil || r.Status != "HOLD" || r.AlertLevel != "unknown" || !has(r, "policy.identity", "not_observed") {
		t.Fatal(out.String())
	}
}
