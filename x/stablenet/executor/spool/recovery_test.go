package spool

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
)

func TestAcknowledgedAppendRemainsIdempotentWithoutRecreatingData(t *testing.T) {
	o, r := fixture(t)
	ctx := context.Background()
	s := opened(t, o)
	e, err := s.Append(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Acknowledge(ctx, ack(o, e)); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Status(ctx)
	same, err := s.Append(ctx, r)
	if err != nil || same != e {
		t.Fatalf("acknowledged duplicate changed identity: %+v %v", same, err)
	}
	after, _ := s.Status(ctx)
	if after != before {
		t.Fatal("acknowledged duplicate recreated data")
	}
	e2, err := s.Append(ctx, nextResult(r))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Acknowledge(ctx, ack(o, e2)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, r); err != nil {
		t.Fatal("bounded acknowledged history not idempotent")
	}
}

func TestACKCannotSkipAnUnacknowledgedResult(t *testing.T) {
	o, r := fixture(t)
	ctx := context.Background()
	s := opened(t, o)
	e1, err := s.Append(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	e2, err := s.Append(ctx, nextResult(r))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Acknowledge(ctx, ack(o, e2)); !errors.Is(err, ErrSequence) {
		t.Fatalf("forward ACK accepted: %v", err)
	}
	if _, err := s.Read(ctx, e1.Ref); err != nil {
		t.Fatal("forward ACK deleted prefix")
	}
}

func TestACKRecoveryAtEachDurableBoundary(t *testing.T) {
	cases := []struct {
		name                              string
		fileSync, rename, dirSync, remove int
	}{
		{"receipt_file_sync", 1, 0, 0, 0}, {"receipt_rename", 0, 1, 0, 0}, {"receipt_dir_sync", 0, 0, 1, 0},
		{"result_remove", 0, 0, 0, 1}, {"result_dir_sync", 0, 0, 2, 0}, {"checkpoint_file_sync", 2, 0, 0, 0},
		{"checkpoint_rename", 0, 2, 0, 0}, {"checkpoint_dir_sync", 0, 0, 3, 0}, {"receipt_remove", 0, 0, 0, 2}, {"cleanup_dir_sync", 0, 0, 4, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, r := fixture(t)
			ctx := context.Background()
			s := opened(t, o)
			e, err := s.Append(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			e2, err := s.Append(ctx, nextResult(r))
			if err != nil {
				t.Fatal(err)
			}
			ops := s.ops
			files, renames, dirs, removes := 0, 0, 0, 0
			injected := errors.New("injected ACK boundary")
			s.ops.syncFile = func(f *os.File) error {
				files++
				if files == tc.fileSync {
					return injected
				}
				return ops.syncFile(f)
			}
			s.ops.rename = func(a, b string) error {
				renames++
				if renames == tc.rename {
					return injected
				}
				return ops.rename(a, b)
			}
			s.ops.syncDir = func(p string) error {
				dirs++
				if dirs == tc.dirSync {
					return injected
				}
				return ops.syncDir(p)
			}
			s.ops.remove = func(p string) error {
				removes++
				if removes == tc.remove {
					return injected
				}
				return ops.remove(p)
			}
			if err := s.Acknowledge(ctx, ack(o, e)); !errors.Is(err, injected) {
				t.Fatalf("failure not exercised: %v", err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = opened(t, o)
			if _, err := s.Read(ctx, e2.Ref); err != nil {
				t.Fatal("recovery deleted later unacknowledged data")
			}
			st, err := s.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if st.Committed == o.Anchor {
				if _, err := s.Read(ctx, e.Ref); err != nil {
					t.Fatal("failed ACK lost both result and durable proof")
				}
				if err := s.Acknowledge(ctx, ack(o, e)); err != nil {
					t.Fatal(err)
				}
			} else if st.Committed != e.Ref {
				t.Fatal("recovery advanced beyond ACK")
			}
		})
	}
}

func TestCorruptAndTruncatedFilesFailClosed(t *testing.T) {
	for _, kind := range []string{"payload", "truncated", "oversize_header"} {
		t.Run(kind, func(t *testing.T) {
			o, r := fixture(t)
			ctx := context.Background()
			s := opened(t, o)
			e, err := s.Append(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(o.Path, recordName(e.Ref))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "payload":
				data[len(data)-1] ^= 1
			case "truncated":
				data = data[:len(data)-1]
			case "oversize_header":
				binary.BigEndian.PutUint32(data[4:8], ^uint32(0))
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			store, err := Open(ctx, o)
			if kind == "payload" {
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = store.Close() }()
				if _, err := store.Read(ctx, e.Ref); !errors.Is(err, ErrCorrupt) {
					t.Fatalf("corrupt payload sent: %v", err)
				}
			} else if !errors.Is(err, ErrCorrupt) {
				if store != nil {
					_ = store.Close()
				}
				t.Fatalf("corrupt length accepted: %v", err)
			}
			if got, err := os.ReadFile(path); err != nil || len(got) != len(data) {
				t.Fatal("corruption handling deleted evidence")
			}
		})
	}
}

func TestForeignMetadataAndSymlinkDoNotMutateOtherData(t *testing.T) {
	o, _ := fixture(t)
	ctx := context.Background()
	s := opened(t, o)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	wrong := o
	wrong.Anchor = execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}
	before, err := os.ReadFile(filepath.Join(o.Path, stateName))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Open(ctx, wrong); err == nil {
		if got != nil {
			_ = got.Close()
		}
		t.Fatal("different anchor accepted")
	}
	after, err := os.ReadFile(filepath.Join(o.Path, stateName))
	if err != nil || string(before) != string(after) {
		t.Fatal("foreign open rewrote identity")
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(o.Path, "foreign.result")); err != nil {
		t.Fatal(err)
	}
	if got, err := Open(ctx, o); err == nil {
		if got != nil {
			_ = got.Close()
		}
		t.Fatal("symlink record accepted")
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "keep" {
		t.Fatal("foreign symlink target changed")
	}
}

func TestAppendSealFailureAndCancellationRecovery(t *testing.T) {
	for _, boundary := range []string{"rename", "directory_sync", "cancel_after_file_sync"} {
		t.Run(boundary, func(t *testing.T) {
			o, r := fixture(t)
			s := opened(t, o)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			injected := errors.New("append seal failure")
			original := s.ops
			switch boundary {
			case "rename":
				s.ops.rename = func(string, string) error { return injected }
			case "directory_sync":
				s.ops.syncDir = func(string) error { return injected }
			default:
				s.ops.syncFile = func(f *os.File) error {
					if err := original.syncFile(f); err != nil {
						return err
					}
					cancel()
					return nil
				}
			}
			_, err := s.Append(ctx, r)
			if boundary == "cancel_after_file_sync" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation ignored", err)
				}
			} else if !errors.Is(err, injected) {
				t.Fatal("boundary not exercised", err)
			}
			if _, ok, err := s.Next(context.Background()); ok || (err != nil && !errors.Is(err, ErrRecovery)) {
				t.Fatal("failed append published before durable completion", err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = opened(t, o)
			st, err := s.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if boundary == "directory_sync" {
				if st.Pending != 1 {
					t.Fatal("renamed input lost on reopen")
				}
				if _, err := s.Read(context.Background(), r.Ref); err != nil {
					t.Fatal(err)
				}
			} else if st.Pending != 0 || st.Quarantined != 1 {
				t.Fatalf("unsealed input not preserved: %+v", st)
			}
		})
	}
}
