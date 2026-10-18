package window

import (
	"encoding/json"
	"os"
)

// The Store and volume write locks are held, and raw bytes are already synced.
// Keep the index-before-seal durability barrier. Seal and the next ID reservation
// have no dependency on each other, so they can share ONE directory sync.
// No next raw file may be created until that shared barrier succeeds.
func (s *Store) sealAndReserve(wantNext bool) (uint64, error) {
	if s.active == nil || len(s.active.refs) == 0 {
		return 0, nil
	}
	a := s.active
	data, digest := makeIndex(a.refs)
	meta := summarize(a.ID, a.DataBytes, a.refs, digest)
	if err := writeAtomic(s.root, segmentName(a.ID, "idx"), data); err != nil {
		return 0, s.poison(err)
	}
	var next uint64
	if wantNext && len(s.segments)+1 < s.opts.MaxSegments && s.next != ^uint64(0) {
		s.rotationStep("indexSynced")
		encoded, err := json.Marshal(meta)
		if err != nil {
			return 0, s.poison(err)
		}
		seal, err := prepareRename(s.root, segmentName(a.ID, "seal"), encoded)
		if err != nil {
			return 0, s.poison(err)
		}
		defer seal.close()
		next = s.next
		s.next++
		if err := s.writeMetaUsing(func(root *os.Root, name string, data []byte) error {
			meta, err := prepareRename(root, name, data)
			if err != nil {
				return err
			}
			defer meta.close()
			s.rotationStep("metadataPrepared")
			if err := seal.commit(); err != nil {
				return err
			}
			s.rotationStep("sealRenamed")
			if err := meta.commit(); err != nil {
				return err
			}
			s.rotationStep("metaRenamed")
			return nil
		}); err != nil {
			return 0, s.poison(err)
		}
		if err := syncDir(s.root); err != nil {
			return 0, s.poison(err)
		}
		s.rotationStep("metadataSynced")
	} else {
		// Explicit Seal/Close and capacity/ID exhaustion keep the old path;
		// reserving an unusable segment is unnecessary.
		if err := writeJSON(s.root, segmentName(a.ID, "seal"), meta); err != nil {
			return 0, s.poison(err)
		}
	}
	if err := a.file.Close(); err != nil {
		return 0, s.poison(err)
	}
	meta.leases = a.leases
	s.segments = append(s.segments, &meta)
	s.active = nil
	return next, s.recount()
}

func (s *Store) rotationStep(step string) {
	if s.afterRotateStep != nil {
		s.afterRotateStep(step)
	}
}
