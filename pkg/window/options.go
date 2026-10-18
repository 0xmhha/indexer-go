// Package window stores bounded, durable raw inputs independently of the
// indexing database. It never acknowledges projection commits or decides
// canonicality. A DB-backed, fenced deletion guard is required for GC.
package window

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

var (
	ErrUnavailable   = errors.New("window: storage unavailable; close and reopen")
	ErrCorrupt       = errors.New("window: corrupt storage")
	ErrCapacity      = errors.New("window: capacity exhausted")
	ErrLocked        = errors.New("window: writer already open")
	ErrConflict      = errors.New("window: conflicting immutable input")
	ErrNotFound      = errors.New("window: record not found")
	ErrAmbiguous     = errors.New("window: multiple block hashes at height")
	ErrBusy          = errors.New("window: active readers or read budget exhausted")
	ErrClosed        = errors.New("window: closed")
	ErrUnsafeGC      = errors.New("window: fenced deletion approval required")
	ErrCursorExpired = errors.New("window: raw cursor was collected; recover from source DB")
	ErrRetain        = errors.New("window: guard retains this segment")
)

// VolumeSpec identifies an already existing volume root. Mounted storage is
// required by default. AllowDirectory is an explicit opt-in for local test
// or directory storage; it must not be used to mask a missing external mount.
type VolumeSpec struct {
	Root           string `yaml:"root"`
	ID             string `yaml:"id"`
	AllowDirectory bool   `yaml:"allow_directory"`
	MaxBytes       int64  `yaml:"max_bytes"` // zero: legacy standalone; positive: shared durable quotas
	MinFreeBytes   int64  `yaml:"min_free_bytes"`
}

type Options struct {
	Volume            VolumeSpec
	Path              string // absolute, strictly below Volume.Root
	Reservation       string // stable instance name in the managed volume; retained on Close/crash
	Chain             model.FeedChain
	MaxBytes          int64 // logical file bytes plus index/metadata reservation
	MinFreeBytes      int64 // filesystem free-space floor, including other users
	SegmentBytes      int64
	MaxPayloadBytes   int64
	MaxSegmentRecords int
	MaxSegments       int
	ReadBudgetBytes   int64
	MaxReaders        int
	Retention         time.Duration
	Now               func() time.Time // nil uses time.Now; injectable logical time
}

func (o Options) normalized() (Options, error) {
	if err := o.Chain.Validate(); err != nil {
		return o, err
	}
	if !filepath.IsAbs(o.Path) || !filepath.IsAbs(o.Volume.Root) || !validVolumeID(o.Volume.ID) {
		return o, errors.New("window: absolute paths and a volume ID are required")
	}
	if o.Volume.MaxBytes < 0 || o.Volume.MaxBytes > 1<<60 || o.Volume.MinFreeBytes < 0 || (o.Volume.MaxBytes == 0 && (o.Reservation != "" || o.Volume.MinFreeBytes != 0)) || (o.Volume.MaxBytes > 0 && (!validVolumeID(o.Reservation) || o.MaxBytes > o.Volume.MaxBytes-VolumeOverheadBytes)) {
		return o, errors.New("window: invalid shared volume budget or reservation")
	}
	o.Path, o.Volume.Root = filepath.Clean(o.Path), filepath.Clean(o.Volume.Root)
	rel, err := filepath.Rel(o.Volume.Root, o.Path)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return o, errors.New("window: path must be strictly below volume root")
	}
	if o.SegmentBytes == 0 {
		o.SegmentBytes = 256 << 20
	}
	if o.MaxPayloadBytes == 0 {
		o.MaxPayloadBytes = 8 << 20
	}
	if o.MaxSegmentRecords == 0 {
		o.MaxSegmentRecords = 4096
	}
	if o.MaxSegments == 0 {
		o.MaxSegments = 4096
	}
	if o.ReadBudgetBytes == 0 {
		o.ReadBudgetBytes = 64 << 20
	}
	if o.MaxReaders == 0 {
		o.MaxReaders = 32
	}
	if o.Retention == 0 {
		o.Retention = 24 * time.Hour
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.MaxPayloadBytes < 1 || o.MaxPayloadBytes > 64<<20 || o.SegmentBytes > 1<<40 || o.MaxBytes > 1<<60 || o.SegmentBytes < o.MaxPayloadBytes+frameHeader+maxMeta ||
		o.MaxSegmentRecords < 1 || o.MaxSegmentRecords > 8192 || o.MaxSegments < 1 || o.MaxSegments > 16384 ||
		o.ReadBudgetBytes < o.MaxPayloadBytes+frameHeader+maxMeta || o.MaxReaders < 1 || o.MaxReaders > 1024 ||
		o.Retention < 24*time.Hour || o.MinFreeBytes < 0 || o.MaxBytes < o.SegmentBytes+o.reservation() {
		return o, errors.New("window: invalid byte, reader, segment or retention limits")
	}
	return o, nil
}

// Normalize validates limits/paths without touching files or starting RPC.
func (o Options) Normalize() (Options, error) { return o.normalized() }

func (o Options) reservation() int64 { return int64(o.MaxSegmentRecords)*indexEntry*2 + 64<<10 }

func validVolumeID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("._-", r):
		default:
			return false
		}
	}
	return id != "." && id != ".."
}

func segmentName(id uint64, suffix string) string { return fmt.Sprintf("%020d.%s", id, suffix) }
