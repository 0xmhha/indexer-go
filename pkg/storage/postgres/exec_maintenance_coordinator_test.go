package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc/collector"
	arcingress "github.com/0xmhha/indexer-go/x/arc/ingress"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/0xmhha/indexer-go/x/arc/nativeops"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type coordinatorCanonical struct {
	hash  common.Hash
	calls atomic.Int32
}

func (c *coordinatorCanonical) HashAt(ctx context.Context, _ uint64) (common.Hash, error) {
	c.calls.Add(1)
	return c.hash, ctx.Err()
}

// Actual fsync-on PG and real files, with a metadata/protocol native helper.
// The empty authenticated baseline and synthetic ACK130 are not Arc consensus,
// actual SDK execution or throughput evidence. That combination belongs to N6.
func TestExecutionMaintenanceCoordinatorUsesExistingPoolAndExactFresh(t *testing.T) {
	s := newTestStore(t)
	m, _ := bootstrapEmpty(t, false)
	m.Chain.ID = "5042002"
	var input bytes.Buffer
	require.NoError(t, format.Write(&input, format.Record{Kind: "manifest", Manifest: &m}))
	require.NoError(t, format.Write(&input, format.Record{Kind: "end"}))
	_, err := s.BootstrapExecution(t.Context(), &input, m, evm.New("evm"), func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0700))
	root, err = filepath.EvalSymlinks(root)
	require.NoError(t, err)
	native, sourcePath := filepath.Join(root, "native"), filepath.Join(root, "source")
	require.NoError(t, os.Mkdir(native, 0700))
	for _, name := range []string{"db", "rocksdb", "static_files", ".arc-native-import-receipts"} {
		require.NoError(t, os.Mkdir(filepath.Join(native, name), 0700))
	}
	boot := []byte("owned metadata fixture; no native SDK execution")
	require.NoError(t, os.WriteFile(filepath.Join(native, nativeinput.BootstrapName), boot, 0600))
	i := nativeinput.Identity{Version: 1, Instance: execwire.Hash{44}, ChainID: 5042002, Genesis: m.Chain.Genesis, ArcRevision: nativeinput.ArcRevision, RethRevision: nativeinput.RethRevision, Layout: "reth-native-v2", BootstrapDigest: execwire.Hash(sha256.Sum256(boot)), Anchor: nativeinput.Ref{Hash: m.Ref.Hash, Root: m.Root}}
	ack := execwire.Ref{Generation: 7, Height: 130, Hash: execwire.Hash{130}}
	head := nativeinput.Head{Version: 1, Instance: i.Instance, ImportEpoch: 1, ReadEpoch: 131, RequestID: 130, Tip: nativeinput.Ref{Height: 130, Hash: ack.Hash, Root: m.Root}}
	receipt := nativeinput.Receipt{Version: 1, Identity: i, RequestID: 130, ImportEpoch: 1, ReadEpoch: 131, Height: 130, Hash: ack.Hash, Parent: execwire.Hash{129}, ParentRoot: m.Root, Root: m.Root, ReceiptsRoot: execwire.Hash{2}, InputDigest: execwire.Hash{3}}
	seal := func(path string, v any) {
		t.Helper()
		data, e := json.Marshal(v)
		require.NoError(t, e)
		encoded, e := json.Marshal(struct {
			Data   json.RawMessage
			Digest execwire.Hash
		}{data, execwire.Hash(sha256.Sum256(data))})
		require.NoError(t, e)
		require.NoError(t, os.WriteFile(path, encoded, 0600))
	}
	seal(filepath.Join(native, nativeinput.IdentityName), i)
	seal(filepath.Join(native, nativeinput.HeadName), head)
	seal(filepath.Join(native, ".arc-native-import-receipts/1-130.json"), receipt)
	volume := window.VolumeSpec{Root: filepath.Join(root, "volume"), ID: "coordinator-db", AllowDirectory: true}
	require.NoError(t, os.Mkdir(volume.Root, 0700))
	require.NoError(t, window.InitializeVolume(volume))
	rawOptions := window.Options{Volume: volume, Path: filepath.Join(volume.Root, "raw"), Chain: model.FeedChain{ID: m.Chain.ID, GenesisHash: common.Hash(m.Chain.Genesis)}, MaxBytes: 4 << 20, MaxPayloadBytes: 4096, SegmentBytes: 64 << 10}
	control := struct {
		Version                 uint8
		Instance                execwire.Hash
		Chain                   model.FeedChain
		Anchor                  nativeinput.Ref
		RawEpoch                uint64
		Received                nativeinput.Ref
		NativeEpoch, Generation uint64
	}{2, i.Instance, rawOptions.Chain, i.Anchor, 19, head.Tip, 1, 7}
	state, _ := json.Marshal(control)
	raw, err := window.CreateOwned(rawOptions, "arc.native/ain1/v1", state)
	require.NoError(t, err)
	rawOwner, err := raw.ReadOwner(t.Context())
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	policy := port.ExecutionIngressPolicy{Version: 1, Module: arcingress.ModuleName, Chain: m.Chain, Anchor: m.Ref, AnchorRoot: m.Root, Requirements: execwire.Hash{9}}
	require.NoError(t, s.ConfigureExecutionIngress(t.Context(), &policy, false))
	owner, err := s.ExecutionIngressOwner(t.Context())
	require.NoError(t, err)
	admission, err := collector.ManagedAdmissionBinding(collector.Options{Chain: m.Chain, Managed: &i})
	require.NoError(t, err)
	identity, _ := json.Marshal(i)
	require.NoError(t, s.BindExecutionIngress(t.Context(), port.ExecutionIngressBinding{Version: 1, PolicyDigest: owner.PolicyDigest, NativeInstance: i.Instance, BootstrapDigest: i.BootstrapDigest, RawStoreID: execwire.Hash(rawOwner.StoreID), SourcePolicyDigest: admission, NativeIdentity: identity}))
	owner, err = s.ExecutionIngressOwner(t.Context())
	require.NoError(t, err)
	part := port.ExecutionPart{Ref: execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}, Parent: m.Ref.Hash, Digest: execwire.Hash{3}}
	archive := port.ExecutionIngressArchive{Version: 1, OwnerDigest: owner.BindingDigest, Ref: part.Ref, Parent: part.Parent, Digest: part.Digest, InputDigest: execwire.Hash{5}, NativeEpoch: 1, NativeReadEpoch: 2, NativeRequestID: 1, ObservedRawEpoch: 19, Provenance: []byte(`{"fixture":"database-coordinator"}`)}
	for _, kind := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		payload := []byte("owned-" + string(kind))
		archive.Datasets = append(archive.Datasets, port.ExecutionIngressDataset{Kind: kind, Codec: "fixture.v1", Reference: []byte(`{"fixture":true}`), Digest: execwire.Hash(sha256.Sum256(payload)), PayloadBytes: uint64(len(payload)), Payload: payload})
	}
	for _, writer := range port.ExecutionWriters() {
		p := part
		p.Writer = writer
		require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error {
			if writer != port.ExecutionBlocks {
				return nil
			}
			if err := s.ApplyExecutionCore(ctx, writer, &model.Block{Number: 1, Hash: common.Hash(part.Ref.Hash), ParentHash: common.Hash(part.Parent), StateRoot: common.Hash(m.Root)}, nil); err != nil {
				return err
			}
			return s.ApplyExecutionIngress(ctx, archive)
		}))
	}
	_, err = s.CommitExecution(t.Context(), port.ExecutionPublication{Ref: part.Ref, Parent: part.Parent, Digest: part.Digest, Root: m.Root}, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	require.NoError(t, err)
	// Seed a bounded synthetic later publication without executing130 blocks.
	parent, digest := execwire.Hash{129}, execwire.Hash{77}
	require.NoError(t, s.SetBlock(t.Context(), &model.Block{Number: 130, Hash: common.Hash(ack.Hash), ParentHash: common.Hash(parent), StateRoot: common.Hash(m.Root)}))
	_, err = s.pool.Exec(t.Context(), "UPDATE exec_control SET generation=7,visible_height=130,visible_hash=$1 WHERE id", ack.Hash[:])
	require.NoError(t, err)
	_, err = s.pool.Exec(t.Context(), "UPDATE exec_cursors SET generation=7,height=130,hash=$1,parent=$2,digest=$3", ack.Hash[:], parent[:], digest[:])
	require.NoError(t, err)
	for _, writer := range port.ExecutionWriters() {
		_, err = s.pool.Exec(t.Context(), "INSERT INTO exec_parts(writer,generation,height,hash,parent,digest) VALUES($1,7,130,$2,$3,$4)", string(writer), ack.Hash[:], parent[:], digest[:])
		require.NoError(t, err)
	}
	_, err = s.pool.Exec(t.Context(), "INSERT INTO exec_publications(generation,height,hash,parent,digest,root) VALUES(7,130,$1,$2,$3,$4)", ack.Hash[:], parent[:], digest[:], m.Root[:])
	require.NoError(t, err)
	sourceOptions := spool.Options{Path: sourcePath, Chain: m.Chain, Anchor: m.Ref, Admission: &admission, MaxBytes: 4 << 20, MaxRecords: 256}
	source, err := spool.Open(t.Context(), sourceOptions)
	require.NoError(t, err)
	require.NoError(t, source.Close())
	base := execwire.Ref{Generation: 7, Height: 129, Hash: parent}
	seal(filepath.Join(sourcePath, ".exec-spool-state"), struct {
		Version   uint16
		Chain     execwire.Chain
		Anchor    execwire.Ref
		Base      *execwire.Ref `json:",omitempty"`
		Committed execwire.Ref
		Digest    execwire.Hash
		History   []spool.Entry
		Admission *execwire.Hash `json:",omitempty"`
	}{Version: 2, Chain: m.Chain, Anchor: m.Ref, Base: &base, Committed: base, Admission: &admission})
	source, err = spool.Open(t.Context(), sourceOptions)
	require.NoError(t, err)
	entry, err := source.Append(t.Context(), execwire.Result{Ref: ack, Parent: parent, StateRoot: m.Root, ReceiptsRoot: receipt.ReceiptsRoot, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}, Steps: []execwire.Step{{Ordinal: 0, Cause: execwire.Finalize, TxIndex: -1}}})
	require.NoError(t, err)
	require.NoError(t, source.Acknowledge(t.Context(), execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: 1, Chain: m.Chain, Ref: ack, Digest: entry.Digest}}))
	require.NoError(t, source.Close())
	binary := filepath.Join(root, "maintenance")
	quoted := "'" + strings.ReplaceAll(os.Args[0], "'", "'\"'\"'") + "'"
	require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\nGORACE=atexit_sleep_ms=0 exec "+quoted+" -test.run=^TestExecutionCoordinatorNativeHelper$ -- --coordinator-native-helper \"$@\"\n"), 0700))
	cfg := ingress.Config{Module: arcingress.ModuleName, Native: ingress.NativeConfig{DataDir: native}, Collector: ingress.CollectorConfig{SpoolDir: sourcePath, MaxBytes: 4 << 20, MaxRecords: 256}}
	dbControl, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	canonical := &coordinatorCanonical{hash: common.Hash(ack.Hash)}
	deps := ingress.Dependencies{Store: s, Control: dbControl, Policy: &policy, Canonical: canonical}
	options := ingress.MaintenanceOptions{Executable: binary, KeepBlocks: 129, ReserveBytes: 1, Timeout: 10 * time.Second}
	result, err := arcingress.Maintain(t.Context(), cfg, deps, rawOptions, options)
	require.NoError(t, err)
	require.Equal(t, "complete", result.Phase)
	require.False(t, result.Resumed)
	require.EqualValues(t, 2, canonical.calls.Load(), "authorization and completion each require a new canonical response")
	require.EqualValues(t, 4, s.pool.Config().MaxConns)
	_, err = s.ExecutionRawDeletion(t.Context(), execwire.Hash(rawOwner.StoreID))
	require.ErrorIs(t, err, port.ErrNotFound, "zero candidates do not manufacture a deletion ticket")
	_, pending, completed, err := s.ExecutionRetentionStatus(t.Context())
	require.NoError(t, err)
	require.False(t, pending)
	require.Equal(t, result.Receipt, *completed)
	_, err = arcingress.RecoverMaintenance(t.Context(), cfg, deps, rawOptions, options)
	require.NoError(t, err)
	require.EqualValues(t, 2, canonical.calls.Load(), "historical completion must not run fresh or deletion again")
}

func TestExecutionCoordinatorNativeHelper(t *testing.T) {
	if !strings.Contains(strings.Join(os.Args, " "), "--coordinator-native-helper") {
		return
	}
	value := func(key string) string {
		for n, a := range os.Args {
			if a == key && n+1 < len(os.Args) {
				return os.Args[n+1]
			}
		}
		return ""
	}
	fail := func(err error) {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	data, err := os.ReadFile(value("--managed-context"))
	fail(err)
	var c nativeops.ManagedContext
	fail(nativeinput.Decode(data, &c))
	fail(nativeinput.VerifyCurrent(value("--datadir"), c.Proof, c.Proof.Identity))
	var input json.RawMessage
	fail(json.NewDecoder(os.Stdin).Decode(&input))
	if value("--operation") == "inspect" || value("--operation") == "hold" {
		h := c.Proof.Head.Tip
		fail(json.NewEncoder(os.Stdout).Encode(nativeops.Observation{Head: execwire.Ref{Height: h.Height, Hash: h.Hash}, Root: h.Root, AnchorRoot: c.Proof.Identity.Anchor.Root}))
		if value("--operation") == "hold" {
			_, err = io.Copy(io.Discard, os.Stdin)
			fail(err)
		}
	} else {
		fail(json.NewEncoder(os.Stdout).Encode(struct{ Status string }{"native_verified"}))
	}
	os.Exit(0)
}
