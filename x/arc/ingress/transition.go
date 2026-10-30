package arcingress

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

func runNativeTransition(parent context.Context, executable, dir string, timeout time.Duration, request nativeinput.TransitionRequest) (nativeinput.Transition, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(dir) || timeout <= 0 || timeout > time.Hour {
		return nativeinput.Transition{}, ErrNative
	}
	info, err := os.Lstat(executable)
	if err != nil {
		return nativeinput.Transition{}, err
	}
	if !ownedExecutable(info) {
		return nativeinput.Transition{}, ErrNative
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return runOwnedTransition(ctx, cancel, exec.CommandContext(ctx, executable, "--datadir", dir, "--transition"), dir, request)
}
func runOwnedTransition(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, dir string, request nativeinput.TransitionRequest) (result nativeinput.Transition, err error) {
	expected, err := nativeinput.ExpectedTransition(request)
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	if len(data) == 0 || len(data) > maxMeta {
		return result, ErrNative
	}
	frame := make([]byte, 8, 8+len(data))
	copy(frame, "ANT1")
	binary.BigEndian.PutUint32(frame[4:], uint32(len(data)))
	frame = append(frame, data...)
	cmd.Env = nativeinput.ChildEnvironment()
	cmd.Stdin = bytes.NewReader(frame)
	var diagnostic boundedError
	cmd.Stderr = &diagnostic
	output, err := cmd.StdoutPipe()
	if err != nil {
		return result, err
	}
	if err = cmd.Start(); err != nil {
		_ = output.Close()
		return result, err
	}
	joined := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = cmd.Process.Kill(); _ = output.Close(); close(joined) })
	reply, readErr := io.ReadAll(io.LimitReader(output, maxMeta+9))
	if readErr != nil || len(reply) > maxMeta+8 {
		cancel()
	}
	waitErr := cmd.Wait()
	if !stop() {
		<-joined
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if readErr != nil || waitErr != nil {
		return result, nativeFailure(errors.Join(readErr, waitErr), &diagnostic)
	}
	if len(reply) < 8 || len(reply) > maxMeta+8 || string(reply[:4]) != "ATR1" || binary.BigEndian.Uint32(reply[4:8]) != uint32(len(reply)-8) || nativeinput.Decode(reply[8:], &result) != nil || result != expected {
		return result, ErrNative
	}
	// The SDK child has exited. Reacquire a managed SH to compare its exact
	// completion with durable controls before SQL recovery can proceed.
	view, err := nativestore.OpenReadView(ctx, dir)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, view.Close()) }()
	lease, err := view.Acquire(ctx)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, lease.Close()) }()
	proof, err := nativeinput.ReadHeadProof(dir, request.Identity)
	if err != nil {
		return result, err
	}
	var sealed nativeinput.Transition
	if proof.Head != result.Head || nativeinput.ReadArtifact(filepath.Join(dir, ".arc-native-import-epochs", strconv.FormatUint(result.Head.ImportEpoch, 10)+".json"), &sealed) != nil || sealed != result {
		return result, ErrNative
	}
	if err = lease.Validate(); err != nil {
		return result, err
	}
	return result, nil
}
