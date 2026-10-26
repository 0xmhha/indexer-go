package collector

import (
	"context"
	"encoding/binary"
	"encoding/json"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
)

type frozen struct {
	Chain   execwire.Chain
	Payload []byte
}

func fixture(t *testing.T) (frozen, execwire.Result) {
	t.Helper()
	return fixtureNamed(t, "arc-storage")
}
func fixtureNamed(t *testing.T, name string) (frozen, execwire.Result) {
	t.Helper()
	p, err := os.ReadFile("../../../testdata/execution/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var f frozen
	if err = json.Unmarshal(p, &f); err != nil {
		t.Fatal(err)
	}
	r, err := execwire.UnmarshalResult(f.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return f, r
}

func helperBackend(t *testing.T, mode string) (*Backend, execwire.Result) {
	t.Helper()
	f, r := fixture(t)
	if strings.HasPrefix(mode, "proof-") {
		f, r = fixtureNamed(t, "arc-state-proofs")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestArcBackendHelper$")
	cmd.Env = append(os.Environ(), "INDEXER_ARC_BACKEND_HELPER="+mode, "GORACE=atexit_sleep_ms=0")
	b, err := startBackend(ctx, cancel, cmd, f.Chain, 5*time.Second)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if strings.HasPrefix(mode, "proof-") {
		var bundle stateproof.Bundle
		if err = json.Unmarshal(r.StateProofBundle.Response, &bundle); err != nil {
			t.Fatal(err)
		}
		b.requiredStateProofs = &bundle.Selection
	}
	t.Cleanup(func() { _ = b.Close() })
	return b, r
}

func TestArcBackendRejectsWrongNativeIdentity(t *testing.T) {
	f, _ := fixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestArcBackendHelper$")
	cmd.Env = append(os.Environ(), "INDEXER_ARC_BACKEND_HELPER=identity", "GORACE=atexit_sleep_ms=0")
	b, err := startBackend(ctx, cancel, cmd, f.Chain, 5*time.Second)
	if err == nil {
		_ = b.Close()
		t.Fatal("wrong pinned producer revision accepted")
	}
	if cmd.ProcessState == nil {
		t.Fatal("rejected child was not joined")
	}
}

func TestArcBackendNativeProtocolAndRetainedReplay(t *testing.T) {
	b, r := helperBackend(t, "normal")
	head, err := b.Head(t.Context())
	if err != nil || head.Height != r.Ref.Height || head.Hash != r.Ref.Hash {
		t.Fatalf("head: %+v %v", head, err)
	}
	hash, err := b.Canonical(t.Context(), r.Ref.Height)
	if err != nil || hash != r.Ref.Hash {
		t.Fatalf("canonical: %v", err)
	}
	got, err := b.Replay(t.Context(), r.Ref.Height, r.Ref.Generation)
	if err != nil || got.Ref != r.Ref || got.Parent != r.Parent {
		t.Fatalf("replay: %v", err)
	}
	for _, mode := range []string{"id", "oversize", "hash", "truncated", "replay"} {
		t.Run(mode, func(t *testing.T) {
			bad, r := helperBackend(t, mode)
			if mode == "replay" {
				_, err = bad.Replay(t.Context(), r.Ref.Height, r.Ref.Generation)
			} else {
				_, err = bad.Head(t.Context())
			}
			if err == nil {
				t.Fatal("invalid native response accepted")
			}
			select {
			case <-bad.done:
			default:
				t.Fatal("failed child not joined")
			}
		})
	}
}

func TestArcBackendCancellationAndConcurrentCloseJoin(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "entered")
	t.Setenv("INDEXER_ARC_HELPER_READY", marker)
	b, _ := helperBackend(t, "block")
	ctx, cancel := context.WithCancel(t.Context())
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() { _, _ = b.Head(ctx) })
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
wait:
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-tick.C:
		case <-timer.C:
			t.Fatal("child did not enter blocking read")
			break wait
		}
	}
	cancel()
	workers.Wait()
	var closers sync.WaitGroup
	for range 3 {
		closers.Go(func() { _ = b.Close() })
	}
	closers.Wait()
	select {
	case <-b.done:
	default:
		t.Fatal("child survives cancel/close")
	}
	if _, err := b.Head(t.Context()); err == nil {
		t.Fatal("closed backend reused")
	}
}

// A test-owned subprocess, never an Arc node or a native database.
func TestArcBackendHelper(t *testing.T) {
	mode := os.Getenv("INDEXER_ARC_BACKEND_HELPER")
	if mode == "" {
		return
	}
	f, r := fixture(t)
	if strings.HasPrefix(mode, "proof-") {
		f, r = fixtureNamed(t, "arc-state-proofs")
	}
	announced := false
	for {
		var size [4]byte
		if _, err := io.ReadFull(os.Stdin, size[:]); err != nil {
			os.Exit(0)
		}
		p := make([]byte, binary.BigEndian.Uint32(size[:]))
		if _, err := io.ReadFull(os.Stdin, p); err != nil {
			os.Exit(2)
		}
		var request nativeRequest
		if err := json.Unmarshal(p, &request); err != nil {
			os.Exit(2)
		}
		var payload []byte
		switch request.Op {
		case "identity":
			identity := nativeIdentity{ChainID: f.Chain.ID, Genesis: f.Chain.Genesis, Revision: arcRevision}
			if mode == "identity" {
				identity.Revision = "wrong"
			}
			if mode == "idle" {
				identity.ChainID = "5042002"
				identity.Genesis = execwire.Hash{1}
			}
			payload, _ = json.Marshal(identity)
		case "head", "canonical":
			ref := nativeHead{Height: r.Ref.Height, Hash: r.Ref.Hash}
			if request.Op == "canonical" && request.Height == r.Ref.Height-1 {
				ref.Height, ref.Hash = request.Height, r.Parent
			}
			if mode == "hash" {
				ref.Hash = execwire.Hash{}
			}
			if mode == "idle" {
				ref.Height = request.Height
				ref.Hash = execwire.Hash{1}
				if request.Op == "head" && !announced {
					work := "entered"
					for i, arg := range os.Args {
						if arg == "--scratch-dir" && i+1 < len(os.Args) {
							work = os.Args[i+1]
						}
					}
					marker := os.Getenv("INDEXER_ARC_HELPER_READY")
					_ = os.WriteFile(marker+".tmp", []byte(work), 0600)
					_ = os.Rename(marker+".tmp", marker)
					announced = true
				}
			}
			payload, _ = json.Marshal(ref)
		case "replay", "replayproofs":
			if request.Op == "replayproofs" {
				if request.StateProofs == nil {
					os.Exit(2)
				}
				var bundle stateproof.Bundle
				if strings.HasPrefix(mode, "proof-") {
					if err := json.Unmarshal(r.StateProofBundle.Response, &bundle); err != nil {
						os.Exit(2)
					}
					_, want, e := bundle.Selection.Normalize()
					if e != nil {
						os.Exit(2)
					}
					_, got, e := request.StateProofs.Normalize()
					if e != nil || got != want {
						os.Exit(2)
					}
				}
			}
			payload = f.Payload
			switch mode {
			case "proof-legacy":
				r.StateProofBundle = nil
				payload, _ = execwire.MarshalResult(r)
			case "proof-provider":
				r.StateProofBundle.Provider = "wrong"
				payload, _ = execwire.MarshalResult(r)
			case "proof-corrupt":
				r.StateProofBundle.Response = []byte(`{"version":1}`)
				payload, _ = execwire.MarshalResult(r)
			}
			if mode == "replay" {
				payload = []byte("EXR1bad")
			}
		default:
			os.Exit(2)
		}
		var header [17]byte
		copy(header[:4], "AXS1")
		binary.BigEndian.PutUint64(header[4:12], request.ID)
		header[12] = operationCode(request.Op)
		binary.BigEndian.PutUint32(header[13:], uint32(len(payload)))
		if request.Op != "identity" {
			switch mode {
			case "id":
				header[11]++
			case "oversize":
				binary.BigEndian.PutUint32(header[13:], 1<<30)
			case "truncated":
				_, _ = os.Stdout.Write(header[:5])
				os.Exit(0)
			case "block", "proof-block":
				_, _ = os.Stdout.Write(header[:1])
				_ = os.WriteFile(os.Getenv("INDEXER_ARC_HELPER_READY"), []byte("entered"), 0600)
				_, _ = io.Copy(io.Discard, os.Stdin)
				os.Exit(0)
			}
		}
		_, _ = os.Stdout.Write(header[:])
		_, _ = os.Stdout.Write(payload)
	}
}
