package window

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func objectFixture(t *testing.T) ObjectOptions {
	t.Helper()
	opts, _ := fixture(t)
	return ObjectOptions{Volume: opts.Volume, Path: filepath.Join(opts.Volume.Root, "objects"), Chain: opts.Chain, MaxBytes: 40 << 20, MaxObjectBytes: 16 << 20, MaxObjects: 16}
}
func TestObjectsStreamRestartAndGuard(t *testing.T) {
	o := objectFixture(t)
	s, e := CreateObjects(o)
	require.NoError(t, e)
	// An encoded object larger than the capture DB's 8 MiB inline limit.
	data := bytes.Repeat([]byte("exact bytes"), 1<<20)
	r, e := s.Put(context.Background(), sha256.Sum256(data), int64(len(data)), bytes.NewReader(data))
	require.NoError(t, e)
	u, e := s.Usage(context.Background())
	require.NoError(t, e)
	require.GreaterOrEqual(t, u.FileBytes, int64(len(data))+objectHeader)
	_, e = OpenObjects(o)
	require.ErrorIs(t, e, ErrLocked)
	require.NoError(t, s.Close())
	s, e = OpenObjects(o)
	require.NoError(t, e)
	defer func() { require.NoError(t, s.Close()) }()
	var b bytes.Buffer
	require.NoError(t, s.Copy(context.Background(), r, &b))
	require.Equal(t, data, b.Bytes())
	all, e := s.Candidates(context.Background(), common.Hash{}, 1)
	require.NoError(t, e)
	require.Equal(t, []ObjectRef{r}, all)
	all, e = s.Candidates(context.Background(), r.ID, 1)
	require.NoError(t, e)
	require.Empty(t, all)
	wrong := r
	wrong.Digest[0] ^= 1
	require.ErrorIs(t, s.Verify(context.Background(), wrong), ErrConflict)
	require.ErrorIs(t, s.Collect(context.Background(), r, nil), ErrUnsafeGC)
	// Corruption is detected from actual disk bytes, not a metadata ACK.
	f, e := os.OpenFile(filepath.Join(o.Path, objectName(r.ID)), os.O_RDWR, 0)
	require.NoError(t, e)
	_, e = f.WriteAt([]byte{0}, objectHeader+10)
	require.NoError(t, e)
	require.NoError(t, f.Close())
	require.ErrorIs(t, s.Verify(context.Background(), r), ErrCorrupt)
}
func TestObjectsBoundsFailureRecoveryAndIdentity(t *testing.T) {
	o := objectFixture(t)
	o.MaxObjects = 1
	s, e := CreateObjects(o)
	require.NoError(t, e)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = s.Put(ctx, sha256.Sum256([]byte("x")), 1, bytes.NewBufferString("x"))
	require.ErrorIs(t, e, context.Canceled)
	for _, data := range []string{"", "xx", "y"} {
		_, e = s.Put(context.Background(), sha256.Sum256([]byte("x")), 1, bytes.NewBufferString(data))
		require.ErrorIs(t, e, ErrCorrupt)
	}
	entries, e := os.ReadDir(o.Path)
	require.NoError(t, e)
	require.Len(t, entries, 2)
	r, e := s.Put(context.Background(), sha256.Sum256([]byte("x")), 1, bytes.NewBufferString("x"))
	require.NoError(t, e)
	_, e = s.Put(context.Background(), r.Digest, 1, bytes.NewBufferString("x"))
	require.ErrorIs(t, e, ErrCapacity)
	require.NoError(t, s.Close())
	// A crash can leave a partial .tmp. Only the exact private temporary name
	// is discarded; an arbitrary file or changed store type is refused.
	tmp := filepath.Join(o.Path, common.Hash{4}.Hex()[2:]+".tmp")
	require.NoError(t, os.WriteFile(tmp, []byte("partial"), 0600))
	s, e = OpenObjects(o)
	require.NoError(t, e)
	_, e = os.Stat(tmp)
	require.True(t, errors.Is(e, os.ErrNotExist))
	require.NoError(t, s.Verify(context.Background(), r))
	require.NoError(t, s.Close())
	_, e = Open(Options{Volume: o.Volume, Path: o.Path, Chain: o.Chain, MaxBytes: o.MaxBytes, SegmentBytes: 9 << 20, MaxSegmentRecords: 1})
	require.ErrorIs(t, e, ErrConflict)
	s, e = OpenObjects(o)
	require.NoError(t, e)
	require.NoError(t, os.Rename(o.Path, o.Path+"-old"))
	require.NoError(t, os.Mkdir(o.Path, 0700))
	require.ErrorIs(t, s.Copy(context.Background(), r, io.Discard), ErrUnavailable)
	require.NoError(t, s.Close()) // faulted close releases handles without writes
}

func TestObjectsShareDurableVolumeQuota(t *testing.T) {
	root := t.TempDir()
	raw := managedOptions(root, "raw")
	raw.Volume.MaxBytes = 44 << 20
	require.NoError(t, InitializeVolume(raw.Volume))
	w, e := Create(raw)
	require.NoError(t, e)
	o := ObjectOptions{Volume: raw.Volume, Path: filepath.Join(root, "permanent"), Reservation: "permanent", Chain: raw.Chain, MaxBytes: 40 << 20, MaxObjectBytes: 16 << 20}
	s, e := CreateObjects(o)
	require.NoError(t, e)
	require.NoError(t, s.Close())
	require.NoError(t, w.Close())
	// The permanent area's quota remains allocated while offline.
	extra := managedOptions(root, "extra")
	extra.Volume = raw.Volume
	_, e = Create(extra)
	require.ErrorIs(t, e, ErrCapacity)
	s, e = OpenObjects(o)
	require.NoError(t, e)
	u, e := s.Usage(context.Background())
	require.NoError(t, e)
	require.EqualValues(t, 42<<20, u.VolumeAllocatedBytes)
	require.NoError(t, s.Close())
	wrong := o
	wrong.MaxBytes--
	_, e = OpenObjects(wrong)
	require.ErrorIs(t, e, ErrConflict)
}

func TestObjectsCrashDurableUpload(t *testing.T) {
	if encoded := os.Getenv("INDEXER_OBJECT_CRASH_OPTIONS"); encoded != "" {
		var o ObjectOptions
		require.NoError(t, json.Unmarshal([]byte(encoded), &o))
		s, e := OpenObjects(o)
		require.NoError(t, e)
		data := bytes.Repeat([]byte{7}, 32<<10)
		_, e = s.Put(context.Background(), sha256.Sum256(data), int64(len(data)), bytes.NewReader(data))
		require.NoError(t, e)
		os.Exit(31) // durable upload, no DB publication, no Close/unlock callback
	}
	o := objectFixture(t)
	s, e := CreateObjects(o)
	require.NoError(t, e)
	require.NoError(t, s.Close())
	encoded, e := json.Marshal(o)
	require.NoError(t, e)
	cmd := exec.Command(os.Args[0], "-test.run=^TestObjectsCrashDurableUpload$")
	cmd.Env = append(os.Environ(), "INDEXER_OBJECT_CRASH_OPTIONS="+string(encoded))
	out, e := cmd.CombinedOutput()
	var exit *exec.ExitError
	require.ErrorAs(t, e, &exit, string(out))
	require.Equal(t, 31, exit.ExitCode(), string(out))
	s, e = OpenObjects(o)
	require.NoError(t, e)
	defer func() { require.NoError(t, s.Close()) }()
	refs, e := s.Candidates(context.Background(), common.Hash{}, 256)
	require.NoError(t, e)
	require.Len(t, refs, 1)
	require.Equal(t, sha256.Sum256(bytes.Repeat([]byte{7}, 32<<10)), [32]byte(refs[0].Digest))
	require.NoError(t, s.Verify(context.Background(), refs[0]))
}
