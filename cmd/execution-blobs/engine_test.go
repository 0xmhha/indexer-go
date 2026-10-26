package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob/proof"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEngineBlobOptInIsDiscoverable(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, run(t.Context(), []string{"--help"}, &out, nil))
	require.Contains(t, out.String(), "engine-rpc", "usable opt-in standard Engine V1 transport must be exposed")
	require.Contains(t, out.String(), "max-lag-blocks", "remote availability must have an explicit bounded head window")
}

func TestEngineBlobOptInFilesAndJWT(t *testing.T) {
	var fixture struct {
		Chain                 execwire.Chain
		ParentHeader, Payload []byte
	}
	raw, e := os.ReadFile("../../testdata/execution/evm-blobs.json")
	require.NoError(t, e)
	require.NoError(t, json.Unmarshal(raw, &fixture))
	result, e := execwire.UnmarshalResult(fixture.Payload)
	require.NoError(t, e)
	parts, e := proof.SplitList(result.BlockRLP, 4)
	require.NoError(t, e)
	var parent, header types.Header
	require.NoError(t, rlp.DecodeBytes(fixture.ParentHeader, &parent))
	require.NoError(t, rlp.DecodeBytes(parts[0], &header))
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	input, output, secretPath := filepath.Join(dir, "input.exr"), filepath.Join(dir, "complete.exr"), filepath.Join(dir, "jwt.hex")
	require.NoError(t, os.WriteFile(input, fixture.Payload, 0600))
	secret := bytes.Repeat([]byte{0x19}, 32)
	require.NoError(t, os.WriteFile(secretPath, []byte(hex.EncodeToString(secret)), 0600))
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
		if len(token) != 3 {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write([]byte(token[0] + "." + token[1]))
		signature, err := base64.RawURLEncoding.DecodeString(token[2])
		if err != nil || !hmac.Equal(mac.Sum(nil), signature) {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		var request struct {
			ID     json.RawMessage
			Method string
			Params []json.RawMessage
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		requests.Add(1)
		var value any
		switch request.Method {
		case "eth_chainId":
			value = "0x539"
		case "eth_getBlockByNumber":
			var selector string
			_ = json.Unmarshal(request.Params[0], &selector)
			if selector == "0x0" {
				value = &parent
			} else {
				value = &header
			}
		case "engine_getBlobsV1":
			var entries []any
			for i := 0; i < 3; i++ {
				p := make([]byte, 48)
				p[0] = 0xc0
				entries = append(entries, map[string]any{"blob": hexutil.Bytes(make([]byte, 131072)), "proof": hexutil.Bytes(p)})
			}
			value = entries
		default:
			http.Error(w, "method", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": value})
	}))
	defer srv.Close()
	genesis, _ := fixture.Chain.Genesis.MarshalText()
	args := []string{"--chain-id", fixture.Chain.ID, "--genesis", string(genesis), "--result", input, "--output", output, "--provider", "engine-v1", "--engine-rpc", srv.URL, "--engine-jwt-file", secretPath, "--blob-contract", execution.DevEngineBlobContract, "--max-lag-blocks", "64"}
	var out bytes.Buffer
	require.NoError(t, run(t.Context(), args, &out, nil))
	require.EqualValues(t, 5, requests.Load())
	enriched, e := os.ReadFile(output)
	require.NoError(t, e)
	prepared, e := execution.PrepareContext(t.Context(), evm.New("evm"), fixture.Chain, enriched, true)
	require.NoError(t, e)
	require.Equal(t, result.Ref, prepared.Ref())
	unchanged, e := os.ReadFile(input)
	require.NoError(t, e)
	require.Equal(t, fixture.Payload, unchanged)
	require.Error(t, run(t.Context(), args, &out, nil), "no overwrite and no new RPC on retry")
	require.EqualValues(t, 5, requests.Load())
	require.Error(t, run(t.Context(), append(args, "--sidecars", input), &out, nil))
	require.EqualValues(t, 5, requests.Load())
	require.NoError(t, os.Chmod(secretPath, 0644))
	args[7] = filepath.Join(dir, "other.exr")
	require.Error(t, run(t.Context(), args, &out, nil), "public JWT secret must be rejected")
	require.EqualValues(t, 5, requests.Load())
}
