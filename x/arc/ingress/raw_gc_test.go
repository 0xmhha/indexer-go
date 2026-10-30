package arcingress

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"github.com/stretchr/testify/require"
)

type rawGCMockKey struct{}
type rawGCMock struct {
	*originStatus
	ticket        *ingress.RawDeletionTicket
	before, after error
	check         func()
}

func (s *rawGCMock) ExecutionRawDeletion(context.Context, execwire.Hash) (ingress.RawDeletionTicket, error) {
	if s.ticket == nil {
		return ingress.RawDeletionTicket{}, port.ErrNotFound
	}
	return *s.ticket, nil
}
func (s *rawGCMock) CheckExecutionRawDeletion(ctx context.Context, p retention.Permit, e ingress.RawDeletion, r window.DeletionRecord) error {
	active, _ := ctx.Value(rawGCMockKey{}).(*bool)
	if active == nil || !*active {
		return port.ErrClosed
	}
	if s.ticket == nil || s.ticket.Completed || s.ticket.Permit != p || r != s.ticket.Evidence.Record {
		return port.ErrExecutionConflict
	}
	_, a, err := e.Bytes(p)
	if err != nil {
		return err
	}
	_, b, _ := s.ticket.Evidence.Bytes(p)
	if a != b {
		return port.ErrExecutionConflict
	}
	return nil
}
func (s *rawGCMock) WithExecutionRawDeletion(ctx context.Context, p retention.Permit, e ingress.RawDeletion, apply func(context.Context) error) error {
	if err := e.Validate(p); err != nil {
		return err
	}
	if s.ticket == nil {
		s.ticket = &ingress.RawDeletionTicket{Permit: p, Evidence: e}
	}
	if s.before != nil {
		return s.before
	}
	if s.check != nil {
		s.check()
	}
	active := true
	defer func() { active = false }()
	if err := apply(context.WithValue(ctx, rawGCMockKey{}, &active)); err != nil {
		return err
	}
	if s.after != nil {
		return s.after
	}
	s.ticket.Completed = true
	return nil
}

// Sealed native control is a metadata fixture, not 130 executed Arc blocks.
// The source uses real spool append/exact-ACK durability; SQL admission has
// separate real PostgreSQL tests. N6 will qualify the combined actual SDK path.
func rawGCFixture(t *testing.T) (RawRetentionOptions, *rawGCMock, []window.Reference) {
	t.Helper()
	p, s, _ := originFixture(t)
	batch, refs, err := p.readBatch(t.Context(), rawFixture(t, p.control.Chain, p.identity.Anchor, 1)[0].Block)
	require.NoError(t, err)
	require.Len(t, batch, 3)
	require.NoError(t, p.raw.Seal(t.Context()))
	parent := nativeRef{Height: 129, Hash: execwire.Hash{129}, Root: p.head.Tip.Root}
	last := rawFixture(t, p.control.Chain, parent, 130)
	_, err = p.raw.AppendBatch(t.Context(), last)
	require.NoError(t, err)
	decoded, err := decodeBatch(p.control.Chain, last)
	require.NoError(t, err)
	head := p.head
	head.RequestID = 130
	head.ReadEpoch = 131
	head.Tip = nativeRef{Height: 130, Hash: execwire.Hash(decoded.block.Hash), Root: execwire.Hash(decoded.block.StateRoot)}
	r := importReceipt{Version: 1, Identity: p.identity, RequestID: 130, ImportEpoch: 1, ReadEpoch: 131, Height: 130, Hash: head.Tip.Hash, Parent: parent.Hash, ParentRoot: parent.Root, Root: head.Tip.Root, ReceiptsRoot: execwire.Hash(decoded.block.ReceiptRoot), InputDigest: execwire.Hash{99}}
	writeNativeArtifact(t, completionPath(p.cfg.Native.DataDir, 1, 130), r)
	writeNativeArtifact(t, filepath.Join(p.cfg.Native.DataDir, headName), head)
	previous, _ := json.Marshal(p.control)
	p.control.Received = head.Tip
	next, _ := json.Marshal(p.control)
	require.NoError(t, p.raw.CompareOwner(t.Context(), previous, next))
	require.NoError(t, p.raw.Close())
	admission := s.owner.Binding.SourcePolicyDigest
	options := spool.Options{Path: p.cfg.Collector.SpoolDir, Chain: p.deps.Control.Chain, Anchor: p.deps.Control.Anchor, Admission: &admission, MaxBytes: 4 << 20, MaxRecords: 256}
	source, err := spool.Open(t.Context(), options)
	require.NoError(t, err)
	defer func() { _ = source.Close() }()
	// Seed only a test checkpoint base; execute one real durable spool ACK.
	require.NoError(t, source.Close())
	base := execwire.Ref{Generation: 7, Height: 129, Hash: parent.Hash}
	state := struct {
		Version   uint16
		Chain     execwire.Chain
		Anchor    execwire.Ref
		Base      *execwire.Ref `json:",omitempty"`
		Committed execwire.Ref
		Digest    execwire.Hash
		History   []spool.Entry
		Admission *execwire.Hash `json:",omitempty"`
	}{Version: 2, Chain: options.Chain, Anchor: options.Anchor, Base: &base, Committed: base, Admission: options.Admission}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	sealed, err := json.Marshal(struct {
		Data   json.RawMessage
		Digest execwire.Hash
	}{data, execwire.Hash(sha256.Sum256(data))})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(options.Path, ".exec-spool-state"), sealed, 0600))
	source, err = spool.Open(t.Context(), options)
	require.NoError(t, err)
	result := execwire.Result{Ref: execwire.Ref{Generation: 7, Height: 130, Hash: head.Tip.Hash}, Parent: base.Hash, Time: 1, StateRoot: head.Tip.Root, ReceiptsRoot: r.ReceiptsRoot, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}, Steps: []execwire.Step{{Ordinal: 0, Cause: execwire.Finalize, TxIndex: -1}}}
	entry, err := source.Append(t.Context(), result)
	require.NoError(t, err)
	require.NoError(t, source.Acknowledge(t.Context(), execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: options.Chain, Ref: entry.Ref, Digest: entry.Digest}}))

	request, err := source.RetentionRequest(t.Context(), execwire.Ref{Height: 130, Hash: head.Tip.Hash}, head.Tip.Root, 129)
	require.NoError(t, err)
	permit := retention.Permit{Version: 1, Coverage: "full", BaselineDigest: execwire.Hash{11}, Token: execwire.Hash{12}, Request: request, Visible: request.ACK, Root: head.Tip.Root}
	require.NoError(t, permit.Validate())
	o := RawRetentionOptions{NativeDataDir: p.cfg.Native.DataDir, Window: p.opts, Source: options, Permit: permit}
	future := time.Now().Add(25 * time.Hour)
	o.Window.Now = func() time.Time { return future }
	return o, &rawGCMock{originStatus: s}, refs
}

func TestArcRawCollectionUsesStoppedOwnersExactSourceAndFullRecord(t *testing.T) {
	o, s, refs := rawGCFixture(t)
	s.check = func() {
		child, err := nativestore.AcquireRun(o.NativeDataDir)
		if child != nil {
			_ = child.Close()
		}
		require.ErrorIs(t, err, nativestore.ErrPending)
		writer, err := spool.Open(t.Context(), o.Source)
		if writer != nil {
			_ = writer.Close()
		}
		require.ErrorIs(t, err, spool.ErrBusy)
		_, err = os.Stat(filepath.Join(o.Window.Path, "00000000000000000001.raw"))
		require.NoError(t, err)
	}
	n, err := CollectRaw(t.Context(), s, o)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.NotNil(t, s.ticket)
	require.True(t, s.ticket.Completed)
	require.NoError(t, s.ticket.Evidence.Record.Match(refs))
	require.EqualValues(t, 17, s.ticket.Evidence.RawEpoch)
	require.EqualValues(t, 1, s.ticket.Evidence.NativeEpoch)
	require.EqualValues(t, 7, s.ticket.Permit.Visible.Generation)
	_, err = os.Stat(filepath.Join(o.Window.Path, "00000000000000000002.raw"))
	require.NoError(t, err, "protected native tip remains")
}

func TestArcRawCollectionRefusesLiveChildAndNativePendingWithoutDeleting(t *testing.T) {
	o, s, _ := rawGCFixture(t)
	child, err := nativestore.AcquireRun(o.NativeDataDir)
	require.NoError(t, err)
	_, err = CollectRaw(t.Context(), s, o)
	require.ErrorIs(t, err, nativestore.ErrPending)
	require.Nil(t, s.ticket)
	require.NoError(t, child.Close())
	pending := filepath.Join(o.NativeDataDir, ".arc-native-import.pending")
	require.NoError(t, os.WriteFile(pending, []byte("uncertain native import"), 0600))
	_, err = CollectRaw(t.Context(), s, o)
	require.Error(t, err)
	require.Nil(t, s.ticket)
	_, err = os.Stat(filepath.Join(o.Window.Path, "00000000000000000001.raw"))
	require.NoError(t, err)
	_, err = os.Stat(pending)
	require.NoError(t, err, "ambiguous pending evidence is never cleared")
}

func TestArcRawRecoveryRequiresDurableTicketAndActualCompletion(t *testing.T) {
	for _, mode := range []string{"before_unlink", "after_unlink", "partial_unlink", "missing_completion", "changed_owner"} {
		t.Run(mode, func(t *testing.T) {
			o, s, _ := rawGCFixture(t)
			failure := errors.New("owned crash boundary")
			if mode == "after_unlink" || mode == "missing_completion" {
				s.after = failure
			} else {
				s.before = failure
			}
			_, err := CollectRaw(t.Context(), s, o)
			require.ErrorIs(t, err, failure)
			require.NotNil(t, s.ticket)
			require.False(t, s.ticket.Completed)
			s.before, s.after = nil, nil
			segment := filepath.Join(o.Window.Path, "00000000000000000001")
			if mode == "changed_owner" {
				raw, err := window.OpenOwnedChecked(o.Window, ownerDomain, func(window.DeletionRecord) error { return window.ErrUnsafeGC })
				require.NoError(t, err)
				owner, err := raw.ReadOwner(t.Context())
				require.NoError(t, err)
				var control rawControl
				require.NoError(t, json.Unmarshal(owner.State, &control))
				control.RawEpoch++
				next, _ := json.Marshal(control)
				require.NoError(t, raw.CompareOwner(t.Context(), owner.State, next))
				require.NoError(t, raw.Close())
			}
			if mode == "partial_unlink" || mode == "changed_owner" {
				record, _ := json.Marshal(s.ticket.Evidence.Record)
				require.NoError(t, os.WriteFile(segment+".gc", record, 0600))
			}
			if mode == "partial_unlink" {
				require.NoError(t, os.Remove(segment+".raw"))
			}
			if mode == "missing_completion" {
				require.NoError(t, os.Remove(filepath.Join(o.Window.Path, "GC.completed.json")))
			}
			err = RecoverRaw(t.Context(), s, o)
			if mode == "missing_completion" || mode == "changed_owner" {
				require.Error(t, err)
				require.False(t, s.ticket.Completed)
				if mode == "changed_owner" {
					_, err = os.Stat(segment + ".raw")
					require.NoError(t, err, "wrong owner refused before recovery unlink")
				}
			} else {
				require.NoError(t, err)
				require.True(t, s.ticket.Completed)
			}
		})
	}
}
