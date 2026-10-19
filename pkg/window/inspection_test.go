package window

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func inspectionFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[path] = fmt.Sprintf("%x/%o/%d", sha256.Sum256(b), info.Mode(), info.ModTime().UnixNano())
		}
		return nil
	}))
	return out
}

func TestInspectWindowReadsWithoutWriterLockOrRecovery(t *testing.T) {
	opts, _ := fixture(t)
	raw := created(t, opts)
	ref, err := raw.Append(context.Background(), envelope(opts, 1))
	require.NoError(t, err)
	lease, err := raw.Acquire(context.Background(), ref)
	require.NoError(t, err)
	defer func() { require.NoError(t, lease.Close()) }()
	record, err := DescribeDeletion([]Reference{ref})
	require.NoError(t, err)
	b, err := json.Marshal(record)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(opts.Path, segmentName(ref.Segment, "gc")), b, 0600))
	before := inspectionFiles(t, opts.Volume.Root)
	r, err := Inspect(context.Background(), opts, []Reference{ref})
	require.NoError(t, err)
	require.Equal(t, "unknown", r.LeaseStatus, "an active lease must never be guessed absent")
	require.Equal(t, "intent_matches", r.PendingMatch)
	require.Equal(t, before, inspectionFiles(t, opts.Volume.Root))
	require.Equal(t, ref.StoreID, r.StoreID)
}

func TestInspectWindowRejectsMissingCorruptSymlinkAndWrongIdentity(t *testing.T) {
	for _, kind := range []string{"missing", "store", "volume", "symlink", "directory_symlink", "corrupt_gc", "wrong_gc_store", "oversized", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			opts, _ := fixture(t)
			raw := created(t, opts)
			require.NoError(t, raw.Close())
			ctx := context.Background()
			switch kind {
			case "missing":
				opts.Path = filepath.Join(opts.Volume.Root, "does-not-exist")
			case "store":
				require.NoError(t, os.WriteFile(filepath.Join(opts.Path, "STORE.json"), []byte("{}"), 0600))
			case "volume":
				opts.Volume.ID = "wrong-volume"
			case "symlink":
				require.NoError(t, os.Rename(filepath.Join(opts.Path, "STORE.json"), filepath.Join(opts.Path, "other")))
				require.NoError(t, os.Symlink("other", filepath.Join(opts.Path, "STORE.json")))
			case "directory_symlink":
				alias := filepath.Join(opts.Volume.Root, "alias")
				require.NoError(t, os.Symlink(opts.Path, alias))
				opts.Path = alias
			case "corrupt_gc":
				require.NoError(t, os.WriteFile(filepath.Join(opts.Path, segmentName(1, "gc")), []byte("{bad"), 0600))
			case "wrong_gc_store":
				b, _ := json.Marshal(DeletionRecord{StoreID: common.Hash{9}, Segment: 1})
				require.NoError(t, os.WriteFile(filepath.Join(opts.Path, segmentName(1, "gc")), b, 0600))
			case "oversized":
				require.NoError(t, os.WriteFile(filepath.Join(opts.Path, completedGC), make([]byte, maxMeta+1), 0600))
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			before := inspectionFiles(t, opts.Volume.Root)
			_, err := Inspect(ctx, opts, nil)
			require.Error(t, err)
			require.Equal(t, before, inspectionFiles(t, opts.Volume.Root))
		})
	}
}

func TestInspectManagedQuotaIsReadOnlyAndMatchesStore(t *testing.T) {
	opts, _ := fixture(t)
	opts.Volume.MaxBytes = 8 << 20
	opts.Reservation = "test"
	// Use a fresh synthetic volume; never migrate an existing marker.
	opts.Volume.Root = t.TempDir()
	opts.Path = filepath.Join(opts.Volume.Root, "raw")
	require.NoError(t, InitializeVolume(opts.Volume))
	raw := created(t, opts)
	require.NoError(t, raw.Close())
	before := inspectionFiles(t, opts.Volume.Root)
	r, err := Inspect(context.Background(), opts, nil)
	require.NoError(t, err)
	require.NotNil(t, r.QuotaStoreMatch)
	require.True(t, *r.QuotaStoreMatch)
	require.Equal(t, before, inspectionFiles(t, opts.Volume.Root))
	opts.Reservation = "different"
	_, err = Inspect(context.Background(), opts, nil)
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, before, inspectionFiles(t, opts.Volume.Root))
}
