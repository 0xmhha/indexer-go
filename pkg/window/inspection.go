package window

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// Inspection is metadata evidence, never a GC permit or a full integrity scan.
// Leases and clock history belong to the live writer and cannot be inferred
// from a directory, PID, missing files or a historical completion receipt.
type Inspection struct {
	StoreID          common.Hash      `json:"storeId"`
	VolumeID         string           `json:"volumeId"`
	Managed          bool             `json:"managed"`
	Reservation      string           `json:"reservation"`
	QuotaStoreMatch  *bool            `json:"quotaStoreMatch"`
	Intents          []DeletionRecord `json:"intents"`
	Completed        *DeletionRecord  `json:"completed"`
	PendingMatch     string           `json:"pendingMatch"`
	PendingFiles     map[string]bool  `json:"pendingFiles"`
	LeaseStatus      string           `json:"leaseStatus"`
	PayloadIntegrity string           `json:"payloadIntegrity"`
}

// Root confines resolution, but a symlink to another file inside the root is
// still a substitution. Check Lstat and the opened handle as well as flags.
func readInspectionFile(root *os.Root, name string, limit int64) ([]byte, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, ErrConflict
	}
	f, err := openRegular(root, name, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err == nil && !os.SameFile(before, opened) {
		err = ErrConflict
	}
	var data []byte
	if err == nil {
		data, err = io.ReadAll(io.LimitReader(f, limit+1))
	}
	if err == nil && int64(len(data)) > limit {
		err = ErrCorrupt
	}
	after, e := root.Lstat(name)
	if e != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		err = errors.Join(err, ErrConflict, e)
	}
	return data, errors.Join(err, f.Close())
}

// DescribeDeletion computes only the existing exact-reference identity. It
// makes no assertion about canonicality, finality, ACKs, leases or DB bytes.
func DescribeDeletion(refs []Reference) (DeletionRecord, error) {
	if len(refs) < 1 || len(refs) > 8192 {
		return DeletionRecord{}, ErrUnsafeGC
	}
	first := refs[0]
	seen := map[int64]bool{}
	for _, ref := range refs {
		if ref.StoreID == (common.Hash{}) || ref.Segment == 0 || ref.StoreID != first.StoreID || ref.Segment != first.Segment || ref.Block.Validate() != nil || !ref.Kind.Valid() || ref.Codec == "" || len(ref.Codec) > 128 || ref.Offset < 0 || ref.FrameBytes <= 0 || ref.PayloadBytes <= 0 || ref.PayloadBytes > ref.FrameBytes || ref.Digest == (common.Hash{}) || ref.FirstReceived <= 0 {
			return DeletionRecord{}, ErrConflict
		}
		if seen[ref.Offset] {
			return DeletionRecord{}, ErrConflict
		}
		seen[ref.Offset] = true
	}
	digest, err := referencesHash(refs)
	return DeletionRecord{StoreID: first.StoreID, Segment: first.Segment, ReferencesHash: digest}, err
}

// Inspect opens existing directories/files O_RDONLY, without a writer lock,
// quota claim, recovery, truncation, fsync, rename or unlink. Options must be
// the exact configured layout. Concurrent metadata changes fail the read;
// success still represents separate observations, not an atomic DB/FS snapshot.
func Inspect(ctx context.Context, options Options, pending []Reference) (out Inspection, err error) {
	opts, err := options.normalized()
	if err != nil {
		return out, err
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	info, err := os.Lstat(opts.Volume.Root)
	if err != nil {
		return out, err
	}
	if !info.IsDir() {
		return out, ErrConflict
	}
	volume, err := os.OpenRoot(opts.Volume.Root)
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, volume.Close()) }()
	volumeDir, err := volume.Open(".")
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, volumeDir.Close()) }()
	volumeInfo, err := volumeDir.Stat()
	if err != nil {
		return out, err
	}
	if err = checkMount(volumeDir, opts.Volume); err != nil {
		return out, err
	}
	// Refuse directory symlinks as well as final-component file symlinks.
	rel, _ := filepath.Rel(opts.Volume.Root, opts.Path)
	for prefix, parts := "", strings.Split(rel, string(filepath.Separator)); len(parts) > 0; parts = parts[1:] {
		prefix = filepath.Join(prefix, parts[0])
		info, e := volume.Lstat(prefix)
		if e != nil {
			return out, e
		}
		if !info.IsDir() {
			return out, ErrConflict
		}
	}
	root, err := volume.OpenRoot(rel)
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	dir, err := root.Open(".")
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, dir.Close()) }()
	dirInfo, err := dir.Stat()
	if err != nil {
		return out, err
	}
	if same, e := sameDevice(volumeDir, dir); e != nil || !same {
		return out, errors.Join(ErrUnavailable, e)
	}
	type observation struct {
		root  *os.Root
		name  string
		data  []byte
		limit int64
	}
	var observations []observation
	read := func(r *os.Root, name string, limit int64) ([]byte, error) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		b, e := readInspectionFile(r, name, limit)
		if e == nil || errors.Is(e, os.ErrNotExist) {
			observations = append(observations, observation{r, name, b, limit})
		}
		return b, e
	}
	b, err := read(volume, ".indexer-volume.json", maxMeta)
	var marker volumeMarker
	if err != nil {
		return out, err
	}
	if json.Unmarshal(b, &marker) != nil || marker != markerFor(opts.Volume) {
		return out, ErrConflict
	}
	b, err = read(root, "STORE.json", maxMeta)
	var meta storeMeta
	if err != nil {
		return out, err
	}
	if json.Unmarshal(b, &meta) != nil || meta.Version != 1 || meta.Chain != opts.Chain || meta.ID == (common.Hash{}) || meta.Next == 0 || meta.Kind != "" {
		return out, ErrConflict
	}
	out = Inspection{StoreID: meta.ID, VolumeID: marker.ID, Managed: opts.Volume.MaxBytes > 0, Reservation: opts.Reservation, Intents: []DeletionRecord{}, PendingMatch: "not_pending", PendingFiles: map[string]bool{}, LeaseStatus: "unknown", PayloadIntegrity: "not_checked"}
	if out.Managed {
		if _, err = read(volume, budgetFile, maxBudgetJSON); err != nil {
			return out, err
		}
		if _, err = read(volume, budgetLock, 32); err != nil {
			return out, err
		}
		budget, e := readBudget(volume, opts.Volume)
		if e != nil {
			return out, e
		}
		match := false
		for _, claim := range budget.Claims {
			if claim.Name == opts.Reservation && claim.Path == rel && claim.Chain == opts.Chain && claim.MaxBytes == opts.MaxBytes && claim.StoreID == meta.ID {
				match = true
			}
		}
		out.QuotaStoreMatch = &match
		if !match {
			return out, ErrConflict
		}
	}
	// entries only reads a bounded directory; no Store lifecycle is opened.
	entries, err := (&Store{root: root, opts: opts}).entries()
	if err != nil {
		return out, err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".gc") {
			continue
		}
		id, suffix, e := parseSegment(entry.Name())
		if e != nil || suffix != "gc" || len(out.Intents) >= 64 {
			return out, ErrCorrupt
		}
		b, e := read(root, entry.Name(), maxMeta)
		if e != nil {
			return out, e
		}
		var record DeletionRecord
		if json.Unmarshal(b, &record) != nil || record.StoreID != meta.ID || record.Segment != id {
			return out, ErrConflict
		}
		out.Intents = append(out.Intents, record)
	}
	sort.Slice(out.Intents, func(i, j int) bool { return out.Intents[i].Segment < out.Intents[j].Segment })
	b, err = read(root, completedGC, maxMeta)
	if err == nil {
		var record DeletionRecord
		if json.Unmarshal(b, &record) != nil || record.StoreID != meta.ID || record.Segment == 0 || record.ReferencesHash == (common.Hash{}) {
			return out, ErrConflict
		}
		out.Completed = &record
	} else if !errors.Is(err, os.ErrNotExist) {
		return out, err
	}
	if len(pending) > 0 {
		expected, e := DescribeDeletion(pending)
		if e != nil || expected.StoreID != meta.ID {
			return out, errors.Join(ErrConflict, e)
		}
		out.PendingMatch = "no_matching_receipt"
		for _, intent := range out.Intents {
			if intent.Segment == expected.Segment {
				if intent.Match(pending) != nil {
					return out, ErrConflict
				}
				out.PendingMatch = "intent_matches"
			}
		}
		if out.Completed != nil && out.Completed.Segment == expected.Segment {
			if out.Completed.Match(pending) != nil {
				return out, ErrConflict
			}
			if out.PendingMatch == "intent_matches" {
				out.PendingMatch = "intent_and_completion_match"
			} else {
				out.PendingMatch = "completion_matches"
			}
		}
		for _, suffix := range []string{"raw", "idx", "seal"} {
			info, e := root.Lstat(segmentName(expected.Segment, suffix))
			if e == nil && !info.Mode().IsRegular() {
				return out, ErrConflict
			}
			if e != nil && !errors.Is(e, os.ErrNotExist) {
				return out, e
			}
			out.PendingFiles[suffix] = e == nil
		}
	}
	for _, seen := range observations {
		if e := ctx.Err(); e != nil {
			return out, e
		}
		current, e := readInspectionFile(seen.root, seen.name, seen.limit)
		if seen.data == nil && errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil || !bytes.Equal(current, seen.data) {
			return out, errors.Join(ErrConflict, e)
		}
	}
	latest, err := (&Store{root: root, opts: opts}).entries()
	if err != nil {
		return out, err
	}
	gcCount := 0
	for _, entry := range latest {
		if strings.HasSuffix(entry.Name(), ".gc") {
			gcCount++
		}
	}
	if gcCount != len(out.Intents) {
		return out, ErrConflict
	}
	for path, pinned := range map[string]os.FileInfo{opts.Volume.Root: volumeInfo, opts.Path: dirInfo} {
		current, e := os.Stat(path)
		if e != nil || !os.SameFile(current, pinned) {
			return out, errors.Join(ErrUnavailable, e)
		}
	}
	if err = checkMount(volumeDir, opts.Volume); err != nil {
		return out, err
	}
	return out, ctx.Err()
}
