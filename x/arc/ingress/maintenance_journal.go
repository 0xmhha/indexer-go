package arcingress

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
)

const maintenanceSuffix = ".arc-execution-maintenance"

var maintenancePhases = []string{"prepared", "authorized", "raw_complete", "cow_complete", "db_complete", "complete"}

type maintenanceScope struct{ Native, Raw, Source, VolumeRoot, VolumeID, Reservation string }
type maintenanceRecord struct {
	Version                                 uint8
	Phase                                   string
	Scope                                   maintenanceScope
	Options                                 ingress.MaintenanceOptions
	BinaryDigest, PolicyDigest, OwnerDigest execwire.Hash
	Point                                   ingress.DrainPoint
	Proof                                   nativeinput.Proof
	Request                                 retention.Request
	PreviousToken                           execwire.Hash
	Permit                                  *retention.Permit
	Receipt                                 *retention.Receipt
}

func maintenancePhase(s string) int {
	for n, p := range maintenancePhases {
		if p == s {
			return n
		}
	}
	return -1
}
func (s maintenanceRecord) validate() error {
	n := maintenancePhase(s.Phase)
	i, h := s.Proof.Identity, s.Proof.Head
	if s.Version != 1 || n < 0 || s.Options.Timeout <= 0 || s.Options.Timeout > time.Hour || s.Options.ReserveBytes == 0 || s.PolicyDigest == (execwire.Hash{}) || s.OwnerDigest == (execwire.Hash{}) || s.BinaryDigest == (execwire.Hash{}) || nativeinput.ValidateProof(s.Proof, i) != nil || s.Proof.Ref != h.Tip || s.Point.Digest == (execwire.Hash{}) || s.Point.Chain.ID != "5042002" || s.Point.Chain.Genesis != i.Genesis || s.Point.Ref.Generation == 0 || s.Point.Ref.Height != h.Tip.Height || s.Point.Ref.Hash != h.Tip.Hash || s.Point.Root != h.Tip.Root || s.Request.Validate() != nil || s.Request.Chain != s.Point.Chain || s.Request.ACK != s.Point.Ref || s.Request.Tail != s.Point.Ref || s.Request.Head.Height != h.Tip.Height || s.Request.Head.Hash != h.Tip.Hash || s.Request.HeadRoot != h.Tip.Root || s.Request.Anchor.Height != i.Anchor.Height || s.Request.Anchor.Hash != i.Anchor.Hash || s.Options.KeepBlocks != s.Request.KeepBlocks {
		return port.ErrExecutionConflict
	}
	for _, path := range []string{s.Scope.Native, s.Scope.Raw, s.Scope.Source, s.Scope.VolumeRoot, s.Options.Executable} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(os.PathSeparator) {
			return port.ErrInvalidData
		}
	}
	if n == 0 {
		if s.Permit != nil || s.Receipt != nil {
			return port.ErrExecutionConflict
		}
		return nil
	}
	if s.Permit == nil || s.Permit.Validate() != nil || s.Permit.Token == s.PreviousToken || s.Permit.Request != s.Request || s.Permit.Visible != s.Point.Ref || s.Permit.Root != s.Point.Root {
		return port.ErrExecutionConflict
	}
	if n < 3 {
		if s.Receipt != nil {
			return port.ErrExecutionConflict
		}
	} else if s.Receipt == nil || s.Receipt.Match(*s.Permit) != nil {
		return port.ErrExecutionConflict
	}
	return nil
}
func (s maintenanceRecord) result() ingress.MaintenanceResult {
	r := ingress.MaintenanceResult{Point: s.Point, Phase: s.Phase}
	if s.Permit != nil {
		r.Permit = *s.Permit
	}
	if s.Receipt != nil {
		r.Receipt = *s.Receipt
	}
	if s.Phase == "complete" {
		r.Release.PermitDigest, _ = retention.Digest(r.Permit)
		r.Release.ReceiptDigest, _ = retention.Digest(r.Receipt)
		r.Release.Version = 1
	}
	return r
}
func maintenancePath(native string) string {
	return filepath.Join(filepath.Dir(native), "."+filepath.Base(native)+maintenanceSuffix)
}

type maintenanceJournal struct {
	path    string
	parent  os.FileInfo
	lock    *os.File
	current *maintenanceRecord
}

func openMaintenanceJournal(native string) (*maintenanceJournal, error) {
	path := maintenancePath(native)
	i, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	if !privateDirectory(i) {
		return nil, ErrNative
	}
	f, err := lockMaintenanceFile(path + ".lock")
	if err != nil {
		return nil, err
	}
	j := &maintenanceJournal{path: path, parent: i, lock: f}
	var s maintenanceRecord
	err = readArtifact(path, &s)
	if errors.Is(err, os.ErrNotExist) {
		return j, nil
	}
	if err == nil {
		err = s.validate()
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	j.current = &s
	return j, nil
}
func (j *maintenanceJournal) close() error { return j.lock.Close() }
func (j *maintenanceJournal) save(ctx context.Context, next maintenanceRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := next.validate(); err != nil {
		return err
	}
	i, err := os.Lstat(filepath.Dir(j.path))
	if err != nil {
		return err
	}
	if !privateDirectory(i) || !os.SameFile(i, j.parent) {
		return ErrNative
	}
	if err = nativestore.PrivateFile(j.path + ".lock"); err != nil {
		return err
	}
	actual, err := os.Lstat(j.path + ".lock")
	if err != nil {
		return err
	}
	held, err := j.lock.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(actual, held) {
		return ErrNative
	}
	var current maintenanceRecord
	err = readArtifact(j.path, &current)
	if j.current == nil {
		if !errors.Is(err, os.ErrNotExist) {
			return port.ErrExecutionConflict
		}
	} else {
		if err != nil {
			return err
		}
		a, _ := json.Marshal(current)
		b, _ := json.Marshal(j.current)
		if string(a) != string(b) {
			return port.ErrExecutionConflict
		}
		if next.Phase == "prepared" {
			if current.Phase != "complete" {
				return port.ErrExecutionMaintenance
			}
		} else if maintenancePhase(next.Phase) != maintenancePhase(current.Phase)+1 {
			return port.ErrExecutionConflict
		}
	}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(sealedArtifact{Data: data, Digest: execwire.Hash(sha256.Sum256(data))})
	if err != nil {
		return err
	}
	if len(encoded) > nativeinput.MaxMeta {
		return port.ErrInvalidData
	}
	f, err := os.CreateTemp(filepath.Dir(j.path), ".arc-phase-")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if _, err = f.Write(encoded); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), j.path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(j.path))
	if err != nil {
		return err
	}
	err = errors.Join(d.Sync(), d.Close())
	if err != nil {
		return err
	}
	j.current = &next
	return nil
}
