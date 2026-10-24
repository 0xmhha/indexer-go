package spool

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"os"
	"path/filepath"
	"reflect"
)

// MetadataObservation is diagnostic only. It neither scans/validates result
// payloads nor authorizes ACK, recovery, garbage collection or startup.
type MetadataObservation struct {
	Chain              execwire.Chain
	Anchor, Committed  execwire.Ref
	TransitionPending  bool
	MaintenancePending bool
	Scope              string
}

func InspectMetadata(ctx context.Context, path string) (out MetadataObservation, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	info, err := os.Lstat(path)
	if err != nil {
		return out, err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return out, ErrConflict
	}
	a, err := readManifest(filepath.Join(path, stateName))
	if err != nil {
		return out, err
	}
	_, e := os.Lstat(filepath.Join(path, ".exec-spool-transition"))
	pending := e == nil
	if e != nil && !os.IsNotExist(e) {
		return out, e
	}
	_, ge := os.Lstat(filepath.Join(path, gcIntentName))
	if ge != nil && !os.IsNotExist(ge) {
		return out, ge
	}
	b, err := readManifest(filepath.Join(path, stateName))
	if err != nil {
		return out, err
	}
	if !reflect.DeepEqual(a, b) {
		return out, ErrRecovery
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	return MetadataObservation{Chain: a.Chain, Anchor: a.Anchor, Committed: a.Committed, TransitionPending: pending, MaintenancePending: ge == nil, Scope: "two matching manifest observations; result payloads and concurrent changes unverified; no ACK/GC/start authorization"}, nil
}
