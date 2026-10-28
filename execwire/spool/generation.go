package spool

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xmhha/indexer-go/execwire"
)

// Rebind starts a newer generation at an already acknowledged canonical
// floor. The caller verifies its canonical hash; this is never a forward ACK.
func (s *Store) Rebind(ctx context.Context, base execwire.Ref) error {
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer s.release()
	if s.recovery || s.maintenance || s.maintenanceOnly {
		return ErrRecovery
	}
	activeBase := s.state.Anchor
	if s.state.Base != nil {
		activeBase = *s.state.Base
	}
	if base == activeBase {
		return nil
	}
	if base.Generation <= s.state.Committed.Generation || base.Height < s.state.Anchor.Height || base.Height > s.state.Committed.Height || base.Height == s.state.Anchor.Height && base.Hash != s.state.Anchor.Hash {
		return ErrSequence
	}
	if err := validateAnchor(Options{Chain: s.options.Chain, Anchor: base}); err != nil {
		return err
	}
	if err := s.checkACKCapacity(); err != nil {
		return err
	}
	j := transition{Version: 1, Chain: s.options.Chain, Anchor: s.state.Anchor, From: s.state.Committed, To: base}
	data, err := sealJSON(j)
	if err != nil {
		return err
	}
	if err = s.atomic(ctx, "transition", transitionName, data); err != nil {
		s.recovery = true
		return err
	}
	if err = s.finishTransition(ctx, j); err != nil {
		s.recovery = true
		return err
	}
	return nil
}

// Retained returns a previously quarantined canonical result, reencoded for
// the active generation. It neither acknowledges nor deletes retained data.
func (s *Store) Retained(ctx context.Context, ref execwire.Ref) (execwire.Result, error) {
	if err := s.acquire(ctx); err != nil {
		return execwire.Result{}, err
	}
	defer s.release()
	if s.recovery || s.maintenance || s.maintenanceOnly {
		return execwire.Result{}, ErrRecovery
	}
	if ref.Generation != s.state.Committed.Generation {
		return execwire.Result{}, ErrSequence
	}
	names, err := os.ReadDir(s.path)
	if err != nil {
		return execwire.Result{}, err
	}
	var found *execwire.Result
	var canonical []byte
	for _, item := range names {
		if err := ctx.Err(); err != nil {
			return execwire.Result{}, err
		}
		if !strings.HasPrefix(item.Name(), ".quarantine-") || !strings.HasSuffix(item.Name(), ".result") {
			continue
		}
		path := filepath.Join(s.path, item.Name())
		e, err := readEntry(path, s.options.Chain)
		if err != nil {
			return execwire.Result{}, err
		}
		if e.Ref.Height != ref.Height || e.Ref.Hash != ref.Hash {
			continue
		}
		r, err := readResult(ctx, path, s.options.Chain, e)
		if err != nil {
			return execwire.Result{}, err
		}
		r.Ref.Generation = ref.Generation
		encoded, err := execwire.MarshalResult(r)
		if err != nil {
			return execwire.Result{}, err
		}
		if found != nil && !bytes.Equal(canonical, encoded) {
			return execwire.Result{}, ErrConflict
		}
		found, canonical = &r, encoded
	}
	if found == nil {
		return execwire.Result{}, os.ErrNotExist
	}
	return *found, nil
}

const transitionName = ".exec-spool-transition"

// Checkpoint reads the durable ACK/base without scanning retained files.
func (s *Store) Checkpoint(ctx context.Context) (execwire.Ref, error) {
	if err := s.acquire(ctx); err != nil {
		return execwire.Ref{}, err
	}
	defer s.release()
	if s.recovery || s.maintenance || s.maintenanceOnly {
		return execwire.Ref{}, ErrRecovery
	}
	return s.state.Committed, nil
}

// Reference returns only a known sealed or acknowledged hash.
func (s *Store) Reference(ctx context.Context, height uint64) (execwire.Ref, bool, error) {
	if err := s.acquire(ctx); err != nil {
		return execwire.Ref{}, false, err
	}
	defer s.release()
	if s.recovery || s.maintenance || s.maintenanceOnly {
		return execwire.Ref{}, false, ErrRecovery
	}
	if height == s.state.Committed.Height {
		return s.state.Committed, true, nil
	}
	if e, ok := s.entries[height]; ok {
		return e.Ref, true, nil
	}
	for _, e := range s.state.History {
		if e.Ref.Height == height {
			return e.Ref, true, nil
		}
	}
	base := s.state.Anchor
	if s.state.Base != nil {
		base = *s.state.Base
	}
	if height == base.Height {
		return base, true, nil
	}
	if height == s.state.Anchor.Height {
		ref := s.state.Anchor
		ref.Generation = s.state.Committed.Generation
		return ref, true, nil
	}
	return execwire.Ref{}, false, nil
}

// Tail is the highest contiguous durably sealed result, not a public ACK.
func (s *Store) Tail(ctx context.Context) (execwire.Ref, error) {
	if err := s.acquire(ctx); err != nil {
		return execwire.Ref{}, err
	}
	defer s.release()
	if s.recovery || s.maintenance || s.maintenanceOnly {
		return execwire.Ref{}, ErrRecovery
	}
	tail := s.state.Committed
	for _, e := range s.entries {
		if e.Ref.Height > tail.Height {
			tail = e.Ref
		}
	}
	return tail, nil
}

// After walks a sender's bounded in-flight window without advancing the ACK.
func (s *Store) After(ctx context.Context, ref execwire.Ref) (Entry, bool, error) {
	if err := s.acquire(ctx); err != nil {
		return Entry{}, false, err
	}
	defer s.release()
	if s.recovery || s.maintenance || s.maintenanceOnly {
		return Entry{}, false, ErrRecovery
	}
	if ref.Generation != s.state.Committed.Generation {
		return Entry{}, false, ErrSequence
	}
	known := s.state.Committed
	if ref.Height != known.Height {
		if e, ok := s.entries[ref.Height]; ok {
			known = e.Ref
		} else {
			found := false
			for _, e := range s.state.History {
				if e.Ref.Height == ref.Height {
					known, found = e.Ref, true
					break
				}
			}
			if !found {
				return Entry{}, false, ErrSequence
			}
		}
	}
	if ref != known {
		return Entry{}, false, ErrConflict
	}
	e, ok := s.entries[ref.Height+1]
	if ok && e.Parent != ref.Hash {
		return Entry{}, false, ErrConflict
	}
	return e, ok, nil
}

type transition struct {
	Version          uint16
	Chain            execwire.Chain
	Anchor, From, To execwire.Ref
}

func (s *Store) recoverTransition(ctx context.Context) error {
	var j transition
	err := readSealedJSON(filepath.Join(s.path, transitionName), &j)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.finishTransition(ctx, j)
}

// validateTransition is the production pure journal/state check. Inspection may
// call it without entering recovery, directory scans, quarantine or state writes.
func validateTransition(state manifest, chain execwire.Chain, j transition) error {
	if j.Version != 1 || j.Chain != chain || j.Anchor != state.Anchor || j.To.Generation <= j.From.Generation || j.To.Height > j.From.Height || j.To.Height < j.Anchor.Height || j.To.Height == j.Anchor.Height && j.To.Hash != j.Anchor.Hash {
		return ErrCorrupt
	}
	if err := validateAnchor(Options{Chain: j.Chain, Anchor: j.To}); err != nil {
		return ErrCorrupt
	}
	already := state.Base != nil && *state.Base == j.To && state.Committed == j.To
	if state.Committed != j.From && !already {
		return ErrCorrupt
	}
	return nil
}

func (s *Store) finishTransition(ctx context.Context, j transition) error {
	if err := validateTransition(s.state, s.options.Chain, j); err != nil {
		return err
	}

	names, err := os.ReadDir(s.path)
	if err != nil {
		return err
	}
	for _, item := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := item.Name()
		if strings.HasPrefix(name, ".quarantine-") || !strings.HasSuffix(name, ".result") && !strings.HasSuffix(name, ".ack") {
			continue
		}
		path := filepath.Join(s.path, name)
		if strings.HasSuffix(name, ".result") {
			e, err := readEntry(path, j.Chain)
			if err != nil {
				return err
			}
			if recordName(e.Ref) != name || e.Ref.Generation != j.From.Generation {
				return ErrConflict
			}
			if _, err := readResult(ctx, path, j.Chain, e); err != nil {
				return err
			}
		} else {
			var a ackReceipt
			if err := readSealedJSON(path, &a); err != nil {
				return err
			}
			if a.Version != 1 || a.Chain != j.Chain || a.Entry.Ref.Generation != j.From.Generation || ackName(a.Entry.Ref) != name {
				return ErrCorrupt
			}
			if err := validateReceipt(a); err != nil {
				return err
			}
		}
		if err := s.ops.rename(path, filepath.Join(s.path, ".quarantine-"+name)); err != nil {
			return err
		}
	}
	if err := s.ops.syncDir(s.path); err != nil {
		return err
	}
	base := j.To
	s.state.Version, s.state.Base, s.state.Committed = 2, &base, base
	s.state.Digest, s.state.History = execwire.Hash{}, nil
	if err := s.saveState(ctx); err != nil {
		return err
	}
	s.entries = make(map[uint64]Entry)
	if err := s.ops.remove(filepath.Join(s.path, transitionName)); err != nil {
		return err
	}
	return s.ops.syncDir(s.path)
}
