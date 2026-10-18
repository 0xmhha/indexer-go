package window

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

func batchInputs(opts Options) []chainfeed.Envelope {
	var inputs []chainfeed.Envelope
	for _, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		e := envelope(opts, 1)
		e.Kind = k
		e.Payload = []byte(k)
		inputs = append(inputs, e)
	}
	return inputs
}

func TestAppendBatchDurabilityRotationAndReplay(t *testing.T) {
	for _, maxRecords := range []int{2, 8} {
		t.Run(fmt.Sprint(maxRecords), func(t *testing.T) {
			opts, _ := fixture(t)
			opts.MaxSegmentRecords = maxRecords
			s := created(t, opts)
			syncs := 0
			s.afterDataSync = func() { syncs++ }
			inputs := batchInputs(opts)
			refs, err := s.AppendBatch(context.Background(), inputs)
			require.NoError(t, err)
			require.Len(t, refs, 3)
			if maxRecords == 2 {
				require.Equal(t, 2, syncs, "sync the first segment before sealing, then the last")
			} else {
				require.Equal(t, 1, syncs, "three frames share one sync")
			}
			again, err := s.AppendBatch(context.Background(), inputs)
			require.NoError(t, err)
			require.Equal(t, refs, again, "duplicates retain identity and original retention time")
			priorSyncs := syncs
			inputs[1].Payload = []byte("changed")
			_, err = s.AppendBatch(context.Background(), inputs)
			require.ErrorIs(t, err, ErrConflict)
			require.Equal(t, priorSyncs, syncs)
			require.NoError(t, s.Close())
			reopened, err := Open(opts)
			require.NoError(t, err)
			defer func() { require.NoError(t, reopened.Close()) }()
			page, _, err := reopened.Page(context.Background(), Cursor{}, 10)
			require.NoError(t, err)
			require.Equal(t, refs, page, "unchanged raw/index format and append order")
			for i, ref := range refs {
				lease, err := reopened.Acquire(context.Background(), ref)
				require.NoError(t, err)
				got, err := lease.Envelope()
				require.NoError(t, err)
				require.Equal(t, batchInputs(opts)[i], got)
				require.NoError(t, lease.Close())
			}
		})
	}
}

func TestAppendBatchRejectsInvalidGroupBeforeWriting(t *testing.T) {
	for _, name := range []string{"empty", "tooMany", "differentBlock", "duplicateKind", "oversize"} {
		t.Run(name, func(t *testing.T) {
			opts, _ := fixture(t)
			s := created(t, opts)
			inputs := batchInputs(opts)
			switch name {
			case "empty":
				inputs = nil
			case "tooMany":
				inputs = append(inputs, inputs[0])
			case "differentBlock":
				inputs[2].Block.Number++
			case "duplicateKind":
				inputs[2].Kind = inputs[0].Kind
			case "oversize":
				inputs[2].Payload = make([]byte, opts.MaxPayloadBytes+1)
			}
			refs, err := s.AppendBatch(context.Background(), inputs)
			require.Error(t, err)
			require.Empty(t, refs)
			require.Zero(t, s.Stats().Records)
		})
	}
}

func TestAppendBatchPartialCapacityRemainsDurable(t *testing.T) {
	opts, _ := fixture(t)
	opts.MaxSegments = 1
	s := created(t, opts)
	refs, err := s.AppendBatch(context.Background(), batchInputs(opts))
	require.ErrorIs(t, err, ErrCapacity)
	require.Nil(t, refs, "failure grants no notification or ACK")
	page, _, err := s.Page(context.Background(), Cursor{}, 10)
	require.NoError(t, err)
	require.Len(t, page, 2, "completed prefix is durable before returning pressure")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = s.AppendBatchWait(ctx, batchInputs(opts))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, s.Close())
	reopened, err := Open(opts)
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()
	got, _, err := reopened.Page(context.Background(), Cursor{}, 10)
	require.NoError(t, err)
	require.Equal(t, page, got)
}

func TestAppendBatchGuardFailureCannotPublish(t *testing.T) {
	opts, _ := fixture(t)
	opts.MaxSegmentRecords = 8
	s := created(t, opts)
	s.afterDataSync = func() {
		require.NoError(t, os.WriteFile(filepath.Join(opts.Volume.Root, ".indexer-volume.json"), []byte("{}"), 0600))
	}
	refs, err := s.AppendBatchWait(context.Background(), batchInputs(opts))
	require.ErrorIs(t, err, ErrUnavailable)
	require.Nil(t, refs)
	_, _, err = s.Page(context.Background(), Cursor{}, 10)
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestBatchCapacityRetryDoesNotHideOtherErrors(t *testing.T) {
	require.True(t, capacityOnly(fmt.Errorf("pressure: %w", ErrCapacity)))
	require.True(t, capacityOnly(errors.Join(ErrCapacity, ErrCapacity)))
	for _, err := range []error{nil, context.Canceled, errors.Join(ErrCapacity, context.Canceled), errors.Join(ErrCapacity, ErrUnavailable)} {
		require.False(t, capacityOnly(err))
	}
}

func TestAppendBatchProcessCrashRecoversSyncedPrefix(t *testing.T) {
	if root := os.Getenv("INDEXER_WINDOW_BATCH_CRASH_ROOT"); root != "" {
		opts := Options{Volume: VolumeSpec{Root: root, ID: "fixture", AllowDirectory: true}, Path: filepath.Join(root, "chain"), Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, MaxBytes: 2 << 20, SegmentBytes: 32 << 10, MaxPayloadBytes: 2048, MaxSegmentRecords: 8, MaxSegments: 16}
		s, err := Create(opts)
		require.NoError(t, err)
		s.afterDataSync = func() { os.Exit(73) }
		_, _ = s.AppendBatch(context.Background(), batchInputs(opts))
		t.Fatal("sync crash hook not called")
	}
	opts, _ := fixture(t)
	opts.MaxSegmentRecords = 8
	cmd := exec.Command(os.Args[0], "-test.run=^TestAppendBatchProcessCrashRecoversSyncedPrefix$")
	cmd.Env = append(os.Environ(), "INDEXER_WINDOW_BATCH_CRASH_ROOT="+opts.Volume.Root)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, string(out))
	require.Equal(t, 73, exit.ExitCode(), string(out))
	s, err := Open(opts)
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()
	refs, _, err := s.Page(context.Background(), Cursor{}, 10)
	require.NoError(t, err)
	require.Len(t, refs, 3, "sync persisted every frame even before returning any reference")
	for i, ref := range refs {
		lease, err := s.Acquire(context.Background(), ref)
		require.NoError(t, err)
		got, err := lease.Envelope()
		require.NoError(t, err)
		require.Equal(t, batchInputs(opts)[i], got)
		require.NoError(t, lease.Close())
	}
}
