package postgres

import (
	"bytes"
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	checkpoint "github.com/0xmhha/indexer-go/pkg/execution/checkpoint"
	"github.com/0xmhha/indexer-go/x/stablenet"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
	"math/big"
	"os"
	"testing"
)

func bootstrapEmpty(t *testing.T, partial bool) (format.Manifest, []byte) {
	t.Helper()
	h := &types.Header{Number: big.NewInt(0), Root: types.EmptyRootHash, Difficulty: big.NewInt(1)}
	header, err := rlp.EncodeToBytes(h)
	require.NoError(t, err)
	m := format.Manifest{Version: 1, Mode: "genesis", Coverage: "full", Chain: execwire.Chain{ID: "1", Genesis: execwire.Hash(h.Hash())}, Ref: execwire.Ref{Generation: 1, Hash: execwire.Hash(h.Hash())}, Root: execwire.Hash(h.Root), Header: header}
	if partial {
		m.Mode = "checkpoint"
		m.Coverage = "partial"
	}
	var b bytes.Buffer
	require.NoError(t, format.Write(&b, format.Record{Kind: "manifest", Manifest: &m}))
	require.NoError(t, format.Write(&b, format.Record{Kind: "end"}))
	return m, b.Bytes()
}
func TestBootstrapExecutionAtomicCoverage(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "full", true: "partial"}[partial], func(t *testing.T) {
			s := newTestStore(t)
			m, payload := bootstrapEmpty(t, partial)
			fresh := 0
			out, err := s.BootstrapExecution(t.Context(), bytes.NewReader(payload), m, evm.New("evm"), func(ctx context.Context, ref execwire.Ref) error { fresh++; require.Equal(t, m.Ref, ref); return nil })
			require.NoError(t, err)
			require.NotZero(t, out.Digest)
			require.Equal(t, 1, fresh)
			c, err := s.ExecutionStatus(t.Context())
			require.NoError(t, err)
			require.True(t, c.BaselineReady)
			require.Equal(t, m.Ref, c.Visible)
			require.EqualValues(t, 4, s.pool.Config().MaxConns)
			a, err := s.ExecutionAccountAt(t.Context(), execwire.Address{9}, 0)
			if partial {
				require.ErrorIs(t, err, port.ErrExecutionCoverage)
			} else {
				require.NoError(t, err)
				require.False(t, a.Exists)
			}
			_, err = s.ExecutionStorageAt(t.Context(), execwire.Address{9}, execwire.Word{1}, 0)
			if partial {
				require.ErrorIs(t, err, port.ErrExecutionCoverage)
			} else {
				require.NoError(t, err)
			}
			_, err = s.BootstrapExecution(t.Context(), bytes.NewReader(payload), m, evm.New("evm"), func(context.Context, execwire.Ref) error { return nil })
			require.ErrorIs(t, err, port.ErrExecutionConflict)
		})
	}
}
func TestBootstrapExecutionFailurePreservesEmptySchema(t *testing.T) {
	for _, kind := range []string{"fresh", "truncation", "identity", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			s := newTestStore(t)
			m, payload := bootstrapEmpty(t, false)
			sentinel := errors.New("canonical failure")
			ctx := t.Context()
			fresh := func(context.Context, execwire.Ref) error { return nil }
			switch kind {
			case "fresh":
				fresh = func(context.Context, execwire.Ref) error { return sentinel }
			case "truncation":
				payload = payload[:len(payload)-4]
			case "identity":
				m.Ref.Hash[0]++
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			_, err := s.BootstrapExecution(ctx, bytes.NewReader(payload), m, evm.New("evm"), fresh)
			require.Error(t, err)
			if kind == "fresh" {
				require.ErrorIs(t, err, sentinel)
			}
			_, err = s.ExecutionStatus(t.Context())
			require.ErrorIs(t, err, port.ErrExecutionUninitialized)
			var count int
			require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM blocks").Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestBootstrapExecutionOfficialCheckpointStateAndRewind(t *testing.T) {
	s := newTestStore(t)
	payload, err := os.ReadFile("../../../testdata/execution/checkpoints/wbft-checkpoint.ndjson")
	require.NoError(t, err)
	first, err := format.NewDecoder(bytes.NewReader(payload)).Next()
	require.NoError(t, err)
	m := *first.Manifest
	_, err = s.BootstrapExecution(t.Context(), bytes.NewReader(payload), m, stablenet.New(), func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	report, err := checkpoint.Verify(t.Context(), bytes.NewReader(payload), m, stablenet.New(), func(v format.Record) error {
		switch v.Kind {
		case "account":
			a, e := s.ExecutionAccountAt(t.Context(), v.Address, m.Ref.Height)
			require.NoError(t, e)
			require.Equal(t, *v.Account, a)
			code, e := s.ExecutionCodeAt(t.Context(), v.Address, m.Ref.Height)
			require.NoError(t, e)
			require.Equal(t, v.Code, bytesOrNil(code))
		case "storage":
			value, e := s.ExecutionStorageAt(t.Context(), v.Address, v.Slot, m.Ref.Height)
			require.NoError(t, e)
			require.Equal(t, v.Value, value)
		}
		return nil
	})
	require.NoError(t, err)
	require.Positive(t, report.Accounts)
	require.Positive(t, report.Slots)
	info, err := s.ExecutionBootstrapStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "full", info.Coverage)
	require.Equal(t, report.Digest[:], info.Digest)
	// An initially zero, unlisted slot in an existing nonempty storage trie must
	// accept a verified zero Before, and return to zero after a fenced rewind.
	var addr execwire.Address
	var a execwire.Account
	d := format.NewDecoder(bytes.NewReader(payload))
	for {
		v, e := d.Next()
		if e != nil {
			break
		}
		if v.Kind == "account" && v.Account.Exists && v.Account.StorageRoot != execwire.Hash(types.EmptyRootHash) {
			addr = v.Address
			a = *v.Account
			break
		}
	}
	require.True(t, a.Exists)
	slot := execwire.Word{0: 0xfe, 31: 0x55}
	zero, err := s.ExecutionStorageAt(t.Context(), addr, slot, m.Ref.Height)
	require.NoError(t, err)
	require.Zero(t, zero)
	cfg := port.ExecutionConfig{Chain: m.Chain, Anchor: m.Ref, AnchorRoot: m.Root, PartitionBlocks: 1_000_000}
	after := a
	after.StorageRoot = execwire.Hash{9}
	r := accountResult(cfg, addr, a, after, after)
	r.Ref.Height = m.Ref.Height + 1
	r.Steps[0].Storage = []execwire.StorageChange{{Address: addr, Slot: slot, After: execwire.Word{31: 7}}}
	writeRewindStateResult(t, s, r, true)
	_, err = s.BeginExecutionRewind(t.Context(), m.Ref, m.Root)
	require.NoError(t, err)
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	v, err := s.VisibleExecutionStorage(t.Context(), addr, slot)
	require.NoError(t, err)
	require.Zero(t, v.Value)
}
func bytesOrNil(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return b
}
func TestBootstrapExecutionRefusesUnrelatedExistingRows(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Put(t.Context(), []byte("existing"), []byte("preserve")))
	m, payload := bootstrapEmpty(t, false)
	_, err := s.BootstrapExecution(t.Context(), bytes.NewReader(payload), m, evm.New("evm"), func(context.Context, execwire.Ref) error { return nil })
	require.ErrorIs(t, err, port.ErrExecutionConflict)
	got, err := s.Get(t.Context(), []byte("existing"))
	require.NoError(t, err)
	require.Equal(t, "preserve", string(got))
}

func TestBootstrapExistingE1BaselineStaysPartial(t *testing.T) {
	s, _, _, _ := accountFixture(t)
	info, err := s.ExecutionBootstrapStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "partial", info.Coverage)
	require.Equal(t, "prepared", info.Mode)
	require.Nil(t, info.Digest)
	_, err = s.ExecutionAccountAt(t.Context(), execwire.Address{9}, 0)
	require.ErrorIs(t, err, port.ErrExecutionCoverage)
}

func TestBootstrapExecutionOfficialPartialProofCoverage(t *testing.T) {
	s := newTestStore(t)
	payload, err := os.ReadFile("../../../testdata/execution/checkpoints/wbft-partial.ndjson")
	require.NoError(t, err)
	first, err := format.NewDecoder(bytes.NewReader(payload)).Next()
	require.NoError(t, err)
	m := *first.Manifest
	_, err = s.BootstrapExecution(t.Context(), bytes.NewReader(payload), m, stablenet.New(), func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	_, err = checkpoint.Verify(t.Context(), bytes.NewReader(payload), m, stablenet.New(), func(v format.Record) error {
		switch v.Kind {
		case "account":
			a, e := s.ExecutionAccountAt(t.Context(), v.Address, m.Ref.Height)
			require.NoError(t, e)
			require.Equal(t, *v.Account, a)
		case "storage":
			value, e := s.ExecutionStorageAt(t.Context(), v.Address, v.Slot, m.Ref.Height)
			require.NoError(t, e)
			require.Equal(t, v.Value, value)
		}
		return nil
	})
	require.NoError(t, err)
	a, err := s.ExecutionAccountAt(t.Context(), execwire.Address{19: 0xfe}, m.Ref.Height)
	require.NoError(t, err)
	require.False(t, a.Exists)
	_, err = s.ExecutionAccountAt(t.Context(), execwire.Address{19: 0xfd}, m.Ref.Height)
	require.ErrorIs(t, err, port.ErrExecutionCoverage)
}

func TestBootstrapMigrationPreservesReleasedE1Baseline(t *testing.T) {
	s, cfg, addr, a := accountFixture(t)
	// Reconstruct the released v14 control shape, retaining its baseline rows
	// and digest, then apply only the new migration as an upgrade.
	_, err := s.pool.Exec(t.Context(), `ALTER TABLE exec_control DROP COLUMN coverage,DROP COLUMN start_mode,DROP COLUMN bootstrap_digest; DELETE FROM schema_migrations WHERE version=15`)
	require.NoError(t, err)
	require.NoError(t, s.Migrate(t.Context()))
	info, err := s.ExecutionBootstrapStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "partial", info.Coverage)
	require.Equal(t, "prepared", info.Mode)
	require.Nil(t, info.Digest)
	got, err := s.ExecutionAccountAt(t.Context(), addr, cfg.Anchor.Height)
	require.NoError(t, err)
	require.Equal(t, a, got)
	_, err = s.ExecutionAccountAt(t.Context(), execwire.Address{9}, cfg.Anchor.Height)
	require.ErrorIs(t, err, port.ErrExecutionCoverage)
}
