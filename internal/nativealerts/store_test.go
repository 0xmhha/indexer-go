//go:build linux || darwin

package nativealerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func privateStore(t *testing.T) (*FileStore, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dir
}
func storedCritical(t *testing.T, s *FileStore) Snapshot {
	t.Helper()
	e := engine(t)
	apply(t, e, 1, 99)
	snapshot := e.Snapshot()
	snapshot.Deliveries = nil
	if err := s.Write(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestCheckpointRejectsMissingCriticalLevel(t *testing.T) {
	s, dir := privateStore(t)
	snapshot := storedCritical(t, s)
	data, _ := json.Marshal(snapshot)
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	delete(document["Records"].([]any)[0].(map[string]any), "Active")
	bad, _ := json.Marshal(document)
	if err := os.WriteFile(filepath.Join(dir, checkpointName), bad, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(context.Background()); err == nil {
		t.Fatal("missing active critical level became default normal")
	}
}

func TestCheckpointRejectsTruncatedIdentityArray(t *testing.T) {
	s, dir := privateStore(t)
	snapshot := storedCritical(t, s)
	data, _ := json.Marshal(snapshot)
	var document map[string]any
	_ = json.Unmarshal(data, &document)
	record := document["Records"].([]any)[0].(map[string]any)
	record["Incarnation"] = []any{float64(1)}
	bad, _ := json.Marshal(document)
	if err := os.WriteFile(filepath.Join(dir, checkpointName), bad, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(context.Background()); err == nil {
		t.Fatal("short identity array silently padded")
	}
}

func TestCheckpointRejectsSpecialPermissionBits(t *testing.T) {
	s, dir := privateStore(t)
	storedCritical(t, s)
	path := filepath.Join(dir, checkpointName)
	if err := os.Chmod(path, 0600|os.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(context.Background()); err == nil {
		t.Fatal("non-private special mode admitted")
	}
}

func TestCheckpointRoundTripsNonHourTimezone(t *testing.T) {
	s, _ := privateStore(t)
	e := engine(t)
	in := signal(1, 99)
	in.ObservedAt = in.ObservedAt.In(time.FixedZone("fixture", 20700))
	if err := e.Apply(rules().Key, in, in.ObservedAt); err != nil {
		t.Fatal(err)
	}
	if err := e.Persist(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(context.Background()); err != nil {
		t.Fatal("valid checkpoint lost across time location decoding")
	}
}

func TestCheckpointAtomicFailureCancellationAndReopen(t *testing.T) {
	s, dir := privateStore(t)
	e := engine(t)
	apply(t, e, 1, 99)
	if err := e.Persist(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, checkpointName))
	if err != nil {
		t.Fatal(err)
	}
	apply(t, e, 2, 0)
	apply(t, e, 3, 0)
	s.fault = func(step string) error {
		if step == "prepared" {
			return errors.New("owned injected sync fault")
		}
		return nil
	}
	if e.Persist(context.Background(), s) == nil {
		t.Fatal("failed save claimed success")
	}
	after, _ := os.ReadFile(filepath.Join(dir, checkpointName))
	if !bytes.Equal(before, after) {
		t.Fatal("pre-publication failure changed checkpoint")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatal("owned temporary not removed")
	}
	s.fault = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e.Persist(ctx, s) == nil {
		t.Fatal("cancelled persistence succeeded")
	}
	if err := e.Persist(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	reopened, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	next := engine(t)
	if err := next.Load(context.Background(), reopened); err != nil {
		t.Fatal(err)
	}
	st, _ := next.State(rules().Key)
	if st.Active != Normal || st.Quality != Reset || st.Sequence != 0 || st.Count != 0 {
		t.Fatal("temporal baseline persisted")
	}
}

func TestPrivateCheckpointHasSingleOwner(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan *FileStore, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { <-start; s, _ := OpenFileStore(dir); results <- s })
	}
	close(start)
	wg.Wait()
	close(results)
	var owner *FileStore
	count := 0
	for s := range results {
		if s != nil {
			count++
			if owner != nil {
				_ = s.Close()
			} else {
				owner = s
			}
		}
	}
	if owner != nil {
		defer func() { _ = owner.Close() }()
	}
	if count != 1 {
		t.Fatalf("expected exactly one directory owner, got %d", count)
	}
	_ = owner.Close()
	next, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal("owner release did not unlock directory")
	}
	_ = next.Close()
}

func TestCheckpointRejectsLinksPermissionsSizeAndCorruption(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "mode", "oversize", "corrupt", "duplicate", "unknown", "trailing", "null"} {
		t.Run(kind, func(t *testing.T) {
			s, dir := privateStore(t)
			snapshot := storedCritical(t, s)
			path := filepath.Join(dir, checkpointName)
			original, _ := os.ReadFile(path)
			switch kind {
			case "symlink":
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".original", path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, path+".linked"); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				f, err := os.OpenFile(path, os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				err = f.Truncate(MaxCheckpointBytes + 1)
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
			default:
				bad := []byte("{")
				if kind == "duplicate" {
					bad = bytes.Replace(original, []byte(`"Version":1`), []byte(`"Version":1,"version":1`), 1)
				}
				if kind == "unknown" {
					bad = bytes.Replace(original, []byte(`"Version":1`), []byte(`"Version":1,"extra":1`), 1)
				}
				if kind == "trailing" {
					bad = append(original, []byte(" {}")...)
				}
				if kind == "null" {
					bad = bytes.Replace(original, []byte(`"Active":2`), []byte(`"Active":null`), 1)
				}
				if err := os.WriteFile(path, bad, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Read(context.Background()); err == nil {
				t.Fatal("unsafe input accepted")
			}
			if s.Write(context.Background(), snapshot) == nil {
				t.Fatal("unsafe checkpoint automatically overwritten")
			}
		})
	}
}

func TestCheckpointRejectsUnsafeDirectoryAndUnloadedReplacement(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if s, err := OpenFileStore(dir); err == nil {
		_ = s.Close()
		t.Fatal("non-private directory admitted")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	if s, err := OpenFileStore(alias); err == nil {
		_ = s.Close()
		t.Fatal("linked directory admitted")
	}
	s, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := storedCritical(t, s)
	_ = s.Close()
	s, err = OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if s.Write(context.Background(), snapshot) == nil {
		t.Fatal("new owner silently overwrote existing unloaded state")
	}
	if _, err = s.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = s.Write(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, err = s.Read(context.Background()); err == nil {
		t.Fatal("closed store read")
	}
}

func TestCheckpointPublicationUncertaintyNeverSends(t *testing.T) {
	s, _ := privateStore(t)
	e := engine(t)
	apply(t, e, 1, 99)
	sink, _ := NewFakeSink(4)
	s.fault = func(step string) error {
		if step == "published" {
			return ErrCheckpoint
		}
		return nil
	}
	at := signal(1, 0).ObservedAt
	if e.Dispatch(context.Background(), at, sink, s) == nil || len(sink.Events()) != 0 || !e.PersistenceFailed() {
		t.Fatal("uncertain attempt persistence sent notification")
	}
	s.fault = nil
	if err := e.Dispatch(context.Background(), at, sink, s); err != nil {
		t.Fatal(err)
	}
	if len(sink.Events()) != 1 {
		t.Fatal("retry did not use durable attempt")
	}
}
