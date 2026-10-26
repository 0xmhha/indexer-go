package execution

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/exporter"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob/proof"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/rpcpool"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

type engineBlobFixture struct {
	policy         BlobSupplyPolicy
	payload        []byte
	result         execwire.Result
	parent, header *types.Header
	entries        []any
	calls          []string
	head           uint64
	after          func()
	fail           error
	wrong          bool
}

func engineBlobMock(t *testing.T) *engineBlobFixture {
	t.Helper()
	chain, payload, response := nativeBlobFixture(t)
	r, e := execwire.UnmarshalResult(payload)
	require.NoError(t, e)
	parts, e := proof.SplitList(r.BlockRLP, 4)
	require.NoError(t, e)
	var header types.Header
	require.NoError(t, rlp.DecodeBytes(parts[0], &header))
	var f struct{ ParentHeader []byte }
	raw, e := os.ReadFile("../../testdata/execution/evm-blobs.json")
	require.NoError(t, e)
	require.NoError(t, json.Unmarshal(raw, &f))
	var parent types.Header
	require.NoError(t, rlp.DecodeBytes(f.ParentHeader, &parent))
	require.Equal(t, chain.Genesis, execwire.Hash(parent.Hash()))
	m := &engineBlobFixture{policy: BlobSupplyPolicy{Contract: DevEngineBlobContract, Chain: chain, Provider: "engine-v1", MaxLagBlocks: 64}, payload: payload, result: r, parent: &parent, header: &header, head: r.Ref.Height}
	for _, entry := range response.Entries {
		m.entries = append(m.entries, map[string]any{"blob": hexutil.Bytes(entry.Blob[:]), "proof": hexutil.Bytes(entry.Proof[:])})
	}
	return m
}
func (m *engineBlobFixture) CallContext(ctx context.Context, out interface{}, method string, args ...interface{}) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	m.calls = append(m.calls, method)
	var value any
	switch method {
	case "eth_chainId":
		value = "0x539"
	case "eth_getBlockByNumber":
		switch args[0] {
		case "0x0":
			value = m.parent
		case "latest":
			h := *m.header
			h.Number = new(big.Int).SetUint64(m.head)
			value = &h
		default:
			h := *m.header
			if m.wrong {
				h.Extra = []byte("reorg")
			}
			value = &h
		}
	case "engine_getBlobsV1":
		if m.fail != nil {
			return m.fail
		}
		if len(args) != 1 {
			return errors.New("incorrect Engine parameters")
		}
		hashes, ok := args[0].([]common.Hash)
		if !ok || len(hashes) != 3 {
			return errors.New("incorrect reference count")
		}
		value = m.entries
		if m.after != nil {
			m.after()
		}
	default:
		return errors.New("unexpected RPC method")
	}
	raw, e := json.Marshal(value)
	if e != nil {
		return e
	}
	return json.Unmarshal(raw, out)
}
func (m *engineBlobFixture) Head(context.Context) (execwire.Ref, error) { return m.result.Ref, nil }
func (m *engineBlobFixture) Canonical(_ context.Context, n uint64) (execwire.Hash, error) {
	if n == 0 {
		return m.policy.Chain.Genesis, nil
	}
	if m.wrong {
		return execwire.Hash{99}, nil
	}
	return m.result.Ref.Hash, nil
}
func (m *engineBlobFixture) Replay(_ context.Context, n, g uint64) (execwire.Result, error) {
	r := m.result
	r.Ref.Generation = g
	return r, nil
}

func TestSeedPriority1BlobSupplyVerification(t *testing.T) {
	t.Run("native_replay_to_real_HTTP_transport", func(t *testing.T) {
		m := engineBlobMock(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request struct {
				ID     json.RawMessage
				Method string
				Params []json.RawMessage
			}
			if json.NewDecoder(r.Body).Decode(&request) != nil {
				http.Error(w, "invalid", 400)
				return
			}
			var args []interface{}
			for _, raw := range request.Params {
				if request.Method == "engine_getBlobsV1" {
					var hashes []common.Hash
					_ = json.Unmarshal(raw, &hashes)
					args = append(args, hashes)
				} else {
					var value interface{}
					_ = json.Unmarshal(raw, &value)
					args = append(args, value)
				}
			}
			var result json.RawMessage
			e := m.CallContext(r.Context(), &result, request.Method, args...)
			if e != nil {
				http.Error(w, "invalid", 400)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		}))
		defer srv.Close()
		pool, e := rpcpool.New(rpcpool.Config{Endpoints: []string{srv.URL}, RateLimit: 5, MaxResponseBytes: 10 << 20})
		require.NoError(t, e)
		defer pool.Close()
		client, e := rpc.DialOptions(t.Context(), pool.Primary(), rpc.WithHTTPClient(&http.Client{Transport: pool}))
		require.NoError(t, e)
		defer client.Close()
		enriched, e := AcquireEngineBlobInput(t.Context(), evm.New("evm"), m.policy, m.payload, client)
		require.NoError(t, e)
		p, e := PrepareContext(t.Context(), evm.New("evm"), m.policy.Chain, enriched, true)
		require.NoError(t, e)
		require.Len(t, p.blobs.Entries, 3)
		require.Equal(t, m.result.Ref, p.Ref())
		require.NotEqual(t, m.payload, enriched)
		require.Equal(t, []string{"eth_chainId", "eth_getBlockByNumber", "eth_getBlockByNumber", "engine_getBlobsV1", "eth_getBlockByNumber"}, m.calls)
	})
	for _, kind := range []string{"missing", "pruned", "wrong_proof", "wrong_reference", "wrong_fork", "reorg", "head_behind", "outside_window", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			m := engineBlobMock(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch kind {
			case "missing":
				m.entries[1] = nil
			case "pruned":
				m.fail = errors.New("pruned blob pool")
			case "wrong_proof":
				m.entries[0].(map[string]any)["proof"] = hexutil.Bytes(make([]byte, 48))
			case "wrong_reference":
				m.entries[0].(map[string]any)["blob"] = hexutil.Bytes(append(make([]byte, 131071), byte(1)))
			case "wrong_fork":
				m.policy.Contract = "peerdas-cells"
			case "reorg":
				m.after = func() { m.wrong = true }
			case "head_behind":
				m.head = 0
			case "outside_window":
				m.head = m.result.Ref.Height + 65
			case "cancel":
				m.after = cancel
			}
			original := append([]byte{}, m.payload...)
			out, e := AcquireEngineBlobInput(ctx, evm.New("evm"), m.policy, m.payload, m)
			require.Error(t, e)
			require.Nil(t, out)
			require.Equal(t, original, m.payload)
			if kind == "reorg" {
				require.ErrorIs(t, e, exporter.ErrCanonicalChanged)
			}
			if kind == "cancel" {
				require.ErrorIs(t, e, context.Canceled)
			}
		})
	}
	t.Run("pinned_spool_restart_capacity_reorg_and_exact_ACK", func(t *testing.T) {
		m := engineBlobMock(t)
		backend, e := NewEngineBlobBackend(evm.New("evm"), m.policy, m, m)
		require.NoError(t, e)
		opts := spool.Options{Path: filepath.Join(t.TempDir(), "spool"), Chain: m.policy.Chain, Anchor: execwire.Ref{Generation: 1, Hash: m.result.Parent}, MaxBytes: 16 << 20, MaxRecords: 2}
		s, e := backend.OpenSpool(t.Context(), opts)
		require.NoError(t, e)
		c := exporter.Collector{Backend: backend, Spool: s, Limit: 1}
		count, e := c.CatchUp(t.Context())
		require.NoError(t, e)
		require.Equal(t, 1, count)
		entry, ok, e := s.Next(t.Context())
		require.NoError(t, e)
		require.True(t, ok)
		require.NoError(t, s.Close())
		// Restart reuses sealed bytes even after remote pruning.
		m.fail = errors.New("pruned")
		s, e = backend.OpenSpool(t.Context(), opts)
		require.NoError(t, e)
		defer func() { require.NoError(t, s.Close()) }()
		retained, e := s.Read(t.Context(), entry.Ref)
		require.NoError(t, e)
		require.NotNil(t, retained.BlobBundle)
		changed := m.policy
		changed.MaxLagBlocks++
		other, e := NewEngineBlobBackend(evm.New("evm"), changed, m, m)
		require.NoError(t, e)
		require.NoError(t, s.Close())
		rejected, e := other.OpenSpool(t.Context(), opts)
		require.ErrorIs(t, e, spool.ErrConflict)
		require.Nil(t, rejected)
		s, e = backend.OpenSpool(t.Context(), opts)
		require.NoError(t, e)
		ack := execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: opts.Chain, Ref: entry.Ref, Digest: entry.Digest}}
		ack.Header.Digest[0] ^= 1
		require.ErrorIs(t, s.Acknowledge(t.Context(), ack), spool.ErrConflict)
		_, ok, e = s.Next(t.Context())
		require.NoError(t, e)
		require.True(t, ok)
		ack.Header.Digest = entry.Digest
		require.NoError(t, s.Acknowledge(t.Context(), ack))
		require.NoError(t, s.Acknowledge(t.Context(), ack), "lost ACK retry is exact and idempotent")
		_, ok, e = s.Next(t.Context())
		require.NoError(t, e)
		require.False(t, ok)
		// A tiny owned spool cannot seal the enriched payload and retains no partial.
		tiny := opts
		tiny.Path = filepath.Join(t.TempDir(), "tiny")
		tiny.MaxBytes = 128 << 10
		small, e := backend.OpenSpool(t.Context(), tiny)
		require.NoError(t, e)
		defer func() { require.NoError(t, small.Close()) }()
		_, e = small.Append(t.Context(), retained)
		require.ErrorIs(t, e, spool.ErrFull)
		_, ok, e = small.Next(t.Context())
		require.NoError(t, e)
		require.False(t, ok)
		// Canonicality changing during acquisition cannot enter a spool.
		fresh := opts
		fresh.Path = filepath.Join(t.TempDir(), "fork")
		fork, e := backend.OpenSpool(t.Context(), fresh)
		require.NoError(t, e)
		defer func() { require.NoError(t, fork.Close()) }()
		m.fail = nil
		m.after = func() { m.wrong = true }
		c.Spool = fork
		_, e = c.CatchUp(t.Context())
		require.ErrorIs(t, e, exporter.ErrCanonicalChanged)
		_, ok, e = fork.Next(t.Context())
		require.NoError(t, e)
		require.False(t, ok)
	})
	t.Run("unsupported_pinned_Arc_policy", func(t *testing.T) {
		m := engineBlobMock(t)
		profile := evm.New("evm", evm.WithBlobCapability(modelUnsupportedBlobPolicy()))
		_, e := AcquireEngineBlobInput(t.Context(), profile, m.policy, m.payload, m)
		require.ErrorIs(t, e, evmblob.ErrUnsupported)
		require.Empty(t, m.calls)
	})
	t.Run("already_admitted_input_is_immutable", func(t *testing.T) {
		m := engineBlobMock(t)
		out, e := AcquireEngineBlobInput(t.Context(), evm.New("evm"), m.policy, m.payload, m)
		require.NoError(t, e)
		m.calls = nil
		_, e = AcquireEngineBlobInput(t.Context(), evm.New("evm"), m.policy, out, m)
		require.ErrorIs(t, e, port.ErrExecutionConflict)
		require.Empty(t, m.calls)
	})
}

func modelUnsupportedBlobPolicy() model.BlobCapability {
	return model.BlobCapability{Version: 1, Profile: "evm", Status: model.BlobUnsupported, Reason: "eip4844_disabled_by_node_policy", Basis: "node_transaction_admission", Revision: "6e764023ee6515fe70573e123ed2db912a7207b4", SourceURL: "https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/crates/execution-txpool/src/pool.rs"}
}
