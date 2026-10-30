package arcingress

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func TestNativeClientHelper(t *testing.T) {
	mode := ""
	for i, a := range os.Args {
		if a == "native-fixture" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
		}
	}
	if mode == "" {
		return
	}
	if os.Getenv("INDEXER_NATIVE_TEST_SECRET") != "" || os.Getenv("HOME") != "" || os.Getenv("DYLD_INSERT_LIBRARIES") != "" {
		os.Exit(71)
	}
	r, _ := validNativeReady()
	r.Head = nativeHead{Version: 1, Instance: r.Identity.Instance, ImportEpoch: 1, ReadEpoch: 1, Tip: r.Identity.Anchor}
	encoded, _ := json.Marshal(r)
	_, _ = os.Stdout.Write(protocolReply("AWH1", 0, encoded))
	var header [16]byte
	if _, err := io.ReadFull(os.Stdin, header[:]); err != nil {
		return
	}
	meta := make([]byte, binary.BigEndian.Uint32(header[4:8]))
	if _, err := io.ReadFull(os.Stdin, meta); err != nil {
		os.Exit(72)
	}
	block := make([]byte, binary.BigEndian.Uint32(header[8:12]))
	rs := make([]byte, binary.BigEndian.Uint32(header[12:]))
	_, _ = io.ReadFull(os.Stdin, block)
	_, _ = io.ReadFull(os.Stdin, rs)
	var m importMeta
	if json.Unmarshal(meta, &m) != nil {
		os.Exit(73)
	}
	if mode == "blocked" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	_, digest, _ := encodeInput(m, block, rs)
	receipt := importReceipt{Version: 1, Identity: r.Identity, RequestID: m.RequestID, ImportEpoch: m.ImportEpoch, ReadEpoch: 2, Height: m.Height, Hash: m.Hash, Parent: m.Parent, ParentRoot: m.ParentRoot, Root: m.ParentRoot, ReceiptsRoot: execwire.Hash(types.EmptyReceiptsHash), InputDigest: digest}
	id := m.RequestID
	if mode == "wrong-id" {
		id++
	}
	encoded, _ = json.Marshal(receipt)
	_, _ = os.Stdout.Write(protocolReply("AWR1", id, encoded))
	_, _ = io.Copy(io.Discard, os.Stdin)
}
func fixtureClient(t *testing.T, mode string) (*nativeClient, *exec.Cmd, *os.File) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeClientHelper$", "--", "native-fixture", mode)
	lease, err := os.OpenFile(filepath.Join(t.TempDir(), "lease"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	require.NoError(t, err)
	c, err := startOwnedNative(ctx, cancel, cmd, lease, 5*time.Second)
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c, cmd, lease
}
func TestNativeOwnedChildFiltersEnvironmentAndValidatesCompletion(t *testing.T) {
	t.Setenv("INDEXER_NATIVE_TEST_SECRET", "must-not-be-inherited")
	c, cmd, lease := fixtureClient(t, "valid")
	m := protocolMeta()
	m.ParentRoot = c.head.Tip.Root
	r, err := c.Import(t.Context(), m, []byte{0xc0}, []byte{0xc0}, m.ParentRoot, execwire.Hash(types.EmptyReceiptsHash))
	require.NoError(t, err)
	require.Equal(t, uint64(1), r.Height)
	c.Close()
	require.NotNil(t, cmd.ProcessState)
	_, err = lease.Stat()
	require.ErrorIs(t, err, os.ErrClosed)
}
func TestNativeOwnedChildWithoutLocaleStillFiltersPrivateEnvironment(t *testing.T) {
	for _, key := range []string{"LANG", "LC_ALL", "TZ"} {
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}
	t.Setenv("INDEXER_NATIVE_TEST_SECRET", "must-not-reach-child")
	c, _, _ := fixtureClient(t, "valid")
	c.Close()
}
func TestNativeOwnedChildWrongReplyAndCancellationJoinBeforeLeaseRelease(t *testing.T) {
	for _, mode := range []string{"wrong-id", "blocked"} {
		t.Run(mode, func(t *testing.T) {
			c, cmd, lease := fixtureClient(t, mode)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				m := protocolMeta()
				_, err := c.Import(ctx, m, []byte{0xc0}, []byte{0xc0}, m.ParentRoot, execwire.Hash(types.EmptyReceiptsHash))
				result <- err
			}()
			if mode == "blocked" {
				cancel()
			}
			select {
			case err := <-result:
				require.Error(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("owned native cancellation did not join")
			}
			c.Close()
			require.NotNil(t, cmd.ProcessState)
			_, err := lease.Stat()
			require.ErrorIs(t, err, os.ErrClosed)
		})
	}
}
