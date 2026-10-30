package nativestore

import (
	"errors"
	"testing"
)

func TestExclusiveRawMaintenanceExcludesImporterAndCollector(t *testing.T) {
	o, _ := fixture(t)
	first, err := AcquireRun(o.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	second, err := AcquireRun(o.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	blocked, err := AcquireExclusiveRun(o.DataDir)
	if blocked != nil {
		_ = blocked.Close()
	}
	if !errors.Is(err, ErrPending) {
		t.Fatalf("exclusive maintenance admitted while two children own native: %v", err)
	}
	_ = first.Close()
	_ = second.Close()
	exclusive, err := AcquireExclusiveRun(o.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = exclusive.Close() }()
	reader, err := AcquireRun(o.DataDir)
	if reader != nil {
		_ = reader.Close()
	}
	if !errors.Is(err, ErrPending) {
		t.Fatalf("child admitted under exclusive maintenance: %v", err)
	}
	_ = exclusive.Close()
	reader, err = AcquireRun(o.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
}
