package spool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestConcurrentCancellationAndCloseJoin(t *testing.T) {
	o, r := fixture(t)
	s := opened(t, o)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	fileSync := s.ops.syncFile
	s.ops.syncFile = func(f *os.File) error { close(entered); <-release; return fileSync(f) }
	first := make(chan error, 1)
	go func() { _, err := s.Append(context.Background(), r); first <- err }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancelled := make(chan error, 1)
	go func() { _, err := s.Read(ctx, r.Ref); cancelled <- err }()
	cancel()
	if err := <-cancelled; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting reader ignored cancellation: %v", err)
	}
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	<-s.done
	select {
	case err := <-closed:
		t.Fatalf("close did not join active fsync: %v", err)
	default:
	}
	if _, _, err := s.Next(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("new operation admitted after close: %v", err)
	}
	once.Do(func() { close(release) })
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := opened(t, o).Read(context.Background(), r.Ref); err != nil {
		t.Fatal("joined append was not durable", err)
	}
}

func TestInterruptedIdentityCannotBeRebound(t *testing.T) {
	o, _ := fixture(t)
	s := opened(t, o)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	temp := filepath.Join(o.Path, ".tmp-state-interrupted")
	if err := os.Rename(filepath.Join(o.Path, stateName), temp); err != nil {
		t.Fatal(err)
	}
	foreign := o
	foreign.Chain.ID = "1"
	if s, err := Open(context.Background(), foreign); err == nil {
		_ = s.Close()
		t.Fatal("unsealed identity was rebound to another chain")
	}
	if _, err := os.Stat(filepath.Join(o.Path, stateName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("foreign open published a new identity")
	}
	if _, err := os.Stat(temp); err != nil {
		t.Fatal("identity evidence changed", err)
	}
	if _, err := Open(context.Background(), o); err == nil {
		t.Fatal("ambiguous unsealed identity requires operator recovery")
	}
}

func TestUnknownTemporaryFileIsPreserved(t *testing.T) {
	o, _ := fixture(t)
	s := opened(t, o)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(o.Path, ".tmp-unrelated")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(context.Background(), o); err == nil {
		_ = s.Close()
		t.Fatal("unknown file admitted as owned temporary data")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
		t.Fatal("unrelated file moved or deleted")
	}
}
