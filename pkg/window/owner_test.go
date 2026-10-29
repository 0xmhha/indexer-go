package window

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const ownerDomain = "test.native.intake/v1"

func refuseOwnerGC(DeletionRecord) error { return ErrUnsafeGC }

func TestOwnedWindowDurableBindingCASAndLegacyRefusal(t *testing.T) {
	opts, _ := fixture(t)
	initial, next := []byte(`{"Epoch":1,"Received":0}`), []byte(`{"Epoch":1,"Received":1}`)
	s, err := CreateOwned(opts, ownerDomain, initial)
	require.NoError(t, err, "owned window must initialize one durable control with its own store identity")
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	owner, err := s.ReadOwner(t.Context())
	require.NoError(t, err)
	require.Equal(t, uint8(1), owner.Version)
	require.Equal(t, ownerDomain, owner.Domain)
	require.Equal(t, initial, []byte(owner.State))
	ref, err := s.Append(t.Context(), envelope(opts, 1))
	require.NoError(t, err)
	require.Equal(t, ref.StoreID, owner.StoreID)
	require.NoError(t, s.CompareOwner(t.Context(), initial, next))
	require.ErrorIs(t, s.CompareOwner(t.Context(), initial, next), ErrConflict)
	require.NoError(t, s.Close())
	_, err = Open(opts)
	require.ErrorIs(t, err, ErrConflict, "legacy capture must not adopt a managed raw window")
	_, err = OpenOwnedChecked(opts, "foreign/v1", refuseOwnerGC)
	require.ErrorIs(t, err, ErrConflict)
	s, err = OpenOwnedChecked(opts, ownerDomain, refuseOwnerGC)
	require.NoError(t, err)
	owner, err = s.ReadOwner(t.Context())
	require.NoError(t, err)
	require.Equal(t, next, []byte(owner.State))
	got, err := s.Lookup(t.Context(), ref.Block, ref.Kind)
	require.NoError(t, err)
	require.Equal(t, ref, got)
	before := s.Stats().FileBytes
	require.NoError(t, s.CompareOwner(t.Context(), next, []byte(`{"Epoch":1,"Received":1,"Padding":"bounded"}`)))
	require.Greater(t, s.Stats().FileBytes, before, "control bytes must be charged to the same window")
}

func TestOwnedWindowMissingCorruptLinkedAndOversizedControlStaysFenced(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt", "symlink", "hardlink", "public"} {
		t.Run(mode, func(t *testing.T) {
			opts, _ := fixture(t)
			s, err := CreateOwned(opts, ownerDomain, []byte(`{"Height":0}`))
			require.NoError(t, err)
			require.NoError(t, s.Close())
			path := filepath.Join(opts.Path, "OWNER.json")
			original, err := os.ReadFile(path)
			require.NoError(t, err)
			switch mode {
			case "missing":
				require.NoError(t, os.Remove(path))
			case "corrupt":
				require.NoError(t, os.WriteFile(path, []byte(`{"truncated":`), 0600))
			case "symlink":
				require.NoError(t, os.Remove(path))
				require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "foreign"), path))
			case "hardlink":
				require.NoError(t, os.Link(path, filepath.Join(t.TempDir(), "linked")))
			case "public":
				require.NoError(t, os.Chmod(path, 0644))
			}
			_, err = OpenOwnedChecked(opts, ownerDomain, refuseOwnerGC)
			require.Error(t, err)
			if mode == "hardlink" || mode == "public" {
				got, e := os.ReadFile(path)
				require.NoError(t, e)
				require.Equal(t, original, got)
			}
		})
	}
	opts, _ := fixture(t)
	s, err := CreateOwned(opts, ownerDomain, []byte(`{"Height":0}`))
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()
	large, err := json.Marshal(string(bytes.Repeat([]byte{'x'}, 8192)))
	require.NoError(t, err)
	require.Error(t, s.CompareOwner(t.Context(), []byte(`{"Height":0}`), large))
	owner, err := s.ReadOwner(t.Context())
	require.NoError(t, err)
	require.JSONEq(t, `{"Height":0}`, string(owner.State))
}

func TestOwnedWindowConcurrentCASHasOneWinnerAndJoinsClose(t *testing.T) {
	opts, _ := fixture(t)
	initial := []byte(`{"Height":0}`)
	s, err := CreateOwned(opts, ownerDomain, initial)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, state := range []string{`{"Height":1}`, `{"Height":2}`} {
		wg.Go(func() { results <- s.CompareOwner(t.Context(), initial, []byte(state)) })
	}
	wg.Wait()
	first, second := <-results, <-results
	if first == nil {
		require.ErrorIs(t, second, ErrConflict)
	} else {
		require.ErrorIs(t, first, ErrConflict)
		require.NoError(t, second)
	}
	require.NoError(t, s.Close())
	_, err = s.ReadOwner(t.Context())
	require.ErrorIs(t, err, ErrClosed)
}

func TestOwnedWindowCorruptionStopsAppendsAndLeases(t *testing.T) {
	opts, _ := fixture(t)
	s, err := CreateOwned(opts, ownerDomain, []byte(`{"Height":0}`))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ref, err := s.Append(t.Context(), envelope(opts, 1))
	require.NoError(t, err)
	bad := []byte(`{"truncated":`)
	path := filepath.Join(opts.Path, ownerName)
	require.NoError(t, os.WriteFile(path, bad, 0600))
	_, err = s.Append(t.Context(), envelope(opts, 2))
	require.Error(t, err, "corrupt control must fence raw writes after open, not only at restart")
	_, err = s.Acquire(t.Context(), ref)
	require.Error(t, err, "corrupt control must fence read leases")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, bad, got)
}

func TestOwnedWindowValidatesOwnerBeforeAnyGCRecoveryAndRequiresGuard(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		opts, clock := fixture(t)
		s, err := CreateOwned(opts, ownerDomain, []byte(`{"Height":0}`))
		require.NoError(t, err)
		ref, err := s.Append(t.Context(), envelope(opts, 1))
		require.NoError(t, err)
		require.NoError(t, s.Seal(t.Context()))
		clock.advance(25 * time.Hour)
		digest, err := referencesHash([]Reference{ref})
		require.NoError(t, err)
		require.NoError(t, writeJSON(s.root, segmentName(ref.Segment, "gc"), gcMarker{StoreID: s.id, Segment: ref.Segment, ReferencesHash: digest}))
		crashClose(t, s)
		if corrupt {
			require.NoError(t, os.WriteFile(filepath.Join(opts.Path, ownerName), []byte(`{"bad":`), 0600))
		}
		called := false
		_, err = OpenOwnedChecked(opts, ownerDomain, func(DeletionRecord) error { called = true; return ErrUnsafeGC })
		require.Error(t, err)
		require.Equal(t, !corrupt, called, "invalid ownership must be rejected before deletion authorization")
		for _, suffix := range []string{"raw", "idx", "seal", "gc"} {
			_, err = os.Lstat(filepath.Join(opts.Path, segmentName(ref.Segment, suffix)))
			require.NoError(t, err)
		}
		_, err = OpenOwnedChecked(opts, ownerDomain, nil)
		require.Error(t, err, "managed raw cannot resume GC without a coordinator")
	}
}
