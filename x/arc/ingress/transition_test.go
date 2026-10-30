//go:build darwin || linux

package arcingress

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func transitionChild(t *testing.T, mode string) (*exec.Cmd, context.Context, context.CancelFunc, string, nativeinput.TransitionRequest) {
	t.Helper()
	parent := t.TempDir()
	require.NoError(t, os.Chmod(parent, 0700))
	dir := filepath.Join(parent, "native")
	require.NoError(t, os.Mkdir(dir, 0700))
	ready, _ := validNativeReady()
	boot := []byte("owned bootstrap bytes")
	ready.Identity.BootstrapDigest = execwire.Hash(sha256.Sum256(boot))
	require.NoError(t, os.WriteFile(filepath.Join(dir, nativeinput.BootstrapName), boot, 0600))
	writeNativeArtifact(t, filepath.Join(dir, identityName), ready.Identity)
	writeNativeArtifact(t, filepath.Join(dir, headName), ready.Head)
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".arc-native-import-receipts"), 0700))
	request := nativeinput.TransitionRequest{Version: 1, Identity: ready.Identity, Previous: ready.Head, Marker: port.ExecutionRewind{RequestGeneration: 7, From: execwire.Ref{Generation: 7, Height: ready.Head.Tip.Height, Hash: ready.Head.Tip.Hash}, Target: execwire.Ref{Generation: 8, Hash: ready.Identity.Anchor.Hash}, FromRoot: ready.Head.Tip.Root, Root: ready.Identity.Anchor.Root}}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	binary := filepath.Join(parent, "helper")
	script := "#!/bin/sh\nexport GORACE=atexit_sleep_ms=0\nexec " + quote(os.Args[0]) + " -test.run=^TestNativeTransitionChildHelper$ -- native-transition-fixture " + quote(mode) + " \"$@\"\n"
	require.NoError(t, os.WriteFile(binary, []byte(script), 0700))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	return exec.CommandContext(ctx, binary, "--datadir", dir, "--transition"), ctx, cancel, dir, request
}
func TestNativeTransitionOwnedChildBindsExactCoordinatorReplyAndDiskProof(t *testing.T) {
	t.Setenv("INDEXER_NATIVE_TEST_SECRET", "fixture-must-not-be-inherited")
	for _, mode := range []string{"valid", "wrong-reply", "wrong-disk", "pending", "oversize", "truncated", "trailing", "native-failure"} {
		t.Run(mode, func(t *testing.T) {
			cmd, ctx, cancel, dir, request := transitionChild(t, mode)
			defer cancel()
			got, err := runOwnedTransition(ctx, cancel, cmd, dir, request)
			if mode == "valid" {
				require.NoError(t, err)
				want, err := nativeinput.ExpectedTransition(request)
				require.NoError(t, err)
				require.Equal(t, want, got)
			} else {
				require.Error(t, err)
				if mode == "native-failure" {
					require.Contains(t, err.Error(), "native fixture refused: incomplete intent")
				}
			}
			require.NotNil(t, cmd.ProcessState, "every spawned child must be joined before accepting or rejecting its reply")
		})
	}
}
func TestNativeTransitionOwnedChildCancellationJoinsBeforeReturning(t *testing.T) {
	cmd, ctx, cancel, dir, request := transitionChild(t, "blocked")
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := runOwnedTransition(ctx, cancel, cmd, dir, request); done <- err }()
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(dir, "entered")); return err == nil }, 5*time.Second, time.Millisecond)
	cancel()
	require.Error(t, <-done)
	require.NotNil(t, cmd.ProcessState)
}
func TestNativeTransitionChildHelper(t *testing.T) {
	mode, dir := "", ""
	for i, a := range os.Args {
		if a == "native-transition-fixture" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
		}
		if a == "--datadir" && i+1 < len(os.Args) {
			dir = os.Args[i+1]
		}
	}
	if mode == "" {
		return
	}
	if os.Getenv("HOME") != "" || os.Getenv("INDEXER_NATIVE_TEST_SECRET") != "" {
		os.Exit(71)
	}
	var header [8]byte
	if _, err := io.ReadFull(os.Stdin, header[:]); err != nil || string(header[:4]) != "ANT1" {
		os.Exit(72)
	}
	p := make([]byte, binary.BigEndian.Uint32(header[4:]))
	if _, err := io.ReadFull(os.Stdin, p); err != nil {
		os.Exit(73)
	}
	var request nativeinput.TransitionRequest
	if nativeinput.Decode(p, &request) != nil {
		os.Exit(74)
	}
	if _, err := io.ReadAll(os.Stdin); err != nil {
		os.Exit(75)
	}
	if mode == "native-failure" {
		_, _ = os.Stderr.WriteString("native fixture refused: incomplete intent")
		os.Exit(1)
	}
	if mode == "blocked" {
		_ = os.WriteFile(filepath.Join(dir, "entered"), []byte("entered"), 0600)
		<-time.NewTimer(time.Hour).C
		os.Exit(76)
	}
	result, err := nativeinput.ExpectedTransition(request)
	if err != nil {
		os.Exit(77)
	}
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".arc-native-import-epochs"), 0700))
	writeNativeArtifact(t, filepath.Join(dir, ".arc-native-import-epochs/2.json"), result)
	h := result.Head
	if mode == "wrong-disk" {
		h.ReadEpoch++
	}
	writeNativeArtifact(t, filepath.Join(dir, headName), h)
	if mode == "pending" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".arc-native-import.pending"), []byte("pending"), 0600))
	}
	if mode == "wrong-reply" {
		result.Marker.Target.Generation++
	}
	p, _ = json.Marshal(result)
	if mode == "oversize" {
		p = make([]byte, maxMeta+1)
	}
	copy(header[:4], "ATR1")
	binary.BigEndian.PutUint32(header[4:], uint32(len(p)))
	_, _ = os.Stdout.Write(header[:])
	if mode == "truncated" {
		p = p[:2]
	}
	_, _ = os.Stdout.Write(p)
	if mode == "trailing" {
		_, _ = os.Stdout.Write([]byte{0})
	}
	os.Exit(0)
}
