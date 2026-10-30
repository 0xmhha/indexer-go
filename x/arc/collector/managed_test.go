//go:build darwin || linux

package collector

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func managedControl(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	b, err := json.Marshal(struct {
		Data   json.RawMessage
		Digest execwire.Hash
	}{data, execwire.Hash(sha256.Sum256(data))})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0600))
}
func managedFixture(t *testing.T, mode string) (Options, nativeinput.Proof) {
	t.Helper()
	parent := t.TempDir()
	require.NoError(t, os.Chmod(parent, 0700))
	dir := filepath.Join(parent, "native")
	require.NoError(t, os.Mkdir(dir, 0700))
	boot := []byte("owned bootstrap fixture")
	i := nativeinput.Identity{Version: 1, Instance: execwire.Hash{3}, ChainID: 5042002, Genesis: execwire.Hash{1}, ArcRevision: nativeinput.ArcRevision, RethRevision: nativeinput.RethRevision, Layout: "reth-native-v2", BootstrapDigest: execwire.Hash(sha256.Sum256(boot)), Anchor: nativeinput.Ref{Hash: execwire.Hash{1}, Root: execwire.Hash{2}}}
	r := &nativeinput.Receipt{Version: 1, Identity: i, RequestID: 1, ImportEpoch: 1, ReadEpoch: 2, Height: 1, Hash: execwire.Hash{4}, Parent: i.Anchor.Hash, ParentRoot: i.Anchor.Root, Root: execwire.Hash{5}, ReceiptsRoot: execwire.Hash{7}, InputDigest: execwire.Hash{8}}
	h := nativeinput.Head{Version: 1, Instance: i.Instance, ImportEpoch: 1, ReadEpoch: 2, RequestID: 1, Tip: nativeinput.Ref{Height: 1, Hash: r.Hash, Root: r.Root}}
	p := nativeinput.Proof{Version: 1, Identity: i, Head: h, Ref: h.Tip, Receipt: r}
	require.NoError(t, os.WriteFile(filepath.Join(dir, nativeinput.BootstrapName), boot, 0600))
	managedControl(t, filepath.Join(dir, nativeinput.IdentityName), i)
	managedControl(t, filepath.Join(dir, nativeinput.HeadName), h)
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".arc-native-import-receipts"), 0700))
	managedControl(t, filepath.Join(dir, ".arc-native-import-receipts/1-1.json"), r)
	b, err := json.Marshal(p)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(parent, "proof.json"), b, 0600))
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	binary := filepath.Join(parent, "adapter")
	script := "#!/bin/sh\nexport GORACE=atexit_sleep_ms=0\nexec " + quote(os.Args[0]) + " -test.run=^TestManagedAdapterHelper$ -- managed-fixture " + quote(parent) + " " + quote(mode) + " \"$@\"\n"
	require.NoError(t, os.WriteFile(binary, []byte(script), 0700))
	// This is a process/protocol correctness fixture, not a startup-latency SLO.
	// Cancellation/close tests use explicit barriers instead of waiting this out.
	return Options{Executable: binary, DataDir: dir, Chain: execwire.Chain{ID: "5042002", Genesis: i.Genesis}, Timeout: 30 * time.Second, Managed: &i}, p
}
func TestManagedAdapterRequiresAXM1AndCurrentProofBeforeAcceptingHead(t *testing.T) {
	o, p := managedFixture(t, "managed")
	b, err := Start(t.Context(), o)
	require.NoError(t, err, "managed proof must be accepted under inherited SH through complete decode")
	t.Cleanup(func() { require.NoError(t, b.Close()) })
	// Copy the caller's expected identity; later mutations cannot change policy.
	o.Managed.Instance[0]++
	h, err := b.Head(t.Context())
	require.NoError(t, err)
	require.Equal(t, p.Head.Tip.Hash, h.Hash)
	hash, err := b.Canonical(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, p.Identity.Anchor.Hash, hash)
	changed := p.Head
	changed.ReadEpoch++
	managedControl(t, filepath.Join(o.DataDir, nativeinput.HeadName), changed)
	_, err = b.Head(t.Context())
	require.Error(t, err, "stale response cannot outlive its completed native head")
	select {
	case <-b.done:
	default:
		t.Fatal("rejected child not joined")
	}
}
func TestManagedAdapterRejectsLegacyDowngradeAndMalformedProofBeforeReady(t *testing.T) {
	for _, mode := range []string{"legacy", "wrong-proof", "oversize-proof", "truncated-proof"} {
		t.Run(mode, func(t *testing.T) {
			o, _ := managedFixture(t, mode)
			b, err := Start(t.Context(), o)
			if b != nil {
				require.NoError(t, b.Close())
			}
			require.Error(t, err, "managed store cannot downgrade its read contract")
		})
	}
}

func TestManagedAdapterStartupIdentityFailurePreservesCauseAndBoundedDiagnostic(t *testing.T) {
	o, _ := managedFixture(t, "startup-failure")
	b, err := Start(t.Context(), o)
	require.Nil(t, b)
	require.ErrorIs(t, err, ErrNative)
	require.ErrorContains(t, err, "startup identity")
	require.ErrorContains(t, err, "owned fixture identity unavailable")
}
func TestManagedAdapterHelper(t *testing.T) {
	dir, mode := "", ""
	for i, arg := range os.Args {
		if arg == "managed-fixture" && i+2 < len(os.Args) {
			dir, mode = os.Args[i+1], os.Args[i+2]
		}
	}
	if dir == "" {
		return
	}
	if mode == "startup-failure" {
		_, _ = os.Stderr.WriteString("owned fixture identity unavailable")
	}
	bytes, err := os.ReadFile(filepath.Join(dir, "proof.json"))
	if err != nil {
		os.Exit(2)
	}
	var original nativeinput.Proof
	if json.Unmarshal(bytes, &original) != nil {
		os.Exit(2)
	}
	for {
		var size [4]byte
		if _, err := io.ReadFull(os.Stdin, size[:]); err != nil {
			os.Exit(0)
		}
		requestBytes := make([]byte, binary.BigEndian.Uint32(size[:]))
		if _, err := io.ReadFull(os.Stdin, requestBytes); err != nil {
			os.Exit(2)
		}
		var r nativeRequest
		if json.Unmarshal(requestBytes, &r) != nil {
			os.Exit(2)
		}
		p := original
		if r.Op == "canonical" && r.Height == 0 {
			p.Ref = p.Identity.Anchor
			p.Receipt = nil
		}
		var value any = nativeHead{Height: p.Ref.Height, Hash: p.Ref.Hash}
		if r.Op == "identity" {
			value = nativeIdentity{ChainID: "5042002", Genesis: p.Identity.Genesis, Revision: arcRevision}
			if mode == "startup-failure" {
				value = nativeIdentity{ChainID: "wrong-network", Genesis: p.Identity.Genesis, Revision: arcRevision}
			}
		}
		payload, _ := json.Marshal(value)
		proof, _ := json.Marshal(p)
		switch mode {
		case "wrong-proof":
			p.Head.Instance[0]++
			proof, _ = json.Marshal(p)
		case "oversize-proof":
			proof = make([]byte, nativeinput.MaxMeta+1)
		}
		h := make([]byte, 21)
		copy(h, "AXM1")
		binary.BigEndian.PutUint64(h[4:12], r.ID)
		h[12] = operationCode(r.Op)
		binary.BigEndian.PutUint32(h[13:17], uint32(len(proof)))
		binary.BigEndian.PutUint32(h[17:21], uint32(len(payload)))
		if mode == "legacy" {
			copy(h, "AXS1")
			binary.BigEndian.PutUint32(h[13:17], uint32(len(payload)))
			h = h[:17]
			proof = nil
		}
		_, _ = os.Stdout.Write(h)
		if mode == "truncated-proof" {
			_, _ = os.Stdout.Write(proof[:2])
			os.Exit(0)
		}
		_, _ = os.Stdout.Write(proof)
		_, _ = os.Stdout.Write(payload)
	}
}
