package execution

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestExecutionBlobFilesPrivateImmutableAndCanceled(t *testing.T) {
	chain, _, raw := preparedFixture(t)
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	in, out := filepath.Join(dir, "original.exr"), filepath.Join(dir, "complete.exr")
	require.NoError(t, os.WriteFile(in, raw, 0600))
	info, e := AttachBlobFiles(t.Context(), evm.New("evm"), chain, in, "", out, "retained-sidecars")
	require.NoError(t, e)
	data, e := os.ReadFile(out)
	require.NoError(t, e, "complete immutable output must be published")
	result, e := execwire.UnmarshalResult(data)
	require.NoError(t, e)
	require.NotNil(t, result.BlobBundle)
	require.Equal(t, result.Ref, info.Ref)
	stat, e := os.Stat(out)
	require.NoError(t, e)
	require.EqualValues(t, 0600, stat.Mode().Perm())
	before, e := os.ReadFile(in)
	require.NoError(t, e)
	require.Equal(t, raw, before)
	_, e = AttachBlobFiles(t.Context(), evm.New("evm"), chain, in, "", out, "retained-sidecars")
	require.Error(t, e)
	after, e := os.ReadFile(out)
	require.NoError(t, e)
	require.Equal(t, data, after, "never overwrite an existing result")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, e = AttachBlobFiles(ctx, evm.New("evm"), chain, in, "", filepath.Join(dir, "canceled.exr"), "retained-sidecars")
	require.ErrorIs(t, e, context.Canceled)
	_, e = os.Stat(filepath.Join(dir, "canceled.exr"))
	require.True(t, os.IsNotExist(e))
	require.NoError(t, os.Chmod(in, 0644))
	_, e = AttachBlobFiles(t.Context(), evm.New("evm"), chain, in, "", filepath.Join(dir, "public.exr"), "retained-sidecars")
	require.Error(t, e)
}

func TestExecutionBlobFilesRejectLinksPublicParentsAndOversize(t *testing.T) {
	chain, _, raw := preparedFixture(t)
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	input := filepath.Join(dir, "input.exr")
	require.NoError(t, os.WriteFile(input, raw, 0600))
	link := filepath.Join(dir, "link.exr")
	require.NoError(t, os.Symlink(input, link))
	_, e := AttachBlobFiles(t.Context(), evm.New("evm"), chain, link, "", filepath.Join(dir, "link-output.exr"), "retained-sidecars")
	require.Error(t, e)
	require.NoError(t, os.Chmod(dir, 0755))
	_, e = AttachBlobFiles(t.Context(), evm.New("evm"), chain, input, "", filepath.Join(dir, "public-output.exr"), "retained-sidecars")
	require.Error(t, e)
	require.NoError(t, os.Chmod(dir, 0700))
	large := filepath.Join(dir, "too-large.exr")
	f, e := os.OpenFile(large, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	require.NoError(t, e)
	require.NoError(t, f.Truncate(execwire.MaxBlockBytes+1))
	require.NoError(t, f.Close())
	_, e = AttachBlobFiles(t.Context(), evm.New("evm"), chain, large, "", filepath.Join(dir, "large-output.exr"), "retained-sidecars")
	require.ErrorIs(t, e, execwire.ErrLimit)
	for _, name := range []string{"link-output.exr", "public-output.exr", "large-output.exr"} {
		_, e = os.Lstat(filepath.Join(dir, name))
		require.True(t, os.IsNotExist(e))
	}
	unchanged, e := os.ReadFile(input)
	require.NoError(t, e)
	require.Equal(t, raw, unchanged)
}
