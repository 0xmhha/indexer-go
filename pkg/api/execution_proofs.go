package api

import (
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"go.uber.org/zap"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func init() {
	RegisterRoute(Route{Method: http.MethodGet, Pattern: "/execution/proofs/{height}/{address}", Handler: executionProofHandler})
}

type executionProofAccountJSON struct {
	Address          execwire.Address         `json:"address"`
	Exists           bool                     `json:"exists"`
	Nonce            hexutil.Uint64           `json:"nonce"`
	ConsensusBalance execwire.Word            `json:"consensusBalance"`
	StorageRoot      execwire.Hash            `json:"storageRoot"`
	CodeHash         execwire.Hash            `json:"codeHash"`
	Code             *hexutil.Bytes           `json:"code,omitempty"`
	Storage          []executionProofSlotJSON `json:"storage"`
}
type executionProofSlotJSON struct {
	Key   execwire.Word `json:"key"`
	Value execwire.Word `json:"value"`
}
type executionProofJSON struct {
	Epoch         uint64                    `json:"epoch"`
	Ref           execwire.Ref              `json:"ref"`
	Root          execwire.Hash             `json:"root"`
	Digest        execwire.Hash             `json:"digest"`
	SelectionHash execwire.Hash             `json:"selectionHash"`
	Profile       string                    `json:"profile"`
	Provider      string                    `json:"provider"`
	Format        string                    `json:"format"`
	Verification  string                    `json:"verification"`
	Account       executionProofAccountJSON `json:"account"`
	Proof         json.RawMessage           `json:"proof"`
}

func proofQuery(r *http.Request) (q port.ExecutionProofQuery, err error) {
	q.Height, err = strconv.ParseUint(URLParam(r, "height"), 10, 63)
	if err != nil {
		return q, port.ErrInvalidData
	}
	text := URLParam(r, "address")
	if len(text) != 42 || !strings.HasPrefix(text, "0x") || !common.IsHexAddress(text) {
		return q, port.ErrInvalidData
	}
	q.Address = execwire.Address(common.HexToAddress(text))
	fields, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return q, port.ErrInvalidData
	}
	for k, v := range fields {
		if len(v) != 1 {
			return q, port.ErrInvalidData
		}
		switch k {
		case "epoch":
			q.Epoch, e = strconv.ParseUint(v[0], 10, 64)
			if e != nil || q.Epoch == 0 {
				return q, port.ErrInvalidData
			}
		case "hash":
			if e = q.Hash.UnmarshalText([]byte(v[0])); e != nil || q.Hash == (execwire.Hash{}) {
				return q, port.ErrInvalidData
			}
		default:
			return q, port.ErrInvalidData
		}
	}
	if (q.Epoch == 0) != (q.Hash == (execwire.Hash{})) {
		return q, port.ErrInvalidData
	}
	return q, nil
}
func executionProofHandler(store port.QueryStore, _ *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		fail := func(status int, code string) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
		}
		q, e := proofQuery(r)
		if e != nil {
			fail(400, "invalid_execution_proof_query")
			return
		}
		reader, ok := store.(port.ExecutionProofReader)
		if !ok {
			fail(503, "execution_proofs_unavailable")
			return
		}
		view, e := reader.ExecutionProof(r.Context(), q)
		if e != nil {
			switch {
			case errors.Is(e, port.ErrNotFound):
				fail(404, "execution_proof_not_collected")
			case errors.Is(e, port.ErrInvalidData):
				fail(400, "invalid_execution_proof_query")
			case errors.Is(e, port.ErrExecutionStale):
				fail(409, "execution_proof_cursor_stale")
			case errors.Is(e, port.ErrExecutionUninitialized), errors.Is(e, port.ErrExecutionRewinding), errors.Is(e, port.ErrExecutionMaintenance):
				fail(503, "execution_proofs_unavailable")
			default:
				fail(500, "execution_proof_error")
			}
			return
		}
		if view.Account.Address != q.Address || len(view.Proof) > 2<<20 || !json.Valid(view.Proof) || len(view.Account.Storage) > 128 || view.Account.Code != nil && len(*view.Account.Code) > 256<<10 {
			fail(500, "execution_proof_error")
			return
		}
		a := view.Account
		account := executionProofAccountJSON{Address: a.Address, Exists: a.Exists, Nonce: hexutil.Uint64(a.Nonce), ConsensusBalance: a.ConsensusBalance, StorageRoot: a.StorageRoot, CodeHash: a.CodeHash, Storage: []executionProofSlotJSON{}}
		if a.Code != nil {
			code := hexutil.Bytes(*a.Code)
			account.Code = &code
		}
		for _, slot := range a.Storage {
			account.Storage = append(account.Storage, executionProofSlotJSON{Key: slot.Key, Value: slot.Value})
		}
		out := executionProofJSON{Epoch: view.Epoch, Ref: view.Ref, Root: view.Root, Digest: view.Digest, SelectionHash: view.SelectionHash, Profile: view.Profile, Provider: view.Provider, Format: execwire.StateProofFormat, Verification: "mpt_at_execution_header_root", Account: account, Proof: view.Proof}
		_ = json.NewEncoder(w).Encode(out)
	})
}
