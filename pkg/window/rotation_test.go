package window

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

func TestRotationCrashBoundaries(t *testing.T) {
	if root := os.Getenv("INDEXER_ROTATION_CRASH_ROOT"); root != "" {
		opts := Options{Volume: VolumeSpec{Root: root, ID: "fixture", AllowDirectory: true}, Path: filepath.Join(root, "chain"), Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, MaxBytes: 2 << 20, SegmentBytes: 32 << 10, MaxPayloadBytes: 2048, MaxSegmentRecords: 2, MaxSegments: 16}
		s, err := Create(opts)
		require.NoError(t, err)
		for n := uint64(1); n <= 2; n++ {
			_, err := s.Append(context.Background(), envelope(opts, n))
			require.NoError(t, err)
		}
		old, err := os.ReadFile(filepath.Join(opts.Path, "STORE.json"))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(root, "before-rotation.json"), old, 0600))
		s.afterRotateStep = func(step string) {
			if step == os.Getenv("INDEXER_ROTATION_CRASH_STEP") {
				os.Exit(74)
			}
		}
		_, _ = s.Append(context.Background(), envelope(opts, 3))
		t.Fatal("rotation crash hook not reached")
	}
	for _, step := range []string{"indexSynced", "metadataPrepared", "sealRenamed", "metaRenamed", "metadataSynced", "nextCreated"} {
		masks := 1
		if step == "metaRenamed" {
			masks = 4 // each subset of the two directory changes may survive
		}
		for lost := 0; lost < masks; lost++ {
			t.Run(step+"/lost"+strconv.Itoa(lost), func(t *testing.T) {
				opts, _ := fixture(t)
				cmd := exec.Command(os.Args[0], "-test.run=^TestRotationCrashBoundaries$")
				cmd.Env = append(os.Environ(), "INDEXER_ROTATION_CRASH_ROOT="+opts.Volume.Root, "INDEXER_ROTATION_CRASH_STEP="+step)
				out, err := cmd.CombinedOutput()
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit, string(out))
				require.Equal(t, 74, exit.ExitCode(), string(out))
				if step != "nextCreated" {
					_, err := os.Lstat(filepath.Join(opts.Path, segmentName(2, "raw")))
					require.ErrorIs(t, err, os.ErrNotExist, "next raw entry must wait for the shared metadata barrier")
				}
				// Model allowable power-loss snapshots BEFORE the parent sync.
				// The index is already durable; seal and ID reservation are independent.
				if lost&1 != 0 {
					require.NoError(t, os.Remove(filepath.Join(opts.Path, segmentName(1, "seal"))))
				}
				if lost&2 != 0 {
					old, err := os.ReadFile(filepath.Join(opts.Volume.Root, "before-rotation.json"))
					require.NoError(t, err)
					require.NoError(t, os.WriteFile(filepath.Join(opts.Path, "STORE.json"), old, 0600))
				}
				s, err := Open(opts)
				require.NoError(t, err)
				defer func() { require.NoError(t, s.Close()) }()
				refs, _, err := s.Page(context.Background(), Cursor{}, 10)
				require.NoError(t, err)
				require.Len(t, refs, 2, "only the previously durable prefix is published")
				for i, ref := range refs {
					lease, err := s.Acquire(context.Background(), ref)
					require.NoError(t, err)
					got, err := lease.Envelope()
					require.NoError(t, err)
					require.Equal(t, envelope(opts, uint64(i+1)), got)
					require.NoError(t, lease.Close())
				}
				_, err = s.Append(context.Background(), envelope(opts, 3))
				require.NoError(t, err, "resume without replacing the retained prefix")
				require.Equal(t, 3, s.Stats().Records)
			})
		}
	}
}

func TestRotationPreparesPrivateMetadataBeforePublishing(t *testing.T) {
	opts, _ := fixture(t)
	s := created(t, opts)
	for n := uint64(1); n <= 2; n++ {
		_, err := s.Append(t.Context(), envelope(opts, n))
		require.NoError(t, err)
	}
	readMeta := func(name string) storeMeta {
		data, err := os.ReadFile(filepath.Join(opts.Path, name))
		require.NoError(t, err)
		var meta storeMeta
		require.NoError(t, json.Unmarshal(data, &meta))
		return meta
	}
	before := readMeta("STORE.json")
	var steps []string
	s.afterRotateStep = func(step string) {
		steps = append(steps, step)
		if step == "metadataPrepared" {
			data, err := os.ReadFile(filepath.Join(opts.Path, segmentName(1, "seal.tmp")))
			require.NoError(t, err)
			var seal segment
			require.NoError(t, json.Unmarshal(data, &seal))
			require.EqualValues(t, 2, seal.Count)
			require.Equal(t, before, readMeta("STORE.json"), "temporary writes grant no reservation")
			next := readMeta("STORE.json.tmp")
			require.Equal(t, before.ID, next.ID)
			require.Equal(t, before.Next+1, next.Next)
			_, err = os.Lstat(filepath.Join(opts.Path, segmentName(1, "seal")))
			require.ErrorIs(t, err, os.ErrNotExist)
		}
		if step == "sealRenamed" {
			require.Equal(t, before, readMeta("STORE.json"))
			require.Equal(t, before.Next+1, readMeta("STORE.json.tmp").Next)
		}
		if step != "nextCreated" {
			_, err := os.Lstat(filepath.Join(opts.Path, segmentName(before.Next, "raw")))
			require.ErrorIs(t, err, os.ErrNotExist, "raw creation follows the original metadata barrier")
		}
	}
	_, err := s.Append(t.Context(), envelope(opts, 3))
	require.NoError(t, err)
	require.Equal(t, []string{"indexSynced", "metadataPrepared", "sealRenamed", "metaRenamed", "metadataSynced", "nextCreated"}, steps)
}

func TestRotationPreparedMetadataFailureKeepsPrefix(t *testing.T) {
	for _, stage := range []string{"prepareMeta", "renameSeal"} {
		t.Run(stage, func(t *testing.T) {
			opts, _ := fixture(t)
			s := created(t, opts)
			for n := uint64(1); n <= 2; n++ {
				_, err := s.Append(t.Context(), envelope(opts, n))
				require.NoError(t, err)
			}
			before, err := os.ReadFile(filepath.Join(opts.Path, "STORE.json"))
			require.NoError(t, err)
			bad := filepath.Join(opts.Path, "STORE.json.tmp")
			when := "indexSynced"
			if stage == "renameSeal" {
				bad = filepath.Join(opts.Path, segmentName(1, "seal"))
				when = "metadataPrepared"
			}
			s.afterRotateStep = func(step string) {
				if step == when {
					require.NoError(t, os.Mkdir(bad, 0700))
					require.NoError(t, os.WriteFile(filepath.Join(bad, "block"), []byte("occupied"), 0600))
				}
			}
			ref, err := s.Append(t.Context(), envelope(opts, 3))
			require.ErrorIs(t, err, ErrUnavailable)
			require.Zero(t, ref)
			after, err := os.ReadFile(filepath.Join(opts.Path, "STORE.json"))
			require.NoError(t, err)
			require.Equal(t, before, after, "failed prepare/sync/rename grants no durable next ID")
			_, err = os.Lstat(filepath.Join(opts.Path, segmentName(2, "raw")))
			require.ErrorIs(t, err, os.ErrNotExist)
			require.NoError(t, s.Close())
			require.NoError(t, os.Remove(filepath.Join(bad, "block")))
			require.NoError(t, os.Remove(bad))
			reopened, err := Open(opts)
			require.NoError(t, err)
			defer func() { require.NoError(t, reopened.Close()) }()
			require.Equal(t, 2, reopened.Stats().Records)
			for _, name := range []string{"STORE.json.tmp", segmentName(1, "seal.tmp")} {
				_, err := os.Lstat(filepath.Join(opts.Path, name))
				require.ErrorIs(t, err, os.ErrNotExist, "unpublished temporaries are discarded")
			}
			_, err = reopened.Append(t.Context(), envelope(opts, 3))
			require.NoError(t, err)
			require.Equal(t, 3, reopened.Stats().Records)
		})
	}
}

func TestRotationCreationFailurePoisonsAndPreservesPrefix(t *testing.T) {
	opts, _ := fixture(t)
	s := created(t, opts)
	for n := uint64(1); n <= 2; n++ {
		_, err := s.Append(context.Background(), envelope(opts, n))
		require.NoError(t, err)
	}
	bad := filepath.Join(opts.Path, segmentName(2, "raw"))
	s.afterRotateStep = func(step string) {
		if step == "metadataSynced" {
			require.NoError(t, os.Mkdir(bad, 0700))
		}
	}
	ref, err := s.Append(context.Background(), envelope(opts, 3))
	require.ErrorIs(t, err, ErrUnavailable)
	require.Zero(t, ref)
	_, _, err = s.Page(context.Background(), Cursor{}, 10)
	require.ErrorIs(t, err, ErrUnavailable)
	require.NoError(t, s.Close())
	require.NoError(t, os.Remove(bad))
	reopened, err := Open(opts)
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Equal(t, 2, reopened.Stats().Records)
	ref, err = reopened.Append(context.Background(), envelope(opts, 3))
	require.NoError(t, err)
	require.EqualValues(t, 3, ref.Segment, "a durably reserved unused ID can be skipped, never reused")
}

func TestExplicitSealDoesNotReserveNextID(t *testing.T) {
	opts, _ := fixture(t)
	s := created(t, opts)
	_, err := s.Append(context.Background(), envelope(opts, 1))
	require.NoError(t, err)
	next := s.next
	require.NoError(t, s.Seal(context.Background()))
	require.Equal(t, next, s.next)
}
